package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"atm/internal/eventsource"
)

// StoreFormat identifies which on-disk event log format a project's writes
// and reads go through: v1's append-only log.jsonl, or v2's EventSource
// events.v2.jsonl (internal/eventsource).
type StoreFormat string

const (
	StoreFormatV1 StoreFormat = "v1"
	StoreFormatV2 StoreFormat = "v2"
)

// StoreMeta is the store-wide state persisted at store.json: the default
// format new projects are born into, replica/instance identity for
// eventsource authoring, the last-seen HLC, and each project's explicit
// format override.
type StoreMeta struct {
	ActiveFormat    StoreFormat            `json:"active_format,omitempty"`
	ReplicaID       string                 `json:"replica_id,omitempty"`
	StoreInstanceID string                 `json:"store_instance_id,omitempty"`
	LastHLC         *eventsource.HLC       `json:"last_hlc,omitempty"`
	ProjectFormats  map[string]StoreFormat `json:"project_formats,omitempty"`
	CreatedAt       time.Time              `json:"created_at,omitempty"`
	UpdatedAt       time.Time              `json:"updated_at,omitempty"`
}

// ProjectEventsourceMeta is the per-project state persisted at
// projects/<CODE>/eventsource.json: derived/rebuildable bookkeeping about
// that project's events.v2.jsonl (event count, file size, DAG frontier,
// last verification, and the v1 generation it was upgraded from, if any).
type ProjectEventsourceMeta struct {
	Generation     string    `json:"generation,omitempty"`
	EventCount     int       `json:"event_count"`
	FileSize       int64     `json:"file_size"`
	Frontier       []string  `json:"frontier,omitempty"`
	LastVerifiedAt time.Time `json:"last_verified_at,omitempty"`
	UpgradedFrom   string    `json:"upgraded_from,omitempty"`
}

func (s *Store) storeMetaPath() string {
	return filepath.Join(s.Root, "store.json")
}

func (s *Store) eventsV2Path(code string) string {
	return filepath.Join(s.projectDir(code), "events.v2.jsonl")
}

func (s *Store) eventsourceMetaPath(code string) string {
	return filepath.Join(s.projectDir(code), "eventsource.json")
}

func (s *Store) readStoreMeta() (*StoreMeta, error) {
	var m StoreMeta
	if err := ReadJSON(s.storeMetaPath(), &m); err != nil {
		if os.IsNotExist(err) {
			return &StoreMeta{ActiveFormat: StoreFormatV1, ProjectFormats: map[string]StoreFormat{}}, nil
		}
		return nil, err
	}
	if m.ActiveFormat == "" {
		m.ActiveFormat = StoreFormatV1
	}
	if m.ProjectFormats == nil {
		m.ProjectFormats = map[string]StoreFormat{}
	}
	return &m, nil
}

func (s *Store) writeStoreMeta(m *StoreMeta) error {
	if m.ProjectFormats == nil {
		m.ProjectFormats = map[string]StoreFormat{}
	}
	now := Now()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	return WriteJSON(s.storeMetaPath(), m)
}

// storeMetaLockName names the store-SCOPED lock that serializes every
// read-modify-write of store.json. StoreMeta is store-wide (ProjectFormats,
// ReplicaID, LastHLC) but every writer holds at most a PER-PROJECT lock, so
// two projects racing on store.json lose each other's updates: a dropped
// ProjectFormats entry makes a v2-media project resolve as v1 through the
// ActiveFormat fallback and sends its next write to log.jsonl. v2 authoring
// makes store.json an RMW on every event append, so the window is live, not
// admin-only. The name cannot collide with a project code (^[A-Z]{3,6}$) —
// same trick as WithLock("personas").
//
// LOCK ORDER: project lock -> store-meta lock. WithLock is NOT reentrant, so
// never take this lock twice, and never take a project lock while holding it.
const storeMetaLockName = "store-meta"

// mutateStoreMeta runs a read-modify-write of store.json under the
// store-scoped lock. It is the ONLY path that may write store.json.
func (s *Store) mutateStoreMeta(fn func(m *StoreMeta) error) error {
	return s.WithLock(storeMetaLockName, func() error {
		m, err := s.readStoreMeta()
		if err != nil {
			return err
		}
		if err := fn(m); err != nil {
			return err
		}
		return s.writeStoreMeta(m)
	})
}

// projectFormat resolves the effective StoreFormat for code: ProjectFormats[code]
// if present, else ActiveFormat, else v1. The rest of this plan maintains the
// invariant that every v2-media project carries an explicit ProjectFormats
// entry (written at cutover or at v2 birth), so an entry-less project is
// always legacy v1 media -- the ActiveFormat fallback is only ever
// load-bearing for such projects. Never infer v2 from ActiveFormat alone for
// a project that might already have v1 media on disk; that is exactly the
// corruption SetActiveFormat(StoreFormatV2) guards against.
func (s *Store) projectFormat(code string) (StoreFormat, error) {
	m, err := s.readStoreMeta()
	if err != nil {
		return "", err
	}
	if f, ok := m.ProjectFormats[code]; ok && f != "" {
		return f, nil
	}
	if m.ActiveFormat != "" {
		return m.ActiveFormat, nil
	}
	return StoreFormatV1, nil
}

// ProjectFormatForCLI is the exported read of a project's effective format, for
// CLI display and branching (`store log` picks its v1 or v2 renderer with it).
// projectFormat itself stays unexported: internal/store remains the
// compatibility API, and the format is an implementation detail everywhere else.
func (s *Store) ProjectFormatForCLI(code string) (StoreFormat, error) {
	return s.projectFormat(code)
}

// testHookAfterDispatchFormat, when non-nil, runs inside dispatchFormat AFTER
// the pre-lock format read and BEFORE the caller takes the project lock. It is
// the seam that lets a test flip a project's format deterministically inside
// the TOCTOU window instead of racing goroutines at it. Production leaves it
// nil; only tests in this package assign it.
var testHookAfterDispatchFormat func(code string)

// dispatchFormat is the PRE-LOCK format read every live mutator makes to pick
// its v1 or v2 body. It is ADVISORY ONLY: `atm` is multi-process (WithLock is a
// cross-process flock precisely because concurrent processes are expected), so
// another process may cut the project over (upgrade) or back (rollback) between
// this read and this mutator acquiring the project lock. The body selected here
// is therefore re-checked under the lock by withProjectFormatLock, which is the
// authoritative gate.
func (s *Store) dispatchFormat(code string) (StoreFormat, error) {
	f, err := s.projectFormat(code)
	if err != nil {
		return "", err
	}
	if testHookAfterDispatchFormat != nil {
		testHookAfterDispatchFormat(code)
	}
	return f, nil
}

// withProjectFormatLock takes the project lock and re-checks, UNDER it, that the
// project's effective format is still `want` — the format whose body the caller
// selected from its pre-lock dispatchFormat read. Without this re-check, an
// upgrade landing in that window makes the v1 body append to log.jsonl on a
// now-v2-active project (violating the byte-identical-v1-log constraint AND
// losing the write, which never reaches events.v2.jsonl); a rollback landing
// there makes the v2 body append to events.v2.jsonl on a now-v1 project. Both
// are silent corruption. ErrConflict — which the caller may simply retry — is
// not. UpgradeProjectToV2 has carried exactly this re-check since Task 4; every
// live mutator now does too.
//
// LOCK ORDER: unchanged. This adds no new lock — projectFormat just reads
// store.json (it takes no store-meta lock; only the store.json read-MODIFY-write
// in mutateStoreMeta does) — so the only lock held here is the project's, and
// the project -> store-meta order the v2 authoring path relies on is untouched.
func (s *Store) withProjectFormatLock(code string, want StoreFormat, fn func() error) error {
	return s.WithLock(code, func() error {
		f, err := s.projectFormat(code)
		if err != nil {
			return err
		}
		if f != want {
			return fmt.Errorf("%w: project %q changed format (%s -> %s) while this write waited for the project lock; retry", ErrConflict, code, want, f)
		}
		return fn()
	})
}

func (s *Store) setProjectFormat(code string, f StoreFormat) error {
	return s.mutateStoreMeta(func(m *StoreMeta) error {
		if m.ProjectFormats == nil {
			m.ProjectFormats = map[string]StoreFormat{}
		}
		m.ProjectFormats[code] = f
		return nil
	})
}

// removeProjectFormat deletes a project's explicit format entry. Used by
// RemoveProject (Task 8): a deleted project must not leave a stale "v2"
// entry that would make a later recreation read as v2 with no event file.
func (s *Store) removeProjectFormat(code string) error {
	return s.mutateStoreMeta(func(m *StoreMeta) error {
		delete(m.ProjectFormats, code)
		return nil
	})
}

// SetActiveFormat sets the store default format, which governs only project
// CREATION (birth format) and the read default for legacy projects with no
// explicit ProjectFormats entry. Setting v2 is refused while any on-disk
// project lacks an explicit entry: entry-less projects are v1 media by
// construction, and flipping the default would read them as v2 with no
// event file. Setting v1 is always safe for the same reason.
func (s *Store) SetActiveFormat(f StoreFormat) error {
	if f != StoreFormatV1 && f != StoreFormatV2 {
		return fmt.Errorf("%w: unknown store format %q", ErrUsage, f)
	}
	return s.mutateStoreMeta(func(m *StoreMeta) error {
		if f == StoreFormatV2 {
			codes, err := s.projectCodesOnDisk()
			if err != nil {
				return err
			}
			for _, code := range codes {
				if _, ok := m.ProjectFormats[code]; !ok {
					return fmt.Errorf("%w: project %q has no explicit format entry; run 'atm store upgrade --all' before setting the active format to v2", ErrConflict, code)
				}
			}
		}
		m.ActiveFormat = f
		return nil
	})
}
