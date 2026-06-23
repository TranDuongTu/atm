package store

import (
	"fmt"
	"os"
	"sort"
	"time"
)

var allowedTransitions = map[string]map[string]bool{
	"open":        {"in-progress": true, "blocked": true, "cancelled": true},
	"in-progress": {"review": true, "done": true, "open": true},
	"blocked":     {"open": true, "in-progress": true, "cancelled": true},
	"review":      {"done": true, "in-progress": true, "open": true},
	"done":        {"open": true},
	"cancelled":   {"open": true},
}

func (s *Store) CreateTask(projectCode, title, description string, labels []string, actor string) (*Task, error) {
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrUsage)
	}
	if err := ValidateActorID(actor); err != nil {
		return nil, err
	}

	var created *Task
	err := s.WithLock(projectCode, func() error {
		if err := s.registerActorLazy(actor); err != nil {
			return err
		}
		p, err := s.GetProject(projectCode)
		if err != nil {
			return err
		}
		for _, l := range labels {
			if err := s.validateLabelInProject(p, l); err != nil {
				return err
			}
		}
		n := p.NextTaskN
		id := RenderTaskID(projectCode, n)
		ts := Now()
		t := &Task{
			ID:          id,
			ProjectCode: projectCode,
			Title:       title,
			Description: description,
			Status:      "open",
			Labels:      append([]string(nil), labels...),
			Links:       []Link{},
			Todos:       []Todo{},
			Followups:   []Followup{},
			Discussions: []DiscussionEntry{},
			History: []HistoryEntry{
				{
					ID:     "h1",
					Action: "created",
					Actor:  actor,
					At:     ts,
					Meta:   map[string]any{},
				},
			},
			CreatedAt: ts,
			UpdatedAt: ts,
		}
		p.NextTaskN = n + 1
		p.UpdatedAt = ts
		if err := WriteJSON(s.projectPath(projectCode), p); err != nil {
			return err
		}
		if err := os.MkdirAll(s.tasksDir(projectCode), 0o755); err != nil {
			return err
		}
		if err := WriteJSON(s.taskPath(id), t); err != nil {
			return err
		}
		created = t
		return nil
	})
	return created, err
}

func (s *Store) GetTask(id string) (*Task, error) {
	var t Task
	if err := ReadJSON(s.taskPath(id), &t); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: task %q", ErrNotFound, id)
		}
		return nil, err
	}
	return &t, nil
}

func (s *Store) SetTitle(id, title, actor string) error {
	if title == "" {
		return fmt.Errorf("%w: title is required", ErrUsage)
	}
	return s.mutateTask(id, actor, "title-changed", func(t *Task, now time.Time) {
		t.Title = title
	})
}

func (s *Store) SetDescription(id, description, actor string) error {
	return s.mutateTask(id, actor, "description-changed", func(t *Task, now time.Time) {
		t.Description = description
	})
}

func (s *Store) SetStatus(id, status, actor string) error {
	if err := ValidateActorID(actor); err != nil {
		return err
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	return s.WithLock(code, func() error {
		if err := s.registerActorLazy(actor); err != nil {
			return err
		}
		t, err := s.GetTask(id)
		if err != nil {
			return err
		}
		if t.Status == status {
			return nil
		}
		if !allowedTransitions[t.Status][status] {
			return fmt.Errorf("%w: invalid transition %s -> %s", ErrConflict, t.Status, status)
		}
		from := t.Status
		now := Now()
		t.Status = status
		t.UpdatedAt = now
		t.appendHistoryAt("status-changed", actor, now, map[string]any{"from": from, "to": status})
		return WriteJSON(s.taskPath(id), t)
	})
}

func (s *Store) TaskLabelAdd(id, label, actor string) error {
	return s.mutateTask(id, actor, "label-added", func(t *Task, now time.Time) {
		for _, l := range t.Labels {
			if l == label {
				return
			}
		}
		t.Labels = append(t.Labels, label)
		sort.Strings(t.Labels)
	})
}

func (s *Store) TaskLabelRemove(id, label, actor string) error {
	return s.mutateTask(id, actor, "label-removed", func(t *Task, now time.Time) {
		out := t.Labels[:0]
		for _, l := range t.Labels {
			if l != label {
				out = append(out, l)
			}
		}
		t.Labels = out
	})
}

func (s *Store) mutateTask(id, actor, action string, fn func(t *Task, now time.Time)) error {
	if err := ValidateActorID(actor); err != nil {
		return err
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	return s.WithLock(code, func() error {
		if err := s.registerActorLazy(actor); err != nil {
			return err
		}
		t, err := s.GetTask(id)
		if err != nil {
			return err
		}
		now := Now()
		fn(t, now)
		t.UpdatedAt = now
		t.appendHistoryAt(action, actor, now, map[string]any{})
		return WriteJSON(s.taskPath(id), t)
	})
}

func (s *Store) validateLabelInProject(p *Project, label string) error {
	for _, l := range p.Labels {
		if l.Name == label {
			return nil
		}
	}
	return fmt.Errorf("%w: label %q not in project's label set", ErrUsage, label)
}

func (t *Task) nextCounter(prefix string) int {
	maxN := 0
	extract := func(id string) {
		if len(id) < 2 || string(id[0]) != prefix {
			return
		}
		n := 0
		for _, c := range id[1:] {
			if c < '0' || c > '9' {
				return
			}
			n = n*10 + int(c-'0')
		}
		if n > maxN {
			maxN = n
		}
	}
	switch prefix {
	case "t":
		for _, e := range t.Todos {
			extract(e.ID)
		}
	case "f":
		for _, e := range t.Followups {
			extract(e.ID)
		}
	case "d":
		for _, e := range t.Discussions {
			extract(e.ID)
		}
	case "h":
		for _, e := range t.History {
			extract(e.ID)
		}
	}
	return maxN + 1
}

func (t *Task) appendHistory(action, actor string, meta map[string]any) {
	t.appendHistoryAt(action, actor, Now(), meta)
}

func (t *Task) appendHistoryAt(action, actor string, at time.Time, meta map[string]any) {
	n := t.nextCounter("h")
	t.History = append(t.History, HistoryEntry{
		ID:     fmt.Sprintf("h%d", n),
		Action: action,
		Actor:  actor,
		At:     at,
		Meta:   meta,
	})
}
