package profile

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"atm/internal/core"
	"atm/profiles"
)

// loadFiles is Load over an in-memory file set.
func loadFiles(t *testing.T, files map[string][]byte) *core.Profile {
	t.Helper()
	p, err := Load(ArtifactFS(files))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return p
}

func mustLoad(t *testing.T, fsys fstest.MapFS) *core.Profile {
	t.Helper()
	p, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWriteRoundTripsTheScrumbanSource(t *testing.T) {
	fsys, ok := profiles.FS(profiles.Scrumban)
	if !ok {
		t.Fatal("scrumban is not embedded")
	}
	p1, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Write(p1)
	if err != nil {
		t.Fatal(err)
	}
	p2 := loadFiles(t, files)
	if !reflect.DeepEqual(p1, p2) {
		t.Fatalf("round trip changed the profile:\n got %+v\nwant %+v", p2, p1)
	}
}

func TestWriteRoundTripsTheFixture(t *testing.T) {
	p1 := mustLoad(t, goodFiles())
	files, err := Write(p1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p1, loadFiles(t, files)) {
		t.Fatal("round trip changed the fixture profile")
	}
	// Placeholders are content, not something the writer resolves.
	if !strings.Contains(string(files["channels/planning.md"]), "<CODE>") {
		t.Fatal("writer lost the <CODE> placeholder")
	}
}

// everyField exercises each optional field and each value the encoders
// must quote or block: colons, brackets, a bare `>`, a multi-line
// description, launch/project_optional, targets, requires_channels.
func everyField() *core.Profile {
	return &core.Profile{
		Manifest: core.ProfileManifest{
			Name: "every", Version: "0.1.0", Format: Format,
			Description:          "first paragraph: with a colon\n\nsecond paragraph",
			Authors:              []string{"a", "b"},
			RequiresCapabilities: []string{"scrum", "channel"},
		},
		Personas: []core.Persona{{
			Name: "ops", Description: "[looks like a list]", Prompt: "# Persona: ops\n\nBody with a `code` span.",
			Launch: "tui", ProjectOptional: true,
		}},
		Checklists: []core.ChecklistRecord{{
			Name: "act", Purpose: ">", Suits: []string{"ops"},
			Requires: core.ChecklistRequires{Capabilities: []string{"scrum"}, Channels: []string{"design"}},
			Target:   core.ChecklistTargetTask, Targets: "(<CODE>:scrum:task) AND <CODE>:scrum-stage:implementable",
			Mode:     core.ChecklistModeInteractive,
			Steps: []core.ChecklistStep{
				{Text: "Top: one", Children: []core.ChecklistStep{
					{Text: "child 1.1", Children: []core.ChecklistStep{{Text: "grandchild 1.1.1"}}},
					{Text: "- looks like a dash item"},
				}},
				{Text: "Top two"},
			},
		}},
		Channels: []core.ChannelRecord{{Name: "design", RoleHint: core.ChannelRoleBroadcast, Purpose: "Specs: here.\n\nSecond paragraph."}},
	}
}

func TestWriteRoundTripsEveryField(t *testing.T) {
	p1 := everyField()
	files, err := Write(p1)
	if err != nil {
		t.Fatal(err)
	}
	p2 := loadFiles(t, files)
	if !reflect.DeepEqual(p1, p2) {
		t.Fatalf("round trip changed the profile:\n got %+v\nwant %+v", p2, p1)
	}
}

// The written form is a fixed point: writing what was loaded from a write
// yields the same bytes. That is what keeps `git diff` after a re-export
// quiet when nothing changed.
func TestWriteIsAFixedPoint(t *testing.T) {
	for name, p := range map[string]*core.Profile{"fixture": mustLoad(t, goodFiles()), "every": everyField()} {
		a, err := Write(p)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Write(loadFiles(t, a))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("%s: second write differs from the first", name)
		}
	}
}

func TestWriteLeavesOutLedgerAndMachineFacts(t *testing.T) {
	p := mustLoad(t, goodFiles())
	p.Personas[0].TaskID, p.Personas[0].Origin = "T-1", "scrumban@1.0.0"
	p.Checklists[0].TaskID, p.Checklists[0].Origin = "T-2", "user"
	p.Channels[0].TaskID, p.Channels[0].Origin = "T-3", "scrumban@1.0.0"
	p.Channels[0].Type, p.Channels[0].Endpoints = "notion", []core.ChannelEndpoint{{Type: "notion", Role: core.ChannelRoleHome}}
	files, err := Write(p)
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range files {
		for _, leak := range []string{"T-1", "T-2", "T-3", "origin", "task_id", "notion", "endpoint"} {
			if strings.Contains(string(b), leak) {
				t.Fatalf("%s leaks %q:\n%s", name, leak, b)
			}
		}
	}
}

func TestWriteRefusesWhatTheFormatCannotSay(t *testing.T) {
	cases := map[string]func(p *core.Profile){
		"bad name":        func(p *core.Profile) { p.Personas[0].Name = "Bad Name" },
		"multi-line step": func(p *core.Profile) { p.Checklists[0].Steps[0].Text = "one\ntwo" },
		"empty step":      func(p *core.Profile) { p.Checklists[0].Steps[0].Text = "   " },
		"both quotes":     func(p *core.Profile) { p.Checklists[0].Purpose = `[a "b" 'c']` },
	}
	for name, mutate := range cases {
		p := mustLoad(t, goodFiles())
		mutate(p)
		if _, err := Write(p); err == nil {
			t.Errorf("%s: Write accepted it", name)
		}
	}
}

func TestWriteManifestPlaceholders(t *testing.T) {
	p := &core.Profile{Manifest: core.ProfileManifest{Name: "bare", Version: "0.1.0", Format: Format}}
	files, err := Write(p)
	if err != nil {
		t.Fatal(err)
	}
	m := string(files["manifest.yaml"])
	for _, want := range []string{"name: bare\n", "version: 0.1.0\n", "format: 1\n", "description:\n", "authors: []\n", "requires_capabilities: []\n"} {
		if !strings.Contains(m, want) {
			t.Fatalf("manifest lacks %q:\n%s", want, m)
		}
	}
	if len(files) != 1 {
		t.Fatalf("a document-less profile is one file, got %d", len(files))
	}
	if _, err := LoadManifest(ArtifactFS(files)); err != nil {
		t.Fatalf("placeholder manifest does not load: %v", err)
	}
}

func TestStaleDocumentsListsOnlyOwnedMarkdown(t *testing.T) {
	existing := fstest.MapFS{
		"manifest.yaml":          &fstest.MapFile{Data: []byte("x")},
		"README.md":              &fstest.MapFile{Data: []byte("keep")},
		"personas/keep.md":       &fstest.MapFile{Data: []byte("x")},
		"personas/old.md":        &fstest.MapFile{Data: []byte("x")},
		"personas/notes.txt":     &fstest.MapFile{Data: []byte("x")},
		"checklists/gone.md":     &fstest.MapFile{Data: []byte("x")},
		"channels/sub/nested.md": &fstest.MapFile{Data: []byte("x")},
	}
	files := map[string][]byte{"manifest.yaml": nil, "personas/keep.md": nil}
	got := StaleDocuments(existing, files)
	want := []string{"checklists/gone.md", "personas/old.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stale = %v, want %v", got, want)
	}
	if got := StaleDocuments(fstest.MapFS{}, files); got != nil {
		t.Fatalf("empty dir: stale = %v, want nil", got)
	}
}
