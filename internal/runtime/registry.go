package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"atm/internal/core"
	"atm/internal/store/fsio"
)

var (
	ErrNotFound   = errors.New("session run not found")
	ErrUnreadable = errors.New("session record written by a newer atm")
)

// runIDRe keeps run ids path-safe: the id is the filename.
var runIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

const lockName = "sessions"

// Registry reads and writes the session records under one store.
type Registry struct {
	root  string // <store>/runtime
	now   func() time.Time
	alive func(pid int) bool
}

// Open returns the registry for a store. Nothing is created until the first
// write.
func Open(storePath string) *Registry {
	return &Registry{root: filepath.Join(storePath, "runtime"), now: core.Now, alive: pidAlive}
}

func (r *Registry) WithClock(now func() time.Time) *Registry    { r.now = now; return r }
func (r *Registry) WithPIDCheck(alive func(int) bool) *Registry { r.alive = alive; return r }

// Dir is the sessions directory.
func (r *Registry) Dir() string { return filepath.Join(r.root, "sessions") }

func (r *Registry) path(runID string) string { return filepath.Join(r.Dir(), runID+".json") }

func (r *Registry) stamp() string { return core.RFC3339UTC(r.now()) }

// pidAlive is the production liveness probe: kill(pid, 0) succeeds or fails
// with EPERM when the process exists.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := unix.Kill(pid, 0)
	return err == nil || errors.Is(err, unix.EPERM)
}

// Create writes a new record. Schema, StartedAt, and a working/launcher
// status are filled in when absent. It refuses an unsafe run id and an
// existing record (a run id is minted once).
func (r *Registry) Create(rec Record) error {
	if !runIDRe.MatchString(rec.RunID) {
		return fmt.Errorf("invalid run id %q", rec.RunID)
	}
	rec.Schema = Schema
	if rec.StartedAt == "" {
		rec.StartedAt = r.stamp()
	}
	if rec.Status.State == "" {
		rec.Status = Status{State: StateWorking, Source: "launcher", At: rec.StartedAt}
	}
	return fsio.WithLock(r.root, lockName, func() error {
		if _, err := os.Stat(r.path(rec.RunID)); err == nil {
			return fmt.Errorf("run %s already registered", rec.RunID)
		}
		return fsio.WriteFileAtomic(r.path(rec.RunID), rec)
	})
}

// Get reads one record. ErrNotFound when absent, ErrUnreadable when its
// schema is newer than this binary's.
func (r *Registry) Get(runID string) (*Record, error) {
	if !runIDRe.MatchString(runID) {
		return nil, ErrNotFound
	}
	return r.read(runID)
}

func (r *Registry) read(runID string) (*Record, error) {
	var rec Record
	err := fsio.ReadJSON(r.path(runID), &rec)
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	if rec.Schema > Schema {
		return nil, ErrUnreadable
	}
	return &rec, nil
}

// update applies fn to the record under the lock and writes it back.
func (r *Registry) update(runID string, fn func(*Record)) error {
	if !runIDRe.MatchString(runID) {
		return ErrNotFound
	}
	return fsio.WithLock(r.root, lockName, func() error {
		rec, err := r.read(runID)
		if err != nil {
			return err
		}
		fn(rec)
		return fsio.WriteFileAtomic(r.path(runID), rec)
	})
}

// SetStatus overwrites the status block (and the watch block when w is
// non-nil). Identity fields and ended_at are never touched, so a late hook
// cannot revive an ended run.
func (r *Registry) SetStatus(runID string, st Status, w *Watch) error {
	return r.update(runID, func(rec *Record) {
		if st.At == "" {
			st.At = r.stamp()
		}
		if st.Source == "" {
			st.Source = "hook"
		}
		rec.Status = st
		if w != nil {
			rec.Watch = w
		}
	})
}

// End stamps ended_at, the exit code, and the ended state.
func (r *Registry) End(runID string, exitCode int) error {
	return r.update(runID, func(rec *Record) {
		now := r.stamp()
		rec.EndedAt = now
		rec.ExitCode = &exitCode
		rec.Status = Status{State: StateEnded, At: now, Source: "launcher"}
	})
}

// Liveness derives the record's life from ended_at and the launcher pid.
func (r *Registry) Liveness(rec Record) Liveness {
	switch {
	case rec.Schema > Schema:
		return LiveUnreadable
	case rec.EndedAt != "":
		return LiveEnded
	case r.alive(rec.LauncherPID):
		return LiveLive
	default:
		return LiveLost
	}
}

// Entry is one listed record with its derived liveness.
type Entry struct {
	Record   Record
	Liveness Liveness
}

// List returns every record, newest StartedAt first, unreadable ones
// included (RunID taken from the filename so a row can still name them).
func (r *Registry) List() ([]Entry, error) {
	names, err := os.ReadDir(r.Dir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, d := range names {
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(d.Name(), ".json")
		rec, err := r.read(id)
		if err != nil {
			out = append(out, Entry{Record: Record{RunID: id, Schema: Schema + 1}, Liveness: LiveUnreadable})
			continue
		}
		out = append(out, Entry{Record: *rec, Liveness: r.Liveness(*rec)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Record.StartedAt != out[j].Record.StartedAt {
			return out[i].Record.StartedAt > out[j].Record.StartedAt
		}
		return out[i].Record.RunID > out[j].Record.RunID
	})
	return out, nil
}
