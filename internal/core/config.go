package core

import (
	"strings"
	"time"
)

type EmbeddingConfig struct {
	Model       string  `json:"model"`
	Endpoint    string  `json:"endpoint"`
	QueryPrefix string  `json:"query_prefix,omitempty"`
	DocPrefix   string  `json:"doc_prefix,omitempty"`
	Dim         int     `json:"dim"`
	Threshold   float64 `json:"threshold"`
}

// ChatConfig is the project's generation endpoint: which local chat model
// answers questions over the ledger, and where it lives. Sibling of
// EmbeddingConfig and deliberately as small — retrieval must keep working
// when this is absent, so nothing here is required in order to search
// (ATM-66a6d2).
type ChatConfig struct {
	Model    string `json:"model"`
	Endpoint string `json:"endpoint"`
}

// BoardsConfig is the per-project boards display preference set, stored under
// config.json's "boards" key. Display preference, not substrate state: no
// event-log entry, and entries naming boards that don't exist are ignored by
// readers (defensive against typos and disabled capabilities).
type BoardsConfig struct {
	// Capability is pane [2]'s current flow capability, and the whole of
	// this config now: written only on an explicit switch in the TUI, and
	// read back with a silent fallback when it names nothing enabled.
	// Pins, order and hidden are gone with the ring that read them —
	// unknown JSON keys are ignored on read, so a config still carrying
	// those keys loads unchanged.
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

// Stamp kinds. A stamp records HOW the endpoint was reached: real work
// (posting a plan, logging a PR) or a read-only check. Both attest that the
// endpoint answered; use carries more weight because it proves the endpoint
// served its purpose, not merely that it exists.
const (
	StampKindUse   = "use"
	StampKindProbe = "probe"
)

// StampKinds is the closed set, in decreasing weight.
var StampKinds = []string{StampKindUse, StampKindProbe}

// ValidStampKind reports whether k is a legal stamp kind.
func ValidStampKind(k string) bool { return k == StampKindUse || k == StampKindProbe }

// VerificationStamp is one tier-2 verification record: an actor touched the
// endpoint and vouched for its wiring at a moment in time. No secrets.
//
// The actor already names the agent harness that recorded it
// (persona@AGENT:model), so "which agents have reached this endpoint" is an
// aggregation of stamps ATM already writes — no new schema.
type VerificationStamp struct {
	At string `json:"at"`
	By string `json:"by"`
	// Kind is use or probe; empty reads as use, because before kinds
	// existed real work was the only way to write a stamp.
	Kind string `json:"kind,omitempty"`
	Note string `json:"note,omitempty"`
}

// EndpointWiring is how THIS machine reaches ONE of a channel's endpoints.
type EndpointWiring struct {
	Path      string              `json:"path,omitempty"`
	MCPServer string              `json:"mcp_server,omitempty"`
	Stamps    []VerificationStamp `json:"stamps,omitempty"`
}

// Wired reports whether this machine has anything recorded for the endpoint.
func (e EndpointWiring) Wired() bool { return e.Path != "" || e.MCPServer != "" }

// ChannelWiring is how THIS machine reaches a channel — tier 2: config, not
// substrate state, no event-log entry, not synced, and never a secret. Path
// is the local clone for repo channels; MCPServer names the agent-side MCP
// server for notion channels. A fresh machine has no wiring until a
// concierge session records it.
type ChannelWiring struct {
	// Path, MCPServer and Stamps are the PRE-ENDPOINT shape, written when a
	// channel had exactly one medium. They read as the wiring of the
	// record's FIRST endpoint; a write for that medium migrates them into
	// Endpoints and clears them, so the two can never disagree.
	Path      string              `json:"path,omitempty"`
	MCPServer string              `json:"mcp_server,omitempty"`
	Stamps    []VerificationStamp `json:"stamps,omitempty"`
	// Endpoints is per-medium wiring, keyed by endpoint type.
	Endpoints map[string]EndpointWiring `json:"endpoints,omitempty"`
}

// AgentOfActor extracts the agent harness from an actor string
// (persona@agent:model). "" when the actor is not in canonical form.
func AgentOfActor(actor string) string {
	_, rest, ok := strings.Cut(actor, "@")
	if !ok {
		return ""
	}
	agent, _, ok := strings.Cut(rest, ":")
	if !ok {
		return ""
	}
	return agent
}

// AgentStamps groups stamps by the agent harness that recorded them,
// keeping each agent's FRESHEST stamp. A tie goes to real use over a probe:
// freshness is freshness, so a probe never displaces a newer use, but where
// both land at the same instant the one proving the endpoint served its
// purpose is the better answer. Stamps whose actor names no agent are
// skipped — an unattributable stamp cannot fill a per-agent matrix.
func AgentStamps(stamps []VerificationStamp) map[string]VerificationStamp {
	out := map[string]VerificationStamp{}
	for _, st := range stamps {
		agent := AgentOfActor(st.By)
		if agent == "" {
			continue
		}
		if st.Kind == "" {
			st.Kind = StampKindUse
		}
		cur, seen := out[agent]
		switch {
		case !seen, st.At > cur.At:
			out[agent] = st
		case st.At == cur.At && st.Kind == StampKindUse:
			out[agent] = st
		}
	}
	return out
}

type ProjectConfig struct {
	UpdatedAt string           `json:"updated_at,omitempty"`
	UpdatedBy string           `json:"updated_by,omitempty"`
	Embedding *EmbeddingConfig `json:"embedding,omitempty"`
	Chat      *ChatConfig      `json:"chat,omitempty"`
	// InquiryLog toggles the auto-append of search and ask queries to
	// inquiry-log.jsonl, the ground-truth stream the eval subsystem replays
	// (ATM-028a8d). A POINTER because nil must mean "enabled": a plain bool
	// would read every project that predates this field as opted out, silently
	// starving eval of exactly the history it exists to measure.
	InquiryLog *bool                    `json:"inquiry_log,omitempty"`
	Remotes    map[string]string        `json:"remotes,omitempty"`
	Boards     *BoardsConfig            `json:"boards,omitempty"`
	Repos      []RepoConfig             `json:"repos,omitempty"`
	Channels   map[string]ChannelWiring `json:"channels,omitempty"`
	// ArtOn toggles the TUI background art on or off. Display preference,
	// not substrate state: no event-log entry, and the default is off.
	ArtOn bool `json:"art_on,omitempty"`
	// ArtPair pins the two theme names shown when ArtOn is true. Empty when
	// the project has never toggled art on (the deterministic Pair(code) is
	// used as a fallback). Set together with ArtOn by the TUI's re-roll
	// toggle; not validated at the store layer (readers fall back to
	// Pair(code) on unknown names).
	ArtPair []string `json:"art_pair,omitempty"`
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
	// Models is the chosen model per selection key, keyed exactly like Args
	// ("claude", "ollama:claude"). Absent means the harness's own default,
	// which ATM does not know.
	Models map[string]string `json:"models,omitempty"`
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
