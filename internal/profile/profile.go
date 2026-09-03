// Package profile is ATM's operating-content format: a profile is a named,
// versioned bundle of personas, checklists, and channel expectations that a
// project applies to get an operating model (DispatchV2 unit 4, ATM-bce933).
//
// The three-layer model this package sits in the middle of: a CAPABILITY is
// code — lanes, axes, verbs, invariants compiled into the binary; a PROFILE
// is named config — how a team uses those words, portable across projects
// and machines; PROJECT RECORDS are the applied state, stamped with the
// origin they came from and free to diverge afterwards.
//
// The package is deliberately low in the import graph: stdlib plus
// internal/core (a leaf) only. Validation that depends on what this build
// actually knows — which capabilities exist — is a separate method callers
// hand the registry's answer to, so nothing here imports internal/capability.
package profile

import (
	"fmt"
	"regexp"
	"strings"

	"atm/internal/core"
)

// Format is the profile format version this build reads. A profile
// declaring anything else is refused rather than half-understood.
const Format = 1

// Checklist target values: what a dispatch of this action operates on.
const (
	TargetProject = "project"
	TargetTask    = "task"
)

// Checklist mode values: the action's natural autonomy. eager sessions are
// spawned with a kickoff and execute immediately; interactive sessions
// render their context and wait for the human; resident is declarable but
// refused at launch until the runtime exists.
const (
	ModeEager       = "eager"
	ModeInteractive = "interactive"
	ModeResident    = "resident"
)

// Channel endpoint roles. A channel's role_hint says what an endpoint
// created for it should default to: home receives the content, broadcast
// receives a one-line reference.
const (
	RoleHome      = "home"
	RoleBroadcast = "broadcast"
)

// Manifest is the profile's identity and its declared prerequisites.
type Manifest struct {
	Name                 string   `json:"name"`
	Version              string   `json:"version"`
	Format               int      `json:"format"`
	Description          string   `json:"description,omitempty"`
	Authors              []string `json:"authors,omitempty"`
	RequiresCapabilities []string `json:"requires_capabilities,omitempty"`
}

// Ref is the manifest's name@version — the value stamped as a record's
// origin at apply time.
func (m Manifest) Ref() string { return m.Name + "@" + m.Version }

// Persona is one profile persona document: identity, not procedure. The
// body is carried WHOLE — the personality overlay that used to be split out
// of a persona file is pruned by this unit.
type Persona struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

// Checklist is one profile action document: the operating procedure for one
// kind of session, plus the dispatch facts that decide what it can run on.
type Checklist struct {
	Name     string                 `json:"name"`
	Purpose  string                 `json:"purpose"`
	Suits    []string               `json:"suits,omitempty"`
	Requires core.ChecklistRequires `json:"requires,omitzero"`
	// Target is what a dispatch of this action operates on: the project as a
	// whole, or one task.
	Target string `json:"target"`
	// Targets is a label expression narrowing the tasks a task-target action
	// may be dispatched on; "" offers every task. The dialog filters on it;
	// the checklist's own gate step stays as defense in depth.
	Targets string               `json:"targets,omitempty"`
	Mode    string               `json:"mode"`
	Steps   []core.ChecklistStep `json:"steps"`
}

// Channel is one profile channel expectation: a handle and what belongs in
// it. Addresses are per-project, per-machine facts and never profile
// content, so a channel document carries none.
type Channel struct {
	Name     string `json:"name"`
	RoleHint string `json:"role_hint"`
	Purpose  string `json:"purpose"`
}

// Profile is one loaded, validated profile. Document slices are name-sorted
// so a profile loads identically from any filesystem.
type Profile struct {
	Manifest   Manifest    `json:"manifest"`
	Personas   []Persona   `json:"personas,omitempty"`
	Checklists []Checklist `json:"checklists,omitempty"`
	Channels   []Channel   `json:"channels,omitempty"`
}

// Persona returns the named persona.
func (p *Profile) Persona(name string) (Persona, bool) {
	for _, x := range p.Personas {
		if x.Name == name {
			return x, true
		}
	}
	return Persona{}, false
}

// Checklist returns the named checklist.
func (p *Profile) Checklist(name string) (Checklist, bool) {
	for _, x := range p.Checklists {
		if x.Name == name {
			return x, true
		}
	}
	return Checklist{}, false
}

// Channel returns the named channel expectation.
func (p *Profile) Channel(name string) (Channel, bool) {
	for _, x := range p.Channels {
		if x.Name == name {
			return x, true
		}
	}
	return Channel{}, false
}

// Origin is the provenance stamped on records this profile creates.
func (p *Profile) Origin() Origin {
	return Origin{Kind: OriginProfile, Profile: p.Manifest.Name, Version: p.Manifest.Version}
}

// ForProject returns a deep copy with every <CODE> placeholder substituted.
// The receiver is left untouched: one loaded profile serves many projects,
// and an embedded profile is shared process-wide.
func (p *Profile) ForProject(code string) *Profile {
	sub := func(s string) string { return strings.ReplaceAll(s, "<CODE>", code) }
	out := &Profile{Manifest: p.Manifest}
	out.Manifest.RequiresCapabilities = copyStrings(p.Manifest.RequiresCapabilities)
	out.Manifest.Authors = copyStrings(p.Manifest.Authors)
	for _, x := range p.Personas {
		x.Description = sub(x.Description)
		x.Body = sub(x.Body)
		out.Personas = append(out.Personas, x)
	}
	for _, x := range p.Checklists {
		x.Purpose = sub(x.Purpose)
		x.Targets = sub(x.Targets)
		x.Suits = copyStrings(x.Suits)
		x.Requires = core.ChecklistRequires{
			Capabilities: copyStrings(x.Requires.Capabilities),
			Channels:     copyStrings(x.Requires.Channels),
		}
		x.Steps = substituteSteps(x.Steps, sub)
		out.Checklists = append(out.Checklists, x)
	}
	for _, x := range p.Channels {
		x.Purpose = sub(x.Purpose)
		out.Channels = append(out.Channels, x)
	}
	return out
}

func copyStrings(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}

func substituteSteps(in []core.ChecklistStep, sub func(string) string) []core.ChecklistStep {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.ChecklistStep, len(in))
	for i, s := range in {
		out[i] = core.ChecklistStep{Text: sub(s.Text), Children: substituteSteps(s.Children, sub)}
	}
	return out
}

// OriginKind distinguishes the three provenances a project record can have.
type OriginKind int

const (
	// OriginUser: the project authored this record itself. Reset refuses it
	// — there is no source to restore from.
	OriginUser OriginKind = iota
	// OriginProfile: applied from a profile, resettable to that profile's
	// version of the document.
	OriginProfile
	// OriginLegacy: stamped before profiles existed (shipped:atm,
	// shipped:<capability>, builtin). Readable, but names no profile to
	// reset against; callers warn rather than fail.
	OriginLegacy
)

// Origin is a parsed record provenance.
type Origin struct {
	Kind    OriginKind
	Profile string
	Version string
	// legacy holds the stored value verbatim so String round-trips a record
	// written by an older binary without rewriting it.
	legacy string
}

var (
	originNameRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9_-]*[a-z0-9])?$`)
	originSemverRe = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	originLegacyRe = regexp.MustCompile(`^shipped:[a-z0-9]([a-z0-9_-]*[a-z0-9])?$`)
)

// DevVersion is the version an unbuilt directory applies as (`--dir` mode).
const DevVersion = "dev"

// ParseOrigin reads a stored origin value.
func ParseOrigin(s string) (Origin, error) {
	switch {
	case s == "user":
		return Origin{Kind: OriginUser}, nil
	case s == "builtin" || originLegacyRe.MatchString(s):
		return Origin{Kind: OriginLegacy, legacy: s}, nil
	}
	name, version, ok := strings.Cut(s, "@")
	if !ok {
		return Origin{}, fmt.Errorf("origin %q: want user, <profile>@<version>, or a legacy shipped:* value", s)
	}
	if !originNameRe.MatchString(name) {
		return Origin{}, fmt.Errorf("origin %q: invalid profile name %q", s, name)
	}
	if !validVersion(version) {
		return Origin{}, fmt.Errorf("origin %q: version %q must be semver or %q", s, version, DevVersion)
	}
	return Origin{Kind: OriginProfile, Profile: name, Version: version}, nil
}

// String renders the value to store. Legacy origins round-trip verbatim.
func (o Origin) String() string {
	switch o.Kind {
	case OriginUser:
		return "user"
	case OriginLegacy:
		return o.legacy
	default:
		return o.Profile + "@" + o.Version
	}
}

// IsUser reports whether the project authored the record itself.
func (o Origin) IsUser() bool { return o.Kind == OriginUser }

// Ref is the profile@version this origin resets against, "" when there is
// none (user and legacy origins).
func (o Origin) Ref() string {
	if o.Kind != OriginProfile {
		return ""
	}
	return o.Profile + "@" + o.Version
}

func validVersion(v string) bool { return v == DevVersion || originSemverRe.MatchString(v) }
