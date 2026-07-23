package store

import (
	"atm/internal/core"
	"fmt"
	"os"
	"path/filepath"
)

func (s *Store) GetProjectConfig(code string) (*ProjectConfig, error) {
	var c ProjectConfig
	if err := ReadJSON(s.configPath(code), &c); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if c.Embedding == nil && c.UpdatedAt == "" && len(c.Remotes) == 0 && c.Boards == nil && c.ArtTheme == "" {
		return nil, nil
	}
	return &c, nil
}

func (s *Store) SetEmbeddingConfig(code string, cfg EmbeddingConfig, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if cfg.Model == "" || cfg.Endpoint == "" {
		return core.ErrUsage
	}
	return s.WithLock(code, func() error {
		existing, err := s.GetProjectConfig(code)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		merged := &ProjectConfig{}
		if existing != nil {
			merged = existing
		}
		merged.Embedding = &cfg
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}

// SetProjectRemote adds or updates a named sync remote in the project's
// config. name and url are both required.
func (s *Store) SetProjectRemote(code, name, url, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if name == "" || url == "" {
		return core.ErrUsage
	}
	return s.WithLock(code, func() error {
		existing, err := s.GetProjectConfig(code)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		merged := &ProjectConfig{}
		if existing != nil {
			merged = existing
		}
		if merged.Remotes == nil {
			merged.Remotes = map[string]string{}
		}
		merged.Remotes[name] = url
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}

// RemoveProjectRemote deletes a named sync remote from the project's config.
// Returns core.ErrNotFound if the name is not present.
func (s *Store) RemoveProjectRemote(code, name, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		existing, err := s.GetProjectConfig(code)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if existing == nil || existing.Remotes == nil {
			return fmt.Errorf("%w: remote %q", core.ErrNotFound, name)
		}
		if _, ok := existing.Remotes[name]; !ok {
			return fmt.Errorf("%w: remote %q", core.ErrNotFound, name)
		}
		delete(existing.Remotes, name)
		existing.UpdatedAt = core.RFC3339UTC(core.Now())
		existing.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), existing)
	})
}

// ProjectRemotes returns the project's configured sync remotes. It returns a
// nil map and no error if the project has no config or no remotes set.
func (s *Store) ProjectRemotes(code string) (map[string]string, error) {
	c, err := s.GetProjectConfig(code)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, nil
	}
	return c.Remotes, nil
}

// legacyPinBoards reads the board list from a pre-boards pins.json, kept
// only for the lazy migration into config.json.boards.pins. Missing or
// malformed reads as nil: display preferences are not worth failing over.
func (s *Store) legacyPinBoards(code string) []string {
	var p struct {
		Boards []string `json:"boards"`
	}
	if err := ReadJSON(filepath.Join(s.projectDir(code), "pins.json"), &p); err != nil {
		return nil
	}
	return p.Boards
}

// GetBoardsConfig returns the project's boards display preferences, never nil
// on success. While config.json carries no boards key, a legacy pins.json is
// folded into Pins — the read half of the pins.json migration. The merged
// value is persisted by the first SetProjectBoards write, after which
// config.json.boards is non-nil and pins.json is ignored forever. A malformed
// pins.json is treated as absent: display preferences are not worth failing a
// read over.
func (s *Store) GetBoardsConfig(code string) (*core.BoardsConfig, error) {
	c, err := s.GetProjectConfig(code)
	if err != nil {
		return nil, err
	}
	if c != nil && c.Boards != nil {
		return c.Boards, nil
	}
	b := &core.BoardsConfig{}
	if pins := s.legacyPinBoards(code); len(pins) > 0 {
		b.Pins = pins
		if len(b.Pins) > core.MaxBoardPins {
			b.Pins = b.Pins[:core.MaxBoardPins]
		}
	}
	return b, nil
}

// SetProjectBoards writes the project's boards display preferences under the
// project lock, read-modify-write like SetEmbeddingConfig, refreshing the
// updated_at/updated_by stamps. Enforces the MaxBoardPins cap. No store
// event: display preferences are config, not substrate state.
func (s *Store) SetProjectBoards(code string, b *core.BoardsConfig, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if b == nil || len(b.Pins) > core.MaxBoardPins {
		return core.ErrUsage
	}
	return s.WithLock(code, func() error {
		existing, err := s.GetProjectConfig(code)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		merged := &ProjectConfig{}
		if existing != nil {
			merged = existing
		}
		merged.Boards = b
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}

// SetProjectArtTheme writes the project's TUI art-theme pin under the
// project lock, read-modify-write like SetProjectBoards. An empty theme
// clears the pin (auto-assignment applies). Theme names are not validated
// here — readers fall back to auto-assignment on unknown names, the same
// defensive posture as BoardsConfig entries. No store event: display
// preference, not substrate state.
func (s *Store) SetProjectArtTheme(code, theme, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		existing, err := s.GetProjectConfig(code)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		merged := &ProjectConfig{}
		if existing != nil {
			merged = existing
		}
		merged.ArtTheme = theme
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}
