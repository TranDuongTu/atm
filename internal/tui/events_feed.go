package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"atm/internal/core"
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
