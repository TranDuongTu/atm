package profile

import (
	"strings"
	"testing"

	"atm/internal/core"
)

func TestValidateManifestName(t *testing.T) {
	wantLoadErr(t, withFile("manifest.yaml", "name: Scrum Ban\nversion: 1.0.0\nformat: 1\n"), "name")
}

func TestValidateManifestVersionMustBeSemver(t *testing.T) {
	wantLoadErr(t, withFile("manifest.yaml", "name: scrumban\nversion: 1.0\nformat: 1\n"), "version")
}

// --dir dev mode applies an unbuilt directory as <name>@dev (plan §3.2), so
// "dev" is the one non-semver version the format accepts.
func TestValidateManifestVersionAcceptsDev(t *testing.T) {
	fsys := withFile("manifest.yaml", "name: scrumban\nversion: dev\nformat: 1\nrequires_capabilities: [scrum, channel]\n")
	p, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if p.Manifest.Ref() != "scrumban@dev" {
		t.Fatalf("Ref() = %q", p.Manifest.Ref())
	}
}

// An unreadable format is a hard stop naming both numbers: a newer profile
// must not be half-applied by an older binary.
func TestValidateManifestFormat(t *testing.T) {
	fsys := withFile("manifest.yaml", "name: scrumban\nversion: 1.0.0\nformat: 99\n")
	_, err := Load(fsys)
	if err == nil {
		t.Fatal("Load accepted format 99")
	}
	if !strings.Contains(err.Error(), "99") || !strings.Contains(err.Error(), "1") {
		t.Fatalf("error %v must name the profile's format and this build's", err)
	}
}

func TestValidateFrontmatterNameMatchesFilename(t *testing.T) {
	wantLoadErr(t, withFile("checklists/planning.md", `---
name: retrospect
purpose: p
---
1. step
`), "filename")
}

// A suits entry naming nobody makes a checklist undispatchable: the profile
// is the closed world that must answer it.
func TestValidateSuitsMustNameAProfilePersona(t *testing.T) {
	wantLoadErr(t, withFile("checklists/planning.md", `---
name: planning
purpose: p
suits: [ghost]
target: project
---
1. step
`), "ghost")
}

func TestValidateRequiresChannelsMustExist(t *testing.T) {
	wantLoadErr(t, withFile("checklists/planning.md", `---
name: planning
purpose: p
suits: [manager]
requires_channels: [nowhere]
---
1. step
`), "nowhere")
}

// A checklist may not require a capability the profile itself does not
// declare: apply enables the manifest's set, so anything beyond it is a hole
// the profile can never fill.
func TestValidateRequiresCapabilitiesSubsetOfManifest(t *testing.T) {
	wantLoadErr(t, withFile("checklists/planning.md", `---
name: planning
purpose: p
suits: [manager]
requires_capabilities: [release]
---
1. step
`), "release")
}

func TestValidateEmptyStepTree(t *testing.T) {
	wantLoadErr(t, withFile("checklists/planning.md", `---
name: planning
purpose: p
suits: [manager]
---
Prose with no numbered or dashed step at all.
`), "step")
}

func TestValidateChecklistPurposeRequired(t *testing.T) {
	wantLoadErr(t, withFile("checklists/planning.md", "---\nname: planning\n---\n1. step\n"), "purpose")
}

func TestValidatePersonaDescriptionRequired(t *testing.T) {
	wantLoadErr(t, withFile("personas/manager.md", "---\nname: manager\n---\nbody\n"), "description")
}

func TestValidatePersonaBodyRequired(t *testing.T) {
	wantLoadErr(t, withFile("personas/manager.md", "---\nname: manager\ndescription: d\n---\n\n"), "body")
}

func TestValidateTargetValue(t *testing.T) {
	wantLoadErr(t, withFile("checklists/planning.md", `---
name: planning
purpose: p
suits: [manager]
target: repository
---
1. step
`), "target")
}

// A targets expression on a project-target checklist has nothing to filter:
// it is a mistake, not a harmless extra.
func TestValidateTargetsRequiresTaskTarget(t *testing.T) {
	wantLoadErr(t, withFile("checklists/planning.md", `---
name: planning
purpose: p
suits: [manager]
target: project
targets: "<CODE>:scrum:task"
---
1. step
`), "targets")
}

func TestValidateModeValue(t *testing.T) {
	wantLoadErr(t, withFile("checklists/planning.md", `---
name: planning
purpose: p
suits: [manager]
mode: daemon
---
1. step
`), "mode")
}

// resident is in the vocabulary but refused at launch (plan §3.7); the
// format accepts it so the enum is declarable before the runtime exists.
func TestValidateModeResidentParses(t *testing.T) {
	p, err := Load(withFile("checklists/planning.md", `---
name: planning
purpose: p
suits: [manager]
mode: resident
---
1. step
`))
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := p.Checklist("planning")
	if pl.Mode != core.ChecklistModeResident {
		t.Fatalf("mode = %q", pl.Mode)
	}
}

func TestValidateChannelRoleHint(t *testing.T) {
	wantLoadErr(t, withFile("channels/planning.md", "---\nname: planning\nrole_hint: shouty\n---\npurpose\n"), "role_hint")
}

// Every failure in one load is reported together: `profile build` must not
// make an author fix documents one round-trip at a time.
func TestValidateReportsEveryFailureAtOnce(t *testing.T) {
	fsys := withFile("checklists/planning.md", `---
name: planning
purpose: p
suits: [ghost]
requires_channels: [nowhere]
mode: daemon
---
1. step
`)
	_, err := Load(fsys)
	if err == nil {
		t.Fatal("Load succeeded")
	}
	for _, want := range []string{"ghost", "nowhere", "mode"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v omits %q — failures must be collected, not first-wins", err, want)
		}
	}
}

// The environment check is separate from Load so the package stays below
// internal/capability in the import graph; apply calls it with the registry.
func TestValidateCapabilities(t *testing.T) {
	p, err := Load(goodFiles())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ValidateCapabilities([]string{"scrum", "channel", "qa"}); err != nil {
		t.Fatalf("all declared capabilities known, got %v", err)
	}
	err = p.ValidateCapabilities([]string{"scrum"})
	if err == nil || !strings.Contains(err.Error(), "channel") {
		t.Fatalf("err = %v, want it to name the unknown capability", err)
	}
}
