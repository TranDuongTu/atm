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

// AgentsConfig is the global (store-root) record of the user's host-agent
// preference: which catalog entry is selected for atm dev / atm manage, and
// per-entry default passthrough args. It lives at <root>/agents.json, distinct
// from the per-project config.json.
type AgentsConfig struct {
	UpdatedAt string              `json:"updated_at,omitempty"`
	UpdatedBy string              `json:"updated_by,omitempty"`
	Selected  string              `json:"selected,omitempty"`
	Args      map[string][]string `json:"args,omitempty"`
}

// Pins is the per-project ordered list of pinned board full names, persisted
// to <store>/projects/<CODE>/pins.json. Missing file == empty state (GetPins
// returns nil, nil), mirroring Vocabulary.
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
