package tui

import (
	"strconv"
	"strings"

	"atm/internal/capability"
	"github.com/charmbracelet/lipgloss"
)

// laneStripHeight is the fixed height of the lane strip rendered above the
// task list. Fixed, not content-derived: the list's page size is computed
// from it, and a strip that breathed with its content would move the list
// boundary under the user between refreshes.
const laneStripHeight = 5

// laneMinCard is the narrowest a card may be and still show a bordered box.
const laneMinCard = 3

// laneWidths splits the pane among the three cards: Inbox 25%, Pipeline 50%,
// Out 25%. Pipeline takes the remainder so the three always sum to exactly
// the pane width — the strip must never leave a ragged column behind.
func laneWidths(w int) (inbox, pipeline, out int) {
	if w < 3*laneMinCard {
		inbox = w / 3
		out = w / 3
		return inbox, w - inbox - out, out
	}
	inbox = w * 25 / 100
	out = w * 25 / 100
	if inbox < laneMinCard {
		inbox = laneMinCard
	}
	if out < laneMinCard {
		out = laneMinCard
	}
	pipeline = w - inbox - out
	if pipeline < laneMinCard {
		pipeline = laneMinCard
		out = w - inbox - pipeline
	}
	return inbox, pipeline, out
}

// render draws the three lane cards on one row. Positions are fixed, so the
// strip renders all three even with no project scoped or no flow current —
// the pane's shape is a constant the user can rely on, and an empty strip
// would make the list jump the moment a capability resolved.
func (l *lanesModel) render(w int) string {
	inboxW, pipelineW, outW := laneWidths(w)
	cells := []string{
		l.renderCard(inboxW, l.lanes[laneInbox]),
		l.renderCard(pipelineW, l.lanes[lanePipeline]),
		l.renderCard(outW, l.lanes[laneOut]),
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}

// renderCard draws one lane: name in the frame, count (with the Inbox's
// attention signal) on the first body line, and the lane's standing role
// hint below it. The hint is canned rather than data-driven because it
// answers "what is this lane FOR", which never varies by project.
func (l *lanesModel) renderCard(w int, row laneRow) string {
	style := l.m.styles.PaneInactive
	chars := roundedBox
	if row.Kind == l.selected {
		chars = doubleBox
		if !row.Broken {
			style = l.m.styles.PaneActive
		}
	}

	count := "?"
	if !row.Broken {
		count = strconv.Itoa(row.Count)
	}
	title := row.Kind.String() + " " + count
	if row.Kind == laneInbox && !row.Broken && row.Count > 0 {
		title += " " + toneStyle(capability.ToneAttention).Render("●")
	}

	// The count rides in the frame title so all three body lines belong to
	// the hint: the Inbox's hint names the key that acts on it, and a hint
	// clipped before its key is worse than no hint.
	body := wrapAnswer(l.hintFor(row), w-2)
	return titledBoxChars(style, w, title, strings.Join(body, "\n"), laneStripHeight, chars)
}

// hintFor is the lane's role in one line: what the lane means and, where
// there is one, the move it invites.
func (l *lanesModel) hintFor(row laneRow) string {
	if row.Broken {
		return "lane board missing — run seed"
	}
	switch row.Kind {
	case laneInbox:
		return "to triage — dispatch the manager (D)"
	case laneOut:
		return "settled — release to reconsider"
	default:
		name := l.m.capability.current
		if name == "" {
			name = "this capability"
		}
		return "what " + name + " is building"
	}
}
