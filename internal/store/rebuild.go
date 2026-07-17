package store

import (
	"database/sql"

	"atm/internal/core"
)

// Rebuild regenerates cache.db from every project's events.v2.jsonl. Clears
// every cache table, then delegates to reprojectAllV2, which enumerates the
// project codes from disk (not from cache.db, which was just wiped), folds
// each v2 project's events, and re-inserts the live set.
func (s *Store) Rebuild() (*core.RebuildReport, error) {
	rep := &core.RebuildReport{}
	db, err := s.cacheDB()
	if err != nil {
		return rep, err
	}
	tx, err := db.Begin()
	if err != nil {
		return rep, err
	}
	for _, table := range []string{"task_labels", "comment_labels", "tasks", "comments", "projects", "labels"} {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			tx.Rollback()
			return rep, err
		}
	}
	if err := tx.Commit(); err != nil {
		return rep, err
	}
	return s.reprojectAllV2(db)
}

// reprojectAllV2 re-projects every on-disk v2 project's cache rows from its
// events.v2.jsonl into db, plus the store-global merged label set. It is the
// shared core of Rebuild (called after Rebuild's own table wipe) and of the
// cache schema migration's eager reprojection (cache.go, run inside
// cacheDB()'s cacheOnce.Do against tables that were just recreated empty).
//
// It takes db directly and MUST NOT call s.cacheDB(): the migration caller is
// already inside cacheOnce.Do, and cacheDB() is not reentrant — calling it
// again from here would deadlock.
func (s *Store) reprojectAllV2(db *sql.DB) (*core.RebuildReport, error) {
	rep := &core.RebuildReport{}
	codes, err := s.projectCodesOnDisk()
	if err != nil {
		return rep, err
	}
	mergedLabels := map[string]Label{}
	for _, code := range codes {
		format, err := s.projectFormat(code)
		if err != nil {
			return rep, err
		}
		if format != StoreFormatV2 {
			continue
		}
		// Tolerate an integrity error on THIS project by skipping it and
		// moving on to the remaining projects, rather than aborting the
		// whole-store rebuild. Any other error is a real operational failure
		// and still aborts.
		snap, err := s.eng.Snapshot(code)
		if err != nil {
			if !IsIntegrity(err) {
				return rep, err
			}
			continue
		}
		if err := s.projectSnapshotDB(db, code, snap); err != nil {
			return rep, err
		}
		rep.Projects++
		// Tombstone-inclusive, matching the pre-carve len(state.Tasks): the
		// report's task count has always included removed tasks.
		rep.Tasks += snap.TotalTasks
		// Fold this v2 project's live label names into the store-global,
		// name-keyed set (the "labels" cache table has no project_code
		// column — see cache_project.go's cacheDeleteProjectRows
		// comment) so a name shared with another project is counted once,
		// not once per project. The snapshot's Labels already exclude
		// tombstoned entries, matching projectSnapshotDB, which never
		// upserts them.
		for _, l := range snap.Labels {
			mergedLabels[l.Name] = Label{Name: l.Name, Description: l.Description, Expr: l.Expr}
		}
	}
	for _, l := range mergedLabels {
		if err := cacheUpsertLabel(db, l); err != nil {
			return rep, err
		}
	}
	rep.Labels = len(mergedLabels)
	return rep, nil
}

// rebuildProjectFromV2 re-derives the project's cache rows from the v2 event
// file: the engine takes a strict snapshot (Snapshot re-reads and folds), the
// facade projects. There is no per-entity variant because the whole live set
// is projected from one snapshot, and the freshness key is a whole-file change
// count. Caller MUST hold the project lock.
func (s *Store) rebuildProjectFromV2(code string) error {
	snap, err := s.eng.Snapshot(code)
	if err != nil {
		return err
	}
	return s.projectSnapshot(code, snap)
}

// rebuildEntityCacheLocked dispatches a point-read rebuild by format: the v2
// snapshot for a v2-active project, the given v1 closure otherwise. Caller MUST
// hold the project lock (directly, or via the entry point's WithLock wrapper).
//
// Routing EVERY point-read rebuild closure through this — including the ones
// the *Locked variants pass — is what keeps a locked validation read (the ones
// v1 and v2 mutators perform mid-transaction) from replaying the frozen v1 log
// into the cache of a v2-active project.
func (s *Store) rebuildEntityCacheLocked(code string, v1 func() error) error {
	if f, err := s.projectFormat(code); err != nil {
		return err
	} else if f == StoreFormatV2 {
		return s.rebuildProjectFromV2(code)
	}
	return v1()
}
