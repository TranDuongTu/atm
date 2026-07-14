package store

import (
	"database/sql"
	"sort"

	"atm/internal/eventsource"
)

// cacheProjectFromV2State replaces the project's cache rows with the live
// entities of a v2 fold. eventCount is the number of events in the file the
// fold came from; it is the v2 freshness key.
func (s *Store) cacheProjectFromV2State(code string, st *eventsource.State, eventCount int) error {
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
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
		if c, ok := st.Comments[id]; ok {
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

// cacheDeleteProjectRows removes the project's task/comment rows and the
// project row itself — the per-project mirror of the global wipe Rebuild
// does. Labels stay: the labels table is store-global (merged across
// projects), so only tombstoned names are deleted, above.
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
	return nil
}

func projectFromV2(p *eventsource.ProjectState) *Project {
	// NextTaskN and LogSeq are v1 bookkeeping; they are meaningless for a
	// v2-active project, and every v2 read path must branch by format
	// before the v1 freshness checks that would read them (Task 9).
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
		LogSeq:      ordinal,
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
		LogSeq:    ordinal,
		CreatedAt: c.CreatedAt,
		CreatedBy: c.CreatedBy,
		UpdatedAt: c.UpdatedAt,
		UpdatedBy: c.UpdatedBy,
	}
}

func labelFromV2(l *eventsource.LabelState, ordinal int) Label {
	return Label{Name: l.Name, Description: l.Description, Expr: l.Expr, LogSeq: ordinal}
}
