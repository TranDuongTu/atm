package tui

import (
	"errors"
	"strings"
	"testing"

	"atm/internal/core"
	"atm/internal/store"
)

// failingCommentService is the real service with one hole punched in it:
// listing a task's comments fails. Embedding the interface keeps every other
// call live, so the page renders normally right up to the comments section.
type failingCommentService struct {
	core.Service
}

func (failingCommentService) ListComments(string) ([]*core.Comment, error) {
	return nil, errors.New("comment index unreadable")
}

func seedComment(t *testing.T, m *Model, taskID, body string, labels ...string) *store.Comment {
	t.Helper()
	c, err := m.store.CreateComment(taskID, body, labels, "", testActor)
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	return c
}

// PART-OF is the one topology edge the page shows, and it comes from the
// same pure Parenter hook the list's grouping uses — never from a scan.
func TestDetailPartOfNamesTheParent(t *testing.T) {
	m := seedDetailModel(t)
	parent := seedTask(t, m, "ATM", "Epic: scrum pipeline", "ATM:scrum:epic")
	child := seedTask(t, m, "ATM", "child task", "ATM:scrum:task")
	linkPartOf(t, m, child.ID, parent.ID)
	m.refreshAll()

	lines := openDetailModal(t, m, child.ID)

	row := detailLineWith(t, lines, "PART-OF")
	if !strings.Contains(row, parent.ID) || !strings.Contains(row, "Epic: scrum pipeline") {
		t.Errorf("PART-OF row = %q, want the parent id and title", row)
	}
}

// A parent someone deleted must still be reported: the edge is real and the
// dangling id is the only way to chase it.
func TestDetailPartOfShowsADanglingParent(t *testing.T) {
	m := seedDetailModel(t)
	child := seedTask(t, m, "ATM", "orphan", "ATM:scrum:task")
	linkPartOf(t, m, child.ID, "ATM-deadbe")
	m.refreshAll()

	lines := openDetailModal(t, m, child.ID)

	row := detailLineWith(t, lines, "PART-OF")
	if !strings.Contains(row, "ATM-deadbe") {
		t.Errorf("PART-OF row = %q, want the dangling id", row)
	}
}

func TestDetailPartOfIsAbsentWithoutAParent(t *testing.T) {
	m := seedDetailModel(t)
	tk := seedTask(t, m, "ATM", "top level", "ATM:scrum:task")
	m.refreshAll()

	lines := openDetailModal(t, m, tk.ID)

	if strings.Contains(strings.Join(lines, "\n"), "PART-OF") {
		t.Errorf("a task with no parent must not render a PART-OF row:\n%s", strings.Join(lines, "\n"))
	}
}

// The comments section is a digest, not a transcript: newest first, one line
// of body each, and a footer that counts what it is not showing.
func TestDetailCommentsAreLatestNCollapsedRows(t *testing.T) {
	m := seedDetailModel(t)
	tk := seedTask(t, m, "ATM", "chatty", "ATM:scrum:task")
	for _, body := range []string{"oldest one", "second one", "third one", "fourth one", "newest one\nsecond line of the newest"} {
		seedComment(t, m, tk.ID, body, "ATM:comment:progress")
	}
	m.refreshAll()

	lines := openDetailModal(t, m, tk.ID)
	body := strings.Join(lines, "\n")

	for _, want := range []string{"newest one", "fourth one", "third one", "ATM:comment:progress", testActor} {
		if !strings.Contains(body, want) {
			t.Errorf("collapsed rows missing %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"second one", "oldest one", "second line of the newest"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("the page must not render %q — only the latest %d, one body line each:\n%s",
				unwanted, detailCommentsShown, body)
		}
	}
	if !strings.Contains(body, "2 older comments") {
		t.Errorf("the older-count footer must name the 2 comments not shown:\n%s", body)
	}
	if !strings.Contains(body, "[C]") {
		t.Errorf("the older-count footer must point at the thread view:\n%s", body)
	}
	if i, j := strings.Index(body, "newest one"), strings.Index(body, "third one"); i > j {
		t.Errorf("comments must read newest first:\n%s", body)
	}
}

func TestDetailCommentWithoutAKindSaysSo(t *testing.T) {
	m := seedDetailModel(t)
	tk := seedTask(t, m, "ATM", "unlabelled", "ATM:scrum:task")
	seedComment(t, m, tk.ID, "no kind on this one")
	m.refreshAll()

	lines := openDetailModal(t, m, tk.ID)

	row := detailLineWith(t, lines, testActor)
	if !strings.Contains(row, "(no kind)") {
		t.Errorf("collapsed row = %q, want a (no kind) marker", row)
	}
}

// A listing that fails is a fact about the store the reader needs. The old
// page swallowed the error and rendered "(no comments)", which is a lie.
func TestDetailCommentsDegradeWhenListingFails(t *testing.T) {
	m := seedDetailModel(t)
	tk := seedTask(t, m, "ATM", "unreadable comments", "ATM:scrum:task")
	m.refreshAll()
	m.store = failingCommentService{m.store}

	lines := openDetailModal(t, m, tk.ID)
	body := strings.Join(lines, "\n")

	if !strings.Contains(body, "comment index unreadable") {
		t.Errorf("a failed listing must degrade to a row naming the error:\n%s", body)
	}
	if strings.Contains(body, "(no comments)") {
		t.Errorf("a failed listing must not read as an empty one:\n%s", body)
	}
}

// openDetailFor opens a task's detail with the pane focused, ready for keys.
func openDetailFor(t *testing.T, m *Model, id string) {
	t.Helper()
	m.focused = paneTasks
	m.tasks.openDetail(id)
	if m.tasks.detailID() != id {
		t.Fatalf("setup: detail did not open on %s", id)
	}
}

// j walks onto the comment rows and enter drills the one under the cursor.
func TestDetailCursorWalksCommentsAndDrillsIn(t *testing.T) {
	m := seedDetailModel(t)
	m.SetSize(140, 44)
	tk := seedTask(t, m, "ATM", "cursor task", "ATM:scrum:task")
	seedComment(t, m, tk.ID, "older body", "ATM:comment:progress")
	newest := seedComment(t, m, tk.ID, "newest body", "ATM:comment:decision")
	m.refreshAll()
	openDetailFor(t, m, tk.ID)

	if got := m.tasks.currentDrill().cursor; got >= 0 {
		t.Fatalf("a freshly opened page starts with no cursor, got %d", got)
	}
	walkCursorToFirstRow(t, m)

	if !strings.Contains(stripANSI(m.tasks.renderDrillModal()), "▶") {
		t.Errorf("the cursor row must be marked:\n%s", stripANSI(m.tasks.renderDrillModal()))
	}
	update(t, m, "enter")
	level := m.tasks.currentDrill()
	if level == nil || level.kind != drillComment || level.id != newest.ID {
		t.Fatalf("enter must drill the newest comment %q, got %+v", newest.ID, level)
	}
	update(t, m, "esc")
	if m.tasks.detailID() != tk.ID {
		t.Fatalf("esc must land back on DETAILS, got %q", m.tasks.detailID())
	}
}

// walkCursorToFirstRow presses j until the cursor lands on the first row,
// which is what "scroll down to the comments, then walk them" means from the
// keyboard's side.
func walkCursorToFirstRow(t *testing.T, m *Model) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if c := m.tasks.currentDrill().cursor; c == 0 {
			return
		}
		update(t, m, "j")
	}
	t.Fatal("j never moved the cursor onto a comment row")
}

// The older-count footer is itself a row: it is what the cursor reaches at
// the bottom, and it opens the thread.
func TestDetailOlderFooterRowOpensTheThread(t *testing.T) {
	m := seedDetailModel(t)
	m.SetSize(140, 44)
	tk := seedTask(t, m, "ATM", "many comments", "ATM:scrum:task")
	for _, b := range []string{"c1", "c2", "c3", "c4", "c5"} {
		seedComment(t, m, tk.ID, b, "ATM:comment:progress")
	}
	m.refreshAll()
	openDetailFor(t, m, tk.ID)

	walkCursorToFirstRow(t, m)
	for i := 0; i < detailCommentsShown; i++ {
		update(t, m, "j")
	}
	update(t, m, "enter")

	level := m.tasks.currentDrill()
	if level == nil || level.kind != drillThread || level.id != tk.ID {
		t.Fatalf("the older-count row must open the thread, got %+v", level)
	}
}

// C is the direct route to the thread, cursor or no cursor.
func TestDetailCOpensTheThreadAndListsEveryComment(t *testing.T) {
	m := seedDetailModel(t)
	m.SetSize(140, 44)
	tk := seedTask(t, m, "ATM", "threaded", "ATM:scrum:task")
	first := seedComment(t, m, tk.ID, "c1 body", "ATM:comment:progress")
	for _, b := range []string{"c2 body", "c3 body", "c4 body", "c5 body"} {
		seedComment(t, m, tk.ID, b, "ATM:comment:progress")
	}
	m.refreshAll()
	openDetailFor(t, m, tk.ID)

	update(t, m, "C")
	level := m.tasks.currentDrill()
	if level == nil || level.kind != drillThread {
		t.Fatalf("C must open the thread, got %+v", level)
	}
	if m.capability.open {
		t.Fatal("C inside the drill must not reach the capability switcher")
	}
	view := stripANSI(m.tasks.renderDrillModal())
	for _, want := range []string{"c1 body", "c2 body", "c3 body", "c4 body", "c5 body"} {
		if !strings.Contains(view, want) {
			t.Errorf("the thread lists every comment, missing %q:\n%s", want, view)
		}
	}

	// The thread's own cursor drills a comment, and esc walks back level by
	// level: comment -> thread -> details -> list.
	for i := 0; i < 4; i++ {
		update(t, m, "j")
	}
	update(t, m, "enter")
	if lv := m.tasks.currentDrill(); lv == nil || lv.kind != drillComment || lv.id != first.ID {
		t.Fatalf("the thread's last row must drill the oldest comment %q, got %+v", first.ID, lv)
	}
	update(t, m, "esc")
	if lv := m.tasks.currentDrill(); lv == nil || lv.kind != drillThread {
		t.Fatalf("esc must return to the thread, got %+v", lv)
	}
	update(t, m, "esc")
	if m.tasks.detailID() != tk.ID {
		t.Fatalf("esc must return to DETAILS, got %q", m.tasks.detailID())
	}
	update(t, m, "esc")
	if len(m.tasks.drillStack) != 0 {
		t.Fatalf("esc must return to the list, stack = %v", m.tasks.drillStack)
	}
}

// C outside a drill still belongs to the capability switcher.
func TestCapabilitySwitcherStillOpensFromTheList(t *testing.T) {
	m := seedDetailModel(t)
	m.focused = paneTasks
	update(t, m, "C")
	if !m.capability.open {
		t.Fatal("C on the task list must still open the capability switcher")
	}
}

// The comments digest yields to the sections under it: on a short terminal
// it shows fewer rows rather than pushing FACTS off the page.
func TestDetailCommentsShrinkOnShortTerminals(t *testing.T) {
	m := seedDetailModel(t)
	m.SetSize(140, 26)
	tk := seedTask(t, m, "ATM", "short terminal", "ATM:scrum:task")
	for _, b := range []string{"c1", "c2", "c3", "c4", "c5"} {
		seedComment(t, m, tk.ID, b, "ATM:comment:progress")
	}
	m.refreshAll()

	lines := openDetailModal(t, m, tk.ID)
	body := strings.Join(lines, "\n")

	if !strings.Contains(body, "FACTS") {
		t.Errorf("FACTS must stay on the page when the terminal is short:\n%s", body)
	}
	if strings.Contains(body, "c3") {
		t.Errorf("a short page must show fewer than the default %d comments:\n%s", detailCommentsShown, body)
	}
}

// Page scrolling is not row navigation: pgdn and g move the viewport and
// leave the cursor where it was.
func TestDetailPageScrollKeysLeaveTheCursorAlone(t *testing.T) {
	m := seedDetailModel(t)
	m.SetSize(140, 30)
	tk := seedTask(t, m, "ATM", "scrolling", "ATM:scrum:task")
	for _, b := range []string{"c1", "c2", "c3", "c4"} {
		seedComment(t, m, tk.ID, b, "ATM:comment:progress")
	}
	m.refreshAll()
	openDetailFor(t, m, tk.ID)
	walkCursorToFirstRow(t, m)

	before := m.tasks.currentDrill().cursor
	update(t, m, "pgdown")
	update(t, m, "g")
	if got := m.tasks.currentDrill().cursor; got != before {
		t.Errorf("page keys moved the cursor from %d to %d", before, got)
	}
}
