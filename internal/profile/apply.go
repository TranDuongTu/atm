package profile

import (
	"fmt"
	"slices"
	"strings"

	"atm/internal/core"
)

// Current is what a project already holds: the inputs apply compares a
// profile against. Enabled nil means a legacy project with every built-in
// capability on.
type Current struct {
	Enabled    []string
	Personas   []core.Persona
	Checklists []core.ChecklistRecord
	Channels   []core.ChannelRecord
}

// PlanApply decides, document by document, what applying p to a project
// would do. p must already be substituted for the project (ForProject).
// previous resolves a profile ref to that version's (substituted) profile,
// or nil when it is not available here; it is what lets apply tell a local
// edit from an upstream change, and may be nil.
//
// The rules, in order, for a record that already exists:
//   - owned by ANOTHER profile → conflict, identical or not: the namespace
//     is flat and a collision is never merged silently;
//   - identical content → in sync; restamped when it sits under an older
//     version of this profile, or under a user/legacy origin (adopting it
//     loses nothing and gains a version to reset to);
//   - differs, but matches its own origin version's document → update:
//     the project never touched it, the profile moved;
//   - differs otherwise → conflict.
func PlanApply(p *core.Profile, cur Current, previous func(ref string) *core.Profile) *core.ApplyPlan {
	plan := &core.ApplyPlan{Ref: p.Manifest.Ref()}
	for _, name := range RequiredCapabilities(p) {
		plan.Capabilities = append(plan.Capabilities, core.ApplyCapability{
			Name:    name,
			Enabled: cur.Enabled == nil || slices.Contains(cur.Enabled, name),
		})
	}
	d := decider{profile: p, previous: previous, cache: map[string]*core.Profile{}}
	for _, doc := range p.Personas {
		existing, ok := find(cur.Personas, doc.Name, func(x core.Persona) string { return x.Name })
		if !ok {
			plan.Items = append(plan.Items, core.ApplyItem{Kind: core.ApplyKindPersona, Name: doc.Name, State: core.ApplyCreate})
			continue
		}
		plan.Items = append(plan.Items, d.decide(core.ApplyKindPersona, doc.Name, existing.Origin,
			personaDiff(existing, doc),
			func(prev *core.Profile) ([]string, bool) {
				old, ok := prev.ProfilePersona(doc.Name)
				return personaDiff(existing, old), ok
			}))
	}
	for _, doc := range p.Checklists {
		existing, ok := find(cur.Checklists, doc.Name, func(x core.ChecklistRecord) string { return x.Name })
		if !ok {
			plan.Items = append(plan.Items, core.ApplyItem{Kind: core.ApplyKindChecklist, Name: doc.Name, State: core.ApplyCreate})
			continue
		}
		plan.Items = append(plan.Items, d.decide(core.ApplyKindChecklist, doc.Name, existing.Origin,
			checklistDiff(existing, doc),
			func(prev *core.Profile) ([]string, bool) {
				old, ok := prev.ProfileChecklist(doc.Name)
				return checklistDiff(existing, old), ok
			}))
	}
	for _, doc := range p.Channels {
		existing, ok := find(cur.Channels, doc.Name, func(x core.ChannelRecord) string { return x.Name })
		if !ok {
			plan.Items = append(plan.Items, core.ApplyItem{Kind: core.ApplyKindChannel, Name: doc.Name, State: core.ApplyCreate})
			continue
		}
		plan.Items = append(plan.Items, d.decide(core.ApplyKindChannel, doc.Name, existing.Origin,
			channelDiff(existing, doc),
			func(prev *core.Profile) ([]string, bool) {
				old, ok := prev.ProfileChannel(doc.Name)
				return channelDiff(existing, old), ok
			}))
	}
	return plan
}

// RequiredCapabilities is everything the profile presupposes: what its
// manifest names, plus the substrate a shipped record kind lives on — a
// profile that ships checklists needs the checklist capability whether or
// not its author wrote that down, and the same for channels. Manifest
// order first, implied ones after, no duplicates.
func RequiredCapabilities(p *core.Profile) []string {
	out := append([]string(nil), p.Manifest.RequiresCapabilities...)
	implied := []string{}
	if len(p.Checklists) > 0 {
		implied = append(implied, "checklist")
	}
	if len(p.Channels) > 0 {
		implied = append(implied, "channel")
	}
	for _, name := range implied {
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

func find[T any](xs []T, name string, key func(T) string) (T, bool) {
	for _, x := range xs {
		if key(x) == name {
			return x, true
		}
	}
	var zero T
	return zero, false
}

// decider applies the existing-record rules, loading each origin version
// at most once per plan.
type decider struct {
	profile  *core.Profile
	previous func(ref string) *core.Profile
	cache    map[string]*core.Profile
}

func (d *decider) load(ref string) *core.Profile {
	if old, seen := d.cache[ref]; seen {
		return old
	}
	var old *core.Profile
	if d.previous != nil {
		old = d.previous(ref)
	}
	d.cache[ref] = old
	return old
}

// decide takes the record's origin, its diff against the applied document,
// and a re-diff against an older version of the same profile (consulted
// only when the record differs and came from one).
func (d *decider) decide(kind, name, origin string, diff []string, diffAgainst func(*core.Profile) ([]string, bool)) core.ApplyItem {
	it := core.ApplyItem{Kind: kind, Name: name, Origin: origin}
	o, err := core.ParseOrigin(origin)
	if err != nil {
		// An unparseable origin reads as user: the project owns what
		// nothing else can claim.
		o = core.Origin{Kind: core.OriginUser}
	}
	ref := d.profile.Manifest.Ref()
	changed := strings.Join(diff, ", ")
	switch {
	case o.Kind == core.OriginProfile && o.Profile != d.profile.Manifest.Name:
		it.State = core.ApplyConflict
		it.Reason = "owned by profile " + o.Ref()
		if len(diff) > 0 {
			it.Reason += "; " + changed
		}
	case len(diff) == 0:
		it.State = core.ApplyInSync
		it.Restamp = o.Ref() != ref
	case o.Kind == core.OriginUser:
		it.State = core.ApplyConflict
		it.Reason = "owned by the project (origin user); " + changed
	case o.Kind == core.OriginLegacy:
		it.State = core.ApplyConflict
		it.Reason = fmt.Sprintf("pre-profile origin %s; %s", origin, changed)
	case o.Ref() == ref:
		// Same version, different words: only the project can have done
		// that.
		it.State = core.ApplyConflict
		it.Reason = "modified locally since " + ref + "; " + changed
	default:
		old := d.load(o.Ref())
		if old == nil {
			it.State = core.ApplyConflict
			it.Reason = fmt.Sprintf("differs from %s, which is not installed here to prove whether the edit is local; %s", o.Ref(), changed)
			break
		}
		if oldDiff, present := diffAgainst(old); present && len(oldDiff) == 0 {
			it.State = core.ApplyUpdate
			it.Reason = "unchanged since " + o.Ref() + "; profile changes " + changed
		} else {
			it.State = core.ApplyConflict
			it.Reason = "modified locally since " + o.Ref() + "; " + changed
		}
	}
	return it
}

func personaDiff(rec, doc core.Persona) []string {
	var d []string
	if strings.TrimSpace(rec.Prompt) != strings.TrimSpace(doc.Prompt) {
		d = append(d, "prompt")
	}
	if strings.TrimSpace(rec.Description) != strings.TrimSpace(doc.Description) {
		d = append(d, "description")
	}
	return d
}

func checklistDiff(rec, doc core.ChecklistRecord) []string {
	var d []string
	if strings.TrimSpace(rec.Purpose) != strings.TrimSpace(doc.Purpose) {
		d = append(d, "purpose")
	}
	if !stepsEqual(rec.Steps, doc.Steps) {
		d = append(d, "steps")
	}
	if !slices.Equal(rec.Suits, doc.Suits) {
		d = append(d, "suits")
	}
	if !slices.Equal(rec.Requires.Capabilities, doc.Requires.Capabilities) || !slices.Equal(rec.Requires.Channels, doc.Requires.Channels) {
		d = append(d, "requires")
	}
	if defaulted(rec.Target, core.ChecklistTargetProject) != defaulted(doc.Target, core.ChecklistTargetProject) {
		d = append(d, "target")
	}
	if rec.Targets != doc.Targets {
		d = append(d, "targets")
	}
	if defaulted(rec.Mode, core.ChecklistModeEager) != defaulted(doc.Mode, core.ChecklistModeEager) {
		d = append(d, "mode")
	}
	return d
}

func channelDiff(rec, doc core.ChannelRecord) []string {
	var d []string
	if strings.TrimSpace(rec.Purpose) != strings.TrimSpace(doc.Purpose) {
		d = append(d, "purpose")
	}
	if defaulted(rec.RoleHint, core.ChannelRoleHome) != defaulted(doc.RoleHint, core.ChannelRoleHome) {
		d = append(d, "role_hint")
	}
	return d
}

func defaulted(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func stepsEqual(a, b []core.ChecklistStep) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i].Text) != strings.TrimSpace(b[i].Text) || !stepsEqual(a[i].Children, b[i].Children) {
			return false
		}
	}
	return true
}

// SetupReport lists the mechanical setup a project still needs after
// apply — the questions only the project or this machine can answer, each
// with the command that answers it. It replaces the starter checklists that
// used to walk a session through the same steps.
func SetupReport(code string, cur Current, launcherConfigured bool) []core.SetupStep {
	var out []core.SetupStep
	for _, ch := range cur.Channels {
		if len(ch.Endpoints) > 0 {
			continue
		}
		out = append(out, core.SetupStep{
			Kind:    core.SetupChannelEndpoint,
			Subject: ch.Name,
			Detail:  fmt.Sprintf("channel %s has no endpoint yet — nothing can land in it", ch.Name),
			Command: fmt.Sprintf("atm channel endpoint add --project %s --name %s --type <%s> [--url|--workspace|--database|--page|--channel-id ...]",
				code, ch.Name, strings.Join(core.ChannelTypes, "|")),
		})
	}
	have := map[string]bool{}
	for _, ch := range cur.Channels {
		have[ch.Name] = true
	}
	reported := map[string]bool{}
	for _, cl := range cur.Checklists {
		for _, h := range cl.Requires.Channels {
			if have[h] || reported[h] {
				continue
			}
			reported[h] = true
			out = append(out, core.SetupStep{
				Kind:    core.SetupChannelMissing,
				Subject: h,
				Detail:  fmt.Sprintf("checklist %s requires channel %s, which no record answers to", cl.Name, h),
				Command: fmt.Sprintf("atm channel add --project %s --name %s --type <%s> ...", code, h, strings.Join(core.ChannelTypes, "|")),
			})
		}
	}
	if !launcherConfigured {
		out = append(out, core.SetupStep{
			Kind:    core.SetupLauncher,
			Detail:  "no agent launcher is selected on this machine, so nothing can be dispatched",
			Command: "atm agents select <name>",
		})
	}
	return out
}
