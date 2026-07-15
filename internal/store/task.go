package store

import (
	"fmt"

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

func (s *Store) GetTask(id string) (*Task, error) {
	code, _, ok := ParseTaskID(id)
	if !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	return s.getTaskWithRebuild(id, code, func() error {
		return s.WithLock(code, func() error {
			return s.rebuildEntityCacheLocked(code, func() error { return noV1RebuildErr(code) })
		})
	})
}

// getTaskLocked is identical to GetTask except that, on a cache miss/stale
// hit, it triggers the rebuild directly instead of wrapping it in
// s.WithLock. Callers MUST already hold the task's project lock (i.e. be
// running inside their own s.WithLock(code, ...) closure) — calling GetTask
// in that situation would re-enter the (non-reentrant) mutex and deadlock.
func (s *Store) getTaskLocked(id string) (*Task, error) {
	code, _, ok := ParseTaskID(id)
	if !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	return s.getTaskWithRebuild(id, code, func() error {
		return s.rebuildEntityCacheLocked(code, func() error { return noV1RebuildErr(code) })
	})
}

// getTaskWithRebuild contains the fast-path cache read + staleness check
// shared by GetTask and getTaskLocked. It is parameterized only by how the
// rebuild call itself gets invoked: wrapped in a fresh s.WithLock (GetTask, for
// callers that do not already hold the lock) or called directly (getTaskLocked,
// for callers that do).
//
// The non-v2 arm below is not a revival of v1 lazy-rebuild — see
// getProjectWithRebuild's doc comment for why a task's project can still
// legitimately resolve to a non-v2 format (a fully removed project, or a
// cache row written directly ahead of format registration) and why the
// correct response is to serve the cache row as-is (or ErrNotFound if
// absent) without ever attempting a rebuild.
func (s *Store) getTaskWithRebuild(id, code string, rebuild func() error) (*Task, error) {
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	format, err := s.projectFormat(code)
	if err != nil {
		return nil, err
	}
	if format != StoreFormatV2 {
		t, found, err := cacheGetTask(db, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: task %q", ErrNotFound, id)
		}
		return t, nil
	}
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
		// re-read before declaring not-found.
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

func (s *Store) SetTitle(id, title, actor string) error {
	if title == "" {
		return fmt.Errorf("%w: title is required", ErrUsage)
	}
	return s.mutateTask(id, actor, ActionTaskTitleChanged, map[string]any{"title": title})
}

func (s *Store) SetDescription(id, description, actor string) error {
	return s.mutateTask(id, actor, ActionTaskDescChanged, map[string]any{"description": description})
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
	code, _, err := s.taskProjectFormat(id)
	if err != nil {
		return err
	}
	return s.taskLabelAddV2(code, id, label, actor)
}

func (s *Store) TaskLabelRemove(id, label, actor string) error {
	return s.mutateTask(id, actor, ActionTaskLabelRemoved, map[string]any{"label": label})
}

func (s *Store) RemoveTask(id, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	code, _, err := s.taskProjectFormat(id)
	if err != nil {
		return err
	}
	return s.mutateTaskV2(code, id, ActionTaskRemoved, actor, nil)
}

// mutateTask is the shared entry point for non-delete task mutations; every
// project is v2-active (D-Task5b removed the v1 write arm), so it resolves
// the project code and delegates to mutateTaskV2 with the given action and
// payload.
func (s *Store) mutateTask(id, actor, action string, v2Payload map[string]any) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	code, _, err := s.taskProjectFormat(id)
	if err != nil {
		return err
	}
	return s.mutateTaskV2(code, id, action, actor, v2Payload)
}
