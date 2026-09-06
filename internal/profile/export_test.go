package profile

import (
	"reflect"
	"strings"
	"testing"

	"atm/internal/core"
)

// exportFixture: a project applied from the fixture profile (scrumban@1.0.0)
// whose records have drifted in every way the rule distinguishes.
// goodFiles() loads name-sorted: personas [developer, manager], checklists
// [planning, scrum-coding], channels [planning].
func exportFixture(t *testing.T) (base *core.Profile, cur Current) {
	t.Helper()
	base = mustLoad(t, goodFiles())
	ref := base.Manifest.Ref()
	sub := base.ForProject("DEMO")
	// manager: untouched since apply -> origin document (with <CODE>).
	manager := sub.Personas[1]
	manager.TaskID, manager.Origin = "T-m", ref
	// developer: prompt edited -> modified.
	developer := sub.Personas[0]
	developer.TaskID, developer.Origin, developer.Prompt = "T-d", ref, "# Persona: developer\n\nYou build things, carefully."
	// planning checklist: in sync.
	planning := sub.Checklists[0]
	planning.TaskID, planning.Origin = "T-p", ref
	// scrum-coding: origin claims a version nobody has -> unverifiable.
	coding := sub.Checklists[1]
	coding.TaskID, coding.Origin = "T-c", "scrumban@0.9.0"
	// a user-authored checklist.
	mine := core.ChecklistRecord{TaskID: "T-u", Name: "mine", Purpose: "my own", Steps: []core.ChecklistStep{{Text: "do"}}, Origin: "user", Target: "project", Mode: "eager"}
	// legacy origin reads as user.
	legacy := core.ChecklistRecord{TaskID: "T-l", Name: "legacy", Purpose: "old", Steps: []core.ChecklistStep{{Text: "do"}}, Origin: "shipped:atm", Target: "project", Mode: "eager"}
	// channel: in sync, but carrying endpoints that must not export.
	ch := sub.Channels[0]
	ch.TaskID, ch.Origin, ch.Type = "T-ch", ref, "notion"
	ch.Endpoints = []core.ChannelEndpoint{{Type: "notion", Role: core.ChannelRoleHome}}
	cur = Current{
		Enabled:    []string{"scrum", "channel", "checklist"},
		Personas:   []core.Persona{developer, manager},
		Checklists: []core.ChecklistRecord{planning, coding, mine, legacy},
		Channels:   []core.ChannelRecord{ch},
	}
	return base, cur
}

func TestExportAppliesTheSourceRule(t *testing.T) {
	base, cur := exportFixture(t)
	origin := func(ref string) *core.Profile {
		if ref == base.Manifest.Ref() {
			return base
		}
		return nil
	}
	p, notes := Export("DEMO", cur, cur.Enabled, origin, core.ProfileManifest{Name: "fork", Version: "0.1.0"})

	byName := map[string]ExportNote{}
	for _, n := range notes {
		byName[n.Kind+"/"+n.Name] = n
	}
	want := map[string]string{
		"persona/manager":        ExportFromOrigin,
		"persona/developer":      ExportModified,
		"checklist/planning":     ExportFromOrigin,
		"checklist/scrum-coding": ExportUnverifiable,
		"checklist/mine":         ExportUser,
		"checklist/legacy":       ExportUser,
		"channel/planning":       ExportFromOrigin,
	}
	for k, src := range want {
		if byName[k].Source != src {
			t.Errorf("%s: source %q, want %q", k, byName[k].Source, src)
		}
	}
	if len(notes) != len(want) {
		t.Fatalf("%d notes, want %d: %+v", len(notes), len(want), notes)
	}

	// In-sync documents come from the origin: the placeholder is back.
	m, _ := p.ProfilePersona("manager")
	if !strings.Contains(m.Prompt, "<CODE>") {
		t.Fatalf("manager prompt should be the origin's, got %q", m.Prompt)
	}
	// Modified documents are the record, literally.
	d, _ := p.ProfilePersona("developer")
	if d.Prompt != "# Persona: developer\n\nYou build things, carefully." {
		t.Fatalf("developer prompt = %q", d.Prompt)
	}
	// Nothing ledger- or machine-shaped survives.
	for _, x := range p.Personas {
		if x.TaskID != "" || x.Origin != "" {
			t.Fatalf("persona %s carries ledger fields", x.Name)
		}
	}
	for _, x := range p.Checklists {
		if x.TaskID != "" || x.Origin != "" {
			t.Fatalf("checklist %s carries ledger fields", x.Name)
		}
	}
	for _, x := range p.Channels {
		if x.TaskID != "" || x.Origin != "" || x.Type != "" || len(x.Endpoints) != 0 {
			t.Fatalf("channel %s carries machine facts", x.Name)
		}
	}
	// Manifest: identity from the caller, format from this build,
	// requires from the project (sorted); description/authors left for the author.
	wantM := core.ProfileManifest{Name: "fork", Version: "0.1.0", Format: Format, RequiresCapabilities: []string{"channel", "checklist", "scrum"}}
	if !reflect.DeepEqual(p.Manifest, wantM) {
		t.Fatalf("manifest = %+v, want %+v", p.Manifest, wantM)
	}
	// Name order, so Write is stable.
	if p.Personas[0].Name != "developer" || p.Checklists[0].Name != "legacy" {
		t.Fatal("documents are not name-sorted")
	}
	// And the whole thing writes and loads.
	files, err := Write(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(ArtifactFS(files)); err != nil {
		t.Fatalf("exported profile does not load: %v", err)
	}
}

func TestExportWithoutAnOriginResolverWritesRecords(t *testing.T) {
	_, cur := exportFixture(t)
	_, notes := Export("DEMO", cur, nil, nil, core.ProfileManifest{Name: "fork", Version: "0.1.0"})
	for _, n := range notes {
		if n.Source == ExportFromOrigin {
			t.Fatalf("%s/%s: origin source with no resolver", n.Kind, n.Name)
		}
	}
}

func TestExportOriginVersionLackingTheDocumentIsUnverifiable(t *testing.T) {
	base, cur := exportFixture(t)
	thin := *base
	thin.Personas = nil // the origin version never shipped any persona
	_, notes := Export("DEMO", cur, nil, func(string) *core.Profile { return &thin }, core.ProfileManifest{Name: "fork", Version: "0.1.0"})
	for _, n := range notes {
		if n.Kind == core.ApplyKindPersona && n.Name == "manager" && n.Source != ExportUnverifiable {
			t.Fatalf("manager: source %q, want unverifiable", n.Source)
		}
	}
}
