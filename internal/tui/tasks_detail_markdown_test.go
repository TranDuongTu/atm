package tui

import (
	"errors"
	"strings"
	"testing"
)

// Markdown is rendered, not printed: the reader gets the heading, not the
// hashes that made it one.
func TestRenderMarkdownRendersTheMarkup(t *testing.T) {
	lines := renderMarkdown("## Approach\n\nSelected **A**: registry hooks only.\n\n- one\n- two\n", 60)
	// Rendered lines carry per-span styling; the assertions are about the
	// text, so they read it with the styling stripped.
	body := stripANSI(strings.Join(lines, "\n"))

	for _, want := range []string{"Approach", "registry hooks only", "• one", "• two"} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered markdown missing %q:\n%s", want, body)
		}
	}
	// Glamour's dark style keeps a "## " prefix on a heading as its visual
	// marker, so the discriminator between rendered and raw is the markup it
	// CONSUMES: emphasis, code spans, list bullets.
	if strings.Contains(body, "**") {
		t.Errorf("rendered markdown still carries raw emphasis markers:\n%s", body)
	}
	for i, l := range lines {
		if len(l) > 0 && strings.HasPrefix(l, "\n") {
			t.Errorf("line %d carries an embedded newline: %q", i, l)
		}
	}
}

// A renderer that cannot be built must not blank the pane: the reader still
// gets the text, unstyled.
func TestRenderMarkdownFallsBackToRawText(t *testing.T) {
	prev := newMarkdownRenderer
	newMarkdownRenderer = func(int) (markdownRenderer, error) { return nil, errors.New("no renderer") }
	t.Cleanup(func() { newMarkdownRenderer = prev })

	lines := renderMarkdown("## Approach\n\nplain fallback text", 60)
	body := strings.Join(lines, "\n")

	if !strings.Contains(body, "plain fallback text") {
		t.Errorf("the fallback must still carry the text:\n%s", body)
	}
	if !strings.Contains(body, "## Approach") {
		t.Errorf("the fallback is the RAW source, markers and all:\n%s", body)
	}
}

// A renderer that fails mid-render degrades the same way.
func TestRenderMarkdownFallsBackWhenRenderingFails(t *testing.T) {
	prev := newMarkdownRenderer
	newMarkdownRenderer = func(int) (markdownRenderer, error) { return failingRenderer{}, nil }
	t.Cleanup(func() { newMarkdownRenderer = prev })

	if got := strings.Join(renderMarkdown("some text", 60), "\n"); !strings.Contains(got, "some text") {
		t.Errorf("a failed render must fall back to the source:\n%s", got)
	}
}

type failingRenderer struct{}

func (failingRenderer) Render(string) (string, error) { return "", errors.New("render failed") }

func TestCommentDrillRendersTheBodyAsMarkdown(t *testing.T) {
	m := seedDetailModel(t)
	m.SetSize(140, 44)
	tk := seedTask(t, m, "ATM", "markdown comment", "ATM:scrum:task")
	c := seedComment(t, m, tk.ID, "## Decision\n\nApproach **A** was selected.", "ATM:comment:decision")
	m.refreshAll()
	openDetailFor(t, m, tk.ID)
	m.tasks.pushDrill(drillLevel{kind: drillComment, id: c.ID, cursor: -1})

	view := stripANSI(m.tasks.renderDrillModal())

	if !strings.Contains(view, "Decision") || !strings.Contains(view, "was selected") {
		t.Errorf("the comment body must render:\n%s", view)
	}
	if strings.Contains(view, "**A**") {
		t.Errorf("the comment body must be RENDERED markdown, not raw:\n%s", view)
	}
}

func TestDescriptionDrillRendersTheDescriptionAsMarkdown(t *testing.T) {
	m := seedDetailModel(t)
	m.SetSize(140, 44)
	tk, err := m.store.CreateTask("ATM", "markdown description",
		"## Where it lives\n\n`internal/tui/tasks_detail.go` holds the page.", []string{"ATM:scrum:task"}, testActor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	m.refreshAll()
	openDetailFor(t, m, tk.ID)

	update(t, m, "v")
	view := stripANSI(m.tasks.renderDrillModal())

	if !strings.Contains(view, "Where it lives") {
		t.Errorf("the description must render:\n%s", view)
	}
	if strings.Contains(view, "`internal/tui/tasks_detail.go`") {
		t.Errorf("the description must be RENDERED markdown, not raw:\n%s", view)
	}
}

// H is the comment drill-in's own key: the audit rows appear on the first
// press and are gone again on the second.
func TestCommentDrillHistoryToggles(t *testing.T) {
	m := seedDetailModel(t)
	m.SetSize(140, 44)
	tk := seedTask(t, m, "ATM", "history comment", "ATM:scrum:task")
	c := seedComment(t, m, tk.ID, "body text", "ATM:comment:progress")
	m.refreshAll()
	openDetailFor(t, m, tk.ID)
	m.tasks.pushDrill(drillLevel{kind: drillComment, id: c.ID, cursor: -1})

	if strings.Contains(stripANSI(m.tasks.renderDrillModal()), "HISTORY") {
		t.Fatal("history must start closed")
	}
	update(t, m, "H")
	view := stripANSI(m.tasks.renderDrillModal())
	if !strings.Contains(view, "HISTORY") {
		t.Fatalf("H must open the history section:\n%s", view)
	}
	if !strings.Contains(view, "comment.created") {
		t.Errorf("the history section must carry the audit rows:\n%s", view)
	}
	update(t, m, "H")
	if strings.Contains(stripANSI(m.tasks.renderDrillModal()), "HISTORY") {
		t.Error("a second H must close the history section again")
	}
}

// A long comment pushes its history below the fold. H has to land the
// reader on the section it just opened, or the key looks dead.
func TestCommentDrillHistoryScrollsIntoView(t *testing.T) {
	m := seedDetailModel(t)
	m.SetSize(140, 30)
	tk := seedTask(t, m, "ATM", "long comment", "ATM:scrum:task")
	c := seedComment(t, m, tk.ID,
		strings.TrimSpace(strings.Repeat("a long paragraph of comment body that wraps and wraps again. ", 40)),
		"ATM:comment:progress")
	m.refreshAll()
	openDetailFor(t, m, tk.ID)
	m.tasks.pushDrill(drillLevel{kind: drillComment, id: c.ID, cursor: -1})

	update(t, m, "H")

	view := stripANSI(m.tasks.renderDrillModal())
	if !strings.Contains(view, "HISTORY") {
		t.Errorf("H must scroll the history section into view:\n%s", view)
	}
	if !strings.Contains(view, "comment.created") {
		t.Errorf("the audit rows must be on screen after H:\n%s", view)
	}
}

// H belongs to the comment level alone: nothing else grows a history section.
func TestHistoryToggleIsCommentOnly(t *testing.T) {
	m := seedDetailModel(t)
	m.SetSize(140, 44)
	tk, err := m.store.CreateTask("ATM", "no history here", "a description", []string{"ATM:scrum:task"}, testActor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	m.refreshAll()
	openDetailFor(t, m, tk.ID)

	update(t, m, "H")
	if strings.Contains(stripANSI(m.tasks.renderDrillModal()), "HISTORY") {
		t.Error("H on DETAILS must not open a history section")
	}
	update(t, m, "v")
	update(t, m, "H")
	if strings.Contains(stripANSI(m.tasks.renderDrillModal()), "HISTORY") {
		t.Error("H on the description must not open a history section")
	}
}

// Every drill-in tells the reader how to leave it, and the comment level
// advertises its own key.
func TestDrillInFootersAdvertiseTheirKeys(t *testing.T) {
	m := seedDetailModel(t)
	m.SetSize(140, 44)
	tk, err := m.store.CreateTask("ATM", "footers", "a description", []string{"ATM:scrum:task"}, testActor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	c := seedComment(t, m, tk.ID, "body text", "ATM:comment:progress")
	m.refreshAll()
	openDetailFor(t, m, tk.ID)

	update(t, m, "v")
	if got := stripANSI(m.tasks.renderDrillModal()); !strings.Contains(got, "esc back") {
		t.Errorf("the description drill-in must say how to leave:\n%s", got)
	}
	update(t, m, "esc")

	m.tasks.pushDrill(drillLevel{kind: drillComment, id: c.ID, cursor: -1})
	view := stripANSI(m.tasks.renderDrillModal())
	for _, want := range []string{"H history", "esc back"} {
		if !strings.Contains(view, want) {
			t.Errorf("the comment drill-in footer is missing %q:\n%s", want, view)
		}
	}
}

// A resize re-renders at the new width instead of serving lines wrapped for
// the old one.
func TestDrillInRewrapsOnResize(t *testing.T) {
	m := seedDetailModel(t)
	m.SetSize(140, 44)
	tk, err := m.store.CreateTask("ATM", "rewrap",
		strings.TrimSpace(strings.Repeat("a long description sentence that has to wrap somewhere sensible ", 6)),
		[]string{"ATM:scrum:task"}, testActor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	m.refreshAll()
	openDetailFor(t, m, tk.ID)
	update(t, m, "v")

	wide := len(strings.Split(m.tasks.renderDrillModal(), "\n")[2])
	m.SetSize(80, 44)
	narrow := len(strings.Split(m.tasks.renderDrillModal(), "\n")[2])

	if narrow >= wide {
		t.Errorf("the drill-in must re-render narrower after a resize: %d columns then %d", wide, narrow)
	}
}
