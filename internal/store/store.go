package store

import (
	"database/sql"
	"errors"
	"fmt"
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

var labelRe = regexp.MustCompile(`^[A-Z]{3,6}(:[a-z0-9][a-z0-9-]*){1,2}$`)

func ValidateLabelName(name string) error {
	if !labelRe.MatchString(name) {
		return fmt.Errorf("invalid label %q (want ^[A-Z]{3,6}(:[a-z0-9][a-z0-9-]*){1,2}$)", name)
	}
	return nil
}

var TaskIDRe = regexp.MustCompile(`^([A-Z][A-Z0-9-]{1,15})-(\d+)$`)

func ParseTaskID(id string) (code string, n int, ok bool) {
	m := TaskIDRe.FindStringSubmatch(id)
	if m == nil {
		return "", 0, false
	}
	var v int
	for _, c := range m[2] {
		v = v*10 + int(c-'0')
	}
	return m[1], v, true
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
		return ni < nj
	})
}

var CommentIDRe = regexp.MustCompile(`^([A-Z]{3,6})-(\d+)-c(\d+)$`)

func ParseCommentID(id string) (code string, taskN int, commentN int, ok bool) {
	m := CommentIDRe.FindStringSubmatch(id)
	if m == nil {
		return "", 0, 0, false
	}
	var t, c int
	for _, r := range m[2] {
		t = t*10 + int(r-'0')
	}
	for _, r := range m[3] {
		c = c*10 + int(r-'0')
	}
	return m[1], t, c, true
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
	_, err := s.cacheDB()
	return err
}

func (s *Store) StorePath() string { return s.Root }

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
