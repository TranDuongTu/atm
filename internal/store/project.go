package store

import (
	"fmt"
	"os"

	"atm/internal/core"
	"atm/internal/store/eventlog"
)

func (s *Store) CreateProject(code, name, actor string) (*Project, error) {
	if err := ValidateProjectCode(code); err != nil {
		return nil, err
	}
	if err := s.validateActor(actor); err != nil {
		return nil, err
	}
	// Every new project is born v2. createProjectV2 ESTABLISHES the format (the
	// engine's WithProjectBirth writes ProjectFormats[code]=v2 itself), so
	// CreateProject no longer dispatches on the store's ActiveFormat — the
	// fresh-store DEFAULT stays v1, but no v1-active project can be created any
	// more.
	return s.createProjectV2(code, name, actor)
}

// createProjectV2 is the v2 birth path — the only mutator that starts from an
// EMPTY event file. No log.jsonl is ever created for such a project. It runs
// inside the engine's WithProjectBirth transaction: a plain project lock (never
// the format gate — the format is being ESTABLISHED), the format entry written
// before the first append, best-effort entry rollback if the closure fails
// before the root event commits. Double-creation is guarded in the PREFLIGHT
// (MediaExists + the cache "already exists" check), which WithProjectBirth runs
// before it touches store.json — so a failed duplicate create leaves an
// existing project's ProjectFormats entry (including a legacy explicit "v1")
// untouched.
func (s *Store) createProjectV2(code, name, actor string) (*Project, error) {
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var created *Project
	preflight := func() error {
		if err := s.eng.MediaExists(code); err != nil {
			return err
		}
		if _, ok, err := cacheGetProject(db, code); err != nil {
			return err
		} else if ok {
			return fmt.Errorf("%w: project %q already exists", core.ErrConflict, code)
		}
		return nil
	}
	err = s.eng.WithProjectBirth(code, preflight, func(cs core.ChangeSet) error {
		// Root event: the fresh file has an empty frontier, so project.created
		// carries parents [].
		if err := cs.CreateProject(name, actor); err != nil {
			return err
		}
		if err := s.reprojectTxn(code, cs); err != nil {
			return err
		}
		// getProjectLocked branches on the effective format in its shared body
		// (Task 9), so this locked read goes through the v2 freshness path
		// reprojectTxn just satisfied — never the v1 checks.
		p, err := s.getProjectLocked(code)
		if err != nil {
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

func (s *Store) GetProject(code string) (*Project, error) {
	return s.getProjectWithRebuild(code, func() error {
		return s.WithLock(code, func() error {
			return s.rebuildEntityCacheLocked(code, func() error { return noV1RebuildErr(code) })
		})
	})
}

// getProjectLocked is identical to GetProject except that, on a cache
// miss/stale hit, it triggers the rebuild directly instead of wrapping it in
// s.WithLock. Callers MUST already hold the project's lock (i.e. be running
// inside their own s.WithLock(code, ...) closure) — calling GetProject in
// that situation would re-enter the (non-reentrant) mutex and deadlock.
func (s *Store) getProjectLocked(code string) (*Project, error) {
	return s.getProjectWithRebuild(code, func() error {
		return s.rebuildEntityCacheLocked(code, func() error { return noV1RebuildErr(code) })
	})
}

// noV1RebuildErr is the v1 arm rebuildEntityCacheLocked forwards to for a
// non-v2 project. It should be unreachable: rebuildEntityCacheLocked is only
// ever invoked (via rebuild()) from the format==eventlog.StoreFormatV2 arm of a
// *WithRebuild accessor below, at which point rebuildEntityCacheLocked's own
// format re-check also finds v2 and dispatches to rebuildProjectFromV2, never
// to this closure. It exists only so the *WithRebuild accessors still have a
// well-typed closure to hand rebuildEntityCacheLocked now that the v1
// rebuildXFromLog helpers are gone; if it ever fires, that means a project
// reached this code with a non-v2 format, which is an integrity violation.
func noV1RebuildErr(code string) error {
	return fmt.Errorf("%w: project %q is not v2 (v1 rebuild path removed)", core.ErrIntegrity, code)
}

// getProjectWithRebuild contains the fast-path cache read + staleness check
// shared by GetProject and getProjectLocked. It is parameterized only by how
// the rebuild call itself gets invoked: wrapped in a fresh s.WithLock
// (GetProject, for callers that do not already hold the lock) or called
// directly (getProjectLocked, for callers that do).
//
// The non-v2 arm below is NOT a revival of v1 lazy-rebuild: there is no v1
// media left to rebuild from, so it never calls rebuild(). It exists because
// a project can resolve to a non-v2 format for two legitimate reasons that
// have nothing to do with v1 storage: (1) RemoveProject clears the project's
// ProjectFormats entry along with its media, so a fully removed project
// falls back to the default/ActiveFormat resolution — GetProject on it must
// still answer core.ErrNotFound; (2) a cache row can be written directly by a
// lower-level v2 helper ahead of format registration — GetProject must still
// serve that row rather than bounce it through the v2 freshness/rebuild dance,
// which has nothing to check the staleness of without a real event file.
// Either way, the cache row (or its absence) is authoritative; there's nothing
// to rebuild.
func (s *Store) getProjectWithRebuild(code string, rebuild func() error) (*Project, error) {
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	format, err := s.eng.ProjectFormat(code)
	if err != nil {
		return nil, err
	}
	if format != eventlog.StoreFormatV2 {
		p, ok, err := cacheGetProject(db, code)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: project %q", core.ErrNotFound, code)
		}
		return p, nil
	}
	if fresh, err := s.v2CacheFresh(code); err != nil {
		return nil, err
	} else if !fresh {
		if err := rebuild(); err != nil {
			return nil, err
		}
	}
	p, ok, err := cacheGetProject(db, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		// A fresh count with a missing row can still be a damaged cache
		// (the freshness key is a count, not a checksum): rebuild once and
		// re-read before declaring not-found.
		if err := rebuild(); err != nil {
			return nil, err
		}
		p, ok, err = cacheGetProject(db, code)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: project %q", core.ErrNotFound, code)
		}
	}
	return p, nil
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
	if _, err := s.eng.DispatchFormat(code); err != nil {
		return err
	}
	return s.setProjectNameV2(code, name, actor)
}

// setProjectNameV2 emits project.name-changed against the project's identity
// (never its code: the fold keys slot writes off subject.id).
func (s *Store) setProjectNameV2(code, name, actor string) error {
	return s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		if err := cs.SetProjectName(name, actor); err != nil {
			return err
		}
		return s.reprojectTxn(code, cs)
	})
}

// EnableProjectCapability records that the project enabled a capability.
func (s *Store) EnableProjectCapability(code, name, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if _, err := s.eng.DispatchFormat(code); err != nil {
		return err
	}
	return s.enableProjectCapabilityV2(code, name, actor)
}

// enableProjectCapabilityV2 emits project.capability-enabled against the
// project's identity (never its code: the fold keys slot writes off
// subject.id).
func (s *Store) enableProjectCapabilityV2(code, name, actor string) error {
	return s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		if err := cs.EnableCapability(name, actor); err != nil {
			return err
		}
		return s.reprojectTxn(code, cs)
	})
}

// DisableProjectCapability records that the project disabled a capability.
func (s *Store) DisableProjectCapability(code, name, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if _, err := s.eng.DispatchFormat(code); err != nil {
		return err
	}
	return s.disableProjectCapabilityV2(code, name, actor)
}

// disableProjectCapabilityV2 emits project.capability-disabled against the
// project's identity (never its code: the fold keys slot writes off
// subject.id).
func (s *Store) disableProjectCapabilityV2(code, name, actor string) error {
	return s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		if err := cs.DisableCapability(name, actor); err != nil {
			return err
		}
		return s.reprojectTxn(code, cs)
	})
}

func (s *Store) RemoveProject(code, actor string) error {
	if err := s.hasTasksGuard(code); err != nil {
		return err
	}
	if _, err := s.eng.DispatchFormat(code); err != nil {
		return err
	}
	return s.removeProjectV2(code)
}

// removeProjectV2 removes a v2-active project. No v1 tombstone is appended:
// log.jsonl must stay byte-identical for a v2-active project, and the whole
// directory — events.v2.jsonl included — is deleted anyway. The event-DAG
// project.removed tombstone exists for REMOTE observers (L4 sync); local
// removal of the entire project is a filesystem operation plus metadata
// cleanup, exactly like v1's RemoveAll.
//
// The "is this project empty?" guard is answered from the FOLD, not from cache
// rows (cs.HasLiveTasks). Under v2 a lagging cache is a designed-for state --
// an external append, or a writer that died between the append commit point and
// its reprojection, leaves the cache legitimately behind the event file -- and
// step 1 below is an IRREVERSIBLE os.RemoveAll that takes events.v2.jsonl with
// it. Every other v2 read path is freshness-gated; this one cannot be (the gate
// takes the project lock we already hold, and WithLock is not reentrant), so it
// consults the truth it already has in hand.
//
// CRASH-WINDOW DECISION (media first, entry second): a crash between the
// os.RemoveAll and ForgetProject leaves ProjectFormats[code]="v2" with no
// media, so a recreation goes to createProjectV2 regardless of ActiveFormat --
// coherent (createProjectV2 starts from an empty event file anyway, and it
// rewrites the same entry) and strictly safer than the reverse order, where the
// entry would be gone while v2 media survived: on a v1-default store that
// project would then read as v1 with no log.jsonl, breaking the invariant that
// every v2-media project carries an explicit entry.
func (s *Store) removeProjectV2(code string) error {
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		if err := cs.RequireProject(); err != nil {
			return err
		}
		if has, err := cs.HasLiveTasks(); err != nil {
			return err
		} else if has {
			return fmt.Errorf("%w: project %q has tasks — remove tasks first", core.ErrConflict, code)
		}
		// 1. Delete the project directory (events.v2.jsonl, vectors, config).
		if err := os.RemoveAll(s.projectDir(code)); err != nil {
			return err
		}
		// 2. Remove the ProjectFormats entry so recreation follows ActiveFormat
		// instead of reading "v2" with no event file.
		if err := cs.ForgetProject(); err != nil {
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
		return fmt.Errorf("%w: project %q has tasks — remove tasks first", core.ErrConflict, code)
	}
	return nil
}
