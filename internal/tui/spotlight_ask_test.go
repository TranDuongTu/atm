package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"atm/internal/answer"
	"atm/internal/core"
	"atm/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

// Tab is the entry key. Sub-task 1 pinned the Ask row and deliberately left it
// hintless, because tab focused the preview pane and advertising it would have
// documented a binding that did something else (spotlight_render.go:489).
func TestSpotlightTabEntersAskMode(t *testing.T) {
	withInstantSpotSearch(t)
	// Tab now runs start(), which launches a real goroutine against
	// askEngineFor's engine. Without withAsker this test would spawn one
	// against a real engine over the test's t.TempDir() store and never peel
	// to stop it -- a leaked goroutine outliving both the test and its temp
	// dir. Script a minimal event sequence instead.
	withAsker(t, &fakeAsker{events: []answer.Event{answer.Retrieved{}, answer.Done{}}})
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
	drainAskTicks(t, m, m.spotlight.ask)
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
	// Same leak this file's other bare-Tab test guards against: Tab now runs
	// start(), which launches a real goroutine.
	withAsker(t, &fakeAsker{events: []answer.Event{answer.Retrieved{}, answer.Done{}}})
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
	drainAskTicks(t, m, m.spotlight.ask)
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
		switch ev.(type) {
		case answer.Done, answer.Failed:
			if f.block != nil {
				<-f.block
			}
		}
		emit(ev)
	}
	return f.err
}

// withAsker swaps the engine seam for the duration of one test. Chat is
// reported configured -- the shape nearly every test wants, since most script
// a Done or Failed that has nothing to do with whether chat is set up.
// withUnconfiguredAsker is the one exception, for the test that specifically
// covers the "chat was never configured" degrade.
func withAsker(t *testing.T, a askAsker) {
	t.Helper()
	withAskerConfigured(t, a, true)
}

// withUnconfiguredAsker scripts a fake asker for a project with no chat
// model set up, matching engine.go:117's own Config.Chat == nil path.
func withUnconfiguredAsker(t *testing.T, a askAsker) {
	t.Helper()
	withAskerConfigured(t, a, false)
}

func withAskerConfigured(t *testing.T, a askAsker, configured bool) {
	t.Helper()
	prev := askEngineFor
	askEngineFor = func(sm *spotlightModel) (askAsker, bool) { return a, configured }
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

// TestAskDropsNoDeltasUnderBurst above drives the burst through a fakeAsker
// whose goroutine runs to completion (5000 short WriteStrings) before the
// test's own applyTick loop gets scheduled at all, so every delta and Done
// land in the SAME drain call -- emit and drain never actually interleave,
// and -race proves nothing about the boundary between them. This test drives
// emit from a goroutine against a drain loop that never sleeps, so the two
// genuinely race on the mutex, and asserts every byte still survives.
func TestAskStreamSurvivesConcurrentEmitAndDrain(t *testing.T) {
	const n = 5000
	st := &askStream{}
	want := strings.Builder{}
	for i := 0; i < n; i++ {
		want.WriteString(fmt.Sprintf("%d ", i))
	}

	go func() {
		for i := 0; i < n; i++ {
			st.emit(answer.Delta{Text: fmt.Sprintf("%d ", i)})
		}
		st.emit(answer.Done{})
	}()

	var got strings.Builder
	for {
		_, _, _, text, _, done := st.drain()
		got.WriteString(text)
		if done {
			break
		}
	}

	if got.String() != want.String() {
		t.Errorf("transcript lost text under concurrent emit/drain: got %d bytes, want %d", got.Len(), want.Len())
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
	// The current generation must still write. Without this half the test
	// passes against a guard that drops EVERY tick -- msg.gen >= p.gen, say --
	// which retires the live stream along with the dead one and leaves the
	// answer permanently blank.
	drainAskTicks(t, m, p)
	if p.transcript != "stale" {
		t.Errorf("transcript = %q, want the CURRENT generation's tick to write", p.transcript)
	}
}

// Ask returns ErrUsage for an empty question and emits NOTHING, so a consumer
// driven purely by events would never learn. The pane must check the error --
// and the constraint is that the error is never SWALLOWED, so the assertion is
// on the rendered view. Asserting on a pane field instead let the error be
// recorded somewhere nothing ever read, which is exactly what happened: the
// field this used to check was write-only and has since been deleted.
func TestAskSurfacesAskReturnedError(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{err: errors.New("ledger is corrupt")})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)
	if view := stripANSI(p.view()); !strings.Contains(view, "ledger is corrupt") {
		t.Errorf("a store error must reach the screen, view:\n%s", view)
	}
}

// applyTick's history-recording rule mirrors cli/ask.go's (ATM-d4ceed): a
// degraded turn generated nothing and must not be replayed as an empty
// assistant reply on the next turn; a canceled partial is the user rejecting
// the answer, not history the model should see; a truncated (non-canceled)
// partial IS genuinely what the conversation contained, and is kept.
func TestAskRecordsHistoryOnlyForNonEmptyUncanceledAnswers(t *testing.T) {
	withInstantSpotSearch(t)

	cases := []struct {
		name   string
		events []answer.Event
		want   bool
	}{
		{
			name:   "a normal Done records",
			events: []answer.Event{answer.Retrieved{}, answer.Delta{Text: "the answer"}, answer.Done{}},
			want:   true,
		},
		{
			name:   "a degraded Done does not record",
			events: []answer.Event{answer.Retrieved{}, answer.Done{Degraded: true, Reason: "no chat model configured"}},
			want:   false,
		},
		{
			name:   "a truncated Failed records the partial",
			events: []answer.Event{answer.Retrieved{}, answer.Delta{Text: "partial"}, answer.Failed{Reason: "endpoint dropped"}},
			want:   true,
		},
		{
			name:   "a canceled Failed does not record",
			events: []answer.Event{answer.Retrieved{}, answer.Delta{Text: "partial"}, answer.Failed{Reason: "canceled", Canceled: true}},
			want:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withAsker(t, &fakeAsker{events: tc.events})
			m, p := openAsk(t, "indexer")
			drainAskTicks(t, m, p)

			if got := len(p.turns) == 1; got != tc.want {
				t.Errorf("recorded = %v, want %v (turns=%+v)", got, tc.want, p.turns)
			}
		})
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

// The layout the redesign fixes (ATM-62adc9): input on top, then two titled
// panes -- Conversation LEFT, Results RIGHT -- and the footer under both. Each
// result row names its entity kind, and the footer says "results", not the
// old "sources".
func TestAskSplitLayout(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{
			{ID: "ATM-0001", Kind: "task", Title: "wire the indexer"},
			{ID: "ATM-0002", Kind: "task", Title: "answer engine core"},
		}},
		answer.Delta{Text: "The indexer is a watcher [1]."},
		answer.Done{},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	view := stripANSI(p.view())
	mustContain(t, view, "Conversation")
	mustContain(t, view, "Results")
	mustContain(t, view, "[1] task ATM-0001")
	mustContain(t, view, "[2] task ATM-0002")
	mustContain(t, view, "The indexer is a watcher [1].")
	mustContain(t, view, "\u2191\u2193 results")
	mustContain(t, view, "esc back")

	lines := strings.Split(view, "\n")
	titles, footer := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "Conversation") && strings.Contains(l, "Results") {
			titles = i
			if strings.Index(l, "Conversation") > strings.Index(l, "Results") {
				t.Fatalf("Conversation must sit LEFT of Results:\n%s", l)
			}
		}
		if strings.Contains(l, "esc back") {
			footer = i
		}
	}
	if titles < 0 {
		t.Fatalf("the two pane titles must share the top border row:\n%s", view)
	}
	if footer < 0 || titles >= footer {
		t.Fatalf("pane titles at %d must sit above the footer at %d:\n%s", titles, footer, view)
	}
}

// Behind renders as its own chip with its own wording. event.go:26 records that
// this number and the dock's "behind" (lastLogSeq - meta.LastLogSeq) disagree
// for the same project at the same instant, so the two must never read as one
// idea.
func TestAskStalenessChipIsNotTheDocksBehind(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: "ATM-0001", Kind: "task"}}, Behind: 4},
		answer.Done{},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	view := stripANSI(p.view())
	mustContain(t, view, "sources may lag · 4 items still indexing")
	if strings.Contains(view, "behind") {
		t.Error("the ask pane must not use the dock's word for a different number")
	}
}

// Nothing pending, nothing to say.
func TestAskNoStalenessChipWhenFresh(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: "ATM-0001", Kind: "task"}}, Behind: 0},
		answer.Done{},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)
	mustNotContain(t, stripANSI(p.view()), "sources may lag")
}

// The question stays visible above its answer, because [n] numbering restarts
// each turn and an older answer's numbers refer to a list no longer on screen.
// Asserting the line ORDER, not just that "indexer" appears somewhere, matters:
// a regression that rendered the answer above the question, or dropped the
// question while some other "indexer" substring survived (e.g. inside the
// question's own echo further down), would pass a mere substring check.
func TestAskTranscriptKeepsTheQuestionAboveTheAnswer(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: "ATM-0001", Kind: "task"}}},
		answer.Delta{Text: "It is a watcher."},
		answer.Done{},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	view := stripANSI(p.view())
	mustContain(t, view, "indexer")
	mustContain(t, view, "It is a watcher.")

	lines := strings.Split(view, "\n")
	question, answerLine := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "indexer") && question < 0 {
			question = i
		}
		if strings.Contains(l, "It is a watcher.") {
			answerLine = i
		}
	}
	if question < 0 || answerLine < 0 || question >= answerLine {
		t.Fatalf("question at %d must sit above its answer at %d:\n%s", question, answerLine, view)
	}
}

// A second turn that ends without being recorded -- canceled, or degraded --
// must not vanish from the transcript. transcriptBody used to approximate
// applyTick's append condition with len(p.turns) == 0, which reads "a turn
// was EVER recorded" as "THIS turn was recorded"; once turn 1 landed in
// p.turns, a canceled or degraded turn 2 was suppressed along with its
// question, leaving no trace the user ever asked it.
func TestAskSecondTurnSurvivesWhenNotRecorded(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: "ATM-0001", Kind: "task"}}},
		answer.Delta{Text: "It is a watcher."},
		answer.Done{},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)
	if len(p.turns) != 1 {
		t.Fatalf("setup: turns = %d, want 1", len(p.turns))
	}

	t.Run("canceled", func(t *testing.T) {
		p.question = "what about the debounce"
		p.transcript = "partial answer before "
		p.streaming = false
		p.canceled = true
		p.scrollToBottom() // mirrors applyTick's follow behavior in production
		view := stripANSI(p.view())
		mustContain(t, view, "what about the debounce")
	})

	t.Run("degraded", func(t *testing.T) {
		p.question = "and the retry policy"
		p.transcript = ""
		p.streaming = false
		p.canceled = false
		p.degraded = true
		p.scrollToBottom()
		view := stripANSI(p.view())
		mustContain(t, view, "and the retry policy")
	})
}

// Enter is overloaded and the disambiguation is the input's emptiness. Both
// directions, because a one-way test would pass on a handler that always
// submits.
func TestAskEnterSubmitsWhenInputHasText(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{}, answer.Delta{Text: "a watcher keeps it fresh."}, answer.Done{},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	for _, r := range "and the watcher?" {
		p.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	p.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if p.question != "and the watcher?" {
		t.Errorf("question = %q, want the follow-up submitted", p.question)
	}
	if p.input != "" {
		t.Errorf("input = %q, want it cleared on submit", p.input)
	}
	if len(p.turns) != 1 {
		t.Errorf("turns = %d, want the first exchange kept as history", len(p.turns))
	}
}

// Enter with nothing typed means OPEN, and the test has to see the open
// happen: asserting only that the question did not change passes just as well
// against a handler that made empty-Enter inert, which is the other way to get
// this wrong. The hit is a real seeded task so openSelected's re-read finds
// it.
func TestAskEnterWithEmptyInputDoesNotSubmit(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	task := seedTask(t, m, "ATM", "wire the indexer")
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: task.ID, Kind: "task"}}}, answer.Done{},
	}})

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	p := m.spotlight.ask
	drainAskTicks(t, m, p)

	before := p.question
	p.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if p.question != before {
		t.Errorf("question changed to %q -- empty input must open, not submit", p.question)
	}
	if m.spotlight.open || m.spotlight.ask != nil || m.focused != paneTasks {
		t.Errorf("empty-input enter must open the source: open=%v ask=%v focused=%v",
			m.spotlight.open, m.spotlight.ask != nil, m.focused)
	}
}

// Esc is two states deep and no deeper: the input never absorbs it.
func TestAskEscCancelsThenPeels(t *testing.T) {
	withInstantSpotSearch(t)
	block := make(chan struct{})
	withAsker(t, &fakeAsker{block: block, events: []answer.Event{
		answer.Retrieved{}, answer.Delta{Text: "partial"}, answer.Done{},
	}})
	m, p := openAsk(t, "indexer")
	for i := 0; i < 50 && p.transcript == ""; i++ {
		p.applyTick(askTickMsg{gen: p.gen})
		time.Sleep(time.Millisecond)
	}
	p.input = "half typed"

	p.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.spotlight.level != levelAsk {
		t.Fatal("the first esc cancels; it must not peel")
	}
	if p.streaming {
		t.Error("the first esc must stop the stream")
	}
	if p.input != "half typed" {
		t.Error("the input must not absorb esc")
	}
	close(block)

	p.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.spotlight.level == levelAsk {
		t.Error("the second esc must peel")
	}
}

// ctrl+r retries a turn that broke on its own -- an endpoint drop, not the
// user walking away from it.
func TestAskRetryAfterInterruptionReRuns(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{}, answer.Delta{Text: "partial"}, answer.Failed{Reason: "endpoint dropped"},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	if !p.failed || p.canceled {
		t.Fatalf("setup: failed=%v canceled=%v, want failed and not canceled", p.failed, p.canceled)
	}
	gen := p.gen

	p.handleKey(tea.KeyMsg{Type: tea.KeyCtrlR})

	if p.gen == gen {
		t.Error("ctrl+r after an interruption must start a new turn")
	}
	if !p.streaming {
		t.Error("ctrl+r after an interruption must be streaming again")
	}
	drainAskTicks(t, m, p)
}

// The guard is the only thing standing between the UI and offering a retry
// for an answer the user deliberately stopped -- ctrl+r after the user's own
// Esc-cancel must be a no-op.
func TestAskRetryAfterCancelDoesNotReRun(t *testing.T) {
	withInstantSpotSearch(t)
	block := make(chan struct{})
	// Failed{Canceled: true} mirrors production: the TUI's context has no
	// deadline, so a user Esc always produces context.Canceled and the engine
	// emits Failed{Canceled: true} (internal/answer/engine.go:150). The block
	// gate holds the terminal event until after Esc has run, so the pane's
	// optimistic p.canceled = true (set by handleKey, not by this event) is
	// confirmed rather than raced.
	withAsker(t, &fakeAsker{block: block, events: []answer.Event{
		answer.Retrieved{}, answer.Delta{Text: "partial"},
		answer.Failed{Reason: "canceled", Canceled: true},
	}})
	m, p := openAsk(t, "indexer")
	for i := 0; i < 50 && p.transcript == ""; i++ {
		p.applyTick(askTickMsg{gen: p.gen})
		time.Sleep(time.Millisecond)
	}
	p.handleKey(tea.KeyMsg{Type: tea.KeyEsc}) // first esc cancels
	close(block)
	drainAskTicks(t, m, p)

	if !p.failed || !p.canceled {
		t.Fatalf("setup: failed=%v canceled=%v, want both true", p.failed, p.canceled)
	}
	gen := p.gen

	p.handleKey(tea.KeyMsg{Type: tea.KeyCtrlR})

	if p.gen != gen {
		t.Error("ctrl+r after the user's own cancel must not start a new turn")
	}
	if p.streaming {
		t.Error("ctrl+r after the user's own cancel must not resume streaming")
	}
}

// Degraded is a SUCCESSFUL outcome with no answer in it: hits reach the user,
// and the hint names the step that would enable answers. It must name the fix,
// not merely report the lack -- a user who cannot see the wizard row cannot
// discover it.
func TestAskDegradedShowsSourcesAndNamesTheFix(t *testing.T) {
	withInstantSpotSearch(t)
	withUnconfiguredAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: "ATM-0001", Kind: "task", Title: "wire the indexer"}}},
		answer.Done{Degraded: true, Reason: "no chat model configured; run 'atm project set-chat'"},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	view := stripANSI(p.view())
	mustContain(t, view, "[1] task ATM-0001")
	mustContain(t, view, "atm project set-chat")
	if strings.Contains(view, "interrupted") {
		t.Error("a degraded answer is not a broken one")
	}
}

// Chat IS configured here -- the degrade is an unreachable endpoint or one
// that answered with nothing (engine.go:155, engine.go:167). Telling the user
// to run `atm project set-chat` would send them to fix a config that is
// already fine, so the hint must show ONLY the reason, never the command.
func TestAskDegradedWithChatConfiguredShowsReasonNotSetChatHint(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: "ATM-0001", Kind: "task", Title: "wire the indexer"}}},
		answer.Done{Degraded: true, Reason: "the chat endpoint returned no answer"},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	view := stripANSI(p.view())
	mustContain(t, view, "the chat endpoint returned no answer")
	if strings.Contains(view, "set-chat") {
		t.Error("chat is already configured -- the hint must not send the user to configure it")
	}
}

// The status line alone was too quiet: one dim row at the bottom, in the same
// style as the footer under it, while the transcript column -- where the eye
// looks for the answer -- stayed blank. A degraded pane read as a plain search
// result list, and users asked where the conversation went (ATM-bc717f). The
// outcome must therefore stand IN the transcript, under the question, where
// the answer would have been -- and in the assistant's own voice, because this
// level is a conversation: a terse system line in the answer's place reads as
// chrome, not as a reply.
func TestAskDegradedNoticeRendersInTranscript(t *testing.T) {
	t.Run("unconfigured chat names the fix conversationally", func(t *testing.T) {
		withInstantSpotSearch(t)
		withUnconfiguredAsker(t, &fakeAsker{events: []answer.Event{
			answer.Retrieved{Hits: []core.Hit{{ID: "ATM-0001", Kind: "task", Title: "wire the indexer"}}},
			answer.Done{Degraded: true, Reason: "no chat model configured; run 'atm project set-chat'"},
		}})
		m, p := openAsk(t, "indexer")
		drainAskTicks(t, m, p)

		body := stripANSI(strings.Join(p.transcriptBody(p.transcriptWidth()), " "))
		mustContain(t, body, "I found 1 related item")
		mustContain(t, body, "atm project set-chat")
	})
	t.Run("configured chat speaks the reason", func(t *testing.T) {
		withInstantSpotSearch(t)
		withAsker(t, &fakeAsker{events: []answer.Event{
			answer.Retrieved{Hits: []core.Hit{{ID: "ATM-0001", Kind: "task", Title: "wire the indexer"}}},
			answer.Done{Degraded: true, Reason: "the chat endpoint returned no answer"},
		}})
		m, p := openAsk(t, "indexer")
		drainAskTicks(t, m, p)

		body := stripANSI(strings.Join(p.transcriptBody(p.transcriptWidth()), " "))
		mustContain(t, body, "the chat endpoint returned no answer")
		if strings.Contains(body, "set-chat") {
			t.Error("chat is already configured -- the transcript notice must not send the user to configure it")
		}
	})
	t.Run("no sources says so instead of counting to zero", func(t *testing.T) {
		withInstantSpotSearch(t)
		withUnconfiguredAsker(t, &fakeAsker{events: []answer.Event{
			answer.Retrieved{},
			answer.Done{Degraded: true, Reason: "no chat model configured; run 'atm project set-chat'"},
		}})
		m, p := openAsk(t, "indexer")
		drainAskTicks(t, m, p)

		body := stripANSI(strings.Join(p.transcriptBody(p.transcriptWidth()), " "))
		mustContain(t, body, "nothing close enough")
		if strings.Contains(body, "0 related") {
			t.Error("an empty retrieval must not be narrated as a count")
		}
	})
}

// A Results row is "[n] kind ID title": the kind comes straight from
// Hit.Kind, so a future searchable entity displays with no new code here. A
// title that still cannot fit the pane ends in an ellipsis instead of
// stopping mid-word (ATM-bc717f: "Semanti", "Spotlig").
func TestAskResultsRowsShowKindAndEllipsis(t *testing.T) {
	withInstantSpotSearch(t)
	withUnconfiguredAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: "ATM-0001", Kind: "task", Title: "semantic search over the whole ledger, cosine plus text fallback and a tail long enough to overflow any pane"}}},
		answer.Done{Degraded: true, Reason: "no chat model configured; run 'atm project set-chat'"},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	view := stripANSI(p.view())
	mustContain(t, view, "[1] task ATM-0001 semantic search")
	mustContain(t, view, "…")
}

// The Conversation pane shows the LATEST exchange only (ATM-62adc9): the
// current question and its answer. Older turns still feed the model as
// history -- the memory is the engine's, not the pane's -- but they do not
// render.
func TestAskConversationShowsOnlyTheLatestExchange(t *testing.T) {
	withInstantSpotSearch(t)
	f := &fakeAsker{events: []answer.Event{
		answer.Retrieved{}, answer.Delta{Text: "the first answer, about watchers"}, answer.Done{},
	}}
	withAsker(t, f)
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	f.events = []answer.Event{
		answer.Retrieved{}, answer.Delta{Text: "the second answer, about embeddings"}, answer.Done{},
	}
	for _, r := range "and embeddings?" {
		p.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	p.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	drainAskTicks(t, m, p)

	body := stripANSI(strings.Join(p.transcriptBody(p.transcriptWidth()), " "))
	mustContain(t, body, "and embeddings?")
	mustContain(t, body, "the second answer, about embeddings")
	if strings.Contains(body, "the first answer") {
		t.Error("the pane must show only the latest exchange -- the first answer belongs to history, not the screen")
	}
	if len(p.turns) != 2 {
		t.Fatalf("turns = %d, want both exchanges recorded as model history", len(p.turns))
	}
}

// Enter on a result is "open the thing", resolved per kind: a task opens its
// detail, a comment resolves to its parent task (both covered elsewhere). A
// kind this dispatch does not know yet -- retrieval may grow projects, lanes,
// capabilities -- degrades honestly: a toast naming the kind, the ask level
// still standing. Never a silent no-op, never a crash.
func TestAskEnterOnUnknownKindToasts(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: "LANE-inbox", Kind: "lane", Title: "Inbox"}}},
		answer.Done{},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	p.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "lane") {
		t.Errorf("toast = %q, want it to name the kind it cannot open", m.toastMsg)
	}
	if m.spotlight.ask == nil || !m.spotlight.open {
		t.Error("an unopenable result must not end the ask")
	}
}

// The user's own key stopped it. Nothing here is an error and nothing offers a
// retry.
func TestAskCanceledKeepsThePartialAndOffersNoRetry(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{}, answer.Delta{Text: "half an answer"},
		answer.Failed{Reason: "context canceled", Canceled: true},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	view := stripANSI(p.view())
	mustContain(t, view, "half an answer")
	mustContain(t, view, "(canceled)")
	if strings.Contains(view, "retry") {
		t.Error("a cancellation must not offer a retry -- the user chose to stop")
	}
}

// A disconnect and an expired deadline are deliberately indistinguishable at
// the event level: both keep the partial and offer a retry, and only Reason
// says which happened.
func TestAskInterruptedKeepsThePartialAndOffersRetry(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{}, answer.Delta{Text: "half an answer"},
		answer.Failed{Reason: "answer timed out", Canceled: false},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	view := stripANSI(p.view())
	mustContain(t, view, "half an answer")
	mustContain(t, view, "answer interrupted")
	mustContain(t, view, "answer timed out")
	mustContain(t, view, "ctrl+r")
}

// Retry re-runs the SAME question with the SAME history. The other two retry
// tests (TestAskRetryAfterInterruptionReRuns, TestAskRetryAfterCancelDoesNotReRun)
// already pin WHETHER a retry starts a new turn; this one pins WHAT that turn
// asks.
func TestAskRetryReRunsTheQuestionWithHistory(t *testing.T) {
	withInstantSpotSearch(t)
	rec := &recordingAsker{events: []answer.Event{
		answer.Retrieved{}, answer.Failed{Reason: "boom"},
	}}
	withAsker(t, rec)
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	p.handleKey(tea.KeyMsg{Type: tea.KeyCtrlR})
	drainAskTicks(t, m, p)

	if len(rec.asked) != 2 {
		t.Fatalf("asked %d times, want 2", len(rec.asked))
	}
	if rec.asked[1].Question != rec.asked[0].Question {
		t.Errorf("retry asked %q, want the original %q", rec.asked[1].Question, rec.asked[0].Question)
	}
	_ = m
}

type recordingAsker struct {
	events []answer.Event
	asked  []answer.Query
}

func (r *recordingAsker) Ask(ctx context.Context, q answer.Query, emit func(answer.Event)) error {
	r.asked = append(r.asked, q)
	for _, ev := range r.events {
		emit(ev)
	}
	return nil
}

// Scrolling up drops follow-the-tail; without that, a user reading the top of
// a long answer is yanked back down by every token.
func TestAskScrollUpDropsFollowAndBottomRestoresIt(t *testing.T) {
	withInstantSpotSearch(t)
	var events []answer.Event
	events = append(events, answer.Retrieved{})
	for i := 0; i < 400; i++ {
		events = append(events, answer.Delta{Text: fmt.Sprintf("line %d\n", i)})
	}
	events = append(events, answer.Done{})
	withAsker(t, &fakeAsker{events: events})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	if !p.follow {
		t.Fatal("a fresh pane follows the tail")
	}
	p.handleKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if p.follow {
		t.Error("scrolling up must drop follow")
	}
	for i := 0; i < 200; i++ {
		p.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if !p.follow {
		t.Error("returning to the bottom must restore follow")
	}
	_ = m
}

// Up and down belong to SOURCES, always -- never to the transcript.
func TestAskArrowsMoveTheSourceCursor(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{
			{ID: "ATM-0001", Kind: "task"}, {ID: "ATM-0002", Kind: "task"},
		}}, answer.Done{},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	p.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if p.cursor != 1 {
		t.Errorf("cursor = %d, want 1", p.cursor)
	}
	p.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if p.cursor != 1 {
		t.Errorf("cursor = %d, want it clamped at the last source", p.cursor)
	}
	p.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if p.cursor != 0 {
		t.Errorf("cursor = %d, want 0", p.cursor)
	}
	_ = m
}

// Backspace edits the input, and does NOT peel the level.
func TestAskBackspaceEditsTheInput(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{answer.Retrieved{}, answer.Done{}}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)
	p.input = "abc"
	p.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if p.input != "ab" {
		t.Errorf("input = %q, want \"ab\"", p.input)
	}
	if m.spotlight.level != levelAsk {
		t.Error("backspace must not leave the level")
	}
}

// Backspace must delete a whole RUNE, not a byte. A byte-slicing
// implementation removes only the last byte of a multi-byte character,
// leaving invalid UTF-8 sitting in p.input -- which then flows through
// strings.TrimSpace into p.question if the user presses Enter before
// noticing.
func TestAskBackspaceRemovesAWholeRuneNotAByte(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{answer.Retrieved{}, answer.Done{}}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)
	p.input = "café"
	p.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if p.input != "caf" {
		t.Errorf("input = %q, want \"caf\"", p.input)
	}
	if !utf8.ValidString(p.input) {
		t.Errorf("input = %q is not valid UTF-8", p.input)
	}
}

// Enter on a source closes the spotlight and opens the task detail -- the same
// call activateTaskAction already makes (spotlight.go:823). The ask session is
// over; reopening the spotlight starts fresh.
func TestAskOpenSourceClosesSpotlightAndOpensDetail(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	id := seedTask(t, m, "ATM", "wire the indexer")
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: id.ID, Kind: "task", Title: "wire the indexer"}}},
		answer.Done{},
	}})

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	p := m.spotlight.ask
	drainAskTicks(t, m, p)

	p.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if m.spotlight.open {
		t.Error("opening a source closes the spotlight")
	}
	if m.spotlight.ask != nil {
		t.Error("the ask session ends when a source is opened")
	}
	if m.focused != paneTasks {
		t.Error("focus must land on the tasks pane")
	}
}

// The click-through is a human judgment, logged apart from any citation.
func TestAskOpenSourceLogsTheClickThrough(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	id := seedTask(t, m, "ATM", "wire the indexer")
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: id.ID, Kind: "task"}}},
		answer.Done{},
	}})

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	p := m.spotlight.ask
	drainAskTicks(t, m, p)
	p.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	entries, err := readInquiriesForTest(t, m, "ATM")
	if err != nil {
		t.Fatalf("read inquiries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("opening a source must log a click-through")
	}
	last := entries[len(entries)-1]
	if last.Query != "indexer" {
		t.Errorf("query = %q, want the question that produced the sources", last.Query)
	}
	if len(last.OpenedIDs) != 1 || last.OpenedIDs[0] != id.ID {
		t.Errorf("OpenedIDs = %v, want [%s]", last.OpenedIDs, id.ID)
	}
	if len(last.CitedIDs) != 0 {
		t.Errorf("CitedIDs = %v, want empty -- an open is not a citation", last.CitedIDs)
	}
}

// A comment source opens the task it belongs to -- a comment has no detail
// view of its own -- but LOGS the comment's own id, not the task's. That id
// space match matters: ReturnedIDs (built from p.sources, i.e. from the hits
// exactly as retrieval returned them) carries the comment id for a comment
// hit, and an OpenedIDs entry the eval can't find in that same turn's
// ReturnedIDs would silently break any correlation between the two.
func TestAskOpenCommentSourceLogsTheCommentIDAndOpensItsTask(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	task := seedTask(t, m, "ATM", "wire the indexer")
	c, err := m.store.CreateComment(task.ID, "see the watcher hookup", nil, "", testActor)
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: c.ID, Kind: "comment"}}},
		answer.Done{},
	}})

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	p := m.spotlight.ask
	drainAskTicks(t, m, p)
	p.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if m.tasks.view != tViewDetail || m.tasks.detail.id != task.ID {
		t.Errorf("a comment source must open its OWNING task; view=%v detail.id=%q, want %q",
			m.tasks.view, m.tasks.detail.id, task.ID)
	}

	entries, err := readInquiriesForTest(t, m, "ATM")
	if err != nil {
		t.Fatalf("read inquiries: %v", err)
	}
	last := entries[len(entries)-1]
	if len(last.OpenedIDs) != 1 || last.OpenedIDs[0] != c.ID {
		t.Errorf("OpenedIDs = %v, want [%s] -- the COMMENT id, not the task it opened", last.OpenedIDs, c.ID)
	}
	if len(last.ReturnedIDs) != 1 || last.ReturnedIDs[0] != c.ID {
		t.Errorf("ReturnedIDs = %v, want [%s]", last.ReturnedIDs, c.ID)
	}
	if last.OpenedIDs[0] != last.ReturnedIDs[0] {
		t.Error("the opened id must appear in this turn's returned_ids -- that is the whole point of logging hit.ID unresolved")
	}
}

// A source whose task another process deleted is not openable. Same re-read
// activateTaskAction does before replaying against a target, for the same
// reason: rows are a snapshot of a store someone else can write to.
func TestAskOpenSourceSurvivesADeletedTask(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: "ATM-dead", Kind: "task"}}},
		answer.Done{},
	}})

	m.spotlight.openSpotlight()
	seedTask(t, m, "ATM", "wire the indexer")
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	p := m.spotlight.ask
	drainAskTicks(t, m, p)

	p.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // must not panic
	// level is not the guard's signal: openSelected never calls setLevel on
	// ANY path (success included), so level alone can't tell a rejected open
	// from an accepted one. open/ask are what the guard actually protects --
	// they are left untouched by the early return, and only flipped/nilled
	// past the re-read on a live target.
	if !m.spotlight.open {
		t.Error("a source that is gone must not close the spotlight")
	}
	if m.spotlight.ask == nil {
		t.Error("a source that is gone must not end the ask session")
	}
}

// readInquiriesForTest reads the click-through log back. ReadInquiries lives
// only on the concrete *store.Store, not on core.Service -- the interface
// deliberately carries just the writer AppendInquiry (internal/core/ask.go:9:
// "a writer can, a reader cannot", because internal/cli must not import
// internal/store). newTestModelWithActor opens a real *store.Store and hands
// it to Model as the Service, so the assertion below always holds in tests.
func readInquiriesForTest(t *testing.T, m *Model, code string) ([]store.InquiryEntry, error) {
	t.Helper()
	s, ok := m.store.(*store.Store)
	if !ok {
		t.Fatalf("m.store is %T, want *store.Store", m.store)
	}
	return s.ReadInquiries(code)
}

// spotSearchK is what retrieval actually asks the store for, and every other
// test in this file scripts one or two hits -- which is how a SOURCES column
// with room for exactly three of them survived every review. Render a full
// retrieval and demand all of it, including the cursor's reach: `down` walks
// onto the last hit whether or not it is drawn, and Enter opens (and logs a
// click-through for) whatever it lands on.
func TestAskRendersEveryRetrievedSource(t *testing.T) {
	withInstantSpotSearch(t)
	hits := make([]core.Hit, 0, spotSearchK)
	for i := 1; i <= spotSearchK; i++ {
		hits = append(hits, core.Hit{ID: fmt.Sprintf("ATM-%04d", i), Kind: "task"})
	}
	withAsker(t, &fakeAsker{events: []answer.Event{answer.Retrieved{Hits: hits}, answer.Done{}}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	view := stripANSI(p.view())
	for i := 1; i <= spotSearchK; i++ {
		mustContain(t, view, fmt.Sprintf("[%d] task ATM-%04d", i, i))
	}

	for i := 0; i < spotSearchK; i++ {
		p.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if p.cursor != spotSearchK-1 {
		t.Fatalf("cursor = %d, want the last of %d sources", p.cursor, spotSearchK)
	}
	mustContain(t, stripANSI(p.view()), fmt.Sprintf("▸ [%d] task ATM-%04d", spotSearchK, spotSearchK))
}

// The ask level's size is derived from the terminal alone (ATM-62adc9). Its
// predecessor rule -- "tab must not resize the box" -- kept the box from
// NARROWING on entry, but it did so by inheriting the entry level's width,
// which made the layout path-dependent: tab from a level with short rows gave
// a cramped ask view on a wide terminal. The invariant is now stronger: the
// ask box is the SAME terminal-derived size from every entry level, and it
// takes the terminal's width rather than the entry level's.
func TestAskBoxIsTerminalSizedFromEveryEntryLevel(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{answer.Retrieved{}, answer.Done{}}})
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "wire the indexer")

	// Entry 1: from the Task group's content search.
	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	drainAskTicks(t, m, m.spotlight.ask)
	taskRows, taskCols := overlaySize(m)
	m.spotlight.ask.stop()
	m.spotlight.ask = nil
	m.spotlight.open = false

	// Entry 2: from the root, whose rows are short labels (or one hint) --
	// the entry that used to produce the cramped view.
	m.spotlight.openSpotlight()
	for _, r := range "indexer" {
		m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	drainAskTicks(t, m, m.spotlight.ask)
	rootRows, rootCols := overlaySize(m)

	if taskCols != rootCols || taskRows != rootRows {
		t.Errorf("ask box = %dx%d from the Task group but %dx%d from the root -- the size must not depend on the entry level", taskCols, taskRows, rootCols, rootRows)
	}
	if want := m.width - 4; taskCols != want {
		t.Errorf("ask box width = %d, want the terminal-derived %d", taskCols, want)
	}
}

// overlaySize is the rendered box's row count and widest line.
func overlaySize(m *Model) (rows, cols int) {
	lines := strings.Split(strings.TrimRight(stripANSI(m.spotlight.renderOverlay()), "\n"), "\n")
	for _, l := range lines {
		if w := len([]rune(l)); w > cols {
			cols = w
		}
	}
	return len(lines), cols
}

// A stop the user chose is sticky. Esc cancels, and the goroutine's own
// terminal event lands behind it -- and that event does not always say
// Canceled: a hydration failure after the cancel reports its own reason
// (answer/engine.go). Overwriting the flag with it tells someone who pressed
// Esc that their answer was interrupted, and offers a retry for a stop they
// made on purpose.
func TestAskEscStaysCanceledWhenTheTerminalEventDisagrees(t *testing.T) {
	withInstantSpotSearch(t)
	block := make(chan struct{})
	withAsker(t, &fakeAsker{block: block, events: []answer.Event{
		answer.Retrieved{}, answer.Delta{Text: "partial"},
		answer.Failed{Reason: "hydrating sources failed", Canceled: false},
	}})
	m, p := openAsk(t, "indexer")
	for i := 0; i < 50 && p.transcript == ""; i++ {
		p.applyTick(askTickMsg{gen: p.gen})
		time.Sleep(time.Millisecond)
	}

	p.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !p.canceled {
		t.Fatal("setup: esc must cancel")
	}
	close(block)
	for i := 0; i < 200 && !p.failed; i++ {
		p.applyTick(askTickMsg{gen: p.gen})
		time.Sleep(time.Millisecond)
	}

	if !p.failed {
		t.Fatal("setup: the scripted Failed never arrived")
	}
	if !p.canceled {
		t.Error("a Failed reporting Canceled:false must not un-cancel a user's own esc")
	}
	if got := p.statusLine(); got != "(canceled)" {
		t.Errorf("status = %q, want the cancel to stand -- no interruption warning, no retry offer", got)
	}
	_ = m
}

// shrinkingAsker returns three sources on its first turn and one on every turn
// after, the shape a retry against a store that changed underneath produces.
type shrinkingAsker struct{ calls int }

func (a *shrinkingAsker) Ask(ctx context.Context, q answer.Query, emit func(answer.Event)) error {
	a.calls++
	n := 3
	if a.calls > 1 {
		n = 1
	}
	hits := make([]core.Hit, 0, n)
	for i := 1; i <= n; i++ {
		hits = append(hits, core.Hit{ID: fmt.Sprintf("ATM-%04d", i), Kind: "task"})
	}
	emit(answer.Retrieved{Hits: hits})
	emit(answer.Failed{Reason: "endpoint dropped"})
	return nil
}

// submit() rehomes the cursor but start() does not, and ctrl+r goes straight
// to start(). A retry that returns fewer sources than the last turn used to
// leave the cursor past the end of the list, where no glyph renders anywhere
// until the user presses up or down.
func TestAskRetryWithFewerSourcesRehomesTheCursor(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &shrinkingAsker{})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	p.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	p.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if p.cursor != 2 {
		t.Fatalf("setup: cursor = %d, want the third of three sources", p.cursor)
	}

	cmd := p.handleKey(tea.KeyMsg{Type: tea.KeyCtrlR})
	if cmd == nil {
		t.Fatal("setup: ctrl+r must retry a failed turn")
	}
	drainAskTicks(t, m, p)

	if len(p.sources) != 1 {
		t.Fatalf("sources = %d, want the retry's single hit", len(p.sources))
	}
	if p.cursor != 0 {
		t.Errorf("cursor = %d, want it rehomed inside the retry's shorter source list", p.cursor)
	}
	mustContain(t, stripANSI(p.view()), "▸ [1] task ATM-0001")
}

// The ask body's line budget is exact: the box is bodyH+6 rows, titledBoxHeight
// spends 2 on borders, and the input takes 1 where the list's search box takes
// 3 -- so the two spare lines are precisely what the status line and the
// staleness chip occupy when BOTH are showing. Anything that adds a row past
// that gets eaten by titledBoxHeight's bodyLines[:innerH], and what it eats is
// the last line: the footer, the only thing on screen naming the level's keys.
// Drive the worst case (a full retrieval, a degraded turn WITH a reason, and a
// non-zero behind count) down to the spotMinBlock floor.
func TestAskFooterSurvivesTheFullLineBudget(t *testing.T) {
	withInstantSpotSearch(t)
	hits := make([]core.Hit, 0, spotSearchK)
	for i := 1; i <= spotSearchK; i++ {
		hits = append(hits, core.Hit{ID: fmt.Sprintf("ATM-%04d", i), Kind: "task"})
	}
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: hits, Behind: 4},
		answer.Done{Degraded: true, Reason: "the chat endpoint returned no answer"},
	}})

	for _, rows := range []int{40, 24, 18, 14, 10} {
		m, p := openAsk(t, "indexer")
		m.SetSize(120, rows)
		drainAskTicks(t, m, p)
		if p.statusLine() == "" || p.stalenessChip() == "" {
			t.Fatalf("rows=%d: setup wants both the status line and the chip, got %q / %q",
				rows, p.statusLine(), p.stalenessChip())
		}

		view := strings.Split(strings.TrimRight(stripANSI(p.view()), "\n"), "\n")
		for _, want := range []string{"esc back", "the chat endpoint returned no answer", "items still indexing"} {
			if !strings.Contains(strings.Join(view, "\n"), want) {
				t.Errorf("rows=%d: %q was truncated out of the box:\n%s", rows, want, strings.Join(view, "\n"))
			}
		}
		// And the footer is the LAST body line, not floating above the border:
		// the status line and the chip are conditional, so without the pad in
		// view() it drifts up by one or two rows with the turn's outcome.
		if n := len(view); n < 2 || !strings.Contains(view[n-2], "esc back") {
			t.Errorf("rows=%d: the footer must sit on the last body line, got %q", rows, view[len(view)-2])
		}
	}
}

// The budget's other half: the case where NEITHER conditional row is showing.
// With the status line and the chip both present the pre-fix arithmetic came
// out exactly right (1 + bodyH + 3 == innerH), so the footer was never at risk
// there -- it was at risk of DRIFTING when they were absent, floating one or
// two rows above the bottom border depending only on how the last turn ended.
// A footer that moves with the outcome reads as the box being broken.
func TestAskFooterSitsOnTheLastRowWithNoStatusOrChip(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: "ATM-0001", Kind: "task"}}, Behind: 0},
		answer.Delta{Text: "the indexer is the watcher"},
		answer.Done{},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)
	if p.statusLine() != "" || p.stalenessChip() != "" {
		t.Fatalf("setup wants a clean turn with no status and no chip, got %q / %q",
			p.statusLine(), p.stalenessChip())
	}

	view := strings.Split(strings.TrimRight(stripANSI(p.view()), "\n"), "\n")
	if n := len(view); n < 2 || !strings.Contains(view[n-2], "esc back") {
		t.Errorf("the footer must sit flush on the last body line, above the border:\n%s",
			strings.Join(view, "\n"))
	}
}

// A follow-up longer than the box must not cost the footer. lipgloss's Width
// WRAPS rather than cuts, so the input rendered as two rows, the body ran one
// line past its budget, and titledBoxHeight dropped the last line it had. The
// caret has to survive the fit too: a field whose head is kept and whose tail
// is cut hides the characters you are currently typing.
func TestAskLongFollowUpDoesNotCostTheFooter(t *testing.T) {
	withInstantSpotSearch(t)
	withAsker(t, &fakeAsker{events: []answer.Event{
		answer.Retrieved{Hits: []core.Hit{{ID: "ATM-0001", Kind: "task"}}}, answer.Done{},
	}})
	m, p := openAsk(t, "indexer")
	drainAskTicks(t, m, p)

	for _, n := range []int{96, 97, 120, 400} {
		p.input = strings.Repeat("x", n-len(" tail")) + " tail"
		view := strings.Split(strings.TrimRight(stripANSI(p.view()), "\n"), "\n")
		if !strings.Contains(strings.Join(view, "\n"), "esc back") {
			t.Errorf("input of %d chars truncated the footer out of the box:\n%s",
				n, strings.Join(view, "\n"))
		}
		if got := len(strings.Split(stripANSI(p.inputBox(m.spotlight.innerWidth())), "\n")); got != 1 {
			t.Errorf("input of %d chars rendered %d rows, want exactly 1", n, got)
		}
		// The tail of what was typed, and the caret, must both still be there.
		if !strings.Contains(strings.Join(view, "\n"), "tail█") {
			t.Errorf("input of %d chars: the caret and the last-typed text must stay visible:\n%s",
				n, view[1])
		}
	}
}

// Every transcript line has to FIT the column it is rendered into, because
// view fitLines whatever it gets and a line one column too wide loses its last
// character with nothing to show it was cut. A bare wordwrap.String overshoots
// its own limit by a column whenever the break lands on a hyphen -- which ATM
// answers are full of, since they cite task ids -- so this pins both halves of
// the invariant: no line over the width, and no text lost getting there.
func TestAskTranscriptWrapsWithoutLosingText(t *testing.T) {
	const prose = "[2], which is why a large project does not pay for a full re-index " +
		"on every keystroke [3]. See ATM-3aafb4 for the debounce window and " +
		"ATM-d25dd8 for the click-through log."
	want := strings.Join(strings.Fields(prose), " ")

	for w := 24; w <= 80; w++ {
		got := wrapAnswer(prose, w)
		for i, l := range got {
			if n := len([]rune(l)); n > w {
				t.Fatalf("w=%d line %d is %d columns wide: %q", w, i, n, l)
			}
		}
		if flat := strings.Join(strings.Fields(strings.Join(got, " ")), " "); flat != want {
			t.Fatalf("w=%d lost or gained text in the wrap:\n got %q\nwant %q", w, flat, want)
		}
	}
}

// Retrieval is scoped to one project. With none in scope store.Search returns
// no hits and no error, so an ask answers from nothing and never says why --
// while the list level, one keystroke away, is honest about it ("select a
// project first"). Both gates are tested: the row must not offer the ask, and
// tab must not take it, because tab reaches enterAsk from levels that never
// render the row.
func TestSpotlightAskNeedsAProjectInScope(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	if m.projectScope != "" {
		t.Fatalf("setup: projectScope = %q, want no project in scope", m.projectScope)
	}

	m.spotlight.openSpotlight()
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if m.spotlight.query == "" {
		t.Fatal("setup: the keystroke must reach the query")
	}
	if m.spotlight.askRowVisible() {
		t.Error("the Ask row must not offer an ask with no project to ask it of")
	}
	if row := m.spotlight.askRow(m.spotlight.innerWidth()); row != "" {
		t.Errorf("askRow = %q, want nothing rendered", stripANSI(row))
	}

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.spotlight.level == levelAsk {
		t.Error("tab must not enter ask mode with no project in scope")
	}
}
