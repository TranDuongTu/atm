package tui

import (
	"strings"
	"testing"

	"atm/internal/store"
)

func TestTaskDetailRendersCommentsSection(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "Agent Tasks Management", "claude")
	tk, _ := m.store.CreateTask("ATM", "Fix thing", "work on it", nil, "claude")
	_, _ = m.store.CreateComment(tk.ID, "first comment body", []string{"ATM:comment:open-question"}, "", "agent")
	_, _ = m.store.CreateComment(tk.ID, "second reply", nil, "ATM-0001-c0001", "ttran")

	m.projectScope = "ATM"
	m.SetSize(240, 70)
	m.tasks.openDetail(tk.ID)
	view := m.tasks.View()
	if !strings.Contains(view, "COMMENTS") {
		t.Fatalf("missing Comments section:\n%s", view)
	}
	if !strings.Contains(view, "agent") {
		t.Fatalf("missing first comment actor:\n%s", view)
	}
	if !strings.Contains(view, "ttran") {
		t.Fatalf("missing second comment actor:\n%s", view)
	}
	if !strings.Contains(view, "first comment body") {
		t.Fatalf("missing first comment body:\n%s", view)
	}
	if !strings.Contains(view, "second reply") {
		t.Fatalf("missing second comment body:\n%s", view)
	}
	hint := m.tasks.statusHint()
	if !strings.Contains(hint, "[M]comment") {
		t.Fatalf("missing [M] hint: %s", hint)
	}
	if !strings.Contains(hint, "[H]history") {
		t.Fatalf("missing [H] hint: %s", hint)
	}
}

func TestTaskDetailHidesHistoryInline(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	m.projectScope = "ATM"
	m.SetSize(120, 70)
	m.tasks.openDetail(tk.ID)
	view := m.tasks.View()
	// History lives behind the [H] overlay; the detail view must not
	// inline-render task.* event rows by default.
	hv := m.store.History(tk.ProjectCode, store.Subject{Kind: "task", ID: tk.ID})
	if len(hv) > 0 && strings.Contains(view, "task.created") {
		t.Fatalf("history must be hidden behind [H] overlay, but found task.created in detail:\n%s", view)
	}
}

func TestTaskDetailMKeyOpensCommentForm(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	if m.form != nil {
		t.Fatal("expected nil form before [M]")
	}
	m.tasks.handleDetailKey(keyMsg("M"))
	if m.form == nil || m.formKind != formCommentAdd {
		t.Fatalf("expected formCommentAdd, got form=%v kind=%v", m.form, m.formKind)
	}
}

func TestEnterOnCommentOpensReadOnlyOverlay(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	_, _ = m.store.CreateComment(tk.ID, "body", []string{"ATM:comment:open-question"}, "", "agent")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	m.tasks.handleDetailKey(keyMsg("enter"))
	if m.tasks.commentOverlay.id != "ATM-0001-c0001" {
		t.Fatalf("comment overlay not opened: %+v", m.tasks.commentOverlay)
	}
}

func TestEnterOnTaskWithNoCommentsIsNoOp(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	m.tasks.handleDetailKey(keyMsg("enter"))
	if m.tasks.commentOverlay.id != "" {
		t.Fatalf("Enter on task with no comments should not open overlay: %+v", m.tasks.commentOverlay)
	}
}

func TestCommentOverlayShowsIDAndBody(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	_, _ = m.store.CreateComment(tk.ID, "the body text", nil, "", "agent")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	m.tasks.handleDetailKey(keyMsg("enter"))
	view := m.tasks.commentOverlay.view(m)
	if !strings.Contains(view, "ATM-0001-c0001") || !strings.Contains(view, "the body text") {
		t.Fatalf("overlay view missing id/body:\n%s", view)
	}
}

func TestCommentOverlayIsReadOnly(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	_, _ = m.store.CreateComment(tk.ID, "orig", nil, "", "agent")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	m.tasks.handleDetailKey(keyMsg("enter"))

	// Edit/remove keys must NOT open forms or confirms; the overlay is a
	// read-only peek. Mutations are CLI-only (`atm task comment ...`).
	for _, key := range []string{"e", "b", "B", "R", "x"} {
		m.form = nil
		m.formKind = formNone
		m.confirm = confirmNone
		m.tasks.handleCommentOverlayKey(keyMsg(key))
		if m.form != nil || m.formKind != formNone {
			t.Fatalf("[%s] should be a no-op on read-only overlay, opened form kind=%v", key, m.formKind)
		}
		if m.confirm != confirmNone {
			t.Fatalf("[%s] should be a no-op on read-only overlay, opened confirm=%v", key, m.confirm)
		}
	}

	// Esc closes the overlay and returns to the detail view.
	m.tasks.handleCommentOverlayKey(keyMsg("esc"))
	if m.tasks.commentOverlay.id != "" {
		t.Fatalf("Esc should close the comment overlay: %+v", m.tasks.commentOverlay)
	}
	if m.tasks.view != tViewDetail {
		t.Fatalf("Esc from comment overlay should stay in detail view, got view=%v", m.tasks.view)
	}
}

func TestEscFromCommentOverlayDoesNotLeakIntoNextDetail(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk1, _ := m.store.CreateTask("ATM", "first task", "", nil, "claude")
	_, _ = m.store.CreateComment(tk1.ID, "comment on first", nil, "", "claude")
	tk2, _ := m.store.CreateTask("ATM", "second task", "", nil, "claude")
	m.projectScope = "ATM"
	m.SetSize(120, 70)

	// Open detail on tk1, open comment overlay, then go back to list.
	m.tasks.openDetail(tk1.ID)
	m.tasks.handleDetailKey(keyMsg("enter"))
	if m.tasks.commentOverlay.id == "" {
		t.Fatal("expected comment overlay open after Enter")
	}
	m.tasks.handleDetailKey(keyMsg("esc")) // close overlay -> back at detail
	if m.tasks.commentOverlay.id != "" {
		t.Fatal("Esc should have closed the comment overlay")
	}
	m.tasks.handleDetailKey(keyMsg("esc")) // back to list

	// Now open a DIFFERENT task with no comments. The detail view must
	// NOT show a stale comment overlay.
	m.tasks.openDetail(tk2.ID)
	view := m.tasks.View()
	if strings.Contains(view, "comment on first") {
		t.Fatalf("stale comment overlay leaked into next detail:\n%s", view)
	}
	if m.tasks.commentOverlay.id != "" {
		t.Fatalf("stale commentOverlay state: %+v", m.tasks.commentOverlay)
	}
}

func TestTaskDetailHKeyOpensHistoryOverlay(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	m.projectScope = "ATM"
	m.SetSize(120, 70)
	m.tasks.openDetail(tk.ID)
	if m.tasks.historyOverlay.active {
		t.Fatal("history overlay should not be active before [H]")
	}
	m.tasks.handleDetailKey(keyMsg("H"))
	if !m.tasks.historyOverlay.active {
		t.Fatal("expected history overlay active after [H]")
	}
	view := m.tasks.View()
	if !strings.Contains(view, "History") {
		t.Fatalf("history overlay view missing History heading:\n%s", view)
	}
	if !strings.Contains(view, "task.created") {
		t.Fatalf("history overlay view missing task.created row:\n%s", view)
	}
	// The detail-mode facts must NOT be visible while the history overlay
	// is open (the overlay replaces the detail view).
	if strings.Contains(view, "FACTS") {
		t.Fatalf("history overlay should hide task detail facts, but found them:\n%s", view)
	}

	// Esc closes the history overlay and returns to the detail view.
	m.tasks.handleDetailKey(keyMsg("esc"))
	if m.tasks.historyOverlay.active {
		t.Fatal("Esc should have closed the history overlay")
	}
	view = m.tasks.View()
	if !strings.Contains(view, "FACTS") {
		t.Fatalf("detail facts should reappear after closing history overlay:\n%s", view)
	}
}

func TestCommentOverlayHasNoTrailingHintLine(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	_, _ = m.store.CreateComment(tk.ID, "the body text", nil, "", "agent")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	m.tasks.handleDetailKey(keyMsg("enter"))
	view := m.tasks.commentOverlay.view(m)
	mustContain(t, view, "BODY")
	mustNotContain(t, view, "[Esc] back")
	mustNotContain(t, view, "[H] history")
}

func TestHistoryOverlayHasNoTrailingHintLine(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	m.projectScope = "ATM"
	m.SetSize(120, 70)
	m.tasks.openDetail(tk.ID)
	m.tasks.handleDetailKey(keyMsg("H"))
	view := m.tasks.historyOverlay.view(m)
	mustContain(t, view, "task.created")
	mustNotContain(t, view, "[Esc] back")
}

func TestStatusHintReflectsOverlayState(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	_, _ = m.store.CreateComment(tk.ID, "body", nil, "", "agent")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)

	base := m.tasks.statusHint()
	if !strings.Contains(base, "[e]title") {
		t.Fatalf("base detail hint = %q, want task-detail hint", base)
	}

	m.tasks.handleDetailKey(keyMsg("enter")) // open comment overlay
	if m.tasks.commentOverlay.id == "" {
		t.Fatal("expected comment overlay open")
	}
	if got := m.tasks.statusHint(); got != "[H]istory   [Esc]back" {
		t.Errorf("statusHint with comment overlay open = %q want [H]istory   [Esc]back", got)
	}
	m.tasks.handleCommentOverlayKey(keyMsg("esc"))

	m.tasks.handleDetailKey(keyMsg("H")) // open history overlay
	if !m.tasks.historyOverlay.active {
		t.Fatal("expected history overlay active")
	}
	if got := m.tasks.statusHint(); got != "[Esc]back" {
		t.Errorf("statusHint with history overlay open = %q want [Esc]back", got)
	}
	m.tasks.handleHistoryOverlayKey(keyMsg("esc"))

	if got := m.tasks.statusHint(); got != base {
		t.Errorf("statusHint after closing overlays = %q want %q", got, base)
	}
}
