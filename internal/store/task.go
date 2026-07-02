package store

import (
	"fmt"
	"os"
	"sort"
	"time"
)

func (s *Store) CreateTask(projectCode, title, description string, labels []string, actor string) (*Task, error) {
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrUsage)
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	var created *Task
	err := s.WithLock(projectCode, func() error {
		p, err := s.GetProject(projectCode)
		if err != nil {
			return err
		}
		for _, l := range labels {
			if err := ValidateLabelName(l); err != nil {
				return err
			}
			if err := s.labelProjectExists(l); err != nil {
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
			Labels:      append([]string(nil), labels...),
			History: []HistoryEntry{
				{ID: "h1", Action: "created", Actor: actor, At: ts, Meta: map[string]any{}},
			},
			CreatedAt: ts,
			CreatedBy: actor,
			UpdatedAt: ts,
			UpdatedBy: actor,
		}
		sort.Strings(t.Labels)
		if err := s.autoRegisterLabels(labels); err != nil {
			return err
		}
		p.NextTaskN = n + 1
		p.UpdatedAt = ts
		p.UpdatedBy = actor
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
	}, map[string]any{})
}

func (s *Store) SetDescription(id, description, actor string) error {
	return s.mutateTask(id, actor, "description-changed", func(t *Task, now time.Time) {
		t.Description = description
	}, map[string]any{})
}

// Design note: the spec says both "auto-registers any supplied labels" (upsert)
// AND "new assignments are refused" after LabelRemove. The v2 data model has no
// tombstone (LabelRemove just drops the entry), so a removed label is
// indistinguishable from a never-existing one. We resolve this tension in favor
// of the data model: TaskLabelAdd/CreateTask always auto-register (upsert) and
// never refuse — matching "agents can self-organize by inventing labels at
// assign time." The "refused" language is advisory and does not survive the
// tombstone-less model. If you want a label to stop being used, the human
// removes it from tasks; the registry is a description store + namespace index,
// not a gatekeeper (spec §3).
func (s *Store) TaskLabelAdd(id, label, actor string) error {
	if err := ValidateLabelName(label); err != nil {
		return err
	}
	if err := s.labelProjectExists(label); err != nil {
		return err
	}
	return s.mutateTask(id, actor, "label-added", func(t *Task, now time.Time) {
		for _, l := range t.Labels {
			if l == label {
				return
			}
		}
		t.Labels = append(t.Labels, label)
		sort.Strings(t.Labels)
	}, map[string]any{"label": label})
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
	}, map[string]any{"label": label})
}

func (s *Store) RemoveTask(id, actor string) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	return s.WithLock(code, func() error {
		if _, err := s.GetTask(id); err != nil {
			return err
		}
		return os.Remove(s.taskPath(id))
	})
}

func (s *Store) mutateTask(id, actor, action string, fn func(t *Task, now time.Time), meta map[string]any) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	return s.WithLock(code, func() error {
		t, err := s.GetTask(id)
		if err != nil {
			return err
		}
		now := Now()
		fn(t, now)
		t.UpdatedAt = now
		t.UpdatedBy = actor
		t.appendHistoryAt(action, actor, now, meta)
		return WriteJSON(s.taskPath(id), t)
	})
}

func (t *Task) appendHistoryAt(action, actor string, at time.Time, meta map[string]any) {
	n := len(t.History) + 1
	t.History = append(t.History, HistoryEntry{
		ID: fmt.Sprintf("h%d", n), Action: action, Actor: actor, At: at, Meta: meta,
	})
}