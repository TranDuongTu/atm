# Cache DB Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace ATM's per-entity JSON cache files (`projects/<CODE>.json`, `projects/<CODE>/tasks/<ID>.json`, `projects/<CODE>/comments/<ID>.json`, top-level `labels.json`) with a single local SQLite file, `$ATM_HOME/cache.db`, fixing the `labels.refresh` TUI perf bug (O(labels×tasks) rescans + double log-parse per task) via indexed queries, while `log.jsonl` per project stays the untouched source of truth.

**Architecture:** New `internal/store/cache.go` owns a `*sql.DB` (driver: `modernc.org/sqlite`, pure Go) with five tables (`projects`, `tasks`, `task_labels`, `labels`, `comments`, `comment_labels`) and package-private CRUD helper functions. `project.go`, `task.go`, `label.go`, `comment.go`, `query.go`, `rebuild.go`, `verify.go` are rewired one at a time to call these helpers instead of `WriteJSON`/`ReadJSON` against per-entity files. `log.jsonl`, `AppendLog`, `ReadLog`, `Replay`, `WithLock`, and the closed action enum are untouched. `cache.db` is fully derived and disposable — `atm store rebuild` regenerates it from the logs — exactly like today's per-file caches.

**Tech Stack:** Go 1.22, `database/sql` + `modernc.org/sqlite` (pure-Go SQLite driver, no cgo), existing `spf13/cobra` CLI, existing test conventions (table-driven, `newTestStore(t)` helper in `internal/store/project_test.go`).

## Global Constraints

- No change to `log.jsonl` format, `AppendLog`/`ReadLog`/`Replay`/`LastLogSeq`/`History` semantics, the closed action enum, or `WithLock`/per-project file locking (per `docs/superpowers/specs/2026-07-04-audit-log-redesign-design.md`).
- `cache.db` is derived and disposable — never hand-edited in production, only via the store's own write paths; tests may hand-edit it directly to simulate corruption/staleness, matching how existing tests hand-edit JSON cache files today.
- `make verify` (`make build && make test`) must pass before each task's commit, per AGENTS.md. The tree does not need to build between every intermediate step within a task, but must build and pass tests at each task's commit boundary.
- Every mutation still appends to `log.jsonl` **first** (the commit point), then writes through to `cache.db` — this ordering is unchanged from today's log-then-cache-file pattern.
- Follow existing package conventions: `store` package is white-box tested (`package store` in `_test.go` files, so tests may call unexported helpers directly); `cli` package tests use `newTestCLI(t)` from `internal/cli/store_test.go`.

---

### Task 1: `cache.go` — schema, DB handle, project cache helpers

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `internal/store/store.go:13-15` (add fields to `Store` struct)
- Create: `internal/store/cache.go`
- Create: `internal/store/cache_test.go`

**Interfaces:**
- Produces: `(s *Store) cachePath() string`, `(s *Store) cacheDB() (*sql.DB, error)`, `cacheUpsertProject(db *sql.DB, p *Project) error`, `cacheGetProject(db *sql.DB, code string) (*Project, bool, error)`, `cacheDeleteProject(db *sql.DB, code string) error`, `cacheListProjectCodes(db *sql.DB) ([]string, error)`. Later tasks add more `cache*` helpers to this same file.

- [ ] **Step 1: Add the SQLite driver dependency**

Run: `cd /home/ttran/projects/scyllas/atm && go get modernc.org/sqlite@latest && go mod tidy`
Expected: `go.mod` gains a `require modernc.org/sqlite vX.Y.Z` line (direct), `go.sum` updated. No errors.

- [ ] **Step 2: Add DB-handle fields to `Store`**

Edit `internal/store/store.go` — change the `Store` struct (currently just `Root string`) and add the import:

```go
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

type Store struct {
	Root string

	cacheOnce   sync.Once
	cacheDBConn *sql.DB
	cacheErr    error
}
```

- [ ] **Step 3: Write `internal/store/cache.go`**

```go
package store

import (
	"database/sql"
	"path/filepath"
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
```

- [ ] **Step 4: Write `internal/store/cache_test.go`**

```go
package store

import "testing"

func TestCacheProjectUpsertGetRoundTrip(t *testing.T) {
	s := newTestStore(t)
	db, err := s.cacheDB()
	if err != nil {
		t.Fatal(err)
	}
	now := Now()
	p := &Project{Code: "ATM", Name: "x", NextTaskN: 3, LogSeq: 5, CreatedAt: now, CreatedBy: "c", UpdatedAt: now, UpdatedBy: "c"}
	if err := cacheUpsertProject(db, p); err != nil {
		t.Fatal(err)
	}
	got, ok, err := cacheGetProject(db, "ATM")
	if err != nil || !ok {
		t.Fatalf("cacheGetProject: ok=%v err=%v", ok, err)
	}
	if got.NextTaskN != 3 || got.LogSeq != 5 {
		t.Fatalf("got = %+v", got)
	}
}

func TestCacheProjectGetMissing(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	_, ok, err := cacheGetProject(db, "NOPE")
	if err != nil || ok {
		t.Fatalf("expected ok=false err=nil, got ok=%v err=%v", ok, err)
	}
}

func TestCacheProjectUpsertOverwrites(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	p := &Project{Code: "ATM", Name: "x", NextTaskN: 1, LogSeq: 1, CreatedAt: now, CreatedBy: "c", UpdatedAt: now, UpdatedBy: "c"}
	_ = cacheUpsertProject(db, p)
	p.NextTaskN = 9
	p.LogSeq = 9
	_ = cacheUpsertProject(db, p)
	got, _, _ := cacheGetProject(db, "ATM")
	if got.NextTaskN != 9 || got.LogSeq != 9 {
		t.Fatalf("upsert did not overwrite: %+v", got)
	}
}

func TestCacheDeleteProject(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	_ = cacheUpsertProject(db, &Project{Code: "ATM", Name: "x", CreatedAt: now, UpdatedAt: now})
	if err := cacheDeleteProject(db, "ATM"); err != nil {
		t.Fatal(err)
	}
	_, ok, _ := cacheGetProject(db, "ATM")
	if ok {
		t.Fatal("project row still present after delete")
	}
}

func TestCacheListProjectCodesSorted(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	_ = cacheUpsertProject(db, &Project{Code: "ZZZ", CreatedAt: now, UpdatedAt: now})
	_ = cacheUpsertProject(db, &Project{Code: "AAA", CreatedAt: now, UpdatedAt: now})
	codes, err := cacheListProjectCodes(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 2 || codes[0] != "AAA" || codes[1] != "ZZZ" {
		t.Fatalf("codes = %v", codes)
	}
}
```

- [ ] **Step 5: Run the new tests**

Run: `go test ./internal/store/... -run TestCacheProject -v`
Expected: all 5 tests PASS.

- [ ] **Step 6: Build and full test suite**

Run: `go build ./... && go test ./...`
Expected: builds clean; all existing tests still PASS (nothing else references `cache.go` yet, so no regressions expected).

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/store/store.go internal/store/cache.go internal/store/cache_test.go
git commit -m "$(cat <<'EOF'
Add cache.db schema and project cache helpers

First layer of the JSON-cache-to-SQLite consolidation
(docs/superpowers/specs/2026-07-06-atm-storage-sync-design.md). Adds
the modernc.org/sqlite driver, the full cache.db schema, and
project-row CRUD helpers. Nothing calls these yet — project.go still
uses the JSON cache files until Task 5.
EOF
)"
```

---

### Task 2: `cache.go` — task + task_labels cache helpers

**Files:**
- Modify: `internal/store/cache.go`
- Modify: `internal/store/cache_test.go`

**Interfaces:**
- Consumes: schema/`cacheDB()` from Task 1.
- Produces: `cacheUpsertTask(db *sql.DB, t *Task) error`, `cacheGetTask(db *sql.DB, id string) (*Task, bool, error)`, `cacheDeleteTask(db *sql.DB, id string) error`, `cacheListTaskIDs(db *sql.DB, projectCode string) ([]string, error)`, `cacheListTasksForProject(db *sql.DB, projectCode string) ([]*Task, error)`.

- [ ] **Step 1: Append task helpers to `cache.go`**

```go
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
```

- [ ] **Step 2: Append task helper tests to `cache_test.go`**

```go
func TestCacheTaskUpsertGetRoundTripWithLabels(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	tk := &Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t", Labels: []string{"ATM:type:bug", "ATM:status:open"},
		LogSeq: 3, CreatedAt: now, CreatedBy: "c", UpdatedAt: now, UpdatedBy: "c"}
	if err := cacheUpsertTask(db, tk); err != nil {
		t.Fatal(err)
	}
	got, ok, err := cacheGetTask(db, "ATM-0001")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(got.Labels) != 2 || got.Labels[0] != "ATM:status:open" || got.Labels[1] != "ATM:type:bug" {
		t.Fatalf("labels = %v", got.Labels)
	}
}

func TestCacheTaskUpsertReplacesLabels(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	tk := &Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t", Labels: []string{"ATM:type:bug"}, CreatedAt: now, UpdatedAt: now}
	_ = cacheUpsertTask(db, tk)
	tk.Labels = []string{"ATM:status:open"}
	_ = cacheUpsertTask(db, tk)
	got, _, _ := cacheGetTask(db, "ATM-0001")
	if len(got.Labels) != 1 || got.Labels[0] != "ATM:status:open" {
		t.Fatalf("labels not replaced: %v", got.Labels)
	}
}

func TestCacheDeleteTaskRemovesLabels(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	tk := &Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t", Labels: []string{"ATM:type:bug"}, CreatedAt: now, UpdatedAt: now}
	_ = cacheUpsertTask(db, tk)
	if err := cacheDeleteTask(db, "ATM-0001"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cacheGetTask(db, "ATM-0001"); ok {
		t.Fatal("task row still present")
	}
	labels, _ := cacheTaskLabels(db, "ATM-0001")
	if len(labels) != 0 {
		t.Fatalf("task_labels rows not cleaned up: %v", labels)
	}
}

func TestCacheListTaskIDsScopedByProjectAndSorted(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	_ = cacheUpsertTask(db, &Task{ID: "ATM-0002", ProjectCode: "ATM", Title: "b", CreatedAt: now, UpdatedAt: now})
	_ = cacheUpsertTask(db, &Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "a", CreatedAt: now, UpdatedAt: now})
	_ = cacheUpsertTask(db, &Task{ID: "OTH-0001", ProjectCode: "OTH", Title: "c", CreatedAt: now, UpdatedAt: now})
	ids, err := cacheListTaskIDs(db, "ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "ATM-0001" || ids[1] != "ATM-0002" {
		t.Fatalf("ids = %v", ids)
	}
}
```

- [ ] **Step 3: Run and verify**

Run: `go test ./internal/store/... -run TestCacheTask -v && go test ./internal/store/... -run TestCacheListTaskIDs -v`
Expected: all PASS.

- [ ] **Step 4: Build and full test suite**

Run: `go build ./... && go test ./...`
Expected: clean, no regressions.

- [ ] **Step 5: Commit**

```bash
git add internal/store/cache.go internal/store/cache_test.go
git commit -m "$(cat <<'EOF'
Add task + task_labels cache helpers

Second layer of cache.db consolidation. task.go still uses the JSON
cache files until Task 6.
EOF
)"
```

---

### Task 3: `cache.go` — label cache helpers (the perf-bug fix)

**Files:**
- Modify: `internal/store/cache.go`
- Modify: `internal/store/cache_test.go`

**Interfaces:**
- Produces: `cacheUpsertLabel(db *sql.DB, l Label) error`, `cacheGetLabel(db *sql.DB, name string) (Label, bool, error)`, `cacheDeleteLabel(db *sql.DB, name string) error`, `cacheListLabels(db *sql.DB, projectPrefix, namespacePrefix string) ([]Label, error)`, `cacheLabelUsage(db *sql.DB, projectCode, label string) (int, error)`, `cacheCountTasksWithLabelGlobally(db *sql.DB, label string) (int, error)`, `cacheNamespaces(db *sql.DB, code string) ([]string, error)`, `cachePresentLabels(db *sql.DB, names []string) (map[string]bool, error)`.

`cacheLabelUsage` is the direct fix for ATM-0027-c0003: today's `LabelUsage` calls `GetTask` once per task (each doing a fresh log parse for staleness). This is one indexed `COUNT` query.

- [ ] **Step 1: Append label helpers to `cache.go`**

```go
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
```

Add `"sort"` and `"strings"` to `cache.go`'s import block.

- [ ] **Step 2: Append label helper tests to `cache_test.go`**

```go
func TestCacheLabelUpsertGetDelete(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	if err := cacheUpsertLabel(db, Label{Name: "ATM:type:bug", Description: "d", LogSeq: 1}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := cacheGetLabel(db, "ATM:type:bug")
	if err != nil || !ok || got.Description != "d" {
		t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
	}
	if err := cacheDeleteLabel(db, "ATM:type:bug"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cacheGetLabel(db, "ATM:type:bug"); ok {
		t.Fatal("label still present after delete")
	}
}

func TestCacheListLabelsFiltersByProjectAndNamespace(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	_ = cacheUpsertLabel(db, Label{Name: "ATM:type:bug"})
	_ = cacheUpsertLabel(db, Label{Name: "ATM:status:open"})
	_ = cacheUpsertLabel(db, Label{Name: "OTH:type:bug"})
	got, err := cacheListLabels(db, "ATM", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 ATM labels, got %d: %v", len(got), got)
	}
	got, _ = cacheListLabels(db, "ATM", "type")
	if len(got) != 1 || got[0].Name != "ATM:type:bug" {
		t.Fatalf("namespace filter = %v", got)
	}
}

func TestCacheLabelUsageCountsOnlyMatchingProject(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	_ = cacheUpsertTask(db, &Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "a", Labels: []string{"ATM:type:bug"}, CreatedAt: now, UpdatedAt: now})
	_ = cacheUpsertTask(db, &Task{ID: "ATM-0002", ProjectCode: "ATM", Title: "b", Labels: []string{"ATM:type:bug"}, CreatedAt: now, UpdatedAt: now})
	_ = cacheUpsertTask(db, &Task{ID: "ATM-0003", ProjectCode: "ATM", Title: "c", CreatedAt: now, UpdatedAt: now})
	count, err := cacheLabelUsage(db, "ATM", "ATM:type:bug")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}

func TestCacheNamespacesDistinctSorted(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	_ = cacheUpsertLabel(db, Label{Name: "ATM:type:bug"})
	_ = cacheUpsertLabel(db, Label{Name: "ATM:type:feature"})
	_ = cacheUpsertLabel(db, Label{Name: "ATM:status:open"})
	ns, err := cacheNamespaces(db, "ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 2 || ns[0] != "status" || ns[1] != "type" {
		t.Fatalf("namespaces = %v", ns)
	}
}

func TestCachePresentLabels(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	_ = cacheUpsertLabel(db, Label{Name: "ATM:type:bug"})
	present, err := cachePresentLabels(db, []string{"ATM:type:bug", "ATM:type:feature"})
	if err != nil {
		t.Fatal(err)
	}
	if !present["ATM:type:bug"] || present["ATM:type:feature"] {
		t.Fatalf("present = %v", present)
	}
}
```

- [ ] **Step 3: Run and verify**

Run: `go test ./internal/store/... -run TestCacheLabel -v && go test ./internal/store/... -run TestCacheNamespaces -v && go test ./internal/store/... -run TestCachePresentLabels -v`
Expected: all PASS.

- [ ] **Step 4: Build and full test suite**

Run: `go build ./... && go test ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/store/cache.go internal/store/cache_test.go
git commit -m "$(cat <<'EOF'
Add label cache helpers, including the indexed LabelUsage query

cacheLabelUsage is the direct fix for the labels.refresh perf bug
(ATM-0027-c0003): one COUNT query instead of a per-task GetTask scan.
label.go still uses labels.json until Task 7.
EOF
)"
```

---

### Task 4: `cache.go` — comment + comment_labels cache helpers

**Files:**
- Modify: `internal/store/cache.go`
- Modify: `internal/store/cache_test.go`

**Interfaces:**
- Produces: `cacheUpsertComment(db *sql.DB, c *Comment) error`, `cacheGetComment(db *sql.DB, id string) (*Comment, bool, error)`, `cacheDeleteComment(db *sql.DB, id string) error`, `cacheListComments(db *sql.DB, taskID string) ([]*Comment, error)`, `cacheListCommentIDsForProject(db *sql.DB, projectCode string) ([]string, error)`.

- [ ] **Step 1: Append comment helpers to `cache.go`**

```go
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
```

- [ ] **Step 2: Append comment helper tests to `cache_test.go`**

```go
func TestCacheCommentUpsertGetWithLabels(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	_ = cacheUpsertTask(db, &Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t", CreatedAt: now, UpdatedAt: now})
	c := &Comment{ID: "ATM-0001-c0001", TaskID: "ATM-0001", Body: "hi", Labels: []string{"ATM:tag:x"}, CreatedAt: now, UpdatedAt: now}
	if err := cacheUpsertComment(db, c); err != nil {
		t.Fatal(err)
	}
	got, ok, err := cacheGetComment(db, "ATM-0001-c0001")
	if err != nil || !ok || got.Body != "hi" || len(got.Labels) != 1 {
		t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
	}
}

func TestCacheDeleteCommentRemovesLabels(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	c := &Comment{ID: "ATM-0001-c0001", TaskID: "ATM-0001", Body: "hi", Labels: []string{"ATM:tag:x"}, CreatedAt: now, UpdatedAt: now}
	_ = cacheUpsertComment(db, c)
	if err := cacheDeleteComment(db, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cacheGetComment(db, c.ID); ok {
		t.Fatal("comment row still present")
	}
}

func TestCacheListCommentsSortedByID(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	_ = cacheUpsertComment(db, &Comment{ID: "ATM-0001-c0002", TaskID: "ATM-0001", Body: "b", CreatedAt: now, UpdatedAt: now})
	_ = cacheUpsertComment(db, &Comment{ID: "ATM-0001-c0001", TaskID: "ATM-0001", Body: "a", CreatedAt: now, UpdatedAt: now})
	got, err := cacheListComments(db, "ATM-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "ATM-0001-c0001" {
		t.Fatalf("got = %+v", got)
	}
}

func TestCacheListCommentIDsForProject(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	_ = cacheUpsertTask(db, &Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t", CreatedAt: now, UpdatedAt: now})
	_ = cacheUpsertComment(db, &Comment{ID: "ATM-0001-c0001", TaskID: "ATM-0001", Body: "a", CreatedAt: now, UpdatedAt: now})
	ids, err := cacheListCommentIDsForProject(db, "ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "ATM-0001-c0001" {
		t.Fatalf("ids = %v", ids)
	}
}
```

- [ ] **Step 3: Run and verify**

Run: `go test ./internal/store/... -run TestCacheComment -v`
Expected: all PASS.

- [ ] **Step 4: Build and full test suite**

Run: `go build ./... && go test ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/store/cache.go internal/store/cache_test.go
git commit -m "$(cat <<'EOF'
Add comment + comment_labels cache helpers

Final cache.go layer. All five tables now have CRUD helpers; no
production code calls them yet — that starts with Task 5.
EOF
)"
```

---

### Task 5: Rewire `project.go` onto `cache.db`

**Files:**
- Modify: `internal/store/project.go`
- Modify: `internal/store/store.go` (add `projectCodesOnDisk`)
- Modify: `internal/store/project_test.go:184-195`

**Interfaces:**
- Consumes: `cacheUpsertProject`, `cacheGetProject`, `cacheDeleteProject`, `cacheListProjectCodes`, `cacheListTaskIDs` (Tasks 1-2), `(s *Store) cacheDB()`.
- Produces: `(s *Store) projectCodesOnDisk() ([]string, error)` (enumerates project codes from the `projects/` directory structure — independent of `cache.db`, needed by `Rebuild`/`Verify` in Tasks 10-11 to work even when the cache is empty/corrupt).

- [ ] **Step 1: Add `projectCodesOnDisk` to `store.go`**

Append to `internal/store/store.go`:

```go
// projectCodesOnDisk enumerates project codes by the projects/<CODE>/
// directory structure (which holds log.jsonl), independent of cache.db.
// Used by Verify/Rebuild so a missing or fully-wiped cache.db doesn't hide
// projects that still have logs on disk.
func (s *Store) projectCodesOnDisk() ([]string, error) {
	entries, err := os.ReadDir(s.projectsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var codes []string
	for _, e := range entries {
		if e.IsDir() {
			codes = append(codes, e.Name())
		}
	}
	sort.Strings(codes)
	return codes, nil
}
```

- [ ] **Step 2: Rewrite `internal/store/project.go`**

```go
package store

import (
	"encoding/json"
	"fmt"
	"os"
)

func (s *Store) CreateProject(code, name, actor string) (*Project, error) {
	if err := ValidateProjectCode(code); err != nil {
		return nil, err
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var created *Project
	err = s.WithLock(code, func() error {
		if _, ok, err := cacheGetProject(db, code); err != nil {
			return err
		} else if ok {
			return fmt.Errorf("%w: project %q already exists", ErrConflict, code)
		}
		now := Now()
		p := &Project{
			Code:      code,
			Name:      name,
			NextTaskN: 1,
			CreatedAt: now,
			CreatedBy: actor,
			UpdatedAt: now,
			UpdatedBy: actor,
			LogSeq:    0,
		}
		// 1. Append project.created to log.
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionProjectCreated,
			Subject: Subject{Kind: "project", Code: code},
			Payload: mustMarshal(p),
		})
		if err != nil {
			return err
		}
		p.LogSeq = entry.Seq
		// 2. Seed default labels (appends label.upserted per default label).
		if err := s.seedLabelsLocked(code, actor, now); err != nil {
			return err
		}
		// 3. Write project cache row.
		if err := cacheUpsertProject(db, p); err != nil {
			return err
		}
		created = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func mustMarshal(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}

func (s *Store) GetProject(code string) (*Project, error) {
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	p, ok, err := cacheGetProject(db, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		if err := s.WithLock(code, func() error { return s.rebuildProjectFromLog(code) }); err != nil {
			return nil, err
		}
		p, ok, err = cacheGetProject(db, code)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: project %q", ErrNotFound, code)
		}
		return p, nil
	}
	last, lastErr := s.LastLogSeq(code)
	if lastErr != nil {
		return nil, lastErr
	}
	if p.LogSeq > last {
		return nil, fmt.Errorf("%w: project %q cache LogSeq=%d > log LastSeq=%d", ErrIntegrity, code, p.LogSeq, last)
	}
	projLast, err := s.lastProjectEventSeq(code)
	if err != nil {
		return nil, err
	}
	if projLast > p.LogSeq {
		if err := s.WithLock(code, func() error { return s.rebuildProjectFromLog(code) }); err != nil {
			return nil, err
		}
		p, ok, err = cacheGetProject(db, code)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: project %q", ErrNotFound, code)
		}
	}
	return p, nil
}

// lastProjectEventSeq returns the seq of the latest project.* log entry.
func (s *Store) lastProjectEventSeq(code string) (int, error) {
	entries, err := s.ReadLog(code)
	if err != nil {
		return 0, err
	}
	last := 0
	for _, e := range entries {
		if e.Subject.Kind == "project" && e.Subject.Code == code {
			last = e.Seq
		}
	}
	return last, nil
}

func (s *Store) rebuildProjectFromLog(code string) error {
	entries, err := s.ReadLog(code)
	if err != nil && !IsIntegrity(err) {
		return err
	}
	var p *Project
	lastSeq := 0
	for _, e := range entries {
		if e.Subject.Kind != "project" || e.Subject.Code != code {
			continue
		}
		lastSeq = e.Seq
		if e.Action == ActionProjectRemoved {
			p = nil
			continue
		}
		var proj Project
		if err := json.Unmarshal(e.Payload, &proj); err == nil {
			p = &proj
		}
	}
	if p == nil {
		return fmt.Errorf("%w: project %q", ErrNotFound, code)
	}
	p.LogSeq = lastSeq
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return cacheUpsertProject(db, p)
}

func (s *Store) ListProjects() []*Project {
	db, err := s.cacheDB()
	if err != nil {
		return nil
	}
	codes, err := cacheListProjectCodes(db)
	if err != nil {
		return nil
	}
	var out []*Project
	for _, code := range codes {
		p, err := s.GetProject(code)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (s *Store) SetProjectName(code, name, actor string) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		p, err := s.GetProject(code)
		if err != nil {
			return err
		}
		p.Name = name
		now := Now()
		p.UpdatedAt = now
		p.UpdatedBy = actor
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionProjectNameChanged,
			Subject: Subject{Kind: "project", Code: code},
			Payload: mustMarshal(p),
		})
		if err != nil {
			return err
		}
		p.LogSeq = entry.Seq
		return cacheUpsertProject(db, p)
	})
}

func (s *Store) RemoveProject(code, actor string) error {
	if err := s.hasTasksGuard(code); err != nil {
		return err
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		if err := s.hasTasksGuard(code); err != nil {
			return err
		}
		p, err := s.GetProject(code)
		if err != nil {
			return err
		}
		now := Now()
		// 1. Append project.removed tombstone (payload = last state).
		_, _ = s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionProjectRemoved,
			Subject: Subject{Kind: "project", Code: code},
			Payload: mustMarshal(p),
		})
		// 2. Delete the project directory (including log.jsonl).
		_ = os.RemoveAll(s.projectDir(code))
		// 3. Delete the project cache row.
		return cacheDeleteProject(db, code)
	})
}

func (s *Store) hasTasksGuard(code string) error {
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	ids, err := cacheListTaskIDs(db, code)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		return fmt.Errorf("%w: project %q has tasks — remove tasks first", ErrConflict, code)
	}
	return nil
}
```

`project.go`'s final imports are `"encoding/json"`, `"fmt"`, `"os"` (as shown in the code block above).

Also delete `mutateProject` (it was already dead/unused-by-callers per the existing code's own comment "callers use SetProjectName directly; mutateProject retained for symmetry") — confirm with `grep -rn "mutateProject" internal/` that nothing calls it before deleting; if something does call it, keep it and port it to `cacheUpsertProject` the same way `SetProjectName` was ported above.

- [ ] **Step 3: Update `project_test.go`'s lazy-miss test**

Replace lines 184-195 of `internal/store/project_test.go`:

```go
func TestGetProjectLazyMissRebuildsFromLog(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	db, _ := s.cacheDB()
	_, _ = db.Exec(`DELETE FROM projects WHERE code = ?`, "ATM")
	got, err := s.GetProject("ATM")
	if err != nil {
		t.Fatalf("GetProject after cache delete: %v", err)
	}
	if got.Code != "ATM" {
		t.Fatalf("rebuilt project code = %q", got.Code)
	}
}
```

Remove the now-unused `os` import from `project_test.go` only if no other test in that file still uses `os` — check with `grep -n '"os"\|os\.' internal/store/project_test.go` first; the earlier `TestRemoveProject...` test (around line 176) uses `os.Stat(s.logPath("ATM"))`, so `os` stays imported.

- [ ] **Step 4: Build and test**

Run: `go build ./... && go test ./internal/store/... -v -run TestCreateProject|TestGetProject|TestListProjects|TestSetProjectName|TestRemoveProject`
Expected: all project-related tests PASS. (`go vet ./...` should also be run to catch unused imports.)

Run: `go build ./... && go test ./...`
Expected: full suite green — `task.go`/`label.go`/`comment.go`/`query.go`/`rebuild.go`/`verify.go` still use the OLD JSON-file helpers (`taskPath`, `commentPath`, `labelsPath` etc. are untouched in this task), so they continue to work against files exactly as before; only project-cache reads/writes moved to `cache.db`.

- [ ] **Step 5: Commit**

```bash
git add internal/store/project.go internal/store/store.go internal/store/project_test.go
git commit -m "$(cat <<'EOF'
Move project cache reads/writes onto cache.db

project.go now reads/writes project rows in cache.db instead of
projects/<CODE>.json. task.go/label.go/comment.go/query.go/rebuild.go/
verify.go still use the old JSON cache files until Tasks 6-11 — this
is the layered-rollout precedent from the v2 and audit-log-redesign
specs (tree may reference both storage paths mid-rollout; each task
commit keeps `make verify` green for the paths it owns).
EOF
)"
```

---

### Task 6: Rewire `task.go` onto `cache.db`

**Files:**
- Modify: `internal/store/task.go`
- Modify: `internal/store/task_test.go` (4 spots: the tombstone-delete test, `TestGetTaskLazyMissRebuildsFromLog`, `TestGetTaskStaleLogSeqTriggersRebuild`, `TestGetTaskFutureLogSeqIntegrity`)

**Interfaces:**
- Consumes: `cacheUpsertTask`, `cacheGetTask`, `cacheDeleteTask` (Task 2); `cachePresentLabels` (Task 3, replaces `labelsInLogLocked`).
- Produces: no new exported surface — `CreateTask`/`GetTask`/`SetTitle`/`SetDescription`/`TaskLabelAdd`/`TaskLabelRemove`/`RemoveTask` keep their existing signatures.

- [ ] **Step 1: Rewrite `internal/store/task.go`**

```go
package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

func (s *Store) CreateTask(projectCode, title, description string, labels []string, actor string) (*Task, error) {
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrUsage)
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var created *Task
	err = s.WithLock(projectCode, func() error {
		p, err := s.GetProject(projectCode)
		if err != nil {
			return err
		}
		for _, l := range labels {
			if err := ValidateLabelName(l); err != nil {
				return err
			}
			if err := s.labelProjectExists(l); err != nil {
				return err
			}
		}
		n := p.NextTaskN
		id := RenderTaskID(projectCode, n)
		ts := Now()
		t := &Task{
			ID:          id,
			ProjectCode: projectCode,
			Title:       title,
			Description: description,
			Labels:      append([]string(nil), labels...),
			CreatedAt:   ts,
			CreatedBy:   actor,
			UpdatedAt:   ts,
			UpdatedBy:   actor,
		}
		sort.Strings(t.Labels)
		// 1. Append label.upserted for any newly-registered labels (BEFORE the task event).
		labelEntries, err := s.appendLabelUpsertsLocked(projectCode, labels, actor, ts)
		if err != nil {
			return err
		}
		_ = labelEntries
		// 2. Append task.created.
		entry, err := s.appendLogLocked(projectCode, LogEntry{
			At:      ts,
			Actor:   actor,
			Action:  ActionTaskCreated,
			Subject: Subject{Kind: "task", ID: id},
			Payload: mustMarshal(t),
		})
		if err != nil {
			return err
		}
		t.LogSeq = entry.Seq
		// 3. Bump project counter and write project cache row.
		p.NextTaskN = n + 1
		p.UpdatedAt = ts
		p.UpdatedBy = actor
		if err := cacheUpsertProject(db, p); err != nil {
			return err
		}
		// 4. Write task cache row.
		if err := cacheUpsertTask(db, t); err != nil {
			return err
		}
		created = t
		return nil
	})
	return created, err
}

// appendLabelUpsertsLocked appends label.upserted for each label name not
// already live in cache.db, and write-throughs the new row immediately.
// Caller MUST hold the project lock.
func (s *Store) appendLabelUpsertsLocked(code string, labels []string, actor string, at time.Time) ([]LogEntry, error) {
	if len(labels) == 0 {
		return nil, nil
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	present, err := cachePresentLabels(db, labels)
	if err != nil {
		return nil, err
	}
	var out []LogEntry
	for _, name := range labels {
		if present[name] {
			continue
		}
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      at,
			Actor:   actor,
			Action:  ActionLabelUpserted,
			Subject: Subject{Kind: "label", Name: name},
			Payload: mustMarshal(Label{Name: name}),
		})
		if err != nil {
			return out, err
		}
		if err := cacheUpsertLabel(db, Label{Name: name, LogSeq: entry.Seq}); err != nil {
			return out, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func (s *Store) GetTask(id string) (*Task, error) {
	code, _, ok := ParseTaskID(id)
	if !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	t, found, err := cacheGetTask(db, id)
	if err != nil {
		return nil, err
	}
	if !found {
		if err := s.WithLock(code, func() error { return s.rebuildTaskFromLog(id, code) }); err != nil {
			return nil, err
		}
		t, found, err = cacheGetTask(db, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: task %q", ErrNotFound, id)
		}
		return t, nil
	}
	last, lastErr := s.LastLogSeq(code)
	if lastErr != nil {
		return nil, lastErr
	}
	if t.LogSeq > last {
		return nil, fmt.Errorf("%w: task %q cache LogSeq=%d > log LastSeq=%d", ErrIntegrity, id, t.LogSeq, last)
	}
	taskLast, err := s.lastTaskEventSeq(code, id)
	if err != nil {
		return nil, err
	}
	if t.LogSeq < taskLast {
		if err := s.WithLock(code, func() error { return s.rebuildTaskFromLog(id, code) }); err != nil {
			return nil, err
		}
		t, found, err = cacheGetTask(db, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: task %q", ErrNotFound, id)
		}
	}
	return t, nil
}

func (s *Store) lastTaskEventSeq(code, id string) (int, error) {
	entries, err := s.ReadLog(code)
	if err != nil {
		return 0, err
	}
	last := 0
	for _, e := range entries {
		if e.Subject.Kind == "task" && e.Subject.ID == id {
			last = e.Seq
		}
	}
	return last, nil
}

// rebuildTaskFromLog replays the task's events and rewrites the cache row.
// Caller MUST hold the project lock.
func (s *Store) rebuildTaskFromLog(id, code string) error {
	entries, err := s.ReadLog(code)
	if err != nil && !IsIntegrity(err) {
		return err
	}
	var t *Task
	lastSeq := 0
	for _, e := range entries {
		if e.Subject.Kind != "task" || e.Subject.ID != id {
			continue
		}
		lastSeq = e.Seq
		if e.Action == ActionTaskRemoved {
			t = nil
			continue
		}
		var tk Task
		if err := json.Unmarshal(e.Payload, &tk); err == nil {
			t = &tk
		}
	}
	if t == nil {
		return fmt.Errorf("%w: task %q", ErrNotFound, id)
	}
	t.LogSeq = lastSeq
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return cacheUpsertTask(db, t)
}

func (s *Store) SetTitle(id, title, actor string) error {
	if title == "" {
		return fmt.Errorf("%w: title is required", ErrUsage)
	}
	return s.mutateTask(id, actor, func(t *Task, now time.Time) {
		t.Title = title
	}, ActionTaskTitleChanged)
}

func (s *Store) SetDescription(id, description, actor string) error {
	return s.mutateTask(id, actor, func(t *Task, now time.Time) {
		t.Description = description
	}, ActionTaskDescChanged)
}

func (s *Store) TaskLabelAdd(id, label, actor string) error {
	if err := ValidateLabelName(label); err != nil {
		return err
	}
	if err := s.labelProjectExists(label); err != nil {
		return err
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		t, err := s.GetTask(id)
		if err != nil {
			return err
		}
		for _, l := range t.Labels {
			if l == label {
				return nil
			}
		}
		t.Labels = append(t.Labels, label)
		sort.Strings(t.Labels)
		if _, err := s.appendLabelUpsertsLocked(code, []string{label}, actor, Now()); err != nil {
			return err
		}
		now := Now()
		t.UpdatedAt = now
		t.UpdatedBy = actor
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionTaskLabelAdded,
			Subject: Subject{Kind: "task", ID: id},
			Payload: mustMarshal(t),
		})
		if err != nil {
			return err
		}
		t.LogSeq = entry.Seq
		return cacheUpsertTask(db, t)
	})
}

func (s *Store) TaskLabelRemove(id, label, actor string) error {
	return s.mutateTask(id, actor, func(t *Task, now time.Time) {
		out := t.Labels[:0]
		for _, l := range t.Labels {
			if l != label {
				out = append(out, l)
			}
		}
		t.Labels = out
	}, ActionTaskLabelRemoved)
}

func (s *Store) RemoveTask(id, actor string) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		t, err := s.GetTask(id)
		if err != nil {
			return err
		}
		now := Now()
		t.UpdatedAt = now
		t.UpdatedBy = actor
		_, err = s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionTaskRemoved,
			Subject: Subject{Kind: "task", ID: id},
			Payload: mustMarshal(t),
		})
		if err != nil {
			return err
		}
		return cacheDeleteTask(db, id)
	})
}

// mutateTask is the log-first write-through helper for non-delete task mutations.
func (s *Store) mutateTask(id, actor string, fn func(t *Task, now time.Time), action string) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		t, err := s.GetTask(id)
		if err != nil {
			return err
		}
		now := Now()
		fn(t, now)
		t.UpdatedAt = now
		t.UpdatedBy = actor
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  action,
			Subject: Subject{Kind: "task", ID: id},
			Payload: mustMarshal(t),
		})
		if err != nil {
			return err
		}
		t.LogSeq = entry.Seq
		return cacheUpsertTask(db, t)
	})
}
```

- [ ] **Step 2: Update `task_test.go`'s four cache-file-touching tests**

Find the test containing `if _, err := os.Stat(s.taskPath(tk.ID)); !os.IsNotExist(err)` (around line 151, inside the `RemoveTask` tombstone test) and replace that one assertion block with:

```go
	db, _ := s.cacheDB()
	if _, ok, _ := cacheGetTask(db, tk.ID); ok {
		t.Fatal("cache row must be deleted")
	}
```

Replace `TestGetTaskLazyMissRebuildsFromLog` (lines 168-185) in full:

```go
func TestGetTaskLazyMissRebuildsFromLog(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	db, _ := s.cacheDB()
	// Hand-delete the cache row. Next read must rebuild from log.
	_, _ = db.Exec(`DELETE FROM tasks WHERE id = ?`, tk.ID)
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatalf("GetTask after cache delete: %v", err)
	}
	if got.ID != tk.ID || got.Title != tk.Title {
		t.Fatalf("rebuilt task = %+v want %+v", got, tk)
	}
	if _, ok, _ := cacheGetTask(db, tk.ID); !ok {
		t.Fatal("cache row was not rewritten after lazy miss")
	}
}
```

Replace `TestGetTaskStaleLogSeqTriggersRebuild` (lines 187-211) in full:

```go
func TestGetTaskStaleLogSeqTriggersRebuild(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	_ = s.SetTitle(tk.ID, "changed", "claude")
	// Stomp the cache back to an old LogSeq (simulate cache write failure after the log append).
	db, _ := s.cacheDB()
	_, _ = db.Exec(`UPDATE tasks SET log_seq = 1 WHERE id = ?`, tk.ID)
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatalf("GetTask with stale cache: %v", err)
	}
	if got.Title != "changed" {
		t.Fatalf("lazy miss did not rebuild: title = %q want %q", got.Title, "changed")
	}
	if got.LogSeq != 21 {
		t.Fatalf("rebuilt LogSeq = %d, want 21 (seq of title-changed entry)", got.LogSeq)
	}
}
```

Replace `TestGetTaskFutureLogSeqIntegrity` (lines 213-226) in full:

```go
func TestGetTaskFutureLogSeqIntegrity(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	db, _ := s.cacheDB()
	// Hand-write a cache row that claims a seq higher than the log's last.
	_, _ = db.Exec(`UPDATE tasks SET log_seq = 9999 WHERE id = ?`, tk.ID)
	_, err := s.GetTask(tk.ID)
	if !IsIntegrity(err) {
		t.Fatalf("expected ErrIntegrity, got %v", err)
	}
}
```

After these edits, `"encoding/json"` and `"os"` may no longer be used in `task_test.go` — run `go build ./...` and remove any import the compiler flags as unused.

- [ ] **Step 3: Build and test**

Run: `go vet ./... && go build ./... && go test ./internal/store/... -v -run TestCreateTask|TestGetTask|TestTaskLabel|TestRemoveTask`
Expected: all PASS, no unused-import errors.

Run: `go test ./...`
Expected: full suite green.

- [ ] **Step 4: Commit**

```bash
git add internal/store/task.go internal/store/task_test.go
git commit -m "$(cat <<'EOF'
Move task cache reads/writes onto cache.db

Also switches appendLabelUpsertsLocked's "already registered?" check
from a full-log Replay (labelsInLogLocked) to an indexed cache lookup
(cachePresentLabels) -- another perf win on every task/comment create
and label-add path.
EOF
)"
```

---

### Task 7: Rewire `label.go` onto `cache.db` (deletes `labels.json` machinery)

**Files:**
- Modify: `internal/store/label.go`
- Modify: `internal/store/label_test.go:165-182`

**Interfaces:**
- Consumes: `cacheUpsertLabel`, `cacheGetLabel`, `cacheDeleteLabel`, `cacheListLabels`, `cacheLabelUsage`, `cacheCountTasksWithLabelGlobally`, `cacheNamespaces` (Task 3).
- Removes: `labelsFile` type, `loadLabels`, `writeLabels`, `refreshDerivedLabelsLocked`, `labelsInLogLocked` (all dead once this task lands — `appendLabelUpsertsLocked` in `task.go`/`comment.go` no longer calls `refreshDerivedLabelsLocked`, see Task 6).

- [ ] **Step 1: Rewrite `internal/store/label.go`**

```go
package store

import (
	"fmt"
	"strings"

	"atm/internal/seed"
)

type LabelRemoveResult struct {
	RetainedUsage int `json:"retained_usage"`
}

// LabelAdd is the explicit "force upsert" path for a label: it always
// appends a label.upserted event to the project's log, then write-throughs
// the cache row. If `description` is empty, the existing description on the
// live row (if any) is preserved; a non-empty description overwrites it.
// Contrast with LabelSeed, which is a no-op when the label already exists.
func (s *Store) LabelAdd(name, description, actor string) error {
	if err := ValidateLabelName(name); err != nil {
		return err
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	if err := s.labelProjectExists(name); err != nil {
		return err
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	code := labelProject(name)
	return s.WithLock(code, func() error {
		l := Label{Name: name, Description: description}
		if description == "" {
			if existing, ok, err := cacheGetLabel(db, name); err != nil {
				return err
			} else if ok {
				l.Description = existing.Description
			}
		}
		now := Now()
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionLabelUpserted,
			Subject: Subject{Kind: "label", Name: name},
			Payload: mustMarshal(l),
		})
		if err != nil {
			return err
		}
		l.LogSeq = entry.Seq
		return cacheUpsertLabel(db, l)
	})
}

// LabelSeed upserts a label but only sets the description when the label is
// newly created. Existing labels keep their descriptions.
func (s *Store) LabelSeed(name, description, actor string) error {
	if err := ValidateLabelName(name); err != nil {
		return err
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	if err := s.labelProjectExists(name); err != nil {
		return err
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	code := labelProject(name)
	return s.WithLock(code, func() error {
		if _, ok, err := cacheGetLabel(db, name); err != nil {
			return err
		} else if ok {
			return nil
		}
		now := Now()
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionLabelUpserted,
			Subject: Subject{Kind: "label", Name: name},
			Payload: mustMarshal(Label{Name: name, Description: description}),
		})
		if err != nil {
			return err
		}
		return cacheUpsertLabel(db, Label{Name: name, Description: description, LogSeq: entry.Seq})
	})
}

// SeedLabels applies the default seed labels (internal/seed.Labels) to the
// project. Idempotent.
func (s *Store) SeedLabels(code, actor string) error {
	for _, l := range seed.Labels {
		full := code + ":" + l.Suffix
		if err := s.LabelSeed(full, l.Description, actor); err != nil {
			return err
		}
	}
	return nil
}

// seedLabelsLocked appends label.upserted events for each default label not
// already live, write-throughing each cache row. Caller MUST hold the
// project lock. Called by CreateProject from inside its own WithLock.
func (s *Store) seedLabelsLocked(code, actor string, at time.Time) error {
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	for _, l := range seed.Labels {
		full := code + ":" + l.Suffix
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      at,
			Actor:   actor,
			Action:  ActionLabelUpserted,
			Subject: Subject{Kind: "label", Name: full},
			Payload: mustMarshal(Label{Name: full, Description: l.Description}),
		})
		if err != nil {
			return err
		}
		if err := cacheUpsertLabel(db, Label{Name: full, Description: l.Description, LogSeq: entry.Seq}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LabelRemove(name, actor string) (*LabelRemoveResult, error) {
	if err := ValidateLabelName(name); err != nil {
		return nil, err
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var result *LabelRemoveResult
	code := labelProject(name)
	err = s.WithLock(code, func() error {
		l, ok, err := cacheGetLabel(db, name)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: label %q", ErrNotFound, name)
		}
		now := Now()
		_, err = s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionLabelRemoved,
			Subject: Subject{Kind: "label", Name: name},
			Payload: mustMarshal(l),
		})
		if err != nil {
			return err
		}
		if err := cacheDeleteLabel(db, name); err != nil {
			return err
		}
		count, err := cacheCountTasksWithLabelGlobally(db, name)
		if err != nil {
			return err
		}
		result = &LabelRemoveResult{RetainedUsage: count}
		return nil
	})
	return result, err
}

func (s *Store) LabelList(project, namespace string) []Label {
	db, err := s.cacheDB()
	if err != nil {
		return nil
	}
	out, err := cacheListLabels(db, project, namespace)
	if err != nil {
		return nil
	}
	return out
}

func (s *Store) LabelShow(name string) (Label, error) {
	db, err := s.cacheDB()
	if err != nil {
		return Label{}, err
	}
	l, ok, err := cacheGetLabel(db, name)
	if err != nil {
		return Label{}, err
	}
	if !ok {
		return Label{}, fmt.Errorf("%w: label %q", ErrNotFound, name)
	}
	return l, nil
}

func (s *Store) Namespaces(code string) []string {
	db, err := s.cacheDB()
	if err != nil {
		return nil
	}
	ns, err := cacheNamespaces(db, code)
	if err != nil {
		return nil
	}
	return ns
}

func (s *Store) labelProjectExists(name string) error {
	code := labelProject(name)
	if _, err := s.GetProject(code); err != nil {
		return fmt.Errorf("%w: project %q for label %q does not exist", ErrUsage, code, name)
	}
	return nil
}

func labelProject(name string) string {
	return strings.SplitN(name, ":", 2)[0]
}

// LabelUsage counts tasks in the given project carrying the label. Exported
// for the TUI's project-detail reconciliation surface (Screen 4: "(N tasks)"
// suffix per label). Backed by one indexed COUNT query — see
// docs/superpowers/specs/2026-07-06-atm-storage-sync-design.md and
// ATM-0027-c0003 (this replaces the old per-task GetTask scan).
func (s *Store) LabelUsage(projectCode, label string) (int, error) {
	db, err := s.cacheDB()
	if err != nil {
		return 0, err
	}
	return cacheLabelUsage(db, projectCode, label)
}
```

Add `"time"` to the import block (used by `seedLabelsLocked`'s `at time.Time` parameter).

- [ ] **Step 2: Replace `TestRebuildRegeneratesLabelsJSON` in `label_test.go`**

```go
func TestRebuildRegeneratesLabelCache(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_ = s.LabelAdd("ATM:type:bug", "d", "claude")
	db, _ := s.cacheDB()
	_, _ = db.Exec(`DELETE FROM labels WHERE name = ?`, "ATM:type:bug")
	if _, err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LabelShow("ATM:type:bug")
	if l.Description != "d" {
		t.Fatalf("rebuilt label desc = %q want %q", l.Description, "d")
	}
}
```

(`Rebuild()` is still on the old JSON-file implementation until Task 10 — this test will not pass until Task 10 lands. That is expected: mark it `t.Skip("pending Task 10: Rebuild() cache.db support")` for now and remove the skip in Task 10's step 2.)

- [ ] **Step 3: Add a direct `LabelUsage` regression test**

Append to `label_test.go`:

```go
func TestLabelUsageCountsOnlyProjectMatchingTasks(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk1, _ := s.CreateTask("ATM", "a", "", []string{"ATM:type:bug"}, "claude")
	_, _ = s.CreateTask("ATM", "b", "", nil, "claude")
	_ = tk1
	n, err := s.LabelUsage("ATM", "ATM:type:bug")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("LabelUsage = %d, want 1", n)
	}
}
```

- [ ] **Step 4: Build and test**

Run: `go vet ./... && go build ./...`

This will fail to build at first: `internal/store/comment.go`'s `CreateComment` and `CommentLabelAdd` each have an `if len(labelEntries) > 0 { s.refreshDerivedLabelsLocked(code) }` block after their `appendLabelUpsertsLocked` call. That function no longer exists after this task's `label.go` rewrite (Task 6 already made `appendLabelUpsertsLocked` write its cache rows through directly, so the refresh was already redundant). Fix: open `comment.go` and delete just those two `if len(labelEntries) > 0 { ... }` blocks (and the now-unused `labelEntries` variable at each call site) — do not otherwise touch `comment.go`; its full migration to `cache.db` is Task 8.

Confirm no other file references the deleted symbols: `grep -rn "refreshDerivedLabelsLocked\|labelsInLogLocked\|loadLabels\|writeLabels\|labelsFile" internal/store/*.go` should now return nothing (Task 6 already removed `task.go`'s `labelsInLogLocked`; `rebuild.go`/`verify.go` never called any of these — `rebuild.go` still writes `labels.json` directly via its own untouched `WriteJSON(s.labelsPath(), ...)` line until Task 10).

Run: `go test ./...`
Expected: full suite green except the one `t.Skip`ped test from Step 2.

- [ ] **Step 5: Commit**

```bash
git add internal/store/label.go internal/store/label_test.go internal/store/comment.go
git commit -m "$(cat <<'EOF'
Move label cache reads/writes onto cache.db; delete labels.json machinery

Deletes refreshDerivedLabelsLocked/labelsInLogLocked/loadLabels/
writeLabels/labelsFile -- every label mutation now write-throughs its
own cache.db row directly instead of re-deriving the whole global
registry file. LabelUsage is now one indexed query
(cacheLabelUsage) -- the direct fix for ATM-0027-c0003.
EOF
)"
```

---

### Task 8: Rewire `comment.go` onto `cache.db`

**Files:**
- Modify: `internal/store/comment.go`
- Modify: `internal/store/comment_test.go` (3 spots)

**Interfaces:**
- Consumes: `cacheUpsertComment`, `cacheGetComment`, `cacheDeleteComment`, `cacheListComments` (Task 4); `cacheUpsertTask`/`cacheGetTask` (Task 2, since `CreateComment`/`RemoveComment` also mutate the parent task's `NextCommentN`).

- [ ] **Step 1: Rewrite `internal/store/comment.go`**

```go
package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

func (s *Store) CreateComment(taskID, body string, labels []string, replyTo, actor string) (*Comment, error) {
	if body == "" {
		return nil, fmt.Errorf("%w: body is required", ErrUsage)
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, ok := ParseTaskID(taskID)
	if !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, taskID)
	}
	if replyTo != "" {
		rcode, _, _, ok := ParseCommentID(replyTo)
		if !ok {
			return nil, fmt.Errorf("%w: invalid reply-to %q", ErrUsage, replyTo)
		}
		if rcode != code {
			return nil, fmt.Errorf("%w: reply-to %q must belong to the same project as task %q", ErrUsage, replyTo, taskID)
		}
		_, rtaskN, _, _ := ParseCommentID(replyTo)
		_, ttaskN, _ := ParseTaskID(taskID)
		if rtaskN != ttaskN {
			return nil, fmt.Errorf("%w: reply-to %q must belong to task %q", ErrUsage, replyTo, taskID)
		}
	}
	for _, l := range labels {
		if err := ValidateLabelName(l); err != nil {
			return nil, err
		}
		if err := s.labelProjectExists(l); err != nil {
			return nil, err
		}
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var created *Comment
	err = s.WithLock(code, func() error {
		t, err := s.GetTask(taskID)
		if err != nil {
			return err
		}
		n := t.NextCommentN
		idN := n + 1
		id := RenderCommentID(taskID, idN)
		ts := Now()
		labelsSorted := append([]string(nil), labels...)
		sort.Strings(labelsSorted)
		c := &Comment{
			ID:        id,
			TaskID:    taskID,
			ReplyTo:   replyTo,
			Body:      body,
			Labels:    labelsSorted,
			CreatedAt: ts,
			CreatedBy: actor,
			UpdatedAt: ts,
			UpdatedBy: actor,
		}
		if _, err := s.appendLabelUpsertsLocked(code, labels, actor, ts); err != nil {
			return err
		}
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      ts,
			Actor:   actor,
			Action:  ActionCommentCreated,
			Subject: Subject{Kind: "comment", ID: id},
			Payload: mustMarshal(c),
		})
		if err != nil {
			return err
		}
		c.LogSeq = entry.Seq
		t.NextCommentN = idN
		t.UpdatedAt = ts
		t.UpdatedBy = actor
		metaEntry, err := s.appendLogLocked(code, LogEntry{
			At:      ts,
			Actor:   actor,
			Action:  ActionTaskMetaChanged,
			Subject: Subject{Kind: "task", ID: taskID},
			Payload: mustMarshal(t),
		})
		if err != nil {
			return err
		}
		t.LogSeq = metaEntry.Seq
		if err := cacheUpsertComment(db, c); err != nil {
			return err
		}
		if err := cacheUpsertTask(db, t); err != nil {
			return err
		}
		created = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Store) GetComment(id string) (*Comment, error) {
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return nil, fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	c, found, err := cacheGetComment(db, id)
	if err != nil {
		return nil, err
	}
	if !found {
		if err := s.WithLock(code, func() error { return s.rebuildCommentFromLog(id, code) }); err != nil {
			return nil, err
		}
		c, found, err = cacheGetComment(db, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: comment %q", ErrNotFound, id)
		}
		return c, nil
	}
	last, lastErr := s.LastLogSeq(code)
	if lastErr != nil {
		return nil, lastErr
	}
	if c.LogSeq > last {
		return nil, fmt.Errorf("%w: comment %q cache LogSeq=%d > log LastSeq=%d", ErrIntegrity, id, c.LogSeq, last)
	}
	commentLast, err := s.lastCommentEventSeq(code, id)
	if err != nil {
		return nil, err
	}
	if c.LogSeq < commentLast {
		if err := s.WithLock(code, func() error { return s.rebuildCommentFromLog(id, code) }); err != nil {
			return nil, err
		}
		c, found, err = cacheGetComment(db, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: comment %q", ErrNotFound, id)
		}
	}
	return c, nil
}

func (s *Store) lastCommentEventSeq(code, id string) (int, error) {
	entries, err := s.ReadLog(code)
	if err != nil {
		return 0, err
	}
	last := 0
	for _, e := range entries {
		if e.Subject.Kind == "comment" && e.Subject.ID == id {
			last = e.Seq
		}
	}
	return last, nil
}

func (s *Store) rebuildCommentFromLog(id, code string) error {
	entries, err := s.ReadLog(code)
	if err != nil && !IsIntegrity(err) {
		return err
	}
	var c *Comment
	lastSeq := 0
	for _, e := range entries {
		if e.Subject.Kind != "comment" || e.Subject.ID != id {
			continue
		}
		lastSeq = e.Seq
		if e.Action == ActionCommentRemoved {
			c = nil
			continue
		}
		var cc Comment
		if err := json.Unmarshal(e.Payload, &cc); err == nil {
			c = &cc
		}
	}
	if c == nil {
		return fmt.Errorf("%w: comment %q", ErrNotFound, id)
	}
	c.LogSeq = lastSeq
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return cacheUpsertComment(db, c)
}

func (s *Store) ListComments(taskID string) ([]*Comment, error) {
	if _, _, ok := ParseTaskID(taskID); !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, taskID)
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	out, err := cacheListComments(db, taskID)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []*Comment{}
	}
	return out, nil
}

func (s *Store) SetCommentBody(id, body, actor string) error {
	if body == "" {
		return fmt.Errorf("%w: body is required", ErrUsage)
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	return s.mutateComment(id, actor, func(c *Comment, now time.Time) {
		c.Body = body
	}, ActionCommentBodyChanged)
}

func (s *Store) CommentLabelRemove(id, label, actor string) error {
	return s.mutateComment(id, actor, func(c *Comment, now time.Time) {
		out := c.Labels[:0]
		for _, l := range c.Labels {
			if l != label {
				out = append(out, l)
			}
		}
		c.Labels = out
	}, ActionCommentLabelRemoved)
}

func (s *Store) RemoveComment(id, actor string) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		c, err := s.GetComment(id)
		if err != nil {
			return err
		}
		now := Now()
		c.UpdatedAt = now
		c.UpdatedBy = actor
		if _, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionCommentRemoved,
			Subject: Subject{Kind: "comment", ID: id},
			Payload: mustMarshal(c),
		}); err != nil {
			return err
		}
		return cacheDeleteComment(db, id)
	})
}

func (s *Store) CommentLabelAdd(id, label, actor string) error {
	if err := ValidateLabelName(label); err != nil {
		return err
	}
	if err := s.labelProjectExists(label); err != nil {
		return err
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		c, err := s.GetComment(id)
		if err != nil {
			return err
		}
		for _, l := range c.Labels {
			if l == label {
				return nil
			}
		}
		c.Labels = append(c.Labels, label)
		sort.Strings(c.Labels)
		if _, err := s.appendLabelUpsertsLocked(code, []string{label}, actor, Now()); err != nil {
			return err
		}
		now := Now()
		c.UpdatedAt = now
		c.UpdatedBy = actor
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionCommentLabelAdded,
			Subject: Subject{Kind: "comment", ID: id},
			Payload: mustMarshal(c),
		})
		if err != nil {
			return err
		}
		c.LogSeq = entry.Seq
		return cacheUpsertComment(db, c)
	})
}

func (s *Store) mutateComment(id, actor string, fn func(c *Comment, now time.Time), action string) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		c, err := s.GetComment(id)
		if err != nil {
			return err
		}
		now := Now()
		fn(c, now)
		c.UpdatedAt = now
		c.UpdatedBy = actor
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  action,
			Subject: Subject{Kind: "comment", ID: id},
			Payload: mustMarshal(c),
		})
		if err != nil {
			return err
		}
		c.LogSeq = entry.Seq
		return cacheUpsertComment(db, c)
	})
}
```

- [ ] **Step 2: Update `comment_test.go`'s three cache-file-touching tests**

Replace `TestGetCommentLazyMissRebuildsFromLog` (lines 117-134):

```go
func TestGetCommentLazyMissRebuildsFromLog(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "persist", nil, "", "claude")
	db, _ := s.cacheDB()
	_, _ = db.Exec(`DELETE FROM comments WHERE id = ?`, c.ID)
	got, err := s.GetComment(c.ID)
	if err != nil {
		t.Fatalf("GetComment after cache delete: %v", err)
	}
	if got.Body != "persist" {
		t.Fatalf("rebuilt comment body = %q want %q", got.Body, "persist")
	}
	if _, ok, _ := cacheGetComment(db, c.ID); !ok {
		t.Fatal("cache row was not rewritten after lazy miss")
	}
}
```

Replace `TestGetCommentFutureLogSeqIntegrity` (lines 136-148):

```go
func TestGetCommentFutureLogSeqIntegrity(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "x", nil, "", "claude")
	db, _ := s.cacheDB()
	_, _ = db.Exec(`UPDATE comments SET log_seq = 9999 WHERE id = ?`, c.ID)
	_, err := s.GetComment(c.ID)
	if !IsIntegrity(err) {
		t.Fatalf("expected ErrIntegrity, got %v", err)
	}
}
```

Replace `TestParseReplayNextCommentNFromMetaChanged` (lines 188-202):

```go
func TestParseReplayNextCommentNFromMetaChanged(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	_, _ = s.CreateComment(tk.ID, "first", nil, "", "claude")
	db, _ := s.cacheDB()
	_, _ = db.Exec(`DELETE FROM tasks WHERE id = ?`, tk.ID)
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.NextCommentN != 1 {
		t.Fatalf("replay-derived NextCommentN = %d want 1", got.NextCommentN)
	}
}
```

After these edits, check whether `"encoding/json"` and `"os"` are still used elsewhere in `comment_test.go` (several other tests in this file still hand-write JSON — check with `grep -n "json\.\|os\." internal/store/comment_test.go` before removing either import).

- [ ] **Step 3: Un-skip the Task 7 test**

`Rebuild()` is not yet migrated (Task 10), so `TestRebuildRegeneratesLabelCache` from Task 7 stays skipped for now — do not un-skip it in this task.

- [ ] **Step 4: Build and test**

Run: `go vet ./... && go build ./... && go test ./internal/store/... -v -run TestCreateComment|TestGetComment|TestListComments|TestSetCommentBody|TestCommentLabel|TestRemoveComment|TestParseReplayNextCommentN`
Expected: all PASS.

Run: `go test ./...`
Expected: full suite green except the one skipped test.

- [ ] **Step 5: Commit**

```bash
git add internal/store/comment.go internal/store/comment_test.go
git commit -m "$(cat <<'EOF'
Move comment cache reads/writes onto cache.db
EOF
)"
```

---

### Task 9: Rewire `query.go` onto `cache.db`

**Files:**
- Modify: `internal/store/query.go`

**Interfaces:**
- Consumes: `cacheListTasksForProject`, `cacheListTaskIDs` (Task 2).

**Note:** `ListTasks`/`GroupTasks` now read task rows straight from `cache.db` (via `cacheListTasksForProject`) instead of calling `s.GetTask` per ID. This intentionally **skips the per-task lazy-miss staleness check** for bulk listing (it still applies to point lookups: `GetTask`, and to `atm store verify`/`rebuild`) — a deliberate tradeoff since write-through keeps `cache.db` fresh under normal operation, and bulk list views (the TUI's most frequent operation) shouldn't pay a per-row log-parse cost. This is the second half of the ATM-0027-c0003 fix.

- [ ] **Step 1: Rewrite `internal/store/query.go`**

```go
package store

import (
	"sort"
	"strings"
)

type QueryFilters struct {
	Project string
	Labels  []string
}

type LabelGroup struct {
	Label string
	Tasks []*Task
}

func (s *Store) ListTasks(filters QueryFilters) []*Task {
	var codes []string
	if filters.Project != "" {
		codes = []string{filters.Project}
	} else {
		for _, p := range s.ListProjects() {
			codes = append(codes, p.Code)
		}
	}
	restricting := restrictingTokens(filters.Labels)
	db, err := s.cacheDB()
	if err != nil {
		return nil
	}
	var out []*Task
	for _, code := range codes {
		tasks, err := cacheListTasksForProject(db, code)
		if err != nil {
			continue
		}
		for _, t := range tasks {
			if !taskMatchesLabels(t, restricting) {
				continue
			}
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci, ni, _ := ParseTaskID(out[i].ID)
		cj, nj, _ := ParseTaskID(out[j].ID)
		if ci != cj {
			return ci < cj
		}
		return ni < nj
	})
	return out
}

func (s *Store) GroupTasks(filters QueryFilters) ([]LabelGroup, []*Task) {
	inScope := s.ListTasks(filters)
	wildcards := wildcardTokens(filters.Labels)
	if len(wildcards) == 0 {
		return nil, inScope
	}
	buckets := map[string][]*Task{}
	order := []string{}
	for _, t := range inScope {
		for _, w := range wildcards {
			for _, l := range t.Labels {
				if labelMatchesWildcard(l, w) {
					if _, exists := buckets[l]; !exists {
						order = append(order, l)
					}
					buckets[l] = append(buckets[l], t)
				}
			}
		}
	}
	sort.Strings(order)
	var groups []LabelGroup
	for _, l := range order {
		groups = append(groups, LabelGroup{Label: l, Tasks: buckets[l]})
	}
	var others []*Task
	for _, t := range inScope {
		matched := false
		for _, w := range wildcards {
			for _, l := range t.Labels {
				if labelMatchesWildcard(l, w) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			others = append(others, t)
		}
	}
	return groups, others
}

func restrictingTokens(labels []string) []string {
	var out []string
	for _, l := range labels {
		if !isWildcard(l) {
			out = append(out, l)
		}
	}
	return out
}

func wildcardTokens(labels []string) []string {
	var out []string
	for _, l := range labels {
		if isWildcard(l) {
			out = append(out, l)
		}
	}
	return out
}

func isWildcard(l string) bool { return strings.HasSuffix(l, ":*") }

func labelMatchesWildcard(label, wildcard string) bool {
	prefix := strings.TrimSuffix(wildcard, "*")
	return strings.HasPrefix(label, prefix)
}

func taskMatchesLabels(t *Task, labels []string) bool {
	for _, want := range labels {
		found := false
		for _, l := range t.Labels {
			if l == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (s *Store) listTaskIDs(code string) []string {
	db, err := s.cacheDB()
	if err != nil {
		return nil
	}
	ids, err := cacheListTaskIDs(db, code)
	if err != nil {
		return nil
	}
	return ids
}
```

(`GroupTasks`'s inner loop drops the earlier dead `_ = matched` line — that was a no-op in the original file and is safe to remove.)

- [ ] **Step 2: Build and test**

Run: `go vet ./... && go build ./... && go test ./internal/store/... -run TestListTasks|TestGroupTasks -v`
Expected: PASS unchanged (query_test.go doesn't reference file paths — confirm with `grep -n "taskPath\|Dir(" internal/store/query_test.go`, expect no matches).

Run: `go test ./...`
Expected: full suite green except the one skipped test from Task 7.

- [ ] **Step 3: Commit**

```bash
git add internal/store/query.go
git commit -m "$(cat <<'EOF'
Move ListTasks/GroupTasks onto cache.db

Bulk listing now reads task rows directly from cache.db instead of
GetTask-per-ID (which previously re-parsed the project log twice per
task). Point lookups (GetTask) and atm store verify/rebuild keep the
full lazy-miss staleness check; bulk views trust the write-through
cache, consistent with docs/superpowers/specs/2026-07-06-atm-storage-sync-design.md.
EOF
)"
```

---

### Task 10: Rewire `rebuild.go` onto `cache.db`

**Files:**
- Modify: `internal/store/rebuild.go`
- Modify: `internal/store/rebuild_test.go`
- Modify: `internal/store/label_test.go` (remove the `t.Skip` from Task 7)

**Interfaces:**
- Consumes: `s.projectCodesOnDisk()` (Task 5), all `cacheUpsert*` helpers (Tasks 1-4).

- [ ] **Step 1: Rewrite `internal/store/rebuild.go`**

```go
package store

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
	}
	for _, l := range mergedLabels {
		if err := cacheUpsertLabel(db, l); err != nil {
			return rep, err
		}
	}
	rep.Labels = len(mergedLabels)
	return rep, nil
}
```

- [ ] **Step 2: Un-skip and confirm the Task 7 test**

In `internal/store/label_test.go`, remove the `t.Skip(...)` line from `TestRebuildRegeneratesLabelCache` (added in Task 7) — it should now pass unmodified.

- [ ] **Step 3: Rewrite `TestRebuildWritesCommentCachesAndSweepsOrphans` in `rebuild_test.go`**

```go
package store

import "testing"

func TestRebuildWritesCommentCachesAndSweepsOrphans(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "hello", nil, "", "claude")
	db, _ := s.cacheDB()
	// Hand-insert an orphan comment row (no log entry for it).
	_, _ = db.Exec(`INSERT INTO comments (id, task_id, reply_to, body, log_seq, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, '', 'orphan', 0, '2026-01-01T00:00:00Z', 'x', '2026-01-01T00:00:00Z', 'x')`,
		"ATM-0001-c0099", tk.ID)
	// Hand-delete the live comment row.
	_, _ = db.Exec(`DELETE FROM comments WHERE id = ?`, c.ID)
	if _, err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cacheGetComment(db, c.ID); !ok {
		t.Fatal("live comment cache not rebuilt")
	}
	if _, ok, _ := cacheGetComment(db, "ATM-0001-c0099"); ok {
		t.Fatal("orphan comment cache not swept")
	}
}
```

- [ ] **Step 4: Build and test**

Run: `go vet ./... && go build ./... && go test ./internal/store/... -run TestRebuild -v`
Expected: all PASS, including the previously-skipped `TestRebuildRegeneratesLabelCache`.

Run: `go test ./...`
Expected: full suite green — no more skipped tests in `internal/store`.

- [ ] **Step 5: Commit**

```bash
git add internal/store/rebuild.go internal/store/rebuild_test.go internal/store/label_test.go
git commit -m "$(cat <<'EOF'
Move Rebuild() onto cache.db

Rebuild now enumerates project codes from disk (projectCodesOnDisk),
wipes every cache.db table in one transaction, then replays each
project's log to repopulate it -- no more per-file orphan sweeping,
the DELETE-then-repopulate transaction makes that automatic.
EOF
)"
```

---

### Task 11: Rewire `verify.go` onto `cache.db`; update CLI rendering

**Files:**
- Modify: `internal/store/verify.go`
- Modify: `internal/store/verify_test.go`
- Modify: `internal/cli/store.go` (`emitVerify`)
- Modify: `internal/cli/store_test.go` (`taskCachePath` helper + `TestStoreVerifyExitsNonzeroOnDivergence`)

**Interfaces:**
- Produces: `CacheCheck` gains `Kind`/`ID` fields, replacing `Path` (breaking change to the JSON shape of `atm store verify --output json`, intentional and documented in the design spec's rollout).

- [ ] **Step 1: Rewrite `internal/store/verify.go`**

```go
package store

import (
	"database/sql"
	"fmt"
	"sort"
)

type VerifyReport struct {
	Project    string
	LogEntries int
	LogOK      bool
	Truncated  int
	SeqGaps    []int
	Caches     []CacheCheck
	Diverged   bool
}

type CacheCheck struct {
	Kind         string // "project" | "task" | "comment"
	ID           string // project code | task id | comment id
	Status       string // "ok" | "stale" | "missing" | "corrupt"
	CacheLogSeq  int
	LastEventSeq int
}

func (s *Store) Verify() ([]VerifyReport, error) {
	codes, err := s.projectCodesOnDisk()
	if err != nil {
		return nil, err
	}
	var out []VerifyReport
	for _, code := range codes {
		r, err := s.VerifyProject(code)
		if err != nil {
			return out, err
		}
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Project < out[j].Project })
	return out, nil
}

func (s *Store) VerifyProject(code string) (*VerifyReport, error) {
	report := &VerifyReport{Project: code, LogOK: true}
	entries, err := s.ReadLog(code)
	if err != nil {
		if IsIntegrity(err) {
			report.LogOK = false
			report.Truncated = extractTruncatedBytes(err)
		} else {
			return nil, err
		}
	}
	report.LogEntries = len(entries)
	last := 0
	for _, e := range entries {
		if e.Seq != last+1 {
			report.SeqGaps = append(report.SeqGaps, last+1)
			report.LogOK = false
		}
		last = e.Seq
	}
	st, _ := s.Replay(code)
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	report.Caches = append(report.Caches, s.checkProjectCache(db, code, st))
	for _, t := range st.Tasks {
		report.Caches = append(report.Caches, s.checkTaskCache(db, code, t.ID))
	}
	for _, c := range st.Comments {
		report.Caches = append(report.Caches, s.checkCommentCache(db, code, c.ID))
	}
	cachedIDs, err := cacheListCommentIDsForProject(db, code)
	if err != nil {
		return nil, err
	}
	liveComments := map[string]bool{}
	for _, c := range st.Comments {
		liveComments[c.ID] = true
	}
	for _, id := range cachedIDs {
		if !liveComments[id] {
			report.Caches = append(report.Caches, CacheCheck{Kind: "comment", ID: id, Status: "corrupt"})
			report.Diverged = true
		}
	}
	for _, c := range report.Caches {
		if c.Status != "ok" {
			report.Diverged = true
		}
	}
	return report, nil
}

func (s *Store) checkProjectCache(db *sql.DB, code string, st *ReplayState) CacheCheck {
	p, ok, err := cacheGetProject(db, code)
	if err != nil {
		return CacheCheck{Kind: "project", ID: code, Status: "corrupt"}
	}
	if !ok {
		return CacheCheck{Kind: "project", ID: code, Status: "missing"}
	}
	last, _ := s.lastProjectEventSeq(code)
	if p.LogSeq > last {
		return CacheCheck{Kind: "project", ID: code, Status: "corrupt", CacheLogSeq: p.LogSeq, LastEventSeq: last}
	}
	if p.LogSeq < last {
		return CacheCheck{Kind: "project", ID: code, Status: "stale", CacheLogSeq: p.LogSeq, LastEventSeq: last}
	}
	return CacheCheck{Kind: "project", ID: code, Status: "ok", CacheLogSeq: p.LogSeq, LastEventSeq: last}
}

func (s *Store) checkTaskCache(db *sql.DB, code, id string) CacheCheck {
	t, ok, err := cacheGetTask(db, id)
	if err != nil {
		return CacheCheck{Kind: "task", ID: id, Status: "corrupt"}
	}
	if !ok {
		return CacheCheck{Kind: "task", ID: id, Status: "missing"}
	}
	last, _ := s.lastTaskEventSeq(code, id)
	if t.LogSeq > last {
		return CacheCheck{Kind: "task", ID: id, Status: "corrupt", CacheLogSeq: t.LogSeq, LastEventSeq: last}
	}
	if t.LogSeq < last {
		return CacheCheck{Kind: "task", ID: id, Status: "stale", CacheLogSeq: t.LogSeq, LastEventSeq: last}
	}
	return CacheCheck{Kind: "task", ID: id, Status: "ok", CacheLogSeq: t.LogSeq, LastEventSeq: last}
}

func (s *Store) checkCommentCache(db *sql.DB, code, id string) CacheCheck {
	c, ok, err := cacheGetComment(db, id)
	if err != nil {
		return CacheCheck{Kind: "comment", ID: id, Status: "corrupt"}
	}
	if !ok {
		return CacheCheck{Kind: "comment", ID: id, Status: "missing"}
	}
	last, _ := s.lastCommentEventSeq(code, id)
	if c.LogSeq > last {
		return CacheCheck{Kind: "comment", ID: id, Status: "corrupt", CacheLogSeq: c.LogSeq, LastEventSeq: last}
	}
	if c.LogSeq < last {
		return CacheCheck{Kind: "comment", ID: id, Status: "stale", CacheLogSeq: c.LogSeq, LastEventSeq: last}
	}
	return CacheCheck{Kind: "comment", ID: id, Status: "ok", CacheLogSeq: c.LogSeq, LastEventSeq: last}
}

func extractTruncatedBytes(err error) int {
	var n int
	var prefix string
	_, _ = fmt.Sscanf(err.Error(), "%s %d bytes", &prefix, &n)
	return n
}
```

- [ ] **Step 2: Update `internal/cli/store.go`'s `emitVerify`**

```go
func (st *cliState) emitVerify(r *store.VerifyReport) error {
	if st.isJSON() {
		return writeJSON(st.stdout(), r)
	}
	fmt.Fprintf(st.stdout(), "project: %s\nlog_entries: %d\nlog_ok: %t\ntruncated: %d\ndiverged: %t\n", r.Project, r.LogEntries, r.LogOK, r.Truncated, r.Diverged)
	for _, c := range r.Caches {
		fmt.Fprintf(st.stdout(), "  %s\t%s:%s\tcache=%d last=%d\n", c.Status, c.Kind, c.ID, c.CacheLogSeq, c.LastEventSeq)
	}
	return nil
}
```

- [ ] **Step 3: Rewrite the four `Path`-touching tests in `internal/store/verify_test.go`**

Replace `TestVerifyCleanStore`'s error line (26): `t.Errorf("cache %s status = %q want ok", c.Path, c.Status)` → `t.Errorf("cache %s:%s status = %q want ok", c.Kind, c.ID, c.Status)`.

Replace `TestVerifyDetectsStaleTaskCache` (31-59):

```go
func TestVerifyDetectsStaleTaskCache(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	_ = s.SetTitle(tk.ID, "changed", "claude")
	db, _ := s.cacheDB()
	_, _ = db.Exec(`UPDATE tasks SET log_seq = 1 WHERE id = ?`, tk.ID)
	report, err := s.VerifyProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Diverged {
		t.Fatal("Diverged=false with stale cache")
	}
	found := false
	for _, c := range report.Caches {
		if c.Status == "stale" {
			found = true
		}
	}
	if !found {
		t.Errorf("no stale cache reported: %+v", report.Caches)
	}
}
```

Replace `TestVerifyDetectsMissingCache` (61-70):

```go
func TestVerifyDetectsMissingCache(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	db, _ := s.cacheDB()
	_, _ = db.Exec(`DELETE FROM tasks WHERE id = ?`, tk.ID)
	report, _ := s.VerifyProject("ATM")
	if !report.Diverged {
		t.Fatal("Diverged=false with missing cache")
	}
}
```

Replace `TestVerifyReportsCommentCacheStale` (117-144):

```go
func TestVerifyReportsCommentCacheStale(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "x", nil, "", "claude")
	db, _ := s.cacheDB()
	_, _ = db.Exec(`UPDATE comments SET log_seq = 0 WHERE id = ?`, c.ID)
	rep, err := s.VerifyProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ck := range rep.Caches {
		if ck.Kind == "comment" && ck.ID == c.ID {
			if ck.Status == "stale" || ck.Status == "corrupt" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("comment cache stale not reported: %+v", rep.Caches)
	}
}
```

`TestVerifyDetectsMalformedLogTail` and `TestVerifyDetectsSeqGap` are unchanged (they only touch `log.jsonl`, not caches). After these edits, check whether `"encoding/json"` is still used in `verify_test.go` (likely not — remove the import if the compiler flags it).

- [ ] **Step 4: Update `internal/cli/store_test.go`**

Delete the `taskCachePath` helper (lines 14-16) and its `path/filepath` import if nothing else in the file needs `filepath` (check with `grep -n "filepath\." internal/cli/store_test.go`). Add `database/sql` and the sqlite driver import.

Replace `TestStoreVerifyExitsNonzeroOnDivergence` (134-161):

```go
func TestStoreVerifyExitsNonzeroOnDivergence(t *testing.T) {
	st := newTestCLI(t)
	_, _ = st.store.CreateProject("ATM", "x", "claude")
	tk, _ := st.store.CreateTask("ATM", "t", "", nil, "claude")
	_ = st.store.SetTitle(tk.ID, "changed", "claude")
	// Stomp the task cache row back to seq 1 (stale) so verify detects divergence.
	db, err := sql.Open("sqlite", filepath.Join(st.store.StorePath(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET log_seq = 1 WHERE id = ?`, tk.ID); err != nil {
		t.Fatal(err)
	}
	_, _, code := runArgs(st, "store", "verify")
	if code != 5 {
		t.Fatalf("store verify exit code = %d, want 5 (integrity divergence)", code)
	}
}
```

Update the import block at the top of `internal/cli/store_test.go` to:

```go
import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/store"

	_ "modernc.org/sqlite"
)
```

(drop `"encoding/json"` and `"os"` if `grep -n "json\.\|os\." internal/cli/store_test.go` shows no other remaining uses after this edit).

- [ ] **Step 5: Build and test**

Run: `go vet ./... && go build ./...`
Expected: clean.

Run: `go test ./internal/store/... -run TestVerify -v && go test ./internal/cli/... -run TestStoreVerify -v`
Expected: all PASS.

Run: `go test ./...`
Expected: full suite green, zero skipped tests.

- [ ] **Step 6: Commit**

```bash
git add internal/store/verify.go internal/store/verify_test.go internal/cli/store.go internal/cli/store_test.go
git commit -m "$(cat <<'EOF'
Move Verify()/VerifyProject onto cache.db

CacheCheck.Path (a filesystem path) is replaced by Kind+ID (entity
kind and identifier) since entities no longer have per-file paths.
This changes atm store verify --output json's cache-check shape --
documented, intentional break per the design spec.
EOF
)"
```

---

### Task 12: Remove obsolete JSON-cache machinery; finalize `Init()`

**Files:**
- Modify: `internal/store/store.go`

**Interfaces:** none new — this is a dead-code removal pass.

- [ ] **Step 1: Confirm nothing still references the obsolete helpers**

Run: `grep -rn "taskPath\|commentPath\b\|projectPath\|labelsPath\|tasksDir\|commentsDir\|touchLabels\|labelsFile" internal/store/*.go internal/cli/*.go internal/tui/*.go`

Expected: matches only inside `internal/store/store.go` itself (the helper definitions) — every call site was already migrated in Tasks 5-11. If any other match appears, stop and port that call site to the equivalent `cache*` helper before continuing (it means an earlier task's migration missed a caller).

- [ ] **Step 2: Remove the obsolete helpers and update `Init()` in `store.go`**

Delete from `internal/store/store.go`: `func (s *Store) tasksDir`, `func (s *Store) taskPath`, `func (s *Store) commentsDir`, `func (s *Store) commentPath`, `func (s *Store) projectPath`, `func (s *Store) labelsPath`, `func (s *Store) touchLabels`, and the `labelsFile` type (if it still lives in `store.go` rather than the already-deleted `label.go` code — confirm which file it's in with `grep -rn "type labelsFile" internal/store/`; it was defined in the old `label.go` and should already be gone after Task 7's rewrite — if `grep` finds nothing, skip this part).

Replace `Init`:

```go
func (s *Store) Init(storePath string) error {
	if storePath != "" {
		abs, err := filepath.Abs(storePath)
		if err != nil {
			return err
		}
		s.Root = abs
	}
	if s.Root == "" {
		root, err := Open("")
		if err != nil {
			return err
		}
		s.Root = root.Root
	}
	if err := os.MkdirAll(s.projectsDir(), 0o755); err != nil {
		return err
	}
	_, err := s.cacheDB()
	return err
}
```

Keep `projectsDir`, `projectDir`, `lockPath`, `cachePath` — all still used.

- [ ] **Step 3: Build and full test suite**

Run: `go vet ./... && go build ./... && go test ./...`
Expected: clean build, zero unused symbols, full suite green.

- [ ] **Step 4: Manual smoke test**

Run:
```bash
rm -rf /tmp/atm-smoke && mkdir /tmp/atm-smoke
go run ./cmd/atm --store /tmp/atm-smoke project create --code ATM --name "Smoke Test" --actor smoketest
go run ./cmd/atm --store /tmp/atm-smoke task create --project ATM --title "hello" --actor smoketest
go run ./cmd/atm --store /tmp/atm-smoke task list --project ATM
go run ./cmd/atm --store /tmp/atm-smoke store verify
ls /tmp/atm-smoke
```
Expected: task create/list work as before; `store verify` reports `diverged: false`; `ls` shows `cache.db` and `projects/` (no more `labels.json`, no `<CODE>.json`, no `tasks/*.json` files) — confirming the on-disk shape actually changed.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go
git commit -m "$(cat <<'EOF'
Remove obsolete JSON-cache file helpers; atm init creates cache.db

Final cleanup of the cache.db consolidation
(docs/superpowers/specs/2026-07-06-atm-storage-sync-design.md). All
entity caches now live in $ATM_HOME/cache.db; log.jsonl per project
remains the untouched source of truth.
EOF
)"
```

---

## Follow-up (not in this plan)

Cross-machine sync (`atm store push`/`pull`, the `SyncTarget` interface, filesystem transport, full-log merge, task-ID collision detection) is deliberately **not** in this plan. It depends on this plan's `Rebuild()`-equivalent (a merge writes a project's merged log, then needs "regenerate this project's cache" — which by the time sync is built will mean cache.db, not JSON files). Writing that plan now would risk stale signatures before this one lands. Once this plan is fully merged and `make verify` is green on `main`, request a second plan for the sync feature referencing the final `cache.go`/`rebuild.go` shapes.
