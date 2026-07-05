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
	if !strings.Contains(view, "Comments") {
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
	if !strings.Contains(view, "[M] add comment") {
		t.Fatalf("missing [M] hint:\n%s", view)
	}
	if !strings.Contains(view, "[H] history") {
		t.Fatalf("missing [H] hint:\n%s", view)
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
	// The full History section (with event rows) must not inline-render by default.
	hv := m.store.History(tk.ProjectCode, store.Subject{Kind: "task", ID: tk.ID})
	if len(hv) > 0 && strings.Contains(view, "task.created") {
		t.Fatalf("history must be hidden behind [H], but found task.created in detail:\n%s", view)
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

func TestEnterOnCommentOpensDetailOverlay(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	_, _ = m.store.CreateComment(tk.ID, "body", []string{"ATM:comment:open-question"}, "", "agent")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	// Cursor at the comments section row.
	m.tasks.commentsCursor = 0
	m.tasks.handleDetailKey(keyMsg("enter"))
	if m.tasks.commentOverlay.id != "ATM-0001-c0001" {
		t.Fatalf("comment overlay not opened: %+v", m.tasks.commentOverlay)
	}
}

func TestCommentOverlayShowsIDAndBody(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	_, _ = m.store.CreateComment(tk.ID, "the body text", nil, "", "agent")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	m.tasks.commentsCursor = 0
	m.tasks.handleDetailKey(keyMsg("enter"))
	view := m.tasks.commentOverlay.view(m)
	if !strings.Contains(view, "ATM-0001-c0001") || !strings.Contains(view, "the body text") {
		t.Fatalf("overlay view missing id/body:\n%s", view)
	}
}

func TestCommentOverlayKeysEditRemove(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := m.store.CreateComment(tk.ID, "orig", nil, "", "agent")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	m.tasks.commentsCursor = 0
	m.tasks.handleDetailKey(keyMsg("enter"))

	// e -> body edit form
	m.tasks.handleCommentOverlayKey(keyMsg("e"))
	if m.form == nil || m.formKind != formCommentSetBody {
		t.Fatalf("[e] should open set-body form: form=%v kind=%v", m.form, m.formKind)
	}
	_ = c

	// x -> confirm remove
	m.tasks.handleCommentOverlayKey(keyMsg("x"))
	if m.confirm != confirmRemoveComment {
		t.Fatalf("[x] should open remove-confirm: %v", m.confirm)
	}
}
