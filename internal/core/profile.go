package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// A PROFILE is a named, versioned bundle of operating content — personas,
// checklists, and channel expectations — that a project applies to get an
// operating model (DispatchV2 unit 4, ATM-bce933).
//
// The three-layer model: a CAPABILITY is code (lanes, axes, verbs compiled
// into the binary); a PROFILE is named config (how a team uses those words,
// portable across projects and machines); PROJECT RECORDS are the applied
// state, stamped with the origin they came from and free to diverge.
//
// The types here are the DATA and its vocabulary. Reading the on-disk
// format is internal/profile's job; keeping installed copies is
// internal/store's.

// ProfileManifest is a profile's identity and its declared prerequisites.
type ProfileManifest struct {
	Name                 string   `json:"name"`
	Version              string   `json:"version"`
	Format               int      `json:"format"`
	Description          string   `json:"description,omitempty"`
	Authors              []string `json:"authors,omitempty"`
	RequiresCapabilities []string `json:"requires_capabilities,omitempty"`
}

// Ref is the manifest's name@version — the value stamped as a record's
// origin at apply time.
func (m ProfileManifest) Ref() string { return m.Name + "@" + m.Version }

// Profile is one loaded profile: a manifest plus the documents that become
// project records when it is applied.
//
// The documents ARE the record types: a profile persona is a Persona, an
// action is a ChecklistRecord, a channel expectation is a ChannelRecord.
// There are no parallel document shapes — apply's whole job is turning
// these into project records, and a second set of structs would mean a
// mapper to keep in step with every field the model grows. The fields a
// document cannot answer for itself stay zero until apply fills them:
// TaskID (the ledger identity), Origin (stamped from the manifest), and —
// for a channel — Type and Address, which are per-project, per-machine
// facts a portable profile must never carry.
//
// Document slices are name-sorted, so a profile loads identically from any
// filesystem.
type Profile struct {
	Manifest   ProfileManifest   `json:"manifest"`
	Personas   []Persona         `json:"personas,omitempty"`
	Checklists []ChecklistRecord `json:"checklists,omitempty"`
	Channels   []ChannelRecord   `json:"channels,omitempty"`
}

// ProfilePersona returns the named persona.
func (p *Profile) ProfilePersona(name string) (Persona, bool) {
	for _, x := range p.Personas {
		if x.Name == name {
			return x, true
		}
	}
	return Persona{}, false
}

// ProfileChecklist returns the named checklist.
func (p *Profile) ProfileChecklist(name string) (ChecklistRecord, bool) {
	for _, x := range p.Checklists {
		if x.Name == name {
			return x, true
		}
	}
	return ChecklistRecord{}, false
}

// ProfileChannel returns the named channel expectation.
func (p *Profile) ProfileChannel(name string) (ChannelRecord, bool) {
	for _, x := range p.Channels {
		if x.Name == name {
			return x, true
		}
	}
	return ChannelRecord{}, false
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
		x.Requires = ChecklistRequires{
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

func substituteSteps(in []ChecklistStep, sub func(string) string) []ChecklistStep {
	if len(in) == 0 {
		return nil
	}
	out := make([]ChecklistStep, len(in))
	for i, s := range in {
		out[i] = ChecklistStep{Text: sub(s.Text), Children: substituteSteps(s.Children, sub)}
	}
	return out
}

// ProfileEntry is one profile available on this machine: installed under
// the store, or embedded in the binary.
type ProfileEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	// Digest of the artifact this version was installed from; empty for an
	// embedded profile, which has no artifact.
	Digest string `json:"digest,omitempty"`
	// Embedded marks a profile served from the binary rather than from disk.
	Embedded bool `json:"embedded"`
	// Path is the version directory on disk; empty when embedded.
	Path        string `json:"path,omitempty"`
	InstalledAt string `json:"installed_at,omitempty"`
}

// Ref is the entry's name@version.
func (e ProfileEntry) Ref() string { return e.Name + "@" + e.Version }

// DevVersion is the version an unbuilt directory applies as (`--dir` mode).
const DevVersion = "dev"

var (
	profileNameRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9_-]*[a-z0-9])?$`)
	profileSemverRe = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	profileLegacyRe = regexp.MustCompile(`^shipped:[a-z0-9]([a-z0-9_-]*[a-z0-9])?$`)
	versionPartRe   = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?`)
)

// ValidProfileName reports whether n is a legal profile name.
func ValidProfileName(n string) bool { return profileNameRe.MatchString(n) }

// ValidProfileVersion accepts semver, plus the dev version --dir mode uses.
func ValidProfileVersion(v string) bool {
	return v == DevVersion || profileSemverRe.MatchString(v)
}

// CompareProfileVersions orders two semver versions: -1, 0, or 1. A
// prerelease sorts BELOW its release (1.0.0-rc1 < 1.0.0), per semver.
// Anything unparseable sorts below everything parseable, so a hand-made
// directory name can never shadow a real version.
func CompareProfileVersions(a, b string) int {
	am, bm := versionPartRe.FindStringSubmatch(a), versionPartRe.FindStringSubmatch(b)
	switch {
	case am == nil && bm == nil:
		return strings.Compare(a, b)
	case am == nil:
		return -1
	case bm == nil:
		return 1
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(am[i])
		y, _ := strconv.Atoi(bm[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case am[4] == bm[4]:
		return 0
	case am[4] == "":
		return 1
	case bm[4] == "":
		return -1
	}
	return strings.Compare(am[4], bm[4])
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

// ParseOrigin reads a stored origin value.
func ParseOrigin(s string) (Origin, error) {
	switch {
	case s == "user":
		return Origin{Kind: OriginUser}, nil
	case s == "builtin" || profileLegacyRe.MatchString(s):
		return Origin{Kind: OriginLegacy, legacy: s}, nil
	}
	name, version, ok := strings.Cut(s, "@")
	if !ok {
		return Origin{}, fmt.Errorf("origin %q: want user, <profile>@<version>, or a legacy shipped:* value", s)
	}
	if !ValidProfileName(name) {
		return Origin{}, fmt.Errorf("origin %q: invalid profile name %q", s, name)
	}
	if !ValidProfileVersion(version) {
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
