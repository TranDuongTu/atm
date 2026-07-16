package core

import "time"

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

type AgentsConfig struct {
	UpdatedAt string              `json:"updated_at,omitempty"`
	UpdatedBy string              `json:"updated_by,omitempty"`
	Selected  string              `json:"selected,omitempty"`
	Args      map[string][]string `json:"args,omitempty"`
}

type Pins struct {
	UpdatedAt time.Time `json:"updated_at"`
	Actor     string    `json:"actor"`
	Boards    []string  `json:"boards"`
}

type VocabularyTerm struct {
	Term   string `json:"term"`
	Weight int    `json:"weight"`
}

type Vocabulary struct {
	UpdatedAt time.Time        `json:"updated_at"`
	Actor     string           `json:"actor"`
	Terms     []VocabularyTerm `json:"terms"`
}
