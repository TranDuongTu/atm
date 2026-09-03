// internal/store/channel.go
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"atm/internal/core"
)

// channelTypeValid reports whether typ is a recognized channel type.
func channelTypeValid(typ string) bool {
	for _, t := range core.ChannelTypes {
		if t == typ {
			return true
		}
	}
	return false
}

// ChannelRecords lists the project's tier-1 channel records, decoding every
// task in the channel:* namespace. Tasks with unreadable payloads are skipped
// here — list degrades rather than failing whole; the capability's Annotate
// cell and a by-name lookup of that record surface the breakage.
func (s *Store) ChannelRecords(code string) ([]core.ChannelRecord, error) {
	tasks, err := s.ListTasksErr(core.QueryFilters{Project: code, Expr: core.ChannelQueryExpr})
	if err != nil {
		return nil, err
	}
	var out []core.ChannelRecord
	for _, t := range tasks {
		rec, err := core.ChannelFromTask(code, *t)
		if err != nil || rec == nil {
			continue
		}
		out = append(out, *rec)
	}
	return out, nil
}

// channelByName resolves a handle to its record; core.ErrNotFound when absent,
// and a decode error ONLY when the named channel's own payload is unreadable.
// A corrupt record must not poison lookups of its neighbours: the handle a
// caller asked for either resolves or is absent, no matter what else is in
// the namespace. Every verb that WRITES a payload fails loudly on the broken
// record rather than overwriting what it cannot read; removal is the one verb
// that needs no payload, so it resolves the handle through channelTaskIDByName
// instead — see there for the title fallback that names the broken record.
func (s *Store) channelByName(code, name string) (*core.ChannelRecord, error) {
	tasks, err := s.ListTasksErr(core.QueryFilters{Project: code, Expr: core.ChannelQueryExpr})
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		rec, err := core.ChannelFromTask(code, *t)
		if err != nil {
			if t.Title == name {
				return nil, err // the one they asked for is the broken one
			}
			continue
		}
		if rec != nil && rec.Name == name {
			return rec, nil
		}
	}
	return nil, fmt.Errorf("%w: channel %q", core.ErrNotFound, name)
}

// CreateChannel authors the tier-1 record: a task titled by the handle,
// labelled channel:<type>, description = purpose, payload = name/type/address.
// The handle must be unique across the project's channels (all types).
func (s *Store) CreateChannel(code string, rec core.ChannelRecord, actor string) (*core.Task, error) {
	if rec.Name == "" {
		return nil, fmt.Errorf("%w: channel needs a name", core.ErrUsage)
	}
	// A caller may still speak the single-address shape; fold it into the
	// endpoint set so there is one representation from here down.
	if len(rec.Endpoints) == 0 && rec.Type != "" {
		rec.Endpoints = []core.ChannelEndpoint{{Type: rec.Type, Role: core.DefaultRoleForType(rec.Type), Address: rec.Address}}
	}
	if len(rec.Endpoints) == 0 {
		return nil, fmt.Errorf("%w: channel needs at least one endpoint type in %v", core.ErrUsage, core.ChannelTypes)
	}
	if err := core.ValidateChannelEndpoints(rec.Endpoints); err != nil {
		return nil, fmt.Errorf("%w: %v", core.ErrUsage, err)
	}
	existing, err := s.ChannelRecords(code)
	if err != nil {
		return nil, err
	}
	for _, e := range existing {
		if e.Name == rec.Name {
			return nil, fmt.Errorf("%w: channel %q already exists (task %s)", core.ErrUsage, rec.Name, e.TaskID)
		}
	}
	t, err := s.CreateTask(code, rec.Name, rec.Purpose, []string{core.ChannelLabel(code)}, actor)
	if err != nil {
		return nil, err
	}
	payload, err := core.EncodeChannelPayload(core.ChannelPayloadFrom(rec))
	if err != nil {
		return nil, err
	}
	if err := s.SetTaskCapabilityMeta(t.ID, core.ChannelMetaKey, payload, actor); err != nil {
		return nil, err
	}
	return s.GetTask(t.ID)
}

// EditChannel updates purpose and/or address. Address writes decode the
// existing payload and mutate only the address key, so unknown fields from
// newer binaries survive (degrade-never-reject applied to ourselves). An
// address with every field empty CLEARS the key rather than writing a null.
// There is no rename: the handle is the channel's identity, referenced by
// tier-2 wiring keys and by agents that resolved it once — re-authoring a
// channel under a new handle is `remove` + `add`.
func (s *Store) EditChannel(code, name string, purpose *string, addr *core.ChannelAddress, actor string) error {
	rec, err := s.channelByName(code, name)
	if err != nil {
		return err
	}
	if purpose != nil {
		if err := s.SetDescription(rec.TaskID, *purpose, actor); err != nil {
			return err
		}
	}
	if addr != nil {
		// An address edit names no medium, so it targets the channel's
		// FIRST endpoint — the one Type and Address have always answered
		// for. Writing through the endpoint set rather than the legacy
		// address key keeps the two from drifting apart; a channel with
		// several media corrects a specific one with `endpoint add`.
		next := make([]core.ChannelEndpoint, len(rec.Endpoints))
		copy(next, rec.Endpoints)
		switch {
		case len(next) > 0:
			next[0].Address = *addr
		case rec.Type != "":
			next = []core.ChannelEndpoint{{Type: rec.Type, Role: core.DefaultRoleForType(rec.Type), Address: *addr}}
		default:
			return fmt.Errorf("%w: channel %s has no endpoint to address; add one with `atm channel endpoint add`", core.ErrUsage, name)
		}
		if err := s.writeChannelEndpoints(code, rec, next, actor); err != nil {
			return err
		}
	}
	return nil
}

// channelTaskIDByName resolves a handle to its TASK ID only — the weaker
// lookup removal needs. A record whose payload is unreadable has no knowable
// handle, so its task TITLE (written from the handle at creation) is the
// fallback identity: without it a corrupt record would be unremovable through
// its own noun, and the capability guide forbids repairing channel records
// with raw task verbs. Healthy records win: the title fallback is only
// consulted after the whole namespace failed to yield a payload match, so a
// broken record can never shadow a live channel of the same handle.
func (s *Store) channelTaskIDByName(code, name string) (string, error) {
	tasks, err := s.ListTasksErr(core.QueryFilters{Project: code, Expr: core.ChannelQueryExpr})
	if err != nil {
		return "", err
	}
	broken := ""
	for _, t := range tasks {
		rec, err := core.ChannelFromTask(code, *t)
		if err != nil {
			if t.Title == name && broken == "" {
				broken = t.ID
			}
			continue
		}
		if rec != nil && rec.Name == name {
			return t.ID, nil
		}
	}
	if broken != "" {
		return broken, nil
	}
	return "", fmt.Errorf("%w: channel %q", core.ErrNotFound, name)
}

// RemoveChannel removes the ledger record (task tombstone) and drops this
// machine's wiring entry for the handle, if any. Unlike every other verb it
// tolerates an unreadable payload — deleting a record does not require
// understanding it, and refusing here would strand the broken record forever.
func (s *Store) RemoveChannel(code, name, actor string) error {
	id, err := s.channelTaskIDByName(code, name)
	if err != nil {
		return err
	}
	if err := s.RemoveTask(id, actor); err != nil {
		return err
	}
	return s.dropChannelWiring(code, name, actor)
}

// SetChannelWiring records how THIS machine reaches one of the channel's
// endpoints — tier 2: config, not substrate, no event, no secrets. Merge
// semantics: a non-empty path or mcpServer overwrites that field, an empty
// one keeps the existing value, and stamps always survive. An empty typ
// resolves to the channel's only endpoint, and is a usage error when the
// channel has several — wiring the wrong medium silently is worse than
// asking which one.
func (s *Store) SetChannelWiring(code, name, typ, path, mcpServer, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	rec, err := s.channelByName(code, name)
	if err != nil {
		return err
	}
	typ, err = resolveEndpointType(rec, typ)
	if err != nil {
		return err
	}
	abs := ""
	if path != "" {
		var err error
		abs, err = filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve channel path: %w", err)
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return fmt.Errorf("%w: channel path does not exist or is not a directory: %s", core.ErrUsage, abs)
		}
	}
	return s.WithLock(code, func() error {
		merged, err := s.lockedProjectConfig(code)
		if err != nil {
			return err
		}
		if merged.Channels == nil {
			merged.Channels = map[string]core.ChannelWiring{}
		}
		w := merged.Channels[name]
		e := endpointWiringOf(w, rec, typ)
		if abs != "" {
			e.Path = abs
		}
		if mcpServer != "" {
			e.MCPServer = mcpServer
		}
		merged.Channels[name] = putEndpointWiring(w, rec, typ, e)
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}

// AddChannelStamp appends a verification stamp (actor + timestamp + kind +
// note) to one endpoint's wiring: "this agent actually reached this endpoint
// and vouches". The actor names the harness, so the per-agent attestation
// matrix is an aggregation of these — no separate record.
func (s *Store) AddChannelStamp(code, name, typ, kind, note, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	rec, err := s.channelByName(code, name)
	if err != nil {
		return err
	}
	typ, err = resolveEndpointType(rec, typ)
	if err != nil {
		return err
	}
	if kind == "" {
		kind = core.StampKindUse
	}
	if !core.ValidStampKind(kind) {
		return fmt.Errorf("%w: stamp kind %q must be one of %v", core.ErrUsage, kind, core.StampKinds)
	}
	return s.WithLock(code, func() error {
		merged, err := s.lockedProjectConfig(code)
		if err != nil {
			return err
		}
		if merged.Channels == nil {
			merged.Channels = map[string]core.ChannelWiring{}
		}
		w := merged.Channels[name]
		e := endpointWiringOf(w, rec, typ)
		e.Stamps = append(e.Stamps, core.VerificationStamp{At: core.RFC3339UTC(core.Now()), By: actor, Kind: kind, Note: note})
		merged.Channels[name] = putEndpointWiring(w, rec, typ, e)
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}

// lockedProjectConfig is the read half of every config read-modify-write in
// this file: current config or a fresh zero value. Call under WithLock only.
func (s *Store) lockedProjectConfig(code string) (*ProjectConfig, error) {
	existing, err := s.GetProjectConfig(code)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	return &ProjectConfig{}, nil
}

// dropChannelWiring removes the handle's wiring entry, silently succeeding
// when there is none (RemoveChannel's local cleanup).
func (s *Store) dropChannelWiring(code, name, actor string) error {
	return s.WithLock(code, func() error {
		merged, err := s.lockedProjectConfig(code)
		if err != nil {
			return err
		}
		if _, ok := merged.Channels[name]; !ok {
			return nil
		}
		delete(merged.Channels, name)
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}

// ProjectChannels is the joined read every surface consumes: tier-1 records
// + this machine's tier-2 wiring + local probes, sorted by handle. Probes run
// only where there is something local to probe (a repo channel with a path).
func (s *Store) ProjectChannels(code string) ([]core.ChannelView, error) {
	recs, err := s.ChannelRecords(code)
	if err != nil {
		return nil, err
	}
	cfg, err := s.GetProjectConfig(code)
	if err != nil {
		return nil, err
	}
	var wirings map[string]core.ChannelWiring
	if cfg != nil {
		wirings = cfg.Channels
	}
	out := make([]core.ChannelView, 0, len(recs))
	for _, rec := range recs {
		v := core.ChannelView{ChannelRecord: rec}
		if w, ok := wirings[rec.Name]; ok {
			wc := w
			v.Wiring = &wc
		}
		// The probe belongs to the REPO endpoint, wherever that sits in the
		// endpoint set: a channel whose Notion database is the home and
		// whose repo is a broadcast is still locally probeable.
		if _, ok := rec.Endpoint(core.ChannelTypeRepo); ok {
			if path := v.EndpointWiring(core.ChannelTypeRepo).Path; path != "" {
				v.Probe = probeRepoPath(path)
			}
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GetChannelByName is ProjectChannels narrowed to one handle — the CLI's
// `atm channel show` read and the agent endpoint's shape.
func (s *Store) GetChannelByName(code, name string) (*core.ChannelView, error) {
	views, err := s.ProjectChannels(code)
	if err != nil {
		return nil, err
	}
	for i := range views {
		if views[i].Name == name {
			return &views[i], nil
		}
	}
	return nil, fmt.Errorf("%w: channel %q", core.ErrNotFound, name)
}

// MigrateReposToChannels lifts every legacy RepoConfig into a repo channel:
// tier-1 record (handle, URL; purpose left for the concierge to author) plus
// tier-2 wiring (path), then clears the legacy repos entries actually
// accounted for. Returns the number migrated, the handles whose PATH could
// not be wired, and the handles left in place because their name already
// belongs to a channel of a DIFFERENT type.
//
// A legacy path that no longer exists (the folder moved since it was
// recorded) must not abort the migration: SetChannelWiring rejects a missing
// directory, and failing there would leave the tier-1 record created, `repos`
// uncleared, and every re-run stuck on the same entry forever. The ledger
// record is the part worth keeping — it is what lets a concierge re-wire the
// channel — so a missing path is reported, not fatal.
//
// A legacy repo name that collides with an EXISTING channel of a different
// type (e.g. a hand-created notion channel named "docs") must not be
// silently dropped: the legacy repo's Path/URL live nowhere else, so
// clearing it out from under the collision would be unrecoverable data
// loss. Such entries are left exactly where they are and reported in
// `skipped`, not migrated and not cleared. Only a same-named REPO-type
// channel (a prior migration run) counts as "already accounted for" and
// clears the legacy entry without recreating anything.
func (s *Store) MigrateReposToChannels(code, actor string) (int, []string, []string, error) {
	repos, err := s.ProjectRepos(code)
	if err != nil {
		return 0, nil, nil, err
	}
	existing, err := s.ChannelRecords(code)
	if err != nil {
		return 0, nil, nil, err
	}
	takenType := make(map[string]string, len(existing))
	for _, e := range existing {
		takenType[e.Name] = e.Type
	}
	n := 0
	var unwired, skipped []string
	accounted := make(map[string]bool, len(repos))
	for _, r := range repos {
		switch takenType[r.Name] {
		case core.ChannelTypeRepo:
			// Already migrated in an earlier run: the information lives in
			// the channel record, so the legacy entry is safe to clear.
			accounted[r.Name] = true
			continue
		case "":
			// No existing channel by this name: safe to migrate below.
		default:
			// Name collides with a channel of a different type: migrating
			// would discard this repo's info with nowhere to preserve it.
			skipped = append(skipped, r.Name)
			continue
		}
		if _, err := s.CreateChannel(code, core.ChannelRecord{Name: r.Name, Type: core.ChannelTypeRepo, Address: core.ChannelAddress{URL: r.URL}}, actor); err != nil {
			return n, unwired, skipped, err
		}
		if err := s.SetChannelWiring(code, r.Name, core.ChannelTypeRepo, r.Path, "", actor); err != nil {
			if !errors.Is(err, core.ErrUsage) {
				return n, unwired, skipped, err
			}
			unwired = append(unwired, r.Name) // path gone: record kept, wiring left to concierge
		}
		accounted[r.Name] = true
		n++
	}
	if len(accounted) > 0 {
		if err := s.WithLock(code, func() error {
			merged, err := s.lockedProjectConfig(code)
			if err != nil {
				return err
			}
			kept := merged.Repos[:0:0]
			for _, r := range merged.Repos {
				if !accounted[r.Name] {
					kept = append(kept, r)
				}
			}
			merged.Repos = kept
			merged.UpdatedAt = core.RFC3339UTC(core.Now())
			merged.UpdatedBy = actor
			return WriteFileAtomic(s.configPath(code), merged)
		}); err != nil {
			return n, unwired, skipped, err
		}
	}
	return n, unwired, skipped, nil
}

// RepoChannelTargets is the dispatch read: repo channels wired on THIS
// machine, as the dispatch targets the dialog already understands. Separate
// from ProjectChannels on purpose — dispatch opens on a keypress and must not
// shell out to `git status`/`rev-list` once per repo to draw a picker. Probes
// belong to the surfaces that display status, not to the ones that navigate.
func (s *Store) RepoChannelTargets(code string) ([]core.RepoConfig, error) {
	recs, err := s.ChannelRecords(code)
	if err != nil {
		return nil, err
	}
	cfg, err := s.GetProjectConfig(code)
	if err != nil {
		return nil, err
	}
	var wirings map[string]core.ChannelWiring
	if cfg != nil {
		wirings = cfg.Channels
	}
	var out []core.RepoConfig
	for _, rec := range recs {
		ep, ok := rec.Endpoint(core.ChannelTypeRepo)
		if !ok {
			continue
		}
		w, ok := wirings[rec.Name]
		if !ok {
			continue
		}
		// Resolve through the view so the pre-endpoint wiring still counts
		// for a record that has never been rewritten.
		view := core.ChannelView{ChannelRecord: rec, Wiring: &w}
		if path := view.EndpointWiring(core.ChannelTypeRepo).Path; path != "" {
			out = append(out, core.RepoConfig{Name: rec.Name, Path: path, URL: ep.Address.URL})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// AddChannelEndpoint adds or replaces the channel's endpoint for one medium.
// Replacing rather than duplicating is deliberate: a channel reaches each
// medium once, so re-adding a type is how an address is corrected.
func (s *Store) AddChannelEndpoint(code, name string, ep core.ChannelEndpoint, actor string) error {
	rec, err := s.channelByName(code, name)
	if err != nil {
		return err
	}
	if ep.Role == "" {
		ep.Role = rec.RoleHint
		if _, hasHome := rec.Home(); hasHome && ep.Role == core.ChannelRoleHome {
			// The hint asked for a home this channel already has; a second
			// one would make "the home endpoint" ambiguous, so the new
			// medium carries the reference instead.
			ep.Role = core.ChannelRoleBroadcast
		}
		if ep.Role == "" {
			ep.Role = core.DefaultRoleForType(ep.Type)
		}
	}
	next := make([]core.ChannelEndpoint, 0, len(rec.Endpoints)+1)
	replaced := false
	for _, e := range rec.Endpoints {
		if e.Type == ep.Type {
			next = append(next, ep)
			replaced = true
			continue
		}
		next = append(next, e)
	}
	if !replaced {
		next = append(next, ep)
	}
	if err := core.ValidateChannelEndpoints(next); err != nil {
		return fmt.Errorf("%w: %v", core.ErrUsage, err)
	}
	return s.writeChannelEndpoints(code, rec, next, actor)
}

// RemoveChannelEndpoint drops the channel's endpoint for one medium. The
// channel record survives: a handle with no endpoints is a legitimate
// expectation waiting to be addressed, which is exactly what applying a
// profile creates.
func (s *Store) RemoveChannelEndpoint(code, name, typ, actor string) error {
	rec, err := s.channelByName(code, name)
	if err != nil {
		return err
	}
	if _, ok := rec.Endpoint(typ); !ok {
		return fmt.Errorf("%w: channel %s has no %s endpoint", core.ErrNotFound, name, typ)
	}
	next := make([]core.ChannelEndpoint, 0, len(rec.Endpoints))
	for _, e := range rec.Endpoints {
		if e.Type != typ {
			next = append(next, e)
		}
	}
	return s.writeChannelEndpoints(code, rec, next, actor)
}

// writeChannelEndpoints replaces the endpoint set, decoding the existing
// payload first so unknown fields from a newer binary survive
// (degrade-never-reject applied to ourselves), and relabelling a v1 record
// to the bare label on the way — writes migrate, reads never do.
func (s *Store) writeChannelEndpoints(code string, rec *core.ChannelRecord, eps []core.ChannelEndpoint, actor string) error {
	t, err := s.GetTask(rec.TaskID)
	if err != nil {
		return err
	}
	m, err := core.DecodeChannelPayload(t.Meta[core.ChannelMetaKey])
	if err != nil {
		return err
	}
	next := *rec
	next.Endpoints = eps
	for k, v := range core.ChannelPayloadFrom(next) {
		m[k] = v
	}
	if len(eps) == 0 {
		delete(m, "endpoints")
		delete(m, "type")
		delete(m, "address")
	}
	payload, err := core.EncodeChannelPayload(m)
	if err != nil {
		return err
	}
	if err := s.relabelChannelV1(code, t, actor); err != nil {
		return err
	}
	return s.SetTaskCapabilityMeta(rec.TaskID, core.ChannelMetaKey, payload, actor)
}

// relabelChannelV1 moves a record from its per-medium label to the bare
// label. A record already carrying the bare label is left alone; a lingering
// medium label from an interrupted earlier move is still cleaned up.
func (s *Store) relabelChannelV1(code string, t *core.Task, actor string) error {
	bare := core.ChannelLabel(code)
	hasBare := false
	var legacy []string
	for _, l := range t.Labels {
		switch {
		case l == bare:
			hasBare = true
		case strings.HasPrefix(l, core.ChannelTypeLabelPrefix(code)):
			legacy = append(legacy, l)
		}
	}
	if !hasBare {
		if err := s.TaskLabelAdd(t.ID, bare, actor); err != nil {
			return err
		}
	}
	for _, l := range legacy {
		if err := s.TaskLabelRemove(t.ID, l, actor); err != nil {
			return err
		}
	}
	return nil
}

// resolveEndpointType turns an unspecified medium into the channel's only
// one. Ambiguity is refused rather than guessed: wiring or stamping the
// wrong medium silently is worse than asking which.
func resolveEndpointType(rec *core.ChannelRecord, typ string) (string, error) {
	if typ != "" {
		if _, ok := rec.Endpoint(typ); !ok && len(rec.Endpoints) > 0 {
			return "", fmt.Errorf("%w: channel %s has no %s endpoint", core.ErrNotFound, rec.Name, typ)
		}
		return typ, nil
	}
	switch len(rec.Endpoints) {
	case 0:
		return "", fmt.Errorf("%w: channel %s has no endpoints yet; add one with `atm channel endpoint add`", core.ErrUsage, rec.Name)
	case 1:
		return rec.Endpoints[0].Type, nil
	default:
		var types []string
		for _, e := range rec.Endpoints {
			types = append(types, e.Type)
		}
		return "", fmt.Errorf("%w: channel %s has several endpoints (%s); name one with --type", core.ErrUsage, rec.Name, strings.Join(types, ", "))
	}
}

// endpointWiringOf reads the current wiring for one medium, folding in the
// pre-endpoint fields when they belong to it.
func endpointWiringOf(w core.ChannelWiring, rec *core.ChannelRecord, typ string) core.EndpointWiring {
	if e, ok := w.Endpoints[typ]; ok {
		return e
	}
	if len(rec.Endpoints) > 0 && rec.Endpoints[0].Type == typ {
		return core.EndpointWiring{Path: w.Path, MCPServer: w.MCPServer, Stamps: w.Stamps}
	}
	return core.EndpointWiring{}
}

// putEndpointWiring stores one medium's wiring. Writing the medium the
// pre-endpoint fields spoke for MIGRATES them: they are cleared, so the two
// representations can never disagree. Same doctrine as the label move —
// reads tolerate the old shape, writes retire it.
func putEndpointWiring(w core.ChannelWiring, rec *core.ChannelRecord, typ string, e core.EndpointWiring) core.ChannelWiring {
	if w.Endpoints == nil {
		w.Endpoints = map[string]core.EndpointWiring{}
	}
	w.Endpoints[typ] = e
	if len(rec.Endpoints) > 0 && rec.Endpoints[0].Type == typ {
		w.Path, w.MCPServer, w.Stamps = "", "", nil
	}
	return w
}
