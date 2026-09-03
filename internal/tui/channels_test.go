package tui

import (
	"strings"
	"testing"
	"time"

	"atm/internal/core"

	tea "github.com/charmbracelet/bubbletea"
)

// seedChannels gives the model a project with two channels: a repo channel
// wired to a real directory and an unwired notion channel. Returns the repo
// channel's local path.
func seedChannels(t *testing.T, m *Model) string {
	t.Helper()
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	dir := t.TempDir()
	repo := core.ChannelRecord{
		Name:    "code",
		Type:    core.ChannelTypeRepo,
		Purpose: "product source",
		Address: core.ChannelAddress{URL: "git@example.com:acme/code.git"},
	}
	if _, err := m.store.CreateChannel("ATM", repo, testActor); err != nil {
		t.Fatalf("CreateChannel code: %v", err)
	}
	if err := m.store.SetChannelWiring("ATM", "code", "", dir, "", testActor); err != nil {
		t.Fatalf("SetChannelWiring code: %v", err)
	}
	notion := core.ChannelRecord{
		Name:    "specs",
		Type:    core.ChannelTypeNotion,
		Purpose: "spec database",
		Address: core.ChannelAddress{Workspace: "acme-hq", Database: "db-42"},
	}
	if _, err := m.store.CreateChannel("ATM", notion, testActor); err != nil {
		t.Fatalf("CreateChannel specs: %v", err)
	}
	m.refreshAll()
	return dir
}

func TestChannelsOverlayListsAndCloses(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	if !m.channelsOv.open {
		t.Fatal("E must open the channels overlay")
	}
	if m.channelsOv.project != "ATM" {
		t.Fatalf("overlay project = %q, want ATM", m.channelsOv.project)
	}
	view := m.channelsOv.renderOverlay()
	for _, want := range []string{"code", "specs", "repo", "notion"} {
		if !strings.Contains(view, want) {
			t.Errorf("list missing %q:\n%s", want, view)
		}
	}

	// Entries sort by name: code, specs. j then Enter opens specs' detail.
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.channelsOv.detail {
		t.Fatal("enter must open the detail view")
	}
	detail := m.channelsOv.renderOverlay()
	for _, want := range []string{"specs", "spec database", "acme-hq", "db-42"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail missing %q:\n%s", want, detail)
		}
	}

	// Esc: detail -> list -> closed.
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.channelsOv.detail || !m.channelsOv.open {
		t.Fatal("first esc must return to the list")
	}
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.channelsOv.open {
		t.Fatal("second esc must close the overlay")
	}
}

// TestChannelsOverlayDetailShowsWiringAndStamps: the repo channel's detail
// pane must show what a concierge needs to fix it — the local wiring path,
// the verification stamps, and the probe.
func TestChannelsOverlayDetailShowsWiringAndStamps(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	dir := seedChannels(t, m)
	if err := m.store.AddChannelStamp("ATM", "code", "", core.StampKindUse, "cloned and built", testActor); err != nil {
		t.Fatalf("AddChannelStamp: %v", err)
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	detail := m.channelsOv.renderOverlay()
	// The stamp row now names its kind too, so a long note wraps; assert on
	// the pieces rather than one pre-wrap line.
	for _, want := range []string{"product source", "code.git", "path", "endpoint", "cloned and", testActor, core.StampKindUse, "probe"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail missing %q:\n%s", want, detail)
		}
	}

	// The wiring path is the field a concierge acts on: it must be in the
	// body in full (the renderer wraps it across rows rather than dropping
	// the tail, so assert on the pre-wrap lines).
	views, err := m.store.ProjectChannels("ATM")
	if err != nil {
		t.Fatalf("ProjectChannels: %v", err)
	}
	body := strings.Join(channelDetailLines(views[0], "ATM", time.Now()), "\n")
	for _, want := range []string{dir, "exists=true", "git=false"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail lines missing %q:\n%s", want, body)
		}
	}
}

// TestChannelsOverlayTopKeepsDetailChannel: `g` is "top of what you are
// reading". In detail mode it must scroll this channel's body back to the top,
// not swap the pane to the first channel's detail (renderDetail reads
// entries[cursor] live, so a cursor reset would be a silent content swap).
func TestChannelsOverlayTopKeepsDetailChannel(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})

	detail := m.channelsOv.renderOverlay()
	if !m.channelsOv.detail {
		t.Fatal("g must not leave the detail view")
	}
	for _, want := range []string{"Channel: specs", "spec database", "acme-hq"} {
		if !strings.Contains(detail, want) {
			t.Errorf("g must keep the second channel's detail; missing %q:\n%s", want, detail)
		}
	}
	if strings.Contains(detail, "product source") {
		t.Errorf("g must not swap the pane to the first channel's detail:\n%s", detail)
	}
	if m.channelsOv.offset != 0 {
		t.Errorf("offset = %d, want 0 after g", m.channelsOv.offset)
	}
}

// TestChannelsOverlayUsesProjectsPaneRow: in the Projects pane, E resolves the
// project the same way D does — the highlighted row — so the two bindings
// cannot point at different projects while the user looks at one row.
func TestChannelsOverlayUsesProjectsPaneRow(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m) // project ATM, scope ATM
	seedProject(t, m, "OTH", "Other")
	if _, err := m.store.CreateChannel("OTH", core.ChannelRecord{Name: "handbook", Type: core.ChannelTypeNotion}, testActor); err != nil {
		t.Fatalf("CreateChannel handbook: %v", err)
	}
	m.refreshAll()

	m.focused = paneProjects
	found := false
	for i, row := range m.projects.list {
		if row.code == "OTH" {
			m.projects.cursor, found = i, true
		}
	}
	if !found {
		t.Fatal("OTH must be in the projects list")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	if m.channelsOv.project != "OTH" {
		t.Fatalf("overlay project = %q, want the highlighted row OTH", m.channelsOv.project)
	}
	view := m.channelsOv.renderOverlay()
	if !strings.Contains(view, "handbook") || strings.Contains(view, "specs") {
		t.Errorf("overlay must list OTH's channels, not the scoped project's:\n%s", view)
	}

	// Outside the Projects pane the overlay stays on the scoped project.
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.focused = paneTasks
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	if m.channelsOv.project != "ATM" {
		t.Fatalf("overlay project = %q, want the scope ATM outside the Projects pane", m.channelsOv.project)
	}
}

// TestChannelsOverlayRendersGlyphs pins that the rendered list uses the same
// rule: the unwired notion channel shows ○ unwired, the wired repo does not.
func TestChannelsOverlayRendersGlyphs(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})

	view := m.channelsOv.renderOverlay()
	if !strings.Contains(view, "○") || !strings.Contains(view, "unwired") {
		t.Errorf("list must mark the unwired channel ○ unwired:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "specs") && !strings.Contains(line, "○") {
			t.Errorf("specs row must carry the unwired glyph: %q", line)
		}
	}
}

func TestChannelsOverlayDispatchConcierge(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)
	m.agentOptionsFn = testAgents
	m.dispatcher = &fakeDispatcher{preview: "tmux · new window"}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if m.channelsOv.open {
		t.Fatal("c must close the channels overlay")
	}
	if !m.dispatchDlg.active {
		t.Fatal("c must open the dispatch dialog")
	}
	if got := m.dispatchDlg.persona(); got != "concierge" {
		t.Errorf("persona = %q, want concierge", got)
	}
	if m.dispatchDlg.project != "ATM" {
		t.Errorf("dispatch project = %q, want ATM", m.dispatchDlg.project)
	}
}

// TestChannelsOverlayCDoesNotDispatchWithoutProject pins the no-project `c`
// guard: with an empty overlay project the channels `c` must refuse with a
// toast and keep the overlay open — dispatching would launch a scoped session
// whose rendered context carries a literal `project <CODE>` placeholder.
func TestChannelsOverlayCDoesNotDispatchWithoutProject(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	if m.channelsOv.project != "" {
		t.Fatalf("overlay project = %q, want empty", m.channelsOv.project)
	}
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if m.dispatchDlg.active {
		t.Error("c with no project must not open the dispatch dialog")
	}
	if !m.channelsOv.open {
		t.Error("c with no project must keep the overlay open")
	}
	if !strings.Contains(m.toastMsg, "select a project first") {
		t.Errorf("toast = %q, want the select-a-project hint", m.toastMsg)
	}
}

func TestChannelsDispatchIsCapabilityScoped(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if !m.dispatchDlg.active {
		t.Fatal("c must open the dispatch dialog")
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "Scope:") || !strings.Contains(view, "channel") {
		t.Errorf("dialog must show the capability scope line:\n%s", view)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatalf("spawned %d, want 1", len(fd.spawned))
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	for _, want := range []string{"--persona concierge", "--project ATM", "--capability channel"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv missing %q: %s", want, argv)
		}
	}
}

// TestChannelsOverlayIsReadOnly: the overlay never mutates channels — every
// mutating-looking key is inert (all writes go through `atm channel`).
func TestChannelsOverlayIsReadOnly(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	before, err := m.store.ProjectChannels("ATM")
	if err != nil {
		t.Fatalf("ProjectChannels: %v", err)
	}
	for _, k := range []string{"e", "d", "a", "x", "n", "w", "s"} {
		m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
	after, err := m.store.ProjectChannels("ATM")
	if err != nil {
		t.Fatalf("ProjectChannels: %v", err)
	}
	if len(before) != len(after) {
		t.Fatalf("channels changed %d -> %d; overlay must be read-only", len(before), len(after))
	}
}
