package compose

import (
	"reflect"
	"testing"

	"atm/internal/core"
	"atm/internal/profile"
)

// TestDispatchActionsDerivesEveryRowFromTheRecord: the dialog's list is a
// VIEW of the checklist records — persona from suits, target/targets/mode
// from the dispatch axes. Nothing here is a dialog-only concept, so a row
// cannot show something Compose would not do.
func TestDispatchActionsDerivesEveryRowFromTheRecord(t *testing.T) {
	recs := testChecklists()
	rows, err := testService(&fakeSvc{all: recs}).DispatchActions("ATM", "claude")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ActionRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	coding := byName["scrum-coding"]
	if coding.Persona != "developer" {
		t.Errorf("persona = %q, want developer (from suits[0])", coding.Persona)
	}
	if coding.Target != core.ChecklistTargetTask || coding.Targets != "scrum:task" {
		t.Errorf("target/targets = %q/%q", coding.Target, coding.Targets)
	}
	if coding.Mode != core.ChecklistModeEager || coding.Purpose != "implement one increment" {
		t.Errorf("mode/purpose = %q/%q", coding.Mode, coding.Purpose)
	}
	if coding.Origin != "scrumban@1.0.0" {
		t.Errorf("origin = %q", coding.Origin)
	}
	if byName["scrum-design"].Mode != core.ChecklistModeInteractive {
		t.Errorf("scrum-design mode = %q, want interactive", byName["scrum-design"].Mode)
	}
	if byName["planning"].Target != core.ChecklistTargetProject {
		t.Errorf("planning target = %q, want project", byName["planning"].Target)
	}
}

// An action nobody suits is listed — hiding it would leave a project record
// invisible — but it is marked undispatchable, which is exactly what Compose
// does with it: refuse, unless a persona is named.
func TestDispatchActionsMarksASuitslessActionUndispatchable(t *testing.T) {
	recs := testChecklists()
	rows, err := testService(&fakeSvc{all: recs}).DispatchActions("ATM", "claude")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range rows {
		if r.Name != "orphan" {
			continue
		}
		found = true
		if r.Persona != "" || r.Dispatchable() {
			t.Errorf("orphan row = %+v, want no persona and not dispatchable", r)
		}
	}
	if !found {
		t.Fatal("a suits-less action must still be listed")
	}
}

func TestDispatchActionsSortsByName(t *testing.T) {
	rows, err := testService(&fakeSvc{all: testChecklists()}).DispatchActions("ATM", "claude")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"orphan", "planning", "qa", "scrum-coding", "scrum-design"}
	var got []string
	for _, r := range rows {
		got = append(got, r.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

// Warnings are AGENT-RELATIVE: attestation is per (endpoint × machine ×
// agent), so cycling the dialog's agent must be able to change them. The
// readiness computation is asked for the named agent and read ONCE for the
// whole list, not once per row.
func TestDispatchActionsWarningsAreAgentRelativeAndComputedOnce(t *testing.T) {
	s := testService(&fakeSvc{all: testChecklists()})
	calls := 0
	var gotAgents []string
	s.Readiness = func(code string, agents []string) *profile.Readiness {
		calls++
		gotAgents = agents
		stale := "stamp is 21 days old on this agent"
		if agents[0] == "codex" {
			stale = "never attested on this agent"
		}
		return &profile.Readiness{Actions: []profile.ActionReadiness{{
			Name:     "scrum-coding",
			Warnings: map[string][]profile.Warning{agents[0]: {{Rung: profile.RungAttested, Text: stale}}},
		}}}
	}
	rows, err := s.DispatchActions("ATM", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("readiness computed %d times for one list, want 1 — drawing a list must not cost N readiness passes", calls)
	}
	if !reflect.DeepEqual(gotAgents, []string{"claude"}) {
		t.Fatalf("readiness asked for %v, want [claude]", gotAgents)
	}
	var coding ActionRow
	for _, r := range rows {
		if r.Name == "scrum-coding" {
			coding = r
		}
	}
	want := []string{"checklist scrum-coding: stamp is 21 days old on this agent"}
	if !reflect.DeepEqual(coding.Warnings, want) {
		t.Fatalf("warnings = %v, want %v", coding.Warnings, want)
	}

	// The same list for a different agent answers differently.
	rows, err = s.DispatchActions("ATM", "codex")
	if err != nil {
		t.Fatal(err)
	}
	codexWant := []string{"checklist scrum-coding: never attested on this agent"}
	for _, r := range rows {
		if r.Name == "scrum-coding" && !reflect.DeepEqual(r.Warnings, codexWant) {
			t.Fatalf("codex warnings = %v, want %v; cycling the agent must recompute them", r.Warnings, codexWant)
		}
	}
}

// Without the injection the list still warns — the same question, minus the
// machine- and agent-level rungs — rather than going silent.
func TestDispatchActionsFallsBackWhenReadinessIsAbsent(t *testing.T) {
	s := testService(&fakeSvc{all: testChecklists()})
	s.Readiness = nil
	rows, err := s.DispatchActions("ATM", "claude")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Name != "qa" {
			continue
		}
		want := []string{"checklist qa: requires capability qa (not enabled)"}
		if !reflect.DeepEqual(r.Warnings, want) {
			t.Fatalf("qa warnings = %v, want %v", r.Warnings, want)
		}
		return
	}
	t.Fatal("qa row missing")
}

// TestEligibleTasksFiltersByTheTargetsExpression: the dialog offers only the
// tasks an action may run on, evaluated by the store's resolver — the same
// call Compose's targets check makes, so the offered list and the launch
// warning cannot disagree.
func TestEligibleTasksFiltersByTheTargetsExpression(t *testing.T) {
	f := &fakeSvc{all: testChecklists(), eligible: map[string][]string{
		"scrum:task": {"ATM-1", "ATM-2"},
		"":           {"ATM-1", "ATM-2", "ATM-3"},
	}}
	s := testService(f)
	rows, err := s.DispatchActions("ATM", "claude")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ActionRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}

	got, err := s.EligibleTasks("ATM", byName["scrum-coding"])
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "ATM-1" {
		t.Fatalf("constrained action offered %v, want the 2 matching tasks", got)
	}

	// No expression: every task of the project is eligible.
	got, err = s.EligibleTasks("ATM", byName["scrum-design"])
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("unconstrained action offered %d tasks, want all 3", len(got))
	}

	// A project-target action has no task to offer at all.
	got, err = s.EligibleTasks("ATM", byName["planning"])
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("project-target action offered tasks: %v", got)
	}
}

// AppliedProfiles is DERIVED from the rows' origins, for the same reason
// `profile status` derives it: a stored list can disagree with the records,
// a derived one cannot.
func TestAppliedProfilesDerivesFromOrigins(t *testing.T) {
	rows, err := testService(&fakeSvc{all: testChecklists()}).DispatchActions("ATM", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"scrumban@1.0.0", "user"}; !reflect.DeepEqual(AppliedProfiles(rows), want) {
		t.Fatalf("applied = %v, want %v", AppliedProfiles(rows), want)
	}
}

func TestDispatchActionsNoProject(t *testing.T) {
	rows, err := testService(&fakeSvc{all: testChecklists()}).DispatchActions("", "claude")
	if err != nil || rows != nil {
		t.Fatalf("rows = %v, err = %v; a projectless dialog has no actions", rows, err)
	}
}
