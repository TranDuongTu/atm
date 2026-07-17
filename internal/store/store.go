package store

import (
	"atm/internal/core"
	"atm/internal/store/eventlog"
	"crypto/rand"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

type Store struct {
	Root string

	cacheOnce   sync.Once
	cacheDBConn *sql.DB
	cacheErr    error

	// builtinsOnce seeds the built-in personas (developer/manager/admin) once
	// per Store on the first validateActor call, skipping actor validation
	// (the personas being created cannot yet satisfy it).
	builtinsOnce sync.Once
	builtinsErr  error

	// logSnapshot memoizes per-project parsed log entries for the TUI's
	// lifetime, so the per-frame renderSummary path doesn't re-scan
	// log.jsonl. Invalidated against the O(1) LastLogSeq cache row: when
	// the cached last_seq advances (via an append in this or another
	// process), the snapshot is dropped and re-scanned on the next call.
	logSnapMu    sync.Mutex
	logSnapshots map[string]logSnapshot

	// Determinism seams for v2 authoring (Task B1). All three default to nil
	// in Open, and Open fills in the production defaults (wall clock,
	// crypto/rand.Reader) when unset -- so store.Open(root) with no options
	// is byte-for-byte the pre-seam behavior. Tests pin these via WithClock/
	// WithReplicaEntropy/WithNow to make v2 hash aliases reproducible.
	clockNow       func() int64     // nil => eventsource.NewClock uses wall clock
	replicaEntropy io.Reader        // nil => rand.Reader (defaulted in Open)
	nowFn          func() time.Time // nil => time.Now().UTC (defaulted in Open)

	// eng is the event-log write-engine (internal/store/eventlog). Its
	// OnProject/OnMediaReplaced hooks (wired in Open) let engine-internal
	// projection paths (sync ingest/bootstrap, upgrade) write through the
	// facade's read-model cache under the project lock.
	eng *eventlog.Engine
}

// Store satisfies core's service seam structurally (refactor step 4).
var _ core.Service = (*Store)(nil)

// logSnapshot holds the parsed log entries for one project plus the
// last_log_seq value the snapshot was built against. When LastLogSeq(code)
// returns a value greater than builtSeq, the snapshot is stale and must be
// rebuilt.
type logSnapshot struct {
	entries  []LogEntry
	builtSeq int
}

// Option configures determinism seams on a Store at Open time. Production
// callers pass none, which keeps Open's behavior byte-for-byte identical to
// before this type existed (wall clock + crypto/rand.Reader).
type Option func(*Store)

// WithClock fixes the millisecond source feeding the v2 HLC clock used by
// v2 authoring (the eventlog engine's beginAuthorLocked). Production
// omits it, leaving eventsource.NewClock to read the wall clock. Tests pass
// a counter so successive HLC ticks -- and therefore minted hex aliases --
// are reproducible.
func WithClock(f func() int64) Option { return func(s *Store) { s.clockNow = f } }

// WithReplicaEntropy fixes the entropy source the store's replica/instance
// ids are minted from (eventsource_replica.go's ensureReplicaForWriteLocked).
// Production omits it, leaving crypto/rand.Reader in place.
func WithReplicaEntropy(r io.Reader) Option { return func(s *Store) { s.replicaEntropy = r } }

// WithNow fixes the wall-clock source backing Store.Now(), which stamps the
// `at` field on v2-authored events. Production omits it, leaving Now() at
// time.Now().UTC().
func WithNow(f func() time.Time) Option { return func(s *Store) { s.nowFn = f } }

// Now returns the current time as seen by this Store instance, honoring
// WithNow if set. Production stores (opened with no options) get
// time.Now().UTC(), identical to the package-level Now() below. v2 authoring
// stamps event `at` fields through this method so tests can pin it via
// WithNow; everything else in the store continues to use the package-level
// Now().
func (s *Store) Now() time.Time { return s.nowFn() }

var projectCodeRe = regexp.MustCompile(`^[A-Z]{3,6}$`)

func ValidateProjectCode(code string) error {
	if !projectCodeRe.MatchString(code) {
		return fmt.Errorf("%w: invalid project code %q (want ^[A-Z]{3,6}$)", core.ErrUsage, code)
	}
	return nil
}

var labelRe = regexp.MustCompile(`^[A-Z]{3,6}:[a-z0-9][a-z0-9-]*(:([a-z0-9][a-z0-9-]*|\*))?$`)

func ValidateLabelName(name string) error {
	if !labelRe.MatchString(name) {
		return fmt.Errorf("invalid label %q (want ^[A-Z]{3,6}:[a-z0-9][a-z0-9-]*(:([a-z0-9][a-z0-9-]*|\\*))?$)", name)
	}
	return nil
}

func RenderTaskID(code string, n int) string {
	if n < 10000 {
		return fmt.Sprintf("%s-%04d", code, n)
	}
	return fmt.Sprintf("%s-%d", code, n)
}

func SortTaskIDs(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool {
		ci, ni, _ := core.ParseTaskID(ids[i])
		cj, nj, _ := core.ParseTaskID(ids[j])
		if ci != cj {
			return ci < cj
		}
		if ni != nj {
			return ni < nj
		}
		return ids[i] < ids[j]
	})
}

// commentTaskAlias returns the task alias a comment id belongs to — the
// prefix before the "-c<suffix>" segment. Well-defined for BOTH generations
// because v1 RenderCommentID and v2 MintCommentAlias both build the comment
// id as <task-alias>-c<suffix>.
func commentTaskAlias(id string) (string, bool) {
	m := core.CommentIDRe.FindStringSubmatch(id)
	if m == nil {
		return "", false
	}
	return m[1] + "-" + m[2], true
}

func RenderCommentID(taskID string, n int) string {
	if n < 10000 {
		return fmt.Sprintf("%s-c%04d", taskID, n)
	}
	return fmt.Sprintf("%s-c%d", taskID, n)
}

func ResolveStorePath(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	if env := os.Getenv("ATM_HOME"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "atm")
	}
	return filepath.Join(home, ".config", "atm")
}

func Open(root string, opts ...Option) (*Store, error) {
	if root == "" {
		root = ResolveStorePath("")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	s := &Store{Root: abs}
	for _, o := range opts {
		o(s)
	}
	if s.replicaEntropy == nil {
		s.replicaEntropy = rand.Reader
	}
	if s.nowFn == nil {
		s.nowFn = func() time.Time { return time.Now().UTC() }
	}
	s.eng = eventlog.New(abs, eventlog.Options{
		ClockNow:       s.clockNow,
		ReplicaEntropy: s.replicaEntropy,
		Now:            s.nowFn,
		OnProject:      func(code string, snap *core.ProjectSnapshot) error { return s.projectSnapshot(code, snap) },
		OnMediaReplaced: func(code string) {
			_ = os.RemoveAll(s.vectorsDir(code))
			s.invalidateLogSnapshot(code)
		},
	})
	return s, nil
}

func (s *Store) Init(storePath string) error {
	if storePath != "" {
		abs, err := filepath.Abs(storePath)
		if err != nil {
			return err
		}
		s.Root = abs
	}
	if s.Root == "" {
		root, err := Open("")
		if err != nil {
			return err
		}
		s.Root = root.Root
	}
	if err := os.MkdirAll(s.projectsDir(), 0o755); err != nil {
		return err
	}
	if _, err := s.cacheDB(); err != nil {
		return err
	}
	// Materialize store.json (defaults included) under the store-scoped lock:
	// an init racing a concurrent writer must not clobber its update.
	return s.eng.MutateStoreMeta(func(*eventlog.StoreMeta) error { return nil })
}

func (s *Store) StorePath() string { return s.Root }

// ensureBuiltinPersonas seeds developer/manager/admin once per Store, skipping
// actor validation (the personas being created cannot yet satisfy it). Called
// lazily by validateActor before the first mutation that needs a registered
// persona. Safe to call repeatedly; only the first call's error is retained.
func (s *Store) ensureBuiltinPersonas() error {
	s.builtinsOnce.Do(func() {
		_, s.builtinsErr = s.SeedPersonas("admin@atm:seed")
	})
	return s.builtinsErr
}

func (s *Store) projectsDir() string { return filepath.Join(s.Root, "projects") }
func (s *Store) projectDir(code string) string {
	return filepath.Join(s.projectsDir(), code)
}
func (s *Store) lockPath(code string) string {
	return filepath.Join(s.projectsDir(), code+".lock")
}
func (s *Store) configPath(code string) string {
	return filepath.Join(s.projectDir(code), "config.json")
}
func (s *Store) vectorsDir(code string) string { return filepath.Join(s.projectDir(code), "vectors") }
func (s *Store) vectorPath(code, slug string) string {
	return filepath.Join(s.vectorsDir(code), slug+".jsonl")
}
func (s *Store) vectorMetaPath(code, slug string) string {
	return filepath.Join(s.vectorsDir(code), slug+".meta.json")
}
func (s *Store) inquiryLogPath(code string) string {
	return filepath.Join(s.projectDir(code), "inquiry-log.jsonl")
}

// storeMetaPath is the store.json path. The engine (internal/store/eventlog)
// owns the reads and writes of store.json now; this facade helper survives
// only for store-package tests that materialize a corrupt store.json to
// exercise error paths. Kept in sync with eventlog.Engine.storeMetaPath.
func (s *Store) storeMetaPath() string { return filepath.Join(s.Root, "store.json") }

// projectCodesOnDisk delegates to the event-log engine, which owns the
// projects/<CODE>/ enumeration (moved there in refactor step 6). Verify/
// Rebuild still call it through the facade.
func (s *Store) projectCodesOnDisk() ([]string, error) { return s.eng.ProjectCodesOnDisk() }
