package runtime

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"atm/internal/store/fsio"
)

// Retention constants (spec §4.4). No config knob until someone needs one.
const (
	DefaultOlderThan = 24 * time.Hour
	DefaultKeep      = 50
)

// PruneOptions override the defaults for one call. All removes every
// readable non-live record regardless of age or cap.
type PruneOptions struct {
	OlderThan time.Duration
	Keep      int
	All       bool
}

// PruneResult names what went: records (by run id) and orphan prompt files
// (by path).
type PruneResult struct {
	Removed []string
	Orphans []string
}

// Prune applies the cycling rules in order: live records are never touched;
// ended and lost records older than OlderThan go; of what is left at most
// Keep non-live records survive, oldest first; then every context file with
// no record is swept. Unreadable records are neither pruned nor orphaned —
// an older binary must not delete what a newer one wrote.
func (r *Registry) Prune(opts PruneOptions) (PruneResult, error) {
	if opts.OlderThan <= 0 {
		opts.OlderThan = DefaultOlderThan
	}
	if opts.Keep <= 0 {
		opts.Keep = DefaultKeep
	}
	var res PruneResult
	err := fsio.WithLock(r.root, lockName, func() error {
		entries, err := r.List()
		if err != nil {
			return err
		}
		now := r.now()
		var candidates []Entry
		known := map[string]bool{}
		for _, e := range entries {
			known[e.Record.RunID] = true
			if e.Liveness == LiveLive || e.Liveness == LiveUnreadable {
				continue
			}
			candidates = append(candidates, e)
		}
		// Newest first, by end time (lost records have no end: use start).
		sort.SliceStable(candidates, func(i, j int) bool {
			return lastSeen(candidates[i].Record) > lastSeen(candidates[j].Record)
		})
		for i, e := range candidates {
			age := now.Sub(parseStamp(lastSeen(e.Record)))
			if !opts.All && i < opts.Keep && age <= opts.OlderThan {
				continue
			}
			if err := r.remove(e.Record); err != nil {
				return err
			}
			delete(known, e.Record.RunID)
			res.Removed = append(res.Removed, e.Record.RunID)
		}
		res.Orphans = r.sweepOrphans(known)
		return nil
	})
	return res, err
}

func lastSeen(rec Record) string {
	if rec.EndedAt != "" {
		return rec.EndedAt
	}
	return rec.StartedAt
}

func parseStamp(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{} // unparseable → infinitely old → pruned
	}
	return t
}

// remove deletes the record and its context file. The context file is only
// deleted when it lives in a cache/sessions directory: a record pointing
// anywhere else is not trusted with a delete.
func (r *Registry) remove(rec Record) error {
	if rec.ContextPath != "" && filepath.Base(filepath.Dir(rec.ContextPath)) == "sessions" {
		if err := os.Remove(rec.ContextPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Remove(r.path(rec.RunID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// sweepOrphans deletes every *.md under <store>/cache/sessions and
// <store>/projects/*/cache/sessions whose stem is not a known run id.
func (r *Registry) sweepOrphans(known map[string]bool) []string {
	store := filepath.Dir(r.root)
	dirs := []string{ContextDir(store, "")}
	if codes, err := os.ReadDir(filepath.Join(store, "projects")); err == nil {
		for _, c := range codes {
			if c.IsDir() {
				dirs = append(dirs, ContextDir(store, c.Name()))
			}
		}
	}
	var gone []string
	for _, dir := range dirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			if known[strings.TrimSuffix(f.Name(), ".md")] {
				continue
			}
			p := filepath.Join(dir, f.Name())
			if err := os.Remove(p); err == nil {
				gone = append(gone, p)
			}
		}
	}
	return gone
}
