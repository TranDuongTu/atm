package qa

import (
	"fmt"

	"atm/internal/core"
)

// Reporter is the read-only side of the capability. It NEVER mutates a task.
type Reporter struct {
	Store core.TaskService
}

// Finding is one observation the manager should resolve.
type Finding struct {
	TaskID string `json:"task"`
	Detail string `json:"detail"`
}

// TaskSummary is one task's qa-visible state.
type TaskSummary struct {
	TaskID    string   `json:"task"`
	State     string   `json:"state,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	PartOf    string   `json:"part_of,omitempty"`
	CoveredBy string   `json:"covered_by,omitempty"`
	Scaffolds []string `json:"scaffolds,omitempty"`
	Live      []string `json:"live_scaffolds,omitempty"`
}

// ProjectReport is the manager's read of one project through the qa lens.
type ProjectReport struct {
	Project  string        `json:"project"`
	Inbox    []string      `json:"inbox,omitempty"`
	Pipeline []TaskSummary `json:"pipeline,omitempty"`
	Out      []TaskSummary `json:"out,omitempty"`
	Findings []Finding     `json:"findings,omitempty"`
}

func (r *Reporter) projectTasks(code string) ([]*core.Task, error) {
	return r.Store.ListTasksErr(core.QueryFilters{Project: code})
}

// Report reads the whole project through the qa lens. Pipeline and Out come
// from the labels, because that is what the axes mean; Inbox comes from the
// seeded board, because eligibility is project wiring.
func (r *Reporter) Report(code string) (*ProjectReport, error) {
	tasks, err := r.projectTasks(code)
	if err != nil {
		return nil, err
	}
	rep := &ProjectReport{Project: code}

	payloads := map[string]*Payload{}
	byID := map[string]*core.Task{}
	for _, tk := range tasks {
		byID[tk.ID] = tk
		pl, err := DecodePayload(tk.Meta[CapabilityName])
		if err != nil {
			rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "payload unparseable (hand-repair needed)"})
			continue
		}
		payloads[tk.ID] = pl
	}

	for _, tk := range tasks {
		if drift := unknownQALabels(tk, code); len(drift) > 0 {
			rep.Findings = append(rep.Findings, Finding{
				TaskID: tk.ID,
				Detail: fmt.Sprintf("unmanaged qa vocabulary drift: %v", drift),
			})
		}
		if _, states := labelValues(tk, code, ClaimNamespace); len(states) > 1 {
			rep.Findings = append(rep.Findings, Finding{
				TaskID: tk.ID,
				Detail: fmt.Sprintf("more than one qa state label %v (re-run a verb to converge)", states),
			})
		}
		pl := payloads[tk.ID]
		if reason := EvictedAs(tk, code); reason != "" {
			sum := TaskSummary{TaskID: tk.ID, Reason: reason}
			if pl != nil {
				sum.PartOf, sum.CoveredBy = pl.PartOf(), pl.CoveredBy()
				if reason == OutCoveredBy && pl.CoveredBy() == "" {
					rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "evict reason " + reason + " without covered_by"})
				}
			}
			if StateOf(tk, code) != "" {
				rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "evicted but still carries a qa claim label"})
			}
			rep.Out = append(rep.Out, sum)
			continue
		}
		state := StateOf(tk, code)
		if state == "" {
			// A passed scaffold keeps its part_of but gives up its claim; it
			// is not a finding, it is the record of a scaffold that is done.
			continue
		}
		sum := TaskSummary{TaskID: tk.ID, State: state}
		if pl != nil {
			sum.PartOf = pl.PartOf()
			sum.Scaffolds = pl.Scaffolds()
			rep.Findings = append(rep.Findings, r.unitFindings(tk, code, state, pl, byID)...)
			if pl.PartOf() == "" {
				sum.Live = liveScaffoldsOf(pl, byID, code)
				// The convergence the manager owes: every scaffold has passed,
				// but the original is not certified, so downstream sees nothing.
				if state == StateTesting && len(pl.Scaffolds()) > 0 && len(sum.Live) == 0 {
					rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "every scaffold has passed but this original is not stamped " + StateDone})
				}
			}
		}
		rep.Pipeline = append(rep.Pipeline, sum)
	}

	inbox, err := r.Store.ListTasksErr(core.QueryFilters{Project: code, Labels: []string{BoardInbox(code)}})
	if err != nil {
		rep.Findings = append(rep.Findings, Finding{
			TaskID: code,
			Detail: "inbox lane board " + BoardInbox(code) + " is missing or unreadable (run `atm capability qa seed`)",
		})
	}
	for _, tk := range inbox {
		rep.Inbox = append(rep.Inbox, tk.ID)
	}
	return rep, nil
}

func (r *Reporter) unitFindings(tk *core.Task, code, state string, pl *Payload, byID map[string]*core.Task) []Finding {
	var out []Finding
	if parent := pl.PartOf(); parent != "" {
		p, ok := byID[parent]
		switch {
		case !ok:
			out = append(out, Finding{TaskID: tk.ID, Detail: "scaffold's original " + parent + " is missing"})
		case StateOf(p, code) == "" && EvictedAs(p, code) == "":
			out = append(out, Finding{TaskID: tk.ID, Detail: "scaffold's original " + parent + " is no longer claimed by qa (released?)"})
		}
		if state == StateDone {
			// Only reachable by a hand-assigned label: the verbs cannot do it.
			out = append(out, Finding{TaskID: tk.ID, Detail: "scaffold carries " + code + ":qa:done — the finish socket belongs to originals only"})
		}
		return out
	}
	for _, id := range pl.Scaffolds() {
		if _, ok := byID[id]; !ok {
			out = append(out, Finding{TaskID: tk.ID, Detail: "roster names a missing scaffold " + id})
		}
	}
	return out
}

// liveScaffoldsOf lists the roster entries still holding a qa claim.
func liveScaffoldsOf(pl *Payload, byID map[string]*core.Task, code string) []string {
	var live []string
	for _, id := range pl.Scaffolds() {
		sc, ok := byID[id]
		if !ok || EvictedAs(sc, code) != "" {
			continue
		}
		if StateOf(sc, code) != "" {
			live = append(live, id)
		}
	}
	return live
}

// unknownQALabels reports labels in this capability's namespaces whose values
// this version does not know — vocabulary drift a manager should resolve.
func unknownQALabels(tk *core.Task, code string) []string {
	var out []string
	check := func(ns string, valid func(string) bool) {
		existing, vals := labelValues(tk, code, ns)
		for i, v := range vals {
			if !valid(v) {
				out = append(out, existing[i])
			}
		}
	}
	check(ClaimNamespace, func(v string) bool { return containsString(States(), v) })
	check(OutNamespace, validReason)
	return out
}
