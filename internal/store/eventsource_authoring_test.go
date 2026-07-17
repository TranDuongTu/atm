package store

import (
	"testing"

	"atm/internal/core"
)

// authorTaskViaEngine appends a single task.created to code's event file
// through the engine's ChangeSet seam WITHOUT reprojecting the cache — the
// post-carve stand-in for the old appendV2TaskCreatedLocked. Store tests use
// it to simulate a writer that committed an event but died before projection
// (a lagging cache is a designed-for v2 state). code must already be v2-active.
func authorTaskViaEngine(t *testing.T, s *Store, code, title, actor string) string {
	t.Helper()
	var alias string
	if err := s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		var err error
		alias, err = cs.CreateTask(core.TaskDraft{Title: title}, actor)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return alias
}

// authorCommentViaEngine is authorTaskViaEngine's comment analogue: it appends
// one comment.created without reprojecting.
func authorCommentViaEngine(t *testing.T, s *Store, code, taskID, body, actor string) string {
	t.Helper()
	var alias string
	if err := s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		var err error
		alias, err = cs.CreateComment(core.CommentDraft{TaskID: taskID, Body: body}, actor)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return alias
}
