package tui

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/capability/release"
	"atm/internal/capability/scrum"
	"atm/internal/capability/workflow"
	"atm/internal/core"
	"atm/internal/store"
)

// newFlowMixTestModel registers a legacy non-flow capability (workflow), a
// flow (scrum) and a registry capability (release), which is the state plan
// 3/4 has not yet cleaned up: pane [2] must already behave as if only the
// flow existed.
func newFlowMixTestModel(t *testing.T) *Model {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	reg := capability.NewRegistry(workflow.New(), scrum.New(), release.New())
	m, err := NewModel(NewModelOpts{Service: s, Actor: testActor, Registry: reg})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	return m
}

func TestBracketKeysMoveBetweenLanes(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.lanes.selectDefault()

	m.tasks.handleKey(keyMsg("]"))
	if m.lanes.selected != laneOut {
		t.Fatalf("] selected %v, want laneOut", m.lanes.selected)
	}
	if got, want := m.tasks.filter, scrum.BoardOut("ATM"); got != want {
		t.Fatalf("] did not refocus the list: filter = %q, want %q", got, want)
	}
	m.tasks.handleKey(keyMsg("["))
	m.tasks.handleKey(keyMsg("["))
	if m.lanes.selected != laneInbox {
		t.Fatalf("two [ selected %v, want laneInbox", m.lanes.selected)
	}
	if got, want := m.tasks.filter, scrum.BoardInbox("ATM"); got != want {
		t.Fatalf("[ did not refocus the list: filter = %q, want %q", got, want)
	}
}

func TestCapabilitySwitcherListsFlowsOnly(t *testing.T) {
	m := newFlowMixTestModel(t)
	setupLanesProject(t, m, true)

	m.capability.openOverlay()
	if len(m.capability.entries) != 1 || m.capability.entries[0].name != "scrum" {
		var got []string
		for _, e := range m.capability.entries {
			got = append(got, e.name)
		}
		t.Fatalf("[C] entries = %v, want only the flow capability scrum", got)
	}
}

func TestCapabilityResolutionFallsBackWhenPersistedIsNotAFlow(t *testing.T) {
	m := newFlowMixTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if err := m.store.SetProjectBoards("ATM", &core.BoardsConfig{Capability: "workflow"}, m.actor); err != nil {
		t.Fatalf("SetProjectBoards: %v", err)
	}
	m.projectScope = "ATM"
	m.refreshAll()

	if m.capability.current != "scrum" {
		t.Fatalf("current = %q, want the first enabled flow (scrum)", m.capability.current)
	}
	// Resolution must not write back — only an explicit switch persists.
	cfg, _ := m.store.GetBoardsConfig("ATM")
	if cfg.Capability != "workflow" {
		t.Fatalf("persisted = %q; resolution must not write back", cfg.Capability)
	}
}

func TestSwitchingCapabilityLandsOnItsPipelineLane(t *testing.T) {
	m := newFlowMixTestModel(t)
	setupLanesProject(t, m, true)
	m.lanes.selectDefault()
	m.lanes.move(-1) // sitting on Inbox

	m.capability.current = "workflow" // pretend a stale in-session value
	m.capability.switchTo("scrum")

	if m.lanes.selected != lanePipeline {
		t.Fatalf("after a switch the strip sits on %v, want lanePipeline", m.lanes.selected)
	}
	if got, want := m.tasks.filter, scrum.BoardPipeline("ATM"); got != want {
		t.Fatalf("after a switch the list shows %q, want %q", got, want)
	}
}

func TestDispatchFromTheTasksPaneCarriesTheLaneScope(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	seedTask(t, m, "ATM", "claimed", "ATM:scrum:task")
	m.refreshAll()
	m.lanes.selectDefault()
	m.focused = paneTasks

	m.openDispatch()

	want := dispatchScope{Capability: "scrum", Lane: "Pipeline"}
	if got := m.dispatchDlg.scope; got != want {
		t.Fatalf("dispatch scope = %+v, want %+v", got, want)
	}
}

func TestDispatchFromTheProjectsPaneCarriesNoLaneScope(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.focused = paneProjects

	m.openDispatch()

	if got := (m.dispatchDlg.scope); got != (dispatchScope{}) {
		t.Fatalf("dispatch scope = %+v, want empty outside the Tasks pane", got)
	}
}
