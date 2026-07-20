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
// gutter derived from the v2 parents DAG. Everything in this file is a pure
// formatting helper over core.LogEntry; the pane wiring lives in projects.go.

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

// newestFeedEntries builds the bounded, newest-first feed from ReadLogCached's
// oldest-first entries: the newest maxFeedEvents are its tail, so the cap is
// a slice rather than a full reverse-then-truncate.
func newestFeedEntries(entries []core.LogEntry) []core.LogEntry {
	if len(entries) > maxFeedEvents {
		entries = entries[len(entries)-maxFeedEvents:]
	}
	feed := make([]core.LogEntry, len(entries))
	for i, e := range entries {
		feed[len(entries)-1-i] = e
	}
	return feed
}

// renderEventsFeed renders the Recent Events section: caption, then a
// windowed, newest-first page of digest lines for the selected project.
// Unfocused it is a tail pinned to the newest event; subfocused (L) the
// cursor row is highlighted and windowLines follows it.
func (p *projectsModel) renderEventsFeed(height int) string {
	lines := []string{dashboardLine(p.width, p.m.styles.HeaderLabel.Render("Recent Events  [L]ogs"))}
	muted := func(s string) string {
		return dashboardLine(p.width, p.m.styles.Muted.Render(s))
	}
	if p.m.projectScope == "" {
		lines = append(lines, muted("select a project to see events"))
		return padToHeight(strings.Join(lines, "\n"), height)
	}
	entries, err := p.m.store.ReadLogCached(p.m.projectScope)
	if err != nil && !core.IsIntegrity(err) {
		lines = append(lines, muted("events could not be loaded"))
		return padToHeight(strings.Join(lines, "\n"), height)
	}
	if len(entries) == 0 {
		lines = append(lines, muted("no events yet"))
		return padToHeight(strings.Join(lines, "\n"), height)
	}
	feed := newestFeedEntries(entries)
	if p.logsCursor > len(feed)-1 {
		p.logsCursor = len(feed) - 1
	}
	cursor := 0
	if p.logsFocus {
		cursor = p.logsCursor
	}
	rows := height - 1 // caption
	start, end := windowLines(len(feed), cursor, rows)
	graph := eventGraphRows(feed)
	laneW := 1
	for i := start; i < end; i++ {
		if len(graph[i]) > laneW {
			laneW = len(graph[i])
		}
	}
	now := core.Now()
	for i := start; i < end; i++ {
		onCursor := p.logsFocus && i == p.logsCursor
		line := p.eventFeedLine(feed[i], graph[i], laneW, now, onCursor)
		if onCursor {
			line = p.m.styles.RowCursor.Render(line)
		}
		lines = append(lines, dashboardLine(p.width, line))
	}
	return padToHeight(strings.Join(lines, "\n"), height)
}

// eventFeedLine assembles one digest line. Column budget (spec): gutter,
// id(7, dim), subject(7), actor(8), message(flex), age(right, dim). The id
// column drops below 36 inner columns, then the age below 30. `plain`
// suppresses the inner dim styles so a cursor row can be re-styled whole.
func (p *projectsModel) eventFeedLine(e core.LogEntry, lanes []rune, laneW int, now time.Time, plain bool) string {
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
	if p.width >= 36 {
		b.WriteString(dim(fmt.Sprintf("%-7s", shortEventID(e.ID))))
		b.WriteString(" ")
		used += 8
	}
	b.WriteString(fmt.Sprintf("%-7s ", truncateRunes(eventFeedSubject(e, code), 7)))
	b.WriteString(fmt.Sprintf("%-8s ", truncateRunes(eventFeedActor(e.Actor), 8)))
	used += 17
	age := ""
	if p.width >= 30 {
		age = compactAge(e.At, now)
	}
	msgW := p.width - used - lipgloss.Width(age)
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
