package core

import (
	"reflect"
	"testing"
	"time"
)

func viewWith(eps []ChannelEndpoint, w *ChannelWiring) ChannelView {
	return ChannelView{ChannelRecord: ChannelRecord{Name: "design", Endpoints: eps}, Wiring: w}
}

// Wiring recorded before endpoints existed belongs to the channel's FIRST
// endpoint. Any other medium is simply unwired, not accidentally sharing
// someone else's path.
func TestEndpointWiringReadCompat(t *testing.T) {
	v := viewWith(
		[]ChannelEndpoint{{Type: ChannelTypeNotion, Role: ChannelRoleHome}, {Type: ChannelTypeSlack, Role: ChannelRoleBroadcast}},
		&ChannelWiring{MCPServer: "notion", Stamps: []VerificationStamp{{At: "2026-01-01T00:00:00Z", By: "developer@claude:x"}}},
	)
	first := v.EndpointWiring(ChannelTypeNotion)
	if first.MCPServer != "notion" || len(first.Stamps) != 1 {
		t.Fatalf("first endpoint = %+v, want the pre-endpoint wiring", first)
	}
	if got := v.EndpointWiring(ChannelTypeSlack); got.MCPServer != "" || len(got.Stamps) != 0 {
		t.Fatalf("second endpoint = %+v, want unwired", got)
	}
}

// Per-endpoint wiring wins over the pre-endpoint slot for its own medium.
func TestEndpointWiringPrefersThePerEndpointRecord(t *testing.T) {
	v := viewWith(
		[]ChannelEndpoint{{Type: ChannelTypeNotion, Role: ChannelRoleHome}},
		&ChannelWiring{
			MCPServer: "stale",
			Endpoints: map[string]EndpointWiring{ChannelTypeNotion: {MCPServer: "notion-mcp"}},
		},
	)
	if got := v.EndpointWiring(ChannelTypeNotion).MCPServer; got != "notion-mcp" {
		t.Fatalf("mcp server = %q, want the per-endpoint value", got)
	}
}

func TestEndpointWiringNilIsUnwired(t *testing.T) {
	v := viewWith([]ChannelEndpoint{{Type: ChannelTypeRepo, Role: ChannelRoleHome}}, nil)
	if got := v.EndpointWiring(ChannelTypeRepo); got.Path != "" || len(got.Stamps) != 0 {
		t.Fatalf("unwired channel = %+v", got)
	}
}

// A stamp records WHICH AGENT HARNESS touched the endpoint. The actor
// already carries it, so the matrix is an aggregation of stamps we already
// write — no new schema.
func TestAgentOfActor(t *testing.T) {
	for _, tc := range []struct{ actor, want string }{
		{"developer@claude:opus-5", "claude"},
		{"manager@ollama:glm-5.3", "ollama"},
		{"admin@cli:unset", "cli"},
		{"malformed", ""},
		{"", ""},
	} {
		if got := AgentOfActor(tc.actor); got != tc.want {
			t.Fatalf("AgentOfActor(%q) = %q, want %q", tc.actor, got, tc.want)
		}
	}
}

// Each agent's attestation is its freshest stamp. Real use outranks a probe
// only when they are equally fresh: freshness is freshness, and claiming
// otherwise would report an old success as newer than a recent check.
func TestAgentStampsKeepsTheFreshestPerAgent(t *testing.T) {
	stamps := []VerificationStamp{
		{At: "2026-01-01T00:00:00Z", By: "developer@claude:x", Kind: StampKindUse},
		{At: "2026-03-01T00:00:00Z", By: "developer@claude:x", Kind: StampKindProbe},
		{At: "2026-02-01T00:00:00Z", By: "manager@ollama:y", Kind: StampKindUse},
		{At: "2026-03-01T00:00:00Z", By: "reviewer@claude:z", Kind: StampKindUse},
	}
	got := AgentStamps(stamps)
	if len(got) != 2 {
		t.Fatalf("agents = %v, want claude and ollama", got)
	}
	// Same instant, so the real use outranks the probe.
	if got["claude"].Kind != StampKindUse {
		t.Fatalf("claude = %+v, want the use stamp to win a tie", got["claude"])
	}
	if got["ollama"].At != "2026-02-01T00:00:00Z" {
		t.Fatalf("ollama = %+v", got["ollama"])
	}
}

// A stamp written before kinds existed was recorded after real use — that
// was the only way to write one.
func TestStampKindDefaultsToUse(t *testing.T) {
	got := AgentStamps([]VerificationStamp{{At: "2026-01-01T00:00:00Z", By: "developer@claude:x"}})
	if got["claude"].Kind != StampKindUse {
		t.Fatalf("kind = %q, want %q for a stamp written before kinds existed", got["claude"].Kind, StampKindUse)
	}
}

func TestAgentStampsIgnoresUnattributableStamps(t *testing.T) {
	if got := AgentStamps([]VerificationStamp{{At: "2026-01-01T00:00:00Z", By: "nonsense"}}); len(got) != 0 {
		t.Fatalf("got %v, want nothing attributable", got)
	}
}

// A channel is wired when ANY of its endpoints is: content still flows even
// if one medium is not set up here. The status rolls up rather than
// reporting only the first endpoint.
func TestChannelStatusRollsUpAcrossEndpoints(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	v := viewWith(
		[]ChannelEndpoint{{Type: ChannelTypeNotion, Role: ChannelRoleHome}, {Type: ChannelTypeSlack, Role: ChannelRoleBroadcast}},
		&ChannelWiring{Endpoints: map[string]EndpointWiring{
			ChannelTypeSlack: {MCPServer: "slack", Stamps: []VerificationStamp{{At: "2026-02-25T00:00:00Z", By: "developer@claude:x"}}},
		}},
	)
	glyph, note := ChannelStatus(v, now)
	if glyph == "○" {
		t.Fatalf("status = %q/%q, want a wired reading from the slack endpoint", glyph, note)
	}

	bare := viewWith([]ChannelEndpoint{{Type: ChannelTypeNotion, Role: ChannelRoleHome}}, nil)
	if glyph, note := ChannelStatus(bare, now); glyph != "○" || note != "unwired" {
		t.Fatalf("unwired channel = %q/%q", glyph, note)
	}
}

func TestValidStampKind(t *testing.T) {
	if !ValidStampKind(StampKindUse) || !ValidStampKind(StampKindProbe) || ValidStampKind("guess") {
		t.Fatal("stamp kinds are exactly use and probe")
	}
	if !reflect.DeepEqual(StampKinds, []string{StampKindUse, StampKindProbe}) {
		t.Fatalf("StampKinds = %v", StampKinds)
	}
}
