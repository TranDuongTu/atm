// Package store is the stable in-process API for the ATM tasks store.
// It owns the on-disk format under a .atm directory and all read/write logic.
// External clients (agents) consume it via the CLI; the TUI consumes it directly.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Store is the root handle to a .atm store directory.
type Store struct {
	// Root is the absolute path to the .atm directory.
	Root string
}

// RFC3339UTC returns a canonical RFC 3339 UTC timestamp string.
func RFC3339UTC(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// Now returns the current time in UTC.
func Now() time.Time {
	return time.Now().UTC()
}

// ValidateActorID reports whether id is a well-formed actor identifier "<kind>:<id>".
var actorIDRe = regexp.MustCompile(`^(agent|human):[A-Za-z0-9._-]+$`)

// ValidateActorID returns nil if id is a valid actor identifier.
func ValidateActorID(id string) error {
	if !actorIDRe.MatchString(id) {
		return fmt.Errorf("invalid actor id %q (want 'agent:<id>' or 'human:<id>')", id)
	}
	return nil
}

// ValidateProjectCode reports whether code matches ^[A-Z][A-Z0-9-]{1,15}$.
var projectCodeRe = regexp.MustCompile(`^[A-Z][A-Z0-9-]{1,15}$`)

// ValidateProjectCode returns nil if code is a valid project code.
func ValidateProjectCode(code string) error {
	if !projectCodeRe.MatchString(code) {
		return fmt.Errorf("invalid project code %q (want ^[A-Z][A-Z0-9-]{1,15}$)", code)
	}
	return nil
}

// ValidateLabelName reports whether a label name is well-formed.
// A label is either a free-form tag or "namespace:value"; both parts must match [a-z0-9][a-z0-9-]*.
var labelRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(:[a-z0-9][a-z0-9-]*)?$`)

// ValidateLabelName returns nil if name is a valid label.
func ValidateLabelName(name string) error {
	if !labelRe.MatchString(name) {
		return fmt.Errorf("invalid label %q", name)
	}
	return nil
}

// TaskIDRe matches a task id "<CODE>-<N>".
var TaskIDRe = regexp.MustCompile(`^([A-Z][A-Z0-9-]{1,15})-(\d+)$`)

// ParseTaskID splits a task id into project code and numeric n. Returns ok=false if malformed.
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

// RenderTaskID formats a task id from a project code and numeric n.
// Numbers below 10000 are zero-padded to 4 digits for sortability; above that, natural width.
func RenderTaskID(code string, n int) string {
	if n < 10000 {
		return fmt.Sprintf("%s-%04d", code, n)
	}
	return fmt.Sprintf("%s-%d", code, n)
}

// SortTaskIDs sorts ids in place by (project code, numeric n).
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

// ErrNotFound is returned when a project/task/actor cannot be located.
var ErrNotFound = errors.New("not found")

// IsNotFound reports whether err is ErrNotFound or wraps it.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// ErrConflict is returned for atomic conflicts (already claimed, invalid transition).
var ErrConflict = errors.New("conflict")

// IsConflict reports whether err is ErrConflict or wraps it.
func IsConflict(err error) bool { return errors.Is(err, ErrConflict) }

// ErrUsage is returned for usage/validation errors (bad flags, removed label, etc.).
var ErrUsage = errors.New("usage")

// IsUsage reports whether err is ErrUsage or wraps it.
func IsUsage(err error) bool { return errors.Is(err, ErrUsage) }

// ErrAlreadyClaimed is returned when a task is claimed by another actor.
var ErrAlreadyClaimed = errors.New("already claimed")

// Open resolves the store rooted at root. If root is empty, it walks up from CWD
// looking for a .atm directory and falls back to .atm in CWD. Open does not create
// anything; use Init to materialize a fresh store.
func Open(root string) (*Store, error) {
	if root == "" {
		root = resolveRootFromCWD()
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Store{Root: abs}, nil
}

// resolveRootFromCWD walks upward from CWD looking for a .atm directory.
func resolveRootFromCWD() string {
	dir, err := os.Getwd()
	if err != nil {
		return ".atm"
	}
	for {
		candidate := filepath.Join(dir, ".atm")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ".atm"
}

// Init materializes an empty store at s.Root. Idempotent: re-running on an
// existing store is a no-op.
func (s *Store) Init() error {
	for _, sub := range []string{"projects"} {
		if err := os.MkdirAll(filepath.Join(s.Root, sub), 0o755); err != nil {
			return err
		}
	}
	return s.touchActors()
}

// touchActors ensures actors.json exists with an empty actors list.
func (s *Store) touchActors() error {
	p := filepath.Join(s.Root, "actors.json")
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	empty := actorsFile{Actors: []Actor{}}
	return WriteJSON(p, empty)
}

// projectsDir returns the path to the projects directory.
func (s *Store) projectsDir() string { return filepath.Join(s.Root, "projects") }

// projectDir returns the path to a single project's directory (where tasks live).
func (s *Store) projectDir(code string) string { return filepath.Join(s.projectsDir(), code) }

// tasksDir returns the path to a project's tasks directory.
func (s *Store) tasksDir(code string) string { return filepath.Join(s.projectDir(code), "tasks") }

// projectPath returns the path to a project's record JSON file.
func (s *Store) projectPath(code string) string { return filepath.Join(s.projectsDir(), code+".json") }

// taskPath returns the path to a single task record JSON file.
func (s *Store) taskPath(id string) string {
	code, _, ok := ParseTaskID(id)
	if !ok {
		return ""
	}
	return filepath.Join(s.tasksDir(code), id+".json")
}

// actorsPath returns the path to actors.json.
func (s *Store) actorsPath() string { return filepath.Join(s.Root, "actors.json") }

// lockPath returns the path to a project's lock file.
func (s *Store) lockPath(code string) string { return filepath.Join(s.projectsDir(), code+".lock") }

// --- helpers for sorted JSON ---

// MarshalSorted marshals v to JSON with object keys emitted in sorted order
// and stable 2-space indentation. The same input produces byte-identical output.
func MarshalSorted(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var dec any
	decUseNumber := json.NewDecoder(strings.NewReader(string(raw)))
	decUseNumber.UseNumber()
	if err := decUseNumber.Decode(&dec); err != nil {
		return nil, err
	}
	sorted := sortKeys(dec)
	return json.MarshalIndent(sorted, "", "  ")
}

// sortKeys recursively sorts map keys so json.MarshalIndent emits sorted object keys.
func sortKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = sortKeys(val)
		}
		return out
	case []any:
		for i := range t {
			t[i] = sortKeys(t[i])
		}
		return t
	default:
		return v
	}
}

// WriteJSON writes v to path as sorted JSON, atomically (temp file + rename).
func WriteJSON(path string, v any) error {
	data, err := MarshalSorted(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// ReadJSON reads JSON from path into v.
func ReadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	return dec.Decode(v)
}
