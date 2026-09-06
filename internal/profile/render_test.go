package profile

import (
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"atm/internal/core"
	"atm/profiles"
)

// TestRenderChecklistDocumentRoundTripsEveryShippedChecklist: the renderer is
// the parser's inverse. If a shipped document does not survive
// parse → render → parse, the TUI's [e] would silently rewrite it.
func TestRenderChecklistDocumentRoundTripsEveryShippedChecklist(t *testing.T) {
	fsys, ok := profiles.FS("scrumban")
	if !ok {
		t.Fatal("scrumban is not embedded")
	}
	names, err := fs.Glob(fsys, "checklists/*.md")
	if err != nil || len(names) == 0 {
		t.Fatalf("glob: %v (%d files)", err, len(names))
	}
	for _, path := range names {
		src, err := fs.ReadFile(fsys, path)
		if err != nil {
			t.Fatal(err)
		}
		stem := strings.TrimSuffix(strings.TrimPrefix(path, "checklists/"), ".md")
		want, err := ParseChecklistDocument(stem, src)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		doc := RenderChecklistDocument(want)
		got, err := ParseChecklistDocument(stem, doc)
		if err != nil {
			t.Fatalf("%s: re-parse: %v\n%s", path, err, doc)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: round trip changed the record\nwant %#v\ngot  %#v\ndoc:\n%s", path, want, got, doc)
		}
		if again := RenderChecklistDocument(got); string(again) != string(doc) {
			t.Fatalf("%s: render is not stable:\n%s\n---\n%s", path, doc, again)
		}
	}
}

// TestRenderChecklistDocumentQuotesWhatTheParserWouldMisread pins the scalar
// edge cases: a colon is fine bare, a leading bracket or quote is not, and
// a newline becomes a literal block.
func TestRenderChecklistDocumentQuotesWhatTheParserWouldMisread(t *testing.T) {
	cases := []struct{ purpose, targets string }{
		{"implement: the increment", ""},
		{"[not a list]", ""},
		{"\"quoted\" at the start", ""},
		{"  padded  ", ""},
		{"first line\nsecond line", ""},
		{"|", ""},
		{"plain", "(scrum:task OR scrum:bug) AND scrum-stage:implementable"},
	}
	for _, c := range cases {
		want := core.ChecklistRecord{Name: "edge", Purpose: c.purpose, Targets: c.targets,
			Target: core.ChecklistTargetTask, Mode: core.ChecklistModeInteractive,
			Suits: []string{"developer"}, Steps: []core.ChecklistStep{{Text: "one"}}}
		if c.targets == "" {
			want.Target = core.ChecklistTargetProject
		}
		got, err := ParseChecklistDocument("edge", RenderChecklistDocument(want))
		if err != nil {
			t.Fatalf("%q: %v\n%s", c.purpose, err, RenderChecklistDocument(want))
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: want %#v\ngot %#v\n%s", c.purpose, want, got, RenderChecklistDocument(want))
		}
	}
}

// TestRenderChecklistDocumentOmitsIdentityAndEmptyLists: TaskID and Origin are
// ledger facts, not document content; an empty list is an absent key; the
// body uses the parser's own markers with numbering restarting per level.
func TestRenderChecklistDocumentOmitsIdentityAndEmptyLists(t *testing.T) {
	doc := string(RenderChecklistDocument(core.ChecklistRecord{
		TaskID: "ATM-1", Name: "bare", Origin: "scrumban@1.0.0", Purpose: "p",
		Steps: []core.ChecklistStep{{Text: "a", Children: []core.ChecklistStep{{Text: "b"}}}},
	}))
	for _, absent := range []string{"ATM-1", "origin", "suits", "requires_capabilities", "requires_channels", "targets"} {
		if strings.Contains(doc, absent) {
			t.Errorf("document must not carry %q:\n%s", absent, doc)
		}
	}
	for _, present := range []string{"name: bare", "purpose: p", "target: project", "mode: eager", "\n1. a\n", "\n   1. b\n"} {
		if !strings.Contains(doc, present) {
			t.Errorf("document missing %q:\n%s", present, doc)
		}
	}
}
