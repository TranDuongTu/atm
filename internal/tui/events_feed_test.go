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

func TestEventFeedActor(t *testing.T) {
	cases := []struct{ in, want string }{
		{"developer@claude:opus-4.8", "dev@clau"},
		{"admin@tui:unset", "adm@tui"},
		{"manager@ollama:unset", "man@olla"},
		{"default", "def"}, // v1-era actor without @
	}
	for _, c := range cases {
		if got := eventFeedActor(c.in); got != c.want {
			t.Errorf("eventFeedActor(%q) = %q, want %q", c.in, got, c.want)
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

func TestProjectPaneSplitHeightsThreeWay(t *testing.T) {
	cases := []struct {
		total, list, events, summary int
	}{
		{27, 8, 9, 10},
		{10, 3, 0, 7}, // events slot would be 3 (<4): collapses to old 30/70
		{2, 1, 0, 1},
		{1, 1, 0, 0},
		{0, 0, 0, 0},
	}
	for _, c := range cases {
		l, e, s := projectPaneSplitHeights(c.total)
		if l != c.list || e != c.events || s != c.summary {
			t.Errorf("split(%d) = (%d,%d,%d), want (%d,%d,%d)", c.total, l, e, s, c.list, c.events, c.summary)
		}
	}
}

func TestRecentEventsFeedRendersDigestLines(t *testing.T) {
	m := newTestModel(t)
	// 200-wide (an existing convention for tests that need to see full
	// rendered content, e.g. TestProjectDetailDashboardSections): at 120 the
	// projects pane's inner width is 46, and the fixed column budget (gutter
	// + id 7 + subject 7 + actor 8 + separators + age 3 = 31 columns) leaves
	// only 15 columns for the message — too narrow to fit the full digest
	// text this test asserts on without truncation.
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	seedTask(t, m, "ATM", "Fix the cache", "ATM:status:open")
	body := m.projects.View()
	mustContain(t, body, "Recent Events")
	mustContain(t, body, `created "Fix the cache"`)
	mustContain(t, body, "dev@clau") // testActor developer@claude:test
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

func TestEventFeedLineDegradesOnNarrowPane(t *testing.T) {
	m := newTestModel(t)
	m.projectScope = "ATM"
	p := &m.projects
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	e := core.LogEntry{
		ID:      "sha256:84fbf586004a",
		Action:  "comment.created",
		At:      now.Add(-time.Hour),
		Actor:   "developer@claude:test",
		Subject: core.Subject{Kind: "task", ID: "ATM-90171b"},
	}
	lanes := []rune{'●'}
	// eventFeedLine sizes to the width passed in directly (the events box's
	// inner width, computed by the caller) rather than p.width, so these
	// cases exercise that parameter, not the model field.
	width := 60 // full budget: id and age both present
	line := p.eventFeedLine(e, lanes, 1, width, now, true)
	mustContain(t, line, "84fbf58")
	mustContain(t, line, "1h")
	width = 46 // below 60: id column drops, age stays
	line = p.eventFeedLine(e, lanes, 1, width, now, true)
	mustNotContain(t, line, "84fbf58")
	mustContain(t, line, "1h")
	width = feedAgeMinWidth // exactly at the age threshold: age still renders
	line = p.eventFeedLine(e, lanes, 1, width, now, true)
	mustNotContain(t, line, "84fbf58")
	mustContain(t, line, "1h")
	width = 28 // below 30: age drops too
	line = p.eventFeedLine(e, lanes, 1, width, now, true)
	mustNotContain(t, line, "84fbf58")
	mustNotContain(t, line, "1h")
}

// TestRecentEventsFeedAt120Columns pins the feed's behavior at the terminal
// width users see most often: 120 columns, where the projects pane's inner
// width is 46 (see the width comment on TestRecentEventsFeedRendersDigestLines).
// At 46 the id column (drops below 60) is gone, so its budget goes to the
// message instead; this asserts that trade actually lands — the short id is
// absent, a short task title's digest message survives whole, and no
// rendered feed line overruns the pane.
func TestRecentEventsFeedAt120Columns(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	seedTask(t, m, "ATM", "Fix cache", "ATM:status:open")
	body := m.projects.View()
	mustContain(t, body, "Recent Events")
	mustContain(t, body, `created "Fix cache"`)
	if regexp.MustCompile(`\bsha256:|[0-9a-f]{7}\b`).MatchString(body) {
		t.Fatalf("feed shows a short event id at 120 columns (want it dropped)\n--- body ---\n%s", body)
	}
	for _, line := range strings.Split(body, "\n") {
		if w := lipgloss.Width(line); w > m.projects.width {
			t.Fatalf("rendered line exceeds pane inner width %d (got %d): %q", m.projects.width, w, line)
		}
	}
}

// TestEventFeedCapsAtMaxFeedEvents pins the amendment to Task 4: the feed
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

// TestRecentEventsFeedScrollRevealsNewContent is the modeless rewrite of the
// old TestRecentEventsFeedScrollRendersNewContent (ATM-793b19 Task 8): the
// underlying property survives — scrolling the feed surfaces a digest line
// that was off the initial tail window — but the mechanism is now
// shift+right (a page) rather than L then ], and there is no cursor row to
// highlight (R2-3: logsOffset is a pure viewport offset). Also asserts the
// negative: no reverse-video escape appears anywhere inside the events box,
// scoped to the box's own rows so the project list's own RowCursor-styled
// selection higher up in the same pane can't produce a false pass.
func TestRecentEventsFeedScrollRevealsNewContent(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
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
	_, eventsH, _ := projectPaneSplitHeights(m.projects.contentHeight)
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
	// feedLen()-1, which the old buggy clamp also satisfied. See
	// TestScrollEventsFeedClampsToVisibleWindow for the dedicated,
	// multi-regime pin of this bound; this assertion keeps this test's own
	// heavy shift+right hammering (a different call path/contentHeight than
	// that test's fixed setup) from silently regressing to the stale bound.
	_, eventsH, _ := projectPaneSplitHeights(m.projects.contentHeight)
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
// original motivation for boxing the feed at all, just inverted. At a small
// pane height (the classic 80x24 terminal, where the persona chart's own
// "4 lines and up" rule keeps it unboxed) the feed must be unboxed too; at a
// large height (where the persona chart boxes) the feed must box too. Keyed
// on both sections' actual rendered output in the same render so it cannot
// pass if only one of them changes.
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
	render := func(t *testing.T, w, h int) (feedLine, personaLine string) {
		t.Helper()
		m := newTestModel(t)
		m.SetSize(w, h)
		seedProject(t, m, "ATM", "Acme Task Manager")
		update(t, m, "s")
		seedTask(t, m, "ATM", "Fix cache")
		body := m.projects.View()
		feedLine = findLine(body, "Recent Events")
		personaLine = findLine(body, "activity by persona")
		if feedLine == "" || personaLine == "" {
			t.Fatalf("missing a section at %dx%d\n--- body ---\n%s", w, h, body)
		}
		return feedLine, personaLine
	}

	smallFeed, smallPersona := render(t, 80, 24)
	if isBoxed(smallPersona) {
		t.Fatalf("setup: expected the persona chart unboxed at 80x24, got: %q", smallPersona)
	}
	if isBoxed(smallFeed) {
		t.Fatalf("events feed is boxed while the persona chart is unboxed at 80x24 — two visual languages: %q vs %q", smallFeed, smallPersona)
	}

	largeFeed, largePersona := render(t, 120, 40)
	if !isBoxed(largePersona) {
		t.Fatalf("setup: expected the persona chart boxed at 120x40, got: %q", largePersona)
	}
	if !isBoxed(largeFeed) {
		t.Fatalf("events feed is unboxed while the persona chart is boxed at 120x40 — two visual languages: %q vs %q", largeFeed, largePersona)
	}
}

// feedBodyLines extracts the events box's rows visible rows from a rendered
// pane view (ANSI-stripped, trimmed, and stripped of the box's own left/right
// "│" border — renderChartBox wraps every body row, blank or not, in exactly
// one border rune each side, so a caller checking a row for blankness would
// otherwise always see a non-empty "│ … │" string), locating the box by its
// title row the same way TestEventsFeedBoxBodyIsLeftAndTopAligned and
// TestRecentEventsFeedScrollRevealsNewContent do. Slices off exactly one rune
// per side rather than strings.Trim, which would also eat a leading "│" that
// is a genuine graph-gutter lane glyph, not border.
func feedBodyLines(t *testing.T, view string, eventsH int) []string {
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
		t.Fatal("no events box")
	}
	rows := eventsFeedVisibleRows(eventsH)
	body := make([]string, 0, rows)
	for i := top + 1; i < top+1+rows && i < len(lines); i++ {
		trimmed := []rune(strings.TrimSpace(stripANSI(lines[i])))
		if len(trimmed) >= 2 {
			trimmed = trimmed[1 : len(trimmed)-1]
		}
		body = append(body, strings.TrimSpace(string(trimmed)))
	}
	return body
}

// TestScrollEventsFeedClampsToVisibleWindow pins the Important-1 review fix:
// scrollEventsFeed's upper clamp is feedLen() minus the visible row count
// (R2-3), not feedLen()-1. The stale clamp let a feed that didn't overflow
// its window scroll anyway (pushing the newest event off the top and
// rendering blank rows), and left one event stranded above a column of
// blanks at maximum scroll on any overflowing feed. This exercises all three
// regimes named in the review: under, exactly-one-over, and well-over the
// window.
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
	_, eventsH, _ := projectPaneSplitHeights(m.projects.contentHeight)
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

// TestShiftRightPageOverlapsByOneLine pins the Important-2 review fix: the
// page-scroll magnitude (eventsPageSize) is rows-1, not rows or rows+1. The
// only prior coverage asserted that a previously-hidden line became visible,
// which any of those three magnitudes would satisfy. This pins the overlap
// precisely — the LAST visible event line before a shift+right page is the
// FIRST visible event line after it — so a regression to the stale
// `eventsH - 1` magnitude (or an off-by-one the other way) fails the test.
func TestShiftRightPageOverlapsByOneLine(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	for i := 0; i < 40; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("Task %02d", i))
	}
	_, eventsH, _ := projectPaneSplitHeights(m.projects.contentHeight)

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
	// Top-aligned: the row directly under the top border carries an event.
	first := stripANSI(lines[top+1])
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

// TestRenderEventsFeedClampFollowsWindowGrowth pins the Task 9 review fix
// (A1): renderEventsFeed's render-time local clamp bounds the offset against
// len(feed)-rows (the same window-aware rule scrollEventsFeed enforces on
// p.logsOffset), not len(feed)-1. Before the fix, growing the terminal
// (which grows `rows` without moving logsOffset — nothing but a shift+arrow
// keypress ever touches that field) could leave a stale, too-high offset
// under the old len(feed)-1 bound: the window would then read past the end
// of the feed and blank-pad the shortfall, even though enough older events
// exist to fill the taller box. This drives the resize purely through
// contentHeight/View() — no key press — so any regression back to the
// len(feed)-1 local clamp shows up as blank rows here.
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
	_, smallEventsH, _ := projectPaneSplitHeights(m.projects.contentHeight)
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
	_, bigEventsH, _ := projectPaneSplitHeights(m.projects.contentHeight)
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
