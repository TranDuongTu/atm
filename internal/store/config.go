package store

import "os"

type EmbeddingConfig struct {
	Model       string  `json:"model"`
	Endpoint    string  `json:"endpoint"`
	QueryPrefix string  `json:"query_prefix,omitempty"`
	DocPrefix   string  `json:"doc_prefix,omitempty"`
	Dim         int     `json:"dim"`
	Threshold   float64 `json:"threshold"`
}

type ProjectConfig struct {
	UpdatedAt string           `json:"updated_at,omitempty"`
	UpdatedBy string           `json:"updated_by,omitempty"`
	Embedding *EmbeddingConfig `json:"embedding,omitempty"`
}

func (s *Store) GetProjectConfig(code string) (*ProjectConfig, error) {
	var c ProjectConfig
	if err := ReadJSON(s.configPath(code), &c); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if c.Embedding == nil && c.UpdatedAt == "" {
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
