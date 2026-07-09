package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// fakePlugin is a minimal plugin for supervisor/dock tests.
type fakePlugin struct {
	resets int
}

func (f *fakePlugin) ID() string                                   { return "fake" }
func (f *fakePlugin) Icon() string                                 { return "#" }
func (f *fakePlugin) OverlayKey() string                           { return "1" }
func (f *fakePlugin) DockLabel(state any) string                   { return "# " + state.(string) }
func (f *fakePlugin) DockColor(state any, s Styles) lipgloss.Style { return s.Status }
func (f *fakePlugin) State(m *Model) any                           { return "off" }
func (f *fakePlugin) Open(m *Model)                                {}
func (f *fakePlugin) Close(m *Model)                               {}
func (f *fakePlugin) Reset(m *Model)                               { f.resets++ }
func (f *fakePlugin) HandleKey(k tea.KeyMsg, m *Model) tea.Cmd     { return nil }
func (f *fakePlugin) Render(m *Model) string                       { return "fake overlay" }

func TestSupervisorResetsAfterThreeErrorsIn30s(t *testing.T) {
	sv := newPluginSupervisor()
	p := &fakePlugin{}
	m := newTestModel(t)
	_ = m
	if sv.recordError(p) {
		t.Fatal("first error should not reset")
	}
	if sv.recordError(p) {
		t.Fatal("second error should not reset")
	}
	if !sv.recordError(p) {
		t.Fatal("third error within 30s should reset")
	}
	// recordError signals reset; the caller performs it (the supervisor does
	// not call Reset itself — see plugin.go recordError).
	p.Reset(m)
	if p.resets != 1 {
		t.Fatalf("Reset called %d times, want 1", p.resets)
	}
	// After a reset, the window clears — a fresh error starts a new window.
	sv.clear(p)
	if sv.recordError(p) {
		t.Fatal("after clear, first error should not reset")
	}
}

func TestSupervisorNoResetOutside30sWindow(t *testing.T) {
	sv := newPluginSupervisor()
	sv.window = 100 * time.Millisecond // shrink for a fast test
	p := &fakePlugin{}
	m := newTestModel(t)
	_ = m
	sv.recordError(p)
	sv.recordError(p)
	time.Sleep(150 * time.Millisecond)
	if sv.recordError(p) {
		t.Fatal("error outside window should not count toward 3-strikes")
	}
	if p.resets != 0 {
		t.Fatalf("Reset called %d times, want 0", p.resets)
	}
}

func TestDockSegmentsEmptyWithNoPlugins(t *testing.T) {
	m := newTestModel(t)
	m.plugins = nil
	segs := dockSegments(m)
	if len(segs) != 0 {
		t.Fatalf("got %d segments, want 0", len(segs))
	}
}

func TestDockSegmentsRendersOnePerPlugin(t *testing.T) {
	m := newTestModel(t)
	m.projectScope = "ATM"
	m.plugins = []plugin{&fakePlugin{}}
	segs := dockSegments(m)
	if len(segs) != 1 {
		t.Fatalf("got %d segments, want 1", len(segs))
	}
	// D12: the segment is "<state-colored label>  <muted g1 hint>" — check
	// both the label and the keybind hint appear (avoid brittle ANSI matching).
	if !strings.Contains(segs[0], "# off") {
		t.Errorf("segment %q missing label '# off'", segs[0])
	}
	if !strings.Contains(segs[0], "g1") {
		t.Errorf("segment %q missing keybind hint 'g1' (D12)", segs[0])
	}
}