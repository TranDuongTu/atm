package tui

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/capability/release"
	"atm/internal/capability/scrum"
	"atm/internal/store"
)

// newLanesTestModel builds a Model over one FLOW capability (scrum) plus one
// REGISTRY capability (release). The mix is deliberate: pane [2] is "one flow
// capability × one lane", so the lane strip must resolve its lanes from the
// flow and ignore the registry capability entirely.
func newLanesTestModel(t *testing.T) *Model {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	reg := capability.NewRegistry(scrum.New(), release.New())
	m, err := NewModel(NewModelOpts{Service: s, Actor: testActor, Registry: reg})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	return m
}

// setupLanesProject scopes the model to a seeded project. seedVocab false
// leaves the lane boards unseeded, which is the "broken lane" fixture.
func setupLanesProject(t *testing.T, m *Model, seedVocab bool) {
	t.Helper()
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if seedVocab {
		if _, err := m.regFor("ATM").EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
			t.Fatalf("ensure: %v", err)
		}
	}
	m.refreshAll()
}

func TestLanesRefreshResolvesFlowLanesInFixedOrder(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)

	want := [3]string{scrum.BoardInbox("ATM"), scrum.BoardPipeline("ATM"), scrum.BoardOut("ATM")}
	for i, w := range want {
		if got := m.lanes.lanes[i].BoardName; got != w {
			t.Fatalf("lanes[%d].BoardName = %q, want %q", i, got, w)
		}
		if m.lanes.lanes[i].Broken {
			t.Fatalf("lanes[%d] (%s) is Broken; the vocabulary is seeded", i, w)
		}
	}
	if m.lanes.lanes[laneInbox].Kind != laneInbox ||
		m.lanes.lanes[lanePipeline].Kind != lanePipeline ||
		m.lanes.lanes[laneOut].Kind != laneOut {
		t.Fatalf("lane kinds are not in fixed Inbox/Pipeline/Out order: %+v", m.lanes.lanes)
	}
}

func TestLanesSelectDefaultSelectsPipelineAndPushesFocus(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)

	m.lanes.selectDefault()

	if m.lanes.selected != lanePipeline {
		t.Fatalf("selected = %v, want lanePipeline", m.lanes.selected)
	}
	if got, want := m.tasks.filter, scrum.BoardPipeline("ATM"); got != want {
		t.Fatalf("tasks.filter = %q, want %q", got, want)
	}
	if m.tasks.focus.mode != focusOff {
		t.Fatalf("tasks.focus.mode = %v, want focusOff", m.tasks.focus.mode)
	}
}

func TestLanesMoveSelectsInboxAndClampsAtTheEdge(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.lanes.selectDefault()

	m.lanes.move(-1)
	if m.lanes.selected != laneInbox {
		t.Fatalf("after move(-1) selected = %v, want laneInbox", m.lanes.selected)
	}
	if got, want := m.tasks.filter, scrum.BoardInbox("ATM"); got != want {
		t.Fatalf("move(-1) did not re-apply focus: filter = %q, want %q", got, want)
	}

	m.lanes.move(-1)
	if m.lanes.selected != laneInbox {
		t.Fatalf("move(-1) at the left edge wrapped to %v; selection must clamp", m.lanes.selected)
	}

	m.lanes.move(1)
	m.lanes.move(1)
	if m.lanes.selected != laneOut {
		t.Fatalf("after two move(1) selected = %v, want laneOut", m.lanes.selected)
	}
	m.lanes.move(1)
	if m.lanes.selected != laneOut {
		t.Fatalf("move(1) at the right edge wrapped to %v; selection must clamp", m.lanes.selected)
	}
	if got, want := m.tasks.filter, scrum.BoardOut("ATM"); got != want {
		t.Fatalf("tasks.filter = %q, want %q", got, want)
	}
}

func TestLanesCountsReflectBoardMembership(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	seedTask(t, m, "ATM", "claimed one", "ATM:scrum:feature", "ATM:scrum-stage:building")
	seedTask(t, m, "ATM", "claimed two", "ATM:scrum:chore", "ATM:scrum-stage:building")
	seedTask(t, m, "ATM", "settled", "ATM:scrum-out:duplicate")
	m.refreshAll()

	if got := m.lanes.lanes[lanePipeline].Count; got != 2 {
		t.Fatalf("pipeline count = %d, want 2", got)
	}
	if got := m.lanes.lanes[laneOut].Count; got != 1 {
		t.Fatalf("out count = %d, want 1", got)
	}
}

func TestLanesMissingBoardIsBrokenAndFocusesNothing(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, false) // no EnsureVocabulary: lane boards do not exist
	seedTask(t, m, "ATM", "a task with no lane", "ATM:stray")
	m.refreshAll()

	for i := range m.lanes.lanes {
		if !m.lanes.lanes[i].Broken {
			t.Fatalf("lanes[%d] (%s) not Broken; its board was never seeded", i, m.lanes.lanes[i].BoardName)
		}
		if got := m.lanes.lanes[i].Count; got != 0 {
			t.Fatalf("lanes[%d] Count = %d, want 0 for a broken lane", i, got)
		}
	}

	m.lanes.selectDefault()
	if len(m.tasks.rows) != 0 {
		t.Fatalf("a broken lane must focus an empty result; got %d rows", len(m.tasks.rows))
	}
}

func TestLanesEmptyWithoutAFlowCapability(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	// release is a registry capability: no lanes to resolve.
	m.capability.current = "release"
	m.lanes.refresh()

	for i := range m.lanes.lanes {
		if m.lanes.lanes[i].BoardName != "" {
			t.Fatalf("lanes[%d].BoardName = %q, want empty for a non-flow capability",
				i, m.lanes.lanes[i].BoardName)
		}
	}
}
