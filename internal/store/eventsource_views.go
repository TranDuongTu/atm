package store

import (
	"bytes"
	"os"
	"sort"
	"time"

	"atm/internal/eventsource"
)

// V2LogView is one row of `store log` for a v2-active project: the v2 event
// file rendered in the deterministic total order, with identities mapped back
// to the aliases a user can type.
type V2LogView struct {
	Ordinal int       `json:"ordinal"`
	ID      string    `json:"id"`
	At      time.Time `json:"at"`
	Actor   string    `json:"actor"`
	Action  string    `json:"action"`
	Subject string    `json:"subject"`
}

// ReadV2LogForDisplay renders the raw v2 event file for `store log`: a strict
// read (never a repair — this is an inspection command), events sorted by
// eventsource.CompareEvents, Ordinal set to the 1-based position in that order.
// The ordinal is a DISPLAY position, not an identity: the event id is, and it
// is carried in ID.
func (s *Store) ReadV2LogForDisplay(code string) ([]V2LogView, error) {
	snap, err := s.verifyV2File(code)
	if err != nil {
		return nil, err
	}
	state, err := eventsource.FoldEvents(snap.Events)
	if err != nil {
		return nil, err
	}
	events := append([]*eventsource.Event(nil), snap.Events...)
	sort.SliceStable(events, func(i, j int) bool { return eventsource.CompareEvents(events[i], events[j]) < 0 })
	out := make([]V2LogView, 0, len(events))
	for i, ev := range events {
		out = append(out, V2LogView{
			Ordinal: i + 1,
			ID:      ev.ID,
			At:      ev.At,
			Actor:   ev.Actor,
			Action:  ev.Action,
			Subject: v2SubjectDisplay(state, ev),
		})
	}
	return out, nil
}

// v2SubjectDisplay renders an event's subject the way a user names it: a
// project by code, a label by name, a task/comment by the alias the fold minted
// for its identity. A CREATION event carries no subject.id — the entity's
// identity IS that event's id (spec decision 1) — so the lookup falls back to
// the event id, and to the raw identity if the fold holds no such entity (an
// event whose entity was never created cannot exist in a valid file, but a
// display path must not panic on one).
func v2SubjectDisplay(st *eventsource.State, ev *eventsource.Event) string {
	su := ev.Subject
	id := su.ID
	if id == "" {
		id = ev.ID
	}
	switch su.Kind {
	case "project":
		if su.Code != "" {
			return "project " + su.Code
		}
		if p, ok := st.Projects[id]; ok {
			return "project " + p.Code
		}
	case "label":
		return "label " + su.Name
	case "task":
		if t, ok := st.Tasks[id]; ok {
			return "task " + t.Alias
		}
	case "comment":
		if c, ok := st.Comments[id]; ok {
			return "comment " + c.Alias
		}
	}
	return su.Kind + " " + id
}

// readV2LogEntries renders the v2 event file as compatibility []LogEntry:
// events sorted by CompareEvents (the deterministic total order), Seq set to the
// 1-based ordinal in that order, and subject aliases restored from the fold so
// v1-shaped consumers (activity.Build, History's subjectMatch) keep working
// unchanged. The DAG is strictly richer than a linear log; this flattening is a
// deliberate L3 display decision — DAG-aware views are L4's problem.
//
// The read is strict about REPORTING: an integrity failure is always returned.
// It is lenient about RENDERING: the error comes back alongside the recoverable
// prefix of the event file (the events before the first damaged line), mirroring
// v1's ReadLog, which returns everything that parsed plus ErrIntegrity. Callers
// with an error channel must surface the error (HistoryE and its CLI callers do);
// the ones that deliberately tolerate it (tui/projects.go's summary pane) then
// still get a partial view instead of a silently empty one.
func (s *Store) readV2LogEntries(code string) ([]LogEntry, error) {
	snap, err := s.readV2File(code, false)
	if err != nil {
		if !IsIntegrity(err) {
			return nil, err
		}
		entries, perr := s.v2LogEntriesFrom(s.readV2EventPrefix(code))
		if perr != nil {
			// Not even a prefix folds (a damaged line early in the file, a
			// dangling parent): nothing renderable, but the integrity error --
			// never a bare empty view -- is what the caller gets.
			return nil, err
		}
		return entries, err
	}
	return s.v2LogEntriesFrom(snap.Events)
}

// readV2EventPrefix parses the longest prefix of COMMITTED lines (complete,
// newline-terminated -- L3-7) that parse cleanly, stopping at the first damaged
// line. It is the recovery read behind readV2LogEntries' partial view and reports
// no error of its own: its whole job is to salvage what it can from a file the
// strict read already rejected.
func (s *Store) readV2EventPrefix(code string) []*eventsource.Event {
	raw, err := os.ReadFile(s.eventsV2Path(code))
	if err != nil {
		return nil
	}
	body := raw
	if n := len(raw); n > 0 && raw[n-1] != '\n' {
		body = raw[:bytes.LastIndexByte(raw, '\n')+1] // drop the uncommitted tail
	}
	var events []*eventsource.Event
	lines := bytes.Split(body, []byte("\n"))
	for i, line := range lines {
		if i == len(lines)-1 && len(line) == 0 {
			break // split artifact after the final newline
		}
		ev, err := eventsource.Parse(line)
		if err != nil {
			break // first damaged committed line ends the recoverable prefix
		}
		events = append(events, ev)
	}
	return events
}

// v2LogEntriesFrom folds an event set and renders it as compatibility []LogEntry.
func (s *Store) v2LogEntriesFrom(evs []*eventsource.Event) ([]LogEntry, error) {
	snap := &V2FileSnapshot{Events: evs}
	state, err := eventsource.FoldEvents(snap.Events)
	if err != nil {
		return nil, err
	}
	alias := func(id string) string {
		if t, ok := state.Tasks[id]; ok {
			return t.Alias
		}
		if c, ok := state.Comments[id]; ok {
			return c.Alias
		}
		return id
	}
	events := append([]*eventsource.Event(nil), snap.Events...)
	sort.SliceStable(events, func(i, j int) bool { return eventsource.CompareEvents(events[i], events[j]) < 0 })
	out := make([]LogEntry, 0, len(events))
	for i, ev := range events {
		subj := Subject{Kind: ev.Subject.Kind, Code: ev.Subject.Code, Name: ev.Subject.Name}
		switch ev.Subject.Kind {
		case "task", "comment":
			id := ev.Subject.ID
			if id == "" {
				id = ev.ID // creation event: the entity's identity IS the event id
			}
			subj.ID = alias(id)
		}
		out = append(out, LogEntry{Seq: i + 1, At: ev.At, Actor: ev.Actor, Action: ev.Action, Subject: subj, Payload: ev.Payload})
	}
	return out, nil
}

// v2CompatEntities returns the project's live tasks and comments as
// compatibility rows, through the freshness-gated cache — the same rows list
// commands display, so search and indexing never disagree with `atm task list`.
// Chosen over a direct fold to avoid a second projection code path (spec
// "Log-derived views").
//
// It calls ensureV2CacheFresh, which takes the project lock: never call this
// from a context that already holds it (WithLock is not reentrant).
func (s *Store) v2CompatEntities(code string) ([]*Task, []*Comment, error) {
	if err := s.ensureV2CacheFresh(code); err != nil {
		return nil, nil, err
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, nil, err
	}
	tasks, err := cacheListTasksForProject(db, code)
	if err != nil {
		return nil, nil, err
	}
	ids, err := cacheListCommentIDsForProject(db, code)
	if err != nil {
		return nil, nil, err
	}
	var comments []*Comment
	for _, id := range ids {
		c, ok, err := cacheGetComment(db, id)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			comments = append(comments, c)
		}
	}
	return tasks, comments, nil
}
