package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"atm/internal/core"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func feedEntry(action string, payload string) core.LogEntry {
	e := core.LogEntry{Action: action}
	if payload != "" {
		e.Payload = json.RawMessage(payload)
	}
	return e
}

func TestShortEventID(t *testing.T) {
	if got := shortEventID("sha256:84fbf586004add7e"); got != "84fbf58" {
		t.Fatalf("shortEventID = %q, want 84fbf58", got)
	}
	if got := shortEventID(""); got != "" {
		t.Fatalf("shortEventID(v1) = %q, want empty", got)
	}
}

func TestCompactAge(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		at   time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "now"},
		{now.Add(-5 * time.Minute), "5m"},
		{now.Add(-3 * time.Hour), "3h"},
		{now.Add(-49 * time.Hour), "2d"},
		{time.Time{}, ""},
	}
	for _, c := range cases {
		if got := compactAge(c.at, now); got != c.want {
			t.Errorf("compactAge(%v) = %q, want %q", c.at, got, c.want)
		}
	}
}

// TestEventFeedActor pins R3-1: the actor column's text is the full
// persona@agent:model identity. Revision 2 abbreviated it to 8 cells, which
// rendered developer@claude and developer@codex identically ("dev@clau").
func TestEventFeedActor(t *testing.T) {
	for _, in := range []string{
		"developer@claude:opus-4.8",
		"developer@codex:unset",
		"admin@tui:unset",
		"default", // v1-era actor without @
	} {
		if got := eventFeedActor(in); got != in {
			t.Errorf("eventFeedActor(%q) = %q, want it unabbreviated", in, got)
		}
	}
}

// TestFeedLayoutWidthTable pins the width allocation at the four inner widths
// measured for the pane (chartBoxInnerWidth(innerPaneWidth(splitWorkspaceWidths(term)))):
// terminal 80 -> 26, 120 -> 42, 160 -> 57, 200 -> 72, for the 22-cell actor
// "developer@claude:unset". It is the table in R3-4 made executable: the
// message reaches its floor before the actor gives up anything, and below the
// floor both columns shrink together rather than one starving the other.
func TestFeedLayoutWidthTable(t *testing.T) {
	const actorFull = 22 // len("developer@claude:unset")
	cases := []struct {
		term, innerW, actorW, msgW int
	}{
		{80, 26, 4, 4},
		{120, 42, 12, 8},
		{160, 57, 22, 13},
		{200, 72, 22, 28},
	}
	for _, c := range cases {
		left, _ := splitWorkspaceWidths(c.term)
		if got := chartBoxInnerWidth(innerPaneWidth(left)); got != c.innerW {
			t.Fatalf("terminal %d: events box inner width = %d, want %d", c.term, got, c.innerW)
		}
		cols := feedLayout(c.innerW, 1, actorFull)
		if cols.actorW != c.actorW || cols.msgW != c.msgW {
			t.Errorf("feedLayout(%d) = actorW %d, msgW %d; want %d, %d", c.innerW, cols.actorW, cols.msgW, c.actorW, c.msgW)
		}
		// The allocation accounts for every cell of the inner width: gutter,
		// id and subject with their separators, then the flexible columns.
		fixed := 1 + 1 + feedIDWidth + 1 + feedSubjectWidth + 1
		if got := fixed + cols.actorW + cols.msgW + cols.ageW; got != c.innerW {
			t.Errorf("feedLayout(%d) allocates %d cells, want %d", c.innerW, got, c.innerW)
		}
	}
}

func TestEventFeedSubject(t *testing.T) {
	task := core.LogEntry{Subject: core.Subject{Kind: "task", ID: "ATM-90171b"}}
	if got := eventFeedSubject(task, "ATM"); got != "90171b" {
		t.Fatalf("task subject = %q, want 90171b", got)
	}
	comment := core.LogEntry{Subject: core.Subject{Kind: "comment", ID: "ATM-90171b-cdf66"}}
	if got := eventFeedSubject(comment, "ATM"); got != "90171b" {
		t.Fatalf("comment subject = %q, want parent task alias 90171b", got)
	}
	project := core.LogEntry{Subject: core.Subject{Kind: "project", Code: "ATM"}}
	if got := eventFeedSubject(project, "ATM"); got != "–" {
		t.Fatalf("project subject = %q, want –", got)
	}
}

func TestFeedLabel(t *testing.T) {
	if got := feedLabel("ATM:status:done", "ATM"); got != "done" {
		t.Fatalf("status label = %q, want bare value done", got)
	}
	if got := feedLabel("ATM:type:bug", "ATM"); got != "type:bug" {
		t.Fatalf("non-status label = %q, want type:bug", got)
	}
}

func TestEventDigestMessage(t *testing.T) {
	cases := []struct {
		action, payload, want string
	}{
		{"task.created", `{"title":"Fix the cache"}`, `created "Fix the cache"`},
		{"task.created", `{}`, "created"}, // v1-upgrade payloads may lack keys
		{"task.title-changed", `{"title":"New title"}`, `retitled "New title"`},
		{"task.description-changed", "", "description edited"},
		{"task.label-added", `{"label":"ATM:status:done"}`, "+done"},
		{"task.label-removed", `{"label":"ATM:status:in-progress"}`, "−in-progress"},
		{"task.label-added", `{"labels":["x"]}`, "+label"}, // v1 snapshot payload
		{"task.removed", "", "removed"},
		{"task.meta-changed", "", "meta"},
		{"comment.created", "", "commented"},
		{"comment.body-changed", "", "comment edited"},
		{"comment.label-added", `{"label":"ATM:comment:decision"}`, "comment +comment:decision"},
		{"comment.removed", "", "comment removed"},
		{"project.created", "", "project created"},
		{"project.name-changed", `{"name":"Acme"}`, `renamed "Acme"`},
		{"project.capability-enabled", `{"capability":"contextmap"}`, "+contextmap"},
		{"project.capability-disabled", `{"capability":"contextmap"}`, "−contextmap"},
	}
	for _, c := range cases {
		e := feedEntry(c.action, c.payload)
		if got := eventDigestMessage(e, "ATM"); got != c.want {
			t.Errorf("%s: digest = %q, want %q", c.action, got, c.want)
		}
	}
	// label.removed names its subject, not a payload field.
	e := core.LogEntry{Action: "label.removed", Subject: core.Subject{Kind: "label", Name: "ATM:status:open"}}
	if got := eventDigestMessage(e, "ATM"); got != "−label status:open" {
		t.Errorf("label.removed digest = %q, want −label status:open", got)
	}
	// label.upserted also names its subject, not a payload field.
	e = core.LogEntry{Action: "label.upserted", Subject: core.Subject{Kind: "label", Name: "ATM:status:open"}}
	if got := eventDigestMessage(e, "ATM"); got != "label status:open" {
		t.Errorf("label.upserted digest = %q, want label status:open", got)
	}
}

func TestEventGraphRowsLinearChain(t *testing.T) {
	entries := []core.LogEntry{ // newest first
		{ID: "sha256:cc", Parents: []string{"sha256:bb"}},
		{ID: "sha256:bb", Parents: []string{"sha256:aa"}},
		{ID: "sha256:aa"},
	}
	rows := eventGraphRows(entries)
	if len(rows) != len(entries) {
		t.Fatalf("len(rows) = %d, want %d", len(rows), len(entries))
	}
	for i, r := range rows {
		if string(r) != "●" {
			t.Fatalf("row %d = %q, want single ●", i, string(r))
		}
	}
}

func TestEventGraphRowsForkAndMerge(t *testing.T) {
	// DAG: root a; concurrent b and c (both parent a); d merges b+c.
	// Newest-first display order: d, c, b, a.
	entries := []core.LogEntry{
		{ID: "sha256:dd", Parents: []string{"sha256:bb", "sha256:cc"}},
		{ID: "sha256:cc", Parents: []string{"sha256:aa"}},
		{ID: "sha256:bb", Parents: []string{"sha256:aa"}},
		{ID: "sha256:aa"},
	}
	rows := eventGraphRows(entries)
	if len(rows) != len(entries) {
		t.Fatalf("len(rows) = %d, want %d", len(rows), len(entries))
	}
	want := []string{"●", "│●", "●│", "●│"}
	for i, w := range want {
		if string(rows[i]) != w {
			t.Fatalf("row %d = %q, want %q\nall rows: %q", i, string(rows[i]), w, rows)
		}
	}
}

func TestEventGraphRowsLaneCap(t *testing.T) {
	// Four concurrent tips (no lane awaits them): lanes must cap at 3.
	entries := []core.LogEntry{
		{ID: "sha256:t1", Parents: []string{"sha256:aa"}},
		{ID: "sha256:t2", Parents: []string{"sha256:aa"}},
		{ID: "sha256:t3", Parents: []string{"sha256:aa"}},
		{ID: "sha256:t4", Parents: []string{"sha256:aa"}},
		{ID: "sha256:aa"},
	}
	rows := eventGraphRows(entries)
	if len(rows) != len(entries) {
		t.Fatalf("len(rows) = %d, want %d", len(rows), len(entries))
	}
	want := []string{"●", "│●", "││●", "││●", "●││"}
	for i, w := range want {
		if string(rows[i]) != w {
			t.Fatalf("row %d = %q, want %q", i, string(rows[i]), w)
		}
		if len(rows[i]) > maxGraphLanes {
			t.Fatalf("row %d has %d lanes, cap is %d", i, len(rows[i]), maxGraphLanes)
		}
	}
}

func TestEventGraphRowsV1EntriesSingleLane(t *testing.T) {
	entries := []core.LogEntry{{Action: "task.created"}, {Action: "project.created"}}
	rows := eventGraphRows(entries)
	if len(rows) != len(entries) {
		t.Fatalf("len(rows) = %d, want %d", len(rows), len(entries))
	}
	for i, r := range rows {
		if string(r) != "●" {
			t.Fatalf("v1 row %d = %q, want ●", i, string(r))
		}
	}
}

// TestProjectPaneSplitHeightsFourWayLegacyCases pins the same totals the
// pre-art three-way split test covered (27, 10, 2, 1, 0), recomputed for the
// four-way split added by ATM-4eae82 (list fixed at 9, art absorbing the
// remainder after events/summary, folding into summary below its 3-line
// minimum). See TestProjectPaneSplitHeights4Way for the fuller table.
func TestProjectPaneSplitHeightsFourWayLegacyCases(t *testing.T) {
	cases := []struct {
		total, list, art, events, summary int
	}{
		{27, 9, 0, 9, 9},
		{10, 9, 0, 0, 1}, // list alone (9) leaves 1: too small for events, art
		{2, 2, 0, 0, 0},  // below the fixed list height: list takes it all
		{1, 1, 0, 0, 0},
		{0, 0, 0, 0, 0},
	}
	for _, c := range cases {
		l, a, e, s := projectPaneSplitHeights(c.total)
		if l != c.list || a != c.art || e != c.events || s != c.summary {
			t.Errorf("split(%d) = (%d,%d,%d,%d), want (%d,%d,%d,%d)", c.total, l, a, e, s, c.list, c.art, c.events, c.summary)
		}
	}
}

func TestRecentEventsFeedRendersDigestLines(t *testing.T) {
	m := newTestModel(t)
	// 200-wide (an existing convention for tests that need to see full
	// rendered content, e.g. TestProjectDetailDashboardSections): only there
	// is the events box's inner width (72) enough for both a whole actor and
	// an untruncated digest message. At 120 the inner width is 42 and the
	// message sits at its floor of 8 (see TestFeedLayoutWidthTable).
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	seedTask(t, m, "ATM", "Fix the cache", "ATM:status:open")
	body := m.projects.View()
	mustContain(t, body, "Recent Events")
	mustContain(t, body, `created "Fix the cache"`)
	mustContain(t, body, testActor) // full actor identity (R3-1)
	mustContain(t, body, "●")
	if !regexp.MustCompile(`[0-9a-f]{7}`).MatchString(body) {
		t.Fatalf("feed shows no short event id\n--- body ---\n%s", body)
	}
	// Newest-first: the task creation renders above the project creation.
	taskIdx := strings.Index(body, `created "Fix the cache"`)
	projIdx := strings.Index(body, "project created")
	if projIdx >= 0 && taskIdx > projIdx {
		t.Fatalf("feed is not newest-first\n--- body ---\n%s", body)
	}
}

func TestRecentEventsFeedPlaceholders(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	// No selection: the feed section is folded away entirely (its rows
	// given back to the summary) rather than showing its own "select a
	// project" placeholder, which would double the summary's identical
	// message directly below it on the fresh-launch screen.
	body := m.projects.View()
	mustNotContain(t, body, "select a project to see events")
	mustContain(t, body, "select a project to see summaries")

	// Once a project is selected, the events section renders normally.
	update(t, m, "s")
	body = m.projects.View()
	mustContain(t, body, "Recent Events")
	mustNotContain(t, body, "select a project to see events") // has events (project.created etc.)
}

// TestEventsFeedPlaceholderRendersInsideBox proves a placeholder body (no
// project selected here) still renders inside the same bordered box the
// populated feed uses, and top-aligned rather than vertically centered —
// placeholder bodies are blank-filled to height-2 lines so renderChartBox's
// top-pad is a no-op in every state, the same way the populated feed already
// is. Calls renderEventsFeed directly (bypassing renderList's empty-scope
// fold-away, and its boxed-vs-compact decision) since the projectScope==""
// branch is otherwise unreachable through View(); boxed=true pins this test
// to the box form specifically.
func TestEventsFeedPlaceholderRendersInsideBox(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	body := m.projects.renderEventsFeed(10, true)
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], "╭") {
		t.Fatalf("placeholder render has no box top border\n--- body ---\n%s", body)
	}
	if !strings.Contains(lines[len(lines)-1], "╰") {
		t.Fatalf("placeholder render has no box bottom border\n--- body ---\n%s", body)
	}
	if !strings.Contains(lines[1], "select a project to see events") {
		t.Fatalf("placeholder text is not top-aligned (row directly under the top border): %q", lines[1])
	}
}

// TestEventFeedLineDegradesOnNarrowPane covers the one surviving degradation
// rung (R3-3): the age column — the least load-bearing of the fixed columns —
// drops below feedAgeMinWidth, giving its cells to the actor and message. The
// id and subject columns never drop, at any width (see
// TestEventIDPresentAtAllWidths); the actor and message narrow rather than
// vanish (see TestMessageTruncatesBeforeActor).
func TestEventFeedLineDegradesOnNarrowPane(t *testing.T) {
	m := newTestModel(t)
	m.projectScope = "ATM"
	p := &m.projects
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	e := core.LogEntry{
		ID:      "sha256:84fbf586004a",
		Action:  "comment.created",
		At:      now.Add(-time.Hour),
		Actor:   testActor,
		Subject: core.Subject{Kind: "task", ID: "ATM-90171b"},
	}
	lanes := []rune{'●'}
	// eventFeedLine lays out from the feedColumns it is handed (allocated by
	// the caller for the events box's inner width), so these cases exercise
	// that allocation, not the model field.
	line := p.eventFeedLine(e, lanes, feedLayout(feedAgeMinWidth, 1, len(testActor)), now, true)
	mustContain(t, line, "84fbf58")
	mustContain(t, line, "1h") // exactly at the threshold: the age still renders
	line = p.eventFeedLine(e, lanes, feedLayout(feedAgeMinWidth-1, 1, len(testActor)), now, true)
	mustContain(t, line, "84fbf58") // the id never drops
	mustNotContain(t, line, "1h")
}

// TestRecentEventsFeedAt120Columns pins the feed's behavior at the terminal
// width users see most often: 120 columns, where the events box's inner width
// is 42. That is the regime R3-4 records as the accepted cost of ranking the
// id and the actor above the message: the id renders, the actor is truncated,
// and the message sits at its floor. Also asserts no rendered feed line
// overruns the pane.
func TestRecentEventsFeedAt120Columns(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	seedTask(t, m, "ATM", "Fix cache", "ATM:status:open")
	body := m.projects.View()
	mustContain(t, body, "Recent Events")
	if !regexp.MustCompile(`[0-9a-f]{7}`).MatchString(strings.Join(feedBoxRows(t, body), "\n")) {
		t.Fatalf("feed shows no short event id at 120 columns (the id never drops)\n--- body ---\n%s", body)
	}
	for _, line := range strings.Split(body, "\n") {
		if w := lipgloss.Width(line); w > m.projects.width {
			t.Fatalf("rendered line exceeds pane inner width %d (got %d): %q", m.projects.width, w, line)
		}
	}
}

// TestEventFeedCapsAtMaxFeedEvents pins the rule: the feed
// bounds itself to the newest maxFeedEvents entries rather than reversing
// and graphing the project's entire history on every render frame (an
// O(total events) allocation pass in the render loop). This tests the
// bounding logic directly against a synthetic oldest-first slice — seeding
// maxFeedEvents+ events through the full model is unnecessarily slow for a
// unit-level guarantee about a pure helper.
func TestEventFeedCapsAtMaxFeedEvents(t *testing.T) {
	total := maxFeedEvents + 50
	entries := make([]core.LogEntry, total)
	for i := range entries {
		entries[i] = core.LogEntry{ID: fmt.Sprintf("sha256:%08x", i)}
	}
	feed := newestFeedEntries(entries)
	if len(feed) != maxFeedEvents {
		t.Fatalf("len(feed) = %d, want %d (cap)", len(feed), maxFeedEvents)
	}
	// Newest-first: feed[0] is the last entry seeded.
	if feed[0].ID != entries[total-1].ID {
		t.Fatalf("feed[0].ID = %q, want %q (newest)", feed[0].ID, entries[total-1].ID)
	}
	if feed[len(feed)-1].ID != entries[total-maxFeedEvents].ID {
		t.Fatalf("feed tail = %q, want %q", feed[len(feed)-1].ID, entries[total-maxFeedEvents].ID)
	}
	// Under-cap: shorter than maxFeedEvents, every entry survives, still
	// newest-first.
	short := []core.LogEntry{{ID: "sha256:aa"}, {ID: "sha256:bb"}, {ID: "sha256:cc"}}
	got := newestFeedEntries(short)
	if len(got) != len(short) {
		t.Fatalf("under-cap len(feed) = %d, want %d (no entries dropped)", len(got), len(short))
	}
	want := []string{"sha256:cc", "sha256:bb", "sha256:aa"}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("under-cap feed[%d].ID = %q, want %q (newest-first)", i, got[i].ID, id)
		}
	}
}

// TestRecentEventsFeedScrollRevealsNewContent asserts the core scrolling
// property: paging the feed (shift+right) surfaces a digest line that was
// off the initial tail window, with no cursor row to highlight (R2-3:
// logsOffset is a pure viewport offset). Also asserts the negative: no
// reverse-video escape appears anywhere inside the events box, scoped to the
// box's own rows so the project list's own RowCursor-styled selection higher
// up in the same pane can't produce a false pass.
func TestRecentEventsFeedScrollRevealsNewContent(t *testing.T) {
	m := newTestModel(t)
	// 200-wide: at 120 the message column sits at its floor of 8 (R3-4), so
	// every "created ..." digest renders as "crea..." and the two titles this
	// test distinguishes between are no longer distinguishable on screen.
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	// 25 tasks, seeded oldest (Task 00) to newest (Task 24): comfortably
	// more than one feed page, so paging once brings entries that were off
	// the initial tail window into view.
	for i := 0; i < 25; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("Task %02d", i))
	}

	tail := m.projects.View()
	mustContain(t, tail, `created "Task 24"`)    // newest task: unscrolled view is pinned here
	mustNotContain(t, tail, `created "Task 13"`) // one page down: not yet visible

	update(t, m, "shift+right")
	scrolled := m.projects.View()
	mustContain(t, scrolled, `created "Task 13"`) // paging surfaced it

	// RowCursor applies a Reverse SGR attribute, invisible at the default
	// (ascii) test color profile, so force ANSI256 for this assertion only.
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	highlighted := m.projects.View()
	lines := strings.Split(highlighted, "\n")
	top := -1
	for i, line := range lines {
		if strings.Contains(line, "Recent Events") {
			top = i
			break
		}
	}
	if top < 0 {
		t.Fatal("no events box")
	}
	_, _, eventsH, _ := projectPaneSplitHeights(m.projects.contentHeight)
	for i := top; i < top+eventsH && i < len(lines); i++ {
		if strings.Contains(lines[i], "\x1b[7m") {
			t.Fatalf("events box row has a reverse-video escape; no row should be highlighted: %q", lines[i])
		}
	}
}

func TestShiftArrowsScrollFeedWhilePlainKeysDriveList(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedProject(t, m, "ZZZ", "Second")
	update(t, m, "s")
	for i := 0; i < 30; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("Task %02d", i))
	}
	listBefore := m.projects.cursor
	update(t, m, "shift+down")
	update(t, m, "shift+down")
	if m.projects.logsOffset != 2 {
		t.Fatalf("shift+down x2: logsOffset = %d, want 2", m.projects.logsOffset)
	}
	if m.projects.cursor != listBefore {
		t.Fatalf("shift+down moved the project list cursor (%d -> %d)", listBefore, m.projects.cursor)
	}
	update(t, m, "j")
	if m.projects.cursor != listBefore+1 {
		t.Fatalf("plain j should move the list cursor, got %d", m.projects.cursor)
	}
	if m.projects.logsOffset != 2 {
		t.Fatalf("plain j moved the feed offset to %d", m.projects.logsOffset)
	}
	update(t, m, "shift+up")
	if m.projects.logsOffset != 1 {
		t.Fatalf("shift+up: logsOffset = %d, want 1", m.projects.logsOffset)
	}
}

func TestShiftLeftRightPageFeedAndOffsetClamps(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	for i := 0; i < 30; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("Task %02d", i))
	}
	update(t, m, "shift+right")
	paged := m.projects.logsOffset
	if paged <= 1 {
		t.Fatalf("shift+right should page by more than one line, got %d", paged)
	}
	for i := 0; i < 200; i++ {
		update(t, m, "shift+right")
	}
	// Window-aware upper bound (R2-3): feedLen()-rows, floored at 0 — not
	// feedLen()-1. See TestScrollEventsFeedClampsToVisibleWindow for the
	// dedicated, multi-regime pin of this bound; this assertion keeps this
	// test's own heavy shift+right hammering (a different call
	// path/contentHeight than that test's fixed setup) from silently
	// regressing to the wrong bound.
	_, _, eventsH, _ := projectPaneSplitHeights(m.projects.contentHeight)
	wantMax := m.projects.feedLen() - eventsFeedVisibleRows(eventsH)
	if wantMax < 0 {
		wantMax = 0
	}
	if m.projects.logsOffset != wantMax {
		t.Fatalf("offset %d at max scroll, want %d (feedLen %d - visible rows)", m.projects.logsOffset, wantMax, m.projects.feedLen())
	}
	for i := 0; i < 300; i++ {
		update(t, m, "shift+left")
	}
	if m.projects.logsOffset != 0 {
		t.Fatalf("shift+left to the top: logsOffset = %d, want 0", m.projects.logsOffset)
	}
}

func TestFeedOffsetResetsOnProjectSwitch(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedProject(t, m, "ZZZ", "Second")
	update(t, m, "s")
	for i := 0; i < 20; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("Task %02d", i))
	}
	update(t, m, "shift+down")
	update(t, m, "shift+down")
	if m.projects.logsOffset == 0 {
		t.Fatal("setup: offset should be non-zero before the switch")
	}
	update(t, m, "j")
	update(t, m, "s") // select ZZZ
	if m.projects.logsOffset != 0 {
		t.Fatalf("project switch should reset logsOffset, got %d", m.projects.logsOffset)
	}
}

func TestNoSubfocusModeRemains(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	seedTask(t, m, "ATM", "Fix cache")
	// L is no longer a feed binding: it must not swallow subsequent list keys.
	update(t, m, "L")
	before := m.projects.cursor
	seedProject(t, m, "ZZZ", "Second")
	update(t, m, "j")
	if m.projects.cursor != before+1 {
		t.Fatalf("after L, plain j must still drive the project list (cursor %d -> %d)", before, m.projects.cursor)
	}
	mustNotContain(t, m.projects.statusHint(), "[L]ogs")
}

func TestEventsFeedRendersAsBoxAlignedWithCharts(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	seedTask(t, m, "ATM", "Fix cache")
	body := m.projects.View()
	mustContain(t, body, "Recent Events  [Shift-↑↓]")
	// The events box and the persona chart box must share left and right edges.
	lines := strings.Split(body, "\n")
	edgesOf := func(marker string) (int, int) {
		t.Helper()
		for i, line := range lines {
			if strings.Contains(line, marker) {
				plain := stripANSI(line)
				// Rune (display-column) index, not byte index: "╭"/"╮" and
				// the title's "↑"/"↓" are 3-byte UTF-8 runes, and the two
				// titles being compared here differ in length, so a
				// byte-offset comparison would fail even when the boxes are
				// genuinely column-aligned.
				runes := []rune(plain)
				left, right := -1, -1
				for j, r := range runes {
					switch r {
					case '╭':
						if left == -1 {
							left = j
						}
					case '╮':
						right = j
					}
				}
				return left, right
			}
			_ = i
		}
		t.Fatalf("no box top border containing %q\n--- body ---\n%s", marker, body)
		return -1, -1
	}
	el, er := edgesOf("Recent Events")
	pl, pr := edgesOf("activity by persona")
	if el != pl || er != pr {
		t.Fatalf("events box edges (%d,%d) != persona box edges (%d,%d)\n--- body ---\n%s", el, er, pl, pr, body)
	}
}

// TestEventsFeedFramingMatchesPersonaChart pins the I1 review fix: the feed
// must not decide boxed-vs-compact independently of the summary section, or
// the pane goes back to reading as sections in two visual languages — the
// original motivation for boxing the feed at all, just inverted.
//
// It sweeps every pane height from 8 through 60 at a fixed width of 80 —
// generously spanning the boxed/unboxed threshold (measured at pane height
// 28 for this fixture; projectPaneSplitHeights + summaryChartsBoxed decide
// it, and either could move it) — rather than pinning two heights either
// side of it, because a threshold move that lands between two pinned heights
// would pass unnoticed. summaryChartsBoxed itself re-derives, rather than
// reuses, renderSummary's and renderPersonaActivityChart's framing rules (see
// the doc comment on summaryChartsBoxed), so nothing at compile time catches
// those rules drifting out of sync with this one; only a render-level check
// like this one does. At each height both sections' framing is read off the
// SAME rendered body (one m.projects.View() call), so the assertion cannot
// pass when only one section's framing actually changed. A height where the
// events slot has collapsed entirely (too short to render at all — a
// legitimate state, see projectPaneSplitHeights) is skipped rather than
// failed, since there is no feed framing to compare. Likewise, ATM-4eae82's
// four-way split can independently collapse the summary slot to 0 (list
// fixed at 9, events at its own ~35% share, leaving nothing for summary —
// e.g. total 13 gives (9,0,4,0)) even while the events slot still renders:
// that state is skipped the same way, keyed off projectPaneSplitHeights
// rather than the rendered body, since a collapsed summary has no persona
// chart to compare framing against either.
func TestEventsFeedFramingMatchesPersonaChart(t *testing.T) {
	findLine := func(body, marker string) string {
		for _, line := range strings.Split(body, "\n") {
			if strings.Contains(line, marker) {
				return line
			}
		}
		return ""
	}
	isBoxed := func(line string) bool {
		return strings.Contains(line, "╭") || strings.Contains(line, "╮")
	}

	const width = 80
	sawBoxed, sawUnboxed := false, false
	for h := 8; h <= 60; h++ {
		m := newTestModel(t)
		m.SetSize(width, h)
		seedProject(t, m, "ATM", "Acme Task Manager")
		update(t, m, "s")
		seedTask(t, m, "ATM", "Fix cache")
		body := m.projects.View()

		feedLine := findLine(body, "Recent Events")
		if feedLine == "" {
			// Events slot collapsed at this height — nothing to compare.
			continue
		}
		personaLine := findLine(body, "activity by persona")
		if personaLine == "" {
			_, _, _, summaryH := projectPaneSplitHeights(m.projects.contentHeight)
			if summaryH == 0 {
				// Summary slot collapsed at this height — nothing to compare.
				continue
			}
			t.Fatalf("height %d: events feed rendered but persona chart section is missing\n--- body ---\n%s", h, body)
		}

		feedBoxed, personaBoxed := isBoxed(feedLine), isBoxed(personaLine)
		if feedBoxed {
			sawBoxed = true
		} else {
			sawUnboxed = true
		}
		if feedBoxed != personaBoxed {
			t.Fatalf("height %d: framing mismatch — events feed boxed=%v (%q), persona chart boxed=%v (%q)",
				h, feedBoxed, feedLine, personaBoxed, personaLine)
		}
	}
	if !sawBoxed || !sawUnboxed {
		t.Fatalf("sweep (heights 8-60) never observed both framings (sawBoxed=%v, sawUnboxed=%v) — it no longer straddles the boxed/unboxed threshold; widen the range", sawBoxed, sawUnboxed)
	}
}

// feedBodyLines extracts the events box's visible EVENT rows from a rendered
// pane view — feedBoxRows without the column header row (revision 3, R3-2)
// and with each row trimmed, so a caller can compare rows for equality or
// check one for blankness.
func feedBodyLines(t *testing.T, view string, eventsH int) []string {
	t.Helper()
	rows := feedBoxRows(t, view)[1:] // [0] is the column header
	body := make([]string, 0, eventsFeedVisibleRows(eventsH))
	for i := 0; i < eventsFeedVisibleRows(eventsH) && i < len(rows); i++ {
		body = append(body, strings.TrimSpace(rows[i]))
	}
	return body
}

// TestScrollEventsFeedNoopWhenSlotCollapsed covers scrollEventsFeed's
// eventsH == 0 early return: when the events slot is too short to render at
// all (projectPaneSplitHeights collapses it to 0), shift+down must not move
// logsOffset — there is no window for an offset to be relative to.
func TestScrollEventsFeedNoopWhenSlotCollapsed(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	m.projects.contentHeight = 3
	_, _, eventsH, _ := projectPaneSplitHeights(m.projects.contentHeight)
	if eventsH != 0 {
		t.Fatalf("setup: expected the events slot collapsed to 0 at contentHeight 3, got %d", eventsH)
	}
	update(t, m, "shift+down")
	if m.projects.logsOffset != 0 {
		t.Fatalf("scrollEventsFeed should no-op with the events slot collapsed, got logsOffset=%d", m.projects.logsOffset)
	}
}

// TestScrollEventsFeedClampsToVisibleWindow pins the rule: scrollEventsFeed's
// upper clamp is feedLen() minus the visible row count (R2-3), not
// feedLen()-1 — a feed that does not overflow its window must not scroll at
// all, and at maximum scroll no event may be stranded above a column of
// blanks. This exercises all three regimes: under, exactly-one-over, and
// well-over the window.
func TestScrollEventsFeedClampsToVisibleWindow(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")

	// Fix the events box to a known, generous height so the visible row
	// count (`rows`) is stable and comfortably larger than the feed
	// selecting a project produces on its own (EnsureVocabulary seeds a
	// handful of label events).
	m.projects.contentHeight = 63
	_, _, eventsH, _ := projectPaneSplitHeights(m.projects.contentHeight)
	rows := eventsFeedVisibleRows(eventsH)

	// (a) feed shorter than the window: shift+down is a no-op, offset stays 0.
	n0 := m.projects.feedLen()
	if n0 >= rows {
		t.Fatalf("setup: feedLen %d not shorter than window %d — adjust contentHeight", n0, rows)
	}
	update(t, m, "shift+down")
	if m.projects.logsOffset != 0 {
		t.Fatalf("(a) feed shorter than window: shift+down should be a no-op, offset = %d", m.projects.logsOffset)
	}
	t.Logf("(a) feedLen=%d rows=%d offset after shift+down=%d (want 0)", n0, rows, m.projects.logsOffset)

	// (b) feed with exactly one row of overflow: offset reaches exactly 1
	// and no further.
	for m.projects.feedLen() < rows+1 {
		if err := m.store.SetProjectName("ATM", fmt.Sprintf("Acme %d", m.projects.feedLen()), m.actor); err != nil {
			t.Fatalf("SetProjectName: %v", err)
		}
	}
	if got := m.projects.feedLen(); got != rows+1 {
		t.Fatalf("setup: feedLen = %d, want exactly %d (rows+1)", got, rows+1)
	}
	update(t, m, "shift+down")
	if m.projects.logsOffset != 1 {
		t.Fatalf("(b) one row overflow: offset after shift+down = %d, want 1", m.projects.logsOffset)
	}
	update(t, m, "shift+down")
	if m.projects.logsOffset != 1 {
		t.Fatalf("(b) one row overflow: offset must not exceed 1, got %d", m.projects.logsOffset)
	}
	t.Logf("(b) feedLen=%d rows=%d max offset=%d (want 1)", m.projects.feedLen(), rows, m.projects.logsOffset)

	// (c) feed much longer than the window: max offset leaves the box
	// completely full of events, no blank rows below.
	for i := 0; i < 40; i++ {
		if err := m.store.SetProjectName("ATM", fmt.Sprintf("Acme long %d", i), m.actor); err != nil {
			t.Fatalf("SetProjectName: %v", err)
		}
	}
	for i := 0; i < 200; i++ {
		update(t, m, "shift+down")
	}
	feedLen := m.projects.feedLen()
	wantMax := feedLen - rows
	if m.projects.logsOffset != wantMax {
		t.Fatalf("(c) max offset = %d, want %d (feedLen %d - rows %d)", m.projects.logsOffset, wantMax, feedLen, rows)
	}
	body := feedBodyLines(t, m.projects.View(), eventsH)
	if len(body) != rows {
		t.Fatalf("(c) expected %d visible rows, got %d", rows, len(body))
	}
	for i, line := range body {
		if line == "" {
			t.Fatalf("(c) blank row at max scroll, row %d of %d: body = %q", i, len(body), body)
		}
	}
	t.Logf("(c) feedLen=%d rows=%d max offset=%d, all %d rows filled", feedLen, rows, m.projects.logsOffset, len(body))
}

// TestShiftRightPageOverlapsByOneLine pins the rule: the page-scroll
// magnitude (eventsPageSize) is rows-1, not rows or rows+1. Asserting only
// that a previously-hidden line became visible would pass for any of those
// three magnitudes, so this pins the overlap precisely — the LAST visible
// event line before a shift+right page is the FIRST visible event line
// after it — catching an off-by-one in either direction.
func TestShiftRightPageOverlapsByOneLine(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	for i := 0; i < 40; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("Task %02d", i))
	}
	_, _, eventsH, _ := projectPaneSplitHeights(m.projects.contentHeight)

	before := feedBodyLines(t, m.projects.View(), eventsH)
	update(t, m, "shift+right")
	after := feedBodyLines(t, m.projects.View(), eventsH)

	lastBefore := before[len(before)-1]
	firstAfter := after[0]
	if lastBefore == "" || firstAfter == "" {
		t.Fatalf("expected non-blank overlap lines, got before-last %q / after-first %q", lastBefore, firstAfter)
	}
	if lastBefore != firstAfter {
		t.Fatalf("page overlap broken: last visible line before shift+right = %q, first visible line after = %q", lastBefore, firstAfter)
	}
}

// TestFeedOffsetResetsOnConfirmYesProjectRemoval covers the second
// logsOffset reset site (confirmYes, projects.go), the one
// TestFeedOffsetResetsOnProjectSwitch above does not exercise. Uses
// SetProjectName rather than seeded tasks to build up feed length, since
// requestRemoveProject refuses to open the confirm prompt while the project
// still has tasks.
func TestFeedOffsetResetsOnConfirmYesProjectRemoval(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	for i := 0; i < 20; i++ {
		if err := m.store.SetProjectName("ATM", fmt.Sprintf("Acme %d", i), m.actor); err != nil {
			t.Fatalf("SetProjectName: %v", err)
		}
	}
	update(t, m, "shift+down")
	update(t, m, "shift+down")
	if m.projects.logsOffset == 0 {
		t.Fatal("setup: offset should be non-zero before removal")
	}

	update(t, m, "x") // requestRemoveProject: no tasks, so this opens the confirm prompt
	if m.confirm != confirmRemoveProject {
		t.Fatalf("expected confirmRemoveProject prompt, got confirm=%v", m.confirm)
	}
	update(t, m, "y") // confirmYes
	if m.projectScope != "" {
		t.Fatalf("setup: expected projectScope cleared after removal, got %q", m.projectScope)
	}
	if m.projects.logsOffset != 0 {
		t.Fatalf("confirmYes should reset logsOffset, got %d", m.projects.logsOffset)
	}
}

func TestEventsFeedBoxBodyIsLeftAndTopAligned(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	seedTask(t, m, "ATM", "Fix cache")
	lines := strings.Split(m.projects.View(), "\n")
	top := -1
	for i, line := range lines {
		if strings.Contains(line, "Recent Events") {
			top = i
			break
		}
	}
	if top < 0 {
		t.Fatal("no events box")
	}
	// Top-aligned: the column header sits directly under the top border
	// (R3-2), and the first event row directly under that — no blank rows
	// floating the body toward the vertical middle.
	if header := stripANSI(lines[top+1]); !strings.Contains(header, "ACTOR") {
		t.Fatalf("row directly under the top border is not the column header: %q", header)
	}
	first := stripANSI(lines[top+2])
	if !strings.Contains(first, "●") {
		t.Fatalf("first body row has no event glyph (not top-aligned): %q", first)
	}
	// Left-aligned: the graph glyph sits immediately after the left border,
	// not pushed rightward by centering. Rune (display-column) index, not
	// byte index: "│" alone is 3 UTF-8 bytes, so a byte-offset diff between
	// adjacent "│●" would always read as 3 and could never satisfy a small
	// column-distance threshold even when genuinely adjacent.
	runes := []rune(first)
	bar, dot := -1, -1
	for i, r := range runes {
		switch r {
		case '│':
			if bar == -1 {
				bar = i
			}
		case '●':
			if dot == -1 {
				dot = i
			}
		}
	}
	if dot-bar > 2 {
		t.Fatalf("event glyph is %d cols from the left border (centered, not left-aligned): %q", dot-bar, first)
	}
}

// TestRenderEventsFeedClampFollowsWindowGrowth pins the rule: the render
// path's local offset clamp (eventsFeedBody) bounds against len(feed)-rows —
// the same window-aware rule scrollEventsFeed enforces on p.logsOffset — not
// len(feed)-1. Growing the terminal grows `rows` without moving logsOffset
// (nothing but a shift+arrow keypress ever touches that field); a stale,
// too-high offset under a len(feed)-1 bound would read the window past the
// end of the feed and blank-pad the shortfall, even though enough older
// events exist to fill the taller box. This drives the resize purely through
// contentHeight/View() — no key press — so a regression to the wrong local
// clamp shows up as blank rows here.
func TestRenderEventsFeedClampFollowsWindowGrowth(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	// Plenty of events: comfortably more than either window below needs to
	// fill completely.
	for i := 0; i < 40; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("Task %02d", i))
	}

	// Small window: scroll all the way to its own max offset.
	m.projects.contentHeight = 30
	_, _, smallEventsH, _ := projectPaneSplitHeights(m.projects.contentHeight)
	smallRows := eventsFeedVisibleRows(smallEventsH)
	for i := 0; i < 200; i++ {
		update(t, m, "shift+down")
	}
	feedLen := m.projects.feedLen()
	wantSmallMax := feedLen - smallRows
	if m.projects.logsOffset != wantSmallMax {
		t.Fatalf("setup: offset at small-window max = %d, want %d", m.projects.logsOffset, wantSmallMax)
	}

	// Grow the terminal (no key press): a bigger window means more visible
	// rows, but logsOffset does not move on its own.
	m.projects.contentHeight = 63
	_, _, bigEventsH, _ := projectPaneSplitHeights(m.projects.contentHeight)
	bigRows := eventsFeedVisibleRows(bigEventsH)
	if bigRows <= smallRows {
		t.Fatalf("setup: grown window (%d rows) is not bigger than the original (%d rows)", bigRows, smallRows)
	}
	if feedLen < bigRows {
		t.Fatalf("setup: feedLen %d shorter than the grown window %d — box would legitimately show blanks", feedLen, bigRows)
	}

	preRenderOffset := m.projects.logsOffset
	body := feedBodyLines(t, m.projects.View(), bigEventsH)
	if m.projects.logsOffset != preRenderOffset {
		t.Fatalf("render path wrote p.logsOffset: was %d, now %d (render must stay pure)", preRenderOffset, m.projects.logsOffset)
	}
	if len(body) != bigRows {
		t.Fatalf("expected %d visible rows in the grown box, got %d", bigRows, len(body))
	}
	for i, line := range body {
		if line == "" {
			t.Fatalf("blank row %d of %d after growing the window with no key press: body = %q", i, len(body), body)
		}
	}
}

// feedBoxRows returns the events box's inner content rows from a rendered
// pane view: everything between the box's top border (located by its title)
// and its bottom border, ANSI-stripped, with the pane's own left indentation
// and the box's two "│" border runes removed but NOTHING else trimmed — so a
// caller can compare display-column offsets between the header row and the
// event rows below it. Row 0 is the column header (revision 3, R3-2); rows
// 1.. are the event lines. feedBodyLines is the trimmed, header-skipping view
// of the same rows.
func feedBoxRows(t *testing.T, view string) []string {
	t.Helper()
	lines := strings.Split(view, "\n")
	top := -1
	for i, line := range lines {
		if strings.Contains(line, "Recent Events") {
			top = i
			break
		}
	}
	if top < 0 {
		t.Fatalf("no events box\n--- view ---\n%s", view)
	}
	var rows []string
	for i := top + 1; i < len(lines); i++ {
		r := []rune(stripANSI(lines[i]))
		lo := 0
		for lo < len(r) && r[lo] == ' ' {
			lo++
		}
		hi := len(r)
		for hi > lo && r[hi-1] == ' ' {
			hi--
		}
		if hi-lo < 2 || r[lo] != '│' {
			break // bottom border (╰…╯) or the end of the box
		}
		rows = append(rows, string(r[lo+1:hi-1]))
	}
	if len(rows) == 0 {
		t.Fatalf("events box has no content rows\n--- view ---\n%s", view)
	}
	return rows
}

// runeIndex is strings.Index in DISPLAY columns rather than bytes: a feed row
// opens with the 3-byte "●" gutter glyph and its subject column may hold the
// 3-byte "–", so a byte offset would not line up with the header's own ASCII
// labels even when the two columns are genuinely aligned.
func runeIndex(s, sub string) int {
	i := strings.Index(s, sub)
	if i < 0 {
		return -1
	}
	return len([]rune(s[:i]))
}

// boxedFeedView renders the pane at the given terminal size with one seeded
// project and task, asserting the events section is boxed there (the feed's
// column header and the box rows feedBoxRows reads both assume the boxed
// framing).
func boxedFeedView(t *testing.T, w, h int, title string) (*Model, string) {
	t.Helper()
	m := newTestModel(t)
	m.SetSize(w, h)
	_, _, _, summaryH := projectPaneSplitHeights(m.projects.contentHeight)
	if !summaryChartsBoxed(summaryH) {
		t.Fatalf("setup: expected the events section boxed at %dx%d", w, h)
	}
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	seedTask(t, m, "ATM", title)
	return m, m.projects.View()
}

// TestEventFeedShowsFullActorIdentity pins R3-1: the actor column carries the
// complete persona@agent:model string, not revision 2's 8-cell "dev@clau"
// abbreviation, which collapsed developer@claude and developer@codex onto the
// same text and hid the model entirely.
func TestEventFeedShowsFullActorIdentity(t *testing.T) {
	_, view := boxedFeedView(t, 200, 40, "Fix cache")
	rows := feedBoxRows(t, view)
	joined := strings.Join(rows, "\n")
	mustContain(t, joined, testActor) // developer@claude:test, whole
	if strings.Contains(joined, "dev@clau") {
		t.Fatalf("actor is still abbreviated\n--- rows ---\n%s", joined)
	}
}

// TestActorColumnWidthStableWhileScrolling pins R3-1's sizing rule: the actor
// column is as wide as the widest actor across the WHOLE bounded feed, not
// across the visible window, so neither the column nor the header shifts as
// the user scrolls. The long actor here is seeded oldest, so it is off the
// initial (newest-first) window and only enters it after paging — a
// window-relative width would widen the column exactly then.
func TestActorColumnWidthStableWhileScrolling(t *testing.T) {
	const longActor = "developer@opencode:deepseek-v4-pro"
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	seedTaskAsActor(t, m, "ATM", "Oldest task", longActor)
	for i := 0; i < 40; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("Task %02d", i))
	}

	before := feedBoxRows(t, m.projects.View())
	if strings.Contains(strings.Join(before, "\n"), longActor) {
		t.Fatal("setup: the long actor is already in the first window — seed more events")
	}
	actorCol := func(row string) int { return runeIndex(row, "developer@") }
	wantCol := actorCol(before[1])
	if wantCol < 0 {
		t.Fatalf("no actor in the first event row: %q", before[1])
	}

	// Scroll until the long actor's own event enters the window rather than
	// scrolling to the very bottom: the oldest events in the feed are the
	// project's vocabulary seeding, which sits below it.
	var after []string
	for i := 0; i < 200 && after == nil; i++ {
		update(t, m, "shift+down")
		rows := feedBoxRows(t, m.projects.View())
		if strings.Contains(strings.Join(rows, "\n"), longActor) {
			after = rows
		}
	}
	if after == nil {
		t.Fatalf("scrolling never brought %q into the window", longActor)
	}

	if before[0] != after[0] {
		t.Fatalf("header moved while scrolling:\nbefore %q\nafter  %q", before[0], after[0])
	}
	for _, rows := range [][]string{before, after} {
		for i, row := range rows[1:] {
			if got := actorCol(row); got != wantCol {
				t.Fatalf("actor column starts at %d, want %d (row %d: %q)", got, wantCol, i, row)
			}
		}
	}
}

// TestEventsFeedHeaderMatchesRenderedColumns pins R3-2: one muted header row
// sits directly under the box's top border, labels are at the same display
// columns as the values beneath them, and only the columns actually rendered
// at the current width are labelled — at a terminal narrow enough to drop the
// age column (feedAgeMinWidth) the header must not claim an AGE column.
func TestEventsFeedHeaderMatchesRenderedColumns(t *testing.T) {
	for _, tc := range []struct {
		term    int
		wantAge bool
	}{
		{200, true},
		{160, true},
		{120, true},
		{80, false},
	} {
		_, view := boxedFeedView(t, tc.term, 40, "Fix cache")
		rows := feedBoxRows(t, view)
		header := rows[0]
		if !strings.Contains(header, "ID") || !strings.Contains(header, "TASK") {
			t.Fatalf("term %d: header does not label the id and task columns: %q", tc.term, header)
		}
		if got := strings.Contains(header, "AGE"); got != tc.wantAge {
			t.Fatalf("term %d: header AGE label = %v, want %v: %q", tc.term, got, tc.wantAge, header)
		}
		// The labels sit at the same offsets as the values below them.
		id := regexp.MustCompile(`[0-9a-f]{7}`).FindStringIndex(rows[1])
		if id == nil {
			t.Fatalf("term %d: no event id in the first event row: %q", tc.term, rows[1])
		}
		idCol := len([]rune(rows[1][:id[0]]))
		if got := runeIndex(header, "ID"); got != idCol {
			t.Fatalf("term %d: ID label at column %d, id value at column %d\nheader %q\nrow    %q", tc.term, got, idCol, header, rows[1])
		}
		if got, want := runeIndex(header, "TASK"), idCol+feedIDWidth+1; got != want {
			t.Fatalf("term %d: TASK label at column %d, want %d\nheader %q", tc.term, got, want, header)
		}
	}
}

// TestHeaderRowAccountedInVisibleRows pins R3-2's hazard: the header costs one
// content row, so eventsFeedVisibleRows — the single source of the visible row
// count for the renderer, the scroll clamp and the page magnitude — must
// exclude it. The box must therefore hold exactly one header row plus
// eventsFeedVisibleRows event rows, and still be exactly eventsH lines tall.
func TestHeaderRowAccountedInVisibleRows(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	for i := 0; i < 40; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("Task %02d", i))
	}
	_, _, eventsH, _ := projectPaneSplitHeights(m.projects.contentHeight)
	rows := feedBoxRows(t, m.projects.View())
	want := eventsFeedVisibleRows(eventsH) + 1
	if len(rows) != want {
		t.Fatalf("box holds %d content rows, want %d (1 header + %d event rows)", len(rows), want, eventsFeedVisibleRows(eventsH))
	}
	if strings.Contains(rows[0], "●") {
		t.Fatalf("row directly under the top border is an event, not the header: %q", rows[0])
	}
	block := strings.Split(m.projects.renderEventsFeed(eventsH, true), "\n")
	if len(block) != eventsH {
		t.Fatalf("boxed feed is %d lines tall, want eventsH=%d", len(block), eventsH)
	}
}

// TestCompactFeedHeaderAndRowAccounting pins the SAME hazard as
// TestHeaderRowAccountedInVisibleRows, but for the compact (unboxed) framing —
// the clause R3-5 states as "in BOTH framings" precisely because revision 2
// shipped a stranded-events bug of the framing-mismatch shape, and revision
// 3's header row is a fresh chance to reintroduce it. The compact form must
// also carry exactly one column header plus eventsFeedVisibleRows event rows,
// with no stranded blank when the feed overflows its window.
func TestCompactFeedHeaderAndRowAccounting(t *testing.T) {
	const width = 80
	// Find a height where the events section renders compact (unboxed) and its
	// slot is tall enough to render at all — the same boxed/unboxed threshold
	// TestEventsFeedFramingMatchesPersonaChart sweeps, read here off
	// projectPaneSplitHeights + summaryChartsBoxed rather than the rendered body.
	var m *Model
	var eventsH int
	for h := 8; h <= 60; h++ {
		mm := newTestModel(t)
		mm.SetSize(width, h)
		_, _, eh, summaryH := projectPaneSplitHeights(mm.projects.contentHeight)
		if eh >= 4 && !summaryChartsBoxed(summaryH) {
			m, eventsH = mm, eh
			break
		}
	}
	if m == nil {
		t.Fatal("no compact height with a rendered events slot in heights 8..60 at width 80")
	}
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	for i := 0; i < 40; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("Task %02d", i))
	}

	rows := eventsFeedVisibleRows(eventsH)
	if got := m.projects.feedLen(); got <= rows {
		t.Fatalf("feed (%d events) does not overflow the %d-row window; the no-stranded-blank check needs an overflowing feed", got, rows)
	}

	// The compact form must not be boxed at this height — otherwise the setup
	// picked the wrong framing and the rest of the assertion is meaningless.
	view := m.projects.View()
	lines := strings.Split(view, "\n")
	capIdx := -1
	for i, line := range lines {
		if strings.Contains(stripANSI(line), "Recent Events") {
			capIdx = i
			break
		}
	}
	if capIdx < 0 {
		t.Fatalf("no Recent Events caption in the compact view\n--- view ---\n%s", view)
	}
	if strings.ContainsAny(stripANSI(lines[capIdx]), "╭╮") {
		t.Fatalf("setup picked a boxed height, not compact: %q", stripANSI(lines[capIdx]))
	}

	// One header row directly under the caption, then exactly `rows` event rows,
	// all populated — no blank line the overflowing feed had room to fill.
	header := stripANSI(lines[capIdx+1])
	if !strings.Contains(header, "ID") || !strings.Contains(header, "TASK") {
		t.Fatalf("row under the caption is not the column header: %q", header)
	}
	if strings.Contains(header, "●") {
		t.Fatalf("row under the caption is an event, not the header: %q", header)
	}
	for i := 0; i < rows; i++ {
		row := stripANSI(lines[capIdx+2+i])
		if !strings.Contains(row, "●") {
			t.Fatalf("compact event row %d is blank while the feed overflows (stranded window): %q", i, row)
		}
	}
}

// TestMessageTruncatesBeforeActor pins R3-3's reordered degradation: the
// message yields first, down to feedMessageMinWidth, and only then does the
// actor start truncating. At 160 columns the actor is whole and the message
// is already clipped; at 120 the message is at its floor and the actor has
// begun to truncate.
func TestMessageTruncatesBeforeActor(t *testing.T) {
	_, wide := boxedFeedView(t, 160, 40, "Fix the flaky cache")
	row := feedBoxRows(t, wide)[1]
	if !strings.Contains(row, testActor) {
		t.Fatalf("160 columns: actor should still be whole: %q", row)
	}
	if !strings.Contains(row, "...") {
		t.Fatalf("160 columns: message should already be truncated: %q", row)
	}

	_, narrow := boxedFeedView(t, 120, 40, "Fix the flaky cache")
	row = feedBoxRows(t, narrow)[1]
	if strings.Contains(row, testActor) {
		t.Fatalf("120 columns: actor should be truncated, not whole: %q", row)
	}
	if !strings.Contains(row, "developer...") {
		t.Fatalf("120 columns: actor should truncate with an ellipsis: %q", row)
	}
}

// TestEventIDPresentAtAllWidths pins R3-3's top priority: the hash id never
// drops. Revision 1 yielded it below feedIDMinWidth (60); the user ranked it
// above the message, so it must survive every width the pane renders at,
// including the narrowest.
func TestEventIDPresentAtAllWidths(t *testing.T) {
	hex := regexp.MustCompile(`[0-9a-f]{7}`)
	for _, term := range []int{80, 100, 120, 160, 200, 240} {
		_, view := boxedFeedView(t, term, 40, "Fix cache")
		rows := feedBoxRows(t, view)
		if !hex.MatchString(rows[1]) {
			t.Fatalf("term %d: no short event id in the first event row: %q", term, rows[1])
		}
	}
}
