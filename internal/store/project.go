package store

import (
	"encoding/json"
	"fmt"
	"os"
)

func (s *Store) CreateProject(code, name, actor string) (*Project, error) {
	if err := ValidateProjectCode(code); err != nil {
		return nil, err
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var created *Project
	err = s.WithLock(code, func() error {
		// cache.db is documented as disposable and rebuildable from log.jsonl,
		// so a cache-only miss does not mean the project doesn't exist: the
		// authoritative check is whether the project's log file is still on
		// disk. RemoveProject deletes the whole project directory (including
		// log.jsonl), so logPath truly absent means the project was actually
		// removed and recreation is allowed.
		if _, err := os.Stat(s.logPath(code)); err == nil {
			return fmt.Errorf("%w: project %q already exists", ErrConflict, code)
		} else if !os.IsNotExist(err) {
			return err
		}
		if _, ok, err := cacheGetProject(db, code); err != nil {
			return err
		} else if ok {
			return fmt.Errorf("%w: project %q already exists", ErrConflict, code)
		}
		now := Now()
		p := &Project{
			Code:      code,
			Name:      name,
			NextTaskN: 1,
			CreatedAt: now,
			CreatedBy: actor,
			UpdatedAt: now,
			UpdatedBy: actor,
			LogSeq:    0,
		}
		// 1. Append project.created to log.
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionProjectCreated,
			Subject: Subject{Kind: "project", Code: code},
			Payload: mustMarshal(p),
		})
		if err != nil {
			return err
		}
		p.LogSeq = entry.Seq
		// 2. Seed default labels (appends label.upserted per default label).
		if err := s.seedLabelsLocked(code, actor, now); err != nil {
			return err
		}
		// 3. Write project cache row.
		if err := cacheUpsertProject(db, p); err != nil {
			return err
		}
		created = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func mustMarshal(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}

func (s *Store) GetProject(code string) (*Project, error) {
	return s.getProjectWithRebuild(code, func() error {
		return s.WithLock(code, func() error { return s.rebuildProjectFromLog(code) })
	})
}

// getProjectLocked is identical to GetProject except that, on a cache
// miss/stale hit, it calls rebuildProjectFromLog directly instead of
// wrapping it in s.WithLock. Callers MUST already hold the project's lock
// (i.e. be running inside their own s.WithLock(code, ...) closure) — calling
// GetProject in that situation would re-enter the (non-reentrant) mutex and
// deadlock.
func (s *Store) getProjectLocked(code string) (*Project, error) {
	return s.getProjectWithRebuild(code, func() error { return s.rebuildProjectFromLog(code) })
}

// getProjectWithRebuild contains the fast-path cache read + staleness check
// shared by GetProject and getProjectLocked. It is parameterized only by how
// the rebuild-from-log call itself gets invoked: wrapped in a fresh
// s.WithLock (GetProject, for callers that do not already hold the lock) or
// called directly (getProjectLocked, for callers that do).
func (s *Store) getProjectWithRebuild(code string, rebuild func() error) (*Project, error) {
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	p, ok, err := cacheGetProject(db, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		if err := rebuild(); err != nil {
			return nil, err
		}
		p, ok, err = cacheGetProject(db, code)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: project %q", ErrNotFound, code)
		}
		return p, nil
	}
	last, lastErr := s.LastLogSeq(code)
	if lastErr != nil {
		return nil, lastErr
	}
	if p.LogSeq > last {
		return nil, fmt.Errorf("%w: project %q cache LogSeq=%d > log LastSeq=%d", ErrIntegrity, code, p.LogSeq, last)
	}
	projLast, err := s.lastProjectEventSeq(code)
	if err != nil {
		return nil, err
	}
	if projLast > p.LogSeq {
		if err := rebuild(); err != nil {
			return nil, err
		}
		p, ok, err = cacheGetProject(db, code)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: project %q", ErrNotFound, code)
		}
	}
	return p, nil
}

// lastProjectEventSeq returns the seq of the latest project.* log entry.
func (s *Store) lastProjectEventSeq(code string) (int, error) {
	entries, err := s.ReadLog(code)
	if err != nil {
		return 0, err
	}
	last := 0
	for _, e := range entries {
		if e.Subject.Kind == "project" && e.Subject.Code == code {
			last = e.Seq
		}
	}
	return last, nil
}

func (s *Store) rebuildProjectFromLog(code string) error {
	entries, err := s.ReadLog(code)
	if err != nil && !IsIntegrity(err) {
		return err
	}
	var p *Project
	lastSeq := 0
	maxTaskN := 0
	for _, e := range entries {
		switch e.Subject.Kind {
		case "project":
			if e.Subject.Code != code {
				continue
			}
			lastSeq = e.Seq
			if e.Action == ActionProjectRemoved {
				p = nil
				continue
			}
			var proj Project
			if err := json.Unmarshal(e.Payload, &proj); err == nil {
				p = &proj
			}
		case "task":
			// Track the highest task-ID N seen across ALL task.* entries
			// (including task.removed tombstones) so NextTaskN can be
			// reconstructed below without relying on a project.* log event
			// that CreateTask never appends. A removed task's number must
			// never be reused.
			if _, n, ok := ParseTaskID(e.Subject.ID); ok && n > maxTaskN {
				maxTaskN = n
			}
		}
	}
	if p == nil {
		return fmt.Errorf("%w: project %q", ErrNotFound, code)
	}
	p.LogSeq = lastSeq
	p.NextTaskN = max(p.NextTaskN, maxTaskN+1)
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return cacheUpsertProject(db, p)
}

func (s *Store) ListProjects() []*Project {
	db, err := s.cacheDB()
	if err != nil {
		return nil
	}
	codes, err := cacheListProjectCodes(db)
	if err != nil {
		return nil
	}
	var out []*Project
	for _, code := range codes {
		p, err := s.GetProject(code)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (s *Store) SetProjectName(code, name, actor string) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		p, err := s.getProjectLocked(code)
		if err != nil {
			return err
		}
		p.Name = name
		now := Now()
		p.UpdatedAt = now
		p.UpdatedBy = actor
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionProjectNameChanged,
			Subject: Subject{Kind: "project", Code: code},
			Payload: mustMarshal(p),
		})
		if err != nil {
			return err
		}
		p.LogSeq = entry.Seq
		return cacheUpsertProject(db, p)
	})
}

func (s *Store) RemoveProject(code, actor string) error {
	if err := s.hasTasksGuard(code); err != nil {
		return err
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		if err := s.hasTasksGuard(code); err != nil {
			return err
		}
		p, err := s.getProjectLocked(code)
		if err != nil {
			return err
		}
		now := Now()
		// 1. Append project.removed tombstone (payload = last state).
		_, _ = s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionProjectRemoved,
			Subject: Subject{Kind: "project", Code: code},
			Payload: mustMarshal(p),
		})
		// 2. Delete the project directory (including log.jsonl).
		_ = os.RemoveAll(s.projectDir(code))
		// 3. Delete the project cache row.
		return cacheDeleteProject(db, code)
	})
}

func (s *Store) hasTasksGuard(code string) error {
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	ids, err := cacheListTaskIDs(db, code)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		return fmt.Errorf("%w: project %q has tasks — remove tasks first", ErrConflict, code)
	}
	return nil
}
