package store

import (
	"fmt"
	"os"
	"path/filepath"

	"atm/internal/eventsource"
)

// UpgradeReport is the per-project outcome of an upgrade to v2 media.
// AlreadyV2 marks a project `upgrade --all` SKIPPED because its effective
// format was already v2 (nothing on disk was touched for it).
type UpgradeReport struct {
	Project      string      `json:"project"`
	Format       StoreFormat `json:"format"`
	Events       int         `json:"events"`
	ArchivedPath string      `json:"archived_path,omitempty"`
	AlreadyV2    bool        `json:"already_v2,omitempty"`
}

// UpgradeProjectToV2 converts a v1-active project's frozen log.jsonl into
// events.v2.jsonl and cuts the project over to v2 media. log.jsonl is never
// written by this path: it stays byte-identical, and is what the read-back
// guard below replays for its final comparison. There is no rollback: once a
// project is cut over, it stays v2 forever.
//
// The ordering below is load-bearing: write a TEMP candidate, verify it,
// semantically compare it to the v1 replay, and only then rename it into
// place. Every failure before step 4 leaves the v1 log and any existing
// events.v2.jsonl exactly as they were.
func (s *Store) UpgradeProjectToV2(code string) (*UpgradeReport, error) {
	// GUARD (spec L3-5): upgrade reads FROM the frozen v1 log, so it is
	// legal only while the project's EFFECTIVE format is v1. Running it
	// against an effective-v2 project would rebuild from stale v1 bytes;
	// against a v2-BORN project it would hard-fail on the missing
	// log.jsonl. With no rollback, a project is upgraded at most once.
	if f, err := s.projectFormat(code); err != nil {
		return nil, err
	} else if f == StoreFormatV2 {
		return nil, fmt.Errorf("%w: project %q is already v2-active; upgrade reads from the v1 log and is only legal for v1-active projects", ErrConflict, code)
	}
	rep := &UpgradeReport{Project: code, Format: StoreFormatV2}
	err := s.WithLock(code, func() error {
		// Re-check under the lock: a concurrent upgrade of the same project
		// may have cut it over between the guard above and here, and the
		// stale v1 bytes we would rebuild from would clobber its live file.
		if f, err := s.projectFormat(code); err != nil {
			return err
		} else if f == StoreFormatV2 {
			return fmt.Errorf("%w: project %q is already v2-active; upgrade reads from the v1 log and is only legal for v1-active projects", ErrConflict, code)
		}
		raw, err := os.ReadFile(s.logPath(code))
		if err != nil {
			return err
		}
		up, err := eventsource.UpgradeV1(raw)
		if err != nil {
			return err
		}

		// 1. Write the candidate file. Nothing existing is touched yet.
		tmp := s.eventsV2Path(code) + ".tmp"
		if err := os.MkdirAll(filepath.Dir(tmp), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		for _, ev := range up.Events {
			if _, err := f.Write(ev.Raw); err != nil {
				_ = f.Close()
				_ = os.Remove(tmp)
				return err
			}
			if _, err := f.Write([]byte("\n")); err != nil {
				_ = f.Close()
				_ = os.Remove(tmp)
				return err
			}
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(tmp)
			return err
		}

		// 2. Verify the candidate BEFORE it becomes events.v2.jsonl (L3-3):
		// re-read it, recompute every id, validate parents, build the DAG,
		// and fold.
		snap, err := s.readV2FileAt(tmp, false)
		if err != nil {
			_ = os.Remove(tmp)
			return err
		}
		state, err := eventsource.FoldEvents(snap.Events)
		if err != nil {
			_ = os.Remove(tmp)
			return err
		}

		// 3. Semantic comparison against the current v1 replay (spec upgrade
		// step 6): a read-back guard over the user's actual on-disk data at
		// cutover time, distinct from UpgradeV1's internal self-verify above.
		v1rep, err := eventsource.ReplayV1(raw)
		if err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if err := eventsource.CompareReplayToFold(v1rep, state); err != nil {
			_ = os.Remove(tmp)
			return err
		}

		// 4. With no rollback, a project is never re-upgraded: a pre-existing
		// events.v2.jsonl at cutover is an error, not a displacement. A
		// failed upgrade must leave both the v1 log and any prior v2 file
		// exactly as they were.
		if _, err := os.Stat(s.eventsV2Path(code)); err == nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("%w: project %q already has a v2 file", ErrConflict, code)
		} else if !os.IsNotExist(err) {
			_ = os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, s.eventsV2Path(code)); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if err := s.cacheProjectFromV2State(code, state, snap.EventCount); err != nil {
			return err
		}
		if err := s.setProjectFormat(code, StoreFormatV2); err != nil {
			return err
		}
		// 5. Wipe the vector indexes (spec L3-15). Their entries are keyed
		// by the v1 log seq, which is meaningless under v2 and would poison
		// dedupVectorsByID and staleness checks; the indexer re-embeds from
		// the v2 fold by text hash (Task 9b).
		_ = os.RemoveAll(s.vectorsDir(code))
		// 6. Drop any memoized v1 log snapshot: a long-lived process (the
		// TUI) must not keep serving pre-cutover ReadLogCached entries
		// across the format switch.
		s.invalidateLogSnapshot(code)
		rep.Events = snap.EventCount
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rep, nil
}

// UpgradeAllToV2 upgrades every v1-active project on disk and then flips the
// store default so NEW projects are born v2. A failure on any project returns
// the reports collected so far WITHOUT flipping: the already-cut-over
// projects stay v2 (their media is complete), and a retry resumes.
func (s *Store) UpgradeAllToV2() ([]UpgradeReport, error) {
	codes, err := s.projectCodesOnDisk()
	if err != nil {
		return nil, err
	}
	out := make([]UpgradeReport, 0, len(codes))
	for _, code := range codes {
		// SKIP effective-v2 projects instead of letting the per-project
		// guard error: a RETRY of a partially-failed `upgrade --all` (A cut
		// over, B failed, no flip) must neither re-upgrade A from its frozen
		// v1 log nor hard-fail on a v2-born project's missing log.jsonl
		// after damaging earlier projects. Skipped projects count as
		// already-upgraded for the ActiveFormat flip decision below.
		if f, err := s.projectFormat(code); err != nil {
			return out, err
		} else if f == StoreFormatV2 {
			out = append(out, UpgradeReport{Project: code, Format: StoreFormatV2, AlreadyV2: true})
			continue
		}
		rep, err := s.UpgradeProjectToV2(code)
		if err != nil {
			return out, err
		}
		out = append(out, *rep)
	}
	// Every project now holds an explicit ProjectFormats entry, so flipping
	// the store default cannot change how any existing project is read — it
	// only makes NEW projects be born v2 (spec L3-14). A partial failure
	// returns above without flipping. SetActiveFormat re-checks the
	// explicit-entry invariant, which is trivially satisfied here.
	if err := s.SetActiveFormat(StoreFormatV2); err != nil {
		return out, err
	}
	return out, nil
}
