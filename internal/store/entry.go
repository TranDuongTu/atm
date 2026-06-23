package store

import (
	"fmt"
	"sort"
	"time"
)

type TimelineEntry struct {
	Kind string    `json:"kind"`
	ID   string    `json:"id"`
	At   time.Time `json:"at"`
	Data any       `json:"-"`
}

type timelineItem struct {
	kind string
	id   string
	at   time.Time
	data any
}

func (s *Store) TodoAdd(id, text, actor string) (*Task, error) {
	if text == "" {
		return nil, fmt.Errorf("%w: todo text is required", ErrUsage)
	}
	var out *Task
	err := s.mutateTask(id, actor, "todo-added", func(t *Task, now time.Time) {
		n := t.nextCounter("t")
		t.Todos = append(t.Todos, Todo{
			ID:     fmt.Sprintf("t%d", n),
			Text:   text,
			Author: actor,
			At:     now,
		})
		out = t
	})
	return out, err
}

func (s *Store) TodoToggle(id, todoID, actor string) (*Task, error) {
	var out *Task
	err := s.mutateTask(id, actor, "todo-toggled", func(t *Task, now time.Time) {
		for i := range t.Todos {
			if t.Todos[i].ID == todoID {
				t.Todos[i].Done = !t.Todos[i].Done
				out = t
				return
			}
		}
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("%w: todo %q not found", ErrNotFound, todoID)
	}
	return out, nil
}

func (s *Store) FollowupAdd(id, text, assignee, author string, due *time.Time) (*Task, error) {
	if text == "" {
		return nil, fmt.Errorf("%w: followup text is required", ErrUsage)
	}
	if assignee == "" {
		assignee = author
	}
	var out *Task
	err := s.mutateTask(id, author, "followup-added", func(t *Task, now time.Time) {
		n := t.nextCounter("f")
		f := Followup{
			ID:       fmt.Sprintf("f%d", n),
			Text:     text,
			Assignee: assignee,
			Status:   "open",
			Due:      due,
			Author:   author,
			At:       now,
		}
		t.Followups = append(t.Followups, f)
		out = t
	})
	return out, err
}

func (s *Store) FollowupResolve(id, followupID, actor string) (*Task, error) {
	var out *Task
	err := s.mutateTask(id, actor, "followup-resolved", func(t *Task, now time.Time) {
		for i := range t.Followups {
			if t.Followups[i].ID == followupID {
				t.Followups[i].Status = "resolved"
				t.Followups[i].ResolvedAt = &now
				t.Followups[i].ResolvedBy = actor
				out = t
				return
			}
		}
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("%w: followup %q not found", ErrNotFound, followupID)
	}
	return out, nil
}

func (s *Store) DiscussionAdd(id, text, actor string) (*Task, error) {
	if text == "" {
		return nil, fmt.Errorf("%w: discussion text is required", ErrUsage)
	}
	var out *Task
	err := s.mutateTask(id, actor, "discussion-added", func(t *Task, now time.Time) {
		n := t.nextCounter("d")
		t.Discussions = append(t.Discussions, DiscussionEntry{
			ID:     fmt.Sprintf("d%d", n),
			Text:   text,
			Author: actor,
			At:     now,
		})
		out = t
	})
	return out, err
}

func (s *Store) Timeline(id string) ([]timelineItem, error) {
	t, err := s.GetTask(id)
	if err != nil {
		return nil, err
	}
	var items []timelineItem
	for _, e := range t.History {
		items = append(items, timelineItem{kind: "history", id: e.ID, at: e.At, data: e})
	}
	for _, e := range t.Todos {
		items = append(items, timelineItem{kind: "todo", id: e.ID, at: e.At, data: e})
	}
	for _, e := range t.Followups {
		items = append(items, timelineItem{kind: "followup", id: e.ID, at: e.At, data: e})
	}
	for _, e := range t.Discussions {
		items = append(items, timelineItem{kind: "discussion", id: e.ID, at: e.At, data: e})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].at.Equal(items[j].at) {
			return items[i].at.Before(items[j].at)
		}
		return items[i].id < items[j].id
	})
	return items, nil
}

func (s *Store) TimelineList(id string) ([]TimelineEntry, error) {
	items, err := s.Timeline(id)
	if err != nil {
		return nil, err
	}
	out := make([]TimelineEntry, 0, len(items))
	for _, it := range items {
		out = append(out, TimelineEntry{Kind: it.kind, ID: it.id, At: it.at, Data: it.data})
	}
	return out, nil
}
