package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

func (s *Store) CreateTask(projectCode, title, description string, labels []string, actor string) (*Task, error) {
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrUsage)
	}
	if err := s.validateActor(actor); err != nil {
		return nil, err
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var created *Task
	err = s.WithLock(projectCode, func() error {
		p, err := s.getProjectLocked(projectCode)
		if err != nil {
			return err
		}
		for _, l := range labels {
			if err := ValidateLabelName(l); err != nil {
				return err
			}
			if err := s.labelProjectExistsLocked(l); err != nil {
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
		// 3. Bump project counter and write project cache row.
		p.NextTaskN = n + 1
		p.UpdatedAt = ts
		p.UpdatedBy = actor
		if err := cacheUpsertProject(db, p); err != nil {
			return err
		}
		// 4. Write task cache row.
		if err := cacheUpsertTask(db, t); err != nil {
			return err
		}
		created = t
		return nil
	})
	return created, err
}

// appendLabelUpsertsLocked appends label.upserted for each label name not
// already live in cache.db, and write-throughs the new row immediately.
// Caller MUST hold the project lock.
func (s *Store) appendLabelUpsertsLocked(code string, labels []string, actor string, at time.Time) ([]LogEntry, error) {
	if len(labels) == 0 {
		return nil, nil
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	present, err := cachePresentLabels(db, labels)
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
		if err := cacheUpsertLabel(db, Label{Name: name, LogSeq: entry.Seq}); err != nil {
			return out, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func (s *Store) GetTask(id string) (*Task, error) {
	code, _, ok := ParseTaskID(id)
	if !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	return s.getTaskWithRebuild(id, code, func() error {
		return s.WithLock(code, func() error { return s.rebuildTaskFromLog(id, code) })
	})
}

// getTaskLocked is identical to GetTask except that, on a cache miss/stale
// hit, it calls rebuildTaskFromLog directly instead of wrapping it in
// s.WithLock. Callers MUST already hold the task's project lock (i.e. be
// running inside their own s.WithLock(code, ...) closure) — calling GetTask
// in that situation would re-enter the (non-reentrant) mutex and deadlock.
func (s *Store) getTaskLocked(id string) (*Task, error) {
	code, _, ok := ParseTaskID(id)
	if !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	return s.getTaskWithRebuild(id, code, func() error { return s.rebuildTaskFromLog(id, code) })
}

// getTaskWithRebuild contains the fast-path cache read + staleness check
// shared by GetTask and getTaskLocked. It is parameterized only by how the
// rebuild-from-log call itself gets invoked: wrapped in a fresh s.WithLock
// (GetTask, for callers that do not already hold the lock) or called
// directly (getTaskLocked, for callers that do).
func (s *Store) getTaskWithRebuild(id, code string, rebuild func() error) (*Task, error) {
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	t, found, err := cacheGetTask(db, id)
	if err != nil {
		return nil, err
	}
	if !found {
		if err := rebuild(); err != nil {
			return nil, err
		}
		t, found, err = cacheGetTask(db, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: task %q", ErrNotFound, id)
		}
		return t, nil
	}
	last, lastErr := s.LastLogSeq(code)
	if lastErr != nil {
		return nil, lastErr
	}
	if t.LogSeq > last {
		return nil, fmt.Errorf("%w: task %q cache LogSeq=%d > log LastSeq=%d", ErrIntegrity, id, t.LogSeq, last)
	}
	taskLast, err := s.lastTaskEventSeq(code, id)
	if err != nil {
		return nil, err
	}
	if t.LogSeq < taskLast {
		if err := rebuild(); err != nil {
			return nil, err
		}
		t, found, err = cacheGetTask(db, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: task %q", ErrNotFound, id)
		}
	}
	return t, nil
}

// lastTaskEventSeq returns the seq of the latest log entry for the given task subject.
// An integrity error from ReadLog is propagated (a corrupt log must not be treated
// as "cache is fresh").
func (s *Store) lastTaskEventSeq(code, id string) (int, error) {
	entries, err := s.ReadLog(code)
	if err != nil {
		return 0, err
	}
	last := 0
	for _, e := range entries {
		if e.Subject.Kind == "task" && e.Subject.ID == id {
			last = e.Seq
		}
	}
	return last, nil
}

// rebuildTaskFromLog replays the task's events and rewrites the cache row.
// Caller MUST hold the project lock.
func (s *Store) rebuildTaskFromLog(id, code string) error {
	entries, err := s.ReadLog(code)
	if err != nil && !IsIntegrity(err) {
		return err
	}
	var t *Task
	lastSeq := 0
	for _, e := range entries {
		if e.Subject.Kind != "task" || e.Subject.ID != id {
			continue
		}
		lastSeq = e.Seq
		if e.Action == ActionTaskRemoved {
			t = nil
			continue
		}
		var tk Task
		if err := json.Unmarshal(e.Payload, &tk); err == nil {
			t = &tk
		}
	}
	if t == nil {
		return fmt.Errorf("%w: task %q", ErrNotFound, id)
	}
	t.LogSeq = lastSeq
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return cacheUpsertTask(db, t)
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
	if err := s.validateActor(actor); err != nil {
		return err
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		t, err := s.getTaskLocked(id)
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
		if _, err := s.appendLabelUpsertsLocked(code, []string{label}, actor, Now()); err != nil {
			return err
		}
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
		return cacheUpsertTask(db, t)
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
	if err := s.validateActor(actor); err != nil {
		return err
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		t, err := s.getTaskLocked(id)
		if err != nil {
			return err
		}
		now := Now()
		t.UpdatedAt = now
		t.UpdatedBy = actor
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
		return cacheDeleteTask(db, id)
	})
}

// mutateTask is the log-first write-through helper for non-delete task mutations.
func (s *Store) mutateTask(id, actor string, fn func(t *Task, now time.Time), action string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		t, err := s.getTaskLocked(id)
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
		return cacheUpsertTask(db, t)
	})
}
