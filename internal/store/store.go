package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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
}

// logSnapshot holds the parsed log entries for one project plus the
// last_log_seq value the snapshot was built against. When LastLogSeq(code)
// returns a value greater than builtSeq, the snapshot is stale and must be
// rebuilt.
type logSnapshot struct {
	entries  []LogEntry
	builtSeq int
}

func RFC3339UTC(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func Now() time.Time {
	return time.Now().UTC()
}

var projectCodeRe = regexp.MustCompile(`^[A-Z]{3,6}$`)

func ValidateProjectCode(code string) error {
	if !projectCodeRe.MatchString(code) {
		return fmt.Errorf("%w: invalid project code %q (want ^[A-Z]{3,6}$)", ErrUsage, code)
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

// IsNamespaceName reports whether name is a namespace label (e.g. "ATM:status:*"),
// whose membership is every label sharing its prefix.
func IsNamespaceName(name string) bool { return strings.HasSuffix(name, ":*") }

// TaskIDRe accepts both alias generations: v1 numeric ids ("ATM-0001") and
// v2 hash aliases ("ATM-7f3a2b" — MintTaskAlias mints "<CODE>-" + >=6
// lowercase hex, locally extended when taken). The alternation orders \d+
// first; an all-digit v2 hex extension therefore parses as numeric, which is
// harmless because the captured text is identical either way.
var TaskIDRe = regexp.MustCompile(`^([A-Z][A-Z0-9-]{1,15})-(\d+|[0-9a-f]{6,})$`)

func ParseTaskID(id string) (code string, n int, ok bool) {
	m := TaskIDRe.FindStringSubmatch(id)
	if m == nil {
		return "", 0, false
	}
	return m[1], numericOrZero(m[2]), true
}

// numericOrZero parses an all-digit alias segment; v2 hex segments yield 0.
// n is v1 bookkeeping (RenderTaskID round-trips, NextTaskN recovery); v2
// code paths key on the FULL alias string and must never depend on n.
func numericOrZero(seg string) int {
	v := 0
	for _, c := range seg {
		if c < '0' || c > '9' {
			return 0
		}
		v = v*10 + int(c-'0')
	}
	return v
}

func RenderTaskID(code string, n int) string {
	if n < 10000 {
		return fmt.Sprintf("%s-%04d", code, n)
	}
	return fmt.Sprintf("%s-%d", code, n)
}

func SortTaskIDs(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool {
		ci, ni, _ := ParseTaskID(ids[i])
		cj, nj, _ := ParseTaskID(ids[j])
		if ci != cj {
			return ci < cj
		}
		if ni != nj {
			return ni < nj
		}
		return ids[i] < ids[j]
	})
}

// CommentIDRe accepts v1 numeric comment ids ("ATM-0001-c0002") and v2 hash
// aliases ("ATM-7f3a2b-c9e1d" — MintCommentAlias mints "<task-alias>-c" +
// >=4 lowercase hex).
var CommentIDRe = regexp.MustCompile(`^([A-Z]{3,6})-(\d+|[0-9a-f]{6,})-c(\d+|[0-9a-f]{4,})$`)

func ParseCommentID(id string) (code string, taskN int, commentN int, ok bool) {
	m := CommentIDRe.FindStringSubmatch(id)
	if m == nil {
		return "", 0, 0, false
	}
	return m[1], numericOrZero(m[2]), numericOrZero(m[3]), true
}

// commentTaskAlias returns the task alias a comment id belongs to — the
// prefix before the "-c<suffix>" segment. Well-defined for BOTH generations
// because v1 RenderCommentID and v2 MintCommentAlias both build the comment
// id as <task-alias>-c<suffix>.
func commentTaskAlias(id string) (string, bool) {
	m := CommentIDRe.FindStringSubmatch(id)
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

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrUsage    = errors.New("usage")
)

func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
func IsConflict(err error) bool { return errors.Is(err, ErrConflict) }
func IsUsage(err error) bool    { return errors.Is(err, ErrUsage) }

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

func Open(root string) (*Store, error) {
	if root == "" {
		root = ResolveStorePath("")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Store{Root: abs}, nil
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
	return s.mutateStoreMeta(func(*StoreMeta) error { return nil })
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

// projectCodesOnDisk enumerates project codes by the projects/<CODE>/
// directory structure (which holds log.jsonl), independent of cache.db.
// Used by Verify/Rebuild so a missing or fully-wiped cache.db doesn't hide
// projects that still have logs on disk.
func (s *Store) projectCodesOnDisk() ([]string, error) {
	entries, err := os.ReadDir(s.projectsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var codes []string
	for _, e := range entries {
		if e.IsDir() {
			codes = append(codes, e.Name())
		}
	}
	sort.Strings(codes)
	return codes, nil
}
