package store

import (
	"database/sql"
	"sort"

	"atm/libs/eventsource"
)

// reprojectV2Locked re-reads events.v2.jsonl (strictly — a v2 mutator has
// just appended to it, so any parse/DAG failure is a real integrity problem),
// folds it, and replaces the project's cache rows from the fold. Every v2
// mutator ends with this, so cache.db is consistent with the event file at
// the end of each mutation. Caller MUST hold the project lock.
func (s *Store) reprojectV2Locked(code string) error {
	snap, err := s.verifyV2File(code)
	if err != nil {
		return err
	}
	state, err := eventsource.FoldEvents(snap.Events)
	if err != nil {
		return err
	}
	return s.cacheProjectFromV2State(code, state, snap.EventCount)
}

// cacheProjectFromV2State replaces the project's cache rows with the live
// entities of a v2 fold. eventCount is the number of events in the file the
// fold came from; it is the v2 freshness key. Thin wrapper over
// cacheProjectFromV2StateDB that resolves the shared db handle via
// cacheDB() — callers already inside cacheDB()'s cacheOnce.Do (the schema
// migration) must call cacheProjectFromV2StateDB directly with their local
// db handle instead, since cacheDB() is not reentrant.
func (s *Store) cacheProjectFromV2State(code string, st *eventsource.State, eventCount int) error {
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.cacheProjectFromV2StateDB(db, code, st, eventCount)
}

// cacheProjectFromV2StateDB is cacheProjectFromV2State's DB-taking core. It
// does NOT call s.cacheDB(), so it is safe to call from inside cacheDB()'s
// cacheOnce.Do (e.g. the schema-migration eager reprojection) as well as from
// ordinary callers via the cacheProjectFromV2State wrapper.
func (s *Store) cacheProjectFromV2StateDB(db *sql.DB, code string, st *eventsource.State, eventCount int) error {
	if err := cacheDeleteProjectRows(db, code); err != nil {
		return err
	}
	for _, p := range st.Projects {
		if p.Code != code || p.Tombstoned {
			continue
		}
		if err := cacheUpsertProject(db, projectFromV2(p)); err != nil {
			return err
		}
	}
	commentAlias := func(id string) string {
		if c, ok := st.Comments[id]; ok && !c.Tombstoned {
			return c.Alias
		}
		return ""
	}
	ordinal := 0
	for _, t := range st.TasksByCreation() {
		ordinal++
		if t.Tombstoned {
			continue
		}
		if err := cacheUpsertTask(db, taskFromV2(code, t, ordinal)); err != nil {
			return err
		}
		for i, c := range st.CommentsByCreation(t.ID) {
			if c.Tombstoned {
				continue
			}
			if err := cacheUpsertComment(db, commentFromV2(c, t.Alias, commentAlias(c.ReplyToRef), i+1)); err != nil {
				return err
			}
		}
	}
	names := make([]string, 0, len(st.Labels))
	for name, l := range st.Labels {
		if l.Tombstoned {
			if _, err := db.Exec(`DELETE FROM labels WHERE name = ?`, name); err != nil {
				return err
			}
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for i, name := range names {
		if err := cacheUpsertLabel(db, labelFromV2(st.Labels[name], i+1)); err != nil {
			return err
		}
	}
	return cacheSetV2Freshness(db, code, eventCount)
}

// cacheDeleteProjectRows removes the project's task/comment/label rows and
// the project row itself — the per-project mirror of the global wipe
// Rebuild does. The labels table is store-global (merged across projects),
// so it has no project_code column; a project's own labels are scoped by
// the "<CODE>:" name prefix instead, the same scoping cacheListLabels uses.
// Sweeping here (not just deleting tombstoned names in the fold loop below)
// matters because that loop only ever visits label names present in the
// CURRENT fold — a label that was live in a previously-projected fold but
// is simply absent from the new one (e.g. a re-upgrade discarded the
// branch that upserted it) would otherwise never be visited, and its row
// would survive indefinitely.
func cacheDeleteProjectRows(db *sql.DB, code string) error {
	for _, stmt := range []string{
		`DELETE FROM comment_labels WHERE comment_id IN (SELECT c.id FROM comments c JOIN tasks t ON t.id = c.task_id WHERE t.project_code = ?)`,
		`DELETE FROM comments WHERE task_id IN (SELECT id FROM tasks WHERE project_code = ?)`,
		`DELETE FROM task_labels WHERE task_id IN (SELECT id FROM tasks WHERE project_code = ?)`,
		`DELETE FROM tasks WHERE project_code = ?`,
		`DELETE FROM projects WHERE code = ?`,
	} {
		if _, err := db.Exec(stmt, code); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`DELETE FROM labels WHERE name LIKE ? ESCAPE '\'`, escapeLike(code)+":%"); err != nil {
		return err
	}
	return nil
}

func projectFromV2(p *eventsource.ProjectState) *Project {
	// A v2 project has no per-project ordinal (only tasks/comments/labels do),
	// so Ordinal is left 0 here.
	return &Project{
		Code:      p.Code,
		Name:      p.Name,
		CreatedAt: p.CreatedAt,
		CreatedBy: p.CreatedBy,
		UpdatedAt: p.UpdatedAt,
		UpdatedBy: p.UpdatedBy,
	}
}

func taskFromV2(code string, t *eventsource.TaskState, ordinal int) *Task {
	labels := append([]string(nil), t.Labels...)
	sort.Strings(labels)
	return &Task{
		ID:          t.Alias,
		ProjectCode: code,
		Title:       t.Title,
		Description: t.Description,
		Labels:      labels,
		Ordinal:     ordinal,
		CreatedAt:   t.CreatedAt,
		CreatedBy:   t.CreatedBy,
		UpdatedAt:   t.UpdatedAt,
		UpdatedBy:   t.UpdatedBy,
	}
}

func commentFromV2(c *eventsource.CommentState, taskAlias, replyToAlias string, ordinal int) *Comment {
	labels := append([]string(nil), c.Labels...)
	sort.Strings(labels)
	return &Comment{
		ID:        c.Alias,
		TaskID:    taskAlias,
		ReplyTo:   replyToAlias,
		Body:      c.Body,
		Labels:    labels,
		Ordinal:   ordinal,
		CreatedAt: c.CreatedAt,
		CreatedBy: c.CreatedBy,
		UpdatedAt: c.UpdatedAt,
		UpdatedBy: c.UpdatedBy,
	}
}

func labelFromV2(l *eventsource.LabelState, ordinal int) Label {
	return Label{Name: l.Name, Description: l.Description, Expr: l.Expr, Ordinal: ordinal}
}
