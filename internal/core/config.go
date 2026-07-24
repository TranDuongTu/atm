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

// RepoConfig is one machine-local dispatch target recorded for a project:
// a local path to spawn agent sessions into, plus an optional remote link
// the concierge logged during onboarding. It is config, not substrate
// state — no event-log entry, not synced — so a fresh machine carrying a
// synced event log has no repos until a concierge session records them.
type RepoConfig struct {
	Name string `json:"name"`          // short handle, unique within the project
	Path string `json:"path"`          // absolute local path (existence-validated on add)
	URL  string `json:"url,omitempty"` // remote link the concierge logged; optional
}

type ProjectConfig struct {
	UpdatedAt string            `json:"updated_at,omitempty"`
	UpdatedBy string            `json:"updated_by,omitempty"`
	Embedding *EmbeddingConfig  `json:"embedding,omitempty"`
	Remotes   map[string]string `json:"remotes,omitempty"`
	Boards    *BoardsConfig     `json:"boards,omitempty"`
	Repos     []RepoConfig      `json:"repos,omitempty"`
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
