package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"atm/internal/eventsource"
)

func (s *Store) CreateTask(projectCode, title, description string, labels []string, actor string) (*Task, error) {
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrUsage)
	}
	if err := s.validateActor(actor); err != nil {
		return nil, err
	}
	// Every project is born v2, so task creation is unconditionally v2.
	return s.createTaskV2(projectCode, title, description, labels, actor)
}

// taskProjectFormat parses a task alias for its project code and resolves the
// project's EFFECTIVE format. Keying on the full alias string (never on a
// numeric segment) is mandatory: a v2 alias's segment is hex (Task 2b).
func (s *Store) taskProjectFormat(id string) (string, StoreFormat, error) {
	code, _, ok := ParseTaskID(id)
	if !ok {
		return "", "", fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	f, err := s.dispatchFormat(code)
	if err != nil {
		return "", "", err
	}
	return code, f, nil
}

// validateTaskLabelsV2Locked mirrors CreateTask's v1 per-label validation, in
// the same order (name → label's project exists → I1: a task may only carry
// stored labels, so no namespace label and no board).
func (s *Store) validateTaskLabelsV2Locked(code string, labels []string) error {
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	for _, l := range labels {
		if err := ValidateLabelName(l); err != nil {
			return err
		}
		if err := s.labelProjectExistsV2Locked(l, code); err != nil {
			return err
		}
		if IsNamespaceName(l) {
			return fmt.Errorf("%w: %s", ErrComputedLabelOnTask, l)
		}
		if lb, ok, err := cacheGetLabel(db, l); err != nil {
			return err
		} else if ok && lb.Expr != "" {
			return fmt.Errorf("%w: %s", ErrComputedLabelOnTask, l)
		}
	}
	return nil
}

// createTaskV2 writes task.created (plus any label.upserted auto-registration)
// to events.v2.jsonl. The alias is minted by the eventsource helper from the
// event identity — L3 never mints one itself (ATM-0125).
func (s *Store) createTaskV2(projectCode, title, description string, labels []string, actor string) (*Task, error) {
	var created *Task
	err := s.withProjectFormatLock(projectCode, StoreFormatV2, func() error {
		ctx, err := s.beginV2AuthorLocked(projectCode)
		if err != nil {
			return err
		}
		if _, err := ctx.resolveProjectRef(projectCode); err != nil {
			return err
		}
		if err := s.validateTaskLabelsV2Locked(projectCode, labels); err != nil {
			return err
		}
		if err := s.appendV2LabelUpsertsLocked(projectCode, labels, actor); err != nil {
			return err
		}
		_, alias, err := s.appendV2TaskCreatedLocked(projectCode, title, description, labels, actor)
		if err != nil {
			return err
		}
		if err := s.reprojectV2Locked(projectCode); err != nil {
			return err
		}
		db, err := s.cacheDB()
		if err != nil {
			return err
		}
		t, ok, err := cacheGetTask(db, alias)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: task %q", ErrNotFound, alias)
		}
		created = t
		return nil
	})
	return created, err
}

// mutateTaskV2 appends one v2 task event against the task's IDENTITY (the fold
// keys every slot write off subject.id, never the alias) and reprojects.
func (s *Store) mutateTaskV2(code, id, action, actor string, payload map[string]any) error {
	return s.withProjectFormatLock(code, StoreFormatV2, func() error {
		ctx, err := s.beginV2AuthorLocked(code)
		if err != nil {
			return err
		}
		ref, err := ctx.resolveTaskRef(id)
		if err != nil {
			return err
		}
		if _, err := s.appendV2Locked(code, V2Draft{
			Actor:   actor,
			Action:  action,
			Subject: eventsource.Subject{Kind: "task", ID: ref},
			Payload: payload,
		}); err != nil {
			return err
		}
		return s.reprojectV2Locked(code)
	})
}

// taskLabelAddV2 auto-registers the label (v1 parity) and asserts membership.
func (s *Store) taskLabelAddV2(code, id, label, actor string) error {
	return s.withProjectFormatLock(code, StoreFormatV2, func() error {
		ctx, err := s.beginV2AuthorLocked(code)
		if err != nil {
			return err
		}
		ref, err := ctx.resolveTaskRef(id)
		if err != nil {
			return err
		}
		if err := s.validateTaskLabelsV2Locked(code, []string{label}); err != nil {
			return err
		}
		if t, ok := ctx.state.Tasks[ref]; ok {
			for _, l := range t.Labels {
				if l == label {
					return nil
				}
			}
		}
		if err := s.appendV2LabelUpsertsLocked(code, []string{label}, actor); err != nil {
			return err
		}
		if _, err := s.appendV2Locked(code, V2Draft{
			Actor:   actor,
			Action:  ActionTaskLabelAdded,
			Subject: eventsource.Subject{Kind: "task", ID: ref},
			Payload: map[string]any{"label": label},
		}); err != nil {
			return err
		}
		return s.reprojectV2Locked(code)
	})
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
		return s.WithLock(code, func() error {
			return s.rebuildEntityCacheLocked(code, func() error { return s.rebuildTaskFromLog(id, code) })
		})
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
	return s.getTaskWithRebuild(id, code, func() error {
		return s.rebuildEntityCacheLocked(code, func() error { return s.rebuildTaskFromLog(id, code) })
	})
}

// getTaskWithRebuild contains the fast-path cache read + staleness check
// shared by GetTask and getTaskLocked. It is parameterized only by how the
// rebuild call itself gets invoked: wrapped in a fresh s.WithLock (GetTask, for
// callers that do not already hold the lock) or called directly (getTaskLocked,
// for callers that do); the closure is format-aware in both cases
// (rebuildEntityCacheLocked).
//
// The v2 branch is in this shared body, and runs BEFORE the v1 freshness checks
// (see getProjectWithRebuild for why both are load-bearing): a v2 cache row's
// LogSeq is the task's CREATION ORDINAL in the fold, and a v2-born project has
// no log.jsonl at all — so the v1 check `LogSeq > LastLogSeq` would hard-fail
// with ErrIntegrity on every v2-born task.
func (s *Store) getTaskWithRebuild(id, code string, rebuild func() error) (*Task, error) {
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	format, err := s.projectFormat(code)
	if err != nil {
		return nil, err
	}
	if format == StoreFormatV2 {
		if fresh, err := s.v2CacheFresh(code); err != nil {
			return nil, err
		} else if !fresh {
			if err := rebuild(); err != nil {
				return nil, err
			}
		}
		t, found, err := cacheGetTask(db, id)
		if err != nil {
			return nil, err
		}
		if !found {
			// A fresh count with a missing row can still be a damaged cache
			// (the freshness key is a count, not a checksum): rebuild once and
			// re-read before declaring not-found — the same idiom as the v1
			// miss path below. ErrNotFound is the same sentinel v1 returns, so
			// the CLI's exit codes are unchanged.
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
	}, ActionTaskTitleChanged, map[string]any{"title": title})
}

func (s *Store) SetDescription(id, description, actor string) error {
	return s.mutateTask(id, actor, func(t *Task, now time.Time) {
		t.Description = description
	}, ActionTaskDescChanged, map[string]any{"description": description})
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
	code, f, err := s.taskProjectFormat(id)
	if err != nil {
		return err
	}
	if f == StoreFormatV2 {
		return s.taskLabelAddV2(code, id, label, actor)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.withProjectFormatLock(code, StoreFormatV1, func() error {
		t, err := s.getTaskLocked(id)
		if err != nil {
			return err
		}
		// I1 - a task may only carry stored labels.
		if IsNamespaceName(label) {
			return fmt.Errorf("%w: %s", ErrComputedLabelOnTask, label)
		}
		if lb, ok, err := cacheGetLabel(db, label); err != nil {
			return err
		} else if ok && lb.Expr != "" {
			return fmt.Errorf("%w: %s", ErrComputedLabelOnTask, label)
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
	}, ActionTaskLabelRemoved, map[string]any{"label": label})
}

func (s *Store) RemoveTask(id, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	code, f, err := s.taskProjectFormat(id)
	if err != nil {
		return err
	}
	if f == StoreFormatV2 {
		return s.mutateTaskV2(code, id, ActionTaskRemoved, actor, nil)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.withProjectFormatLock(code, StoreFormatV1, func() error {
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

// mutateTask is the log-first write-through helper for non-delete task
// mutations. v2Payload is the equivalent v2 event payload (the writesOf key
// set for action); on a v2-active project the v1 body is never reached.
func (s *Store) mutateTask(id, actor string, fn func(t *Task, now time.Time), action string, v2Payload map[string]any) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	code, f, err := s.taskProjectFormat(id)
	if err != nil {
		return err
	}
	if f == StoreFormatV2 {
		return s.mutateTaskV2(code, id, action, actor, v2Payload)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.withProjectFormatLock(code, StoreFormatV1, func() error {
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
