package profile

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"atm/internal/core"
)

func TestLoadManifest(t *testing.T) {
	p, err := Load(goodFiles())
	if err != nil {
		t.Fatal(err)
	}
	m := p.Manifest
	if m.Name != "scrumban" || m.Version != "1.0.0" || m.Format != 1 {
		t.Fatalf("manifest identity = %+v", m)
	}
	// The folded `>` scalar joins its lines into one paragraph.
	want := "ATM's standard operating model: weekly planning and design-gated incremental implementation."
	if m.Description != want {
		t.Fatalf("folded description = %q, want %q", m.Description, want)
	}
	if !reflect.DeepEqual(m.Authors, []string{"ATM"}) {
		t.Fatalf("authors = %v", m.Authors)
	}
	if !reflect.DeepEqual(m.RequiresCapabilities, []string{"scrum", "channel"}) {
		t.Fatalf("requires_capabilities = %v", m.RequiresCapabilities)
	}
	if got := m.Ref(); got != "scrumban@1.0.0" {
		t.Fatalf("Ref() = %q", got)
	}
}

func TestLoadPersonas(t *testing.T) {
	p, err := Load(goodFiles())
	if err != nil {
		t.Fatal(err)
	}
	// Name-sorted, so a profile loads identically on every filesystem.
	var names []string
	for _, x := range p.Personas {
		names = append(names, x.Name)
	}
	if !reflect.DeepEqual(names, []string{"developer", "manager"}) {
		t.Fatalf("persona order = %v, want name-sorted", names)
	}
	mgr, ok := p.Persona("manager")
	if !ok {
		t.Fatal("Persona(manager) not found")
	}
	if mgr.Description != "Runs the flow." {
		t.Fatalf("description = %q", mgr.Description)
	}
	// The body is carried WHOLE: no section is split out. The personality
	// overlay this unit prunes is exactly the split skills/ used to do.
	if !strings.HasPrefix(mgr.Body, "# Persona: manager") || !strings.Contains(mgr.Body, "## Principles") {
		t.Fatalf("body not carried whole:\n%s", mgr.Body)
	}
}

func TestLoadChecklists(t *testing.T) {
	p, err := Load(goodFiles())
	if err != nil {
		t.Fatal(err)
	}
	pl, ok := p.Checklist("planning")
	if !ok {
		t.Fatal("Checklist(planning) not found")
	}
	if pl.Purpose != "the weekly planning pass for <CODE>" {
		t.Fatalf("purpose = %q", pl.Purpose)
	}
	if !reflect.DeepEqual(pl.Suits, []string{"manager"}) {
		t.Fatalf("suits = %v", pl.Suits)
	}
	if !reflect.DeepEqual(pl.Requires.Capabilities, []string{"scrum", "channel"}) {
		t.Fatalf("requires capabilities = %v", pl.Requires.Capabilities)
	}
	if !reflect.DeepEqual(pl.Requires.Channels, []string{"planning"}) {
		t.Fatalf("requires channels = %v", pl.Requires.Channels)
	}
	wantSteps := []core.ChecklistStep{
		{Text: "Orient before deciding anything.", Children: []core.ChecklistStep{
			{Text: "Scan #planning over the last week."},
			{Text: "Scan #standup since the previous plan."},
		}},
		{Text: "Sweep each enabled flow capability."},
	}
	if !reflect.DeepEqual(pl.Steps, wantSteps) {
		t.Fatalf("steps = %#v", pl.Steps)
	}
}

func TestLoadChecklistTargetTargetsMode(t *testing.T) {
	p, err := Load(goodFiles())
	if err != nil {
		t.Fatal(err)
	}
	sc, _ := p.Checklist("scrum-coding")
	if sc.Target != TargetTask {
		t.Fatalf("target = %q, want task", sc.Target)
	}
	if sc.Mode != ModeInteractive {
		t.Fatalf("mode = %q, want interactive", sc.Mode)
	}
	if !strings.Contains(sc.Targets, "scrum-stage:implementable") {
		t.Fatalf("targets = %q", sc.Targets)
	}
}

// Defaults are the plan's: target project, mode eager. A checklist that
// declares neither is the common case and must not need boilerplate.
func TestLoadChecklistDefaults(t *testing.T) {
	fsys := withFile("checklists/standup.md", `---
name: standup
purpose: the daily heartbeat
suits: [manager]
---
1. Establish the window.
`)
	p, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	su, _ := p.Checklist("standup")
	if su.Target != TargetProject {
		t.Fatalf("default target = %q, want project", su.Target)
	}
	if su.Mode != ModeEager {
		t.Fatalf("default mode = %q, want eager", su.Mode)
	}
	if su.Targets != "" {
		t.Fatalf("default targets = %q, want empty", su.Targets)
	}
}

func TestLoadChannels(t *testing.T) {
	p, err := Load(goodFiles())
	if err != nil {
		t.Fatal(err)
	}
	ch, ok := p.Channel("planning")
	if !ok {
		t.Fatal("Channel(planning) not found")
	}
	if ch.RoleHint != RoleHome {
		t.Fatalf("role_hint = %q", ch.RoleHint)
	}
	if !strings.Contains(ch.Purpose, "The weekly plan and its discussion") {
		t.Fatalf("purpose = %q", ch.Purpose)
	}
}

// A channel document declares NO address: endpoints are per-project,
// per-machine facts, never profile content (plan §3.1).
func TestLoadChannelRejectsAddress(t *testing.T) {
	wantLoadErr(t, withFile("channels/planning.md", `---
name: planning
role_hint: home
address: https://slack.com/x
---
purpose text
`), "address")
}

func TestLoadChannelRoleHintDefaultsHome(t *testing.T) {
	p, err := Load(withFile("channels/prs.md", "---\nname: prs\n---\nThe PR log.\n"))
	if err != nil {
		t.Fatal(err)
	}
	ch, _ := p.Channel("prs")
	if ch.RoleHint != RoleHome {
		t.Fatalf("role_hint = %q, want the home default", ch.RoleHint)
	}
}

// Non-markdown files in a document directory are ignored, so a profile
// directory may carry a README or an editor artefact.
func TestLoadIgnoresNonMarkdown(t *testing.T) {
	fsys := withFile("checklists/README.txt", "notes for maintainers")
	if _, err := Load(fsys); err != nil {
		t.Fatal(err)
	}
}

// An entirely absent optional directory is legal: an extension profile may
// ship only channels, or only checklists.
func TestLoadAllowsMissingDirectories(t *testing.T) {
	fsys := fstest.MapFS{
		"manifest.yaml": &fstest.MapFile{Data: []byte("name: bare\nversion: 0.1.0\nformat: 1\n")},
	}
	p, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Personas)+len(p.Checklists)+len(p.Channels) != 0 {
		t.Fatalf("bare profile carries documents: %+v", p)
	}
}

func TestLoadRejectsMissingManifest(t *testing.T) {
	wantLoadErr(t, withoutFile("manifest.yaml"), "manifest.yaml")
}
