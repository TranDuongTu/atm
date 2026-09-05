package compose

import (
	"sort"

	"atm/internal/core"
	"atm/internal/profile"
)

// ActionRow is one dispatchable action as the dispatch dialog shows it: what
// it is called, who runs it, what it operates on, and what is wrong with
// dispatching it here and now.
//
// Every field is DERIVED from the checklist record — nothing here is a
// dialog-only concept. The dialog is a view of the binding Compose performs,
// so a row that showed something Compose would not do is a bug, not a design
// choice.
type ActionRow struct {
	Name    string
	Purpose string
	// Persona is the action's suits[0] — who this action runs as unless the
	// dispatch overrides it. Empty when the action suits nobody, which the
	// dialog shows and Compose refuses to dispatch without an override.
	Persona string
	// Target is project or task; Targets is the label expression narrowing
	// which tasks a task-target action may run on ("" = every task).
	Target  string
	Targets string
	Mode    string // eager | interactive | resident
	// Origin is the record's provenance — "<profile>@<version>" or "user".
	// The dialog's profile cycler filters on it.
	Origin string
	// Warnings are the unmet requirements for THIS agent, from the readiness
	// computation. They grey the row; they never make it unselectable
	// (warn never block, spec decision 4).
	Warnings []string
}

// Dispatchable reports whether Compose could bind this action without an
// explicit persona override.
func (r ActionRow) Dispatchable() bool { return r.Persona != "" }

// DispatchActions lists every action the project can dispatch, in name
// order, with warnings computed for the given agent.
//
// The agent matters: attestation is per (endpoint × machine × agent), so
// "is this action ready?" has a different answer per harness, and the dialog
// recomputes this list when its agent cycler moves. An empty agent yields
// the machine-level answer only.
//
// It reads THE readiness computation once and indexes it, rather than asking
// per row: readiness assembles the project's channels, profiles and stamps,
// and doing that N times to draw one list would make opening the dialog cost
// N times more than it should.
func (s *Service) DispatchActions(code, agent string) ([]ActionRow, error) {
	if code == "" {
		return nil, nil
	}
	recs, err := s.Svc.ChecklistRecords(code)
	if err != nil {
		return nil, err
	}
	warnings := s.actionWarnings(code, agent, recs)
	rows := make([]ActionRow, 0, len(recs))
	for _, rec := range recs {
		row := ActionRow{
			Name:     rec.Name,
			Purpose:  rec.Purpose,
			Target:   rec.Target,
			Targets:  rec.Targets,
			Mode:     rec.Mode,
			Origin:   rec.Origin,
			Warnings: warnings[rec.Name],
		}
		if len(rec.Suits) > 0 {
			row.Persona = rec.Suits[0]
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, nil
}

// actionWarnings computes every action's unmet requirements in one pass.
// Readiness answers for all of them at once; the fallback (a Service built
// without the injection) evaluates the same question per record, minus the
// machine- and agent-level rungs.
func (s *Service) actionWarnings(code, agent string, recs []core.ChecklistRecord) map[string][]string {
	out := map[string][]string{}
	if s.Readiness != nil && agent != "" {
		if r := s.Readiness(code, []string{agent}); r != nil {
			for _, a := range r.Actions {
				out[a.Name] = warningTexts(a.Name, a.Warnings[agent])
			}
			return out
		}
	}
	var enabled []string
	if s.EnabledCapabilities != nil {
		enabled = s.EnabledCapabilities(code)
	}
	channels := s.channelViewsUnprobed(code)
	for _, rec := range recs {
		out[rec.Name] = core.ChecklistRequireWarnings(rec, enabled, channels)
	}
	return out
}

// warningTexts renders readiness warnings as the dialog and the launcher
// both show them: prefixed with the action they belong to, because a
// warning read out of its row has to name its own subject.
func warningTexts(name string, ws []profile.Warning) []string {
	if len(ws) == 0 {
		return nil
	}
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, "checklist "+name+": "+w.Text)
	}
	return out
}

// EligibleTasks lists the tasks an action may be dispatched on: every task of
// the project for an unconstrained action, and the tasks matching the targets
// expression otherwise. A project-target action has no task, so it returns
// nothing.
//
// The expression is evaluated by the store's resolver through the ordinary
// task query — the same call Compose's targets check makes, so the list the
// dialog offers and the warning a launch prints cannot disagree about what
// "eligible" means.
func (s *Service) EligibleTasks(code string, row ActionRow) ([]*core.Task, error) {
	if code == "" || row.Target != core.ChecklistTargetTask {
		return nil, nil
	}
	tasks, err := s.Svc.ListTasksErr(core.QueryFilters{Project: code, Expr: row.Targets})
	if err != nil {
		return nil, err
	}
	// Project RECORDS are tasks in this store — a checklist, a channel and a
	// persona each live as one — but nothing is ever dispatched ON a record.
	// They are filtered here rather than left to the targets expression
	// because an action with no expression would otherwise offer the
	// project's own checklists as work to do.
	out := tasks[:0]
	for _, t := range tasks {
		if !core.IsRecordTask(t) {
			out = append(out, t)
		}
	}
	return out, nil
}

// AppliedProfiles lists the distinct record origins the project's actions
// carry, in name order, so the dialog can scope its action list to one
// profile. It is derived from the records themselves — the same reason
// `profile status` derives applied profiles from origins rather than storing
// them: a stored list can disagree with the records, a derived one cannot.
func AppliedProfiles(rows []ActionRow) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if r.Origin == "" || seen[r.Origin] {
			continue
		}
		seen[r.Origin] = true
		out = append(out, r.Origin)
	}
	sort.Strings(out)
	return out
}
