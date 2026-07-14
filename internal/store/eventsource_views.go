package store

import (
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
