package eventlog

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"atm/internal/core"
)

// PruneLegacy retires an upgraded project's frozen log.jsonl. It refuses
// unless the project is v2-active and the caller's verifyClean gate passes; a
// v1-active or born-v2 project is skipped (Pruned=false, Reason set). By
// default the log is archived (recoverable); del=true removes it outright.
//
// verifyClean is the facade's clean-cutover gate (VerifyProject: the v2 file
// folds AND the read-cache is consistent with it). It runs under the project
// lock, between the log-exists check and the archive/delete, exactly where the
// pre-carve prune ran its VerifyProject call. The cache half of that check is a
// facade concern the engine has no handle on, so the engine takes the whole
// gate as a callback rather than reaching for the sqlite cache itself; a
// non-nil return refuses the prune with the caller's error verbatim.
func (e *Engine) PruneLegacy(code string, del bool, verifyClean func() error) (*core.PruneReport, error) {
	rep := &core.PruneReport{Project: code}
	err := e.WithLock(code, func() error {
		f, err := e.ProjectFormat(code)
		if err != nil {
			return err
		}
		if f != StoreFormatV2 {
			rep.Reason = "not v2-active"
			return nil
		}
		if _, err := os.Stat(e.LogPath(code)); os.IsNotExist(err) {
			rep.Reason = "born v2 (no v1 log)"
			return nil
		} else if err != nil {
			return err
		}
		// The v1 log is the only surviving pre-cutover copy; do not retire it
		// unless the caller's gate proves the live v2 state consistent.
		if err := verifyClean(); err != nil {
			return err
		}
		if del {
			if err := os.Remove(e.LogPath(code)); err != nil {
				return err
			}
			rep.Deleted = true
		} else {
			dst, err := e.archiveLogFileLocked(code)
			if err != nil {
				return err
			}
			rep.Archived = dst
		}
		rep.Pruned = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rep, nil
}

// archiveLogFileLocked moves log.jsonl aside under a collision-safe timestamped
// name (log.pruned.<unix-ts>[.n].jsonl) rather than deleting it outright.
// Caller holds the project lock.
func (e *Engine) archiveLogFileLocked(code string) (string, error) {
	path := e.LogPath(code)
	base := filepath.Join(e.projectDir(code), fmt.Sprintf("log.pruned.%d", time.Now().UTC().Unix()))
	for n := 0; ; n++ {
		dst := base + ".jsonl"
		if n > 0 {
			dst = fmt.Sprintf("%s.%d.jsonl", base, n)
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		if err := os.Rename(path, dst); err != nil {
			_ = os.Remove(dst)
			return "", err
		}
		return dst, nil
	}
}
