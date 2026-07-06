package store

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const cacheSchema = `
CREATE TABLE IF NOT EXISTS projects (
	code TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	next_task_n INTEGER NOT NULL,
	log_seq INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	created_by TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	updated_by TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY,
	project_code TEXT NOT NULL,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	log_seq INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	created_by TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	updated_by TEXT NOT NULL,
	next_comment_n INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_tasks_project_code ON tasks(project_code);

CREATE TABLE IF NOT EXISTS task_labels (
	task_id TEXT NOT NULL,
	label TEXT NOT NULL,
	PRIMARY KEY (task_id, label)
);
CREATE INDEX IF NOT EXISTS idx_task_labels_label ON task_labels(label);

CREATE TABLE IF NOT EXISTS labels (
	name TEXT PRIMARY KEY,
	description TEXT NOT NULL DEFAULT '',
	log_seq INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS comments (
	id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	reply_to TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL,
	log_seq INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	created_by TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	updated_by TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_comments_task_id ON comments(task_id);

CREATE TABLE IF NOT EXISTS comment_labels (
	comment_id TEXT NOT NULL,
	label TEXT NOT NULL,
	PRIMARY KEY (comment_id, label)
);
`

func (s *Store) cachePath() string { return filepath.Join(s.Root, "cache.db") }

// cacheDB returns the store's single shared *sql.DB, opening and migrating it
// on first use. WAL mode lets short-lived CLI invocations for different
// projects avoid contending on reads; MaxOpenConns(1) keeps this process's
// writes serialized so SQLite's own busy_timeout handles cross-process
// contention instead of surfacing SQLITE_BUSY.
func (s *Store) cacheDB() (*sql.DB, error) {
	s.cacheOnce.Do(func() {
		db, err := sql.Open("sqlite", s.cachePath())
		if err != nil {
			s.cacheErr = err
			return
		}
		db.SetMaxOpenConns(1)
		if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
			s.cacheErr = err
			return
		}
		if _, err := db.Exec(`PRAGMA busy_timeout=5000;`); err != nil {
			s.cacheErr = err
			return
		}
		if _, err := db.Exec(cacheSchema); err != nil {
			s.cacheErr = err
			return
		}
		s.cacheDBConn = db
	})
	return s.cacheDBConn, s.cacheErr
}

// ---- project cache ----

func cacheUpsertProject(db *sql.DB, p *Project) error {
	_, err := db.Exec(`INSERT INTO projects (code, name, next_task_n, log_seq, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(code) DO UPDATE SET
			name=excluded.name, next_task_n=excluded.next_task_n, log_seq=excluded.log_seq,
			updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
		p.Code, p.Name, p.NextTaskN, p.LogSeq, RFC3339UTC(p.CreatedAt), p.CreatedBy, RFC3339UTC(p.UpdatedAt), p.UpdatedBy)
	return err
}

func cacheGetProject(db *sql.DB, code string) (*Project, bool, error) {
	var p Project
	var createdAt, updatedAt string
	err := db.QueryRow(`SELECT code, name, next_task_n, log_seq, created_at, created_by, updated_at, updated_by
		FROM projects WHERE code = ?`, code).
		Scan(&p.Code, &p.Name, &p.NextTaskN, &p.LogSeq, &createdAt, &p.CreatedBy, &updatedAt, &p.UpdatedBy)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &p, true, nil
}

func cacheDeleteProject(db *sql.DB, code string) error {
	_, err := db.Exec(`DELETE FROM projects WHERE code = ?`, code)
	return err
}

func cacheListProjectCodes(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT code FROM projects ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, rows.Err()
}

// ---- task cache ----

func cacheUpsertTask(db *sql.DB, t *Task) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO tasks (id, project_code, title, description, log_seq, created_at, created_by, updated_at, updated_by, next_comment_n)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title, description=excluded.description, log_seq=excluded.log_seq,
			updated_at=excluded.updated_at, updated_by=excluded.updated_by, next_comment_n=excluded.next_comment_n`,
		t.ID, t.ProjectCode, t.Title, t.Description, t.LogSeq, RFC3339UTC(t.CreatedAt), t.CreatedBy, RFC3339UTC(t.UpdatedAt), t.UpdatedBy, t.NextCommentN)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM task_labels WHERE task_id = ?`, t.ID); err != nil {
		return err
	}
	for _, l := range t.Labels {
		if _, err = tx.Exec(`INSERT INTO task_labels (task_id, label) VALUES (?, ?)`, t.ID, l); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func cacheTaskLabels(db *sql.DB, taskID string) ([]string, error) {
	rows, err := db.Query(`SELECT label FROM task_labels WHERE task_id = ? ORDER BY label`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func cacheGetTask(db *sql.DB, id string) (*Task, bool, error) {
	var t Task
	var createdAt, updatedAt string
	err := db.QueryRow(`SELECT id, project_code, title, description, log_seq, created_at, created_by, updated_at, updated_by, next_comment_n
		FROM tasks WHERE id = ?`, id).
		Scan(&t.ID, &t.ProjectCode, &t.Title, &t.Description, &t.LogSeq, &createdAt, &t.CreatedBy, &updatedAt, &t.UpdatedBy, &t.NextCommentN)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	labels, err := cacheTaskLabels(db, id)
	if err != nil {
		return nil, false, err
	}
	t.Labels = labels
	return &t, true, nil
}

func cacheDeleteTask(db *sql.DB, id string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM task_labels WHERE task_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM tasks WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func cacheListTaskIDs(db *sql.DB, projectCode string) ([]string, error) {
	rows, err := db.Query(`SELECT id FROM tasks WHERE project_code = ?`, projectCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	SortTaskIDs(out)
	return out, nil
}

func cacheListTasksForProject(db *sql.DB, projectCode string) ([]*Task, error) {
	ids, err := cacheListTaskIDs(db, projectCode)
	if err != nil {
		return nil, err
	}
	out := make([]*Task, 0, len(ids))
	for _, id := range ids {
		t, ok, err := cacheGetTask(db, id)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// ---- label cache ----

func cacheUpsertLabel(db *sql.DB, l Label) error {
	_, err := db.Exec(`INSERT INTO labels (name, description, log_seq) VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET description=excluded.description, log_seq=excluded.log_seq`,
		l.Name, l.Description, l.LogSeq)
	return err
}

func cacheGetLabel(db *sql.DB, name string) (Label, bool, error) {
	var l Label
	err := db.QueryRow(`SELECT name, description, log_seq FROM labels WHERE name = ?`, name).
		Scan(&l.Name, &l.Description, &l.LogSeq)
	if err == sql.ErrNoRows {
		return Label{}, false, nil
	}
	if err != nil {
		return Label{}, false, err
	}
	return l, true, nil
}

func cacheDeleteLabel(db *sql.DB, name string) error {
	_, err := db.Exec(`DELETE FROM labels WHERE name = ?`, name)
	return err
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func cacheListLabels(db *sql.DB, projectPrefix, namespacePrefix string) ([]Label, error) {
	query := `SELECT name, description, log_seq FROM labels WHERE 1=1`
	var args []any
	if projectPrefix != "" {
		query += ` AND name LIKE ? ESCAPE '\'`
		args = append(args, escapeLike(projectPrefix)+":%")
	}
	if namespacePrefix != "" {
		query += ` AND name LIKE ? ESCAPE '\'`
		args = append(args, escapeLike(projectPrefix)+":"+escapeLike(namespacePrefix)+":%")
	}
	query += ` ORDER BY name`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Label
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.Name, &l.Description, &l.LogSeq); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// cacheLabelUsage counts tasks in projectCode carrying label — one indexed
// query, replacing the old per-task GetTask scan (ATM-0027-c0003).
func cacheLabelUsage(db *sql.DB, projectCode, label string) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM task_labels tl JOIN tasks t ON t.id = tl.task_id
		WHERE tl.label = ? AND t.project_code = ?`, label, projectCode).Scan(&count)
	return count, err
}

func cacheCountTasksWithLabelGlobally(db *sql.DB, label string) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM task_labels WHERE label = ?`, label).Scan(&count)
	return count, err
}

func cacheNamespaces(db *sql.DB, code string) ([]string, error) {
	prefix := code + ":"
	rows, err := db.Query(`SELECT name FROM labels WHERE name LIKE ? ESCAPE '\'`, escapeLike(prefix)+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		rest := strings.TrimPrefix(name, prefix)
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) == 2 && !seen[parts[0]] {
			seen[parts[0]] = true
			out = append(out, parts[0])
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// cachePresentLabels returns the subset of names that currently exist as
// live label rows. Used by appendLabelUpsertsLocked to decide which labels
// need a new label.upserted log entry (replaces the old full-log-Replay
// based labelsInLogLocked).
func cachePresentLabels(db *sql.DB, names []string) (map[string]bool, error) {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		_, ok, err := cacheGetLabel(db, n)
		if err != nil {
			return nil, err
		}
		if ok {
			out[n] = true
		}
	}
	return out, nil
}

// ---- comment cache ----

func cacheUpsertComment(db *sql.DB, c *Comment) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO comments (id, task_id, reply_to, body, log_seq, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			body=excluded.body, log_seq=excluded.log_seq, updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
		c.ID, c.TaskID, c.ReplyTo, c.Body, c.LogSeq, RFC3339UTC(c.CreatedAt), c.CreatedBy, RFC3339UTC(c.UpdatedAt), c.UpdatedBy)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM comment_labels WHERE comment_id = ?`, c.ID); err != nil {
		return err
	}
	for _, l := range c.Labels {
		if _, err = tx.Exec(`INSERT INTO comment_labels (comment_id, label) VALUES (?, ?)`, c.ID, l); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func cacheGetComment(db *sql.DB, id string) (*Comment, bool, error) {
	var c Comment
	var createdAt, updatedAt string
	err := db.QueryRow(`SELECT id, task_id, reply_to, body, log_seq, created_at, created_by, updated_at, updated_by
		FROM comments WHERE id = ?`, id).
		Scan(&c.ID, &c.TaskID, &c.ReplyTo, &c.Body, &c.LogSeq, &createdAt, &c.CreatedBy, &updatedAt, &c.UpdatedBy)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	rows, err := db.Query(`SELECT label FROM comment_labels WHERE comment_id = ? ORDER BY label`, id)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, false, err
		}
		c.Labels = append(c.Labels, l)
	}
	return &c, true, rows.Err()
}

func cacheDeleteComment(db *sql.DB, id string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM comment_labels WHERE comment_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM comments WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func cacheListComments(db *sql.DB, taskID string) ([]*Comment, error) {
	rows, err := db.Query(`SELECT id FROM comments WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rerr := rows.Err()
	rows.Close()
	if rerr != nil {
		return nil, rerr
	}
	out := make([]*Comment, 0, len(ids))
	for _, id := range ids {
		c, ok, err := cacheGetComment(db, id)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// cacheListCommentIDsForProject lists all comment IDs belonging to any task
// in projectCode — used by VerifyProject to sweep orphan comment rows.
func cacheListCommentIDsForProject(db *sql.DB, projectCode string) ([]string, error) {
	rows, err := db.Query(`SELECT c.id FROM comments c JOIN tasks t ON t.id = c.task_id WHERE t.project_code = ?`, projectCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
