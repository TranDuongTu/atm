package tui

import (
	"context"
	"errors"
	"fmt"
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

func (p *indexerPlugin) HandleKey(k tea.KeyMsg, m *Model) tea.Cmd {
	im := p.model(m)
	if im.editMode {
		return p.handleEditKey(k, m)
	}
	switch k.String() {
	case "S":
		return p.handleStartStop(m)
	case "e":
		p.openEdit(m)
		return nil
	case "j", "down", "pgdown":
		im.logOffset = scrollDown(im.logs, im.logOffset)
		return nil
	case "k", "up", "pgup":
		im.logOffset = scrollUp(im.logs, im.logOffset)
		return nil
	case "G":
		im.logOffset = -1
		return nil
	}
	return nil
}

func (p *indexerPlugin) Render(m *Model) string {
	im := p.model(m)
	_ = im
	bw, bh := m.helpBoxSize()
	innerW := bw - 2
	if innerW < 1 {
		innerW = 1
	}
	var b strings.Builder
	b.WriteString(p.renderConfigBlock(m, innerW))
	b.WriteString("\n")
	b.WriteString(p.renderStatusBlock(m, innerW))
	b.WriteString("\n")
	b.WriteString(p.renderActionRow(m, innerW))
	b.WriteString("\n")
	b.WriteString(p.renderLogPane(m, innerW))
	return titledBoxHeight(m.styles.DialogBody, bw, "Indexer — "+m.projectScope, b.String(), bh)
}

func (p *indexerPlugin) renderConfigBlock(m *Model, w int) string {
	var b strings.Builder
	b.WriteString(sectionDivider(m.styles, w, "Config"))
	b.WriteString("\n")
	im := p.model(m)
	if im.editMode {
		for i, f := range im.editFields {
			active := i == im.editCursor
			label := m.styles.FieldLabel.Render(f.Label + ":")
			val := m.styles.FieldValue.Render(f.Value)
			if active {
				val += m.styles.FieldValue.Underline(true).Render(" ")
			}
			b.WriteString(dashboardLine(w, fmt.Sprintf("%s %s", label, val)))
			b.WriteString("\n")
			if f.Hint != "" {
				b.WriteString(dashboardLine(w, m.styles.FieldHint.Render("  " + f.Hint)))
				b.WriteString("\n")
			}
		}
		return b.String()
	}
	if im.cfg == nil {
		b.WriteString(dashboardLine(w, m.styles.Muted.Render("Embedding model: (none — press [e] to configure)")))
		return b.String()
	}
	cfg := im.cfg
	rows := []string{
		fmt.Sprintf("Embedding model:   %s", cfg.Model),
		fmt.Sprintf("Endpoint:          %s", cfg.Endpoint),
		fmt.Sprintf("Dim / threshold:   %d / %.2f", cfg.Dim, cfg.Threshold),
	}
	if cfg.QueryPrefix != "" || cfg.DocPrefix != "" {
		rows = append(rows, fmt.Sprintf("Prefixes:          %s / %s", cfg.QueryPrefix, cfg.DocPrefix))
	}
	for _, r := range rows {
		b.WriteString(dashboardLine(w, r))
		b.WriteString("\n")
	}
	return b.String()
}

func (p *indexerPlugin) renderStatusBlock(m *Model, w int) string {
	var b strings.Builder
	b.WriteString(sectionDivider(m.styles, w, "Status"))
	b.WriteString("\n")
	im := p.model(m)
	stateWord := stateWord(im.state)
	b.WriteString(dashboardLine(w, fmt.Sprintf("Status:    %s %s", p.Icon(), stateWord)))
	b.WriteString("\n")
	if im.cfg != nil && im.startedAt != (time.Time{}) {
		cfg, _ := m.store.GetProjectConfig(m.projectScope)
		if cfg != nil && cfg.UpdatedAt != "" {
			if updated, err := time.Parse(time.RFC3339, cfg.UpdatedAt); err == nil && im.startedAt.Before(updated) {
				b.WriteString(dashboardLine(w, m.styles.Muted.Render(fmt.Sprintf("           %s running (config changed — S to restart)", p.Icon()))))
				b.WriteString("\n")
			}
		}
	}
	for _, r := range im.status {
		line := fmt.Sprintf("Index:     %s  count=%d  last_log_seq=%d  behind=%d", r.Model, r.Count, r.Last, r.Behind)
		b.WriteString(dashboardLine(w, line))
		b.WriteString("\n")
	}
	if im.lastError != "" {
		b.WriteString(dashboardLine(w, m.styles.Error.Render("error: "+im.lastError)))
		b.WriteString("\n")
	}
	return b.String()
}

func (p *indexerPlugin) renderActionRow(m *Model, w int) string {
	im := p.model(m)
	if im.editMode {
		return dashboardLine(w, m.styles.KeyMenu.Render("[Tab] next field   [s] save   [p] nomic preset   [Esc] cancel"))
	}
	return dashboardLine(w, m.styles.KeyMenu.Render("[e] edit config   [s] save   [S] start/stop   [r] reindex once   [d] drop model   [Esc] close"))
}

func (p *indexerPlugin) renderLogPane(m *Model, w int) string {
	var b strings.Builder
	b.WriteString(sectionDivider(m.styles, w, "log"))
	b.WriteString("\n")
	im := p.model(m)
	if len(im.logs) == 0 {
		b.WriteString(dashboardLine(w, m.styles.Muted.Render("(no log lines yet)")))
		return b.String()
	}
	visible := im.logs
	if im.logOffset != -1 {
		off := im.logOffset
		if off < 0 {
			off = 0
		}
		if off > len(im.logs) {
			off = len(im.logs)
		}
		visible = im.logs[off:]
	} else {
		if len(visible) > 12 {
			visible = visible[len(visible)-12:]
		}
	}
	for _, l := range visible {
		b.WriteString(dashboardLine(w, fitLine(l, w)))
		b.WriteString("\n")
	}
	return b.String()
}

func stateWord(s indexerState) string {
	switch s {
	case idxOff:
		return "off"
	case idxStopped:
		return "stopped"
	case idxIdle:
		return "on"
	case idxWorking:
		return "running"
	case idxError:
		return "error"
	}
	return "?"
}

func (p *indexerPlugin) handleStartStop(m *Model) tea.Cmd {
	im := p.model(m)
	if im.cfg == nil {
		m.showToast("no embedding configured; press e to edit")
		return nil
	}
	if im.cancel == nil {
		return startIndexer(m, m.projectScope)
	}
	resetIndexer(m)
	return nil
}

func (p *indexerPlugin) openEdit(m *Model) {
	im := p.model(m)
	var model, endpoint, dim, threshold, qp, dp string
	if im.cfg != nil {
		model = im.cfg.Model
		endpoint = im.cfg.Endpoint
		dim = fmt.Sprintf("%d", im.cfg.Dim)
		threshold = fmt.Sprintf("%.2f", im.cfg.Threshold)
		qp = im.cfg.QueryPrefix
		dp = im.cfg.DocPrefix
	}
	im.editFields = []formField{
		{Label: "model", Value: model, Required: true, Hint: "embedding model slug"},
		{Label: "endpoint", Value: endpoint, Required: true, Hint: "OpenAI-compatible /v1/embeddings base URL"},
		{Label: "dim", Value: dim, Hint: "vector dimension"},
		{Label: "threshold", Value: threshold, Hint: "cosine threshold (0 = engine default)"},
		{Label: "query_prefix", Value: qp, Hint: "applied to query text"},
		{Label: "doc_prefix", Value: dp, Hint: "applied to document text"},
	}
	im.editCursor = 0
	im.editMode = true
}

func (p *indexerPlugin) handleEditKey(k tea.KeyMsg, m *Model) tea.Cmd {
	im := p.model(m)
	switch k.String() {
	case "esc":
		im.editMode = false
		return nil
	case "p":
		p.applyNomicPreset(im)
		return nil
	case "s":
		return p.saveConfig(m)
	case "tab", "down":
		im.editCursor = (im.editCursor + 1) % len(im.editFields)
		return nil
	case "shift+tab", "up":
		im.editCursor = (im.editCursor - 1 + len(im.editFields)) % len(im.editFields)
		return nil
	case "backspace":
		f := &im.editFields[im.editCursor]
		if len(f.Value) > 0 {
			f.Value = f.Value[:len(f.Value)-1]
		}
		return nil
	case " ":
		im.editFields[im.editCursor].Value += " "
		return nil
	}
	if k.Type == tea.KeyRunes {
		im.editFields[im.editCursor].Value += string(k.Runes)
	}
	return nil
}

func (p *indexerPlugin) applyNomicPreset(im *indexerModel) {
	set := func(label, val string) {
		for i := range im.editFields {
			if im.editFields[i].Label == label {
				im.editFields[i].Value = val
				return
			}
		}
	}
	set("model", "nomic-embed-text")
	set("endpoint", "http://localhost:11434/v1")
	set("dim", "768")
	set("threshold", "0.55")
	set("query_prefix", "search_query: ")
	set("doc_prefix", "search_document: ")
}

func (p *indexerPlugin) saveConfig(m *Model) tea.Cmd {
	im := p.model(m)
	vals := editFieldValues(im)
	if vals["model"] == "" || vals["endpoint"] == "" {
		m.showToast("model and endpoint are required")
		return nil
	}
	dim := 0
	if vals["dim"] != "" {
		if _, err := fmt.Sscanf(vals["dim"], "%d", &dim); err != nil {
			m.showToast("dim must be an integer")
			return nil
		}
	}
	threshold := 0.0
	if vals["threshold"] != "" {
		if _, err := fmt.Sscanf(vals["threshold"], "%f", &threshold); err != nil {
			m.showToast("threshold must be a number")
			return nil
		}
	}
	cfg := store.EmbeddingConfig{
		Model:       vals["model"],
		Endpoint:    vals["endpoint"],
		QueryPrefix: vals["query_prefix"],
		DocPrefix:   vals["doc_prefix"],
		Dim:         dim,
		Threshold:   threshold,
	}
	if err := m.store.SetEmbeddingConfig(m.projectScope, cfg, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	im.editMode = false
	im.refreshStatus()
	m.showToast("embedding config saved")
	return nil
}

func editFieldValues(im *indexerModel) map[string]string {
	out := map[string]string{}
	for _, f := range im.editFields {
		out[f.Label] = f.Value
	}
	return out
}

func scrollDown(logs []string, offset int) int {
	if offset == -1 {
		return -1
	}
	off := offset + 1
	if off >= len(logs) {
		return -1
	}
	return off
}

func scrollUp(logs []string, offset int) int {
	if offset == -1 {
		if len(logs) == 0 {
			return -1
		}
		start := len(logs) - 12
		if start < 0 {
			start = 0
		}
		return start
	}
	off := offset - 1
	if off < 0 {
		off = 0
	}
	return off
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
