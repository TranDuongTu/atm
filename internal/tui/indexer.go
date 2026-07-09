package tui

import (
	"context"
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

func (p *indexerPlugin) ID() string        { return "indexer" }
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
