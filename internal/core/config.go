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

// MaxBoardPins caps the pinned boards per project (Shift-1..3 slots).
const MaxBoardPins = 3

// BoardsConfig is the per-project boards display preference set, stored under
// config.json's "boards" key. Display preference, not substrate state: no
// event-log entry, and entries naming boards that don't exist are ignored by
// readers (defensive against typos and disabled capabilities).
type BoardsConfig struct {
	Order  []string `json:"order,omitempty"`  // ring order override (partial, FullName list)
	Hidden []string `json:"hidden,omitempty"` // hidden FullNames
	Pins   []string `json:"pins,omitempty"`   // pin-slot FullNames (max MaxBoardPins)
	// Capability is the current capability-view selection ("workflow",
	// "unmanaged", ...). Written only on an explicit switch in the TUI;
	// readers fall back silently when it names nothing enabled.
	Capability string `json:"capability,omitempty"`
}

type ProjectConfig struct {
	UpdatedAt string            `json:"updated_at,omitempty"`
	UpdatedBy string            `json:"updated_by,omitempty"`
	Embedding *EmbeddingConfig  `json:"embedding,omitempty"`
	Remotes   map[string]string `json:"remotes,omitempty"`
	Boards    *BoardsConfig     `json:"boards,omitempty"`
	// ArtOn toggles the TUI background art on or off. Display preference,
	// not substrate state: no event-log entry, and the default is off.
	ArtOn bool `json:"art_on,omitempty"`
}

// AgentsConfig is the global (store-root) record of the user's host-agent
// preference: which catalog entry is selected for the unified atm --persona
// launcher, and per-entry default passthrough args. It lives at
// <root>/agents.json, distinct from the per-project config.json.
type AgentsConfig struct {
	UpdatedAt string              `json:"updated_at,omitempty"`
	UpdatedBy string              `json:"updated_by,omitempty"`
	Selected  string              `json:"selected,omitempty"`
	Args      map[string][]string `json:"args,omitempty"`
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
