package tui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"atm/internal/profile"

	tea "github.com/charmbracelet/bubbletea"
)

// The editor round-trip for checklists: the record is rendered to a temp
// document, the TUI suspends on $VISUAL/$EDITOR, and what comes back is
// parsed by the same parser `atm checklist set` and the profile loader use,
// then stored wholesale (decision 11: the document is the record). The
// launch is a tea.Cmd, never a render, so view_purity_test holds.

// checklistEditedMsg is the editor's return: which record, the document
// path, and how the editor exited. tea.ExecProcess's callback delivers it;
// tests deliver it directly — the editor itself never runs under test.
type checklistEditedMsg struct {
	name string // "" for a new checklist
	path string
	err  error
}

// checklistEdit is the round-trip in flight: the temp document and the
// bytes it started with, so an unchanged save writes nothing.
type checklistEdit struct {
	name     string
	path     string
	original []byte
}

// checklistSkeleton is what [n] opens. Every key is present so the author
// sees the whole contract; the empty name and purpose fail the parser, and
// the re-edit loop names them. No trailing spaces: an editor that trims
// them would otherwise turn an untouched skeleton into a "changed" one.
const checklistSkeleton = `---
name:
purpose:
suits: []
requires_capabilities: []
requires_channels: []
target: project
mode: eager
---
1. First step
   1. A sub-step
`

// checklistEditor resolves the external editor: $VISUAL, then $EDITOR.
func checklistEditor() string {
	for _, k := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// execEditor is the production runEditor: suspend the TUI, run the editor
// on the document, deliver the exit as a checklistEditedMsg. The editor
// value may carry arguments ("code --wait").
func execEditor(editor, path string, done func(error) tea.Msg) tea.Cmd {
	argv := strings.Fields(editor)
	c := exec.Command(argv[0], append(argv[1:], path)...)
	return tea.ExecProcess(c, done)
}

// beginEdit writes the document for name ("" = the skeleton) to a temp file
// and returns the Cmd that suspends the TUI on the editor. Without an
// editor, [e] leaves the file and names the CLI verb — the escape hatch —
// and [n] just says so, since there is no import verb for a new record.
func (p *profilesModel) beginEdit(name string) tea.Cmd {
	if p.project == "" {
		return nil
	}
	editor := checklistEditor()
	if editor == "" && name == "" {
		p.m.showToast("no $VISUAL or $EDITOR set — a new checklist needs one (or use atm checklist add)")
		return nil
	}
	doc := []byte(checklistSkeleton)
	if name != "" {
		rec, ok := p.records[name]
		if !ok {
			p.m.showToast("no record loaded for " + name)
			return nil
		}
		doc = profile.RenderChecklistDocument(rec)
	}
	stem := name
	if stem == "" {
		stem = "new"
	}
	f, err := os.CreateTemp("", "atm-checklist-"+stem+"-*.md")
	if err != nil {
		p.m.showToast("error: " + err.Error())
		return nil
	}
	path := f.Name()
	_, werr := f.Write(doc)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(path)
		p.m.showToast("error: " + werr.Error())
		return nil
	}
	if editor == "" {
		p.m.showToast(fmt.Sprintf("no $VISUAL or $EDITOR set — edit %s, then: atm checklist set --project %s --name %s --file %s", path, p.project, name, path))
		return nil
	}
	p.pending = &checklistEdit{name: name, path: path, original: doc}
	return p.launchEditor(editor, path)
}

func (p *profilesModel) launchEditor(editor, path string) tea.Cmd {
	name := p.pending.name
	done := func(err error) tea.Msg { return checklistEditedMsg{name: name, path: path, err: err} }
	if p.runEditor != nil {
		return p.runEditor(editor, path, done)
	}
	return execEditor(editor, path, done)
}

// applyEdited is the return from the editor. A message for no pending
// round-trip, or for another path, is stale and dropped.
func (p *profilesModel) applyEdited(msg checklistEditedMsg) tea.Cmd {
	pe := p.pending
	if pe == nil || pe.path != msg.path {
		return nil
	}
	if msg.err != nil {
		p.pending = nil
		p.m.showToast("editor failed: " + msg.err.Error() + " — your text is at " + msg.path)
		return nil
	}
	data, err := os.ReadFile(msg.path)
	if err != nil {
		p.pending = nil
		p.m.showToast("error: " + err.Error())
		return nil
	}
	if bytes.Equal(data, pe.original) {
		p.discardEdit()
		p.m.showToast("checklist unchanged")
		return nil
	}
	name, err := p.commitDocument(pe.name, data)
	if err != nil {
		subject := pe.name
		if subject == "" {
			subject = "new checklist"
		}
		p.m.confirm = confirmChecklistReedit
		p.m.confirmMsg = "Re-edit " + subject + "?"
		p.m.confirmArg = "The document was rejected:\n" + err.Error() + "\nEnter reopens your text; Esc discards it."
		return nil
	}
	created := pe.name == ""
	p.discardEdit()
	p.loadFor(p.project)
	p.m.refreshAll()
	if created {
		p.m.showToast("created checklist " + name)
	} else {
		p.m.showToast("set checklist " + name)
	}
	return nil
}

// commitDocument parses and stores. An existing record is set with its
// name as the stem, so a rename in the frontmatter is refused exactly as
// `atm checklist set --name` refuses it; a new record is parsed with an
// empty stem (the document's name wins) and created with origin user.
func (p *profilesModel) commitDocument(name string, data []byte) (string, error) {
	rec, err := profile.ParseChecklistDocument(name, data)
	if err != nil {
		return "", err
	}
	if name == "" {
		rec.Origin = "user"
		if _, err := p.m.store.CreateChecklist(p.project, rec, p.m.actor); err != nil {
			return "", err
		}
		return rec.Name, nil
	}
	return name, p.m.store.SetChecklist(p.project, name, rec, p.m.actor)
}

// reedit reopens the editor on the kept document (confirm: Enter).
func (p *profilesModel) reedit() tea.Cmd {
	if p.pending == nil {
		return nil
	}
	editor := checklistEditor()
	if editor == "" {
		p.m.showToast("no $VISUAL or $EDITOR set — your text is at " + p.pending.path)
		p.pending = nil
		return nil
	}
	return p.launchEditor(editor, p.pending.path)
}

// discardEdit removes the temp document and forgets the round-trip
// (confirm: Esc, and every completed path).
func (p *profilesModel) discardEdit() {
	if p.pending != nil {
		os.Remove(p.pending.path)
		p.pending = nil
	}
}
