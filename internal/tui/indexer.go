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
	case "r":
		return p.handleReindexOnce(m)
	case "d":
		return p.handleDrop(m)
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
	bw, bh := m.helpBoxSize()
	innerW := bw - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := bh - 2 // titledBoxHeight draws border + title + bottom row
	if innerH < 1 {
		innerH = 1
	}
	cfgBlock := p.renderConfigBlock(m, innerW)
	statusBlock := p.renderStatusBlock(m, innerW)
	actionRow := p.renderActionRow(m, innerW)
	// Each section is separated by one blank line.
	used := lineCount(cfgBlock) + 1 + lineCount(statusBlock) + 1 + lineCount(actionRow) + 1
	logH := innerH - used
	if logH < 1 {
		logH = 1
	}
	logBlock := p.renderLogPane(m, innerW, logH)
	var b strings.Builder
	b.WriteString(cfgBlock)
	b.WriteString("\n")
	b.WriteString(statusBlock)
	b.WriteString("\n")
	b.WriteString(actionRow)
	b.WriteString("\n")
	b.WriteString(logBlock)
	return titledBoxHeight(m.styles.DialogBody, bw, indexerTitle(m), b.String(), bh)
}

// indexerTitle returns the overlay title, handling the no-project case (D14).
func indexerTitle(m *Model) string {
	if m.projectScope == "" {
		return "Indexer — (no project)"
	}
	return "Indexer — " + m.projectScope
}

// lineCount returns the number of newline-separated lines in s (a trailing
// newline does not add an empty line).
func lineCount(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if strings.HasSuffix(s, "\n") {
		return n
	}
	return n + 1
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
				b.WriteString(dashboardLine(w, m.styles.FieldHint.Render("  "+f.Hint)))
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

// renderLogPane renders the log section bottom-anchored (D13): the divider
// sits first, then blank lines pad ABOVE the log content so a few log lines
// read at the BOTTOM of the pane (next to the [Esc] close footer), not
// collapsed at the middle. `height` is the total pane height (including the
// divider line).
func (p *indexerPlugin) renderLogPane(m *Model, w, height int) string {
	im := p.model(m)
	var body strings.Builder
	if len(im.logs) == 0 {
		body.WriteString(dashboardLine(w, m.styles.Muted.Render("(no log lines yet)")))
	} else {
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
			// tail: show the last (height-1) lines — 1 line for the divider.
			tail := height - 1
			if tail < 1 {
				tail = 1
			}
			if len(visible) > tail {
				visible = visible[len(visible)-tail:]
			}
		}
		for _, l := range visible {
			body.WriteString(dashboardLine(w, fitLine(l, w)))
			body.WriteString("\n")
		}
	}
	logContentLines := lineCount(body.String())
	dividerLines := 1
	padAbove := height - dividerLines - logContentLines
	if padAbove < 0 {
		padAbove = 0
	}
	var b strings.Builder
	b.WriteString(sectionDivider(m.styles, w, "log"))
	b.WriteString("\n")
	for i := 0; i < padAbove; i++ {
		b.WriteString(spaces(w))
		b.WriteString("\n")
	}
	b.WriteString(body.String())
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

func (p *indexerPlugin) handleReindexOnce(m *Model) tea.Cmd {
	im := p.model(m)
	if im.cfg == nil {
		m.showToast("no embedding configured; press e to edit")
		return nil
	}
	if im.cancel != nil {
		m.showToast("stop the watcher first (S)")
		return nil
	}
	embedFn := im.embedFnBuilder(im.cfg)
	progress := func(msg string) {
		sendIndexerMsg(im, indexerMsg{kind: msgProgress, line: msg})
	}
	return func() tea.Msg {
		res, err := m.store.ReindexOnce(m.projectScope, embedFn, progress)
		if err != nil {
			return reindexResultMsg{err: err.Error()}
		}
		return reindexResultMsg{indexed: res.Indexed, model: res.Model, logSeq: res.LogSeq}
	}
}

type reindexResultMsg struct {
	indexed int
	model   string
	logSeq  int
	err     string
}

func (p *indexerPlugin) handleDrop(m *Model) tea.Cmd {
	im := p.model(m)
	if len(im.status) == 0 {
		m.showToast("no index file to drop")
		return nil
	}
	row := im.status[0]
	m.confirm = confirmDropIndex
	m.confirmMsg = "Drop vector index?"
	m.confirmArg = fmt.Sprintf("%s/%s will be deleted (re-run r to rebuild)", m.projectScope, row.Model)
	m.confirmPayload = row.Model
	return nil
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
		// No config: no-op. The caller (autoStartIndexer or handleStartStop)
		// decides what to show — autoStart opens the overlay; S toasts.
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
		sendIndexerMsg(im, indexerMsg{kind: kind, line: line, state: st, err: err})
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

// autoStartIndexer is the project-selection entry point (D15). It refreshes
// status, then starts the watcher if the project has embedding config and no
// watcher is already running. When the project has no embedding config it does
// NOT auto-open the overlay — the dock's persistent `⌬ off  g1` hint surfaces
// the not-configured state without hijacking project selection. (A runtime
// start failure — endpoint down — is handled separately by applyIndexerMsg's
// D16 auto-open on error.) Idempotent: re-selecting a project whose watcher is
// already running is a no-op. The previous project's watcher must have been
// reset by the caller before setting the new projectScope.
//
// It returns the tea.Cmd from startIndexer (a pluginTickCmd) so the caller can
// propagate it up the Update chain — that cmd is the only thing that schedules
// the pluginTickMsg handler which drains im.msgCh. Discarding it (the original
// ATM-0077 bug) leaves the dock stuck on "running" with an empty log pane,
// because progress/state messages pile into msgCh and nothing ever applies
// them. Returns nil when there is no config or a watcher is already running.
func autoStartIndexer(m *Model, code string) tea.Cmd {
	im := newIndexerPlugin().model(m)
	im.refreshStatus()
	if im.cfg == nil {
		return nil // no config: dock shows `off g1`; don't hijack selection
	}
	if im.cancel != nil {
		return nil // already running
	}
	return startIndexer(m, code)
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
		} else if m.pluginOverlay == -1 {
			// D16: auto-open the indexer overlay so the user sees the error
			// (don't steal focus if another plugin overlay is already open).
			m.pluginOverlay = 0
			newIndexerPlugin().Open(m)
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

// sendIndexerMsg performs a non-blocking send onto im.msgCh, dropping the
// oldest queued message on overflow so the latest progress is preferred. It
// is safe to call from a Cmd goroutine; the Update tick drain applies the
// messages (all model mutation happens in Update, per D6).
func sendIndexerMsg(im *indexerModel, msg indexerMsg) {
	select {
	case im.msgCh <- msg:
	default:
		// drop-oldest on overflow
		select {
		case <-im.msgCh:
		default:
		}
		select {
		case im.msgCh <- msg:
		default:
		}
	}
}

// isCaughtUpLine reports whether a progress line indicates the watcher just
// completed a pass and is caught up (idle until the next delta). The "wrote"
// completion line and the no-op "nothing to do"/"index fresh" lines both count.
//
// isCaughtUpLine and isFreshDeltaLine match the progress-string prefixes
// emitted by store.Watch / store.ReindexOnce ("indexing ", "embedding ",
// "nothing to do", "index fresh", "wrote "). Changing the store's log
// phrasing requires updating these matchers.
func isCaughtUpLine(line string) bool {
	return strings.Contains(line, "nothing to do") || strings.Contains(line, "index fresh") || strings.Contains(line, "wrote ")
}

// isFreshDeltaLine reports whether a progress line indicates the watcher just
// started embedding new deltas (a pass that will write vectors). Used to flip
// idxIdle -> idxWorking when new work arrives.
func isFreshDeltaLine(line string) bool {
	return strings.HasPrefix(line, "indexing ") || strings.Contains(line, "embedding ")
}
