package tui

import (
	"fmt"
	"strings"
	"testing"

	"atm/internal/capability"
	"atm/internal/capability/contextmap"
	"atm/internal/capability/workflow"
	"atm/internal/core"
	"atm/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// --- Board ring / default selection tests ---

func TestSelectDefaultPicksAllTasksBoard(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	if m.boards.selected != workflow.BoardAllTasks("ATM") {
		t.Errorf("selected = %q, want ATM:all-tasks", m.boards.selected)
	}
}

// TestSelectDefaultOpenTasksRemainsSelectableInRing guards the demote-
// not-remove decision: all-tasks becomes the default, but open-tasks stays
// in the board ring as a normal selectable member. A single [ press from
// the all-tasks default must be able to reach it.
func TestSelectDefaultOpenTasksRemainsSelectableInRing(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	if m.boards.selected != workflow.BoardAllTasks("ATM") {
		t.Fatalf("precondition: selected = %q, want ATM:all-tasks", m.boards.selected)
	}
	// Cycle the ring until open-tasks is selected; it MUST be present.
	found := false
	for i := 0; i < len(m.boards.rows); i++ {
		m.boards.cycleBoard(1)
		if m.boards.selected == workflow.BoardOpenTasks("ATM") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("open-tasks not reachable by cycling from all-tasks; ring = %v", m.boards.rowNames())
	}
}

// TestSelectDefaultPicksAllTasksWithoutRegistryConsultation re-anchors the
// post-DefaultBoard policy: the UI picks <CODE>:all-tasks when present, else
// the first non-umbrella ring row, with NO registry consultation. To prove the
// pick is BY NAME (not "first row"), a second capability (contextmap) is
// registered BEFORE workflow, so its context-current board becomes rows[0]
// — yet the UI still picks all-tasks further down the ring. (The DefaultBoard
// registry method is gone entirely after Task 2; this test pins the
// name-based replacement.)
func TestSelectDefaultPicksAllTasksWithoutRegistryConsultation(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// contextmap registered FIRST so context-current becomes rows[0]; the
	// old DefaultBoard-consultation code would have picked rows[0]. The new
	// code picks ATM:all-tasks by name, wherever it sits in the ring.
	m, err := NewModel(NewModelOpts{Service: s, Actor: testActor, Registry: capability.NewRegistry(contextmap.New(), workflow.New())})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := contextmap.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure contextmap: %v", err)
	}
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure workflow: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	if len(m.boards.rows) < 2 || m.boards.rows[0].FullName != "ATM:context-current" {
		t.Fatalf("precondition: rows[0] = %v, want ATM:context-current (registered first)", m.boards.rowNames())
	}
	m.boards.selectDefault()
	if m.boards.selected != "ATM:all-tasks" {
		t.Errorf("selected = %q, want ATM:all-tasks (UI policy: all-tasks by name, not first row)", m.boards.selected)
	}
}

// TestSelectDefaultFallsBackToFirstWhenAllTasksAbsent re-anchors the fallback
// branch: with workflow disabled and all-tasks absent, the FIRST ring row is
// selected, with no registry consultation.
func TestSelectDefaultFallsBackToFirstWhenAllTasksAbsent(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// contextmap-only registry: context-current is the only board it seeds.
	// all-tasks is absent, so the fallback fires.
	m, err := NewModel(NewModelOpts{Service: s, Actor: testActor, Registry: capability.NewRegistry(contextmap.New())})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := contextmap.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure contextmap: %v", err)
	}
	seedTask(t, m, "ATM", "context pointer", "ATM:context:documentation")
	m.boards.refresh()
	m.boards.selectDefault()
	if len(m.boards.rows) == 0 {
		t.Fatal("expected at least one ring row (context-current)")
	}
	if m.boards.selected != m.boards.rows[0].FullName {
		t.Errorf("selected = %q, want first ring row %q (fallback, no registry)", m.boards.selected, m.boards.rows[0].FullName)
	}
}

func TestSelectDefaultFallsBackToFirstWhenOpenTasksAbsent(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	// Do NOT ensure open-tasks; it is absent.
	m.boards.refresh()
	m.boards.selectDefault()
	if m.boards.selected == "" && len(m.boards.rows) > 0 {
		t.Errorf("selected empty but ring has %d boards", len(m.boards.rows))
	}
	if len(m.boards.rows) > 0 && m.boards.selected != m.boards.rows[0].FullName {
		t.Errorf("selected = %q, want first ring board %q", m.boards.selected, m.boards.rows[0].FullName)
	}
}

func TestCycleBoardMovesRing(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	// Two distinct namespaces so the ring has more than one entry to cycle.
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	seedTask(t, m, "ATM", "high one", "ATM:priority:high")
	m.boards.refresh()
	m.boards.selectDefault()
	first := m.boards.selected
	m.boards.cycleBoard(1) // next
	if m.boards.selected == first {
		t.Error("cycleBoard(1) did not move selection")
	}
	m.boards.cycleBoard(-1) // back
	if m.boards.selected != first {
		t.Errorf("after cycle back, selected = %q, want %q", m.boards.selected, first)
	}
}

// --- Pinning tests ---

func TestTogglePinPersists(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	m.boards.togglePin()
	p, err := m.store.GetPins("ATM")
	if err != nil {
		t.Fatalf("get pins: %v", err)
	}
	if p == nil || len(p.Boards) != 1 || p.Boards[0] != m.boards.selected {
		t.Errorf("pins after toggle = %+v, want [%s]", p, m.boards.selected)
	}
	// Toggle again unpins.
	m.boards.togglePin()
	p, _ = m.store.GetPins("ATM")
	if p != nil && len(p.Boards) != 0 {
		t.Errorf("pins after second toggle = %v, want empty", p.Boards)
	}
}

func TestJumpPinSelectsNth(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	first := m.boards.selected
	// Pin a second board if one exists; else pin first twice is a no-op. For a
	// deterministic test, pin first and verify jumpPin(1) selects it.
	m.boards.togglePin()
	if !m.boards.jumpPin(1) {
		t.Fatal("jumpPin(1) returned false with 1 pin")
	}
	if m.boards.selected != first {
		t.Errorf("after jumpPin(1), selected = %q, want %q", m.boards.selected, first)
	}
}

// TestJumpPinResetsDrillState guards against a jump leaking a stale
// chart/detail/cursor from the previously-drilled board into the newly
// jumped-to board: drill into a namespace board's chart, then jumpPin to a
// DIFFERENT pinned board, and confirm the thumbnail is back at L0 rather
// than still showing the old namespace's chart under the new title.
func TestJumpPinResetsDrillState(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	seedTask(t, m, "ATM", "high one", "ATM:priority:high")
	m.boards.refresh()

	var statusFull, priorityFull string
	for _, r := range m.boards.rows {
		switch r.Name {
		case "status":
			statusFull = r.FullName
		case "priority":
			priorityFull = r.FullName
		}
	}
	if statusFull == "" || priorityFull == "" {
		t.Fatalf("expected status and priority namespace boards, got rows: %v", m.boards.rowNames())
	}

	// Pin the priority board (the jump target) while it is SELECTED, so the
	// jump lands on a board different from the one we are about to drill into.
	m.boards.selected = priorityFull
	m.boards.applyFocus()
	m.boards.togglePin()

	// Select and drill into the status namespace's chart.
	m.boards.selected = statusFull
	m.boards.applyFocus()
	m.boards.drillIn()
	if m.boards.level != lLevelChart {
		t.Fatalf("level = %v, want lLevelChart after drillIn on status", m.boards.level)
	}

	if !m.boards.jumpPin(1) {
		t.Fatal("jumpPin(1) returned false with 1 pin")
	}
	if m.boards.selected != priorityFull {
		t.Fatalf("selected = %q, want %q", m.boards.selected, priorityFull)
	}
	if m.boards.level != lLevelTable {
		t.Errorf("level = %v after jumpPin, want lLevelTable (stale chart leaked)", m.boards.level)
	}
	if m.boards.ns != "" {
		t.Errorf("ns = %q after jumpPin, want empty (stale namespace leaked)", m.boards.ns)
	}
	if m.boards.detail != (labelDetailState{}) {
		t.Errorf("detail = %+v after jumpPin, want zero value", m.boards.detail)
	}
	if m.boards.cursor != 0 {
		t.Errorf("cursor = %d after jumpPin, want 0", m.boards.cursor)
	}
}

// TestFocusCenterEntersNamespaceChartForImmediateNav verifies Shift-0
// (focusCenter) doesn't just restore the highlight — it ENTERS the center
// board so member navigation works right away. On a namespace board at L0 it
// drills into the chart (level == lLevelChart) so Shift-up/down (chartCursorMove)
// move among members with no intervening Shift-right.
func TestFocusCenterEntersNamespaceChartForImmediateNav(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	seedTask(t, m, "ATM", "done one", "ATM:status:done")
	m.boards.refresh()

	var statusFull string
	for _, r := range m.boards.rows {
		if r.Name == "status" {
			statusFull = r.FullName
		}
	}
	if statusFull == "" {
		t.Fatalf("expected a status namespace board, got rows: %v", m.boards.rowNames())
	}
	m.boards.selected = statusFull
	m.boards.applyFocus()
	if m.boards.level != lLevelTable {
		t.Fatalf("precondition: level = %v, want lLevelTable before focusCenter", m.boards.level)
	}

	m.boards.focusCenter() // Shift-0
	if m.boards.pinFocus != -1 {
		t.Errorf("pinFocus = %d after focusCenter, want -1", m.boards.pinFocus)
	}
	if m.boards.level != lLevelChart {
		t.Fatalf("level = %v after focusCenter on a namespace board, want lLevelChart (should enter the chart)", m.boards.level)
	}

	// Shift-down moves the chart cursor immediately — no Shift-right first.
	rows := m.boards.chartRows()
	if len(rows) < 2 {
		t.Fatalf("expected >= 2 chart rows for status, got %d", len(rows))
	}
	before := m.boards.cursor
	m.boards.chartCursorMove(1)
	if m.boards.cursor == before {
		t.Errorf("chartCursorMove(1) did not move the cursor (was %d) after focusCenter", before)
	}
}

// --- pinFocus (current-filter highlight location) tests ---

// TestPinFocusDefaultsToStrip verifies pinFocus starts at -1 (the strip's
// SELECTED board is the active filter) and that selectDefault, the entry
// point called on project select, leaves it there.
func TestPinFocusDefaultsToStrip(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	if m.boards.pinFocus != -1 {
		t.Errorf("pinFocus after selectDefault = %d, want -1", m.boards.pinFocus)
	}
}

// TestJumpPinSetsPinFocus verifies Shift-N moves the highlight to the jumped
// pin's index without touching how the filter itself is chosen.
func TestJumpPinSetsPinFocus(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	m.boards.togglePin()
	if !m.boards.jumpPin(1) {
		t.Fatal("jumpPin(1) returned false with 1 pin")
	}
	if m.boards.pinFocus != 0 {
		t.Errorf("pinFocus after jumpPin(1) = %d, want 0", m.boards.pinFocus)
	}
}

// TestCycleBoardReturnsPinFocusToStrip verifies "[" / "]" (cycleBoard) give
// the highlight back to the strip even after a Shift-N jump moved it onto a
// pin box.
func TestCycleBoardReturnsPinFocusToStrip(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	seedTask(t, m, "ATM", "high one", "ATM:priority:high")
	m.boards.refresh()
	m.boards.selectDefault()
	m.boards.togglePin()
	if !m.boards.jumpPin(1) {
		t.Fatal("jumpPin(1) returned false with 1 pin")
	}
	m.boards.cycleBoard(1)
	if m.boards.pinFocus != -1 {
		t.Errorf("pinFocus after cycleBoard = %d, want -1 (strip regains the highlight)", m.boards.pinFocus)
	}
}

// TestSelectDefaultReturnsPinFocusToStrip verifies the refresh()-triggered
// fallback path (a jumped-to pin's board vanishing mid-session) also resets
// the highlight to the strip, not just the direct project-select call.
func TestSelectDefaultReturnsPinFocusToStrip(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	m.boards.togglePin()
	if !m.boards.jumpPin(1) {
		t.Fatal("jumpPin(1) returned false with 1 pin")
	}
	m.boards.selectDefault()
	if m.boards.pinFocus != -1 {
		t.Errorf("pinFocus after selectDefault = %d, want -1", m.boards.pinFocus)
	}
}

// TestLoadPinsClampsToMaxPins verifies a pins.json written before the cap
// dropped to 3 (or edited by hand) is clamped on load rather than rendering
// or being jumpable past what the fixed slot holds.
func TestLoadPinsClampsToMaxPins(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// Persist a pin list longer than maxPins consisting of workflow's exposed
	// boards (custom boards are no longer ring rows, so they are no longer
	// pin candidates). loadPins must clamp to maxPins on load.
	boards := []string{
		"ATM:all-tasks", "ATM:open-tasks", "ATM:in-progress-tasks",
		"ATM:backlog", "ATM:status:*", "ATM:priority:*",
	}
	m.boards.refresh()
	if err := m.store.WritePins("ATM", &store.Pins{Actor: m.actor, Boards: boards}); err != nil {
		t.Fatalf("write pins: %v", err)
	}
	m.boards.refresh()
	if len(m.boards.pins) != maxPins {
		t.Fatalf("pins after loading %d stored = %d, want %d (clamped)", len(boards), len(m.boards.pins), maxPins)
	}
}

// --- pinFocus repair on b.pins shrinkage (highlight-exclusivity invariant) ---

// TestUnpinFocusedPinResetsFocusToStrip covers unpinning the very board that
// is currently focused (jumped to via Shift-N): b.selected drops out of
// b.pins entirely, so the strong highlight must fall back to the strip
// (pinFocus == -1) rather than staying at a now-out-of-range or
// wrongly-shifted index.
func TestUnpinFocusedPinResetsFocusToStrip(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	pinned := []string{"ATM:all-tasks", "ATM:open-tasks"}
	m.boards.refresh()
	for _, full := range pinned {
		m.boards.selected = full
		m.boards.togglePin()
	}
	if len(m.boards.pins) != 2 {
		t.Fatalf("pins = %v, want 2", m.boards.pins)
	}

	if !m.boards.jumpPin(1) {
		t.Fatal("jumpPin(1) returned false with 2 pins")
	}
	focused := m.boards.selected
	if m.boards.pinFocus != 0 {
		t.Fatalf("pinFocus after jumpPin(1) = %d, want 0", m.boards.pinFocus)
	}

	// Unpin the focused board itself.
	m.boards.togglePin()

	if m.boards.selected != focused {
		t.Errorf("selected = %q, want unchanged %q (unpin must not move the filter)", m.boards.selected, focused)
	}
	if m.boards.pinFocus != -1 {
		t.Errorf("pinFocus after unpinning the focused board = %d, want -1 (strip reclaims the highlight)", m.boards.pinFocus)
	}
}

// TestUnpinLowerIndexPinKeepsFocusOnSameBoard covers b.pins shrinking below
// the focused index while the focused board itself stays pinned and its
// label/board stays alive (unlike TestLoadPinsPruneKeepsFocusOnSameBoard,
// where the underlying board is deleted): another session drops a
// lower-index pin straight from the persisted pin list, and the next
// loadPins-driven refresh must still re-derive pinFocus so the highlight
// follows the focused BOARD, not the slot.
func TestUnpinLowerIndexPinKeepsFocusOnSameBoard(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	pinned := []string{"ATM:all-tasks", "ATM:open-tasks", "ATM:in-progress-tasks"}
	m.boards.refresh()
	for _, full := range pinned {
		m.boards.selected = full
		m.boards.togglePin()
	}
	if len(m.boards.pins) != 3 {
		t.Fatalf("pins = %v, want 3", m.boards.pins)
	}

	if !m.boards.jumpPin(3) { // jump to in-progress-tasks, the last pin
		t.Fatal("jumpPin(3) returned false with 3 pins")
	}
	focused := m.boards.selected // "ATM:in-progress-tasks"
	if m.boards.pinFocus != 2 {
		t.Fatalf("pinFocus after jumpPin(3) = %d, want 2", m.boards.pinFocus)
	}

	// Another session unpins all-tasks (index 0, below the focused index)
	// straight through the store; all-tasks's label/board stays alive, only
	// the persisted pin list shrinks.
	if err := m.store.WritePins("ATM", &store.Pins{Actor: m.actor, Boards: []string{"ATM:open-tasks", "ATM:in-progress-tasks"}}); err != nil {
		t.Fatalf("write pins: %v", err)
	}
	m.boards.refresh()

	if len(m.boards.pins) != 2 {
		t.Fatalf("pins after external unpin = %v, want 2", m.boards.pins)
	}
	if m.boards.selected != focused {
		t.Fatalf("selected = %q, want unchanged %q", m.boards.selected, focused)
	}
	if m.boards.pinFocus < 0 || m.boards.pinFocus >= len(m.boards.pins) || m.boards.pins[m.boards.pinFocus] != focused {
		t.Errorf("pins=%v pinFocus=%d, want pinFocus to still point at %q", m.boards.pins, m.boards.pinFocus, focused)
	}
}

// TestLoadPinsPruneKeepsFocusOnSameBoard covers loadPins pruning a
// lower-index pinned board (its label removed from the store) while a
// higher-index pin stays focused: refresh must re-derive pinFocus so it
// still points at the focused board, not a shifted slot.
func TestLoadPinsPruneKeepsFocusOnSameBoard(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// Pin three workflow-exposed boards. Pruning is exercised by HIDING
	// all-tasks via the boards config: a hidden board leaves the ring (and
	// thus b.rows), so loadPins drops it as no-longer-live — the new
	// capability-authored ring cannot be pruned by removing a label (Exposed
	// is pure and survives label deletion), so hiding is the prune path.
	pinned := []string{"ATM:all-tasks", "ATM:open-tasks", "ATM:priority:*"}
	m.boards.refresh()
	for _, full := range pinned {
		m.boards.selected = full
		m.boards.togglePin()
	}
	if len(m.boards.pins) != 3 {
		t.Fatalf("pins = %v, want 3", m.boards.pins)
	}

	if !m.boards.jumpPin(3) { // jump to priority:*, the last pin
		t.Fatal("jumpPin(3) returned false with 3 pins")
	}
	focused := m.boards.selected // "ATM:priority:*"
	if m.boards.pinFocus != 2 {
		t.Fatalf("pinFocus after jumpPin(3) = %d, want 2", m.boards.pinFocus)
	}

	// Hide all-tasks so it leaves the ring -> loadPins prunes it on refresh.
	if err := m.store.SetProjectBoards("ATM", &core.BoardsConfig{
		Hidden: []string{"ATM:all-tasks"},
		Pins:   m.boards.pins, // preserve the pin list
	}, m.actor); err != nil {
		t.Fatalf("SetProjectBoards: %v", err)
	}
	m.boards.refresh()

	if len(m.boards.pins) != 2 {
		t.Fatalf("pins after prune = %v, want 2", m.boards.pins)
	}
	if m.boards.selected != focused {
		t.Fatalf("selected = %q, want unchanged %q (priority:* still exists)", m.boards.selected, focused)
	}
	if m.boards.pinFocus < 0 || m.boards.pinFocus >= len(m.boards.pins) || m.boards.pins[m.boards.pinFocus] != focused {
		t.Errorf("pins=%v pinFocus=%d, want pinFocus to still point at %q", m.boards.pins, m.boards.pinFocus, focused)
	}
}

// --- Boards pane tests ---

// newTestStore opens a fresh temp-dir store for direct store-API tests that
// do not need a full TUI Model. Auto-initialized.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return s
}

// newTestBoardsModel builds a *boardsModel scoped to code over the given store,
// for direct row assertions without driving the full key harness.
func newTestBoardsModel(t *testing.T, s *store.Store, code string) *boardsModel {
	t.Helper()
	mm, err := NewModel(NewModelOpts{Service: s, Actor: testActor, Registry: capability.NewRegistry(workflow.New())})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	mm.projectScope = code
	return &mm.boards
}

// rowNames returns the display Names of the Boards pane's flat row list.
func (b *boardsModel) rowNames() []string {
	out := make([]string, 0, len(b.rows))
	for _, r := range b.rows {
		out = append(out, r.Name)
	}
	return out
}

// row returns the first boardRow whose Name matches, or ok=false.
func (b *boardsModel) row(name string) (boardRow, bool) {
	for _, r := range b.rows {
		if r.Name == name {
			return r, true
		}
	}
	return boardRow{}, false
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestBoardsPaneListsComputedLabelsFlat(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if _, err := workflow.EnsureVocabulary(s, "ATM", testActor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// An ad-hoc board label: previously an emergent L0 row, now umbrella-only.
	_ = s.LabelAdd("ATM:next-sprint", "the sprint board", "status:open", testActor)

	m := newTestBoardsModel(t, s, "ATM")
	m.refresh()

	rows := m.rowNames()
	// Boards and namespaces sit in ONE flat list, indistinguishable by design.
	// The ring is now capability-authored: workflow exposes both boards
	// (all-tasks, open-tasks, ...) and namespaces (status, priority).
	if !contains(rows, "all-tasks") {
		t.Errorf("board missing from rows: %v", rows)
	}
	if !contains(rows, "status") {
		t.Errorf("namespace missing from rows: %v", rows)
	}
	// Ad-hoc boards no longer surface at L0 — they live under the umbrella.
	if contains(rows, "next-sprint") {
		t.Errorf("ad-hoc board must not appear at L0 (umbrella-only): %v", rows)
	}
	// A board is not a namespace, so it must not appear as one.
	if contains(rows, "all-tasks:*") {
		t.Errorf("a board must not render as a namespace: %v", rows)
	}
}

// TestBoardsPaneBoardCountSumsMatchingTasks guards the boardCount fix: a
// board's FullName is never a wildcard, so GroupTasksErr's no-wildcard branch
// returns the matching tasks as the second return value. The board's Count
// must equal the number of tasks matching its expression, not 0.
func TestBoardsPaneBoardCountSumsMatchingTasks(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if _, err := workflow.EnsureVocabulary(s, "ATM", testActor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	mk := func(title string, labels ...string) {
		if _, err := s.CreateTask("ATM", title, "", labels, testActor); err != nil {
			t.Fatalf("CreateTask %q: %v", title, err)
		}
	}
	mk("open1", "ATM:status:open")
	mk("open2", "ATM:status:open")
	mk("done1", "ATM:status:done")

	b := newTestBoardsModel(t, s, "ATM")
	b.refresh()

	// open-tasks is workflow-exposed with Expr "status:open" -> 2 matches.
	row, ok := b.row("open-tasks")
	if !ok {
		t.Fatalf("open-tasks board missing from rows: %v", b.rowNames())
	}
	if row.Count != 2 {
		t.Errorf("open-tasks Count = %d want 2 (matching tasks)", row.Count)
	}
	if row.Broken {
		t.Errorf("open-tasks marked broken; expression status:open is valid")
	}
}

func TestBoardsPaneFlagsUndescribedRows(t *testing.T) {
	// Task 4 narrows the ⚠ flag: only capability-Exposed namespace rows can
	// be flagged at L0, and only when the Exposed literal carries no
	// description. workflow's seeded namespaces all carry descriptions, so
	// none are flagged — this test now pins that the seeded namespaces are
	// NOT flagged (the flag still fires in code for a future capability
	// that exposes an undescribed namespace).
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if _, err := workflow.EnsureVocabulary(s, "ATM", testActor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// An unmanaged namespace descriptor — appears under the umbrella, NOT
	// at L0 (Task 4). It must not surface as a flagged L0 row.
	_ = s.LabelAdd("ATM:sprint:*", "", "", testActor)

	m := newTestBoardsModel(t, s, "ATM")
	m.refresh()

	// sprint must NOT appear at L0 — it is umbrella-only now.
	if _, ok := m.row("sprint"); ok {
		t.Fatalf("unmanaged sprint namespace must not surface at L0: %v", m.rowNames())
	}
	// Seeded workflow namespaces carry descriptions and must not be flagged.
	for _, ns := range []string{"status", "priority"} {
		row, ok := m.row(ns)
		if !ok {
			t.Fatalf("%s namespace missing from rows: %v", ns, m.rowNames())
		}
		if row.NeedsDescription {
			t.Errorf("%s namespace has a description and must not be flagged", ns)
		}
	}
}

// --- Boards pane tests (ported from the Labels pane) ---

// The Boards pane is no longer reachable via focus (Task 3 removed the [3]
// pane; Task 7 re-wires boards actions into the Tasks pane). These tests
// drive boardsModel directly via m.boards.handleKey instead of routing keys
// through the Model's pane-focus dispatch.

func TestBoardsTabEmptyStateNoProject(t *testing.T) {
	m := newTestModel(t)
	// boardsModel is no longer sized by Model.SetSize (it is not part of the
	// rendered workspace as of Task 3), so tests that render its View must
	// size it directly.
	m.boards.SetSize(80, 20)
	v := m.boards.View()
	mustContain(t, v, "no project selected")
	mustContain(t, v, "press [s] in the Projects pane")
}

func TestBoardsTabAddLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	m.boards.handleKey(keyMsg("a")) // add label form
	if m.form == nil {
		t.Fatalf("add-label form not open")
	}
	for _, r := range "patch:urgent" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	if m.form != nil {
		t.Fatalf("form should be closed after submit")
	}
	if _, err := m.store.LabelShow("ATM:patch:urgent"); err != nil {
		t.Errorf("ATM:patch:urgent not in registry after add: %v", err)
	}
}

func TestBoardsTabSeedKey(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// Remove a workflow-owned board, then re-ensure via [S] and confirm it returns.
	_, _ = m.store.LabelRemove("ATM:open-tasks", testActor)
	m.refreshAll()
	update(t, m, "s")
	m.boards.handleKey(keyMsg("S"))
	if !strings.Contains(m.toastMsg, "ensured capability vocabulary in ATM") {
		t.Fatalf("toast = %q, want ensured capability vocabulary in ATM", m.toastMsg)
	}
	if _, err := m.store.LabelShow("ATM:open-tasks"); err != nil {
		t.Errorf("ATM:open-tasks not restored after [S]: %v", err)
	}
}

// TestBoardsL0FlatCounts replaces TestLabelsL0NamespaceTableCounts. The L0
// view is now a flat list of computed labels (boards + namespaces). The old
// synthetic "tags" and "(none)" rows are gone — bare tags are not computed
// labels and do not appear; tasks with no labels are not a board. Namespace
// rows still carry a distinct-task count.
func TestBoardsL0FlatCounts(t *testing.T) {
	m := newTestModel(t)
	m.boards.SetSize(80, 20)
	seedProject(t, m, "ATM", "Acme")
	mk := func(title string, labels ...string) {
		if _, err := m.store.CreateTask("ATM", title, "", labels, m.actor); err != nil {
			t.Fatal(err)
		}
	}
	mk("a", "ATM:status:open")
	mk("b", "ATM:status:done", "ATM:priority:high")
	mk("c", "ATM:priority:high")
	update(t, m, "s")

	byName := map[string]boardRow{}
	for _, r := range m.boards.rows {
		byName[r.Name] = r
	}
	if r, ok := byName["status"]; !ok || !r.Expandable {
		t.Fatalf("status namespace row missing or not expandable: %+v", byName["status"])
	} else if got := r.Count; got != 2 {
		t.Errorf("status count = %d want 2", got)
	}
	if r, ok := byName["priority"]; !ok || !r.Expandable {
		t.Fatalf("priority namespace row missing or not expandable: %+v", byName["priority"])
	} else if got := r.Count; got != 2 {
		t.Errorf("priority count = %d want 2", got)
	}
	// Bare tags and (none) are NOT boards; they must not appear in the flat list.
	if _, ok := byName["tags"]; ok {
		t.Errorf("bare-tags row must not appear in the flat boards list")
	}
	if _, ok := byName["(none)"]; ok {
		t.Errorf("(none) row must not appear in the flat boards list")
	}
	v := m.boards.View()
	mustContain(t, v, "BOARD")
	mustContain(t, v, "status")
}

func TestBoardsL0FlatListUsesFullWidth(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.boards.SetSize(72, 10)
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")

	lines := strings.Split(m.boards.View(), "\n")
	if len(lines) < 2 {
		t.Fatalf("table rendered too few lines:\n%s", m.boards.View())
	}
	if got := lipgloss.Width(lines[0]); got != m.boards.width {
		t.Fatalf("header width = %d want %d: %q", got, m.boards.width, lines[0])
	}
	if got := lipgloss.Width(lines[1]); got != m.boards.width {
		t.Fatalf("row width = %d want %d: %q", got, m.boards.width, lines[1])
	}
}

// TestBoardsL0CountColumnAlignsDespiteMarkers guards the boardTableLine
// display-width fix: rows carrying a multi-byte glyph must not push the COUNT
// column out of alignment. Every row — plain or marked — must render at the
// pane's display width, so the count column's right edge lines up. The
// umbrella row's owner is the multi-byte em-dash "—"; an unmanaged label
// makes the umbrella appear so the marked row is actually rendered.
func TestBoardsL0CountColumnAlignsDespiteMarkers(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.boards.SetSize(72, 12)
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	// An ad-hoc label no capability owns -> umbrella appears with owner "—".
	if err := m.store.LabelAdd("ATM:type:bug", "", "", m.actor); err != nil {
		t.Fatal(err)
	}
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	update(t, m, "s")

	lines := strings.Split(m.boards.View(), "\n")
	if len(lines) < 2 {
		t.Fatalf("table rendered too few lines:\n%s", m.boards.View())
	}
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if got := lipgloss.Width(ln); got != m.boards.width {
			t.Errorf("line %d display width = %d want %d (count column drifted on a marked row): %q", i, got, m.boards.width, ln)
		}
	}
	// Confirm the umbrella row (owner "—") was actually rendered, so the
	// test is not vacuously passing on plain rows only.
	if !strings.Contains(m.boards.View(), "unmanaged") {
		t.Fatalf("no umbrella row rendered — test is vacuous:\n%s", m.boards.View())
	}
}

// TestBoardsL0ShowsDescriptionColumn guards that the row's description is
// actually rendered in the table (the pane loads Description but must
// display it, not just clear the ⚠ flag when one is added).
func TestBoardsL0ShowsDescriptionColumn(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.boards.SetSize(120, 12) // wide enough that the seeded description is not truncated
	// status is seeded with a description; sprint is emergent and
	// undescribed (renders ⚠ and an empty description cell).
	seedTask(t, m, "ATM", "in-sprint", "ATM:sprint:next", "ATM:status:open")
	update(t, m, "s")

	view := m.boards.View()
	// The header has a DESCRIPTION column.
	mustContain(t, view, "DESCRIPTION")
	// The status namespace's seeded description is present.
	l, err := m.store.LabelShow("ATM:status:*")
	if err != nil {
		t.Fatalf("LabelShow ATM:status:*: %v", err)
	}
	if l.Description == "" {
		t.Fatal("seeded status:* descriptor has no description")
	}
	mustContain(t, view, l.Description)
}

func TestBoardsL0EnterDrillsIntoNamespaceAndFocusesTasks(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter"))
	if m.boards.level != lLevelChart {
		t.Fatalf("level = %v want chart", m.boards.level)
	}
	if m.tasks.filter != "ATM:status:*" {
		t.Fatalf("filter = %q want ATM:status:*", m.tasks.filter)
	}
	if m.tasks.focus.mode != focusPresent || m.tasks.focus.ns != "status" {
		t.Fatalf("focus = %+v want present/status", m.tasks.focus)
	}
	m.boards.handleKey(keyMsg("esc"))
	if m.boards.level != lLevelTable {
		t.Fatalf("level = %v want table after esc", m.boards.level)
	}
	if m.tasks.filter != "" || m.tasks.focus.mode != focusOff {
		t.Fatalf("focus/filter not cleared after esc: %q %+v", m.tasks.filter, m.tasks.focus)
	}
}

// TestBoardsL0EnterBoardFiltersTasksByLabel replaces the old L0 enter test
// for namespace drill-down: a board row (a computed label) selects straight
// to tasks via QueryFilters{Labels: [FullName]} — no chart level.
func TestBoardsL0EnterBoardFiltersTasksByLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	seedTask(t, m, "ATM", "open1", "ATM:status:open")
	seedTask(t, m, "ATM", "done1", "ATM:status:done")
	update(t, m, "s")
	// open-tasks is a workflow-exposed board (Expr "status:open").
	cursorToBoardRow(t, m, "open-tasks")
	m.boards.handleKey(keyMsg("enter"))
	if m.boards.level != lLevelTable {
		t.Fatalf("board selection must not leave L0: level = %v", m.boards.level)
	}
	if m.tasks.filter != "ATM:open-tasks" {
		t.Fatalf("tasks filter = %q want ATM:open-tasks", m.tasks.filter)
	}
	if m.tasks.focus.mode != focusOff {
		t.Fatalf("tasks focus = %+v want focusOff (board is an exact-label filter)", m.tasks.focus)
	}
	// The Tasks pane should show only the matching task.
	mustContain(t, m.tasks.View(), "open1")
	mustNotContain(t, m.tasks.View(), "done1")
}

// TestBoardsL0EditNamespaceOpensDescriptorEditor guards that [e] on a
// namespace row (which has no Expr) opens a description-only editor for its
// <ns>:* descriptor, and that saving upserts the descriptor so a human's
// new description overwrites the workflow-seeded one.
func TestBoardsL0EditNamespaceOpensDescriptorEditor(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	update(t, m, "s")

	// Cursor on the status namespace row (workflow-exposed, has a seeded
	// description — [e] still opens the descriptor editor).
	cursorToNamespaceRow(t, m, "status")

	// [e] on the namespace row opens the descriptor form, not the board editor.
	m.boards.handleKey(keyMsg("e"))
	if m.form == nil || m.formKind != formNamespaceDescribe {
		t.Fatalf("[e] on namespace must open formNamespaceDescribe; form=%v kind=%v", m.form, m.formKind)
	}
	// The form's read-only namespace field is pre-filled; the description
	// field is the second field, pre-filled with the seeded description.
	if got := m.form.Fields[0].Value; got != "status" {
		t.Errorf("namespace field = %q want status", got)
	}
	// Move to the description field and overwrite with a new description.
	update(t, m, "tab")
	// Replace the pre-filled seeded description with a new one.
	m.form.Fields[1].Value = ""
	for _, r := range "lifecycle state of a task" {
		update(t, m, string(r))
	}
	// Enter on the last field submits.
	update(t, m, "enter")
	if m.form != nil {
		t.Fatalf("form should be closed after submit")
	}

	// The descriptor was upserted: ATM:status:* now carries the typed text.
	l, err := m.store.LabelShow("ATM:status:*")
	if err != nil {
		t.Fatalf("LabelShow ATM:status:*: %v", err)
	}
	if l.Description != "lifecycle state of a task" {
		t.Errorf("descriptor description = %q want the typed text", l.Description)
	}
}

func TestBoardsEscFromChartRestoresTableCursor(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter"))
	m.boards.handleKey(keyMsg("esc"))

	if m.boards.level != lLevelTable {
		t.Fatalf("level = %v want table", m.boards.level)
	}
	if got := m.boards.rows[m.boards.cursor].Name; got != "status" {
		t.Fatalf("table cursor = %q want status", got)
	}
}

// TestBoardsL0HasNoNoneRow replaces TestLabelsL0EnterNoneFiltersUnlabeled.
// The flat boards list has no synthetic "(none)" row: a task with no labels
// is not a computed label. The focusUnlabeled mode still exists in the Tasks
// pane (driven elsewhere), but the Boards pane no longer surfaces it as a row.
func TestBoardsL0HasNoNoneRow(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "naked", "", nil, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	for _, r := range m.boards.rows {
		if r.Name == "(none)" {
			t.Fatalf("(none) row must not appear in flat boards list: %+v", r)
		}
	}
}

func TestBoardsDetailIsCompactAtDefaultAndSmallTerminals(t *testing.T) {
	for _, size := range []struct {
		name string
		w    int
		h    int
	}{
		{name: "default", w: 100, h: 30},
		{name: "small", w: 80, h: 24},
	} {
		t.Run(size.name, func(t *testing.T) {
			m := newTestModel(t)
			seedProject(t, m, "ATM", "Acme")
			if err := m.store.LabelAdd("ATM:status:open", "selected status description", "", m.actor); err != nil {
				t.Fatal(err)
			}
			seedTask(t, m, "ATM", "open", "ATM:status:open")
			m.SetSize(size.w, size.h)
			m.boards.SetSize(size.w, size.h)
			update(t, m, "s")
			cursorToNamespaceRow(t, m, "status")
			m.boards.handleKey(keyMsg("enter"))
			cursorToChartLabel(t, m, "ATM:status:open")
			m.boards.handleKey(keyMsg("enter"))

			view := m.boards.View()
			mustContain(t, view, "name        ATM:status:open")
			mustContain(t, view, "usage       1 use")
			mustContain(t, view, "description selected status description")
		})
	}
}

func cursorToNamespaceRow(t *testing.T, m *Model, ns string) {
	t.Helper()
	for i, r := range m.boards.rows {
		if r.Name == ns && r.Expandable {
			m.boards.cursor = i
			return
		}
	}
	t.Fatalf("namespace row %q not found in boards rows: %v", ns, m.boards.rowNames())
}

func cursorToBoardRow(t *testing.T, m *Model, name string) {
	t.Helper()
	for i, r := range m.boards.rows {
		if r.Name == name {
			m.boards.cursor = i
			return
		}
	}
	t.Fatalf("board row %q not found in boards rows: %v", name, m.boards.rowNames())
}

func TestBoardsChartCursorAndUnsetRow(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(120, 80)
	m.boards.SetSize(120, 80)
	if err := m.store.LabelAdd("ATM:status:blocked", "", "", m.actor); err != nil {
		t.Fatal(err)
	}
	mk := func(title string, labels ...string) {
		if _, err := m.store.CreateTask("ATM", title, "", labels, m.actor); err != nil {
			t.Fatal(err)
		}
	}
	mk("a", "ATM:status:open")
	mk("b", "ATM:status:open")
	mk("c", "ATM:status:done")
	mk("d", "ATM:priority:high") // no status -> unset

	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter")) // into chart

	rows := m.boards.chartRows()
	// open(2), blocked(0), done(1), unset(1) in this fixture.
	var openCount, blockedCount, unsetCount int
	sawUnset := false
	sawBlocked := false
	for _, r := range rows {
		if r.unset {
			sawUnset = true
			unsetCount = r.count
		}
		if r.full == "ATM:status:open" {
			openCount = r.count
		}
		if r.full == "ATM:status:blocked" {
			sawBlocked = true
			blockedCount = r.count
		}
	}
	if openCount != 2 {
		t.Errorf("open count = %d want 2", openCount)
	}
	if !sawUnset || unsetCount != 1 {
		t.Errorf("unset row missing or wrong: saw=%v count=%d want 1", sawUnset, unsetCount)
	}
	if !sawBlocked || blockedCount != 0 {
		t.Errorf("blocked row missing or wrong: saw=%v count=%d want 0", sawBlocked, blockedCount)
	}
	v := m.boards.View()
	mustContain(t, v, "(unset)")
	mustContain(t, v, "█")
}

func TestBoardsChartHighlightsOnlyName(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := newTestModel(t)
	m.boards.SetSize(80, 20)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter"))
	cursorToChartLabel(t, m, "ATM:status:open")

	line := ""
	for _, candidate := range strings.Split(m.boards.View(), "\n") {
		if strings.Contains(candidate, "ATM:status:open") {
			line = candidate
			break
		}
	}
	if line == "" {
		t.Fatalf("status:open chart row not found:\n%s", m.boards.View())
	}
	barAt := strings.Index(line, "█")
	resetAt := strings.Index(line, "\x1b[0m")
	if barAt < 0 {
		t.Fatalf("chart row has no bar:\n%q", line)
	}
	if resetAt < 0 {
		t.Fatalf("chart row has no cursor reset:\n%q", line)
	}
	if resetAt > barAt {
		t.Fatalf("chart cursor styling reaches the bar; reset=%d bar=%d line=%q", resetAt, barAt, line)
	}
}

func TestBoardsChartCursorCanStayOnUnset(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.CreateTask("ATM", "b", "", []string{"ATM:priority:high"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter"))

	unset := -1
	for i, r := range m.boards.chartRows() {
		if r.unset {
			unset = i
			break
		}
	}
	if unset < 0 {
		t.Fatalf("unset row not found")
	}
	for m.boards.cursor < unset {
		m.boards.handleKey(keyMsg("j"))
	}
	if !m.boards.chartRows()[m.boards.cursor].unset {
		t.Fatalf("cursor = %d want unset row %d before render", m.boards.cursor, unset)
	}
	_ = m.boards.View()
	if !m.boards.chartRows()[m.boards.cursor].unset {
		t.Fatalf("cursor moved after render: got %d want unset row %d", m.boards.cursor, unset)
	}
	if err := m.store.LabelAdd("ATM:status:later", "", "", m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	if !m.boards.chartRows()[m.boards.cursor].unset {
		t.Fatalf("cursor moved after refresh: got %d want unset row %d", m.boards.cursor, unset)
	}
}

func TestBoardsChartHeadlineCountsDistinctPresentTasks(t *testing.T) {
	m := newTestModel(t)
	m.boards.SetSize(80, 20)
	seedProject(t, m, "ATM", "Acme")
	seedTask(t, m, "ATM", "both", "ATM:status:open", "ATM:status:done")
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	seedTask(t, m, "ATM", "unset", "ATM:priority:high")
	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter"))

	mustContain(t, m.boards.View(), "status  ·  2 tasks")
}

func TestBoardsChartEnterRowOpensDetailAndFocusesExactLabel(t *testing.T) {
	m := newTestModel(t)
	m.boards.SetSize(80, 20)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter")) // chart
	cursorToChartLabel(t, m, "ATM:status:open")
	m.boards.handleKey(keyMsg("enter")) // detail

	if m.boards.level != lLevelDetail {
		t.Fatalf("level = %v want detail", m.boards.level)
	}
	if m.tasks.filter != "ATM:status:open" || m.tasks.focus.mode != focusOff {
		t.Fatalf("tasks focus/filter = %+v %q want off/exact", m.tasks.focus, m.tasks.filter)
	}
	mustContain(t, m.boards.View(), "name        ATM:status:open")

	// Esc returns to the chart and re-applies present focus.
	m.boards.handleKey(keyMsg("esc"))
	if m.boards.level != lLevelChart {
		t.Fatalf("level = %v want chart after esc", m.boards.level)
	}
	if m.tasks.filter != "ATM:status:*" || m.tasks.focus.mode != focusPresent {
		t.Fatalf("chart focus not restored: %+v %q", m.tasks.focus, m.tasks.filter)
	}
}

func TestBoardsChartEnterUnsetFiltersAbsent(t *testing.T) {
	m := newTestModel(t)
	m.boards.SetSize(80, 20)
	seedProject(t, m, "ATM", "Acme")
	mk := func(title string, labels ...string) {
		if _, err := m.store.CreateTask("ATM", title, "", labels, m.actor); err != nil {
			t.Fatal(err)
		}
	}
	mk("a", "ATM:status:open")
	mk("b", "ATM:priority:high") // no status

	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter")) // chart
	cursorToChartUnset(t, m)
	m.boards.handleKey(keyMsg("enter")) // unset leaf

	if m.tasks.focus.mode != focusAbsent || m.tasks.focus.ns != "status" {
		t.Fatalf("focus = %+v want absent/status", m.tasks.focus)
	}
	mustContain(t, m.boards.View(), "1 task with no status")
	m.boards.handleKey(keyMsg("esc"))
	if m.boards.level != lLevelChart || m.tasks.focus.mode != focusPresent {
		t.Fatalf("esc from unset leaf did not restore chart present focus: %v %+v", m.boards.level, m.tasks.focus)
	}
}

func TestBoardsChartRemovePrefillsCursorLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter"))
	cursorToChartLabel(t, m, "ATM:status:open")
	m.boards.handleKey(keyMsg("l"))

	if m.form == nil || m.formKind != formLabelRemove {
		t.Fatalf("remove form not open: form=%v kind=%v", m.form, m.formKind)
	}
	if got := m.form.Fields[0].Value; got != "status:open" {
		t.Fatalf("remove form name = %q want status:open", got)
	}
}

func TestBoardsDetailRemovePrefillsDisplayedLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter"))
	cursorToChartLabel(t, m, "ATM:status:open")
	m.boards.handleKey(keyMsg("enter"))
	m.boards.handleKey(keyMsg("l"))

	if m.form == nil || m.formKind != formLabelRemove {
		t.Fatalf("remove form not open: form=%v kind=%v", m.form, m.formKind)
	}
	if got := m.form.Fields[0].Value; got != "status:open" {
		t.Fatalf("remove form name = %q want status:open", got)
	}
}

func TestBoardsSyntheticUnsetRemoveIsNoOp(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:priority:high"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter"))
	cursorToChartUnset(t, m)
	m.boards.handleKey(keyMsg("l"))
	if m.form != nil {
		t.Fatalf("remove form opened for chart (unset) row")
	}
	m.boards.handleKey(keyMsg("enter"))
	m.boards.handleKey(keyMsg("l"))
	if m.form != nil {
		t.Fatalf("remove form opened for unset detail leaf")
	}
}

func cursorToChartLabel(t *testing.T, m *Model, full string) {
	t.Helper()
	for i, r := range m.boards.chartRows() {
		if r.full == full {
			m.boards.cursor = i
			return
		}
	}
	t.Fatalf("chart label %q not found", full)
}

func cursorToChartUnset(t *testing.T, m *Model) {
	t.Helper()
	for i, r := range m.boards.chartRows() {
		if r.unset {
			m.boards.cursor = i
			return
		}
	}
	t.Fatalf("chart (unset) row not found")
}

// --- Thumbnail drill-down tests (Task 8) ---

func TestDrillIntoNamespaceChart(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	// Select the status:* namespace board.
	for i, r := range m.boards.rows {
		if r.Expandable && r.Name == "status" {
			m.boards.selected = r.FullName
			_ = i
			break
		}
	}
	m.boards.applyFocus()
	m.boards.drillIn()
	if m.boards.level != lLevelChart {
		t.Errorf("level = %v, want lLevelChart after drillIn on namespace", m.boards.level)
	}
	m.boards.drillOut()
	if m.boards.level != lLevelTable {
		t.Errorf("level = %v, want lLevelTable after drillOut", m.boards.level)
	}
}

func TestChartCursorMoveTargetsMember(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	seedTask(t, m, "ATM", "done one", "ATM:status:done")
	m.boards.refresh()
	for _, r := range m.boards.rows {
		if r.Expandable && r.Name == "status" {
			m.boards.selected = r.FullName
			break
		}
	}
	m.boards.applyFocus()
	m.boards.drillIn() // -> chart
	if m.boards.chartCursorMove(0); m.boards.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 at chart entry", m.boards.cursor)
	}
	m.boards.chartCursorMove(1)
	if m.boards.cursor != 1 {
		t.Errorf("cursor = %d after move, want 1", m.boards.cursor)
	}
	m.boards.chartCursorMove(-1)
	if m.boards.cursor != 0 {
		t.Errorf("cursor = %d after move back, want 0", m.boards.cursor)
	}
}

func TestDrillOutOfLeafBoardKeepsBoardFocus(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault() // SELECTED = ATM:all-tasks (leaf board)
	m.boards.drillIn()       // leaf board -> its detail
	if m.boards.level != lLevelDetail {
		t.Fatalf("level = %v, want lLevelDetail", m.boards.level)
	}
	m.boards.drillOut() // back to L0 — board focus must be re-applied, not cleared
	if m.boards.level != lLevelTable {
		t.Fatalf("level = %v, want lLevelTable", m.boards.level)
	}
	// The task list must still be filtered by the SELECTED board (assert via
	// the tasks pane's focus caption / filter — check the exact accessor in
	// tasks.go; the invariant is: NOT the unfiltered focusOff+"" state).
	if got := m.tasks.focusCaption(); !strings.Contains(got, "all-tasks") {
		t.Errorf("focus caption = %q, want it to reference all-tasks after drillOut", got)
	}
}

func TestFitLineResetsANSIWhenTruncatingSelectedRows(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := newTestModel(t)
	line := m.styles.RowCursor.Render(strings.Repeat("x", 80))

	got := fitLine(line, 20)

	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("truncated selected row does not reset ANSI styling: %q", got)
	}
}

// --- UX refinement follow-up tests (pin cap, description wrapping, hint removal) ---

// TestTogglePinCapsAtThree verifies a 4th pin is ignored rather than evicting
// an existing pin or growing past what jumpPin / the fixed pin slot can
// address (maxPins is 3 for the fixed-slot rework, reachable by Shift-1..3).
func TestTogglePinCapsAtThree(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	var boards []string
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("ATM:board-%02d", i)
		if err := m.store.LabelAdd(name, "", "status:open", m.actor); err != nil {
			t.Fatal(err)
		}
		boards = append(boards, name)
	}
	m.boards.refresh()
	for _, full := range boards {
		m.boards.selected = full
		m.boards.togglePin()
	}
	if len(m.boards.pins) != 3 {
		t.Fatalf("pins after 4 toggles = %d, want 3 (cap)", len(m.boards.pins))
	}
	if m.boards.pins[len(m.boards.pins)-1] == boards[3] {
		t.Errorf("4th board %q was pinned past the cap", boards[3])
	}
	p, err := m.store.GetPins("ATM")
	if err != nil {
		t.Fatalf("get pins: %v", err)
	}
	if p == nil || len(p.Boards) != 3 {
		t.Errorf("persisted pins = %+v, want 3", p)
	}
}

// TestRenderDetailWrapsLongDescription verifies renderDetail wraps a
// description that overflows the pane width into multiple lines rather than
// truncating it to one.
func TestRenderDetailWrapsLongDescription(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	long := "this description is intentionally long enough that it must wrap across more than one line at a narrow pane width"
	if err := m.store.LabelAdd("ATM:status:open", long, "", m.actor); err != nil {
		t.Fatal(err)
	}
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	m.SetSize(40, 30)
	m.boards.SetSize(40, 30)
	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter"))
	cursorToChartLabel(t, m, "ATM:status:open")
	m.boards.handleKey(keyMsg("enter"))

	view := m.boards.View()
	descLines := 0
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "description") || (descLines > 0 && strings.TrimSpace(line) != "") {
			descLines++
		} else if descLines > 0 {
			break
		}
	}
	if descLines < 2 {
		t.Errorf("description rendered in %d line(s), want wrapped across >= 2 at width 40:\n%s", descLines, view)
	}
	mustContain(t, view, "intentionally long")
	mustContain(t, view, "wrap across")
}

// TestRenderChartShowsNamespaceDescriptorDescription verifies renderChart
// surfaces the namespace descriptor label's (<scope>:<ns>:*) description
// above the member bars, when one is set.
func TestRenderChartShowsNamespaceDescriptorDescription(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if err := m.store.LabelAdd("ATM:status:*", "the lifecycle stage of a task", "", m.actor); err != nil {
		t.Fatal(err)
	}
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	m.SetSize(100, 30)
	m.boards.SetSize(100, 30)
	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter"))

	view := m.boards.View()
	mustContain(t, view, "the lifecycle stage of a task")
}

// TestRenderChartOmitsHintLine and TestRenderDetailOmitsBackToChartHint verify
// change 4: the leftover Labels-pane hints embedded in renderChart/renderDetail
// are gone (the [2] pane's own statusHint already covers navigation).
func TestRenderChartOmitsHintLine(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter"))

	view := m.boards.View()
	mustNotContain(t, view, "[Enter]inspect")
	mustNotContain(t, view, "[Esc]back")
}

func TestRenderDetailOmitsBackToChartHint(t *testing.T) {
	m := newTestModel(t)
	m.boards.SetSize(120, 80)
	seedProject(t, m, "ATM", "Acme")
	if err := m.store.LabelAdd("ATM:status:blocked", "", "", m.actor); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.CreateTask("ATM", "no-status", "", nil, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	cursorToNamespaceRow(t, m, "status")
	m.boards.handleKey(keyMsg("enter"))
	cursorToChartUnsetRow(t, m)
	m.boards.handleKey(keyMsg("enter"))

	view := m.boards.View()
	mustNotContain(t, view, "back to chart")
}

// cursorToChartUnsetRow moves the chart cursor to the "(unset)" synthetic row.
func cursorToChartUnsetRow(t *testing.T, m *Model) {
	t.Helper()
	rows := m.boards.chartRows()
	for i, r := range rows {
		if r.unset {
			m.boards.cursor = i
			return
		}
	}
	t.Fatalf("no (unset) chart row found")
}

// --- Task 4: capability-authored ring tests ---

// TestBuildBoardRowsIsCapabilityAuthored: the ring is exactly what enabled
// capabilities expose (registration order) plus the umbrella when unmanaged
// labels exist — emergent namespaces no longer surface at L0.
func TestBuildBoardRowsIsCapabilityAuthored(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	// An ad-hoc namespace member: previously an emergent L0 row, now umbrella-only.
	if err := m.store.LabelAdd("ATM:type:bug", "", "", m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	wantRing := []string{
		"ATM:all-tasks", "ATM:open-tasks", "ATM:in-progress-tasks", "ATM:backlog",
		"ATM:status:*", "ATM:priority:*", "ATM:unmanaged",
	}
	if len(m.boards.rows) != len(wantRing) {
		t.Fatalf("ring = %+v, want %v", m.boards.rows, wantRing)
	}
	for i, r := range m.boards.rows {
		if r.FullName != wantRing[i] {
			t.Errorf("rows[%d] = %s, want %s", i, r.FullName, wantRing[i])
		}
	}
	if own := m.boards.rows[0].Owner; own != "workflow" {
		t.Errorf("all-tasks owner = %q, want workflow", own)
	}
	last := m.boards.rows[len(m.boards.rows)-1]
	if !last.Umbrella || last.Owner != "" || !last.Expandable {
		t.Errorf("umbrella row = %+v, want Umbrella+Expandable, no owner", last)
	}
}

// TestUmbrellaOmittedWhenNoUnmanaged: fully-owned label set -> no umbrella row.
func TestUmbrellaOmittedWhenNoUnmanaged(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	for _, r := range m.boards.rows {
		if r.Umbrella {
			t.Fatalf("umbrella present with no unmanaged labels: %+v", m.boards.rows)
		}
	}
}

// TestUmbrellaSuppressedByRealCollision: a real ATM:unmanaged tag (or
// ATM:unmanaged:* namespace) shadows the sentinel; the real label renders as
// a normal unmanaged label under no umbrella (the sentinel loses).
func TestUmbrellaSuppressedByRealCollision(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.LabelAdd("ATM:unmanaged", "hand-made collision", "", m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	for _, r := range m.boards.rows {
		if r.Umbrella {
			t.Fatalf("sentinel must lose to a real ATM:unmanaged label; ring = %+v", m.boards.rows)
		}
	}
}

// TestHiddenAndOrderApply: hidden rows leave the ring entirely; order is a
// partial override with unmatched rows appended in registration order.
func TestHiddenAndOrderApply(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	err := m.store.SetProjectBoards("ATM", &core.BoardsConfig{
		Order:  []string{"ATM:status:*", "ATM:open-tasks", "ATM:nosuch"},
		Hidden: []string{"ATM:backlog"},
	}, m.actor)
	if err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	want := []string{
		"ATM:status:*", "ATM:open-tasks", // ordered prefix
		"ATM:all-tasks", "ATM:in-progress-tasks", "ATM:priority:*", // rest, registration order
	}
	if len(m.boards.rows) != len(want) {
		t.Fatalf("ring = %+v, want %v", m.boards.rows, want)
	}
	for i, r := range m.boards.rows {
		if r.FullName != want[i] {
			t.Errorf("rows[%d] = %s, want %s", i, r.FullName, want[i])
		}
		if r.FullName == "ATM:backlog" {
			t.Error("hidden board must not appear in the ring")
		}
	}
}

// TestStoredDescriptionWinsOverExposedLiteral: a human-curated label
// description beats the capability's baked-in text.
func TestStoredDescriptionWinsOverExposedLiteral(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.LabelAdd("ATM:all-tasks", "our everything view", "*", m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	for _, r := range m.boards.rows {
		if r.FullName == "ATM:all-tasks" && r.Description != "our everything view" {
			t.Errorf("description = %q, want the stored (curated) one", r.Description)
		}
	}
}

// TestSelectDefaultSkipsUmbrella: umbrella is never the default selection;
// selecting it via cycleBoard applies no task filter.
func TestSelectDefaultSkipsUmbrella(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.LabelAdd("ATM:urgent", "", "", m.actor); err != nil { // makes umbrella appear
		t.Fatal(err)
	}
	m.boards.refresh()
	m.boards.selectDefault()
	if m.boards.selected != "ATM:all-tasks" {
		t.Fatalf("selected = %q, want ATM:all-tasks", m.boards.selected)
	}
	// Cycle to the umbrella (last row) and confirm no filter is applied.
	for m.boards.selected != "ATM:unmanaged" {
		m.boards.cycleBoard(1)
	}
	if m.tasks.focus.mode != focusOff || m.tasks.filter != "" {
		t.Errorf("umbrella selection must clear the task filter (focus=%v filter=%q)", m.tasks.focus, m.tasks.filter)
	}
}
