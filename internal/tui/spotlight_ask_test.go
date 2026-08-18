package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"atm/internal/answer"
	"atm/internal/core"

	tea "github.com/charmbracelet/bubbletea"
)

// Tab is the entry key. Sub-task 1 pinned the Ask row and deliberately left it
// hintless, because tab focused the preview pane and advertising it would have
// documented a binding that did something else (spotlight_render.go:489).
func TestSpotlightTabEntersAskMode(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})

	if m.spotlight.level != levelAsk {
		t.Fatalf("level = %v, want levelAsk", m.spotlight.level)
	}
	if m.spotlight.ask == nil || m.spotlight.ask.question != "indexer" {
		t.Errorf("the query must arrive as the question, got %+v", m.spotlight.ask)
	}
}

// An empty query has nothing to ask about, and tab no longer means preview.
func TestSpotlightTabWithEmptyQueryDoesNothing(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")

	m.spotlight.openSpotlight()
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})

	if m.spotlight.level == levelAsk {
		t.Error("tab must not enter ask mode with nothing to ask")
	}
	if m.spotlight.focus == focusPreview {
		t.Error("tab no longer focuses the preview -- right-arrow does")
	}
}

// Preview focus moves to the arrow that points at the pane it focuses.
func TestSpotlightRightArrowFocusesPreview(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")

	m.spotlight.openSpotlight()
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.spotlight.focus != focusPreview {
		t.Fatal("right-arrow must focus the preview")
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.spotlight.focus != focusList {
		t.Error("left-arrow must return focus to the list")
	}
}

// Peel restores the list exactly: the query is back, and so are its rows.
func TestAskEscPeelRestoresTheListExactly(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")
	before := stripANSI(m.spotlight.renderOverlay())

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	flushSpotSearch(t, m, m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc}))

	if m.spotlight.level == levelAsk {
		t.Fatal("esc must peel out of the ask level")
	}
	if m.spotlight.query != "indexer" {
		t.Errorf("query = %q, want it restored to \"indexer\"", m.spotlight.query)
	}
	if got := stripANSI(m.spotlight.renderOverlay()); got != before {
		t.Errorf("peel must restore the list view exactly.\n--- before ---\n%s\n--- after ---\n%s", before, got)
	}
}

// The row now advertises its key, and the footer names the rebinding.
func TestSpotlightAskRowAdvertisesTab(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")

	view := stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, `Ask ATM: "indexer"`)
	mustContain(t, view, "[Tab] ask")
	mustContain(t, view, "[→] preview")
}

// The gate is gone. A missing chat model is communicated by degraded mode with
// a hint naming the fix -- not by hiding the row, which would leave the user
// with nothing to discover the fix from.
func TestAskRowNeedsOnlyANonEmptyQuery(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	mustNotContain(t, stripANSI(m.spotlight.renderOverlay()), "Ask ATM:")
	searchQuery(t, m, "indexer")
	mustContain(t, stripANSI(m.spotlight.renderOverlay()), "Ask ATM:")
}

// fakeAsker scripts an event sequence onto whatever emit it is given.
type fakeAsker struct {
	events []answer.Event
	err    error
	block  chan struct{} // if non-nil, waits before the terminal event
}

func (f *fakeAsker) Ask(ctx context.Context, q answer.Query, emit func(answer.Event)) error {
	for _, ev := range f.events {
		if _, terminal := ev.(answer.Done); terminal && f.block != nil {
			<-f.block
		}
		emit(ev)
	}
	return f.err
}

// withAsker swaps the engine seam for the duration of one test.
func withAsker(t *testing.T, a askAsker) {
	t.Helper()
	prev := askEngineFor
	askEngineFor = func(sm *spotlightModel) askAsker { return a }
	t.Cleanup(func() { askEngineFor = prev })
}

// SOURCES paint on Retrieved, before any generation is attempted. This is the
// ordering the whole degraded story rests on: hits reach the user even when no
// model ever answers.
func TestAskRendersSourcesBeforeAnyDelta(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: "ATM-0001", Kind: "task", Title: "wire the indexer"}}, Behind: 4},
		answer.Done{},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	if len(p.sources) != 1 || p.sources[0].ID != "ATM-0001" {
		t.Fatalf("sources = %+v, want the retrieved hit", p.sources)
	}
	if p.behind != 4 {
		t.Errorf("behind = %d, want 4", p.behind)
	}
}

// The transport's entire justification. The indexer's precedent -- a buffered
// channel with non-blocking sends that DROP on overflow (indexer.go:768) -- is
// right for progress lines, where the newest supersedes the last, and wrong
// here: a dropped delta is a hole in the middle of the answer that the user has
// no way to notice. Burst far more deltas between two ticks than any channel
// would buffer, and demand every one of them.
func TestAskDropsNoDeltasUnderBurst(t *testing.T) {
	withInstantSpotSearch(t)
	const n = 5000
	events := []answer.Event{answer.Retrieved{}}
	want := strings.Builder{}
	for i := 0; i < n; i++ {
		chunk := fmt.Sprintf("%d ", i)
		events = append(events, answer.Delta{Text: chunk})
		want.WriteString(chunk)
	}
	events = append(events, answer.Done{})
	withAsker(t, &fakeAsker{events: events})

	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	if p.transcript != want.String() {
		t.Errorf("transcript lost text: got %d bytes, want %d", len(p.transcript), want.Len())
	}
}

// A tick belonging to a retired stream must not write into the pane. Same
// reason searchGen exists (spotlight.go:34): the user moved on.
func TestAskIgnoresTicksFromARetiredStream(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{answer.Retrieved{}, answer.Delta{Text: "stale"}, answer.Done{}}})
	m, p := openAsk(t, "indexer")
	stale := askTickMsg{gen: p.gen - 1}
	if cmd := p.applyTick(stale); cmd != nil {
		t.Error("a retired tick must not reschedule")
	}
	if p.transcript != "" {
		t.Errorf("transcript = %q, want a retired tick to write nothing", p.transcript)
	}
	_ = m
}

// Ask returns ErrUsage for an empty question and emits NOTHING, so a consumer
// driven purely by events would never learn. The pane must check the error.
func TestAskSurfacesAskReturnedError(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{err: errors.New("ledger is corrupt")})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)
	if !strings.Contains(p.errText+p.transcript, "ledger is corrupt") {
		t.Errorf("a store error must reach the pane, got errText=%q", p.errText)
	}
}

// openAsk opens the spotlight, searches, and presses Tab.
func openAsk(t *testing.T, query string) (*Model, *askPane) {
	t.Helper()
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "wire the indexer")
	m.spotlight.openSpotlight()
	// searchQuery requires contentSearchable() -- true only at levelGroup /
	// groupTask -- or its flushSpotSearch fatals on a nil scheduled search.
	// Every other test in this file reaches that level the same way before
	// searching; the brief's openAsk omitted it.
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, query)
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.spotlight.ask == nil {
		t.Fatal("tab did not enter ask mode")
	}
	return m, m.spotlight.ask
}

// drainAskTicks runs ticks through applyTick until the stream reports done,
// the same route production takes. Bounded so a bug fails the test rather than
// hanging the suite.
func drainAskTicks(t *testing.T, m *Model, p *askPane) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		if cmd := p.applyTick(askTickMsg{gen: p.gen}); cmd == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the stream never reported done")
}
