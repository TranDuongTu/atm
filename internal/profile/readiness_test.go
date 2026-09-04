package profile

import (
	"strings"
	"testing"
	"time"

	"atm/internal/core"
)

var readyNow = time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

// readyInput is the fixture applied verbatim at 1.0.0: the planning
// checklist requires the planning channel, scrum-coding requires none.
func readyInput(t *testing.T) ReadinessInput {
	t.Helper()
	p := planFixture(t)
	cur := currentFrom(p, p.Origin().String())
	in := ReadinessInput{
		Code:    "DEMO",
		Current: cur,
		Agents:  []string{"claude"},
		Now:     readyNow,
		Profile: func(ref string) *core.Profile {
			if ref == "scrumban@1.0.0" {
				return p
			}
			return nil
		},
	}
	for _, ch := range cur.Channels {
		in.Channels = append(in.Channels, core.ChannelView{ChannelRecord: ch})
	}
	return in
}

func action(t *testing.T, r *Readiness, name string) ActionReadiness {
	t.Helper()
	for _, a := range r.Actions {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no action %s in %+v", name, r.Actions)
	return ActionReadiness{}
}

func stampAt(agent string, daysAgo int, kind string) core.VerificationStamp {
	return core.VerificationStamp{At: readyNow.AddDate(0, 0, -daysAgo).Format(time.RFC3339), By: "manager@" + agent + ":x", Kind: kind}
}

// setEndpoint gives the planning channel one addressed endpoint, wired or
// not, with the given stamps.
func setEndpoint(in *ReadinessInput, typ string, wired bool, stamps ...core.VerificationStamp) {
	for i := range in.Channels {
		if in.Channels[i].Name != "planning" {
			continue
		}
		in.Channels[i].Endpoints = []core.ChannelEndpoint{{Type: typ, Role: core.ChannelRoleHome, Address: core.ChannelAddress{Page: "p1", URL: "u"}}}
		in.Channels[i].Type = typ
		w := core.EndpointWiring{Stamps: stamps}
		if wired {
			w.MCPServer = "notion"
			if typ == core.ChannelTypeRepo {
				w.MCPServer, w.Path = "", "/repo"
			}
		}
		in.Channels[i].Wiring = &core.ChannelWiring{Endpoints: map[string]core.EndpointWiring{typ: w}}
	}
}

// An action with no channel requirements is attested for every agent as
// soon as it is applied; one with a channel expectation that has no
// endpoint stops at applied and names the endpoint command.
func TestReadinessClimbsToAppliedWithoutAnEndpoint(t *testing.T) {
	r := ComputeReadiness(readyInput(t))
	if a := action(t, r, "scrum-coding"); a.Rung["claude"] != RungAttested || len(a.Warnings["claude"]) != 0 {
		t.Fatalf("scrum-coding = %+v", a)
	}
	a := action(t, r, "planning")
	if a.Rung["claude"] != RungApplied || a.Persona != "manager" {
		t.Fatalf("planning = %+v", a)
	}
	w := a.Warnings["claude"]
	if len(w) != 1 || w[0].Rung != RungAddressed || !strings.Contains(w[0].Command, "atm channel endpoint add --project DEMO --name planning") {
		t.Fatalf("warnings = %+v", w)
	}
	if r.Ready["claude"] {
		t.Fatal("project reads ready with an unaddressed channel")
	}
}

func TestReadinessStopsAtAppliedForMissingCapabilityOrPersona(t *testing.T) {
	in := readyInput(t)
	in.Current.Enabled = []string{"scrum"}        // channel missing
	in.Current.Personas = in.Current.Personas[1:] // drop developer? keep manager: fixture order is developer, manager
	r := ComputeReadiness(in)
	a := action(t, r, "planning")
	if a.Rung["claude"] != RungValid {
		t.Fatalf("planning = %+v", a)
	}
	var texts []string
	for _, w := range a.Warnings["claude"] {
		texts = append(texts, w.Rung+": "+w.Text+" → "+w.Command)
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "capability channel") || !strings.Contains(joined, "atm project capability add --project DEMO --name channel") {
		t.Fatalf("no capability warning: %s", joined)
	}
	// scrum-coding suits developer, who is gone.
	if sc := action(t, r, "scrum-coding"); sc.Rung["claude"] != RungValid || !strings.Contains(sc.Warnings["claude"][0].Text, "persona developer") {
		t.Fatalf("scrum-coding = %+v", sc)
	}
	// A legacy project (nil enabled) has every capability.
	in.Current.Enabled = nil
	if a := action(t, ComputeReadiness(in), "planning"); a.Rung["claude"] != RungApplied {
		t.Fatalf("legacy project: %+v", a)
	}
}

func TestReadinessClimbsAddressedWiredAttested(t *testing.T) {
	in := readyInput(t)
	setEndpoint(&in, core.ChannelTypeNotion, false)
	a := action(t, ComputeReadiness(in), "planning")
	if a.Rung["claude"] != RungAddressed || !strings.Contains(a.Warnings["claude"][0].Command, "atm channel wire --project DEMO --name planning --type notion --mcp-server") {
		t.Fatalf("unwired = %+v", a)
	}

	setEndpoint(&in, core.ChannelTypeNotion, true)
	a = action(t, ComputeReadiness(in), "planning")
	if a.Rung["claude"] != RungWired || !strings.Contains(a.Warnings["claude"][0].Text, "never been reached by claude") ||
		a.Warnings["claude"][0].Command != "atm profile verify --project DEMO --agent claude" {
		t.Fatalf("unattested = %+v", a)
	}

	setEndpoint(&in, core.ChannelTypeNotion, true, stampAt("claude", 3, core.StampKindProbe))
	r := ComputeReadiness(in)
	if a := action(t, r, "planning"); a.Rung["claude"] != RungAttested || len(a.Warnings["claude"]) != 0 {
		t.Fatalf("attested = %+v", a)
	}
	if !r.Ready["claude"] {
		t.Fatal("every action attested, yet not ready")
	}
	if row := r.Matrix[0]; row.Channel != "planning" || !row.Wired || row.Wiring != "mcp:notion" || row.Agents["claude"].State != AttestFresh || row.Agents["claude"].Kind != core.StampKindProbe {
		t.Fatalf("matrix = %+v", row)
	}
}

// Attestation is per agent: one agent's stamp says nothing about another.
// Stale stamps hold the action at wired, with the age in the warning.
func TestReadinessIsAgentRelativeAndAges(t *testing.T) {
	in := readyInput(t)
	in.Agents = []string{"codex", "claude"}
	setEndpoint(&in, core.ChannelTypeNotion, true, stampAt("claude", 20, core.StampKindUse), stampAt("codex", 60, core.StampKindUse))
	r := ComputeReadiness(in)
	if r.Agents[0] != "claude" {
		t.Fatalf("agents not sorted: %v", r.Agents)
	}
	a := action(t, r, "planning")
	if a.Rung["claude"] != RungWired || !strings.Contains(a.Warnings["claude"][0].Text, "20d ago") {
		t.Fatalf("claude stale = %+v", a.Warnings["claude"])
	}
	if a.Rung["codex"] != RungWired || !strings.Contains(a.Warnings["codex"][0].Text, "stale") {
		t.Fatalf("codex expired = %+v", a.Warnings["codex"])
	}
	row := r.Matrix[0]
	if row.Agents["claude"].State != AttestStale || row.Agents["codex"].State != AttestNone || row.Agents["codex"].Days != 60 {
		t.Fatalf("matrix = %+v", row.Agents)
	}
	if r.Ready["claude"] || r.Ready["codex"] {
		t.Fatal("stale attestation reads ready")
	}
}

// A repo endpoint whose recorded path is missing on this machine is not
// wired, whatever config.json says.
func TestReadinessTrustsTheProbeOverRecordedRepoWiring(t *testing.T) {
	in := readyInput(t)
	setEndpoint(&in, core.ChannelTypeRepo, true)
	for i := range in.Channels {
		if in.Channels[i].Name == "planning" {
			in.Channels[i].Probe = &core.ChannelProbe{PathExists: false}
		}
	}
	a := action(t, ComputeReadiness(in), "planning")
	if a.Rung["claude"] != RungAddressed || !strings.Contains(a.Warnings["claude"][0].Text, "/repo does not exist") {
		t.Fatalf("missing path = %+v", a)
	}
}

// With no agent configured nothing can attest: the ladder stops at the
// machine rungs under the empty agent key.
func TestReadinessWithoutAgentsStopsAtWired(t *testing.T) {
	in := readyInput(t)
	in.Agents = nil
	setEndpoint(&in, core.ChannelTypeNotion, true)
	r := ComputeReadiness(in)
	if a := action(t, r, "planning"); a.Rung[""] != RungWired {
		t.Fatalf("planning = %+v", a)
	}
	if len(r.Ready) != 0 {
		t.Fatalf("ready = %+v", r.Ready)
	}
}

// The applied-profile list is derived from origins: sync counts, a
// missing record, a local edit, a newer installed version, and a version
// that is not available here.
func TestReadinessDerivesProfileSyncFromOrigins(t *testing.T) {
	in := readyInput(t)
	in.Available = []core.ProfileEntry{{Name: "scrumban", Version: "1.0.0"}, {Name: "scrumban", Version: "1.2.0"}}
	in.Current.Checklists[0].Purpose = "edited"
	in.Current.Channels = nil // channel record removed
	in.Current.Personas[0].Origin = "other@0.1.0"
	r := ComputeReadiness(in)
	if len(r.Profiles) != 2 || r.Profiles[0].Ref != "other@0.1.0" || r.Profiles[1].Ref != "scrumban@1.0.0" {
		t.Fatalf("profiles = %+v", r.Profiles)
	}
	other := r.Profiles[0]
	if other.Available || len(other.Records) != 1 || other.Records[0].State != "unverifiable" {
		t.Fatalf("other = %+v", other)
	}
	ps := r.Profiles[1]
	if !ps.Available || ps.Latest != "1.2.0" || ps.Modified != 1 || ps.Missing != 1 || ps.InSync != 2 {
		t.Fatalf("scrumban = %+v", ps)
	}
	// The persona now owned by other@0.1.0 is not counted under scrumban.
	for _, rec := range ps.Records {
		if rec.Kind == core.ApplyKindPersona && rec.Name == in.Current.Personas[0].Name {
			t.Fatalf("record %s counted under the wrong profile", rec.Name)
		}
	}
}
