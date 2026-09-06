package tui

import (
	"errors"
	"os"
	"strings"
	"testing"

	"atm/internal/profile"

	tea "github.com/charmbracelet/bubbletea"
)

// stubEditor replaces the real ExecProcess with a recorder: the editor is
// never run under test; the test edits the file itself and delivers the
// checklistEditedMsg by hand, exactly as the callback would.
func stubEditor(m *Model) *[]string {
	calls := &[]string{}
	m.profilesOv.runEditor = func(editor, path string, done func(error) tea.Msg) tea.Cmd {
		*calls = append(*calls, editor+" "+path)
		return nil
	}
	return calls
}

func openProfilesOn(t *testing.T, m *Model, name string) {
	t.Helper()
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	for i, a := range m.profilesOv.actions() {
		if a.Name == name {
			m.profilesOv.cursor = i
			return
		}
	}
	t.Fatalf("no action %s", name)
}

func keys(m *Model, ks ...string) {
	for _, k := range ks {
		switch k {
		case "enter":
			m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		case "esc":
			m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
		default:
			m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		}
	}
}

func TestChecklistEditorPrefersVisualThenEditor(t *testing.T) {
	t.Setenv("VISUAL", "vim")
	t.Setenv("EDITOR", "nano")
	if got := checklistEditor(); got != "vim" {
		t.Fatalf("got %q, want vim", got)
	}
	t.Setenv("VISUAL", "")
	if got := checklistEditor(); got != "nano" {
		t.Fatalf("got %q, want nano", got)
	}
	t.Setenv("EDITOR", " ")
	if got := checklistEditor(); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// TestProfilesOverlayEditRendersTheRecordAndRunsTheEditor: [e] writes the
// record's document — byte-identical to the renderer's output — and hands
// the path to the editor with its own arguments intact.
func TestProfilesOverlayEditRendersTheRecordAndRunsTheEditor(t *testing.T) {
	t.Setenv("VISUAL", "fake-editor --wait")
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)
	seedScrumbanChecklist(t, m, "planning")
	calls := stubEditor(m)
	openProfilesOn(t, m, "planning")
	keys(m, "e")
	t.Cleanup(m.profilesOv.discardEdit)

	if len(*calls) != 1 || !strings.HasPrefix((*calls)[0], "fake-editor --wait ") {
		t.Fatalf("editor calls = %v", *calls)
	}
	pe := m.profilesOv.pending
	if pe == nil || pe.name != "planning" {
		t.Fatalf("pending = %+v", pe)
	}
	data, err := os.ReadFile(pe.path)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := m.store.GetChecklist("ATM", "planning")
	if err != nil {
		t.Fatal(err)
	}
	if want := profile.RenderChecklistDocument(*rec); string(data) != string(want) {
		t.Fatalf("document:\n%s\nwant:\n%s", data, want)
	}
}

// TestProfilesOverlayEditWithoutAnEditorToastsTheCommand: no editor is not
// an error — the document is written anyway and the CLI verb is named.
func TestProfilesOverlayEditWithoutAnEditorToastsTheCommand(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)
	seedScrumbanChecklist(t, m, "planning")
	before, _ := m.store.StoreStats("ATM")
	openProfilesOn(t, m, "planning")
	keys(m, "e")
	if !strings.Contains(m.toastMsg, "atm checklist set --project ATM --name planning --file ") {
		t.Fatalf("toast = %q", m.toastMsg)
	}
	if m.profilesOv.pending != nil {
		t.Fatal("nothing is pending without an editor")
	}
	if after, _ := m.store.StoreStats("ATM"); after.EventCount != before.EventCount {
		t.Fatal("must write nothing")
	}
}

func TestProfilesOverlayEditedUnchangedWritesNothing(t *testing.T) {
	t.Setenv("VISUAL", "fake-editor")
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)
	seedScrumbanChecklist(t, m, "planning")
	stubEditor(m)
	openProfilesOn(t, m, "planning")
	keys(m, "e")
	path := m.profilesOv.pending.path
	before, _ := m.store.StoreStats("ATM")

	m.Update(checklistEditedMsg{name: "planning", path: path})
	if after, _ := m.store.StoreStats("ATM"); after.EventCount != before.EventCount {
		t.Fatal("an unchanged document must write nothing")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp document must be removed, stat: %v", err)
	}
	if m.profilesOv.pending != nil || !strings.Contains(m.toastMsg, "unchanged") {
		t.Fatalf("pending=%v toast=%q", m.profilesOv.pending, m.toastMsg)
	}
}

// TestProfilesOverlayEditedDocumentIsSet: the edited document replaces the
// record wholesale through SetChecklist — origin survives, the overlay
// reloads, and the record now reads as modified against its origin.
func TestProfilesOverlayEditedDocumentIsSet(t *testing.T) {
	t.Setenv("VISUAL", "fake-editor")
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)
	seedScrumbanChecklist(t, m, "planning")
	stubEditor(m)
	openProfilesOn(t, m, "planning")
	keys(m, "e")
	path := m.profilesOv.pending.path
	data, _ := os.ReadFile(path)
	edited := strings.Replace(string(data), "purpose: ", "purpose: edited in the editor — ", 1)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	m.Update(checklistEditedMsg{name: "planning", path: path})
	rec, err := m.store.GetChecklist("ATM", "planning")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rec.Purpose, "edited in the editor") || rec.Origin != "scrumban@1.0.0" {
		t.Fatalf("record = %+v", rec)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temp document must be removed after a successful set")
	}
	if m.profilesOv.syncs["planning"].State != "modified" || !strings.Contains(m.toastMsg, "set checklist planning") {
		t.Fatalf("syncs=%+v toast=%q", m.profilesOv.syncs["planning"], m.toastMsg)
	}
}

// TestProfilesOverlayRejectedDocumentOffersReedit: a parse failure keeps the
// user's text. Enter reopens the SAME file; Esc discards it.
func TestProfilesOverlayRejectedDocumentOffersReedit(t *testing.T) {
	t.Setenv("VISUAL", "fake-editor")
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)
	seedScrumbanChecklist(t, m, "planning")
	calls := stubEditor(m)
	openProfilesOn(t, m, "planning")
	keys(m, "e")
	path := m.profilesOv.pending.path
	if err := os.WriteFile(path, []byte("not a document\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m.Update(checklistEditedMsg{name: "planning", path: path})
	if m.confirm != confirmChecklistReedit || !strings.Contains(m.confirmArg, "frontmatter") {
		t.Fatalf("confirm=%v arg=%q", m.confirm, m.confirmArg)
	}
	if view := m.View(); !strings.Contains(view, "Re-edit planning?") {
		t.Fatalf("the confirm must paint over the overlay:\n%s", view)
	}
	keys(m, "enter")
	if len(*calls) != 2 || !strings.HasSuffix((*calls)[1], path) {
		t.Fatalf("Enter must reopen the same file: %v", *calls)
	}
	if m.confirm != confirmNone {
		t.Fatal("confirm must close on Enter")
	}

	m.Update(checklistEditedMsg{name: "planning", path: path})
	keys(m, "esc")
	if m.profilesOv.pending != nil {
		t.Fatal("Esc must discard the round-trip")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Esc must remove the temp document")
	}
	if !m.profilesOv.open {
		t.Fatal("the overlay stays open after a discarded edit")
	}
}

// TestProfilesOverlayEditRefusesARename: the stem pins the name, as `atm
// checklist set --name` does — a renamed frontmatter enters the re-edit
// loop instead of silently editing another record.
func TestProfilesOverlayEditRefusesARename(t *testing.T) {
	t.Setenv("VISUAL", "fake-editor")
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)
	seedScrumbanChecklist(t, m, "planning")
	stubEditor(m)
	openProfilesOn(t, m, "planning")
	keys(m, "e")
	path := m.profilesOv.pending.path
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "name: planning", "name: renamed", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Update(checklistEditedMsg{name: "planning", path: path})
	if m.confirm != confirmChecklistReedit || !strings.Contains(m.confirmArg, "must match") {
		t.Fatalf("confirm=%v arg=%q", m.confirm, m.confirmArg)
	}
	m.profilesOv.discardEdit()
}
