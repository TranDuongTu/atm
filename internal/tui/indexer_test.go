package tui

import (
	"testing"

	"atm/internal/store"

	"github.com/charmbracelet/lipgloss"
)

func newIndexerTestModel(t *testing.T) *Model {
	t.Helper()
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	m.projectScope = "ATM"
	m.SetSize(100, 30)
	return m
}

func setEmbedding(t *testing.T, m *Model, code string) {
	t.Helper()
	cfg := store.EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 2, Threshold: 0.5}
	if err := m.store.SetEmbeddingConfig(code, cfg, "claude"); err != nil {
		t.Fatalf("SetEmbeddingConfig: %v", err)
	}
}

func TestIndexerDockLabelPerState(t *testing.T) {
	p := newIndexerPlugin()
	cases := []struct {
		state indexerState
		want  string
	}{
		{idxOff, "⌬ off"},
		{idxStopped, "⌬ stopped"},
		{idxIdle, "⌬ on"},
		{idxWorking, "⌬ running"},
		{idxError, "⌬ error"},
	}
	for _, c := range cases {
		got := p.DockLabel(c.state)
		if got != c.want {
			t.Errorf("state %d: got %q want %q", c.state, got, c.want)
		}
	}
}

func styleProbe(s lipgloss.Style) string { return s.Render("x") }

func TestIndexerDockColorPerState(t *testing.T) {
	p := newIndexerPlugin()
	m := newIndexerTestModel(t)
	s := m.styles
	if styleProbe(p.DockColor(idxOff, s)) != styleProbe(s.Status) {
		t.Error("idxOff should use Status")
	}
	if styleProbe(p.DockColor(idxStopped, s)) != styleProbe(s.Status) {
		t.Error("idxStopped should use Status")
	}
	if styleProbe(p.DockColor(idxIdle, s)) != styleProbe(s.StatusOK) {
		t.Error("idxIdle should use StatusOK")
	}
	if styleProbe(p.DockColor(idxWorking, s)) != styleProbe(s.StatusLabel) {
		t.Error("idxWorking should use StatusLabel")
	}
	if styleProbe(p.DockColor(idxError, s)) != styleProbe(s.Warning) {
		t.Error("idxError should use Warning")
	}
}

func TestIndexerStateOffWhenNoConfig(t *testing.T) {
	m := newIndexerTestModel(t)
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	if p.State(m).(indexerState) != idxOff {
		t.Errorf("no config -> state %v, want idxOff", p.State(m))
	}
}

func TestIndexerStateStoppedWhenConfigPresent(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	if p.State(m).(indexerState) != idxStopped {
		t.Errorf("config present, not started -> state %v, want idxStopped", p.State(m))
	}
}
