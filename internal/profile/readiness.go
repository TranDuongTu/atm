package profile

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"atm/internal/core"
)

// The readiness ladder (plan §3.10). Readiness is not a property of the
// profile artifact: it is a property of (profile × project × machine ×
// agent), and each rung is answered by a different party.
const (
	// RungValid: the profile is self-consistent — answered by build.
	RungValid = "valid"
	// RungApplied: the records live in this project and the capabilities
	// they need are enabled — answered by apply.
	RungApplied = "applied"
	// RungAddressed: every channel the action needs has an endpoint with
	// a real address — a project fact (channel endpoint add).
	RungAddressed = "addressed"
	// RungWired: this machine can reach each of those endpoints — a
	// machine fact (channel wire).
	RungWired = "wired"
	// RungAttested: THIS agent has actually touched each endpoint recently
	// — stamps, per agent, aging.
	RungAttested = "attested"
)

// Rungs lists the ladder bottom to top.
var Rungs = []string{RungValid, RungApplied, RungAddressed, RungWired, RungAttested}

// RungIndex orders rungs; -1 for an unknown name.
func RungIndex(r string) int { return slices.Index(Rungs, r) }

// Attestation states, per (endpoint × agent).
const (
	AttestFresh = "attested"
	AttestStale = "stale"
	AttestNone  = "unverified"
)

// ReadinessInput is everything the computation reads. It is plain data so
// every surface — `profile status`, the dispatch dialog, the TUI overlays —
// feeds the same function and cannot disagree.
type ReadinessInput struct {
	Code    string
	Current Current
	// Channels are the project's channels joined with THIS machine's
	// wiring and probes (the store's ProjectChannels read).
	Channels []core.ChannelView
	// Agents are the harnesses configured on this machine (agents.json),
	// never every conceivable one. Empty means nothing can be attested.
	Agents []string
	// Available lists the profiles installed or embedded here.
	Available []core.ProfileEntry
	// Profile loads one applied profile version, substituted for the
	// project; nil when it is not available here. May be nil.
	Profile func(ref string) *core.Profile
	Now     time.Time
}

// Readiness is the computed answer.
type Readiness struct {
	Code string `json:"project"`
	// Profiles is the sync state of every profile the project's records
	// came from — DERIVED from record origins, so it cannot drift.
	Profiles []ProfileSync `json:"profiles"`
	Agents   []string      `json:"agents"`
	// Matrix is endpoint × agent attestation.
	Matrix []EndpointRow `json:"matrix"`
	// Actions is the agent-relative readiness of every checklist.
	Actions []ActionReadiness `json:"actions"`
	// Ready, per agent: every action reaches attested with no warning.
	Ready map[string]bool `json:"ready"`
}

// ProfileSync is one applied profile's state against its records.
type ProfileSync struct {
	Ref       string       `json:"ref"`
	Available bool         `json:"available"`
	Latest    string       `json:"latest,omitempty"`
	Records   []RecordSync `json:"records"`
	InSync    int          `json:"in_sync"`
	Modified  int          `json:"modified"`
	Missing   int          `json:"missing"`
}

// RecordSync is one document's state: in-sync | modified | missing |
// unverifiable (the version is not installed here).
type RecordSync struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// EndpointRow is one endpoint's wiring on this machine and attestation per
// configured agent.
type EndpointRow struct {
	Channel   string                 `json:"channel"`
	Type      string                 `json:"type"`
	Role      string                 `json:"role"`
	Addressed bool                   `json:"addressed"`
	Wired     bool                   `json:"wired"`
	Wiring    string                 `json:"wiring,omitempty"`
	Agents    map[string]Attestation `json:"agents"`
}

// Attestation is one agent's freshest stamp on one endpoint.
type Attestation struct {
	State string `json:"state"`
	Kind  string `json:"kind,omitempty"`
	At    string `json:"at,omitempty"`
	Days  int    `json:"days,omitempty"`
}

// ActionReadiness is one checklist's rung per agent, with the warnings
// that hold it there and the command that answers each.
type ActionReadiness struct {
	Name     string   `json:"name"`
	Persona  string   `json:"persona,omitempty"`
	Channels []string `json:"channels,omitempty"`
	// Rung is the highest rung reached, per agent. Machine-level rungs are
	// the same for every agent; only attested differs.
	Rung map[string]string `json:"rung"`
	// Warnings, per agent, bottom rung first.
	Warnings map[string][]Warning `json:"warnings"`
}

// Warning names the rung it blocks, what is wrong, and what fixes it.
type Warning struct {
	Rung    string `json:"rung"`
	Text    string `json:"text"`
	Command string `json:"command,omitempty"`
}

// ComputeReadiness is the ONE readiness computation.
func ComputeReadiness(in ReadinessInput) *Readiness {
	agents := append([]string(nil), in.Agents...)
	sort.Strings(agents)
	out := &Readiness{Code: in.Code, Agents: agents, Ready: map[string]bool{}}
	out.Profiles = profileSyncs(in)
	out.Matrix = attestationMatrix(in, agents)
	for _, a := range agents {
		out.Ready[a] = true
	}
	for _, cl := range in.Current.Checklists {
		ar := actionReadiness(in, cl, agents, out.Matrix)
		out.Actions = append(out.Actions, ar)
		for _, a := range agents {
			if ar.Rung[a] != RungAttested {
				out.Ready[a] = false
			}
		}
	}
	return out
}

// profileSyncs folds record origins into the applied-profile list.
func profileSyncs(in ReadinessInput) []ProfileSync {
	refs := map[string]bool{}
	type rec struct{ kind, name, origin string }
	var recs []rec
	for _, x := range in.Current.Personas {
		recs = append(recs, rec{core.ApplyKindPersona, x.Name, x.Origin})
	}
	for _, x := range in.Current.Checklists {
		recs = append(recs, rec{core.ApplyKindChecklist, x.Name, x.Origin})
	}
	for _, x := range in.Current.Channels {
		recs = append(recs, rec{core.ApplyKindChannel, x.Name, x.Origin})
	}
	for _, r := range recs {
		if o, err := core.ParseOrigin(r.origin); err == nil && o.Kind == core.OriginProfile {
			refs[o.Ref()] = true
		}
	}
	names := make([]string, 0, len(refs))
	for ref := range refs {
		names = append(names, ref)
	}
	sort.Strings(names)
	var out []ProfileSync
	for _, ref := range names {
		ps := ProfileSync{Ref: ref}
		o, _ := core.ParseOrigin(ref)
		for _, e := range in.Available {
			if e.Name == o.Profile && (ps.Latest == "" || core.CompareProfileVersions(e.Version, ps.Latest) > 0) {
				ps.Latest = e.Version
			}
		}
		if ps.Latest == o.Version {
			ps.Latest = ""
		}
		var p *core.Profile
		if in.Profile != nil {
			p = in.Profile(ref)
		}
		if p == nil {
			for _, r := range recs {
				if r.origin == ref {
					ps.Records = append(ps.Records, RecordSync{Kind: r.kind, Name: r.name, State: "unverifiable"})
				}
			}
			out = append(out, ps)
			continue
		}
		ps.Available = true
		add := func(kind, name string, exists bool, diff []string) {
			rs := RecordSync{Kind: kind, Name: name}
			switch {
			case !exists:
				rs.State = "missing"
				ps.Missing++
			case len(diff) > 0:
				rs.State = "modified"
				ps.Modified++
			default:
				rs.State = "in-sync"
				ps.InSync++
			}
			ps.Records = append(ps.Records, rs)
		}
		for _, doc := range p.Personas {
			x, ok := find(in.Current.Personas, doc.Name, func(x core.Persona) string { return x.Name })
			if ok && x.Origin != ref {
				continue // counted under the version it actually came from
			}
			add(core.ApplyKindPersona, doc.Name, ok, personaDiff(x, doc))
		}
		for _, doc := range p.Checklists {
			x, ok := find(in.Current.Checklists, doc.Name, func(x core.ChecklistRecord) string { return x.Name })
			if ok && x.Origin != ref {
				continue
			}
			add(core.ApplyKindChecklist, doc.Name, ok, checklistDiff(x, doc))
		}
		for _, doc := range p.Channels {
			x, ok := find(in.Current.Channels, doc.Name, func(x core.ChannelRecord) string { return x.Name })
			if ok && x.Origin != ref {
				continue
			}
			add(core.ApplyKindChannel, doc.Name, ok, channelDiff(x, doc))
		}
		out = append(out, ps)
	}
	return out
}

// attestationMatrix is endpoint × agent, in channel then endpoint order.
func attestationMatrix(in ReadinessInput, agents []string) []EndpointRow {
	var out []EndpointRow
	for _, v := range in.Channels {
		for _, ep := range v.Endpoints {
			w := v.EndpointWiring(ep.Type)
			row := EndpointRow{
				Channel:   v.Name,
				Type:      ep.Type,
				Role:      ep.Role,
				Addressed: addressed(ep),
				Wired:     endpointWired(v, ep, w),
				Agents:    map[string]Attestation{},
			}
			switch {
			case w.Path != "":
				row.Wiring = w.Path
			case w.MCPServer != "":
				row.Wiring = "mcp:" + w.MCPServer
			}
			byAgent := core.AgentStamps(w.Stamps)
			for _, a := range agents {
				row.Agents[a] = attestationOf(byAgent[a], in.Now)
			}
			out = append(out, row)
		}
	}
	return out
}

func addressed(ep core.ChannelEndpoint) bool {
	a := ep.Address
	return a.URL != "" || a.Workspace != "" || a.Database != "" || a.Page != "" || a.ChannelID != ""
}

// endpointWired: recorded wiring, and for a repo endpoint with a probe, a
// path that actually exists. A probe that could not answer is not a no.
func endpointWired(v core.ChannelView, ep core.ChannelEndpoint, w core.EndpointWiring) bool {
	if !w.Wired() {
		return false
	}
	if ep.Type == core.ChannelTypeRepo && v.Probe != nil && !v.Probe.PathExists {
		return false
	}
	return true
}

func attestationOf(st core.VerificationStamp, now time.Time) Attestation {
	if st.At == "" {
		return Attestation{State: AttestNone}
	}
	days, ok := core.StampAgeDays(st, now)
	if !ok {
		return Attestation{State: AttestNone, Kind: st.Kind, At: st.At}
	}
	a := Attestation{Kind: st.Kind, At: st.At, Days: days}
	switch {
	case days <= core.StampFreshDays:
		a.State = AttestFresh
	case days <= core.StampStaleDays:
		a.State = AttestStale
	default:
		a.State = AttestNone
	}
	return a
}

// actionReadiness climbs the ladder for one checklist. Rungs below
// attested are machine-level and shared by every agent; attested is where
// the agents part ways.
func actionReadiness(in ReadinessInput, cl core.ChecklistRecord, agents []string, matrix []EndpointRow) ActionReadiness {
	ar := ActionReadiness{Name: cl.Name, Channels: cl.Requires.Channels, Rung: map[string]string{}, Warnings: map[string][]Warning{}}
	if len(cl.Suits) > 0 {
		ar.Persona = cl.Suits[0]
	}
	types := strings.Join(core.ChannelTypes, "|")
	var shared []Warning
	// applied: capabilities enabled, persona present.
	for _, c := range cl.Requires.Capabilities {
		if in.Current.Enabled != nil && !slices.Contains(in.Current.Enabled, c) {
			shared = append(shared, Warning{Rung: RungApplied,
				Text:    fmt.Sprintf("requires capability %s, which is not enabled", c),
				Command: fmt.Sprintf("atm project capability add --project %s --name %s", in.Code, c)})
		}
	}
	if ar.Persona != "" {
		if _, ok := find(in.Current.Personas, ar.Persona, func(x core.Persona) string { return x.Name }); !ok {
			shared = append(shared, Warning{Rung: RungApplied,
				Text:    fmt.Sprintf("suits persona %s, which is not a project record", ar.Persona),
				Command: fmt.Sprintf("atm profile apply --project %s --name <profile>", in.Code)})
		}
	}
	// addressed and wired, per required channel.
	var endpoints []EndpointRow
	for _, h := range cl.Requires.Channels {
		i := slices.IndexFunc(in.Channels, func(v core.ChannelView) bool { return v.Name == h })
		if i < 0 {
			shared = append(shared, Warning{Rung: RungAddressed,
				Text:    fmt.Sprintf("requires channel %s, which does not exist", h),
				Command: fmt.Sprintf("atm channel add --project %s --name %s --type <%s> ...", in.Code, h, types)})
			continue
		}
		v := in.Channels[i]
		var rows []EndpointRow
		for _, row := range matrix {
			if row.Channel == h {
				rows = append(rows, row)
			}
		}
		if len(rows) == 0 {
			shared = append(shared, Warning{Rung: RungAddressed,
				Text:    fmt.Sprintf("channel %s has no endpoint", h),
				Command: fmt.Sprintf("atm channel endpoint add --project %s --name %s --type <%s> ...", in.Code, h, types)})
			continue
		}
		for _, row := range rows {
			if !row.Addressed {
				shared = append(shared, Warning{Rung: RungAddressed,
					Text:    fmt.Sprintf("channel %s: %s endpoint has no address", h, row.Type),
					Command: fmt.Sprintf("atm channel endpoint add --project %s --name %s --type %s ...", in.Code, h, row.Type)})
				continue
			}
			if !row.Wired {
				how := "--mcp-server <name>"
				if row.Type == core.ChannelTypeRepo {
					how = "--path <dir>"
				}
				text := fmt.Sprintf("channel %s: %s endpoint is not wired on this machine", h, row.Type)
				if row.Wiring != "" {
					text = fmt.Sprintf("channel %s: %s endpoint path %s does not exist on this machine", h, row.Type, row.Wiring)
				}
				shared = append(shared, Warning{Rung: RungWired, Text: text,
					Command: fmt.Sprintf("atm channel wire --project %s --name %s --type %s %s", in.Code, h, row.Type, how)})
				continue
			}
			endpoints = append(endpoints, row)
		}
		_ = v
	}
	sort.SliceStable(shared, func(i, j int) bool { return RungIndex(shared[i].Rung) < RungIndex(shared[j].Rung) })
	sharedRung := RungAttested
	if len(shared) > 0 {
		// The rung reached is one below the lowest rung a warning blocks.
		sharedRung = Rungs[RungIndex(shared[0].Rung)-1]
	}
	for _, a := range agents {
		ws := append([]Warning(nil), shared...)
		rung := sharedRung
		if sharedRung == RungAttested {
			for _, row := range endpoints {
				at := row.Agents[a]
				if at.State == AttestFresh {
					continue
				}
				text := fmt.Sprintf("channel %s: %s endpoint has never been reached by %s", row.Channel, row.Type, a)
				if at.State == AttestStale {
					text = fmt.Sprintf("channel %s: %s endpoint was last reached by %s %dd ago", row.Channel, row.Type, a, at.Days)
				} else if at.At != "" {
					text = fmt.Sprintf("channel %s: %s endpoint was last reached by %s %dd ago — stale", row.Channel, row.Type, a, at.Days)
				}
				ws = append(ws, Warning{Rung: RungAttested, Text: text,
					Command: fmt.Sprintf("atm profile verify --project %s --agent %s", in.Code, a)})
				rung = RungWired
			}
		}
		ar.Rung[a] = rung
		ar.Warnings[a] = ws
	}
	if len(agents) == 0 {
		// Nothing can attest; the ladder stops at the machine rungs.
		rung := sharedRung
		if rung == RungAttested && len(endpoints) > 0 {
			rung = RungWired
		}
		ar.Rung[""] = rung
		ar.Warnings[""] = shared
	}
	return ar
}
