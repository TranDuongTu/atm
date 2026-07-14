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

func (s *Store) setProjectFormat(code string, f StoreFormat) error {
	m, err := s.readStoreMeta()
	if err != nil {
		return err
	}
	if m.ProjectFormats == nil {
		m.ProjectFormats = map[string]StoreFormat{}
	}
	m.ProjectFormats[code] = f
	return s.writeStoreMeta(m)
}

// removeProjectFormat deletes a project's explicit format entry. Used by
// RemoveProject (Task 8): a deleted project must not leave a stale "v2"
// entry that would make a later recreation read as v2 with no event file.
func (s *Store) removeProjectFormat(code string) error {
	m, err := s.readStoreMeta()
	if err != nil {
		return err
	}
	delete(m.ProjectFormats, code)
	return s.writeStoreMeta(m)
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
	m, err := s.readStoreMeta()
	if err != nil {
		return err
	}
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
	return s.writeStoreMeta(m)
}
