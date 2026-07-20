package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"atm/internal/core"
	"github.com/charmbracelet/lipgloss"
)

// The Recent Events feed (ATM-793b19) renders the selected project's event
// log git-log-style: one event per line, newest first, with a commit-graph
// gutter derived from the v2 parents DAG. This file holds the formatting
// helpers over core.LogEntry (digest wording, graph lanes, column layout)
// alongside the pane's own render and key-handling methods (renderEventsFeed,
// feedLen, scrollEventsFeed, eventFeedLine); projects.go owns the surrounding
// pane split. Navigation is modeless (revision 2, R2-2): the Shift modifier
// alone decides whether arrows drive the feed or the project list, mirroring
// tasksModel's board-thumbnail pattern in tasks_list.go — there is no
// subfocus to route through.

// shortEventID renders the 7-char short form of a "sha256:…" event id; a v1
// entry (no id) renders empty and the column shows blank.
func shortEventID(id string) string {
	h := strings.TrimPrefix(id, "sha256:")
	if len(h) > 7 {
		h = h[:7]
	}
	return h
}

// compactAge is relTime's terser cousin for the feed's right-aligned age
// column: no " ago" suffix, so the column stays ≤4 cells.
func compactAge(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// eventFeedActor abbreviates the actor grammar persona@agent:model to a
// ≤8-cell "per@agen": persona to 3 runes, agent to 4, model dropped.
func eventFeedActor(actor string) string {
	persona, rest, hasAgent := strings.Cut(actor, "@")
	agent, _, _ := strings.Cut(rest, ":")
	p := []rune(persona)
	if len(p) > 3 {
		p = p[:3]
	}
	if !hasAgent || agent == "" {
		return string(p)
	}
	a := []rune(agent)
	if len(a) > 4 {
		a = a[:4]
	}
	return string(p) + "@" + string(a)
}

// eventFeedSubject renders the subject column: the TASK the event touched,
// as its alias with the project-code prefix stripped (the feed is scoped to
// one project, so the prefix carries no information). A comment's alias is
// <task-alias>-c<hex>; the trailing comment segment is cut so the column
// names the task. Project/label subjects render "–".
func eventFeedSubject(e core.LogEntry, projectCode string) string {
	switch e.Subject.Kind {
	case "task", "comment":
		alias := strings.TrimPrefix(e.Subject.ID, projectCode+"-")
		if e.Subject.Kind == "comment" {
			if i := strings.LastIndex(alias, "-c"); i > 0 {
				alias = alias[:i]
			}
		}
		return alias
	}
	return "–"
}

// feedLabel renders a label for the feed: the project prefix is always
// stripped, and the status facet — the dominant, unambiguous one — renders
// as its bare value ("done"). Other facets keep their name ("type:bug").
func feedLabel(label, projectCode string) string {
	l := strings.TrimPrefix(label, projectCode+":")
	if v, ok := strings.CutPrefix(l, "status:"); ok && v != "" {
		return v
	}
	return l
}

// eventDigestMessage renders the digest wording for one event. Payload
// shapes differ between native-v2 events ({"label": …}) and v1-upgraded
// snapshots (a full entity dump), so every payload field is optional and
// absence degrades to the bare verb.
func eventDigestMessage(e core.LogEntry, projectCode string) string {
	var p struct {
		Title      string `json:"title"`
		Name       string `json:"name"`
		Label      string `json:"label"`
		Capability string `json:"capability"`
	}
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &p)
	}
	switch e.Action {
	case "task.created":
		if p.Title != "" {
			return fmt.Sprintf("created %q", p.Title)
		}
		return "created"
	case "task.title-changed":
		if p.Title != "" {
			return fmt.Sprintf("retitled %q", p.Title)
		}
		return "retitled"
	case "task.description-changed":
		return "description edited"
	case "task.label-added", "task.label-removed", "comment.label-added", "comment.label-removed":
		sign := "+"
		if strings.HasSuffix(e.Action, "-removed") {
			sign = "−"
		}
		prefix := ""
		if strings.HasPrefix(e.Action, "comment.") {
			prefix = "comment "
		}
		if p.Label != "" {
			return prefix + sign + feedLabel(p.Label, projectCode)
		}
		return prefix + sign + "label"
	case "task.removed":
		return "removed"
	case "task.meta-changed":
		return "meta"
	case "comment.created":
		return "commented"
	case "comment.body-changed":
		return "comment edited"
	case "comment.removed":
		return "comment removed"
	case "label.upserted":
		if e.Subject.Name != "" {
			return "label " + strings.TrimPrefix(e.Subject.Name, projectCode+":")
		}
		return "label upserted"
	case "label.removed":
		if e.Subject.Name != "" {
			return "−label " + strings.TrimPrefix(e.Subject.Name, projectCode+":")
		}
		return "−label"
	case "project.created":
		return "project created"
	case "project.name-changed":
		if p.Name != "" {
			return fmt.Sprintf("renamed %q", p.Name)
		}
		return "renamed"
	case "project.removed":
		return "project removed"
	case "project.capability-enabled":
		if p.Capability != "" {
			return "+" + p.Capability
		}
		return "+capability"
	case "project.capability-disabled":
		if p.Capability != "" {
			return "−" + p.Capability
		}
		return "−capability"
	}
	return e.Action
}

// feedIDMinWidth and feedAgeMinWidth are the events-box inner-width
// thresholds below which eventFeedLine drops the id column, then the age
// column, to give the space to the message column. See eventFeedLine's doc
// comment for why the id yields first.
const (
	feedIDMinWidth  = 60
	feedAgeMinWidth = 30
)

// maxGraphLanes caps the commit-graph gutter width. Local histories are
// linear (one lane); lanes >1 appear only after a sync merges concurrent
// replicas, and 3 parallel branches is already an extraordinary display.
const maxGraphLanes = 3

// eventGraphRows assigns commit-graph lanes to entries rendered NEWEST-FIRST
// (entries[0] is the newest event). Walking downward, each lane "awaits" an
// event id: the row's event takes the first lane awaiting its id (or opens a
// new lane — a branch tip), other lanes awaiting the same id converge into
// it, and the event's lane then awaits its first parent while extra parents
// open new lanes (a fork, i.e. a merge event read top-down). Overflow beyond
// maxGraphLanes reuses the last lane; extra parents past the cap are simply
// not tracked — the chain re-anchors when an awaited id appears. Entries
// without ids (v1 logs) render a single static lane.
func eventGraphRows(entries []core.LogEntry) [][]rune {
	rows := make([][]rune, len(entries))
	var lanes []string
	for i, e := range entries {
		if e.ID == "" {
			rows[i] = []rune{'●'}
			continue
		}
		lane := -1
		for j, want := range lanes {
			if want == e.ID {
				lane = j
				break
			}
		}
		if lane == -1 {
			if len(lanes) < maxGraphLanes {
				lanes = append(lanes, e.ID)
				lane = len(lanes) - 1
			} else {
				lane = len(lanes) - 1
				lanes[lane] = e.ID
			}
		}
		row := make([]rune, len(lanes))
		for j := range lanes {
			if j == lane {
				row[j] = '●'
			} else {
				row[j] = '│'
			}
		}
		rows[i] = row
		// Converge: every other lane awaiting this id merges into `lane`.
		next := lanes[:0:0]
		for j, want := range lanes {
			if j != lane && want == e.ID {
				continue
			}
			if j == lane {
				lane = len(next)
			}
			next = append(next, want)
		}
		lanes = next
		// Advance: await the first parent; extra parents open lanes (capped).
		// A parentless event is the root — its lane closes.
		if len(e.Parents) == 0 {
			lanes = append(lanes[:lane], lanes[lane+1:]...)
			continue
		}
		lanes[lane] = e.Parents[0]
		for _, parent := range e.Parents[1:] {
			if len(lanes) >= maxGraphLanes {
				break
			}
			lanes = append(lanes, parent)
		}
	}
	return rows
}

// maxFeedEvents bounds the Recent Events feed to the newest N entries.
// ReadLogCached returns the project's ENTIRE history (this store already
// holds ~1800 events on some projects), and renderEventsFeed runs on every
// TUI render frame, including every keystroke. Reversing and graphing the
// full history each frame is an O(total events) allocation pass in the hot
// render loop; capping it makes every downstream step — cursor clamping,
// windowLines, eventGraphRows — operate on a small, fixed-size slice
// instead. An event whose parent falls outside the cap simply leaves a
// lane awaiting an id that never arrives (the same rendering git uses for
// an off-screen parent), which is harmless for ATM's linear history.
const maxFeedEvents = 500

// boundedFeedLen applies the maxFeedEvents cap to a count without touching
// the underlying entries. newestFeedEntries and feedLen both fold through
// this so the cap value cannot drift between the copy-producing path and the
// length-only path.
func boundedFeedLen(n int) int {
	if n > maxFeedEvents {
		return maxFeedEvents
	}
	return n
}

// newestFeedEntries builds the bounded, newest-first feed from ReadLogCached's
// oldest-first entries: the newest maxFeedEvents are its tail, so the cap is
// a slice rather than a full reverse-then-truncate.
func newestFeedEntries(entries []core.LogEntry) []core.LogEntry {
	if n := boundedFeedLen(len(entries)); len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	feed := make([]core.LogEntry, len(entries))
	for i, e := range entries {
		feed[len(entries)-1-i] = e
	}
	return feed
}

// readEventLog reads the selected project's event log and applies the
// tolerance policy shared by renderEventsFeed and feedLen: a v2 integrity
// failure still hands back the recoverable prefix alongside the error (see
// ReadLogCached), so it is tolerated; any other read error is rejected. Both
// callers fold through this one call so the policy lives in exactly one
// place. Callers own the p.m.projectScope == "" short-circuit themselves,
// since they render different placeholders (or return 0) for it.
func (p *projectsModel) readEventLog() (entries []core.LogEntry, ok bool) {
	entries, err := p.m.store.ReadLogCached(p.m.projectScope)
	if err != nil && !core.IsIntegrity(err) {
		return nil, false
	}
	return entries, true
}

// eventsFeedTitle is the box title: the key hint appended the same shape as
// the persona chart's "activity by persona  [P]expand". Shift-↑↓ scrolls the
// feed one line; Shift-←→ (not shown in the title — the box is narrow) pages
// it (ATM-793b19 revision 2, R2-2).
const eventsFeedTitle = "Recent Events  [Shift-↑↓]"

// padFeedLine pads a rendered (possibly ANSI-styled) single line out to
// exactly innerW display columns, fitting it first if it overruns. Used for
// the feed's placeholder states, which renderChartBox otherwise would
// center — see renderEventsFeed's boxed-body doc comment.
func padFeedLine(s string, innerW int) string {
	s = fitLine(s, innerW)
	if pad := innerW - lipgloss.Width(s); pad > 0 {
		s += spaces(pad)
	}
	return s
}

// renderEventsFeed renders the Recent Events section as a bordered box
// (ATM-793b19 revision 2, R2-1), matching the summary charts below it.
// renderChartBox does the drawing; it center-pads each body line and
// top-pads a short body to float it to the vertical middle, both written
// for charts. The feed wants left-and-top alignment instead, achieved
// without touching renderChartBox by handing it a body for which both
// behaviors are arithmetic no-ops: every line — placeholder or populated —
// is exactly chartBoxInnerWidth(p.width) wide (leftPad = (innerW-innerW)/2 =
// 0) and the body always emits exactly height-2 lines, blank-padded at the
// bottom (topPad = (innerH-innerH)/2 = 0). The feed is a pure viewport: it
// windows from p.logsOffset (moved only by scrollEventsFeed), with no
// cursor row and no highlight (R2-3).
func (p *projectsModel) renderEventsFeed(height int) string {
	innerW := chartBoxInnerWidth(p.width)
	rows := height - 2 // box top and bottom border
	if rows < 1 {
		rows = 1
	}
	muted := func(s string) string {
		return padFeedLine(p.m.styles.Muted.Render(s), innerW)
	}
	blank := spaces(innerW)
	// placeholder blank-fills a single-line message out to height-2 lines,
	// same as the populated body below it, so renderChartBox's top-pad is a
	// no-op in every state — a short body would otherwise float to the
	// vertical middle while the populated feed stays top-aligned.
	placeholder := func(msg string) string {
		body := make([]string, 1, rows)
		body[0] = muted(msg)
		for len(body) < rows {
			body = append(body, blank)
		}
		return strings.Join(body, "\n")
	}
	if p.m.projectScope == "" {
		// Unreachable via renderList today: it folds the events section away
		// entirely when scope is empty. Kept as defensive cover for a future caller.
		return p.renderChartBox(eventsFeedTitle, placeholder("select a project to see events"), height)
	}
	entries, ok := p.readEventLog()
	if !ok {
		return p.renderChartBox(eventsFeedTitle, placeholder("events could not be loaded"), height)
	}
	if len(entries) == 0 {
		return p.renderChartBox(eventsFeedTitle, placeholder("no events yet"), height)
	}
	feed := newestFeedEntries(entries)
	// Clamp into a LOCAL offset only: View has a pointer receiver, and
	// writing p.logsOffset here would leak render-time clamping (sized to
	// whichever project is on screen) back into model state, silently
	// resetting a viewport position set on a larger project's feed.
	offset := p.logsOffset
	if offset > len(feed)-1 {
		offset = len(feed) - 1
	}
	if offset < 0 {
		offset = 0
	}
	start := offset
	end := start + rows
	if end > len(feed) {
		end = len(feed)
	}
	// eventGraphRows(feed[:end]) rather than eventGraphRows(feed): row i's
	// lane state depends only on entries[0..i] (the loop walks forward,
	// carrying `lanes` from earlier iterations), so truncating the input to
	// the entries the window can possibly read leaves rows[start:end]
	// byte-identical while skipping the graphing work for everything below
	// the window — up to 500 entries when only a handful are ever shown.
	graph := eventGraphRows(feed[:end])
	laneW := 1
	for i := start; i < end; i++ {
		if len(graph[i]) > laneW {
			laneW = len(graph[i])
		}
	}
	now := core.Now()
	body := make([]string, 0, rows)
	for i := start; i < end; i++ {
		body = append(body, p.eventFeedLine(feed[i], graph[i], laneW, innerW, now, false))
	}
	for len(body) < rows {
		body = append(body, blank)
	}
	return p.renderChartBox(eventsFeedTitle, strings.Join(body, "\n"), height)
}

// feedLen returns the current bounded Recent Events feed length for the
// selected project: the same maxFeedEvents-capped count renderEventsFeed
// computes, reused here so scrollEventsFeed can clamp logsOffset against the
// real feed instead of duplicating the cap arithmetic. Returns 0 when no
// project is selected, and 0 for a hard read error (readEventLog's !ok);
// a v2 integrity failure is tolerated the same way renderEventsFeed
// tolerates it. Deliberately goes through boundedFeedLen rather than
// newestFeedEntries: this runs on every shift+arrow keypress, and only the
// count is needed, so there is no reason to allocate and reverse a copy of
// up to 500 entries just to take its length.
func (p *projectsModel) feedLen() int {
	if p.m.projectScope == "" {
		return 0
	}
	entries, ok := p.readEventLog()
	if !ok {
		return 0
	}
	return boundedFeedLen(len(entries))
}

// eventsPageSize returns the events feed's page-scroll magnitude for
// shift+left/right: the actual visible row count — the events box's height
// minus its two border rows, i.e. what renderEventsFeed windows by — minus
// one more, so a page jump leaves one row of context from the previous page
// (the same overlap convention listPageSize uses for the project list).
// This replaces handleLogsKey's stale `eventsH - 1`, which assumed a
// one-row caption rather than the two-row box border the feed actually
// renders inside; that mismatch made every page jump skip a row instead of
// overlapping one.
func (p *projectsModel) eventsPageSize() int {
	_, eventsH, _ := projectPaneSplitHeights(p.contentHeight)
	page := eventsH - 2 - 1
	if page < 1 {
		page = 1
	}
	return page
}

// scrollEventsFeed moves the Recent Events feed's viewport by dir*magnitude
// lines (dir is -1 or 1; magnitude is 1 for a shift+up/down line-scroll or
// eventsPageSize() for a shift+left/right page). Modeless (R2-2): called
// straight out of handleListKey's switch, the same way tasksModel.chartCursorMove
// drives the board thumbnail — there is no subfocus to route through first.
// The clamp to feedLen's last row is the only bound on logsOffset — nothing
// else clamps this field; renderEventsFeed clamps only a local copy for
// display, never writing back, to keep the render path pure — so an
// unbounded delta must not be able to walk it past the feed's last row.
func (p *projectsModel) scrollEventsFeed(dir, magnitude int) {
	p.logsOffset += dir * magnitude
	last := p.feedLen() - 1
	if last < 0 {
		last = 0
	}
	if p.logsOffset > last {
		p.logsOffset = last
	}
	if p.logsOffset < 0 {
		p.logsOffset = 0
	}
}

// eventFeedLine assembles one digest line, sized to width (the events box's
// inner width — see renderEventsFeed). Column budget (spec): gutter, id(7,
// dim), subject(7), actor(8), message(flex), age(right, dim). The id column
// drops below feedIDMinWidth of width, then the age below feedAgeMinWidth:
// the id is a lookup key needed only when acting on a specific event, so it
// yields the message column — the one carrying what the user is actually
// scanning — first. `plain` suppresses the inner dim styles so a cursor row
// can be re-styled whole.
func (p *projectsModel) eventFeedLine(e core.LogEntry, lanes []rune, laneW, width int, now time.Time, plain bool) string {
	dim := func(s string) string {
		if plain {
			return s
		}
		return p.m.styles.Muted.Render(s)
	}
	gutter := make([]rune, laneW)
	for i := range gutter {
		gutter[i] = ' '
	}
	copy(gutter, lanes)
	code := p.m.projectScope
	var b strings.Builder
	b.WriteString(string(gutter))
	b.WriteString(" ")
	used := laneW + 1
	if width >= feedIDMinWidth {
		b.WriteString(dim(fmt.Sprintf("%-7s", shortEventID(e.ID))))
		b.WriteString(" ")
		used += 8
	}
	b.WriteString(fmt.Sprintf("%-7s ", truncateRunes(eventFeedSubject(e, code), 7)))
	b.WriteString(fmt.Sprintf("%-8s ", truncateRunes(eventFeedActor(e.Actor), 8)))
	used += 17
	age := ""
	if width >= feedAgeMinWidth {
		age = compactAge(e.At, now)
	}
	msgW := width - used - lipgloss.Width(age)
	if age != "" {
		msgW-- // the space before the age
	}
	if msgW < 4 {
		msgW = 4
	}
	b.WriteString(fmt.Sprintf("%-*s", msgW, truncateRunes(eventDigestMessage(e, code), msgW)))
	if age != "" {
		b.WriteString(" ")
		b.WriteString(dim(age))
	}
	return b.String()
}
