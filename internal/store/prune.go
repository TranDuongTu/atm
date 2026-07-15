package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PruneReport is the per-project outcome of `atm store prune-v1`.
type PruneReport struct {
	Project  string `json:"project"`
	Pruned   bool   `json:"pruned"`
	Archived string `json:"archived,omitempty"`
	Deleted  bool   `json:"deleted,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// PruneProjectV1 retires an upgraded project's frozen log.jsonl. It refuses
// unless the project is v2-active and verifies clean; a v1-active or born-v2
// project is skipped (Pruned=false, Reason set). By default the log is
// archived (recoverable); del=true removes it outright.
func (s *Store) PruneProjectV1(code string, del bool) (*PruneReport, error) {
	rep := &PruneReport{Project: code}
	err := s.WithLock(code, func() error {
		f, err := s.projectFormat(code)
		if err != nil {
			return err
		}
		if f != StoreFormatV2 {
			rep.Reason = "not v2-active"
			return nil
		}
		if _, err := os.Stat(s.logPath(code)); os.IsNotExist(err) {
			rep.Reason = "born v2 (no v1 log)"
			return nil
		} else if err != nil {
			return err
		}
		// The v1 log is the only surviving pre-cutover copy; do not retire it
		// unless the live v2 cache is provably consistent with the event file.
		vr, err := s.VerifyProject(code)
		if err != nil {
			return err
		}
		if vr.Diverged || !vr.LogOK {
			return fmt.Errorf("%w: project %q does not verify clean; refusing to prune", ErrIntegrity, code)
		}
		if del {
			if err := os.Remove(s.logPath(code)); err != nil {
				return err
			}
			rep.Deleted = true
		} else {
			dst, err := s.archiveLogFileLocked(code)
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
func (s *Store) archiveLogFileLocked(code string) (string, error) {
	path := s.logPath(code)
	base := filepath.Join(s.projectDir(code), fmt.Sprintf("log.pruned.%d", time.Now().UTC().Unix()))
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

// ProjectCodes enumerates every project code on disk under projects/, sorted.
// It is the exported enumeration surface the CLI's `--all` verbs (prune-v1
// included) drive over; internally it delegates to projectCodesOnDisk, which
// every other store method (Verify, UpgradeAllToV2, Rebuild) already uses.
func (s *Store) ProjectCodes() ([]string, error) {
	return s.projectCodesOnDisk()
}
