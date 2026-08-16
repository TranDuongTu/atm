package tui

import (
	"strings"
	"testing"

	"atm/internal/store"
)

func TestTaskDetailRendersCommentsSection(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "Agent Tasks Management", testActor)
	tk, _ := m.store.CreateTask("ATM", "Fix thing", "work on it", nil, testActor)
	c1, _ := m.store.CreateComment(tk.ID, "first comment body", []string{"ATM:comment:open-question"}, "", testActor)
	_, _ = m.store.CreateComment(tk.ID, "second reply", nil, c1.ID, "manager@cli:test")

	m.projectScope = "ATM"
	m.SetSize(240, 70)
	m.tasks.openDetail(tk.ID)
	view := m.tasks.View()
	if !strings.Contains(view, "COMMENTS") {
		t.Fatalf("missing Comments section:\n%s", view)
	}
	if !strings.Contains(view, "developer") {
		t.Fatalf("missing first comment actor:\n%s", view)
	}
	if !strings.Contains(view, "manager") {
		t.Fatalf("missing second comment actor:\n%s", view)
	}
	if !strings.Contains(view, "first comment body") {
		t.Fatalf("missing first comment body:\n%s", view)
	}
	if !strings.Contains(view, "second reply") {
		t.Fatalf("missing second comment body:\n%s", view)
	}
}

func TestTaskDetailHidesHistoryInline(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", testActor)
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, testActor)
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
	_, _ = m.store.CreateProject("ATM", "x", testActor)
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, testActor)
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
	_, _ = m.store.CreateProject("ATM", "x", testActor)
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, testActor)
	c, _ := m.store.CreateComment(tk.ID, "body", []string{"ATM:comment:open-question"}, "", testActor)
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	m.tasks.handleDetailKey(keyMsg("enter"))
	if m.tasks.commentOverlay.id != c.ID {
		t.Fatalf("comment overlay not opened: %+v", m.tasks.commentOverlay)
	}
}

func TestEnterOnTaskWithNoCommentsIsNoOp(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", testActor)
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, testActor)
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	m.tasks.handleDetailKey(keyMsg("enter"))
	if m.tasks.commentOverlay.id != "" {
		t.Fatalf("Enter on task with no comments should not open overlay: %+v", m.tasks.commentOverlay)
	}
}

func TestCommentOverlayShowsIDAndBody(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", testActor)
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, testActor)
	c, _ := m.store.CreateComment(tk.ID, "the body text", nil, "", testActor)
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	m.tasks.handleDetailKey(keyMsg("enter"))
	view := m.tasks.commentOverlay.view(m)
	if !strings.Contains(view, c.ID) || !strings.Contains(view, "the body text") {
		t.Fatalf("overlay view missing id/body:\n%s", view)
	}
}

func TestCommentOverlayIsReadOnly(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", testActor)
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, testActor)
	_, _ = m.store.CreateComment(tk.ID, "orig", nil, "", testActor)
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
	_, _ = m.store.CreateProject("ATM", "x", testActor)
	tk1, _ := m.store.CreateTask("ATM", "first task", "", nil, testActor)
	_, _ = m.store.CreateComment(tk1.ID, "comment on first", nil, "", testActor)
	tk2, _ := m.store.CreateTask("ATM", "second task", "", nil, testActor)
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

// TestTaskHistoryLines verifies taskHistoryLines, the pure line renderer
// extracted from the deleted task-detail history overlay: same header,
// same "[seq] time actor action" rows — Task 9's spotlight task preview
// renders these lines directly, so the format must stay exactly this shape.
func TestTaskHistoryLines(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", testActor)
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, testActor)
	m.projectScope = "ATM"
	m.SetSize(120, 70)

	lines := taskHistoryLines(m, "ATM", tk.ID, 60)
	if len(lines) == 0 {
		t.Fatal("taskHistoryLines returned no lines")
	}
	if !strings.Contains(lines[0], "History") || !strings.Contains(lines[0], tk.ID) {
		t.Fatalf("header line missing History heading or task ID: %q", lines[0])
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "task.created") {
		t.Fatalf("history lines missing task.created row:\n%s", joined)
	}
}

func TestCommentOverlayHasNoTrailingHintLine(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", testActor)
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, testActor)
	_, _ = m.store.CreateComment(tk.ID, "the body text", nil, "", testActor)
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	m.tasks.handleDetailKey(keyMsg("enter"))
	view := m.tasks.commentOverlay.view(m)
	mustContain(t, view, "BODY")
	mustNotContain(t, view, "[Esc] back")
	mustNotContain(t, view, "[H] history")
}
