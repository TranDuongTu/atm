package core

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ChannelMetaKey is the Task.Meta key the channel capability owns.
const ChannelMetaKey = "channel"

// Channel types recognized today; the type is the channel:<type> label suffix.
const (
	ChannelTypeRepo   = "repo"
	ChannelTypeNotion = "notion"
	ChannelTypeSlack  = "slack"
)

var ChannelTypes = []string{ChannelTypeRepo, ChannelTypeNotion, ChannelTypeSlack}

// ValidChannelRole reports whether r is a legal endpoint role.
func ValidChannelRole(r string) bool {
	return r == ChannelRoleHome || r == ChannelRoleBroadcast
}

// ChannelLabel is the stored label a v2 channel task carries: bare and
// handle-keyed. The TYPE left the label when a channel gained several
// endpoints — a record's identity lives in its payload, the same move v2
// checklists made when suits became many-valued.
func ChannelLabel(code string) string { return code + ":channel" }

// ChannelQueryExpr matches both record generations: the bare v2 label by
// stored-label lookup, and the v1 per-medium labels via the namespace
// predicate. Both terms are required — the namespace predicate is a prefix
// test on "channel:" and does not match the bare label.
const ChannelQueryExpr = "channel OR channel:*"

// ChannelTypeLabelPrefix is the v1 label prefix (<code>:channel:<type>).
// v1 records stay readable under it and decode as a single endpoint.
func ChannelTypeLabelPrefix(code string) string { return code + ":channel:" }

// ChannelTypeLabel is the v1 label for one type. Kept for the vocabulary
// the capability still seeds so old records keep a described label.
func ChannelTypeLabel(code, typ string) string { return code + ":channel:" + typ }

// ChannelAddress is the machine-independent address of a channel — tier 1,
// synced. An address is not a credential. Type-shaped: repo uses URL; notion
// uses Workspace plus Database or Page; slack uses Workspace plus ChannelID.
// Slack shares Workspace because workspace is Slack's own noun for it, and it
// holds the domain SLUG ("acme" of acme.slack.com), not the T… team id — the
// slug is what builds a permalink, and the team id is not.
type ChannelAddress struct {
	URL       string `json:"url,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Database  string `json:"database,omitempty"`
	Page      string `json:"page,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
}

// ChannelEndpoint is one way a channel is reached: a medium, an address
// shaped for it, and the ROLE it plays.
//
// A channel is a place content flows, not a single address. The design
// channel can be a Notion database that holds the documents AND a Slack
// channel that gets told when one lands. Roles make that mechanical rather
// than conventional: content goes to the home, a one-line reference goes to
// every broadcast, and reads scan them all.
type ChannelEndpoint struct {
	Type string `json:"type"`
	// Role is home (content lands here; at most one per channel) or
	// broadcast (receives a reference to what landed).
	Role    string         `json:"role"`
	Address ChannelAddress `json:"address,omitzero"`
}

// Endpoint roles. A channel's role_hint names the role an endpoint created
// for it should take by default.
const (
	ChannelRoleHome      = "home"
	ChannelRoleBroadcast = "broadcast"
)

// DefaultRoleForType is the role an endpoint of this medium takes when
// nobody says otherwise: document-shaped media hold content, messaging
// media carry references to it.
func DefaultRoleForType(typ string) string {
	switch typ {
	case ChannelTypeSlack:
		return ChannelRoleBroadcast
	default:
		return ChannelRoleHome
	}
}

// ValidateChannelEndpoints enforces the endpoint-set invariants: known
// types and roles, one medium at most once, and AT MOST ONE HOME — content
// lands in exactly one place, or "the home endpoint" means nothing to a
// writer. An empty set is legal: a profile declares channel EXPECTATIONS
// with no address at all, and the endpoints are per-project setup that
// follows.
func ValidateChannelEndpoints(eps []ChannelEndpoint) error {
	homes := 0
	seen := map[string]bool{}
	for _, e := range eps {
		if !slices.Contains(ChannelTypes, e.Type) {
			return fmt.Errorf("endpoint type %q is not one of %v", e.Type, ChannelTypes)
		}
		if seen[e.Type] {
			return fmt.Errorf("endpoint type %q is declared twice; a channel reaches each medium once", e.Type)
		}
		seen[e.Type] = true
		switch e.Role {
		case ChannelRoleHome:
			homes++
		case ChannelRoleBroadcast:
		default:
			return fmt.Errorf("endpoint role %q must be %s or %s", e.Role, ChannelRoleHome, ChannelRoleBroadcast)
		}
	}
	if homes > 1 {
		return fmt.Errorf("a channel has at most one %s endpoint; content lands in exactly one place", ChannelRoleHome)
	}
	return nil
}

// ChannelRecord is the tier-1 ledger record decoded from a channel task.
// Name is the unique handle within the project (canonical in the payload, so
// it survives title edits); Purpose is the task description. The json tags
// are load-bearing: ChannelView embeds this struct and `--output json` on
// list/show is the agent endpoint's contract — snake_case keys, not Go field
// names.
type ChannelRecord struct {
	TaskID  string `json:"task_id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Purpose string `json:"purpose,omitempty"`
	// RoleHint is what an endpoint created for this channel defaults to.
	// A profile declares it on a channel EXPECTATION — a handle and what
	// belongs in it, with no address and no type, since both are
	// per-project, per-machine facts. Empty means home.
	RoleHint string `json:"role_hint,omitempty"`
	// Endpoints are every way the channel is reached. Type and Address
	// above are the FIRST endpoint's, kept so readers written before
	// endpoints existed keep working; new code reads Endpoints.
	Endpoints []ChannelEndpoint `json:"endpoints,omitempty"`
	Address   ChannelAddress    `json:"address,omitzero"`
	// Origin is reset provenance: user | <profile>@<version>. A channel
	// applied from a profile restores its purpose and role hint from that
	// version; its endpoints are project facts and are never part of it.
	Origin string `json:"origin,omitempty"`
}

// Home is the endpoint content lands in.
func (r ChannelRecord) Home() (ChannelEndpoint, bool) {
	for _, e := range r.Endpoints {
		if e.Role == ChannelRoleHome {
			return e, true
		}
	}
	return ChannelEndpoint{}, false
}

// Broadcasts are the endpoints that receive a reference to what landed,
// in declaration order.
func (r ChannelRecord) Broadcasts() []ChannelEndpoint {
	var out []ChannelEndpoint
	for _, e := range r.Endpoints {
		if e.Role == ChannelRoleBroadcast {
			out = append(out, e)
		}
	}
	return out
}

// Endpoint returns the channel's endpoint for one medium.
func (r ChannelRecord) Endpoint(typ string) (ChannelEndpoint, bool) {
	for _, e := range r.Endpoints {
		if e.Type == typ {
			return e, true
		}
	}
	return ChannelEndpoint{}, false
}

// ChannelProbe is the cheap local-probe result for a channel's wiring. All
// checks are local; probing never fetches or speaks a third-party API.
type ChannelProbe struct {
	PathExists  bool `json:"path_exists"`
	IsGitRepo   bool `json:"is_git_repo"`
	Dirty       bool `json:"dirty"`
	HasUpstream bool `json:"has_upstream"`
	Ahead       int  `json:"ahead"`
	Behind      int  `json:"behind"`
}

// ChannelView is the joined read: ledger record + this machine's wiring +
// probe. Wiring is nil when this machine has none; Probe is nil when there
// is nothing to probe locally (e.g. a notion channel).
type ChannelView struct {
	ChannelRecord
	Wiring *ChannelWiring `json:"wiring,omitempty"`
	Probe  *ChannelProbe  `json:"probe,omitempty"`
}

// EndpointWiring resolves how THIS machine reaches one of the channel's
// endpoints. Per-endpoint wiring wins; the pre-endpoint fields answer for
// the record's FIRST endpoint and for nothing else, so a second medium
// added later starts out honestly unwired rather than inheriting a path
// that was never about it.
func (v ChannelView) EndpointWiring(typ string) EndpointWiring {
	if v.Wiring == nil {
		return EndpointWiring{}
	}
	if e, ok := v.Wiring.Endpoints[typ]; ok {
		return e
	}
	if len(v.Endpoints) > 0 && v.Endpoints[0].Type == typ {
		return EndpointWiring{Path: v.Wiring.Path, MCPServer: v.Wiring.MCPServer, Stamps: v.Wiring.Stamps}
	}
	return EndpointWiring{}
}

// WiredEndpoints are the endpoints this machine can actually reach.
func (v ChannelView) WiredEndpoints() []ChannelEndpoint {
	var out []ChannelEndpoint
	for _, e := range v.Endpoints {
		if v.EndpointWiring(e.Type).Wired() {
			out = append(out, e)
		}
	}
	return out
}

// statusWiring picks the endpoint the channel's one-line status speaks for:
// the first WIRED one in declaration order. A channel with no endpoints at
// all falls back to the pre-endpoint wiring, so a record that predates
// endpoints and has never been rewritten still reports.
func statusWiring(v ChannelView) (EndpointWiring, bool) {
	if v.Wiring == nil {
		return EndpointWiring{}, false
	}
	for _, e := range v.Endpoints {
		if w := v.EndpointWiring(e.Type); w.Wired() {
			return w, true
		}
	}
	if len(v.Endpoints) == 0 {
		legacy := EndpointWiring{Path: v.Wiring.Path, MCPServer: v.Wiring.MCPServer, Stamps: v.Wiring.Stamps}
		if legacy.Wired() {
			return legacy, true
		}
	}
	return EndpointWiring{}, false
}

// ChannelStatus is the single-sourced status rule every surface reads: ●
// wired and verified fresh (or probe-green), ◐ wired but aging/dirty, ○
// unwired, missing, or stale. It lives in core because the CLI and the TUI
// must not answer differently about the same record — a probe the store
// already paid for is part of the answer, and no surface may claim more than
// ATM can know. now is injected for testability; the note is the text-mode
// answer, the glyph the TUI's.
//
// A channel is WIRED when any endpoint is: content still flows even if one
// medium is not set up here. A single-endpoint channel reads exactly as it
// always did.
func ChannelStatus(v ChannelView, now time.Time) (string, string) {
	wiring, ok := statusWiring(v)
	if !ok {
		return "○", "unwired"
	}
	if v.Probe != nil {
		if !v.Probe.PathExists {
			return "○", "path missing"
		}
		if !v.Probe.IsGitRepo {
			return "◐", "not a git repo"
		}
		note := "clean"
		if v.Probe.Dirty {
			note = "dirty"
		}
		if v.Probe.HasUpstream && (v.Probe.Ahead > 0 || v.Probe.Behind > 0) {
			return "◐", fmt.Sprintf("%s · %d ahead %d behind", note, v.Probe.Ahead, v.Probe.Behind)
		}
		if v.Probe.Dirty {
			return "◐", note
		}
		return "●", note
	}
	if len(wiring.Stamps) == 0 {
		return "◐", "wired, never verified"
	}
	last := wiring.Stamps[len(wiring.Stamps)-1]
	at, err := time.Parse(time.RFC3339, last.At)
	if err != nil {
		return "◐", "unparseable stamp"
	}
	days := int(now.Sub(at).Hours() / 24)
	switch {
	case days <= 14:
		return "●", fmt.Sprintf("verified %dd ago", days)
	case days <= 45:
		return "◐", fmt.Sprintf("verified %dd ago", days)
	default:
		return "○", fmt.Sprintf("stale · verified %dd ago", days)
	}
}

// DecodeChannelPayload parses a payload string; "" is a valid empty payload.
// A malformed payload is an ERROR — verbs fail rather than overwrite state
// they cannot read (the same doctrine as every capability payload).
func DecodeChannelPayload(s string) (map[string]any, error) {
	if s == "" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("%s payload is not a JSON object (hand-repair needed): %w", ChannelMetaKey, err)
	}
	return m, nil
}

// EncodeChannelPayload serializes, stamping v:1. Unknown fields survive
// because the map is the source of truth. Empty (besides v) encodes to "".
// The argument is copied, never mutated: callers decode-mutate-encode and
// must not have their map silently stamped by a failed encode.
func EncodeChannelPayload(m map[string]any) (string, error) {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		if k != "v" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return "", nil
	}
	out["v"] = 1
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ChannelPayloadFrom builds the payload map for a record (name, type, address).
func ChannelPayloadFrom(rec ChannelRecord) map[string]any {
	m := map[string]any{"name": rec.Name}
	if rec.RoleHint != "" && rec.RoleHint != ChannelRoleHome {
		m["role_hint"] = rec.RoleHint
	}
	if rec.Origin != "" && rec.Origin != "user" {
		// user is the default an unwritten key already means; writing it
		// would churn every record that predates origins on first edit.
		m["origin"] = rec.Origin
	}
	eps := rec.Endpoints
	if len(eps) == 0 && rec.Type != "" {
		// A caller still speaking the single-address shape writes one
		// endpoint, so one writer cannot silently drop the other's data.
		eps = []ChannelEndpoint{{Type: rec.Type, Role: DefaultRoleForType(rec.Type), Address: rec.Address}}
	}
	if len(eps) > 0 {
		list := make([]any, 0, len(eps))
		for _, e := range eps {
			ep := map[string]any{"type": e.Type, "role": e.Role}
			if addr := channelAddressToAny(e.Address); len(addr) > 0 {
				ep["address"] = addr
			}
			list = append(list, ep)
		}
		m["endpoints"] = list
		// Type and address stay written for a binary that predates
		// endpoints: it reads the first endpoint and keeps working.
		m["type"] = eps[0].Type
		if addr := channelAddressToAny(eps[0].Address); len(addr) > 0 {
			m["address"] = addr
		}
	}
	return m
}

func channelAddressToAny(a ChannelAddress) map[string]any {
	out := map[string]any{}
	if a.URL != "" {
		out["url"] = a.URL
	}
	if a.Workspace != "" {
		out["workspace"] = a.Workspace
	}
	if a.Database != "" {
		out["database"] = a.Database
	}
	if a.Page != "" {
		out["page"] = a.Page
	}
	if a.ChannelID != "" {
		out["channel_id"] = a.ChannelID
	}
	return out
}

func channelAddressFromAny(v any) ChannelAddress {
	m, ok := v.(map[string]any)
	if !ok {
		return ChannelAddress{}
	}
	return ChannelAddress{
		URL:       channelStr(m["url"]),
		Workspace: channelStr(m["workspace"]),
		Database:  channelStr(m["database"]),
		Page:      channelStr(m["page"]),
		ChannelID: channelStr(m["channel_id"]),
	}
}

// channelEndpointsFromAny decodes the endpoints list, defaulting a missing
// role from the medium.
func channelEndpointsFromAny(v any) []ChannelEndpoint {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []ChannelEndpoint
	for _, e := range raw {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		ep := ChannelEndpoint{Type: channelStr(m["type"]), Role: channelStr(m["role"]), Address: channelAddressFromAny(m["address"])}
		if ep.Type == "" {
			continue
		}
		if ep.Role == "" {
			ep.Role = DefaultRoleForType(ep.Type)
		}
		out = append(out, ep)
	}
	return out
}

func ChannelFromTask(code string, t Task) (*ChannelRecord, error) {
	bare := ChannelLabel(code)
	prefix := ChannelTypeLabelPrefix(code)
	isV2Label, labelType := false, ""
	for _, l := range t.Labels {
		if l == bare {
			isV2Label = true
			break
		}
		if strings.HasPrefix(l, prefix) {
			labelType = strings.TrimPrefix(l, prefix)
			break
		}
	}
	if !isV2Label && labelType == "" {
		return nil, nil
	}
	m, err := DecodeChannelPayload(t.Meta[ChannelMetaKey])
	if err != nil {
		return nil, fmt.Errorf("task %s: %w", t.ID, err)
	}
	rec := &ChannelRecord{TaskID: t.ID, Name: channelStr(m["name"]), Purpose: t.Description, RoleHint: channelStr(m["role_hint"]), Origin: channelStr(m["origin"])}
	if rec.Name == "" {
		rec.Name = t.Title
	}
	if rec.Origin == "" {
		rec.Origin = "user"
	}
	if rec.RoleHint == "" {
		rec.RoleHint = ChannelRoleHome
	}
	rec.Endpoints = channelEndpointsFromAny(m["endpoints"])
	if len(rec.Endpoints) == 0 {
		// READ-COMPAT: a record written before endpoints existed carries
		// its medium in the label and one address in the payload. It reads
		// as a single endpoint whose role is inferred from the medium, so
		// no project loses its channels on upgrade and no migration runs.
		typ := labelType
		if typ == "" {
			typ = channelStr(m["type"])
		}
		if addr := channelAddressFromAny(m["address"]); typ != "" {
			rec.Endpoints = []ChannelEndpoint{{Type: typ, Role: DefaultRoleForType(typ), Address: addr}}
		}
	}
	if len(rec.Endpoints) > 0 {
		// Type and Address answer for the FIRST endpoint, so readers
		// written before this change keep working unchanged.
		rec.Type = rec.Endpoints[0].Type
		rec.Address = rec.Endpoints[0].Address
	} else if labelType != "" {
		rec.Type = labelType
	}
	return rec, nil
}

func channelStr(v any) string { s, _ := v.(string); return s }
