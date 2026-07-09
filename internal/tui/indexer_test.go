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

func TestStartIndexerNoConfigIsNoOp(t *testing.T) {
	m := newIndexerTestModel(t)
	m.projectScope = "ATM"
	p := newIndexerPlugin()
	im := p.model(m) // initialize
	im.refreshStatus()
	cmd := startIndexer(m, "ATM")
	if cmd != nil {
		t.Fatal("no config -> startIndexer should return nil (no-op)")
	}
	if im.state != idxOff {
		t.Fatalf("no config -> state %v, want idxOff", im.state)
	}
	// startIndexer no longer toasts; the caller decides (handleStartStop toasts).
	if m.toastMsg != "" {
		t.Fatalf("startIndexer should not toast on no-config, got %q", m.toastMsg)
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

func TestIndexerOverlayOpensWithoutProjectShowsOff(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.projectScope = ""
	update(t, m, "g")
	update(t, m, "1")
	if m.pluginOverlay != 0 {
		t.Fatalf("overlay should open even with no project (D14), got %d", m.pluginOverlay)
	}
	view := m.View()
	if !strings.Contains(view, "(no project)") {
		t.Errorf("title should show '(no project)' when none selected:\n%s", view)
	}
	if !strings.Contains(view, "(none") {
		t.Errorf("config block should show '(none — press [e] to configure)':\n%s", view)
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

func TestIndexerEditPrefillsFromCurrentConfig(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("e"), m)
	if !im.editMode {
		t.Fatal("e should toggle edit mode on")
	}
	if len(im.editFields) == 0 {
		t.Fatal("edit fields should be populated")
	}
	vals := editFieldValues(im)
	if vals["model"] != "m" {
		t.Errorf("prefill model = %q, want m", vals["model"])
	}
	if vals["endpoint"] != "http://x" {
		t.Errorf("prefill endpoint = %q, want http://x", vals["endpoint"])
	}
}

func TestIndexerEditNomicPresetFillsDefaults(t *testing.T) {
	m := newIndexerTestModel(t)
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("e"), m)
	p.HandleKey(keyMsg("p"), m)
	vals := editFieldValues(im)
	if vals["model"] != "nomic-embed-text" {
		t.Errorf("preset model = %q, want nomic-embed-text", vals["model"])
	}
	if vals["endpoint"] != "http://localhost:11434/v1" {
		t.Errorf("preset endpoint = %q", vals["endpoint"])
	}
	if vals["dim"] != "768" {
		t.Errorf("preset dim = %q, want 768", vals["dim"])
	}
	if vals["threshold"] != "0.55" {
		t.Errorf("preset threshold = %q, want 0.55", vals["threshold"])
	}
	if vals["query_prefix"] != "search_query: " {
		t.Errorf("preset query_prefix = %q", vals["query_prefix"])
	}
	if vals["doc_prefix"] != "search_document: " {
		t.Errorf("preset doc_prefix = %q", vals["doc_prefix"])
	}
}

func TestIndexerEditSaveWritesConfig(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("e"), m)
	setEditField(t, im, "model", "newmodel")
	cmd := p.HandleKey(keyMsg("s"), m)
	_ = cmd
	if im.editMode {
		t.Fatal("s should exit edit mode")
	}
	cfg, _ := m.store.GetProjectConfig("ATM")
	if cfg.Embedding.Model != "newmodel" {
		t.Errorf("after save: model = %q, want newmodel", cfg.Embedding.Model)
	}
}

func TestIndexerEditCancelReverts(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("e"), m)
	setEditField(t, im, "model", "discarded")
	p.HandleKey(keyMsg("esc"), m)
	if im.editMode {
		t.Fatal("Esc should exit edit mode")
	}
	cfg, _ := m.store.GetProjectConfig("ATM")
	if cfg.Embedding.Model != "m" {
		t.Errorf("after cancel: model = %q, want m (unchanged)", cfg.Embedding.Model)
	}
}

func TestIndexerEditSaveRequiredValidation(t *testing.T) {
	m := newIndexerTestModel(t)
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("e"), m)
	setEditField(t, im, "model", "")
	p.HandleKey(keyMsg("s"), m)
	if !im.editMode {
		t.Fatal("s with empty model should stay in edit mode (validation fail)")
	}
	cfg, _ := m.store.GetProjectConfig("ATM")
	if cfg != nil && cfg.Embedding != nil {
		t.Fatal("validation fail should not write config")
	}
}

func setEditField(t *testing.T, im *indexerModel, label, value string) {
	t.Helper()
	for i := range im.editFields {
		if im.editFields[i].Label == label {
			im.editFields[i].Value = value
			return
		}
	}
	t.Fatalf("edit field %q not found", label)
}

func TestIndexerReindexOnceRunsAndLogs(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	cmd := p.HandleKey(keyMsg("r"), m)
	if cmd == nil {
		t.Fatal("r should return a cmd (ReindexOnce)")
	}
	res, err := m.store.ReindexOnce("ATM", im.embedFnBuilder(im.cfg), func(msg string) {
		im.logs = append(im.logs, msg)
	})
	if err != nil {
		t.Fatalf("ReindexOnce: %v", err)
	}
	if res.Indexed != 1 {
		t.Errorf("indexed = %d, want 1", res.Indexed)
	}
	if len(im.logs) == 0 {
		t.Error("reindex should have logged progress lines")
	}
}

func TestIndexerReindexProgressRoutedThroughMsgCh(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	cmd := p.HandleKey(keyMsg("r"), m)
	if cmd == nil {
		t.Fatal("r should return a cmd (ReindexOnce)")
	}
	// Execute the returned cmd and drain the channel via applyTick, the same
	// path Update's pluginTickMsg handler takes. Progress must NOT mutate
	// im.logs directly from the goroutine; it routes through msgCh and the
	// tick drain applies it in the main Update path.
	msg := cmd()
	if _, ok := msg.(reindexResultMsg); !ok {
		t.Fatalf("cmd should return reindexResultMsg, got %T", msg)
	}
	applyTick(m)
	if len(im.logs) == 0 {
		t.Fatal("reindex progress should have been routed through msgCh into im.logs")
	}
}

func TestDockAlwaysVisibleWithNoProject(t *testing.T) {
	m := newIndexerTestModel(t)
	m.projectScope = ""
	m.SetSize(100, 30)
	segs := dockSegments(m)
	if len(segs) != 1 {
		t.Fatalf("dock should always render (D14), got %d segments: %v", len(segs), segs)
	}
	if !strings.Contains(segs[0], "off") {
		t.Errorf("no-project dock should show 'off' state, got %q", segs[0])
	}
	if !strings.Contains(segs[0], "g1") {
		t.Errorf("dock should include the 'g1' keybind hint (D12), got %q", segs[0])
	}
}

func TestIndexerReindexOnceDisabledWhileRunning(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("S"), m)
	if im.cancel == nil {
		t.Fatal("S should have started the watcher")
	}
	p.HandleKey(keyMsg("r"), m)
	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "stop the watcher") {
		t.Fatalf("r while running should toast 'stop the watcher', got %q", m.toastMsg)
	}
	resetIndexer(m)
}

func TestIndexerDropModelConfirmAndDrop(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})
	if _, err := m.store.ReindexOnce("ATM", im.embedFnBuilder(im.cfg), nil); err != nil {
		t.Fatalf("seed ReindexOnce: %v", err)
	}
	im.refreshStatus()
	if len(im.status) == 0 {
		t.Fatal("expected one index row after seeding")
	}
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("d"), m)
	if m.confirm == confirmNone {
		t.Fatal("d should open the confirm overlay")
	}
	m.confirmYes()
	models, _ := m.store.ListVectorModels("ATM")
	if len(models) != 0 {
		t.Fatalf("after confirm drop: models = %v, want empty", models)
	}
}

func TestIndexerDropNoIndexToasts(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("d"), m)
	if m.confirm != confirmNone {
		t.Fatal("d with no index should not open confirm")
	}
	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "no index") {
		t.Fatalf("expected 'no index' toast, got %q", m.toastMsg)
	}
}

func TestDockShowsKeybindHintWithProject(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	segs := dockSegments(m)
	if len(segs) != 1 {
		t.Fatalf("got %d segments, want 1", len(segs))
	}
	if !strings.Contains(segs[0], "stopped") {
		t.Errorf("config present, not started -> want 'stopped' state, got %q", segs[0])
	}
	if !strings.Contains(segs[0], "g1") {
		t.Errorf("dock missing 'g1' keybind hint (D12), got %q", segs[0])
	}
}

func TestIndexerOverlayLogBottomAnchored(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.logs = []string{"only line"}
	im.logOffset = -1
	m.SetSize(100, 40)
	m.pluginOverlay = 0
	p.Open(m)
	view := p.Render(m)
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	half := len(lines) / 2
	found := -1
	for i, l := range lines {
		if strings.Contains(l, "only line") {
			found = i
			break
		}
	}
	if found == -1 {
		t.Fatal("log line 'only line' not found in view")
	}
	if found < half {
		t.Fatalf("log line at row %d of %d (above halfway) — log pane must be bottom-anchored (D13)", found, len(lines))
	}
}

func TestAutoStartStartsWatcherWhenConfigPresent(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})
	autoStartIndexer(m, "ATM")
	if im.state != idxWorking {
		t.Fatalf("autoStart with config: state %v, want idxWorking", im.state)
	}
	if im.cancel == nil {
		t.Fatal("autoStart should have started the goroutine")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && im.state == idxWorking {
		applyTick(m)
		time.Sleep(20 * time.Millisecond)
	}
	if im.state != idxIdle {
		t.Fatalf("after settle: state %v, want idxIdle", im.state)
	}
	// re-selecting the same project is a no-op (watcher already running)
	autoStartIndexer(m, "ATM")
	if im.cancel == nil {
		t.Fatal("re-select should not stop the watcher")
	}
	resetIndexer(m)
}

func TestAutoStartNoConfigDoesNotHijack(t *testing.T) {
	m := newIndexerTestModel(t)
	p := newIndexerPlugin()
	p.model(m)
	autoStartIndexer(m, "ATM")
	if m.pluginOverlay != -1 {
		t.Fatalf("autoStart with no config should NOT open the overlay (dock shows off g1), got %d", m.pluginOverlay)
	}
	im := p.model(m)
	if im.state != idxOff {
		t.Fatalf("no config -> state %v, want idxOff", im.state)
	}
}

func TestErrorAutoOpensOverlay(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = func(*store.EmbeddingConfig) store.EmbedFunc {
		return func(text, role string) ([]float64, error) {
			return nil, errors.New("endpoint down")
		}
	}
	startIndexer(m, "ATM")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && im.state != idxError {
		applyTick(m)
		time.Sleep(20 * time.Millisecond)
	}
	if im.state != idxError {
		t.Fatalf("after drain: state %v, want idxError", im.state)
	}
	if m.pluginOverlay != 0 {
		t.Fatalf("error should auto-open the indexer overlay (D16), got %d", m.pluginOverlay)
	}
	view := m.View()
	if !strings.Contains(view, "error") {
		t.Errorf("overlay should show the error state:\n%s", view)
	}
	resetIndexer(m)
}
