package scrum

import (
	"fmt"

	"atm/internal/core"
)

// Reporter is the read-only side of the capability: it derives the inbound
// views nobody stores (a parent's children, a task's dependents) and reports
// what the manager should look at. It NEVER mutates a task — the manager is
// the decider; the reporter only shows evidence and gaps.
type Reporter struct {
	Store core.TaskService
}

// TaskLinks is one unit's topology, outbound (stored in its own payload) and
// inbound (derived by scanning the project).
type TaskLinks struct {
	PartOf    string   `json:"part_of,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	CoveredBy string   `json:"covered_by,omitempty"`

	Children   []string `json:"children,omitempty"`
	Dependents []string `json:"dependents,omitempty"`
	Covered    []string `json:"covered,omitempty"`
}

// Finding is one observation the manager should resolve. The reporter never
// repairs it.
type Finding struct {
	TaskID string `json:"task"`
	Detail string `json:"detail"`
}

// TaskSummary is one task's scrum-visible state.
type TaskSummary struct {
	TaskID    string   `json:"task"`
	Type      string   `json:"type,omitempty"`
	Stage     string   `json:"stage,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	PartOf    string   `json:"part_of,omitempty"`
	CoveredBy string   `json:"covered_by,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	BlockedBy []string `json:"blocked_by,omitempty"`
	Children  int      `json:"children,omitempty"`
	Spec      string   `json:"spec,omitempty"`
	Plan      string   `json:"plan,omitempty"`
}

// ProjectReport is the manager's read of one project through the scrum lens.
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

// Links returns the unit's outbound links plus the inbound views derived from
// its siblings. A sibling whose own payload is unparseable is skipped for
// derivation (Report lists it) — one bad payload never blinds the topology.
func (r *Reporter) Links(taskID string) (*TaskLinks, error) {
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return nil, fmt.Errorf("invalid task id %q", taskID)
	}
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	pl, err := DecodePayload(tk.Meta[CapabilityName])
	if err != nil {
		return nil, fmt.Errorf("%s: %w", taskID, err)
	}
	out := &TaskLinks{PartOf: pl.PartOf(), DependsOn: pl.DependsOn(), CoveredBy: pl.CoveredBy()}
	tasks, err := r.projectTasks(code)
	if err != nil {
		return nil, err
	}
	for _, other := range tasks {
		if other.ID == taskID {
			continue
		}
		opl, err := DecodePayload(other.Meta[CapabilityName])
		if err != nil {
			continue // listed by Report; never fatal here
		}
		if opl.PartOf() == taskID {
			out.Children = append(out.Children, other.ID)
		}
		if containsString(opl.DependsOn(), taskID) {
			out.Dependents = append(out.Dependents, other.ID)
		}
		if opl.CoveredBy() == taskID {
			out.Covered = append(out.Covered, other.ID)
		}
	}
	return out, nil
}

// Report reads the whole project through the scrum lens: the three lane
// rosters and the findings a manager should resolve.
//
// Pipeline and Out are derived from labels directly — they are what the claim
// and evict axes MEAN, so they hold whatever a project wires. Inbox is the one
// lane that depends on project wiring, so it is read from the seeded inbox
// board and reflects exactly what the TUI shows.
func (r *Reporter) Report(code string) (*ProjectReport, error) {
	tasks, err := r.projectTasks(code)
	if err != nil {
		return nil, err
	}
	rep := &ProjectReport{Project: code}

	// Pass 1: decode every payload once. A task whose own payload is
	// unparseable is a finding and contributes nothing to derivation.
	payloads := map[string]*Payload{}
	byID := map[string]*core.Task{}
	children := map[string][]string{}
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
		pl := payloads[tk.ID]
		if pl == nil || pl.PartOf() == "" {
			continue
		}
		children[pl.PartOf()] = append(children[pl.PartOf()], tk.ID)
	}

	for _, tk := range tasks {
		if drift := unknownScrumLabels(tk, code); len(drift) > 0 {
			rep.Findings = append(rep.Findings, Finding{
				TaskID: tk.ID,
				Detail: fmt.Sprintf("unmanaged scrum vocabulary drift: %v", drift),
			})
		}
		if _, types := labelValues(tk, code, TypeNamespace); len(types) > 1 {
			rep.Findings = append(rep.Findings, Finding{
				TaskID: tk.ID,
				Detail: fmt.Sprintf("more than one type label %v (re-run absorb to converge)", types),
			})
		}
		if reason := EvictedAs(tk, code); reason != "" {
			sum := TaskSummary{TaskID: tk.ID, Reason: reason}
			if pl := payloads[tk.ID]; pl != nil {
				sum.PartOf, sum.CoveredBy = pl.PartOf(), pl.CoveredBy()
				if (reason == OutCoveredBy || reason == OutDuplicate) && pl.CoveredBy() == "" {
					rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "evict reason " + reason + " without covered_by"})
				}
			}
			if typ := TypeOf(tk, code); typ != "" {
				rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "evicted but still carries the claim label " + typ})
			}
			rep.Out = append(rep.Out, sum)
			continue
		}
		typ := TypeOf(tk, code)
		if typ == "" {
			continue // unclaimed: the inbox roster below decides whether it is eligible
		}
		sum := TaskSummary{TaskID: tk.ID, Type: typ, Stage: StageOf(tk, code), Children: len(children[tk.ID])}
		if sum.Stage == "" {
			rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "claimed as " + typ + " with no stage label"})
		}
		if pl := payloads[tk.ID]; pl != nil {
			sum.PartOf = pl.PartOf()
			sum.DependsOn = pl.DependsOn()
			sum.Spec = pl.Spec()
			sum.Plan = pl.Plan()
			rep.Findings = append(rep.Findings, r.unitFindings(tk, code, typ, sum.Stage, pl, byID, children)...)
			for _, dep := range pl.DependsOn() {
				other, ok := byID[dep]
				if !ok {
					sum.BlockedBy = append(sum.BlockedBy, dep)
					rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "missing linked target " + dep + " (depends_on)"})
					continue
				}
				if StageOf(other, code) != StageDone && EvictedAs(other, code) == "" {
					sum.BlockedBy = append(sum.BlockedBy, dep)
				}
			}
		}
		rep.Pipeline = append(rep.Pipeline, sum)
	}

	inbox, err := r.Store.ListTasksErr(core.QueryFilters{Project: code, Labels: []string{BoardInbox(code)}})
	if err != nil {
		rep.Findings = append(rep.Findings, Finding{
			TaskID: code,
			Detail: "inbox lane board " + BoardInbox(code) + " is missing or unreadable (run `atm capability scrum seed`)",
		})
	}
	for _, tk := range inbox {
		rep.Inbox = append(rep.Inbox, tk.ID)
	}
	return rep, nil
}

// unitFindings collects the gaps the recorder maintains but a raw label edit
// can break, plus the convergence the manager owes the ledger.
func (r *Reporter) unitFindings(tk *core.Task, code, typ, stage string, pl *Payload, byID map[string]*core.Task, children map[string][]string) []Finding {
	var out []Finding
	if parent := pl.PartOf(); parent != "" {
		p, ok := byID[parent]
		switch {
		case !ok:
			out = append(out, Finding{TaskID: tk.ID, Detail: "missing linked target " + parent + " (part_of)"})
		case EvictedAs(p, code) != "":
			out = append(out, Finding{TaskID: tk.ID, Detail: "part_of target " + parent + " has been evicted from scrum"})
		case TypeOf(p, code) == "":
			out = append(out, Finding{TaskID: tk.ID, Detail: "part_of target " + parent + " is not claimed by scrum"})
		}
	}
	// The convergence the manager owes: a parent whose work is finished but
	// whose own finish socket is unstamped. Downstream sees nothing until it is.
	if (typ == TypeStory || typ == TypeEpic) && stage != StageDone {
		kids := children[tk.ID]
		if len(kids) > 0 && everyChildDone(kids, byID, code) {
			out = append(out, Finding{TaskID: tk.ID, Detail: "every child is done but this " + typ + " is not stamped " + StageDone})
		}
	}
	if typ == TypeDesign && pl.Plan() == "" && stage == StageDone {
		out = append(out, Finding{TaskID: tk.ID, Detail: "design finished without a plan locator (record it with `scrum plan`)"})
	}
	return out
}

// everyChildDone reports whether every live child converged. An evicted child
// is not part of what the parent owes.
func everyChildDone(kids []string, byID map[string]*core.Task, code string) bool {
	live := 0
	for _, id := range kids {
		c, ok := byID[id]
		if !ok || EvictedAs(c, code) != "" {
			continue
		}
		live++
		if StageOf(c, code) != StageDone {
			return false
		}
	}
	return live > 0
}

// unknownScrumLabels reports labels in this capability's namespaces whose
// values this version does not know — vocabulary drift a manager should
// resolve. Recorders converge only their own known axes.
func unknownScrumLabels(tk *core.Task, code string) []string {
	var out []string
	check := func(ns string, valid func(string) bool) {
		existing, vals := labelValues(tk, code, ns)
		for i, v := range vals {
			if !valid(v) {
				out = append(out, existing[i])
			}
		}
	}
	check(TypeNamespace, validType)
	check(StageNamespace, validStage)
	check(OutNamespace, validReason)
	return out
}
