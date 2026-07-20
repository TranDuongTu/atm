package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"atm/internal/core"
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
	// No selection: muted placeholder, no digest lines.
	mustContain(t, m.projects.View(), "select a project to see events")
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
	p.width = 46 // full budget: id and age both present
	line := p.eventFeedLine(e, lanes, 1, now, true)
	mustContain(t, line, "84fbf58")
	mustContain(t, line, "1h")
	p.width = 34 // below 36: id column drops, age stays
	line = p.eventFeedLine(e, lanes, 1, now, true)
	mustNotContain(t, line, "84fbf58")
	mustContain(t, line, "1h")
	p.width = 28 // below 30: age drops too
	line = p.eventFeedLine(e, lanes, 1, now, true)
	mustNotContain(t, line, "84fbf58")
	mustNotContain(t, line, "1h")
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
}
