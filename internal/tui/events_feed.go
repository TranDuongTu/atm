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
// eventsFeedBody, feedLen, scrollEventsFeed, eventFeedLine); projects.go owns
// the surrounding pane split, including the boxed-vs-compact decision
// (summaryChartsBoxed) that renderEventsFeed renders under. Navigation is
// modeless (revision 2, R2-2): the Shift modifier alone decides whether
// arrows drive the feed or the project list, mirroring tasksModel's
// board-thumbnail pattern in tasks_list.go — there is no subfocus to route
// through.

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

// eventFeedActor renders the actor column's text: the full persona@agent:model
// identity, unabbreviated (ATM-793b19 revision 3, R3-1). Revision 2 squeezed it
// into 8 cells as "per@agen", which collapsed developer@claude and
// developer@codex onto the same string and dropped the model entirely, so the
// column could not answer "which agent did this". The column is instead sized
// to the widest actor in the feed (feedActorWidth) and truncated only when the
// pane cannot afford that width. Kept as a function — rather than reading
// e.Actor at the call site — so actor display stays decided in one place.
func eventFeedActor(actor string) string { return actor }

// feedActorWidth returns the display width of the widest actor across the
// WHOLE bounded feed, which is what the actor column is sized to (R3-1).
// Sizing to the widest actor in the VISIBLE window instead would make the
// column — and the header above it — jitter as the user scrolls. The feed is
// bounded to maxFeedEvents, so this is one pass over at most 500 short
// strings, done once per render rather than once per line.
func feedActorWidth(feed []core.LogEntry) int {
	w := 0
	for i := range feed {
		if n := lipgloss.Width(eventFeedActor(feed[i].Actor)); n > w {
			w = n
		}
	}
	return w
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

// The events line's column widths. The id and subject columns are fixed and
// never drop (R3-3, priorities 1 and 2): the id is how a reader ties a feed
// row to `atm store log`, and the subject names WHAT changed. feedAgeMinWidth
// is the one surviving rung — below this inner width the age column, the
// least load-bearing of what remains, drops entirely. The actor and message
// columns share whatever is left, message-floor-first (see feedLayout).
//
// feedMessageMinWidth is the message column's floor, not a guarantee: the
// message yields down to it before the actor gives up a single column, and
// below it (feedLayout's avail/2 term) both columns shrink together rather
// than one starving the other.
const (
	feedIDWidth         = 7
	feedSubjectWidth    = 7
	feedAgeWidth        = 3
	feedAgeMinWidth     = 30
	feedMessageMinWidth = 8
)

// feedColumns is one events-line width allocation: the single result that
// BOTH the column header and every event line below it are laid out from
// (R3-2), so the header cannot label a column the body did not render or sit
// at a different offset than the values beneath it. Widths are cell budgets
// including each column's own separator where noted on feedLayout.
type feedColumns struct {
	innerW int // the events section's inner width this was allocated for
	laneW  int // commit-graph gutter
	actorW int // actor text; 0 means the column is not rendered
	msgW   int // message column INCLUDING its leading separator
	ageW   int // age column including its leading separator; 0 means dropped
}

// feedLayout allocates the events line's flexible columns for an inner width
// (the box's inner width when boxed, or the pane width when compact) given
// the gutter width and actorFullWidth — the widest actor across the whole
// bounded feed (feedActorWidth).
//
// The degradation order is the user's, most-protected first (R3-3): the id
// never drops, the subject never drops, the age drops below feedAgeMinWidth,
// the message truncates next down to feedMessageMinWidth, and the actor
// truncates last. The avail/2 term is what stops either flexible column
// reaching zero on a narrow pane: without it a message floor of 8 starves the
// actor entirely at an 80-column terminal (inner width 26, avail 8).
//
// Every column's budget includes exactly one separator, so the allocated
// widths sum to innerW exactly and no column has to be measured twice:
// fixed carries the separators after the gutter, the id and the subject; the
// message carries the one between it and the actor; the age carries the one
// before it.
func feedLayout(innerW, laneW, actorFullWidth int) feedColumns {
	c := feedColumns{innerW: innerW, laneW: laneW}
	fixed := laneW + 1 + feedIDWidth + 1 + feedSubjectWidth + 1
	if innerW >= feedAgeMinWidth {
		c.ageW = feedAgeWidth + 1
	}
	avail := innerW - fixed - c.ageW
	if avail < 2 {
		// Degenerate: there is not enough room for two columns at all, so
		// there is nothing to trade off — hand what is left to the message.
		if avail > 0 {
			c.msgW = avail
		}
		return c
	}
	c.msgW = feedMessageMinWidth
	if half := avail / 2; half < c.msgW {
		c.msgW = half
	}
	c.actorW = avail - c.msgW
	if actorFullWidth < c.actorW {
		c.actorW = actorFullWidth
	}
	c.msgW = avail - c.actorW // surplus the actor does not need
	return c
}

// ageCellWidth is the width the age text is right-aligned in. Normally
// feedAgeWidth, but compactAge can return four cells ("11mo") for an event
// most of a year old; that row borrows the extra column from its own message
// rather than overrunning the line, which would push the box's right border
// out by one on that row alone.
func (c feedColumns) ageCellWidth(age string) int {
	if w := lipgloss.Width(age); w > feedAgeWidth {
		return w
	}
	return feedAgeWidth
}

// msgTextWidth is the message column's text width — its budget less its own
// leading separator, less whatever an over-wide age borrowed (ageCellWidth).
func (c feedColumns) msgTextWidth(age string) int {
	w := c.msgW - 1
	if c.ageW > 0 {
		w -= c.ageCellWidth(age) - feedAgeWidth
	}
	if w < 0 {
		return 0
	}
	return w
}

// padCell pads an already-truncated (and possibly ANSI-styled) field out to w
// display cells.
func padCell(s string, w int) string {
	if pad := w - lipgloss.Width(s); pad > 0 {
		return s + spaces(pad)
	}
	return s
}

// row assembles one events line — the column header or an event — from this
// allocation, given each field's already-truncated (and, for an event line,
// already-styled) text. A column whose allocation is zero is skipped entirely,
// which is what makes the header label exactly the columns the body renders.
func (c feedColumns) row(gutter, id, subject, actor, msg, age string) string {
	var b strings.Builder
	b.WriteString(padCell(gutter, c.laneW))
	b.WriteString(" ")
	b.WriteString(padCell(id, feedIDWidth))
	b.WriteString(" ")
	b.WriteString(padCell(subject, feedSubjectWidth))
	b.WriteString(" ")
	if c.actorW > 0 {
		b.WriteString(padCell(actor, c.actorW))
	}
	if c.msgW > 0 {
		b.WriteString(" ")
		b.WriteString(padCell(msg, c.msgTextWidth(age)))
	}
	if c.ageW > 0 {
		b.WriteString(" ")
		w := c.ageCellWidth(age)
		b.WriteString(spaces(w-lipgloss.Width(age)) + age)
	}
	return b.String()
}

// feedHeaderRow renders the feed's column header (R3-2): one row naming the
// columns this allocation actually renders, laid out by the same
// feedColumns.row the event lines use so the two cannot drift apart. Returned
// unstyled; the caller applies the muted style to the whole line.
//
// Labels are clipped with fitLine rather than truncateRunes: a label is a
// known fixed word, so "ACTO" reads better than an ellipsis that would spend
// three of a four-cell column saying nothing.
func feedHeaderRow(c feedColumns) string {
	age := ""
	if c.ageW > 0 {
		age = "AGE"
	}
	return c.row(
		"",
		fitLine("ID", feedIDWidth),
		fitLine("TASK", feedSubjectWidth),
		fitLine("ACTOR", c.actorW),
		fitLine("MESSAGE", c.msgTextWidth(age)),
		age,
	)
}

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

// eventsFeedBody builds the Recent Events feed's line content — a muted
// placeholder state, or a column header over the windowed, newest-first slice
// of digest lines — sized to innerW and returning exactly rows+1 lines,
// blank-padded at the bottom when the feed itself is shorter. The +1 is the
// header row (R3-2); `rows` is the EVENT row count, which is what
// eventsFeedVisibleRows returns and what the scroll clamp and page magnitude
// are expressed in. A placeholder state carries no header — there are no
// columns to label — and blank-fills the same rows+1 lines instead.
//
// Shared by the boxed and compact render forms (ATM-793b19 revision-2 review,
// I1) so the content, ordering, column degradation, and offset/scroll behavior
// are identical in both — only the frame differs.
//
// Pure with respect to model state: the offset is clamped into a LOCAL copy
// only. View has a pointer receiver, and writing p.logsOffset here would
// leak render-time clamping (sized to whichever project and frame are on
// screen) back into model state, silently resetting a viewport position set
// on a larger project's feed.
func (p *projectsModel) eventsFeedBody(innerW, rows int) []string {
	muted := func(s string) string {
		return padFeedLine(p.m.styles.Muted.Render(s), innerW)
	}
	blank := spaces(innerW)
	// placeholder blank-fills a single-line message out to `rows` lines, same
	// as the populated body below it, so the boxed form's top-pad is a no-op
	// in every state — a short body would otherwise float to the vertical
	// middle while the populated feed stays top-aligned (irrelevant to the
	// compact form, which never pads internally).
	placeholder := func(msg string) []string {
		body := make([]string, 1, rows+1)
		body[0] = muted(msg)
		for len(body) < rows+1 {
			body = append(body, blank)
		}
		return body
	}
	if p.m.projectScope == "" {
		// Unreachable via renderList today: it folds the events section away
		// entirely when scope is empty. Kept as defensive cover for a future caller.
		return placeholder("select a project to see events")
	}
	entries, ok := p.readEventLog()
	if !ok {
		return placeholder("events could not be loaded")
	}
	if len(entries) == 0 {
		return placeholder("no events yet")
	}
	feed := newestFeedEntries(entries)
	// The bound mirrors scrollEventsFeed's window-aware clamp exactly —
	// against len(feed)-rows, not len(feed)-1 — so a terminal resize that
	// grows `rows` beyond the old maximum's remaining events cannot leave the
	// display showing fewer events than it has room for, with blank rows
	// below, until the next shift+arrow keypress recomputes the real clamp.
	offset := p.logsOffset
	if offset > len(feed)-rows {
		offset = len(feed) - rows
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
	// One allocation per render, not per line: the actor column is sized to
	// the widest actor in the WHOLE bounded feed (R3-1) so it cannot jitter as
	// the window moves, and the header below is laid out from the very same
	// feedColumns the event lines are.
	cols := feedLayout(innerW, laneW, feedActorWidth(feed))
	now := core.Now()
	body := make([]string, 0, rows+1)
	body = append(body, padFeedLine(p.m.styles.Muted.Render(feedHeaderRow(cols)), innerW))
	for i := start; i < end; i++ {
		body = append(body, p.eventFeedLine(feed[i], graph[i], cols, now, false))
	}
	for len(body) < rows+1 {
		body = append(body, blank)
	}
	return body
}

// renderEventsFeed renders the Recent Events section either as a bordered
// box aligned with the summary charts, or as a compact caption-plus-rows
// form matching the summary charts' own unboxed degradation (ATM-793b19
// revision-2 review, I1). `boxed` is decided once by the caller — renderList,
// from summaryChartsBoxed — rather than inferred here, so the feed and the
// summary section always agree on which visual language the pane is
// speaking; deciding it independently in each section is exactly how the
// pane ended up rendering two visual languages at some heights.
//
// Both forms window from p.logsOffset (moved only by scrollEventsFeed), with
// no cursor row and no highlight (R2-3); eventsFeedBody is the single content
// path both share, so the windowing and column degradation cannot drift
// apart between them — only the frame differs.
func (p *projectsModel) renderEventsFeed(height int, boxed bool) string {
	rows := eventsFeedVisibleRows(height)
	if boxed {
		// renderChartBox center-pads each body line and top-pads a short body
		// to float it to the vertical middle, both written for charts. The
		// feed wants left-and-top alignment instead, achieved without
		// touching renderChartBox by handing it a body for which both
		// behaviors are arithmetic no-ops: eventsFeedBody emits lines already
		// exactly chartBoxInnerWidth(p.width) wide (leftPad =
		// (innerW-innerW)/2 = 0) and exactly rows+1 = height-2 lines — the
		// column header plus the event rows — blank-padded at the bottom
		// (topPad = (innerH-innerH)/2 = 0).
		innerW := chartBoxInnerWidth(p.width)
		body := p.eventsFeedBody(innerW, rows)
		return p.renderChartBox(eventsFeedTitle, strings.Join(body, "\n"), height)
	}
	// Compact: a caption line carrying the title (and its key hint) plus
	// left-aligned rows sized to the pane width directly — the way the feed
	// rendered before it was ever boxed. rows still comes from
	// eventsFeedVisibleRows, the same function the boxed form and
	// scrollEventsFeed's offset clamp use, so this form never shows a
	// different window than the one the offset was clamped against; the
	// caller's padToHeight makes up the one line of difference between this
	// form's 1-line caption and the boxed form's 2-line border.
	lines := make([]string, 0, 1+rows)
	lines = append(lines, dashboardLine(p.width, p.m.styles.HeaderLabel.Render(eventsFeedTitle)))
	for _, line := range p.eventsFeedBody(p.width, rows) {
		lines = append(lines, dashboardLine(p.width, line))
	}
	return strings.Join(lines, "\n")
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

// eventsFeedVisibleRows converts an events section height into the number of
// EVENT lines visible inside it — height minus two lines of frame minus the
// one column-header row (R3-2), floored at 1. This is the exact arithmetic
// renderEventsFeed applies to the height it's handed, in both the boxed and
// compact forms; eventsPageSize's page magnitude and scrollEventsFeed's offset
// clamp both fold through this same function (fed the same eventsH from
// projectPaneSplitHeights) rather than recomputing the "-3" independently, so
// none of them can drift apart — which is also why the compact form budgets
// the same two "frame" lines as the boxed form even though its own caption is
// only one line: a different row count between the two forms would desync the
// offset clamp from what a given render actually draws. Revision 2 shipped a
// bug of exactly this shape when the framing changed the row overhead, and
// revision 3's header changes it again — hence the single source.
func eventsFeedVisibleRows(height int) int {
	rows := height - 3
	if rows < 1 {
		rows = 1
	}
	return rows
}

// eventsPageSize returns the events feed's page-scroll magnitude for
// shift+left/right: the actual visible row count (eventsFeedVisibleRows)
// minus one more, so a page jump leaves one row of context from the
// previous page (the same overlap convention listPageSize uses for the
// project list). The magnitude must come from the same visible-row count the
// renderer windows by — never a value computed independently — or a page
// jump can skip or duplicate rows relative to what's actually on screen.
func (p *projectsModel) eventsPageSize() int {
	_, eventsH, _ := projectPaneSplitHeights(p.contentHeight)
	page := eventsFeedVisibleRows(eventsH) - 1
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
//
// The upper clamp is against feedLen() minus the visible row count (R2-3),
// not feedLen() alone: a feed that does not overflow its window must not
// scroll at all, and at maximum scroll the window must be entirely full of
// events rather than parking one event above a column of blanks. The
// visible row count comes from eventsFeedVisibleRows fed the same eventsH
// eventsPageSize and renderEventsFeed use, so this bound cannot drift from
// what is actually rendered. This clamp is the only bound on logsOffset —
// nothing else clamps this field; renderEventsFeed clamps only a local copy
// for display, never writing back, to keep the render path pure — so an
// unbounded delta must not be able to walk it past the feed's last window.
func (p *projectsModel) scrollEventsFeed(dir, magnitude int) {
	_, eventsH, _ := projectPaneSplitHeights(p.contentHeight)
	if eventsH == 0 {
		// The events slot is collapsed (too short to render at all, or
		// folded away for an empty scope) — an explicit no-op rather than
		// moving an offset for a box nothing draws.
		return
	}
	p.logsOffset += dir * magnitude
	last := p.feedLen() - eventsFeedVisibleRows(eventsH)
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

// eventFeedLine assembles one digest line from a feedColumns allocation — the
// same one feedHeaderRow lays the column header out from, so a value can never
// render under the wrong label. Columns: gutter, id (dim), subject, actor,
// message, age (right-aligned, dim); which of them render, and how wide, is
// entirely feedLayout's decision.
//
// `plain` suppresses the inner dim styles; the production call site always
// passes false, and only tests pass true, to assert on unstyled output without
// fighting ANSI codes.
//
// The line is exactly cols.innerW wide by construction, and is padded rather
// than fitted if a degenerate allocation left it short. It CAN overrun innerW
// at inner widths below the fixed columns' own cost (~18 cells, i.e. terminals
// narrower than ~70 columns), the accepted consequence of ranking the id above
// the message (R3-4). Harmless: fitLine truncates it back down wherever the
// line is placed, and the id is the only styled field before the truncation
// point, so no ANSI code straddles it.
func (p *projectsModel) eventFeedLine(e core.LogEntry, lanes []rune, cols feedColumns, now time.Time, plain bool) string {
	dim := func(s string) string {
		if plain {
			return s
		}
		return p.m.styles.Muted.Render(s)
	}
	gutter := make([]rune, cols.laneW)
	for i := range gutter {
		gutter[i] = ' '
	}
	copy(gutter, lanes)
	code := p.m.projectScope
	age := ""
	if cols.ageW > 0 {
		age = compactAge(e.At, now)
	}
	line := cols.row(
		string(gutter),
		dim(shortEventID(e.ID)),
		truncateRunes(eventFeedSubject(e, code), feedSubjectWidth),
		truncateRunes(eventFeedActor(e.Actor), cols.actorW),
		truncateRunes(eventDigestMessage(e, code), cols.msgTextWidth(age)),
		dim(age),
	)
	return padCell(line, cols.innerW)
}
