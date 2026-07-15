package eventsync

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"atm/internal/eventsource"
)

// DirTarget is a SyncTarget whose remote is a plain directory tree: each
// project's full event log lives at <root>/<CODE>/events.v2.jsonl, one
// canonical event line per line, mirroring how a local v2 store lays
// projects out on disk. Publish never rewrites or renames that file — it
// only appends — so any prefix that existed before a call still exists
// after it, and two concurrent publishers can at worst interleave whole
// lines, never tear one another's.
type DirTarget struct {
	root string
}

// NewDirTarget returns a DirTarget rooted at root, a directory holding
// one subdirectory per project code.
func NewDirTarget(root string) *DirTarget {
	return &DirTarget{root: root}
}

func (d *DirTarget) eventsPath(project string) string {
	return filepath.Join(d.root, project, "events.v2.jsonl")
}

// Fetch reads project's full event log. A missing file means the
// project doesn't exist on this target yet: RemoteSnapshot{Absent: true},
// not an error. Every line is re-parsed via eventsource.Parse to
// recompute its id — the wire is never trusted — and repeats of an id
// already seen are dropped, same as Plan does for a remote's repeats. A
// final line with no trailing newline is an uncommitted, torn write and
// is skipped, matching the store's own commit-point rule: a line only
// counts once its trailing newline has landed.
func (d *DirTarget) Fetch(ctx context.Context, project string) (*RemoteSnapshot, error) {
	data, err := os.ReadFile(d.eventsPath(project))
	if err != nil {
		if os.IsNotExist(err) {
			return &RemoteSnapshot{Absent: true}, nil
		}
		return nil, fmt.Errorf("eventsync: dirtarget: read %s: %w", project, err)
	}

	lines := bytes.Split(data, []byte("\n"))
	lines = lines[:len(lines)-1] // drop the tail: "" after a trailing \n, or a torn line without one

	var events []RawEvent
	var ids []string
	seen := make(map[string]bool, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		raw := make([]byte, len(line))
		copy(raw, line) // independent of data's backing array, which Fetch doesn't retain otherwise

		ev, err := eventsource.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("eventsync: dirtarget: %s: %w", project, err)
		}
		if seen[ev.ID] {
			continue
		}
		seen[ev.ID] = true
		events = append(events, RawEvent{ID: ev.ID, Raw: raw})
		ids = append(ids, ev.ID)
	}

	return &RemoteSnapshot{Events: events, Digest: SetDigest(ids)}, nil
}

// Publish appends missing's canonical lines to project's event log,
// creating the project's directory and log file on first publish. It
// opens the file O_APPEND and writes one line per event, so a write can
// only extend the file, never rewrite or reorder what's already there —
// the on-disk history is a pure append log. base is unused: a directory
// target has no compare-and-swap state to round-trip.
func (d *DirTarget) Publish(ctx context.Context, project string, missing []RawEvent, base *RemoteSnapshot) error {
	dir := filepath.Join(d.root, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("eventsync: dirtarget: mkdir %s: %w", project, err)
	}

	f, err := os.OpenFile(d.eventsPath(project), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("eventsync: dirtarget: open %s: %w", project, err)
	}
	defer f.Close()

	for _, ev := range missing {
		line := make([]byte, len(ev.Raw)+1)
		copy(line, ev.Raw)
		line[len(ev.Raw)] = '\n'
		if _, err := f.Write(line); err != nil {
			return fmt.Errorf("eventsync: dirtarget: write %s: %w", project, err)
		}
	}
	return f.Sync()
}
