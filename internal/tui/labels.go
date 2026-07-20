package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

// pluralUses returns "use"/"uses" for the given count — neutral over tasks
// and comments, since LabelUsage counts both kinds of entities.
func pluralUses(n int) string {
	if n == 1 {
		return "use"
	}
	return "uses"
}

func pluralTasks(n int) string {
	if n == 1 {
		return "task"
	}
	return "tasks"
}

// labelSuffixRe validates the suffix the user types in the label add/remove
// forms. The fixed "<CODE>:" prefix is prepended by the form submit handler,
// so the suffix is "<namespace>:<value>" or "<tag>" with NO leading colon.
var labelSuffixRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(:[a-z0-9][a-z0-9-]*)?$`)

// openLabelAddForm opens the add-label form bound to the given project code.
// Used by the Labels pane.
func (m *Model) openLabelAddForm(code string) {
	validator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>, e.g. status:open")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Hint: "<namespace>:<value> or <tag>, e.g. status:open", Validator: validator},
	}
	f := NewForm(fmt.Sprintf("Add label  %s:", code), fields)
	f.Title = fmt.Sprintf("Add label  %s:", code)
	m.form = f
	m.formKind = formLabelAdd
	m.formPayload = code
}

// openLabelRemoveForm opens the remove-label form bound to the given project code.
func (m *Model) openLabelRemoveForm(code string) {
	m.openLabelRemoveFormFor(code, "")
}

// openLabelRemoveFormFor opens the remove-label form with a known suffix.
// Used when the Labels pane has a real label under the cursor.
func (m *Model) openLabelRemoveFormFor(code, suffix string) {
	validator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Value: suffix, Hint: "<namespace>:<value> or <tag>", Validator: validator},
	}
	f := NewForm(fmt.Sprintf("Remove label  %s:", code), fields)
	f.Title = fmt.Sprintf("Remove label  %s:", code)
	m.form = f
	m.formKind = formLabelRemove
	m.formPayload = code
}

// doLabelAdd handles submit of the add-label form.
func (m *Model) doLabelAdd(vals map[string]string) tea.Cmd {
	code := m.formPayload
	suffix := vals["name"]
	full := code + ":" + suffix
	if err := m.store.LabelAdd(full, "", "", m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	return nil
}

// doLabelRemove handles submit of the remove-label form.
func (m *Model) doLabelRemove(vals map[string]string) tea.Cmd {
	code := m.formPayload
	suffix := vals["name"]
	full := code + ":" + suffix
	res, err := m.store.LabelRemove(full, m.actor)
	if err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.showToast(fmt.Sprintf("removed label %s (retained usage: %d)", full, res.RetainedUsage))
	m.refreshAll()
	return nil
}

// --- Boards pane model ---

// boardRow is one row of the Boards pane: a computed label. Boards and
// namespaces render identically on purpose — the user should not have to
// know which is which. The only difference is that a namespace can expand.
type boardRow struct {
	Name             string // display name: "next-sprint" or "status"
	FullName         string // "ATM:next-sprint" or "ATM:status:*"
	Description      string
	Expr             string // empty for a namespace
	Count            int
	Expandable       bool // true for namespaces (they have members)
	NeedsDescription bool // renders the warning mark — conventions rule 6
	Broken           bool // expression invalid or cyclic -> render as broken
}

type boardsModel struct {
	m             *Model
	width         int
	contentHeight int
	rows          []boardRow // L0 flat list of computed labels (boards + namespaces)
	memberRows    []labelRow // all project labels (flat, used to build charts/detail)
	level         lLevel
	ns            string // active namespace at chart/detail
	cursor        int    // indexes the current level's row slice
	tableCursor   int    // L0 cursor saved before entering a chart
	offset        int
	pageSize      int
	detail        labelDetailState

	// selected is the FullName of the ring-selected board, driving the Tasks
	// pane focus. Empty when no project is scoped. Set by selectDefault (on
	// project select) and cycleBoard; refresh() only touches it when the
	// previously selected board vanished from the rebuilt ring.
	selected string

	pins []string // ordered pinned board FullNames; loaded from boardsCfg (config.json.boards.pins)

	// unmanaged holds labels no enabled capability owns — the umbrella's
	// contents. Populated by buildBoardRows via reg.Unmanaged; Task 5's
	// buildUmbrellaRows consumes it to render the drill-in sub-table.
	unmanaged []core.Label // labels no enabled capability owns (umbrella contents)

	// boardsCfg is the per-project boards display preference set (hidden,
	// order, pins), loaded each refresh. Never nil once a project is scoped.
	boardsCfg *core.BoardsConfig

	// pinFocus is WHERE the strong current-filter highlight is drawn: -1 means
	// the strip's SELECTED board is the active filter (the default — set by
	// cycleBoard/selectDefault); >=0 is the index into pins of the pin that
	// Shift-N jumped to. It never changes the filter itself (b.selected still
	// drives that via applyFocus) — only which element gets the strong border.
	pinFocus int

	// umbrellaRows holds the emergent derivation over ONLY the unmanaged
	// labels (the pre-v2 L0 derivation scoped to b.unmanaged). Populated by
	// enterUmbrella/reenterUmbrella and consumed by renderUmbrella.
	umbrellaRows []boardRow // emergent rows over unmanaged labels (umbrella drill-in)

	// fromUmbrella records whether the current chart/detail was entered
	// from the umbrella sub-table, so Esc climbs chart/detail -> sub-table
	// -> ring instead of chart/detail -> ring.
	fromUmbrella bool

	// umbrellaCursor preserves the umbrella row cursor across a drill-in /
	// drill-out of one of its rows, so reenterUmbrella lands back on the
	// row the user drilled from (rather than the top). Set in
	// handleUmbrellaKey's "enter" case; restored (clamped) in
	// reenterUmbrella. Only meaningful in unmanaged mode.
	umbrellaCursor int
}

type lLevel int

const (
	lLevelTable    lLevel = iota // L0 flat boards list
	lLevelChart                 // L1 per-namespace chart
	lLevelDetail                // L2 label detail (or unset leaf)
	lLevelUmbrella              // L0.5: the umbrella's unmanaged sub-table
)

type labelRow struct {
	suffix      string
	full        string
	description string
	usage       int
}

type labelDetailState struct {
	row  labelRow
	leaf string // "" for a real label; "unset" for the synthetic leaf
}

// umbrellaCaption is the unmanaged-mode sub-table's header caption: the
// sentence describing the unmanaged label set, short enough to hold one
// line at the strip's usual width.
const (
	umbrellaCaption = "labels no capability owns; triage via atm capability unmanaged"
)

type chartRow struct {
	full  string
	count int
	unset bool
}

func newBoardsModel(m *Model) boardsModel {
	return boardsModel{m: m, pinFocus: -1}
}

func (b *boardsModel) SetSize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	b.width = w
	b.contentHeight = h
	b.pageSize = h - 2 // table header + trailing line
	if b.pageSize < 1 {
		b.pageSize = 1
	}
}

func (b *boardsModel) refresh() {
	var selectedChart chartRow
	restoreChart := false
	if b.level == lLevelChart {
		rows := b.chartRows()
		if b.cursor >= 0 && b.cursor < len(rows) {
			selectedChart = rows[b.cursor]
			restoreChart = true
		}
	}
	b.rows = nil
	b.memberRows = nil
	if b.m.projectScope == "" {
		return
	}
	scope := b.m.projectScope
	cfg, err := b.m.store.GetBoardsConfig(scope)
	if err != nil || cfg == nil {
		cfg = &core.BoardsConfig{}
	}
	b.boardsCfg = cfg
	ls := b.m.store.LabelList(scope, "")
	usage, _ := b.m.store.LabelUsageGrouped(scope)
	for _, lab := range ls {
		b.memberRows = append(b.memberRows, labelRow{
			suffix:      strings.TrimPrefix(lab.Name, scope+":"),
			full:        lab.Name,
			description: lab.Description,
			usage:       usage[lab.Name],
		})
	}
	b.unmanaged = nil
	b.rows = b.buildBoardRows(ls)
	if b.inUnmanagedMode() {
		un, _ := b.m.regFor(scope).Unmanaged(b.m.store, scope)
		b.unmanaged = un
		b.rows = nil
		b.selected = ""
		switch b.level {
		case lLevelUmbrella:
			// Background tick while browsing: rebuild rows, keep the cursor
			// meaningful (clamped; an unset cursor stays unset).
			b.umbrellaRows = b.buildUmbrellaRows()
			if b.cursor >= len(b.umbrellaRows) {
				b.cursor = len(b.umbrellaRows) - 1
			}
		case lLevelChart, lLevelDetail:
			// Drilled below the sub-table: leave the drill state alone.
		default:
			// Just switched in (resetDrill left us at lLevelTable).
			b.enterUnmanagedBase()
		}
		b.loadPins()
		return
	}
	if restoreChart && b.restoreChartCursor(selectedChart) {
		return
	}
	b.clampCursor()
	b.loadPins()
	// The initial selection happens on project select (selectDefault is
	// called there), not here — refresh runs on every tick and must not
	// clobber a deliberately-empty selection. Only recover when the
	// previously selected board vanished from the rebuilt ring (deleted
	// mid-session), so a stale selection never keeps driving the task list.
	if b.selected != "" && b.ringIndex() < 0 {
		b.selectDefault()
	}
}

// selectDefault selects the All Tasks board if present, else the first ring
// board. Called on project select after EnsureVocabulary, and from refresh()
// when the previously selected board vanished mid-session — that fallback can
// fire while a chart/detail is drilled into the now-vanished board, so this
// always resets the drill state for the same leak-prevention invariant as
// cycleBoard/jumpPin.
func (b *boardsModel) selectDefault() {
	if b.inUnmanagedMode() {
		b.enterUnmanagedBase()
		return
	}
	b.resetDrill()
	b.pinFocus = -1 // the ring board becomes the active-filter highlight
	// UI policy, not a capability concern: all-tasks if the ring has it
	// (workflow enabled), else the first board any capability seeded.
	want := b.m.projectScope + ":all-tasks"
	for _, r := range b.rows {
		if r.FullName == want {
			b.selected = want
			b.applyFocus()
			return
		}
	}
	if len(b.rows) > 0 {
		b.selected = b.rows[0].FullName
		b.applyFocus()
		return
	}
	b.selected = ""
	b.applyFocus()
}

// loadPins reads the project's pins from the boards config and prunes any
// whose board no enabled capability exposes (or that is hidden). Pins are
// GLOBAL across capabilities: a pin may name a board outside the current
// ring — jumpPin switches capability to follow it. Clamped to maxPins.
func (b *boardsModel) loadPins() {
	b.pins = nil
	if b.m.projectScope == "" || b.boardsCfg == nil {
		return
	}
	live := map[string]bool{}
	for _, e := range b.m.regFor(b.m.projectScope).Exposed(b.m.projectScope) {
		live[e.Label.Name] = true
	}
	hidden := map[string]bool{}
	for _, n := range b.boardsCfg.Hidden {
		hidden[n] = true
	}
	for _, full := range b.boardsCfg.Pins {
		if live[full] && !hidden[full] {
			b.pins = append(b.pins, full)
		}
		if len(b.pins) >= maxPins {
			break
		}
	}
	b.syncPinFocus()
}

// maxPins caps the pinned boards at 3, reachable by Shift-1..3 and surfaced as
// the Shift-1..3 tabs of the single tabbed pinned box (see renderPinnedTabs).
// The pinned region is a FIXED slot: the task list reserves a constant
// pinnedBoxHeight lines for it (see listContentHeight), so its height never
// changes as pins are added or removed — an empty slot's tab just renders
// dimmed rather than collapsing.
const maxPins = core.MaxBoardPins

// togglePin adds the selected board to the pin list (at the end) if absent, or
// removes it if present, then persists. Adding past maxPins (3) is ignored
// rather than evicting an existing pin. When a pin is the active highlight
// (pinFocus >= 0), b.selected equals that pin's board, so [p] unpins the
// focused pin and syncPinFocus resets pinFocus back to the strip.
func (b *boardsModel) togglePin() {
	if b.selected == "" || b.m.projectScope == "" {
		return
	}
	pinned := false
	for _, full := range b.pins {
		if full == b.selected {
			pinned = true
			break
		}
	}
	if !pinned && len(b.pins) >= maxPins {
		return
	}
	out := b.pins[:0:0]
	for _, full := range b.pins {
		if full != b.selected {
			out = append(out, full)
		}
	}
	if !pinned {
		out = append(out, b.selected)
	}
	b.pins = out
	b.persistPins()
	b.syncPinFocus()
}

// syncPinFocus re-derives the highlighted-pin index after b.pins changes, so
// the strong highlight stays on the active filter (b.selected). When the
// focused board is no longer pinned, pinFocus falls back to -1 and the strip's
// SELECTED cell reclaims the highlight (b.selected still drives the filter).
func (b *boardsModel) syncPinFocus() {
	if b.pinFocus < 0 {
		return
	}
	b.pinFocus = -1
	for i, full := range b.pins {
		if full == b.selected {
			b.pinFocus = i
			return
		}
	}
}

// jumpPin moves the selection to the nth pinned board (1-based), switching
// the current capability first when the pin belongs to another one. Returns
// false if n is out of range.
func (b *boardsModel) jumpPin(n int) bool {
	if n < 1 || n > len(b.pins) {
		return false
	}
	full := b.pins[n-1]
	if owner := b.ownerOf(full); owner != "" && owner != b.m.capability.current {
		b.m.capability.switchTo(owner)
	}
	b.selected = full
	// A jump must not leak a stale chart/detail/cursor from whatever board was
	// previously drilled into (see cycleBoard's resetDrill call for the same
	// invariant).
	b.resetDrill()
	b.pinFocus = n - 1 // the jumped-to pin becomes the active-filter highlight
	b.applyFocus()
	return true
}

// ownerOf returns the enabled capability exposing full, or "".
func (b *boardsModel) ownerOf(full string) string {
	for _, e := range b.m.regFor(b.m.projectScope).Exposed(b.m.projectScope) {
		if e.Label.Name == full {
			return e.Owner
		}
	}
	return ""
}

// focusCenter is the inverse of jumpPin: it moves the strong current-filter
// highlight from a pin box back to the strip's SELECTED (center) board. Beyond
// restoring pinFocus, it also ENTERS the center board for immediate
// navigation: a namespace board (Expandable) sitting at L0 drills straight into
// its chart, so Shift-↑/↓ move among members right away without a preceding
// Shift-→. A leaf board (no chart) just takes the focus. b.selected and the
// task filter are otherwise untouched (enterChart re-applies the same facet).
func (b *boardsModel) focusCenter() {
	b.pinFocus = -1
	if b.selected == "" || b.m.projectScope == "" {
		return
	}
	// Only enter from L0; if the board is already drilled (chart/detail) there
	// is nothing to enter, and we must not disturb the current level.
	if b.level != lLevelTable {
		return
	}
	idx := b.ringIndex()
	if idx < 0 {
		return
	}
	if r := b.rows[idx]; r.Expandable {
		b.enterChart(r.Name)
	}
}

// resetDrill returns the SELECTED thumbnail to L0 so a board switch never
// leaks a stale chart/detail/cursor into the newly selected board.
func (b *boardsModel) resetDrill() {
	b.level = lLevelTable
	b.ns = ""
	b.cursor = 0
	b.detail = labelDetailState{}
	b.fromUmbrella = false
	b.umbrellaRows = nil
}

func (b *boardsModel) persistPins() {
	if b.m.projectScope == "" {
		return
	}
	cfg, err := b.m.store.GetBoardsConfig(b.m.projectScope)
	if err != nil || cfg == nil {
		cfg = &core.BoardsConfig{}
	}
	cfg.Pins = b.pins
	_ = b.m.store.SetProjectBoards(b.m.projectScope, cfg, b.m.actor)
	b.boardsCfg = cfg
}

// cycleBoard moves the ring selection by dir (+1 next, -1 prev) with
// wraparound and applies the new board's focus to the Tasks list.
func (b *boardsModel) cycleBoard(dir int) {
	if len(b.rows) == 0 {
		return
	}
	idx := b.ringIndex()
	if idx < 0 {
		idx = 0
	}
	idx = (idx + dir) % len(b.rows)
	if idx < 0 {
		idx += len(b.rows)
	}
	b.selected = b.rows[idx].FullName
	b.resetDrill()
	b.pinFocus = -1 // the ring board becomes the active-filter highlight
	b.applyFocus()
}

// ringIndex returns the current ring index of b.selected, or -1 if absent.
func (b *boardsModel) ringIndex() int {
	for i, r := range b.rows {
		if r.FullName == b.selected {
			return i
		}
	}
	return -1
}

// drillIn advances the SELECTED thumbnail one level deeper. For a namespace
// board: L0 -> chart. For a leaf board: L0 -> its detail. For a chart row
// under the cursor: chart -> that label's detail (or unset leaf). At detail,
// it is already the deepest level and is a no-op.
func (b *boardsModel) drillIn() {
	if b.selected == "" || b.m.projectScope == "" {
		return
	}
	idx := b.ringIndex()
	if idx < 0 {
		return
	}
	r := b.rows[idx]
	switch b.level {
	case lLevelTable:
		if r.Expandable {
			b.enterChart(r.Name)
		} else {
			// Leaf board: show its detail.
			b.level = lLevelDetail
			b.detail = labelDetailState{row: labelRow{
				suffix:      strings.TrimPrefix(r.FullName, b.m.projectScope+":"),
				full:        r.FullName,
				description: r.Description,
				usage:       r.Count,
			}}
		}
	case lLevelChart:
		rows := b.chartRows()
		if b.cursor >= 0 && b.cursor < len(rows) {
			if rows[b.cursor].unset {
				b.enterUnsetLeaf()
				return
			}
			if rr, ok := b.chartLabelRow(); ok {
				b.enterDetail(rr)
			}
		}
	case lLevelUmbrella:
		// Shift-→ from the sub-table: same as Enter on the cursor row.
		b.handleUmbrellaKey(tea.KeyMsg{Type: tea.KeyEnter})
	case lLevelDetail:
		// already at the deepest level; no-op
	}
}

// drillOut climbs the SELECTED thumbnail one level out: detail -> chart ->
// L0. At L0 it is a no-op. It must NOT route through enterTable(), whose
// setFocus(focusOff, "") would clear the task filter while a board is still
// SELECTED — climbing out re-applies the selected board's own focus instead.
func (b *boardsModel) drillOut() {
	switch b.level {
	case lLevelDetail:
		if b.ns != "" {
			// Came from a namespace chart (member detail or unset leaf):
			// climb back to the chart. reenterChart restores the facet focus.
			b.reenterChart()
			return
		}
		// Loose-tag detail: climb back to the sub-table when entered from
		// the umbrella, else to L0 (the SELECTED board keeps driving the list).
		if b.fromUmbrella {
			b.fromUmbrella = false
			b.reenterUmbrella()
			b.applyUmbrellaSelection()
			return
		}
		b.level = lLevelTable
		b.detail = labelDetailState{}
		b.applyFocus()
	case lLevelChart:
		if b.fromUmbrella {
			b.fromUmbrella = false
			b.reenterUmbrella()
			b.applyUmbrellaSelection()
			return
		}
		b.level = lLevelTable
		b.ns = ""
		b.cursor = 0
		b.applyFocus()
	case lLevelUmbrella:
		// Unmanaged mode's base level: there is no ring above.
		return
	}
}

// chartCursorMove moves the SELECTED thumbnail's chart cursor (the member row
// that d, l target). Meaningful at the chart level and inside the umbrella
// sub-table, whose rows are navigated by the same Shift-↑/↓ keys; no-op
// elsewhere.
func (b *boardsModel) chartCursorMove(dir int) {
	switch b.level {
	case lLevelChart:
		n := len(b.chartRows())
		if n == 0 {
			return
		}
		b.cursor += dir
		if b.cursor < 0 {
			b.cursor = 0
		}
		if b.cursor >= n {
			b.cursor = n - 1
		}
	case lLevelUmbrella:
		n := len(b.umbrellaRows)
		if n == 0 {
			return
		}
		if b.cursor < 0 {
			// First move from the unset cursor lands on the top row
			// regardless of direction.
			b.cursor = 0
		} else {
			b.cursor += dir
			if b.cursor < 0 {
				b.cursor = 0
			}
			if b.cursor >= n {
				b.cursor = n - 1
			}
		}
		b.applyUmbrellaSelection()
	}
}

// applyFocus pushes the selected board's focus to the Tasks pane, reusing the
// existing setFocus channel. A namespace board (Expandable) uses focusPresent;
// a leaf board uses focusOff + the board's FullName as the filter token.
func (b *boardsModel) applyFocus() {
	if b.selected == "" || b.m.projectScope == "" {
		b.m.tasks.setFocus(taskFocus{mode: focusOff}, "")
		return
	}
	idx := b.ringIndex()
	if idx < 0 {
		return
	}
	r := b.rows[idx]
	if r.Expandable {
		b.m.tasks.setFocus(taskFocus{mode: focusPresent, ns: r.Name}, core.FacetToken(b.m.projectScope, r.Name))
	} else {
		b.m.tasks.setFocus(taskFocus{mode: focusOff}, r.FullName)
	}
}

// buildBoardRows constructs the ring for the CURRENT capability: its Exposed
// labels in the capability's preferred order, hidden rows dropped, then the
// boards-config order override applied as a partial reorder. Other enabled
// capabilities' boards are not in this ring — the [C] switcher changes scope.
// In unmanaged mode the ring is empty (the drill-down is the surface).
func (b *boardsModel) buildBoardRows(ls []core.Label) []boardRow {
	scope := b.m.projectScope
	current := b.m.capability.current
	if current == "" || current == unmanagedCapability {
		return nil
	}
	stored := map[string]core.Label{}
	for _, l := range ls {
		stored[l.Name] = l
	}
	reg := b.m.regFor(scope)
	var out []boardRow
	seen := map[string]bool{}
	for _, e := range reg.Exposed(scope) {
		if e.Owner != current {
			continue
		}
		l := e.Label
		if seen[l.Name] {
			continue
		}
		seen[l.Name] = true
		if s, ok := stored[l.Name]; ok && s.Description != "" {
			l.Description = s.Description
		}
		suffix := strings.TrimPrefix(l.Name, scope+":")
		if core.IsNamespaceName(l.Name) {
			ns := strings.TrimSuffix(suffix, ":*")
			out = append(out, boardRow{
				Name:             ns,
				FullName:         l.Name,
				Description:      l.Description,
				Count:            b.namespaceTaskCount(ns),
				Expandable:       true,
				NeedsDescription: l.Description == "",
			})
			continue
		}
		count, broken := b.boardCount(l.Name)
		out = append(out, boardRow{
			Name:        suffix,
			FullName:    l.Name,
			Description: l.Description,
			Expr:        l.Expr,
			Count:       count,
			Broken:      broken,
		})
	}
	// Hidden filter, then partial order override (unchanged).
	hidden := map[string]bool{}
	for _, n := range b.boardsCfg.Hidden {
		hidden[n] = true
	}
	kept := out[:0:0]
	for _, r := range out {
		if !hidden[r.FullName] {
			kept = append(kept, r)
		}
	}
	out = kept
	if len(b.boardsCfg.Order) > 0 {
		effective := make([]string, len(out))
		for i, r := range out {
			effective[i] = r.FullName
		}
		pos := map[string]int{}
		for i, n := range capability.OrderFullNames(effective, b.boardsCfg.Order) {
			pos[n] = i
		}
		sort.SliceStable(out, func(i, j int) bool { return pos[out[i].FullName] < pos[out[j].FullName] })
	}
	return out
}

// boardCount returns the task count for a board (label with an Expr) and
// whether the expression is broken (invalid or cyclic). Uses GroupTasksErr
// via a single-label query because ListTasks swallows expression errors
// and would conflate a broken board with an empty one.
func (b *boardsModel) boardCount(full string) (int, bool) {
	_, others, err := b.m.store.GroupTasksErr(core.QueryFilters{
		Project: b.m.projectScope,
		Labels:  []string{full},
	})
	if err != nil {
		return 0, true
	}
	return len(others), false
}

// namespaceTaskCount counts distinct tasks carrying the namespace, the same
// surface the old L0 namespace table showed.
func (b *boardsModel) namespaceTaskCount(ns string) int {
	scope := b.m.projectScope
	count := 0
	for _, tk := range b.m.store.ListTasks(core.QueryFilters{Project: scope}) {
		for _, full := range tk.Labels {
			if strings.HasPrefix(full, scope+":"+ns+":") {
				count++
				break
			}
		}
	}
	return count
}

// unmanagedTaskCount counts distinct tasks carrying any label no enabled
// capability owns — the population the umbrella row represents. A task is
// counted once even if it carries several unmanaged labels. A task carrying
// an ad-hoc member of an unmanaged :* namespace (e.g. ATM:comment:foo under
// an unmanaged ATM:comment:* descriptor) is counted via the namespace prefix;
// a task carrying an unmanaged tag or board directly is counted via the
// exact-name set. This matches what the user sees when they drill into the
// umbrella: the sub-table's rows collectively cover these tasks.
//
// The caller passes b.unmanaged explicitly because buildBoardRows has already
// fetched it from the registry; we don't re-derive here.
func (b *boardsModel) unmanagedTaskCount(unmanaged []core.Label) int {
	scope := b.m.projectScope
	exact := make(map[string]bool, len(unmanaged))
	var nsPrefixes []string
	for _, l := range unmanaged {
		exact[l.Name] = true
		if core.IsNamespaceName(l.Name) {
			nsPrefixes = append(nsPrefixes, strings.TrimSuffix(l.Name, "*"))
		}
	}
	count := 0
	for _, tk := range b.m.store.ListTasks(core.QueryFilters{Project: scope}) {
		matched := false
		for _, full := range tk.Labels {
			if exact[full] {
				matched = true
				break
			}
			for _, p := range nsPrefixes {
				if strings.HasPrefix(full, p) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched {
			count++
		}
	}
	return count
}

// buildUmbrellaRows runs the pre-v2 emergent derivation over ONLY the
// unmanaged labels: every unmanaged label with an Expr is a board row, every
// unmanaged <ns>:<value> or <ns>:* introduces a namespace row, and every
// loose tag (no namespace, no Expr) gets a leaf row — the umbrella exists to
// surface EVERY unmanaged label, unlike the old L0 which hid loose tags.
// Sorted by display name, exactly like the old L0.
func (b *boardsModel) buildUmbrellaRows() []boardRow {
	scope := b.m.projectScope
	byName := map[string]core.Label{}
	for _, l := range b.unmanaged {
		byName[l.Name] = l
	}
	var out []boardRow
	seen := map[string]bool{}
	for _, l := range b.unmanaged {
		if l.Expr == "" {
			continue
		}
		name := strings.TrimPrefix(l.Name, scope+":")
		count, broken := b.boardCount(l.Name)
		seen[name] = true
		out = append(out, boardRow{
			Name: name, FullName: l.Name, Description: l.Description,
			Expr: l.Expr, Count: count, Broken: broken,
		})
	}
	nsOrder := []string{}
	nsSeen := map[string]bool{}
	for _, l := range b.unmanaged {
		suffix := strings.TrimPrefix(l.Name, scope+":")
		if core.IsNamespaceName(l.Name) {
			ns := strings.TrimSuffix(suffix, ":*")
			if !nsSeen[ns] {
				nsSeen[ns] = true
				nsOrder = append(nsOrder, ns)
			}
			continue
		}
		parts := strings.SplitN(suffix, ":", 2)
		if len(parts) == 2 {
			if ns := parts[0]; !nsSeen[ns] {
				nsSeen[ns] = true
				nsOrder = append(nsOrder, ns)
			}
		}
	}
	sort.Strings(nsOrder)
	for _, ns := range nsOrder {
		if seen[ns] {
			continue
		}
		descFull := scope + ":" + ns + ":*"
		descLabel, hasDesc := byName[descFull]
		desc := ""
		needsDesc := true
		if hasDesc && descLabel.Description != "" {
			desc = descLabel.Description
			needsDesc = false
		}
		out = append(out, boardRow{
			Name: ns, FullName: descFull, Description: desc,
			Count: b.namespaceTaskCount(ns), Expandable: true, NeedsDescription: needsDesc,
		})
	}
	// Loose tags (no namespace, no Expr): browsable as leaf details. The old
	// L0 derivation treated these as invisible; the umbrella surfaces every
	// unmanaged label, so they get leaf rows here. The usage lookup is hoisted
	// out of the loop so it runs once for the whole loose-tag pass.
	usage, _ := b.m.store.LabelUsageGrouped(scope)
	for _, l := range b.unmanaged {
		suffix := strings.TrimPrefix(l.Name, scope+":")
		if l.Expr != "" || core.IsNamespaceName(l.Name) || strings.Contains(suffix, ":") {
			continue
		}
		out = append(out, boardRow{
			Name: suffix, FullName: l.Name, Description: l.Description,
			Count: usage[l.Name], NeedsDescription: l.Description == "",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// enterUmbrella opens the unmanaged sub-table. No task-filter change: the
// umbrella is a browsing surface, not a filter.
func (b *boardsModel) enterUmbrella() {
	b.tableCursor = b.cursor
	b.level = lLevelUmbrella
	b.umbrellaRows = b.buildUmbrellaRows()
	b.cursor = 0
	b.offset = 0
}

// inUnmanagedMode reports whether pane [2] is in the unmanaged capability
// view: no ring, the label drill-down is the whole surface.
func (b *boardsModel) inUnmanagedMode() bool {
	return b.m.capability.unmanagedCurrent()
}

// enterUnmanagedBase (re)enters unmanaged mode's base level: the full-width
// label drill-down with an UNSET cursor (-1) and the idle task focus. The
// first Shift-↑/↓ sets the cursor and applies that label's filter — until
// then no task query runs (the performance guarantee of the capability-view
// spec).
func (b *boardsModel) enterUnmanagedBase() {
	b.level = lLevelUmbrella
	b.umbrellaRows = b.buildUmbrellaRows()
	b.cursor = -1
	b.offset = 0
	b.tableCursor = 0
	b.fromUmbrella = false
	b.m.tasks.setFocus(taskFocus{mode: focusUmbrellaIdle}, "")
}

// applyUmbrellaSelection pushes the cursor row's label as the tasks filter:
// namespace rows facet (focusPresent), leaf rows filter exactly. An unset or
// out-of-range cursor restores the idle state.
func (b *boardsModel) applyUmbrellaSelection() {
	if b.cursor < 0 || b.cursor >= len(b.umbrellaRows) {
		b.m.tasks.setFocus(taskFocus{mode: focusUmbrellaIdle}, "")
		return
	}
	r := b.umbrellaRows[b.cursor]
	if r.Expandable {
		b.m.tasks.setFocus(taskFocus{mode: focusPresent, ns: r.Name}, core.FacetToken(b.m.projectScope, r.Name))
	} else {
		b.m.tasks.setFocus(taskFocus{mode: focusOff}, r.FullName)
	}
}

// reenterUmbrella returns from a chart/detail entered via the umbrella. The
// cursor is restored from umbrellaCursor (clamped) so the user lands back on
// the row they drilled from; in unmanaged mode that row's filter is then
// re-applied by applyUmbrellaSelection.
func (b *boardsModel) reenterUmbrella() {
	b.level = lLevelUmbrella
	b.umbrellaRows = b.buildUmbrellaRows()
	b.cursor = b.umbrellaCursor
	if b.cursor < 0 {
		b.cursor = 0
	}
	if b.cursor >= len(b.umbrellaRows) {
		b.cursor = len(b.umbrellaRows) - 1
	}
	if b.cursor < 0 {
		b.cursor = 0
	}
	b.ns = ""
	b.detail = labelDetailState{}
}

func (b *boardsModel) handleUmbrellaKey(k tea.KeyMsg) tea.Cmd {
	rows := b.umbrellaRows
	switch k.String() {
	case "j", "down":
		if b.cursor < len(rows)-1 {
			b.cursor++
		}
	case "k", "up":
		if b.cursor > 0 {
			b.cursor--
		}
	case "g":
		b.cursor = 0
	case "]":
		b.cursor += b.pageSize
		if b.cursor > len(rows)-1 {
			b.cursor = len(rows) - 1
		}
		if b.cursor < 0 {
			b.cursor = 0
		}
	case "[":
		b.cursor -= b.pageSize
		if b.cursor < 0 {
			b.cursor = 0
		}
	case "enter":
		if b.cursor < 0 || b.cursor >= len(rows) {
			return nil
		}
		r := rows[b.cursor]
		b.fromUmbrella = true
		b.umbrellaCursor = b.cursor
		if r.Expandable {
			b.enterChart(r.Name)
			return nil
		}
		b.level = lLevelDetail
		b.detail = labelDetailState{row: labelRow{
			suffix:      strings.TrimPrefix(r.FullName, b.m.projectScope+":"),
			full:        r.FullName,
			description: r.Description,
			usage:       r.Count,
		}}
	case "esc":
		// Unmanaged mode's base level: there is no ring above to climb to.
		return nil
	}
	return nil
}

// renderUmbrella draws the unmanaged sub-table in the same meter-bar shape as
// any other namespace board (see renderChart): a "unmanaged · N tasks" header,
// the umbrella's description, then one bar per unmanaged label. It deliberately
// carries no OWNER column — every row here is unowned by definition, so the
// column would repeat "—" and make the umbrella read as a different kind of
// surface than the boards beside it.
func (b *boardsModel) renderUmbrella() string {
	if len(b.umbrellaRows) == 0 {
		return padToHeight("nothing unmanaged — every label is capability-owned", b.contentHeight)
	}
	rows := make([]chartRow, 0, len(b.umbrellaRows))
	for _, r := range b.umbrellaRows {
		rows = append(rows, chartRow{full: r.FullName, count: r.Count})
	}

	var sb strings.Builder
	sb.WriteString(dashboardLine(b.width, fmt.Sprintf("unmanaged  ·  %d %s", len(b.unmanaged), pluralLabels(len(b.unmanaged)))))
	sb.WriteString("\n")
	for _, line := range strings.Split(wordwrap.String(umbrellaCaption, b.width), "\n") {
		sb.WriteString(dashboardLine(b.width, b.m.styles.Muted.Render(line)))
		sb.WriteString("\n")
	}
	sb.WriteString(b.renderMeterRows(rows))
	return padToHeight(sb.String(), b.contentHeight)
}

func (b *boardsModel) restoreChartCursor(selected chartRow) bool {
	rows := b.chartRows()
	for i, r := range rows {
		if selected.unset && r.unset {
			b.cursor = i
			return true
		}
		if !selected.unset && !r.unset && r.full == selected.full {
			b.cursor = i
			return true
		}
	}
	return false
}

func (b *boardsModel) clampCursor() {
	max := len(b.rows) - 1
	if b.level == lLevelChart {
		max = len(b.chartRows()) - 1
	}
	if max < 0 {
		b.cursor = 0
		return
	}
	if b.cursor > max {
		b.cursor = max
	}
	if b.cursor < 0 {
		b.cursor = 0
	}
}

// enterTable returns the pane to L0 and clears the Tasks-pane focus so the
// Tasks pane shows all tasks. cursor is preserved so Esc lands where the user
// drilled from.
func (b *boardsModel) enterTable() {
	b.level = lLevelTable
	b.ns = ""
	b.cursor = b.tableCursor
	if b.cursor >= len(b.rows) {
		b.cursor = len(b.rows) - 1
	}
	if b.cursor < 0 {
		b.cursor = 0
	}
	b.m.tasks.setFocus(taskFocus{mode: focusOff}, "")
}

// enterChart drills into a namespace row's chart and focuses the Tasks pane on
// tasks carrying that namespace. cursor is reset to the top of the chart.
func (b *boardsModel) enterChart(ns string) {
	b.tableCursor = b.cursor
	b.level = lLevelChart
	b.ns = ns
	b.cursor = 0
	b.offset = 0
	b.m.tasks.setFocus(taskFocus{mode: focusPresent, ns: ns}, core.FacetToken(b.m.projectScope, ns))
}

// enterBoard filters the Tasks pane to a board's computed membership. A board
// is a label with an Expr; passing its FullName as a QueryFilters label token
// evaluates the expression through the resolver (Task 4). No new query path.
func (b *boardsModel) enterBoard(r boardRow) {
	b.tableCursor = b.cursor
	b.m.tasks.setFocus(taskFocus{mode: focusOff}, r.FullName)
}

// chartRows returns the active namespace's per-label task counts plus a
// trailing (unset) row for tasks lacking the namespace. The (unset) row is
// suppressed when the chart was entered from the umbrella sub-table: there,
// "tasks lacking this namespace" measures the whole project (including tasks
// that have nothing to do with unmanaged labels), which is nonsensical inside
// a browse-unmanaged-labels view. The ring-entered chart keeps it — (unset)
// is a backlog-triage affordance for owned namespaces (e.g. tasks with no
// status label).
func (b *boardsModel) chartRows() []chartRow {
	scope := b.m.projectScope
	var rows []chartRow
	unset := 0
	groups, others := b.m.store.GroupTasks(core.QueryFilters{Project: scope, Labels: []string{core.FacetToken(scope, b.ns)}})
	counts := map[string]int{}
	for _, g := range groups {
		counts[g.Label] = len(g.Tasks)
	}
	for _, r := range b.memberRows {
		if strings.HasPrefix(r.suffix, b.ns+":") {
			rows = append(rows, chartRow{full: r.full, count: counts[r.full]})
		}
	}
	unset = len(others)
	if unset > 0 && !b.fromUmbrella {
		rows = append(rows, chartRow{full: "(unset)", count: unset, unset: true})
	}
	return rows
}

// enterDetail opens a label's detail (L2) and focuses the Tasks pane on that
// exact label as a flat list.
func (b *boardsModel) enterDetail(r labelRow) {
	b.level = lLevelDetail
	b.detail = labelDetailState{row: r}
	b.m.tasks.setFocus(taskFocus{mode: focusOff}, r.full)
}

// enterUnsetLeaf focuses the Tasks pane on tasks lacking the active namespace.
func (b *boardsModel) enterUnsetLeaf() {
	b.level = lLevelDetail
	b.detail = labelDetailState{leaf: "unset"}
	b.m.tasks.setFocus(taskFocus{mode: focusAbsent, ns: b.ns}, core.FacetToken(b.m.projectScope, b.ns))
}

// reset returns the pane to L0 and clears Tasks focus. Called on project switch
// so no stale filter survives.
func (b *boardsModel) reset() {
	b.level = lLevelTable
	b.ns = ""
	b.cursor = 0
	b.tableCursor = 0
	b.offset = 0
	b.detail = labelDetailState{}
	b.fromUmbrella = false
	b.umbrellaRows = nil
}

func (b *boardsModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch b.level {
	case lLevelTable:
		return b.handleTableKey(k)
	case lLevelChart:
		return b.handleChartKey(k)
	case lLevelDetail:
		return b.handleDetailKey(k)
	case lLevelUmbrella:
		return b.handleUmbrellaKey(k)
	}
	return nil
}

// handleAuthoringKey dispatches the board-authoring keys (n/e/S/d/l) from the
// merged Tasks pane, scoped to the SELECTED board and current drill level.
// It is the selection-aware counterpart to handleTableKey/handleChartKey/
// handleDetailKey (which stay driven directly by the Task-3 re-homed tests):
//
//   - n / S are project-scoped and work at any level.
//   - e (table level only) edits the SELECTED board — b.rows[b.ringIndex()],
//     NOT b.cursor. cycleBoard resets b.cursor to 0, so in the merged pane the
//     cursor no longer tracks the selection; reading it would edit index 0.
//   - d / l describe/remove a label. At chart level they target the {/}-moved
//     chart cursor (b.chartLabelRow, keyed on b.cursor); at a leaf board's
//     detail they target b.detail.row. Both are already correct in the merged
//     pane, so they reuse the same scoping the old handlers use.
//
// Nav keys (j/k/g/[/]/enter/esc) and a=add-task are never routed here — only
// n/e/S/d/l reach this handler.
func (b *boardsModel) handleAuthoringKey(k tea.KeyMsg) tea.Cmd {
	if b.m.projectScope == "" {
		return nil
	}
	switch k.String() {
	case "n":
		b.m.openBoardEditorForm(b.m.projectScope, "")
	case "S":
		return b.seedDefaults()
	case "e":
		// Edit the SELECTED board (meaningful only at table level). A board (a
		// label with an Expr) opens the full editor; a namespace row opens its
		// descriptor's description-only editor.
		if b.level != lLevelTable {
			return nil
		}
		idx := b.ringIndex()
		if idx < 0 {
			return nil
		}
		r := b.rows[idx]
		if r.Expr != "" {
			b.m.openBoardEditorForm(b.m.projectScope, r.Name)
		} else if r.Expandable {
			b.m.openNamespaceDescribeForm(b.m.projectScope, r.Name, r.Description)
		}
	case "d":
		switch b.level {
		case lLevelChart:
			if r, ok := b.chartLabelRow(); ok {
				b.m.openLabelDescribeFormFor(r.suffix, r.description)
			}
		case lLevelDetail:
			if b.detail.leaf == "" {
				b.m.openLabelDescribeFormFor(b.detail.row.suffix, b.detail.row.description)
			}
		}
	case "l":
		switch b.level {
		case lLevelChart:
			if r, ok := b.chartLabelRow(); ok {
				b.m.openLabelRemoveFormFor(b.m.projectScope, r.suffix)
			}
		case lLevelDetail:
			if b.detail.leaf == "" {
				b.m.openLabelRemoveFormFor(b.m.projectScope, b.detail.row.suffix)
			}
		}
	}
	return nil
}

func (b *boardsModel) handleTableKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		if b.cursor < len(b.rows)-1 {
			b.cursor++
		}
	case "k", "up":
		if b.cursor > 0 {
			b.cursor--
		}
	case "g":
		b.cursor = 0
	case "]":
		b.cursor += b.pageSize
		if b.cursor > len(b.rows)-1 {
			b.cursor = len(b.rows) - 1
		}
		if b.cursor < 0 {
			b.cursor = 0
		}
	case "[":
		b.cursor -= b.pageSize
		if b.cursor < 0 {
			b.cursor = 0
		}
	case "a":
		if b.m.projectScope == "" {
			return nil
		}
		b.m.openLabelAddForm(b.m.projectScope)
	case "n":
		if b.m.projectScope == "" {
			return nil
		}
		b.m.openBoardEditorForm(b.m.projectScope, "")
	case "e":
		if b.m.projectScope == "" {
			return nil
		}
		if b.cursor < 0 || b.cursor >= len(b.rows) {
			return nil
		}
		r := b.rows[b.cursor]
		// A board (a label with an Expr) opens the full board editor.
		// A namespace row (no Expr) opens a description-only editor for
		// its descriptor label (<ns>:*) — this is how a human curates the
		// ⚠-flagged undescribed namespaces conventions rule 6 calls out.
		if r.Expr != "" {
			b.m.openBoardEditorForm(b.m.projectScope, r.Name)
		} else if r.Expandable {
			b.m.openNamespaceDescribeForm(b.m.projectScope, r.Name, r.Description)
		}
	case "S":
		if b.m.projectScope == "" {
			return nil
		}
		return b.seedDefaults()
	case "enter":
		if b.cursor < 0 || b.cursor >= len(b.rows) {
			return nil
		}
		r := b.rows[b.cursor]
		if r.Expandable {
			b.enterChart(r.Name)
			return nil
		}
		b.enterBoard(r)
	}
	return nil
}

func (b *boardsModel) handleChartKey(k tea.KeyMsg) tea.Cmd {
	rows := b.chartRows()
	switch k.String() {
	case "j", "down":
		if b.cursor < len(rows)-1 {
			b.cursor++
		}
	case "k", "up":
		if b.cursor > 0 {
			b.cursor--
		}
	case "g":
		b.cursor = 0
	case "]":
		b.cursor += b.pageSize
		if b.cursor > len(rows)-1 {
			b.cursor = len(rows) - 1
		}
		if b.cursor < 0 {
			b.cursor = 0
		}
	case "[":
		b.cursor -= b.pageSize
		if b.cursor < 0 {
			b.cursor = 0
		}
	case "d":
		if r, ok := b.chartLabelRow(); ok {
			b.m.openLabelDescribeFormFor(r.suffix, r.description)
		}
	case "l":
		if r, ok := b.chartLabelRow(); ok {
			b.m.openLabelRemoveFormFor(b.m.projectScope, r.suffix)
		}
	case "enter":
		if b.cursor < 0 || b.cursor >= len(rows) {
			return nil
		}
		if rows[b.cursor].unset {
			b.enterUnsetLeaf()
			return nil
		}
		if r, ok := b.chartLabelRow(); ok {
			b.enterDetail(r)
		}
	case "esc":
		if b.fromUmbrella {
			b.fromUmbrella = false
			b.reenterUmbrella()
			b.applyUmbrellaSelection()
			return nil
		}
		b.enterTable()
	}
	return nil
}

// chartLabelRow returns the labelRow under the chart cursor, or ok=false when
// the cursor is on the (unset) row or out of range.
func (b *boardsModel) chartLabelRow() (labelRow, bool) {
	rows := b.chartRows()
	if b.cursor < 0 || b.cursor >= len(rows) || rows[b.cursor].unset {
		return labelRow{}, false
	}
	full := rows[b.cursor].full
	for _, r := range b.memberRows {
		if r.full == full {
			return r, true
		}
	}
	return labelRow{}, false
}

func (b *boardsModel) handleDetailKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "d":
		if b.detail.leaf == "" {
			b.m.openLabelDescribeFormFor(b.detail.row.suffix, b.detail.row.description)
		}
	case "l":
		if b.detail.leaf == "" {
			b.m.openLabelRemoveFormFor(b.m.projectScope, b.detail.row.suffix)
		}
	case "esc":
		// A real label detail and the (unset) leaf sit above the chart.
		if b.ns != "" {
			b.reenterChart()
			return nil
		}
		// Loose-tag detail (no namespace): climb back to the sub-table when
		// entered from the umbrella, else to L0.
		if b.fromUmbrella {
			b.fromUmbrella = false
			b.reenterUmbrella()
			b.applyUmbrellaSelection()
			return nil
		}
		b.level = lLevelTable
		b.detail = labelDetailState{}
		b.applyFocus()
	}
	return nil
}

// reenterChart re-applies the L1 chart state for the active namespace. Used by
// Esc from a label detail or the (unset) leaf.
func (b *boardsModel) reenterChart() {
	tableCursor := b.tableCursor
	b.enterChart(b.ns)
	b.tableCursor = tableCursor
}

func (b *boardsModel) seedDefaults() tea.Cmd {
	boards, err := b.m.regFor(b.m.projectScope).EnsureVocabulary(b.m.store, b.m.projectScope, b.m.actor)
	if err != nil {
		b.m.showToast("error: " + err.Error())
		return nil
	}
	b.m.showToast(fmt.Sprintf("ensured capability vocabulary in %s (%d boards)", b.m.projectScope, len(boards)))
	b.m.refreshAll()
	return nil
}

func (b *boardsModel) View() string {
	if b.m.projectScope == "" {
		lines := []string{
			b.m.styles.EmptyHead.Render("no project selected"),
			"",
			b.m.styles.EmptyText.Render(fmt.Sprintf("press %s in the Projects pane to scope this view", b.m.styles.EmptyKey.Render("[s]"))),
		}
		return padToHeight(centerLinesBoth(lines, b.width, b.contentHeight), b.contentHeight)
	}
	switch b.level {
	case lLevelChart:
		return b.renderChart()
	case lLevelDetail:
		return b.renderDetail()
	case lLevelUmbrella:
		return b.renderUmbrella()
	default:
		return b.renderTable()
	}
}

func (b *boardsModel) renderTable() string {
	if len(b.rows) == 0 {
		return padToHeight("no boards", b.contentHeight)
	}
	var sb strings.Builder
	header := boardTableLine(b.width, "BOARD", "DESCRIPTION", "COUNT")
	sb.WriteString(dashboardLine(b.width, b.m.styles.HeaderLabel.Render(header)))
	sb.WriteString("\n")

	var lines []string
	for i, r := range b.rows {
		name := r.Name
		if r.NeedsDescription {
			name = name + " " + b.m.styles.Warning.Render("⚠")
		}
		if r.Broken {
			name = name + " " + b.m.styles.Warning.Render("⚠ broken")
		}
		count := fmt.Sprintf("%d", r.Count)
		if r.Broken {
			count = "-"
		}
		line := boardTableLine(b.width, name, r.Description, count)
		if i == b.cursor {
			line = " " + b.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
		}
		lines = append(lines, dashboardLine(b.width, line))
	}
	start, end := windowLines(len(lines), b.cursor, b.pageSize)
	for i := start; i < end; i++ {
		sb.WriteString(lines[i])
		sb.WriteString("\n")
	}
	return padToHeight(sb.String(), b.contentHeight)
}

// boardTableLine renders one row of the flat Boards list: fixed-width name,
// flexible description, and an 8-wide count. Padding is by display width
// (lipgloss.Width), not byte length.
func boardTableLine(width int, name, description, count string) string {
	nameW := 16
	countW := 8
	// leading space (1) + 2 inter-column separators (2) = 3
	descW := width - nameW - countW - 3
	if descW < 8 {
		descW = 8
	}
	namePad := nameW - lipgloss.Width(name)
	if namePad < 0 {
		namePad = 0
	}
	desc := truncateRunes(description, descW)
	descPad := descW - lipgloss.Width(desc)
	if descPad < 0 {
		descPad = 0
	}
	return fitLine(fmt.Sprintf(" %s%s %s%s %8s", name, spaces(namePad), desc, spaces(descPad), count), width)
}

// namespaceDescription returns the description of ns's namespace descriptor
// label (<scope>:<ns>:*), or "" if it has none. Looked up against the
// already-computed L0 row list rather than re-querying the store — buildBoardRows
// resolves the same descLabel.Description onto that namespace's boardRow.
func (b *boardsModel) namespaceDescription(ns string) string {
	for _, r := range b.rows {
		if r.Expandable && r.Name == ns {
			return r.Description
		}
	}
	return ""
}

func (b *boardsModel) renderChart() string {
	var sb strings.Builder
	sb.WriteString(dashboardLine(b.width, fmt.Sprintf("%s  ·  %d tasks", b.ns, b.activeNamespaceTaskCount())))
	sb.WriteString("\n")
	if desc := b.namespaceDescription(b.ns); desc != "" {
		for _, line := range strings.Split(wordwrap.String(desc, b.width), "\n") {
			sb.WriteString(dashboardLine(b.width, b.m.styles.Muted.Render(line)))
			sb.WriteString("\n")
		}
	}
	sb.WriteString(b.renderMeterRows(b.chartRows()))
	return padToHeight(sb.String(), b.contentHeight)
}

// renderMeterRows draws a chart body: one cursor-aware meter bar per row,
// windowed to pageSize. Shared by renderChart and renderUmbrella so a namespace
// board and the unmanaged umbrella read as the same kind of surface.
func (b *boardsModel) renderMeterRows(rows []chartRow) string {
	barTotal := 0
	for _, r := range rows {
		barTotal += r.count
	}

	var sb strings.Builder
	nameW := 0
	for _, r := range rows {
		if w := len(r.full); w > nameW {
			nameW = w
		}
	}
	if nameW < 8 {
		nameW = 8
	}
	meterW := b.width - nameW - 14
	if meterW < 10 {
		meterW = 10
	}

	var lines []string
	for i, r := range rows {
		percent := 0
		if barTotal > 0 {
			percent = (r.count*100 + barTotal/2) / barTotal
		}
		name := fmt.Sprintf("%-*s", nameW, r.full)
		if i == b.cursor {
			name = b.m.styles.RowCursor.Render(r.full) + spaces(nameW-lipgloss.Width(r.full))
		}
		line := fmt.Sprintf(" %s %s %4d", name, meterBar(percent, meterW), r.count)
		lines = append(lines, dashboardLine(b.width, line))
	}
	cur := b.cursor
	if cur < 0 {
		cur = 0
	}
	start, end := windowLines(len(lines), cur, b.pageSize)
	for i := start; i < end; i++ {
		sb.WriteString(lines[i])
		sb.WriteString("\n")
	}
	return sb.String()
}

func (b *boardsModel) renderDetail() string {
	var sb strings.Builder
	switch b.detail.leaf {
	case "unset":
		count := b.syntheticLeafTaskCount()
		sb.WriteString(dashboardLine(b.width, fmt.Sprintf("%d %s with no %s", count, pluralTasks(count), b.ns)))
		return padToHeight(sb.String(), b.contentHeight)
	}
	r := b.detail.row
	sb.WriteString(dashboardLine(b.width, fmt.Sprintf("name        %s", r.full)))
	sb.WriteString("\n")
	sb.WriteString(dashboardLine(b.width, fmt.Sprintf("usage       %d %s", r.usage, pluralUses(r.usage))))
	sb.WriteString("\n")
	const descLabel = "description "
	if r.description == "" {
		sb.WriteString(dashboardLine(b.width, descLabel+b.m.styles.Warning.Render("needs description")))
		return padToHeight(sb.String(), b.contentHeight)
	}
	wrapW := b.width - len(descLabel)
	if wrapW < 1 {
		wrapW = 1
	}
	for i, line := range strings.Split(wordwrap.String(r.description, wrapW), "\n") {
		if i > 0 {
			sb.WriteString("\n")
			sb.WriteString(dashboardLine(b.width, spaces(len(descLabel))+line))
		} else {
			sb.WriteString(dashboardLine(b.width, descLabel+line))
		}
	}
	return padToHeight(sb.String(), b.contentHeight)
}

// activeNamespaceTaskCount counts distinct project tasks carrying the active
// namespace. Chart rows intentionally retain membership counts, so their
// sum is not suitable for the headline.
func (b *boardsModel) activeNamespaceTaskCount() int {
	scope := b.m.projectScope
	count := 0
	for _, tk := range b.m.store.ListTasks(core.QueryFilters{Project: scope}) {
		for _, full := range tk.Labels {
			if strings.HasPrefix(full, scope+":"+b.ns+":") {
				count++
				break
			}
		}
	}
	return count
}

func (b *boardsModel) syntheticLeafTaskCount() int {
	count := 0
	for _, tk := range b.m.store.ListTasks(core.QueryFilters{Project: b.m.projectScope}) {
		if b.detail.leaf != "unset" {
			continue
		}
		hasNamespace := false
		for _, full := range tk.Labels {
			if strings.HasPrefix(full, b.m.projectScope+":"+b.ns+":") {
				hasNamespace = true
				break
			}
		}
		if !hasNamespace {
			count++
		}
	}
	return count
}

func (b *boardsModel) statusHint() string {
	if b.m.projectScope == "" {
		return ""
	}
	switch b.level {
	case lLevelChart:
		return "[Enter]inspect [d]esc [l]remove [Esc]back"
	case lLevelDetail:
		if b.detail.leaf != "" {
			return "[Esc]back"
		}
		return "[d]esc [l]remove [Esc]back"
	case lLevelUmbrella:
		return "[Shift-↑/↓]select label  [Shift-→]drill"
	default:
		return "[Enter]open [n]ew [e]dit [a]dd [S]eed"
	}
}

// --- describe form (used by [d] in list and detail) ---

// openLabelDescribeForm opens a form with name + description fields. The
// user types the label suffix and a new description; submit calls LabelAdd
// (the upsert that overwrites the description).
func (m *Model) openLabelDescribeForm() {
	f := m.newLabelDescribeForm("", "")
	m.form = f
	m.formKind = formLabelDescribe
	m.formPayload = m.projectScope
}

// openLabelDescribeFormFor opens the describe form pre-filled with a known
// suffix and its current description (used from the label detail view).
func (m *Model) openLabelDescribeFormFor(suffix, currentDesc string) {
	f := m.newLabelDescribeForm(suffix, currentDesc)
	m.form = f
	m.formKind = formLabelDescribe
	m.formPayload = m.projectScope
}

func (m *Model) newLabelDescribeForm(suffix, desc string) *Form {
	nameValidator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Value: suffix, Hint: "<namespace>:<value> or <tag>", Validator: nameValidator},
		{Label: "description", Required: false, Value: desc, Hint: "new description (overwrites)"},
	}
	f := NewForm(fmt.Sprintf("Describe label  %s:", m.projectScope), fields)
	f.Title = fmt.Sprintf("Describe label  %s:", m.projectScope)
	return f
}

// doLabelDescribe handles submit of the describe-label form.
func (m *Model) doLabelDescribe(vals map[string]string) tea.Cmd {
	code := m.formPayload
	suffix := vals["name"]
	full := code + ":" + suffix
	desc := vals["description"]
	if err := m.store.LabelAdd(full, desc, "", m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	return nil
}

// --- namespace descriptor form (used by [e] on a namespace row) ---

// openNamespaceDescribeForm opens a description-only form for a namespace
// descriptor label (<code>:<ns>:*). The namespace name is fixed (shown as a
// read-only hint); only the description is editable. Submit upserts the
// descriptor via LabelAdd, which creates it if absent or overwrites the
// description if present. This is the curation path for the ⚠-flagged
// undescribed namespaces conventions rule 6 asks a human to reconcile.
func (m *Model) openNamespaceDescribeForm(code, ns, currentDesc string) {
	fields := []formField{
		{Label: "namespace", Required: true, Value: ns, Hint: "fixed — edit description below"},
		{Label: "description", Required: false, Value: currentDesc, Hint: "what this namespace means (overwrites)"},
	}
	f := NewForm(fmt.Sprintf("Describe namespace  %s:%s:*", code, ns), fields)
	f.Title = fmt.Sprintf("Describe namespace  %s:%s:*", code, ns)
	m.form = f
	m.formKind = formNamespaceDescribe
	// Stash the code + ns so the submit handler can rebuild the descriptor
	// name; the form's own "namespace" field is read-only display.
	m.formPayload = code + ":" + ns
}

// doNamespaceDescribe handles submit of the namespace-describe form. It
// upserts the <code>:<ns>:* descriptor with the typed description.
func (m *Model) doNamespaceDescribe(vals map[string]string) tea.Cmd {
	payload := m.formPayload
	desc := vals["description"]
	full := payload + ":*"
	if err := m.store.LabelAdd(full, desc, "", m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.showToast(fmt.Sprintf("saved descriptor %s", full))
	m.refreshAll()
	return nil
}
