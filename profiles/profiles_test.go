package profiles_test

import (
	"bytes"
	"reflect"
	"slices"
	"testing"

	"atm/internal/capability"
	"atm/internal/capability/channel"
	"atm/internal/capability/checklist"
	"atm/internal/capability/codereview"
	"atm/internal/capability/qa"
	"atm/internal/capability/release"
	"atm/internal/capability/scrum"
	"atm/internal/core"
	"atm/internal/profile"
	"atm/profiles"
)

// registry mirrors the composition root in cmd/atm/main.go. The contract
// tests below judge the shipped profile against the capabilities this binary
// actually provides, so they must see the same set main does.
func registry() *capability.Registry {
	return capability.NewRegistry(
		channel.New(), checklist.New(),
		scrum.New(), qa.New(), codereview.New(), release.New(),
	)
}

func loadScrumban(t *testing.T) *core.Profile {
	t.Helper()
	fsys, ok := profiles.FS(profiles.Scrumban)
	if !ok {
		t.Fatalf("embedded profile %q not found; Names() = %v", profiles.Scrumban, profiles.Names())
	}
	p, err := profile.Load(fsys)
	if err != nil {
		t.Fatalf("scrumban does not load: %v", err)
	}
	return p
}

// The shipped profile must pass the same loader and validation a
// third-party profile does. This is what `make verify` runs in place of
// `atm profile build` on every commit.
func TestScrumbanLoadsClean(t *testing.T) {
	p := loadScrumban(t)
	if p.Manifest.Name != profiles.Scrumban {
		t.Fatalf("manifest name = %q", p.Manifest.Name)
	}
	if p.Manifest.Format != profile.Format {
		t.Fatalf("format = %d, want %d", p.Manifest.Format, profile.Format)
	}
	if p.Manifest.Version == "" || p.Manifest.Description == "" {
		t.Fatalf("manifest %+v needs a version and a description — both are what an install shows", p.Manifest)
	}
}

func TestScrumbanShipsTheDocumentedSet(t *testing.T) {
	p := loadScrumban(t)
	names := func(get func() []string) []string { out := get(); slices.Sort(out); return out }

	wantPersonas := []string{"developer", "manager", "qa", "staff"}
	got := names(func() (out []string) {
		for _, x := range p.Personas {
			out = append(out, x.Name)
		}
		return
	})
	if !reflect.DeepEqual(got, wantPersonas) {
		t.Fatalf("personas = %v, want %v", got, wantPersonas)
	}

	wantChecklists := []string{"attest", "codereview", "planning", "pr-review", "qa", "retrospect", "scrum-coding", "scrum-design", "standup"}
	got = names(func() (out []string) {
		for _, x := range p.Checklists {
			out = append(out, x.Name)
		}
		return
	})
	if !reflect.DeepEqual(got, wantChecklists) {
		t.Fatalf("checklists = %v, want %v", got, wantChecklists)
	}

	wantChannels := []string{"design", "planning", "prs", "qa", "reviews", "standup"}
	got = names(func() (out []string) {
		for _, x := range p.Channels {
			out = append(out, x.Name)
		}
		return
	})
	if !reflect.DeepEqual(got, wantChannels) {
		t.Fatalf("channels = %v, want %v", got, wantChannels)
	}
}

// This is the enforcement that replaces the registry's Duty panic: a flow
// capability the profile presupposes must come with an action that operates
// it, or enabling the capability buys the project a lane nobody works.
func TestEveryRequiredFlowCapabilityHasAnAction(t *testing.T) {
	p := loadScrumban(t)
	flows := map[string]bool{}
	for _, f := range registry().Flows() {
		flows[f.Name()] = true
	}
	for _, name := range p.Manifest.RequiresCapabilities {
		if !flows[name] {
			continue
		}
		covered := slices.ContainsFunc(p.Checklists, func(c core.ChecklistRecord) bool {
			return slices.Contains(c.Requires.Capabilities, name)
		})
		if !covered {
			t.Errorf("flow capability %q is required but no checklist operates it", name)
		}
	}
}

// The manifest may not presuppose a capability this binary does not have:
// apply enables the manifest's set, and an unknown name is a hard error
// there (plan §3.2 step 2). Catching it here means it can never ship.
func TestScrumbanRequiresOnlyKnownCapabilities(t *testing.T) {
	var known []string
	for _, n := range registry().Names() {
		known = append(known, n)
	}
	if err := profile.ValidateProfileCapabilities(loadScrumban(t), known); err != nil {
		t.Fatal(err)
	}
}

// Dispatch derives the persona from suits, and the dialog lists an action by
// its purpose. An action missing either is undispatchable or unreadable.
func TestEveryActionIsDispatchable(t *testing.T) {
	p := loadScrumban(t)
	for _, c := range p.Checklists {
		if len(c.Suits) == 0 {
			t.Errorf("checklist %s: no suits — dispatch has no persona to resolve", c.Name)
		}
		if c.Purpose == "" {
			t.Errorf("checklist %s: no purpose — the action list has nothing to show", c.Name)
		}
		if c.Target == core.ChecklistTargetTask && c.Targets == "" {
			t.Errorf("checklist %s: task-target action with no targets expression offers every task in the project", c.Name)
		}
	}
}

// A channel expectation is a handle and what belongs in it. Type and address
// are per-project, per-machine facts; a profile carrying one would be
// unportable the moment it left its author's workspace.
func TestChannelExpectationsCarryNoAddress(t *testing.T) {
	for _, ch := range loadScrumban(t).Channels {
		if ch.Type != "" || ch.Address != (core.ChannelAddress{}) {
			t.Errorf("channel %s: profile documents declare no type or address, got %+v", ch.Name, ch)
		}
		if ch.Purpose == "" {
			t.Errorf("channel %s: no purpose — nothing tells an agent what belongs here", ch.Name)
		}
	}
}

// Substitution reaches the shipped content, and the embedded profile is
// never mutated by serving one project.
func TestScrumbanSubstitutesProjectCode(t *testing.T) {
	p := loadScrumban(t)
	for _, c := range p.ForProject("ATM").Checklists {
		if c.Name == "planning" && len(c.Steps) == 0 {
			t.Fatal("planning lost its steps in ForProject")
		}
	}
	if _, ok := loadScrumban(t).ProfileChecklist("planning"); !ok {
		t.Fatal("planning missing on a fresh load — ForProject mutated the embedded profile")
	}
}

// Release CI packs and publishes this profile. Building it here means a
// commit that would break `atm profile build` fails in `make verify`
// instead of at release time — and pins that the artifact is reproducible,
// which is the whole basis for publishing a digest alongside it.
func TestScrumbanBuildsReproducibly(t *testing.T) {
	fsys, ok := profiles.FS(profiles.Scrumban)
	if !ok {
		t.Fatalf("embedded profile %q not found", profiles.Scrumban)
	}
	var first, second bytes.Buffer
	a, err := profile.Build(fsys, &first)
	if err != nil {
		t.Fatalf("scrumban does not build: %v", err)
	}
	b, err := profile.Build(fsys, &second)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest || !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatalf("two builds of the embedded profile differ: %s vs %s", a.Digest, b.Digest)
	}
	if a.Filename() != "scrumban-"+a.Version+".atmprofile" {
		t.Fatalf("Filename() = %q", a.Filename())
	}
}
