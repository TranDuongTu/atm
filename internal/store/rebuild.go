package store

import (
	"database/sql"

	"atm/libs/eventsource"
)

type RebuildReport struct {
	Projects int
	Tasks    int
	Labels   int
}

// Rebuild regenerates cache.db from every project's events.v2.jsonl. Clears
// every cache table, then delegates to reprojectAllV2, which enumerates the
// project codes from disk (not from cache.db, which was just wiped), folds
// each v2 project's events, and re-inserts the live set.
func (s *Store) Rebuild() (*RebuildReport, error) {
	rep := &RebuildReport{}
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
func (s *Store) reprojectAllV2(db *sql.DB) (*RebuildReport, error) {
	rep := &RebuildReport{}
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
		snap, err := s.verifyV2File(code)
		if err != nil {
			if !IsIntegrity(err) {
				return rep, err
			}
			continue
		}
		state, err := eventsource.FoldEvents(snap.Events)
		if err != nil {
			if !IsIntegrity(err) {
				return rep, err
			}
			continue
		}
		if err := s.projectSnapshotDB(db, code, s.eng.ConvertState(code, state, snap.EventCount)); err != nil {
			return rep, err
		}
		rep.Projects++
		rep.Tasks += len(state.Tasks)
		// Fold this v2 project's live label names into the store-global,
		// name-keyed set (the "labels" cache table has no project_code
		// column — see cache_project.go's cacheDeleteProjectRows
		// comment) so a name shared with another project is counted once,
		// not once per project. Tombstoned entries are excluded to match
		// projectSnapshotDB, which never upserts them.
		for name, l := range state.Labels {
			if l.Tombstoned {
				continue
			}
			mergedLabels[name] = Label{Name: l.Name, Description: l.Description, Expr: l.Expr}
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
