package codereview

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

// TaskSummary is one task's codereview-visible state.
type TaskSummary struct {
	TaskID string `json:"task"`
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
	PR     string `json:"pr,omitempty"`
	Report string `json:"report,omitempty"`
}

// ProjectReport is the manager's read of one project through the codereview
// lens. Inbox is deliberately first: its SIZE is the signal this capability
// exists to give.
type ProjectReport struct {
	Project  string        `json:"project"`
	Inbox    []string      `json:"inbox,omitempty"`
	Pipeline []TaskSummary `json:"pipeline,omitempty"`
	Out      []TaskSummary `json:"out,omitempty"`
	Findings []Finding     `json:"findings,omitempty"`
}

// ByState groups the pipeline roster by review state, for the manager's read
// of "what is scheduled and what is actually moving". There is no staleness
// threshold here on purpose: the reporter shows the roster, and the manager —
// not a hard-coded number of days — decides what counts as stuck.
func (rep *ProjectReport) ByState() map[string][]string {
	out := map[string][]string{}
	for _, s := range rep.Pipeline {
		out[s.State] = append(out[s.State], s.TaskID)
	}
	return out
}

func (r *Reporter) projectTasks(code string) ([]*core.Task, error) {
	return r.Store.ListTasksErr(core.QueryFilters{Project: code})
}

// Report reads the whole project through the codereview lens. Pipeline and Out
// come from the labels; Inbox comes from the seeded board, because eligibility
// is project wiring.
func (r *Reporter) Report(code string) (*ProjectReport, error) {
	tasks, err := r.projectTasks(code)
	if err != nil {
		return nil, err
	}
	rep := &ProjectReport{Project: code}

	for _, tk := range tasks {
		if drift := unknownLabels(tk, code); len(drift) > 0 {
			rep.Findings = append(rep.Findings, Finding{
				TaskID: tk.ID,
				Detail: fmt.Sprintf("unmanaged codereview vocabulary drift: %v", drift),
			})
		}
		if _, states := labelValues(tk, code, ClaimNamespace); len(states) > 1 {
			rep.Findings = append(rep.Findings, Finding{
				TaskID: tk.ID,
				Detail: fmt.Sprintf("more than one codereview state label %v (re-run a verb to converge)", states),
			})
		}
		pl, plErr := DecodePayload(tk.Meta[CapabilityName])
		if plErr != nil {
			rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "payload unparseable (hand-repair needed)"})
		}
		if reason := EvictedAs(tk, code); reason != "" {
			sum := TaskSummary{TaskID: tk.ID, Reason: reason}
			if plErr == nil {
				sum.PR = pl.PR()
			}
			if StateOf(tk, code) != "" {
				rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "evicted but still carries a codereview claim label"})
			}
			rep.Out = append(rep.Out, sum)
			continue
		}
		state := StateOf(tk, code)
		if state == "" {
			continue
		}
		sum := TaskSummary{TaskID: tk.ID, State: state}
		if plErr == nil {
			sum.PR, sum.Report = pl.PR(), pl.Report()
			// absorb cannot produce this, so its presence means someone
			// assigned the label by hand. Surface it rather than trust it.
			if sum.PR == "" {
				rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "claimed by codereview with no pull request recorded (hand-assigned label?)"})
			}
		}
		rep.Pipeline = append(rep.Pipeline, sum)
	}

	inbox, err := r.Store.ListTasksErr(core.QueryFilters{Project: code, Labels: []string{BoardInbox(code)}})
	if err != nil {
		rep.Findings = append(rep.Findings, Finding{
			TaskID: code,
			Detail: "inbox lane board " + BoardInbox(code) + " is missing or unreadable (run `atm capability codereview seed`)",
		})
	}
	for _, tk := range inbox {
		rep.Inbox = append(rep.Inbox, tk.ID)
	}
	return rep, nil
}

// unknownLabels reports labels in this capability's namespaces whose values
// this version does not know — vocabulary drift a manager should resolve.
func unknownLabels(tk *core.Task, code string) []string {
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
