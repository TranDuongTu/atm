package tui

import (
	"strings"
	"testing"
)

// openDetailModal seeds a scrum-scoped project, opens the given task's detail
// modal, and returns the modal's rendered lines with styling stripped. The
// capability registry must be resolved (setupLanesProject seeds the
// vocabulary) or Annotate has nothing to say and every status assertion below
// would pass vacuously.
func openDetailModal(t *testing.T, m *Model, id string) []string {
	t.Helper()
	if m.capability.current == "" {
		t.Fatalf("setup: no current capability; Annotate cannot be exercised")
	}
	m.tasks.openDetail(id)
	if m.tasks.detailID() != id {
		t.Fatalf("setup: detail did not open on %s", id)
	}
	return strings.Split(stripANSI(m.tasks.renderDrillModal()), "\n")
}

func detailLineWith(t *testing.T, lines []string, sub string) string {
	t.Helper()
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return l
		}
	}
	t.Fatalf("no modal line contains %q\n--- modal ---\n%s", sub, strings.Join(lines, "\n"))
	return ""
}

func seedDetailModel(t *testing.T) *Model {
	t.Helper()
	m := newLanesTestModel(t)
	m.SetSize(140, 40)
	setupLanesProject(t, m, true)
	return m
}

// The badge and the STATUS line are the SAME cell, read through
// registry.Annotate for the capability the pane currently shows — the detail
// page must not special-case scrum, and must not invent a second status
// vocabulary next to the list's ANNOTATE column.
func TestDetailStatusComesFromTheAnnotateCell(t *testing.T) {
	m := seedDetailModel(t)
	tk := seedTask(t, m, "ATM", "wire the drill stack", "ATM:scrum:task", "ATM:scrum-stage:implementing")
	m.refreshAll()

	lines := openDetailModal(t, m, tk.ID)

	if !strings.Contains(lines[0], "task · implementing") {
		t.Errorf("top border must carry the status badge, got:\n%s", lines[0])
	}
	if !strings.Contains(lines[0], "Task "+tk.ID) {
		t.Errorf("top border must still name the task, got:\n%s", lines[0])
	}
	status := detailLineWith(t, lines, "STATUS")
	if !strings.Contains(status, "task · implementing") {
		t.Errorf("STATUS line = %q, want the full cell text", status)
	}
}

// A task no capability has claimed has no cell. The page still has a STATUS
// row: an em dash says "nothing to say", where a missing row would read as a
// rendering bug.
func TestDetailStatusIsEmDashWhenTheCellIsNil(t *testing.T) {
	m := seedDetailModel(t)
	tk := seedTask(t, m, "ATM", "unclaimed work")
	m.refreshAll()

	lines := openDetailModal(t, m, tk.ID)

	status := detailLineWith(t, lines, "STATUS")
	if !strings.Contains(status, "—") {
		t.Errorf("STATUS line = %q, want an em dash for a nil cell", status)
	}
	if strings.Contains(lines[0], "·") {
		t.Errorf("top border must carry no badge for a nil cell, got:\n%s", lines[0])
	}
}

// The title is the page heading. It used to be duplicated as a FACTS row;
// the heading replaces it rather than joining it.
func TestDetailTitleIsTheHeadingNotAFactsRow(t *testing.T) {
	m := seedDetailModel(t)
	tk := seedTask(t, m, "ATM", "Improve the task details page", "ATM:scrum:task")
	m.refreshAll()

	lines := openDetailModal(t, m, tk.ID)
	body := strings.Join(lines, "\n")

	heading := detailLineWith(t, lines, "Improve the task details page")
	if strings.Contains(heading, "title") {
		t.Errorf("the title renders as a heading, not a facts row: %q", heading)
	}
	if strings.Count(body, "Improve the task details page") != 1 {
		t.Errorf("the title must appear exactly once:\n%s", body)
	}
}

// FACTS collapses to one compact line so the sections above it stay on
// screen. The six facts are still all there.
func TestDetailFactsAreOneCompactLine(t *testing.T) {
	m := seedDetailModel(t)
	tk := seedTask(t, m, "ATM", "compact facts", "ATM:scrum:task")
	m.refreshAll()

	lines := openDetailModal(t, m, tk.ID)

	facts := detailLineWith(t, lines, "id "+tk.ID)
	for _, want := range []string{"project ATM", "created", "updated", "by " + testActor} {
		if !strings.Contains(facts, want) {
			t.Errorf("FACTS line %q missing %q", facts, want)
		}
	}
}

// The page shows a readable head of the description and says so; it never
// grows to fit one. A description that fits advertises nothing.
func TestDetailDescriptionHintAppearsOnlyWhenTruncated(t *testing.T) {
	m := seedDetailModel(t)
	long, err := m.store.CreateTask("ATM", "long desc",
		strings.TrimSpace(strings.Repeat("a wrapped description line that keeps going\n", 30)),
		[]string{"ATM:scrum:task"}, testActor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	short, err := m.store.CreateTask("ATM", "short desc", "one short line", []string{"ATM:scrum:task"}, testActor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	m.refreshAll()

	longLines := openDetailModal(t, m, long.ID)
	if !strings.Contains(strings.Join(longLines, "\n"), "[v] view") {
		t.Errorf("a truncated description must advertise [v] view:\n%s", strings.Join(longLines, "\n"))
	}
	if got := countDescriptionLines(longLines); got != detailDescriptionLines {
		t.Errorf("truncated description rendered %d lines, want %d", got, detailDescriptionLines)
	}

	m.tasks.backToList()
	shortLines := openDetailModal(t, m, short.ID)
	if strings.Contains(strings.Join(shortLines, "\n"), "[v] view") {
		t.Errorf("a description that fits must not advertise [v] view:\n%s", strings.Join(shortLines, "\n"))
	}
}

// countDescriptionLines counts the rendered lines of the DESCRIPTION section:
// its label row plus every continuation row before the next section.
func countDescriptionLines(lines []string) int {
	n := 0
	for _, l := range lines {
		plain := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(l, "│"), "│"))
		if strings.HasPrefix(plain, "DESCRIPTION") {
			n = 1
			continue
		}
		if n == 0 {
			continue
		}
		if plain == "" || isDetailSectionLabel(plain) {
			break
		}
		n++
	}
	return n
}

func isDetailSectionLabel(plain string) bool {
	for _, label := range []string{"STATUS", "PART-OF", "COMMENTS", "FACTS", "LABELS"} {
		if strings.HasPrefix(plain, label) {
			return true
		}
	}
	return false
}

// v is the description's own drill level: it opens over DETAILS and esc pops
// back to it, never out to the list.
func TestDetailVOpensTheDescriptionDrill(t *testing.T) {
	m := seedDetailModel(t)
	tk, err := m.store.CreateTask("ATM", "readable", "the whole description", []string{"ATM:scrum:task"}, testActor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	m.refreshAll()
	m.focused = paneTasks
	m.tasks.openDetail(tk.ID)

	update(t, m, "v")
	level := m.tasks.currentDrill()
	if level == nil || level.kind != drillDescription {
		t.Fatalf("v must push a description level, stack = %v", m.tasks.drillStack)
	}
	if level.id != tk.ID {
		t.Errorf("description level id = %q, want the task %q", level.id, tk.ID)
	}
	mustContain(t, stripANSI(m.View()), "the whole description")

	update(t, m, "esc")
	if got := m.tasks.detailID(); got != tk.ID {
		t.Fatalf("esc from the description must land back on DETAILS, detailID = %q", got)
	}
	update(t, m, "esc")
	if len(m.tasks.drillStack) != 0 {
		t.Fatalf("a second esc must return to the list, stack = %v", m.tasks.drillStack)
	}
}

// The footer advertises only keys that work. Task 3 adds the comment cursor
// keys to it; until then it must not promise them.
func TestDetailFooterAdvertisesTheLiveKeys(t *testing.T) {
	m := seedDetailModel(t)
	tk := seedTask(t, m, "ATM", "footer", "ATM:scrum:task")
	m.refreshAll()

	lines := openDetailModal(t, m, tk.ID)
	footer := detailLineWith(t, lines, "esc back")
	for _, want := range []string{"e edit title", "d description", "b add label", "M comment", "v view"} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer %q missing %q", footer, want)
		}
	}
}
