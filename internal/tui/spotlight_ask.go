package tui

import (
	"context"
	"strings"
	"sync"
	"time"

	"atm/internal/answer"
	"atm/internal/chat"
	"atm/internal/core"
	"atm/internal/embed"

	tea "github.com/charmbracelet/bubbletea"
)

// askTurn is one completed exchange. Turns live for the lifetime of the level
// and no longer: the durable case is `atm ask --session`, whose JSONL history
// belongs to the CLI. (Note the terminology trap ATM-d4ceed recorded: --session
// there is a conversation-history key, not an ATM agent session.)
type askTurn struct {
	question string
	answer   string
}

// askPane owns the spotAsk level. It is a type rather than eight more fields
// on spotlightModel because spotlight.go is already the largest file in its
// cluster, and because everything here -- a goroutine, a transcript, a second
// text input -- is unlike anything the tree levels do.
type askPane struct {
	sm *spotlightModel

	question string
	turns    []askTurn
	input    string

	sources []core.Hit
	behind  int

	transcript string
	streaming  bool

	cursor int  // SOURCES cursor
	offset int  // transcript scroll
	follow bool // pinned to the tail; dropped when the user scrolls up

	// leftW is the list's left-pane width at the instant this level was
	// pushed, so the box does not narrow on Tab. leftPaneWidth reads it; see
	// its comment for why the column is inherited rather than re-measured.
	leftW int

	// gen retires ticks belonging to a stream the user has moved on from --
	// the same guard sm.searchGen provides on the search path. stream is the
	// goroutine-to-UI accumulator for the generation currently running; cancel
	// stops it.
	gen    int
	stream *askStream
	cancel context.CancelFunc

	// degraded means the turn completed with sources but no generated answer
	// (no chat model configured, or one that never produced a delta).
	// failed/canceled cover a turn that broke instead of completing --
	// canceled distinguishes the user's own Esc/ctrl-C from an endpoint that
	// dropped mid-answer, mirroring answer.Failed.
	degraded       bool
	degradedReason string
	failed         bool
	failedReason   string
	canceled       bool

	// chatConfigured is captured from askEngineFor at the top of start(), not
	// re-derived from degradedReason: a degraded turn's reason is free text
	// (the stream error, or "the chat endpoint returned no answer") and
	// string-matching it to guess whether chat was configured would be
	// fragile. statusLine uses this to decide whether the degraded hint
	// should tell the user to configure chat (nothing is set up) or just show
	// the reason (chat IS configured; something else went wrong, and there is
	// no config for the user to fix).
	chatConfigured bool

	// snap is where the list was standing when this level was pushed. Peel
	// replays it through openAt, which re-runs the search, rebuilds the rows,
	// rehomes a stale cursor, and -- via restorableLevel -- degrades a level
	// when the task behind the snapshot was deleted while the user was asking.
	snap spotlightSnapshot
}

// enterAsk pushes the ask level, carrying the query across as the question.
//
// setLevel clears sm.query unconditionally, and its doc comment says that
// discipline is the point: a caller needing the old query reads it into a local
// first. escPeel and activate both do exactly this, and so does this -- rather
// than growing an exception inside setLevel.
//
// The project gate repeats askRowVisible's rather than deferring to it: tab
// reaches here from every level, including the ones that never render the row,
// and an ask with no project in scope retrieves from nothing while reporting no
// error at all (see askRowVisible for the whole reason).
//
// leftPaneWidth is read BEFORE setLevel, for the same reason the query is:
// setLevel rebuilds, and the rows the width is measured from are gone after it.
func (sm *spotlightModel) enterAsk() tea.Cmd {
	q := strings.TrimSpace(sm.query)
	if q == "" || sm.m.projectScope == "" {
		return nil
	}
	snap := sm.snapshot()
	leftW := sm.leftPaneWidth()
	sm.setLevel(levelAsk, groupNone)
	sm.ask = &askPane{sm: sm, question: q, snap: snap, follow: true, leftW: leftW}
	return sm.ask.start()
}

// peelAsk drops the level and puts the list back exactly where it was.
func (sm *spotlightModel) peelAsk() tea.Cmd {
	p := sm.ask
	if p == nil {
		return nil
	}
	p.stop()
	snap := p.snap
	sm.ask = nil
	return sm.openAt(snap)
}

// handleKey is the ask level's whole keymap. The level forks here at the top of
// spotlightModel.handleKey rather than threading cases through the tree's
// switch: almost every key means something different inside the pane, so a
// shared switch would be a list of exceptions.
//
// Enter is overloaded and the input's emptiness disambiguates it: with text to
// send, Enter sends; with none, Enter opens the selected source. One key, no
// mode, and unambiguous at every moment, because the input either has text or
// it does not.
//
// Retry is ctrl+r rather than the umbrella spec's Enter. Enter already carries
// two meanings, and a third would take effect precisely in the state where the
// user is most likely to want a source instead -- the answer just broke, and
// the retrieved sources are the fallback. A bare `r` cannot serve: printable
// runes belong to the input.
//
// ctrl+c is not a case here: dispatchKey handles it as the global quit
// (app.go) and never reaches the spotlight with it, so a case in this switch
// would be dead code that reads as though the pane deliberately swallows quit.
func (p *askPane) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		// Two states deep and no deeper. The input never absorbs esc.
		if p.streaming {
			p.stop()
			p.canceled = true
			return nil
		}
		return p.sm.peelAsk()
	case "enter":
		if strings.TrimSpace(p.input) != "" {
			return p.submit()
		}
		return p.openSelected()
	case "ctrl+r":
		if p.failed && !p.canceled {
			return p.start()
		}
	case "up":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down":
		if p.cursor < len(p.sources)-1 {
			p.cursor++
		}
	case "pgup", "ctrl+u":
		p.scroll(-p.transcriptHeight())
	case "pgdown", "ctrl+d":
		p.scroll(p.transcriptHeight())
	case "backspace":
		if r := []rune(p.input); len(r) > 0 {
			p.input = string(r[:len(r)-1])
		}
	default:
		if r, ok := printableRune(k); ok {
			p.input += string(r)
		}
	}
	return nil
}

// submit starts a new turn from the input. Retrieval re-runs, so SOURCES and
// [n] belong to the new turn alone.
func (p *askPane) submit() tea.Cmd {
	p.stop()
	p.question = strings.TrimSpace(p.input)
	p.input = ""
	p.sources = nil
	p.cursor = 0
	return p.start()
}

// scroll moves the transcript window. Moving off the bottom drops
// follow-the-tail, and landing back on it restores it: a user reading the top
// of a long answer must not be yanked down by every token, and a user who has
// caught up should not have to keep pressing a key to stay caught up.
func (p *askPane) scroll(delta int) {
	max := len(p.transcriptBody(p.transcriptWidth())) - p.transcriptHeight()
	if max < 0 {
		max = 0
	}
	p.offset += delta
	if p.offset < 0 {
		p.offset = 0
	}
	if p.offset > max {
		p.offset = max
	}
	p.follow = p.offset >= max
}

// openSelected opens the highlighted source's task and ends the ask session.
//
// The same shape activateTaskAction uses (spotlight.go:823): re-read the
// target first, then close the launcher and hand off to the tasks pane. No
// spotlightReturn is recorded -- opening a source is the end of the ask, not
// a detour out of it, which is why spotlightSnapshot never has to learn how
// to carry a transcript.
func (p *askPane) openSelected() tea.Cmd {
	if p.cursor < 0 || p.cursor >= len(p.sources) {
		return nil
	}
	hit := p.sources[p.cursor]
	id := hit.ID
	m := p.sm.m
	if hit.Kind == "comment" {
		// A comment is how the source was found, not a thing with a detail view
		// of its own -- the same rule a comment row follows in the list.
		c, err := m.store.GetComment(hit.ID)
		if err != nil {
			m.showToast(err.Error())
			return nil
		}
		id = c.TaskID
	}
	// Re-read: the rows are a snapshot of a store another process can write
	// to, same reason activateTaskAction re-reads before replaying.
	if _, err := m.store.GetTask(id); err != nil {
		m.showToast("task " + id + " is gone")
		return nil
	}
	// Log hit.ID, not id: returned (built below from p.sources) is the
	// candidate set exactly as retrieval returned it, and for a comment hit
	// that's the COMMENT id -- id has already been resolved to the comment's
	// TASK id, for openDetail below, which has no comment view of its own.
	// Logging the resolved task id here would put an opened_ids entry absent
	// from that same turn's returned_ids, breaking any eval correlating the
	// two. cli/ask.go's idsOf never resolves comment ids to task ids either,
	// for the same reason: one id space, one log. Do not "fix" this to id.
	p.logClickThrough(hit.ID)
	p.stop()
	m.spotlight.ask = nil
	m.spotlight.open = false
	m.focused = paneTasks
	return m.tasks.openDetail(id)
}

// logClickThrough records the human judgment: this question produced these
// sources, and the user opened this one. id is the SOURCE's id, exactly as
// retrieval returned it -- for a comment hit that's the comment id, not the
// task openSelected navigates to (see the call site). Kept out of CitedIDs,
// which is the model's opinion rather than a person's (ATM-028a8d weighs
// them differently).
//
// A logging failure must never cost the user their navigation, so it is
// toasted rather than kept on the pane -- openSelected sets sm.ask = nil and
// closes the spotlight in the same call, so anything recorded here would live
// on a pane about to be discarded and would never reach the screen. showToast
// lives on the Model and renders after the spotlight closes, the lifetime this
// path needs. Either way, the open proceeds.
func (p *askPane) logClickThrough(id string) {
	m := p.sm.m
	returned := make([]string, 0, len(p.sources))
	for _, h := range p.sources {
		returned = append(returned, h.ID)
	}
	if err := m.store.AppendInquiry(m.projectScope, p.question, returned, nil, []string{id}); err != nil {
		m.showToast(err.Error())
	}
}

// askStream is the goroutine-to-UI boundary.
//
// The indexer streams progress over a buffered channel with NON-BLOCKING sends
// that drop on overflow (indexer.go:768-780). That is correct there -- the
// newest progress line supersedes the last -- and wrong here: a dropped Delta
// is a hole in the middle of the answer, invisible to the reader.
//
// So deltas coalesce into a builder instead. It cannot drop, it batches to the
// frame rate rather than re-rendering per token, and its memory is bounded by
// the length of the answer itself.
type askStream struct {
	mu        sync.Mutex
	retrieved bool
	sources   []core.Hit
	behind    int
	text      strings.Builder
	terminal  answer.Event
	done      bool
}

func (s *askStream) emit(ev answer.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		// Ask both emits a terminal event AND returns an error when the ledger
		// itself failed; the goroutine below emits on a non-nil error too. First
		// terminal event wins, so the second is not mistaken for a new one.
		return
	}
	switch e := ev.(type) {
	case answer.Retrieved:
		s.retrieved, s.sources, s.behind = true, e.Hits, e.Behind
	case answer.Delta:
		s.text.WriteString(e.Text)
	case answer.Done, answer.Failed:
		s.terminal, s.done = ev, true
	default:
		// Any event added to the Event interface later that is neither of the
		// above is ignored rather than treated as terminal -- a terminal
		// default would flip s.done early and silently drop every delta that
		// followed, turning "truncated" into "finished".
	}
}

// drain hands over everything accumulated since the last call, emptying the
// text buffer so the same bytes are never applied twice.
func (s *askStream) drain() (retrieved bool, sources []core.Hit, behind int, text string, terminal answer.Event, done bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	text = s.text.String()
	s.text.Reset()
	return s.retrieved, s.sources, s.behind, text, s.terminal, s.done
}

// askAsker is answer.Engine narrowed to the one method the pane calls, so
// tests script event sequences without an endpoint. Same injected-seam design
// internal/setup uses for probes.
type askAsker interface {
	Ask(ctx context.Context, q answer.Query, emit func(answer.Event)) error
}

// askEngineFor builds the engine for a spotlight, plus whether chat is
// configured for it. A package var because it is the test seam; production
// never reassigns it.
//
// The bool travels alongside the asker rather than being re-derived later
// from a Done.Reason string: statusLine needs to know, for a degraded turn,
// whether the fix is "configure chat" or something the user cannot fix by
// running a command, and the config is only known here.
var askEngineFor = func(sm *spotlightModel) (askAsker, bool) {
	m := sm.m
	acfg := answer.Config{Project: m.projectScope, Searcher: m.store, K: spotSearchK}
	configured := false
	if cfg, err := m.store.GetProjectConfig(m.projectScope); err == nil && cfg != nil {
		if cfg.Embedding != nil {
			client := embed.New(*cfg.Embedding)
			acfg.Embed = func(ctx context.Context, text, role string) ([]float64, error) {
				return client.Embed(ctx, text, role)
			}
			acfg.Model = cfg.Embedding.Model
			acfg.Threshold = cfg.Embedding.Threshold
		}
		// Assigned ONLY inside the branch, exactly as cli/ask.go does it and for
		// the same reason: declaring the client above and assigning it
		// unconditionally puts a TYPED NIL in the interface, so Config.Chat != nil
		// is true for a project with no chat model -- turning a clean degrade into
		// a nil-pointer panic.
		if cfg.Chat != nil {
			acfg.Chat = chat.New(*cfg.Chat)
			configured = true
		}
	}
	return answer.New(acfg), configured
}

var askTickInterval = 60 * time.Millisecond

// askTickMsg drains the accumulator. gen retires ticks belonging to a stream
// the user has moved on from -- the same guard searchGen provides on the
// search path.
type askTickMsg struct{ gen int }

func askTickCmd(gen int) tea.Cmd {
	return tea.Tick(askTickInterval, func(time.Time) tea.Msg { return askTickMsg{gen: gen} })
}

// start runs one turn. Cancellation is the user's alone: internal/chat's
// watchdogs cancel a DERIVED context, never the caller's, so a watchdog abort
// can never arrive here looking like an Esc.
func (p *askPane) start() tea.Cmd {
	// A second start() on the same pane must not overwrite p.cancel out from
	// under a still-running goroutine: that would orphan it against a live
	// endpoint with nothing left able to cancel it.
	p.stop()
	p.gen++
	gen := p.gen
	st := &askStream{}
	p.stream = st
	p.streaming = true
	p.transcript = ""
	p.degraded, p.failed, p.canceled = false, false, false
	p.offset, p.follow = 0, true

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	q := answer.Query{Question: p.question, History: p.history()}
	eng, configured := askEngineFor(p.sm)
	p.chatConfigured = configured
	go func() {
		if err := eng.Ask(ctx, q, st.emit); err != nil {
			// Ask returns ErrUsage for an empty question and emits NOTHING, so a
			// consumer driven purely by events would never see it. emit's done
			// guard keeps this from overwriting a real terminal event.
			st.emit(answer.Failed{Reason: err.Error()})
		}
	}()
	return askTickCmd(gen)
}

// history is the conversation replayed to the model on a follow-up.
func (p *askPane) history() []answer.Turn {
	out := make([]answer.Turn, 0, len(p.turns))
	for _, t := range p.turns {
		out = append(out, answer.Turn{Question: t.question, Answer: t.answer})
	}
	return out
}

func (p *askPane) stop() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.streaming = false
}

// applyTick drains one frame's worth of stream into the pane, rescheduling
// itself while the answer is still coming.
func (p *askPane) applyTick(msg askTickMsg) tea.Cmd {
	if p.stream == nil || msg.gen != p.gen {
		return nil
	}
	retrieved, sources, behind, text, terminal, done := p.stream.drain()
	if retrieved {
		p.sources, p.behind = sources, behind
		// Retrieval re-runs every turn and a retry can return fewer hits than
		// the last one, so the cursor is rehomed here rather than only in
		// submit(): ctrl+r goes straight to start(), and a cursor left past
		// the end renders no glyph anywhere until the user presses up or down.
		if p.cursor >= len(p.sources) {
			p.cursor = len(p.sources) - 1
		}
		if p.cursor < 0 {
			p.cursor = 0
		}
	}
	if text != "" {
		p.transcript += text
		if p.follow {
			p.scrollToBottom()
		}
	}
	if !done {
		return askTickCmd(msg.gen)
	}
	p.streaming = false
	switch e := terminal.(type) {
	case answer.Done:
		p.degraded, p.degradedReason = e.Degraded, e.Reason
	case answer.Failed:
		p.failed, p.failedReason = true, e.Reason
		// A user stop is sticky. Esc sets canceled and then the goroutine's
		// own terminal event arrives behind it, and that event does not always
		// carry Canceled -- a hydration failure after the cancel reports its
		// own reason (answer/engine.go). Assigning e.Canceled unconditionally
		// therefore tells a user who pressed Esc that the answer was
		// interrupted and offers them a retry for a stop they chose.
		p.canceled = p.canceled || e.Canceled
	}
	// Recorded only when there is an answer to record, mirroring cli/ask.go's
	// rule (ATM-d4ceed): a degraded turn generated nothing, and an empty
	// assistant turn replayed as history poisons every later turn. A
	// TRUNCATED partial IS recorded -- that text is genuinely what the
	// conversation contained. A CANCELED partial is not: a cancel is the user
	// rejecting the answer, not the clock running out on one they still
	// wanted, so it must not be replayed as the assistant's real prior reply
	// on the next turn.
	if !p.canceled && strings.TrimSpace(p.transcript) != "" {
		p.turns = append(p.turns, askTurn{question: p.question, answer: p.transcript})
	}
	return nil
}
