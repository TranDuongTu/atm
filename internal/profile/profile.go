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

// Profile is one loaded, validated profile: a manifest plus the documents
// that become project records when it is applied.
//
// The documents ARE the record types from internal/core — a profile persona
// is a core.Persona, an action is a core.ChecklistRecord, a channel
// expectation is a core.ChannelRecord. This package deliberately defines no
// parallel shapes: apply's whole job is turning these documents into project
// records, and a second set of structs would mean a mapper to keep in step
// with every field the model grows. The record fields a document cannot
// answer for itself stay zero until apply fills them: TaskID (the ledger
// identity), Origin (stamped from the manifest), and — for a channel — Type
// and Address, which are per-project, per-machine facts a portable profile
// must never carry.
//
// Document slices are name-sorted, so a profile loads identically from any
// filesystem.
type Profile struct {
	Manifest   Manifest               `json:"manifest"`
	Personas   []core.Persona         `json:"personas,omitempty"`
	Checklists []core.ChecklistRecord `json:"checklists,omitempty"`
	Channels   []core.ChannelRecord   `json:"channels,omitempty"`
}

// Persona returns the named persona.
func (p *Profile) Persona(name string) (core.Persona, bool) {
	for _, x := range p.Personas {
		if x.Name == name {
			return x, true
		}
	}
	return core.Persona{}, false
}

// Checklist returns the named checklist.
func (p *Profile) Checklist(name string) (core.ChecklistRecord, bool) {
	for _, x := range p.Checklists {
		if x.Name == name {
			return x, true
		}
	}
	return core.ChecklistRecord{}, false
}

// Channel returns the named channel expectation.
func (p *Profile) Channel(name string) (core.ChannelRecord, bool) {
	for _, x := range p.Channels {
		if x.Name == name {
			return x, true
		}
	}
	return core.ChannelRecord{}, false
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
		x.Prompt = sub(x.Prompt)
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
