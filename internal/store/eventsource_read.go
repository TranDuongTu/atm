package store

import (
	"bytes"
	"os"

	"atm/libs/eventsource"
)

// rebuildProjectFromV2 re-derives the project's cache rows from the v2 event
// file: strict read, fold, project. There is no per-entity variant because
// cacheProjectFromV2State always projects the whole live set from one fold,
// and the freshness key is a whole-file event count. Caller MUST hold the
// project lock.
func (s *Store) rebuildProjectFromV2(code string) error {
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

// rebuildEntityCacheLocked dispatches a point-read rebuild by format: the v2
// fold for a v2-active project, the given v1 closure otherwise. Caller MUST
// hold the project lock (directly, or via the entry point's WithLock wrapper).
//
// Routing EVERY point-read rebuild closure through this — including the ones
// the *Locked variants pass — is what keeps a locked validation read (the ones
// v1 and v2 mutators perform mid-transaction) from replaying the frozen v1 log
// into the cache of a v2-active project.
func (s *Store) rebuildEntityCacheLocked(code string, v1 func() error) error {
	if f, err := s.projectFormat(code); err != nil {
		return err
	} else if f == StoreFormatV2 {
		return s.rebuildProjectFromV2(code)
	}
	return v1()
}

// v2EventCount is the number of COMMITTED events in a project's event file:
// the number of newline-terminated lines, counted without parsing (the commit
// point is a complete line — L3-7 — so any unterminated tail is uncommitted and
// correctly excluded). A missing file counts as zero.
func (s *Store) v2EventCount(code string) (int, error) {
	raw, err := os.ReadFile(s.eventsV2Path(code))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return bytes.Count(raw, []byte("\n")), nil
}

// v2CacheFresh reports whether the project's cache rows were projected from the
// event file as it stands now, by comparing the freshness row
// (cacheGetV2Freshness — the event count of the file the cache was last
// projected from) against the current event count. A missing freshness row
// means "never projected from a v2 file" and is never fresh, so it can be told
// apart from a genuine projection at count 0.
//
// The v1 last_log_seq freshness key is meaningless here: a v2 cache row's
// Ordinal holds a creation ordinal from the fold, unrelated to any v1 log seq.
func (s *Store) v2CacheFresh(code string) (bool, error) {
	db, err := s.cacheDB()
	if err != nil {
		return false, err
	}
	got, ok, err := cacheGetV2Freshness(db, code)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	n, err := s.v2EventCount(code)
	if err != nil {
		return false, err
	}
	return got == n, nil
}

// ensureV2CacheFresh rebuilds the project's cache rows from the v2 fold iff the
// freshness probe says the cache is behind the event file. It takes the project
// lock; NEVER call it from a *Locked context (point reads use v2CacheFresh plus
// their format-aware rebuild closure instead, which preserves the locked /
// unlocked split the *WithRebuild bodies are parameterized on).
func (s *Store) ensureV2CacheFresh(code string) error {
	if fresh, err := s.v2CacheFresh(code); err != nil {
		return err
	} else if fresh {
		return nil
	}
	return s.WithLock(code, func() error {
		// Re-probe under the lock: another process may have projected the cache
		// while this reader waited, and reprojecting is a full delete+insert of
		// every row in the project.
		if fresh, err := s.v2CacheFresh(code); err != nil || fresh {
			return err
		}
		return s.rebuildProjectFromV2(code)
	})
}
