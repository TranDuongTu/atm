package store

import "os"

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
