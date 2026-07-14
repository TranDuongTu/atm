package store

import (
	"encoding/json"
	"fmt"
	"os"

	"atm/internal/eventsource"
	"atm/internal/seed"
)

// projectMediaExists reports ErrConflict when EITHER format's media is on
// disk for code. cache.db is documented as disposable and rebuildable, so a
// cache row alone is not proof of life; log.jsonl alone stopped being proof of
// ABSENCE the moment projects can be born on v2 (a v2-born project has no
// log.jsonl at all, so the old os.Stat(logPath) check would have let
// CreateProject append a second project.created over a live project).
// RemoveProject deletes the whole project directory, so both paths truly
// absent means the project was actually removed and recreation is allowed.
func (s *Store) projectMediaExists(code string) error {
	for _, path := range []string{s.logPath(code), s.eventsV2Path(code)} {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%w: project %q already exists", ErrConflict, code)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s *Store) CreateProject(code, name, actor string) (*Project, error) {
	if err := ValidateProjectCode(code); err != nil {
		return nil, err
	}
	if err := s.validateActor(actor); err != nil {
		return nil, err
	}
	// Birth format: projectFormat on a not-yet-existing project has no
	// ProjectFormats entry to find, so it resolves to the store's ActiveFormat
	// — which `atm store upgrade --all` / `set-format --format v2` flip to v2.
	if f, err := s.projectFormat(code); err != nil {
		return nil, err
	} else if f == StoreFormatV2 {
		return s.createProjectV2(code, name, actor)
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var created *Project
	err = s.WithLock(code, func() error {
		if err := s.projectMediaExists(code); err != nil {
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
		// 4. Record the born format EXPLICITLY, exactly as the v2 birth path
		// does. Every project created from L3 onward therefore carries an
		// entry, which is what makes SetActiveFormat's refusal rule precise:
		// the entry-less set is exactly the pre-L3 legacy projects (v1 media
		// by construction).
		if err := s.setProjectFormat(code, StoreFormatV1); err != nil {
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

// createProjectV2 is the v2 birth path — the only mutator that starts from an
// EMPTY event file. No log.jsonl is ever created for such a project.
func (s *Store) createProjectV2(code, name, actor string) (*Project, error) {
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var created *Project
	err = s.WithLock(code, func() error {
		if err := s.projectMediaExists(code); err != nil {
			return err
		}
		if _, ok, err := cacheGetProject(db, code); err != nil {
			return err
		} else if ok {
			return fmt.Errorf("%w: project %q already exists", ErrConflict, code)
		}
		// CRASH-WINDOW DECISION: write the ProjectFormats entry BEFORE the
		// first append. A crash between the two leaves an entry pointing at an
		// absent event file — benign: readV2File treats a missing file as an
		// empty snapshot, the project reads as an empty v2 project, and the
		// media-based existence check still allows recreation. The reverse
		// order is the dangerous window: v2 media with NO entry would read as
		// v1 (no log.jsonl) on a v1-default store AND block recreation,
		// violating the Task 1 invariant that every v2-media project carries an
		// explicit entry. On any error before the root append commits,
		// best-effort remove the entry again.
		if err := s.setProjectFormat(code, StoreFormatV2); err != nil {
			return err
		}
		// Root event: the fresh file has an empty frontier, so project.created
		// carries parents [] — beginV2AuthorLocked derives exactly that from
		// the (absent) events.v2.jsonl.
		ctx, err := s.beginV2AuthorLocked(code)
		if err != nil {
			_ = s.removeProjectFormat(code)
			return err
		}
		ev, _, err := eventsource.NewProjectCreated(ctx.clock, ctx.replica, ctx.snap.Frontier, eventsource.ProjectCreateDraft{
			Code: code, Name: name, At: Now(), Actor: actor,
		})
		if err != nil {
			_ = s.removeProjectFormat(code)
			return err
		}
		if err := s.commitV2AuthorLocked(code, ev); err != nil {
			_ = s.removeProjectFormat(code)
			return err
		}
		// Seed default labels as label.upserted v2 events — v1 parity with
		// seedLabelsLocked, same seed.Labels source; the payload carries only
		// the fields being set (the writesOf action table).
		for _, l := range seed.Labels {
			payload := map[string]any{"description": l.Description}
			if l.Expr != "" {
				payload["expr"] = l.Expr
			}
			if _, err := s.appendV2Locked(code, V2Draft{
				Actor:   actor,
				Action:  ActionLabelUpserted,
				Subject: eventsource.Subject{Kind: "label", Name: code + ":" + l.Suffix},
				Payload: payload,
			}); err != nil {
				return err
			}
		}
		if err := s.reprojectV2Locked(code); err != nil {
			return err
		}
		// TODO(Task 9): switch to getProjectLocked once its shared body branches
		// on the effective format; today it would run the v1 freshness checks.
		p, ok, err := cacheGetProject(db, code)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: project %q", ErrNotFound, code)
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
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if f, err := s.projectFormat(code); err != nil {
		return err
	} else if f == StoreFormatV2 {
		return s.setProjectNameV2(code, name, actor)
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

// setProjectNameV2 emits project.name-changed against the project's identity
// (never its code: the fold keys slot writes off subject.id).
func (s *Store) setProjectNameV2(code, name, actor string) error {
	return s.WithLock(code, func() error {
		ctx, err := s.beginV2AuthorLocked(code)
		if err != nil {
			return err
		}
		ref, err := ctx.resolveProjectRef(code)
		if err != nil {
			return err
		}
		if _, err := s.appendV2Locked(code, V2Draft{
			Actor:   actor,
			Action:  ActionProjectNameChanged,
			Subject: eventsource.Subject{Kind: "project", ID: ref, Code: code},
			Payload: map[string]any{"name": name},
		}); err != nil {
			return err
		}
		return s.reprojectV2Locked(code)
	})
}

func (s *Store) RemoveProject(code, actor string) error {
	if err := s.hasTasksGuard(code); err != nil {
		return err
	}
	if f, err := s.projectFormat(code); err != nil {
		return err
	} else if f == StoreFormatV2 {
		return s.removeProjectV2(code)
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
		// 3. Drop any explicit format entry: a v1 project born under L3 (or one
		// rolled back to v1) carries one, and a stale entry must not outlive
		// the project it describes.
		if err := s.removeProjectFormat(code); err != nil {
			return err
		}
		// 4. Delete the project cache row.
		return cacheDeleteProject(db, code)
	})
}

// removeProjectV2 removes a v2-active project. No v1 tombstone is appended:
// log.jsonl must stay byte-identical for a v2-active project, and the whole
// directory — events.v2.jsonl included — is deleted anyway. The event-DAG
// project.removed tombstone exists for REMOTE observers (L4 sync); local
// removal of the entire project is a filesystem operation plus metadata
// cleanup, exactly like v1's RemoveAll.
func (s *Store) removeProjectV2(code string) error {
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		if err := s.hasTasksGuard(code); err != nil {
			return err
		}
		ctx, err := s.beginV2AuthorLocked(code)
		if err != nil {
			return err
		}
		if _, err := ctx.resolveProjectRef(code); err != nil {
			return err
		}
		// 1. Delete the project directory (events.v2.jsonl, vectors, config).
		if err := os.RemoveAll(s.projectDir(code)); err != nil {
			return err
		}
		// 2. Remove the ProjectFormats entry so recreation follows ActiveFormat
		// instead of reading "v2" with no event file.
		if err := s.removeProjectFormat(code); err != nil {
			return err
		}
		// 3. Delete the project's cache rows AND its v2 freshness meta row.
		if err := cacheDeleteProjectRows(db, code); err != nil {
			return err
		}
		return cacheClearV2Freshness(db, code)
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
