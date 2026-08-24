package tui

import (
	"strings"
	"testing"

	"atm/internal/capability/scrum"
)

// setupLaneWalk seeds one project with a task standing in each lane and
// drives the REAL entry point: selecting the project from pane [1]. The
// project-select handler is what wires the panes together, so an integration
// test that pokes projectScope directly would prove nothing about it.
func setupLaneWalk(t *testing.T) *Model {
	t.Helper()
	m := newLanesTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := m.regFor("ATM").EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "undecided work")                         // Inbox: unclaimed
	seedTask(t, m, "ATM", "being built", "ATM:scrum:task")          // Pipeline: claimed
	seedTask(t, m, "ATM", "settled out", "ATM:scrum-out:duplicate") // Out: evicted
	m.projectScope = ""
	m.refreshAll()

	// Select ATM from the Projects pane, exactly as a user would.
	m.focused = paneProjects
	m.projects.refresh()
	m.projects.cursor = 0
	update(t, m, "s")
	if m.projectScope != "ATM" {
		t.Fatalf("projectScope = %q, want ATM after select", m.projectScope)
	}
	return m
}

func TestProjectSelectLandsOnThePipelineLane(t *testing.T) {
	m := setupLaneWalk(t)

	if m.lanes.selected != lanePipeline {
		t.Fatalf("project select landed on %v, want lanePipeline", m.lanes.selected)
	}
	if got, want := m.tasks.filter, scrum.BoardPipeline("ATM"); got != want {
		t.Fatalf("tasks.filter = %q, want %q", got, want)
	}
	v := stripANSI(m.tasks.View())
	if !strings.Contains(v, "being built") {
		t.Fatalf("the pipeline task is not listed:\n%s", v)
	}
	for _, col := range []string{"ID", "TITLE", "ANNOTATE", "UPDATED"} {
		if !strings.Contains(v, col) {
			t.Fatalf("column %s missing from the list:\n%s", col, v)
		}
	}
}

func TestWalkingTheLanesShowsEachLanesWork(t *testing.T) {
	m := setupLaneWalk(t)
	m.focused = paneTasks

	update(t, m, "[") // Pipeline -> Inbox
	v := stripANSI(m.tasks.View())
	if !strings.Contains(v, "undecided work") {
		t.Fatalf("Inbox does not list the unclaimed task:\n%s", v)
	}
	if strings.Contains(v, "being built") {
		t.Fatalf("Inbox lists claimed work:\n%s", v)
	}

	update(t, m, "]") // Inbox -> Pipeline
	update(t, m, "]") // Pipeline -> Out
	v = stripANSI(m.tasks.View())
	if !strings.Contains(v, "settled out") {
		t.Fatalf("Out does not list the evicted task:\n%s", v)
	}
	if strings.Contains(v, "undecided work") {
		t.Fatalf("Out lists unclaimed work:\n%s", v)
	}
}

func TestLanePaneKeepsItsFooterAndHeight(t *testing.T) {
	m := setupLaneWalk(t)

	v := stripANSI(m.tasks.View())
	if !strings.Contains(v, "showing 1-1 of 1") {
		t.Fatalf("footer count missing from the pane:\n%s", v)
	}
	lines := strings.Split(v, "\n")
	if len(lines) != m.tasks.contentHeight {
		t.Fatalf("pane rendered %d lines, want exactly contentHeight %d", len(lines), m.tasks.contentHeight)
	}
	// The lane strip is the last laneStripHeight lines, and the list keeps
	// everything above it.
	strip := strings.Join(lines[len(lines)-laneStripHeight:], "\n")
	for _, lane := range []string{"Inbox", "Pipeline", "Out"} {
		if !strings.Contains(strip, lane) {
			t.Fatalf("lane strip missing %s:\n%s", lane, strip)
		}
	}
}

func TestLaneCountsMatchWhatEachLaneLists(t *testing.T) {
	m := setupLaneWalk(t)

	for _, tc := range []struct {
		kind laneKind
		want int
	}{{laneInbox, 1}, {lanePipeline, 1}, {laneOut, 1}} {
		if got := m.lanes.lanes[tc.kind].Count; got != tc.want {
			t.Errorf("%v card shows %d, want %d", tc.kind, got, tc.want)
		}
		m.lanes.selected = tc.kind
		m.lanes.applyFocus()
		if got := len(m.tasks.rows); got != tc.want {
			t.Errorf("%v lists %d rows, want %d (card and list disagree)", tc.kind, got, tc.want)
		}
	}
}
