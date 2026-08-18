package tui

import (
	"strings"

	"atm/internal/core"

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

	errText string // a store error, surfaced rather than swallowed

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
func (sm *spotlightModel) enterAsk() tea.Cmd {
	q := strings.TrimSpace(sm.query)
	if q == "" {
		return nil
	}
	snap := sm.snapshot()
	sm.setLevel(levelAsk, groupNone)
	sm.ask = &askPane{sm: sm, question: q, snap: snap, follow: true}
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
func (p *askPane) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		return p.sm.peelAsk()
	}
	return nil
}

// start and stop are filled in by the streaming task; the level pushes and
// peels without them.
func (p *askPane) start() tea.Cmd { return nil }
func (p *askPane) stop()          {}
