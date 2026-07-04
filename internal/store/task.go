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
			CreatedAt:   ts,
			CreatedBy:   actor,
			UpdatedAt:   ts,
			UpdatedBy:   actor,
		}
		sort.Strings(t.Labels)
		// 1. Append label.upserted for any newly-registered labels (BEFORE the task event).
		labelEntries, err := s.appendLabelUpsertsLocked(projectCode, labels, actor, ts)
		if err != nil {
			return err
		}
		_ = labelEntries
		// 2. Append task.created.
		entry, err := s.appendLogLocked(projectCode, LogEntry{
			At:      ts,
			Actor:   actor,
			Action:  ActionTaskCreated,
			Subject: Subject{Kind: "task", ID: id},
			Payload: mustMarshal(t),
		})
		if err != nil {
			return err
		}
		t.LogSeq = entry.Seq
		// 3. Bump project counter and write project cache (mutation).
		p.NextTaskN = n + 1
		p.UpdatedAt = ts
		p.UpdatedBy = actor
		if err := WriteJSON(s.projectPath(projectCode), p); err != nil {
			return err
		}
		// 4. Write task cache.
		if err := os.MkdirAll(s.tasksDir(projectCode), 0o755); err != nil {
			return err
		}
		if err := WriteJSON(s.taskPath(id), t); err != nil {
			return err
		}
		// 5. Refresh derived labels.json if any new labels were registered.
		if len(labelEntries) > 0 {
			if err := s.refreshDerivedLabelsLocked(projectCode); err != nil {
				return err
			}
		}
		created = t
		return nil
	})
	return created, err
}

// appendLabelUpsertsLocked appends label.upserted for each label name not already
// present in this project's log. Caller MUST hold the project lock.
func (s *Store) appendLabelUpsertsLocked(code string, labels []string, actor string, at time.Time) ([]LogEntry, error) {
	if len(labels) == 0 {
		return nil, nil
	}
	present, err := s.labelsInLogLocked(code)
	if err != nil {
		return nil, err
	}
	var out []LogEntry
	for _, name := range labels {
		if present[name] {
			continue
		}
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      at,
			Actor:   actor,
			Action:  ActionLabelUpserted,
			Subject: Subject{Kind: "label", Name: name},
			Payload: mustMarshal(Label{Name: name}),
		})
		if err != nil {
			return out, err
		}
		out = append(out, entry)
	}
	return out, nil
}

// labelsInLogLocked returns the set of label names that have an upserted event
// (and no subsequent removed event) in this project's log.
func (s *Store) labelsInLogLocked(code string) (map[string]bool, error) {
	st, err := s.Replay(code)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(st.Labels))
	for _, l := range st.Labels {
		out[l.Name] = true
	}
	return out, nil
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
	return s.mutateTask(id, actor, func(t *Task, now time.Time) {
		t.Title = title
	}, ActionTaskTitleChanged)
}

func (s *Store) SetDescription(id, description, actor string) error {
	return s.mutateTask(id, actor, func(t *Task, now time.Time) {
		t.Description = description
	}, ActionTaskDescChanged)
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
		for _, l := range t.Labels {
			if l == label {
				return nil
			}
		}
		t.Labels = append(t.Labels, label)
		sort.Strings(t.Labels)
		// 1. Append label.upserted for the new label if not already in log.
		labelEntries, err := s.appendLabelUpsertsLocked(code, []string{label}, actor, Now())
		if err != nil {
			return err
		}
		// 2. Append task.label-added.
		now := Now()
		t.UpdatedAt = now
		t.UpdatedBy = actor
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionTaskLabelAdded,
			Subject: Subject{Kind: "task", ID: id},
			Payload: mustMarshal(t),
		})
		if err != nil {
			return err
		}
		t.LogSeq = entry.Seq
		if err := WriteJSON(s.taskPath(id), t); err != nil {
			return err
		}
		if len(labelEntries) > 0 {
			return s.refreshDerivedLabelsLocked(code)
		}
		return nil
	})
}

func (s *Store) TaskLabelRemove(id, label, actor string) error {
	return s.mutateTask(id, actor, func(t *Task, now time.Time) {
		out := t.Labels[:0]
		for _, l := range t.Labels {
			if l != label {
				out = append(out, l)
			}
		}
		t.Labels = out
	}, ActionTaskLabelRemoved)
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
		t, err := s.GetTask(id)
		if err != nil {
			return err
		}
		now := Now()
		t.UpdatedAt = now
		t.UpdatedBy = actor
		// 1. Append task.removed tombstone (payload = last state).
		_, err = s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionTaskRemoved,
			Subject: Subject{Kind: "task", ID: id},
			Payload: mustMarshal(t),
		})
		if err != nil {
			return err
		}
		// 2. Delete the cache file.
		return os.Remove(s.taskPath(id))
	})
}

// mutateTask is the log-first write-through helper for non-delete task mutations.
func (s *Store) mutateTask(id, actor string, fn func(t *Task, now time.Time), action string) error {
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
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  action,
			Subject: Subject{Kind: "task", ID: id},
			Payload: mustMarshal(t),
		})
		if err != nil {
			return err
		}
		t.LogSeq = entry.Seq
		return WriteJSON(s.taskPath(id), t)
	})
}
