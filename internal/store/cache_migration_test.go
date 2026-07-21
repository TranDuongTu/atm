package store

import (
	"database/sql"
	"os"
	"testing"
)

// legacyCacheSchema is the pre-ATM-0127 cache.db shape: projects.next_task_n
// and the log_seq columns exist, and there is no PRAGMA user_version marker
// (fresh DBs report 0). Hand-creating it lets the migration test prove that
// cacheDB() transparently rebuilds an old cache.db at the current shape.
const legacyCacheSchema = `
CREATE TABLE projects (
	code TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	next_task_n INTEGER NOT NULL,
	log_seq INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	created_by TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	updated_by TEXT NOT NULL
);
CREATE TABLE tasks (
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
CREATE TABLE meta (
	key TEXT PRIMARY KEY,
	value INTEGER NOT NULL
);
`

// seedLegacyCacheDB writes a cache.db at the OLD schema (user_version 0) with a
// stale project row and a last_log_seq meta row for code, so the migration test
// can prove both that the schema is rebuilt and that the orphaned last_log_seq
// meta row does not survive.
func seedLegacyCacheDB(t *testing.T, path, code string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy cache.db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(legacyCacheSchema); err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO projects (code, name, next_task_n, log_seq, created_at, created_by, updated_at, updated_by)
		VALUES (?, 'stale', 7, 3, '2020-01-01T00:00:00Z', 'x', '2020-01-01T00:00:00Z', 'x')`, code); err != nil {
		t.Fatalf("seed stale project: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES (?, 3)`, "last_log_seq:"+code); err != nil {
		t.Fatalf("seed last_log_seq meta: %v", err)
	}
	// user_version defaults to 0, matching a pre-ATM-0127 cache.db.
}

// TestCacheMigratesLegacySchema proves the PRAGMA user_version gate: an old
// cache.db (log_seq/next_task_n columns, user_version 0) is transparently
// dropped and rebuilt at the current shape on open, a read re-projects the
// project from events.v2.jsonl and exposes the creation Ordinal, and the
// orphaned last_log_seq meta row does not survive.
//
// It also proves the ATM-0127 list-all regression stays fixed: ListProjects,
// called immediately after opening a fresh Store on a to-be-migrated cache
// (no ListTasks/GetTask "touch" of any project first), must return every
// on-disk v2 project. ensureV2CacheFresh only self-heals a project that a
// caller reads BY CODE; a plain ListProjects reading the (freshly emptied)
// projects table never does that, so before the eager-reprojection fix this
// returned an empty slice despite fully intact v2 logs on disk.
func TestCacheMigratesLegacySchema(t *testing.T) {
	dir := t.TempDir()

	// First store: create TWO v2 projects (each with a task), writing
	// events.v2.jsonl per project (the source of truth the second store will
	// re-project from) and a fresh cache. Two projects matter here: a single
	// project could accidentally pass via some other single-project code
	// path; only a list-all read across multiple projects catches the bug.
	s1, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.Init(""); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.CreateProject("ATM", "x", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.CreateTask("ATM", "t1", "d1", nil, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.CreateProject("BTM", "y", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.CreateTask("BTM", "t1", "d1", nil, testActor); err != nil {
		t.Fatal(err)
	}
	// Release the first store's handle so we can replace the file underneath it.
	if s1.cacheDBConn != nil {
		if err := s1.cacheDBConn.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(s1.cachePath() + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove %s: %v", s1.cachePath()+suffix, err)
		}
	}
	seedLegacyCacheDB(t, s1.cachePath(), "ATM")

	// Second store on the same dir: the first cache use runs the migration.
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	// The critical assertion: ListProjects, with NO prior per-project read,
	// must see both projects immediately after the migration.
	projects := s2.ListProjects()
	gotCodes := map[string]bool{}
	for _, p := range projects {
		gotCodes[p.Code] = true
	}
	if !gotCodes["ATM"] || !gotCodes["BTM"] {
		t.Fatalf("ListProjects immediately after migration = %+v, want both ATM and BTM present", projects)
	}

	tasks, err := s2.ListTasksErr(QueryFilters{Project: "ATM"})
	if err != nil {
		t.Fatalf("ListTasksErr after migration: %v", err)
	}
	if len(tasks) == 0 || tasks[0].Ordinal == 0 {
		t.Fatalf("expected migrated ordinal, got %+v", tasks)
	}

	db, err := s2.cacheDB()
	if err != nil {
		t.Fatal(err)
	}
	var uv int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&uv); err != nil {
		t.Fatal(err)
	}
	if uv != 4 {
		t.Fatalf("user_version = %d after migration, want 4", uv)
	}
	var stale int
	if err := db.QueryRow(`SELECT COUNT(*) FROM meta WHERE key LIKE 'last_log_seq:%'`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Fatalf("last_log_seq meta rows survived migration: %d", stale)
	}
}
