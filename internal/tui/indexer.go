package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"atm/internal/embed"
	"atm/internal/store"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type indexerState int

const (
	idxOff indexerState = iota
	idxStopped
	idxIdle
	idxWorking
	idxError
)

type indexerMsgKind int

const (
	msgProgress indexerMsgKind = iota
	msgState
	msgError
	msgDone
)

type indexerMsg struct {
	kind  indexerMsgKind
	line  string
	state indexerState
	err   string
}

type indexerStatusRow struct {
	Model  string
	Count  int
	Last   int
	Behind int
}

type indexerModel struct {
	m              *Model
	state          indexerState
	lastError      string
	logs           []string
	logOffset      int
	cfg            *store.EmbeddingConfig
	status         []indexerStatusRow
	cancel         context.CancelFunc
	done           chan struct{}
	startedAt      time.Time
	editMode       bool
	editFields     []formField
	editCursor     int
	msgCh          chan indexerMsg
	embedFnBuilder func(*store.EmbeddingConfig) store.EmbedFunc
}

type indexerPlugin struct{}

func newIndexerPlugin() *indexerPlugin { return &indexerPlugin{} }

func (p *indexerPlugin) ID() string         { return "indexer" }
func (p *indexerPlugin) Icon() string       { return "⌬" }
func (p *indexerPlugin) OverlayKey() string { return "1" }

func (p *indexerPlugin) DockLabel(state any) string {
	switch state.(indexerState) {
	case idxOff:
		return "⌬ off"
	case idxStopped:
		return "⌬ stopped"
	case idxIdle:
		return "⌬ on"
	case idxWorking:
		return "⌬ running"
	case idxError:
		return "⌬ error"
	}
	return "⌬ ?"
}

func (p *indexerPlugin) DockColor(state any, s Styles) lipgloss.Style {
	switch state.(indexerState) {
	case idxIdle:
		return s.StatusOK
	case idxWorking:
		return s.StatusLabel
	case idxError:
		return s.Warning
	}
	return s.Status
}

func (p *indexerPlugin) State(m *Model) any {
	return p.model(m).state
}

func (p *indexerPlugin) Open(m *Model)  { p.model(m).refreshStatus() }
func (p *indexerPlugin) Close(m *Model) {}
func (p *indexerPlugin) Reset(m *Model) { resetIndexer(m) }

func (p *indexerPlugin) HandleKey(k tea.KeyMsg, m *Model) tea.Cmd { return nil }

func (p *indexerPlugin) Render(m *Model) string {
	return titledBoxHeight(m.styles.DialogBody, m.width, "Indexer — "+m.projectScope, "(indexer overlay — Task 7 fills this in)", m.contentHeight)
}

func (p *indexerPlugin) model(m *Model) *indexerModel {
	if m.indexer == nil {
		m.indexer = &indexerModel{
			m:              m,
			state:          idxOff,
			logOffset:      -1,
			msgCh:          make(chan indexerMsg, 256),
			embedFnBuilder: defaultEmbedFnBuilder,
		}
	}
	return m.indexer
}

func defaultEmbedFnBuilder(cfg *store.EmbeddingConfig) store.EmbedFunc {
	client := embed.New(*cfg)
	return func(text, role string) ([]float64, error) { return client.Embed(text, role) }
}

func (im *indexerModel) refreshStatus() {
	cfg, err := im.m.store.GetProjectConfig(im.m.projectScope)
	if err != nil || cfg == nil || cfg.Embedding == nil {
		im.cfg = nil
		im.status = nil
		if im.state == idxOff || im.state == idxStopped {
			im.state = idxOff
		}
		return
	}
	im.cfg = cfg.Embedding
	if im.state == idxOff {
		im.state = idxStopped
	}
	last, _ := im.m.store.LastLogSeq(im.m.projectScope)
	models, _ := im.m.store.ListVectorModels(im.m.projectScope)
	rows := make([]indexerStatusRow, 0, len(models))
	for _, slug := range models {
		meta, _ := im.m.store.VectorMeta(im.m.projectScope, slug)
		r := indexerStatusRow{Model: slug}
		if meta != nil {
			r.Count = meta.Count
			r.Last = meta.LastLogSeq
			r.Behind = last - meta.LastLogSeq
		}
		rows = append(rows, r)
	}
	im.status = rows
}

type pluginTickMsg struct{}

func pluginTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return pluginTickMsg{} })
}

func startIndexer(m *Model, code string) tea.Cmd {
	im := m.indexer
	if im == nil {
		im = newIndexerPlugin().model(m)
	}
	im.refreshStatus()
	if im.cfg == nil {
		m.showToast("no embedding configured; press e to edit")
		return nil
	}
	if im.cancel != nil {
		return nil // already running
	}
	embedFn := im.embedFnBuilder(im.cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	im.cancel = cancel
	im.done = done
	im.state = idxWorking
	im.startedAt = time.Now()
	send := func(kind indexerMsgKind, line string, st indexerState, err string) {
		select {
		case im.msgCh <- indexerMsg{kind: kind, line: line, state: st, err: err}:
		default:
			// drop-oldest on overflow
			select {
			case <-im.msgCh:
			default:
			}
			select {
			case im.msgCh <- indexerMsg{kind: kind, line: line, state: st, err: err}:
			default:
			}
		}
	}
	progress := func(msg string) {
		switch {
		case isFreshDeltaLine(msg):
			send(msgState, "", idxWorking, "")
			send(msgProgress, msg, 0, "")
		case isCaughtUpLine(msg):
			send(msgState, "", idxIdle, "")
			send(msgProgress, msg, 0, "")
		default:
			send(msgProgress, msg, 0, "")
		}
	}
	go func() {
		defer close(done)
		send(msgState, "", idxWorking, "")
		err := m.store.Watch(ctx, code, embedFn, progress)
		if err != nil && !errors.Is(err, context.Canceled) {
			send(msgError, "", idxError, err.Error())
			return
		}
		send(msgDone, "", idxStopped, "")
	}()
	return pluginTickCmd()
}

func applyIndexerMsg(m *Model, msg indexerMsg) {
	im := m.indexer
	if im == nil {
		return
	}
	switch msg.kind {
	case msgProgress:
		im.logs = append(im.logs, msg.line)
		if len(im.logs) > 1000 {
			im.logs = im.logs[len(im.logs)-1000:]
		}
		if im.logOffset == -1 {
			// stay tailed
		}
	case msgState:
		im.state = msg.state
	case msgError:
		im.lastError = msg.err
		im.state = idxError
		if m.supervisor.recordError(newIndexerPlugin()) {
			resetIndexer(m)
			m.showToast("indexer reset after repeated errors: " + msg.err)
		}
	case msgDone:
		if im.state != idxError {
			im.state = idxStopped
		}
		im.cancel = nil
		im.done = nil
	}
}

func resetIndexer(m *Model) {
	im := m.indexer
	if im == nil {
		return
	}
	stopIndexer(m)
	im.logs = nil
	im.logOffset = -1
	im.lastError = ""
	im.state = idxStopped
	m.supervisor.clear(newIndexerPlugin())
	im.refreshStatus()
}

func stopIndexer(m *Model) {
	im := m.indexer
	if im == nil || im.cancel == nil {
		return
	}
	im.cancel()
	if im.done != nil {
		<-im.done
	}
	drainChannel(im.msgCh)
	im.cancel = nil
	im.done = nil
}

func drainChannel(ch chan indexerMsg) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// isCaughtUpLine reports whether a progress line indicates the watcher just
// completed a pass and is caught up (idle until the next delta). The "wrote"
// completion line and the no-op "nothing to do"/"index fresh" lines both count.
func isCaughtUpLine(line string) bool {
	return strings.Contains(line, "nothing to do") || strings.Contains(line, "index fresh") || strings.Contains(line, "wrote ")
}

// isFreshDeltaLine reports whether a progress line indicates the watcher just
// started embedding new deltas (a pass that will write vectors). Used to flip
// idxIdle -> idxWorking when new work arrives.
func isFreshDeltaLine(line string) bool {
	return strings.HasPrefix(line, "indexing ") || strings.Contains(line, "embedding ")
}
