package tui

import (
	"strings"
	"testing"

	"atm/internal/answer"
	"atm/internal/core"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TestTaskDetailUsesADimmedModal catches a regression where opening a task
// replaces the Tasks pane instead of layering the detail surface over the
// workspace.
func TestTaskDetailUsesADimmedModal(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 36)
	seedProject(t, m, "ATM", "Acme")
	task := seedTask(t, m, "ATM", "modal task")
	update(t, m, "s")
	m.tasks.setFocus(taskFocus{mode: focusOff}, "")
	update(t, m, "2")
	update(t, m, "enter")

	view := stripANSI(m.View())
	mustContain(t, view, "Task "+task.ID)
	mustContain(t, view, "░")
	mustNotContain(t, view, "[1] Projects")
	mustNotContain(t, view, "[2] Tasks")
}

func TestTaskDetailModalUsesNearFullScreenDimensions(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 36)
	seedProject(t, m, "ATM", "Acme")
	task := seedTask(t, m, "ATM", "sized modal")
	m.tasks.openDetail(task.ID)

	modal := m.tasks.renderDetailModal()
	lines := strings.Split(modal, "\n")
	if got, want := len(lines), m.tasks.contentHeight-2; got != want {
		t.Fatalf("modal height = %d want %d", got, want)
	}
	for i, line := range lines {
		if got, want := lipgloss.Width(line), m.width-6; got != want {
			t.Fatalf("modal line %d width = %d want %d", i, got, want)
		}
	}
}

// TestTaskDetailEscRestoresTheList catches a modal that remains in the
// overlay chain after its only drill level is popped.
func TestTaskDetailEscRestoresTheList(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 36)
	seedProject(t, m, "ATM", "Acme")
	task := seedTask(t, m, "ATM", "return to list")
	update(t, m, "s")
	m.tasks.setFocus(taskFocus{mode: focusOff}, "")
	update(t, m, "2")
	update(t, m, "enter")
	update(t, m, "esc")

	view := stripANSI(m.View())
	mustContain(t, view, "[1] Projects")
	mustContain(t, view, "[2] Tasks")
	mustNotContain(t, view, "Task "+task.ID)
	if strings.Contains(view, "░") {
		t.Fatalf("detail backdrop leaked after Esc:\n%s", view)
	}
}

// TestTaskDetailBlocksWorkspaceIdle catches a detail surface omitted from the
// art animation gate while it is visibly covering the workspace.
func TestTaskDetailBlocksWorkspaceIdle(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	seedTask(t, m, "ATM", "freeze art")
	update(t, m, "s")
	m.tasks.setFocus(taskFocus{mode: focusOff}, "")
	update(t, m, "2")
	update(t, m, "enter")

	if m.workspaceIdle() {
		t.Fatal("workspaceIdle must be false while a task detail modal is open")
	}
}

// The two spotlight entry points must push the SAME detail level the list's
// enter does — not a second surface with its own lifetime. Both assert the
// stack SHAPE (one level, kind detail, the chosen id), which is what the
// pane-level tests around them cannot: they read detailID(), and detailID()
// answers the same for a stack of one and a stack of five.
func TestSpotlightTaskActionPushesOneDetailLevel(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	target := seedTask(t, m, "ATM", "wire the indexer")
	seedTask(t, m, "ATM", "decoy task")

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\\")})
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")
	moveCursorToTask(t, m, target.ID)
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	moveCursorToLabel(t, m, "Add comment")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	mustBeSingleDetailLevel(t, m, target.ID)
}

func TestAskOpenSourcePushesOneDetailLevel(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	tk := seedTask(t, m, "ATM", "wire the indexer")
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: tk.ID, Kind: "task", Title: "wire the indexer"}}},
		answer.Done{},
	}})

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	p := m.spotlight.ask
	drainAskTicks(t, m, p)
	p.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	mustBeSingleDetailLevel(t, m, tk.ID)
}

func mustBeSingleDetailLevel(t *testing.T, m *Model, id string) {
	t.Helper()
	if got := len(m.tasks.drillStack); got != 1 {
		t.Fatalf("drill stack depth = %d, want exactly 1 (%v)", got, m.tasks.drillStack)
	}
	level := m.tasks.currentDrill()
	if level.kind != drillDetail || level.id != id {
		t.Fatalf("drill level = {kind:%v id:%q}, want {detail %s}", level.kind, level.id, id)
	}
}
