package store

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
	identity TEXT NOT NULL DEFAULT '',
	-- capabilities is NULL when no capability event was ever recorded (a
	-- legacy project, read as "all built-ins enabled" by consumers), or a
	-- comma-joined list otherwise ('' encodes a non-nil empty set, i.e.
	-- explicitly none). Mirrors core.Project.Capabilities's nil-vs-empty split.
	capabilities TEXT
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
