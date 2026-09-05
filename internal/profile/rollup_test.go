package profile

import (
	"reflect"
	"testing"
)

func matrixFixture() []EndpointRow {
	return []EndpointRow{
		{Channel: "design", Type: "notion", Addressed: true, Wired: true, Agents: map[string]Attestation{
			"claude": {State: AttestFresh, Days: 1},
			"codex":  {State: AttestNone},
		}},
		{Channel: "design", Type: "slack", Addressed: true, Wired: true, Agents: map[string]Attestation{
			"claude": {State: AttestStale, Days: 30},
			"codex":  {State: AttestFresh, Days: 2},
		}},
		{Channel: "prs", Type: "slack", Addressed: true, Wired: false, Agents: map[string]Attestation{
			"claude": {State: AttestNone},
			"codex":  {State: AttestNone},
		}},
		{Channel: "notes", Type: "notion", Addressed: false, Agents: map[string]Attestation{
			"claude": {State: AttestNone},
			"codex":  {State: AttestNone},
		}},
	}
}

// A channel is only as reachable as its LEAST reachable endpoint. A rollup
// that surfaced the best one would tell an agent it can reach a channel it
// cannot.
func TestRollupByChannelTakesTheWorstAttestation(t *testing.T) {
	got := RollupByChannel(matrixFixture(), []string{"claude", "codex"})
	if len(got) != 3 {
		t.Fatalf("rollups = %d, want 3 channels", len(got))
	}
	if want := []string{"design", "notes", "prs"}; !reflect.DeepEqual([]string{got[0].Channel, got[1].Channel, got[2].Channel}, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	design := got[0]
	if design.Endpoints != 2 {
		t.Errorf("design endpoints = %d, want 2", design.Endpoints)
	}
	// claude is fresh on one endpoint and stale on the other: stale wins.
	if a := design.Agents["claude"]; a.State != AttestStale || a.Days != 30 {
		t.Errorf("design/claude = %+v, want the stale 30d answer", a)
	}
	// codex is never-attested on one: that is worse than fresh.
	if a := design.Agents["codex"]; a.State != AttestNone {
		t.Errorf("design/codex = %+v, want unverified", a)
	}
}

// The rollup says WHY an agent column is empty: an endpoint with no address
// or no wiring is stuck below attestation entirely.
func TestRollupByChannelCountsWhatBlocksAttestation(t *testing.T) {
	got := RollupByChannel(matrixFixture(), []string{"claude"})
	byName := map[string]ChannelRollup{}
	for _, r := range got {
		byName[r.Channel] = r
	}
	if r := byName["prs"]; r.Unwired != 1 || r.Unaddressed != 0 {
		t.Errorf("prs = %+v, want one unwired endpoint", r)
	}
	if r := byName["notes"]; r.Unaddressed != 1 || r.Unwired != 0 {
		t.Errorf("notes = %+v, want one unaddressed endpoint", r)
	}
}

// Two stale stamps roll up to the OLDER one — the age a reader should act on.
func TestRollupByChannelSurfacesTheOldestStaleStamp(t *testing.T) {
	m := []EndpointRow{
		{Channel: "c", Addressed: true, Wired: true, Agents: map[string]Attestation{"a": {State: AttestStale, Days: 10}}},
		{Channel: "c", Addressed: true, Wired: true, Agents: map[string]Attestation{"a": {State: AttestStale, Days: 45}}},
	}
	got := RollupByChannel(m, []string{"a"})
	if a := got[0].Agents["a"]; a.Days != 45 {
		t.Fatalf("rolled up %+v, want the 45-day stamp", a)
	}
}

// TestSummarizeMatrixExcludesUnreachableEndpointsFromAgentBlame: an agent
// cannot attest what this machine cannot reach, so counting an unwired
// endpoint as "never attested by codex" would blame the agent for a wiring
// gap the human has to fix.
func TestSummarizeMatrixExcludesUnreachableEndpointsFromAgentBlame(t *testing.T) {
	s := SummarizeMatrix(matrixFixture(), []string{"claude", "codex"})
	if s.Endpoints != 4 || s.Unaddressed != 1 || s.Unwired != 1 {
		t.Fatalf("summary = %+v, want 4 endpoints with one unaddressed and one unwired", s)
	}
	// Only the two reachable endpoints are graded per agent.
	if s.NeverAttested["codex"] != 1 {
		t.Errorf("codex never-attested = %d, want 1 (the unwired/unaddressed ones do not count)", s.NeverAttested["codex"])
	}
	if s.Stale["claude"] != 1 {
		t.Errorf("claude stale = %d, want 1", s.Stale["claude"])
	}
	if s.NeverAttested["claude"] != 0 {
		t.Errorf("claude never-attested = %d, want 0", s.NeverAttested["claude"])
	}
}

func TestRollupByChannelEmptyMatrix(t *testing.T) {
	if got := RollupByChannel(nil, []string{"claude"}); len(got) != 0 {
		t.Fatalf("rollups = %v, want none", got)
	}
}
