package tui

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

// fakeEmbedFnBuilder returns an embedFn that yields deterministic 2-dim vectors.
func fakeEmbedFnBuilder(vec []float64) func(*store.EmbeddingConfig) store.EmbedFunc {
	return func(*store.EmbeddingConfig) store.EmbedFunc {
		return func(text, role string) ([]float64, error) { return vec, nil }
	}
}

func applyTick(m *Model) {
	// Synchronously drain the channel + apply messages, mirroring Update's tick handler.
	im := m.indexer
	if im == nil {
		return
	}
	for {
		select {
		case msg := <-im.msgCh:
			applyIndexerMsg(m, msg)
		default:
			return
		}
	}
}

func TestStartIndexerTransitionsToIdleOnCaughtUp(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})

	cmd := startIndexer(m, "ATM")
	if cmd == nil {
		t.Fatal("startIndexer should return a tick cmd")
	}
	if im.state != idxWorking {
		t.Fatalf("after start: state %v, want idxWorking", im.state)
	}
	if im.cancel == nil || im.done == nil {
		t.Fatal("start should set cancel + done")
	}

	// Drain: fire ticks until idle or timeout.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && im.state == idxWorking {
		applyTick(m)
		time.Sleep(20 * time.Millisecond)
	}
	if im.state != idxIdle {
		t.Fatalf("after drain: state %v, want idxIdle", im.state)
	}

	resetIndexer(m)
	if im.state != idxStopped {
		t.Fatalf("after reset: state %v, want idxStopped", im.state)
	}
	if im.cancel != nil || im.done != nil {
		t.Fatal("reset should clear cancel + done")
	}
}

func TestStartIndexerErrorsOnEmbedFailure(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	var calls int32
	im.embedFnBuilder = func(*store.EmbeddingConfig) store.EmbedFunc {
		return func(text, role string) ([]float64, error) {
			atomic.AddInt32(&calls, 1)
			return nil, errors.New("endpoint down")
		}
	}

	startIndexer(m, "ATM")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && im.state == idxWorking {
		applyTick(m)
		time.Sleep(20 * time.Millisecond)
	}
	if im.state != idxError {
		t.Fatalf("after drain: state %v, want idxError", im.state)
	}
	if im.lastError == "" {
		t.Fatal("lastError should record the endpoint error")
	}
	resetIndexer(m)
	if im.state != idxStopped {
		t.Fatalf("after reset: state %v, want idxStopped", im.state)
	}
}

func TestStartIndexerNoConfigToasts(t *testing.T) {
	m := newIndexerTestModel(t)
	m.projectScope = "ATM"
	p := newIndexerPlugin()
	p.model(m) // initialize
	cmd := startIndexer(m, "ATM")
	if cmd != nil {
		t.Fatal("no config -> startIndexer should return nil")
	}
	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "no embedding") {
		t.Fatalf("expected a 'no embedding' toast, got %q", m.toastMsg)
	}
}

func TestStopIndexerBlocksUntilGoroutineReturns(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})
	startIndexer(m, "ATM")
	// let it go idle
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && im.state == idxWorking {
		applyTick(m)
		time.Sleep(20 * time.Millisecond)
	}
	stopIndexer(m)
	if im.cancel != nil || im.done != nil {
		t.Fatal("stop should clear cancel + done")
	}
}

func TestIndexerOverlayRefusesWithoutProject(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.projectScope = ""
	update(t, m, "g")
	update(t, m, "1")
	if m.pluginOverlay != -1 {
		t.Fatal("overlay must not open without a project")
	}
	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "select a project") {
		t.Fatalf("expected 'select a project' toast, got %q", m.toastMsg)
	}
}

func TestIndexerOverlayShowsConfigAndStatus(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	p.model(m).refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	view := p.Render(m)
	mustContain(t, view, "Indexer — ATM")
	mustContain(t, view, "Embedding model:")
	mustContain(t, view, "Embedding model:   m")
	mustContain(t, view, "Endpoint:")
	mustContain(t, view, "Status:")
	mustContain(t, view, "[e] edit config")
	mustContain(t, view, "[S] start/stop")
	mustContain(t, view, "[Esc] close")
}

func TestIndexerOverlayNoConfigShowsNone(t *testing.T) {
	m := newIndexerTestModel(t)
	p := newIndexerPlugin()
	p.model(m).refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	view := p.Render(m)
	mustContain(t, view, "(none")
	mustContain(t, view, "press [e] to configure")
}

func TestIndexerOverlaySTogglesRuntime(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})
	m.pluginOverlay = 0
	p.Open(m)

	// S from stopped -> start
	cmd := p.HandleKey(keyMsg("S"), m)
	if cmd == nil {
		t.Fatal("S from stopped should start the watcher (return tick cmd)")
	}
	if im.state != idxWorking {
		t.Fatalf("S from stopped: state %v, want idxWorking", im.state)
	}
	// let it settle
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && im.state == idxWorking {
		applyTick(m)
		time.Sleep(20 * time.Millisecond)
	}
	if im.state != idxIdle {
		t.Fatalf("after settle: state %v, want idxIdle", im.state)
	}
	// S from running -> stop
	p.HandleKey(keyMsg("S"), m)
	if im.state != idxStopped {
		t.Fatalf("S from running: state %v, want idxStopped", im.state)
	}
}

func TestIndexerOverlaySNoConfigToasts(t *testing.T) {
	m := newIndexerTestModel(t)
	p := newIndexerPlugin()
	p.model(m).refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("S"), m)
	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "no embedding") {
		t.Fatalf("expected 'no embedding' toast, got %q", m.toastMsg)
	}
}

func TestIndexerOverlayLogScroll(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.logs = []string{"line one", "line two", "line three"}
	im.logOffset = -1
	m.pluginOverlay = 0
	p.Open(m)
	// k pins offset away from tail
	p.HandleKey(keyMsg("k"), m)
	if im.logOffset == -1 {
		t.Fatal("k should pin logOffset away from -1")
	}
	// G resets to tail
	p.HandleKey(keyMsg("G"), m)
	if im.logOffset != -1 {
		t.Fatalf("G should reset logOffset to -1 (tail), got %d", im.logOffset)
	}
}
