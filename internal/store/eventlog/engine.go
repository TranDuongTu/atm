// Package eventlog is the event-sourced write-engine behind internal/store:
// the ONLY package in the repository that imports atm/libs/eventsource. It
// owns events.v2.jsonl and store.json, authors and ingests events, and hands
// state upward exclusively as core-typed snapshots. The facade supplies the
// read-model projection through the OnProject hook; the engine never touches
// sqlite.
package eventlog

import (
	"io"
	"path/filepath"
	"time"

	"atm/internal/core"
	"atm/internal/store/fsio"
)

// Options carries the determinism seams (mirroring store.WithClock /
// WithReplicaEntropy / WithNow — the facade threads them through) and the
// facade hooks. All fields must be non-nil except the hooks, which are only
// invoked by paths that project (nil is fine for engines used in tests that
// never sync or upgrade).
type Options struct {
	ClockNow       func() int64 // nil => eventsource.NewClock uses wall clock
	ReplicaEntropy io.Reader
	Now            func() time.Time
	// OnProject projects a snapshot into the facade's read-model. Invoked
	// under the project lock at the exact points the pre-carve code called
	// reprojectV2Locked from engine-internal paths (sync ingest/bootstrap,
	// upgrade).
	OnProject func(code string, snap *core.ProjectSnapshot) error
	// OnMediaReplaced runs after an upgrade replaces a project's media
	// (facade drops its log memo and wipes vector indexes).
	OnMediaReplaced func(code string)
}

type Engine struct {
	root string
	opts Options
}

func New(root string, o Options) *Engine { return &Engine{root: root, opts: o} }

func (e *Engine) now() time.Time { return e.opts.Now() }

func (e *Engine) projectsDir() string { return filepath.Join(e.root, "projects") }
func (e *Engine) projectDir(code string) string {
	return filepath.Join(e.projectsDir(), code)
}
func (e *Engine) EventsV2Path(code string) string {
	return filepath.Join(e.projectDir(code), "events.v2.jsonl")
}
func (e *Engine) LogPath(code string) string {
	return filepath.Join(e.projectDir(code), "log.jsonl")
}
func (e *Engine) eventsourceMetaPath(code string) string {
	return filepath.Join(e.projectDir(code), "eventsource.json")
}
func (e *Engine) storeMetaPath() string { return filepath.Join(e.root, "store.json") }

// WithLock delegates to the shared fsio primitive (same registry the facade
// uses, so facade-held and engine-held locks exclude each other correctly).
func (e *Engine) WithLock(name string, fn func() error) error {
	return fsio.WithLock(e.projectsDir(), name, fn)
}
