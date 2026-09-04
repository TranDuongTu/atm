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
	view := m.tasks.renderDrillModal()
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
	view := m.tasks.renderDrillModal()
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
	m.tasks.handleDrillKey(keyMsg("M"))
	if m.form == nil || m.formKind != formCommentAdd {
		t.Fatalf("expected formCommentAdd, got form=%v kind=%v", m.form, m.formKind)
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
