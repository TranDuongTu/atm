package store

import (
	"os"

	"atm/internal/eventsource"
)

type RebuildReport struct {
	Projects int
	Tasks    int
	Labels   int
}

// Rebuild regenerates cache.db from every project's log. Enumerates project
// codes from disk (not from cache.db, which this function is about to wipe),
// clears every table, then replays each project's log and re-inserts the
// live set.
func (s *Store) Rebuild() (*RebuildReport, error) {
	rep := &RebuildReport{}
	db, err := s.cacheDB()
	if err != nil {
		return rep, err
	}
	codes, err := s.projectCodesOnDisk()
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
	mergedLabels := map[string]Label{}
	for _, code := range codes {
		format, err := s.projectFormat(code)
		if err != nil {
			return rep, err
		}
		if format == StoreFormatV2 {
			// Match v1's discipline three lines below: tolerate an integrity
			// error on THIS project by skipping it and moving on to the
			// remaining projects, rather than aborting the whole-store
			// rebuild. Any other error is a real operational failure and
			// still aborts.
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
			if err := s.cacheProjectFromV2State(code, state, snap.EventCount); err != nil {
				return rep, err
			}
			rep.Projects++
			rep.Tasks += len(state.Tasks)
			// Fold this v2 project's live label names into the same
			// store-global, name-keyed set the v1 path below uses (the
			// "labels" cache table has no project_code column — see
			// eventsource_projector.go's cacheDeleteProjectRows comment) so
			// a name shared with another project (v1 or v2) is counted once,
			// not once per project. Tombstoned entries are excluded to match
			// cacheProjectFromV2State, which never upserts them.
			for name, l := range state.Labels {
				if l.Tombstoned {
					continue
				}
				mergedLabels[name] = Label{Name: l.Name, Description: l.Description, Expr: l.Expr}
			}
			continue
		}
		st, err := s.Replay(code)
		if err != nil && !IsIntegrity(err) {
			return rep, err
		}
		if st.Project != nil {
			if err := cacheUpsertProject(db, st.Project); err != nil {
				return rep, err
			}
			rep.Projects++
		}
		for _, t := range st.Tasks {
			if err := cacheUpsertTask(db, t); err != nil {
				return rep, err
			}
			rep.Tasks++
		}
		for _, c := range st.Comments {
			if err := cacheUpsertComment(db, c); err != nil {
				return rep, err
			}
		}
		for _, l := range st.Labels {
			mergedLabels[l.Name] = l
		}
		_ = os.RemoveAll(s.vectorsDir(code))
	}
	for _, l := range mergedLabels {
		if err := cacheUpsertLabel(db, l); err != nil {
			return rep, err
		}
	}
	rep.Labels = len(mergedLabels)
	return rep, nil
}
