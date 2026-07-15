package store

import (
	"fmt"
	"os"
)

type EmbeddingConfig struct {
	Model       string  `json:"model"`
	Endpoint    string  `json:"endpoint"`
	QueryPrefix string  `json:"query_prefix,omitempty"`
	DocPrefix   string  `json:"doc_prefix,omitempty"`
	Dim         int     `json:"dim"`
	Threshold   float64 `json:"threshold"`
}

type ProjectConfig struct {
	UpdatedAt string            `json:"updated_at,omitempty"`
	UpdatedBy string            `json:"updated_by,omitempty"`
	Embedding *EmbeddingConfig  `json:"embedding,omitempty"`
	Remotes   map[string]string `json:"remotes,omitempty"`
}

func (s *Store) GetProjectConfig(code string) (*ProjectConfig, error) {
	var c ProjectConfig
	if err := ReadJSON(s.configPath(code), &c); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if c.Embedding == nil && c.UpdatedAt == "" && len(c.Remotes) == 0 {
		return nil, nil
	}
	return &c, nil
}

func (s *Store) SetEmbeddingConfig(code string, cfg EmbeddingConfig, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if cfg.Model == "" || cfg.Endpoint == "" {
		return ErrUsage
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
		merged.UpdatedAt = RFC3339UTC(Now())
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
		return ErrUsage
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
		merged.UpdatedAt = RFC3339UTC(Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}

// RemoveProjectRemote deletes a named sync remote from the project's config.
// Returns ErrNotFound if the name is not present.
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
			return fmt.Errorf("%w: remote %q", ErrNotFound, name)
		}
		if _, ok := existing.Remotes[name]; !ok {
			return fmt.Errorf("%w: remote %q", ErrNotFound, name)
		}
		delete(existing.Remotes, name)
		existing.UpdatedAt = RFC3339UTC(Now())
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
