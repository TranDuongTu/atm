package store

// v2CompatEntities returns the project's live tasks and comments as
// compatibility rows, through the freshness-gated cache — the same rows list
// commands display, so search and indexing never disagree with `atm task list`.
// Chosen over a direct fold to avoid a second projection code path (spec
// "Log-derived views").
//
// It calls ensureV2CacheFresh, which takes the project lock: never call this
// from a context that already holds it (WithLock is not reentrant).
func (s *Store) v2CompatEntities(code string) ([]*Task, []*Comment, error) {
	if err := s.ensureV2CacheFresh(code); err != nil {
		return nil, nil, err
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, nil, err
	}
	tasks, err := cacheListTasksForProject(db, code)
	if err != nil {
		return nil, nil, err
	}
	ids, err := cacheListCommentIDsForProject(db, code)
	if err != nil {
		return nil, nil, err
	}
	var comments []*Comment
	for _, id := range ids {
		c, ok, err := cacheGetComment(db, id)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			comments = append(comments, c)
		}
	}
	return tasks, comments, nil
}
