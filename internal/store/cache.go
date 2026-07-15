package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// cacheSchema is the full, current cache.db shape. It carries every column the
// live cache uses, including the v2 projection columns (identity/alias) that
// were once added by ad-hoc ALTERs: the cacheSchemaVersion gate in cacheDB()
// recreates the derived tables on any schema bump, so there is no incremental
// ALTER path to keep in sync — bump cacheSchemaVersion and edit this DDL.
const cacheSchema = `
CREATE TABLE IF NOT EXISTS projects (
	code TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	ordinal INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	created_by TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	updated_by TEXT NOT NULL,
	identity TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY,
	project_code TEXT NOT NULL,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	ordinal INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	created_by TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	updated_by TEXT NOT NULL,
	identity TEXT NOT NULL DEFAULT '',
	alias TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_tasks_project_code ON tasks(project_code);
CREATE INDEX IF NOT EXISTS idx_tasks_identity ON tasks(identity);
CREATE INDEX IF NOT EXISTS idx_tasks_alias ON tasks(alias);

CREATE TABLE IF NOT EXISTS task_labels (
	task_id TEXT NOT NULL,
	label TEXT NOT NULL,
	PRIMARY KEY (task_id, label)
);
CREATE INDEX IF NOT EXISTS idx_task_labels_label ON task_labels(label);

CREATE TABLE IF NOT EXISTS labels (
	name TEXT PRIMARY KEY,
	description TEXT NOT NULL DEFAULT '',
	expr TEXT NOT NULL DEFAULT '',
	ordinal INTEGER NOT NULL DEFAULT 0,
	identity TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS comments (
	id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	reply_to TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL,
	ordinal INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	created_by TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	updated_by TEXT NOT NULL,
	identity TEXT NOT NULL DEFAULT '',
	alias TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_comments_task_id ON comments(task_id);
CREATE INDEX IF NOT EXISTS idx_comments_identity ON comments(identity);
CREATE INDEX IF NOT EXISTS idx_comments_alias ON comments(alias);

CREATE TABLE IF NOT EXISTS comment_labels (
	comment_id TEXT NOT NULL,
	label TEXT NOT NULL,
	PRIMARY KEY (comment_id, label)
);

CREATE TABLE IF NOT EXISTS meta (
	key TEXT PRIMARY KEY,
	value INTEGER NOT NULL
);
`

func (s *Store) cachePath() string { return filepath.Join(s.Root, "cache.db") }

// v2FreshnessMetaKey for the per-project last-projected v2 event count row.
func v2FreshnessMetaKey(code string) string { return "last_v2_event_count:" + code }

// cacheSetV2Freshness upserts the per-project v2 freshness row: the event
// count of the events.v2.jsonl file the cache was last projected from, keyed
// on the v2 event count since v2 files have no monotonic seq column.
func cacheSetV2Freshness(db *sql.DB, code string, eventCount int) error {
	_, err := db.Exec(`INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		v2FreshnessMetaKey(code), eventCount)
	return err
}

// cacheClearV2Freshness removes the project's v2 freshness row, so a later
// reader sees "never projected from a v2 file" rather than a stale count that
// happens to match. Called by RemoveProject alongside cacheDeleteProjectRows,
// which sweeps entity tables only and never meta.
func cacheClearV2Freshness(db *sql.DB, code string) error {
	_, err := db.Exec(`DELETE FROM meta WHERE key = ?`, v2FreshnessMetaKey(code))
	return err
}

// cacheGetV2Freshness returns the cached v2 event count and a found flag. A
// missing row returns (0, false) so the caller can tell "never projected"
// apart from "projected at count 0".
func cacheGetV2Freshness(db *sql.DB, code string) (int, bool, error) {
	var v int
	err := db.QueryRow(`SELECT value FROM meta WHERE key = ?`, v2FreshnessMetaKey(code)).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return v, true, nil
}

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
		// cache.db carries its schema shape in PRAGMA user_version. cache.db is
		// derived and rebuildable, so on any schema bump we DROP the derived
		// tables, recreate them at the current cacheSchema, and then EAGERLY
		// re-project every on-disk v2 project into the fresh tables via
		// reprojectAllV2 — we do NOT defer to ensureV2CacheFresh's per-project
		// lazy self-heal, because a list-all read (ListProjects, the TUI's
		// opening screen) reads the emptied table directly and never touches
		// ensureV2CacheFresh for any single project; without eager
		// reprojection here, a store with fully intact v2 logs would appear
		// empty until each project was individually read or `store rebuild`
		// was run. A fresh DB reports user_version 0, so it takes the same
		// path and lands at the current version. Bump cacheSchemaVersion
		// whenever cacheSchema changes shape.
		const cacheSchemaVersion = 2
		var uv int
		if err := db.QueryRow(`PRAGMA user_version`).Scan(&uv); err != nil {
			s.cacheErr = err
			return
		}
		if uv < cacheSchemaVersion {
			for _, t := range []string{"projects", "tasks", "task_labels", "labels", "comments", "comment_labels", "meta"} {
				if _, err := db.Exec(`DROP TABLE IF EXISTS ` + t); err != nil {
					s.cacheErr = err
					return
				}
			}
			if _, err := db.Exec(cacheSchema); err != nil {
				s.cacheErr = err
				return
			}
			if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, cacheSchemaVersion)); err != nil {
				s.cacheErr = err
				return
			}
			// Tables were just recreated empty, so reprojectAllV2 needs no
			// wipe first (unlike Rebuild, which wipes a possibly-populated
			// cache before calling it). MUST use the local db handle here,
			// never s.cacheDB(): we are still inside cacheOnce.Do, and
			// cacheDB() is not reentrant.
			if _, err := s.reprojectAllV2(db); err != nil {
				s.cacheErr = err
				return
			}
		}
		s.cacheDBConn = db
	})
	return s.cacheDBConn, s.cacheErr
}

// ---- project cache ----

func cacheUpsertProject(db *sql.DB, p *Project) error {
	_, err := db.Exec(`INSERT INTO projects (code, name, ordinal, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(code) DO UPDATE SET
			name=excluded.name, ordinal=excluded.ordinal,
			updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
		p.Code, p.Name, p.Ordinal, RFC3339UTC(p.CreatedAt), p.CreatedBy, RFC3339UTC(p.UpdatedAt), p.UpdatedBy)
	return err
}

func cacheGetProject(db *sql.DB, code string) (*Project, bool, error) {
	var p Project
	var createdAt, updatedAt string
	err := db.QueryRow(`SELECT code, name, ordinal, created_at, created_by, updated_at, updated_by
		FROM projects WHERE code = ?`, code).
		Scan(&p.Code, &p.Name, &p.Ordinal, &createdAt, &p.CreatedBy, &updatedAt, &p.UpdatedBy)
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
	_, err = tx.Exec(`INSERT INTO tasks (id, project_code, title, description, ordinal, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title, description=excluded.description, ordinal=excluded.ordinal,
			updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
		t.ID, t.ProjectCode, t.Title, t.Description, t.Ordinal, RFC3339UTC(t.CreatedAt), t.CreatedBy, RFC3339UTC(t.UpdatedAt), t.UpdatedBy)
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
	err := db.QueryRow(`SELECT id, project_code, title, description, ordinal, created_at, created_by, updated_at, updated_by
		FROM tasks WHERE id = ?`, id).
		Scan(&t.ID, &t.ProjectCode, &t.Title, &t.Description, &t.Ordinal, &createdAt, &t.CreatedBy, &updatedAt, &t.UpdatedBy)
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
	// Single JOIN: one row per (task, label). Assemble labels in Go. This
	// replaces the per-id N+1 (SELECT ids; then SELECT * + labels per id),
	// which at 80 tasks issued ~160 round-trips. The query returns tasks
	// with zero labels as a single NULL-label row (LEFT JOIN).
	rows, err := db.Query(`SELECT t.id, t.project_code, t.title, t.description, t.ordinal,
		t.created_at, t.created_by, t.updated_at, t.updated_by,
		tl.label
		FROM tasks t
		LEFT JOIN task_labels tl ON tl.task_id = t.id
		WHERE t.project_code = ?
		ORDER BY t.id, tl.label`, projectCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]*Task{}
	order := []string{}
	for rows.Next() {
		var (
			id, projectCode, title, description, createdAt, createdBy, updatedAt, updatedBy string
			ordinal                                                                         int
			label                                                                           sql.NullString
		)
		if err := rows.Scan(&id, &projectCode, &title, &description, &ordinal,
			&createdAt, &createdBy, &updatedAt, &updatedBy, &label); err != nil {
			return nil, err
		}
		tk, ok := byID[id]
		if !ok {
			tk = &Task{
				ID:          id,
				ProjectCode: projectCode,
				Title:       title,
				Description: description,
				Ordinal:     ordinal,
				CreatedBy:   createdBy,
				UpdatedBy:   updatedBy,
			}
			tk.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
			tk.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
			byID[id] = tk
			order = append(order, id)
		}
		if label.Valid && label.String != "" {
			tk.Labels = append(tk.Labels, label.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// ORDER BY t.id, tl.label already gives id-asc + label-asc; the Go
	// append preserves label ordering within each task. cacheListTaskIDs
	// callers expect SortTaskIDs ordering, which for fixed-code IDs is
	// numeric-asc — t.id ORDER BY in SQLite is lexicographic, so re-sort
	// to guarantee numeric-asc parity with the N+1 path.
	out := make([]*Task, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	SortTaskIDsByFunc(out)
	return out, nil
}

// SortTaskIDsByFunc sorts a slice of tasks by their ID using the canonical
// (code-asc, numeric-asc) order, matching SortTaskIDs. Kept local to avoid
// widening the store API.
func SortTaskIDsByFunc(tasks []*Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		ci, ni, _ := ParseTaskID(tasks[i].ID)
		cj, nj, _ := ParseTaskID(tasks[j].ID)
		if ci != cj {
			return ci < cj
		}
		if ni != nj {
			return ni < nj
		}
		return tasks[i].ID < tasks[j].ID
	})
}

// ---- label cache ----

func cacheUpsertLabel(db *sql.DB, l Label) error {
	_, err := db.Exec(`INSERT INTO labels (name, description, expr, ordinal) VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET description=excluded.description, expr=excluded.expr, ordinal=excluded.ordinal`,
		l.Name, l.Description, l.Expr, l.Ordinal)
	return err
}

func cacheGetLabel(db *sql.DB, name string) (Label, bool, error) {
	var l Label
	err := db.QueryRow(`SELECT name, description, expr, ordinal FROM labels WHERE name = ?`, name).
		Scan(&l.Name, &l.Description, &l.Expr, &l.Ordinal)
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
	query := `SELECT name, description, expr, ordinal FROM labels WHERE 1=1`
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
		if err := rows.Scan(&l.Name, &l.Description, &l.Expr, &l.Ordinal); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// cacheLabelUsage counts entities in projectCode carrying label — tasks and
// comments — via two indexed queries. Comments carry labels too (the
// comment:* namespace), and a label like ATM:comment:open-question can have
// zero tasks but many comments; counting only tasks showed "0 tasks" for
// labels that are genuinely in use. The total is the sum of task usage and
// comment usage so the Labels pane and `atm label list` reflect real
// adoption across all entities.
func cacheLabelUsage(db *sql.DB, projectCode, label string) (int, error) {
	var taskCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_labels tl JOIN tasks t ON t.id = tl.task_id
		WHERE tl.label = ? AND t.project_code = ?`, label, projectCode).Scan(&taskCount); err != nil {
		return 0, err
	}
	var commentCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM comment_labels cl JOIN comments c ON c.id = cl.comment_id
		JOIN tasks t ON t.id = c.task_id
		WHERE cl.label = ? AND t.project_code = ?`, label, projectCode).Scan(&commentCount); err != nil {
		return 0, err
	}
	return taskCount + commentCount, nil
}

func cacheCountTasksWithLabelGlobally(db *sql.DB, label string) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM task_labels WHERE label = ?`, label).Scan(&count)
	return count, err
}

// cacheLabelUsageGrouped returns a map of label -> usage count (tasks +
// comments) for every label in projectCode, in a single pair of grouped
// queries instead of two COUNT queries per label. Used by the TUI Labels
// pane refresh path, which previously fired 2N queries for N labels.
func cacheLabelUsageGrouped(db *sql.DB, projectCode string) (map[string]int, error) {
	out := map[string]int{}
	// Task usage grouped by label, scoped to the project's tasks.
	rows, err := db.Query(`SELECT tl.label, COUNT(*) FROM task_labels tl
		JOIN tasks t ON t.id = tl.task_id
		WHERE t.project_code = ?
		GROUP BY tl.label`, projectCode)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var label string
		var n int
		if err := rows.Scan(&label, &n); err != nil {
			rows.Close()
			return nil, err
		}
		out[label] += n
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// Comment usage grouped by label, scoped to the project's tasks.
	rows, err = db.Query(`SELECT cl.label, COUNT(*) FROM comment_labels cl
		JOIN comments c ON c.id = cl.comment_id
		JOIN tasks t ON t.id = c.task_id
		WHERE t.project_code = ?
		GROUP BY cl.label`, projectCode)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var label string
		var n int
		if err := rows.Scan(&label, &n); err != nil {
			rows.Close()
			return nil, err
		}
		out[label] += n
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	return out, nil
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
// live label rows. Its production caller, the v1 appendLabelUpsertsLocked,
// was deleted in D-Task5b; left in place (cache.go schema/query cleanup is
// D-Task6's scope) and still covered directly by TestCachePresentLabels.
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
	_, err = tx.Exec(`INSERT INTO comments (id, task_id, reply_to, body, ordinal, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			body=excluded.body, ordinal=excluded.ordinal, updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
		c.ID, c.TaskID, c.ReplyTo, c.Body, c.Ordinal, RFC3339UTC(c.CreatedAt), c.CreatedBy, RFC3339UTC(c.UpdatedAt), c.UpdatedBy)
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

func cacheCommentLabels(db *sql.DB, commentID string) ([]string, error) {
	rows, err := db.Query(`SELECT label FROM comment_labels WHERE comment_id = ? ORDER BY label`, commentID)
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

func cacheGetComment(db *sql.DB, id string) (*Comment, bool, error) {
	var c Comment
	var createdAt, updatedAt string
	err := db.QueryRow(`SELECT id, task_id, reply_to, body, ordinal, created_at, created_by, updated_at, updated_by
		FROM comments WHERE id = ?`, id).
		Scan(&c.ID, &c.TaskID, &c.ReplyTo, &c.Body, &c.Ordinal, &createdAt, &c.CreatedBy, &updatedAt, &c.UpdatedBy)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	labels, err := cacheCommentLabels(db, id)
	if err != nil {
		return nil, false, err
	}
	c.Labels = labels
	return &c, true, nil
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
	// Single JOIN: one row per (comment, label). Assemble labels in Go.
	// Replaces the per-id N+1 (SELECT ids; then SELECT * + labels per id).
	rows, err := db.Query(`SELECT c.id, c.task_id, c.reply_to, c.body, c.ordinal,
		c.created_at, c.created_by, c.updated_at, c.updated_by, cl.label
		FROM comments c
		LEFT JOIN comment_labels cl ON cl.comment_id = c.id
		WHERE c.task_id = ?
		ORDER BY c.id, cl.label`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]*Comment{}
	order := []string{}
	for rows.Next() {
		var (
			id, taskID, replyTo, body, createdAt, createdBy, updatedAt, updatedBy string
			ordinal                                                               int
			label                                                                 sql.NullString
		)
		if err := rows.Scan(&id, &taskID, &replyTo, &body, &ordinal,
			&createdAt, &createdBy, &updatedAt, &updatedBy, &label); err != nil {
			return nil, err
		}
		c, ok := byID[id]
		if !ok {
			c = &Comment{
				ID:        id,
				TaskID:    taskID,
				ReplyTo:   replyTo,
				Body:      body,
				Ordinal:   ordinal,
				CreatedBy: createdBy,
				UpdatedBy: updatedBy,
			}
			c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
			c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
			byID[id] = c
			order = append(order, id)
		}
		if label.Valid && label.String != "" {
			c.Labels = append(c.Labels, label.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*Comment, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	// ORDER BY c.id gives lexicographic ordering; comment IDs share the
	// task prefix so lex == numeric-asc within a task. Caller (task detail
	// render) expects id-asc; keep that.
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
