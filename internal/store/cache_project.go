package store

import (
	"database/sql"

	"atm/internal/core"
)

// projectSnapshot replaces the project's cache rows from a domain snapshot.
// Caller MUST hold the project lock (or be the cacheDB migration, which uses
// the DB-taking variant).
func (s *Store) projectSnapshot(code string, snap *core.ProjectSnapshot) error {
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.projectSnapshotDB(db, code, snap)
}

// projectSnapshotDB is projectSnapshot's DB-taking core. It does NOT call
// s.cacheDB(), so it is safe from inside cacheDB()'s cacheOnce.Do (the schema
// migration's eager reprojection) as well as from ordinary callers.
//
// The whole rewrite — delete, upserts, freshness row — runs in ONE SQLite
// transaction: a live-scale project is ~1,500 rows, and per-row implicit
// commits made each reprojection pay ~1,500 fsyncs (seconds of wall clock,
// ATM-d402aa). One transaction is one commit, and it also makes a crash
// mid-projection atomic instead of leaving a half-rewritten project.
//
// It preserves the row-level results cacheProjectFromV2StateDB produced: the
// upserts are independent rows keyed by id/name, so their relative order does
// not matter; the only order-sensitive part is delete-before-upsert per table,
// which is preserved (cacheDeleteProjectRows first, RemovedLabels deletes
// before label upserts).
func (s *Store) projectSnapshotDB(db *sql.DB, code string, snap *core.ProjectSnapshot) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := cacheDeleteProjectRows(tx, code); err != nil {
		return err
	}
	if snap.Project != nil {
		if err := cacheUpsertProject(tx, snap.Project); err != nil {
			return err
		}
	}
	for _, t := range snap.Tasks {
		if err := cacheUpsertTask(tx, t); err != nil {
			return err
		}
	}
	for _, c := range snap.Comments {
		if err := cacheUpsertComment(tx, c); err != nil {
			return err
		}
	}
	for _, name := range snap.RemovedLabels {
		if _, err := tx.Exec(`DELETE FROM labels WHERE name = ?`, name); err != nil {
			return err
		}
	}
	for _, l := range snap.Labels {
		if err := cacheUpsertLabel(tx, l); err != nil {
			return err
		}
	}
	if err := cacheSetV2Freshness(tx, code, snap.ChangeCount); err != nil {
		return err
	}
	return tx.Commit()
}

// reprojectTxn is the in-transaction projection every mutator ends with — the
// old reprojectV2Locked, split across the seam: the engine folds (cs.Snapshot
// re-reads the file strictly, including this transaction's own writes), the
// facade projects. A CLEAN transaction (no appends — e.g. SeedLabel of an
// already-live label) skips both the fold and the rewrite: the event file did
// not advance, so the cache cannot be behind this txn. That skip is what keeps
// the TUI's per-select EnsureVocabulary (4 idempotent seeds) from paying 4
// full reprojections (ATM-d402aa); healing a cache that was ALREADY stale
// belongs to ensureV2CacheFresh on the read side, not here.
func (s *Store) reprojectTxn(code string, cs core.ChangeSet) error {
	if !cs.Dirty() {
		return nil
	}
	snap, err := cs.Snapshot()
	if err != nil {
		return err
	}
	return s.projectSnapshot(code, snap)
}

// cacheDeleteProjectRows removes the project's task/comment/label rows and
// the project row itself — the per-project mirror of the global wipe
// Rebuild does. The labels table is store-global (merged across projects),
// so it has no project_code column; a project's own labels are scoped by
// the "<CODE>:" name prefix instead, the same scoping cacheListLabels uses.
// Sweeping here (not just deleting tombstoned names in the fold loop) matters
// because a label that was live in a previously-projected fold but is simply
// absent from the new one (e.g. a re-upgrade discarded the branch that
// upserted it) would otherwise never be visited, and its row would survive
// indefinitely.
func cacheDeleteProjectRows(x sqlExecer, code string) error {
	for _, stmt := range []string{
		`DELETE FROM comment_labels WHERE comment_id IN (SELECT c.id FROM comments c JOIN tasks t ON t.id = c.task_id WHERE t.project_code = ?)`,
		`DELETE FROM comments WHERE task_id IN (SELECT id FROM tasks WHERE project_code = ?)`,
		`DELETE FROM task_labels WHERE task_id IN (SELECT id FROM tasks WHERE project_code = ?)`,
		`DELETE FROM tasks WHERE project_code = ?`,
		`DELETE FROM projects WHERE code = ?`,
	} {
		if _, err := x.Exec(stmt, code); err != nil {
			return err
		}
	}
	if _, err := x.Exec(`DELETE FROM labels WHERE name LIKE ? ESCAPE '\'`, escapeLike(code)+":%"); err != nil {
		return err
	}
	return nil
}
