package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

// view is the ask level's whole box: the follow-up input across the top, then
// SOURCES beside the transcript, then the staleness chip and the footer.
//
// It reuses the list level's width and height helpers rather than computing its
// own, so the box does not visibly resize when the level is pushed -- the
// umbrella spec's "pushes a level over the same spotlight box".
func (p *askPane) view() string {
	sm := p.sm
	st := sm.m.styles
	inner := sm.innerWidth()
	leftW := sm.leftPaneWidth()
	rightW := inner - leftW - spotPaneGap
	bodyH := sm.blockHeight()

	var b strings.Builder
	for _, line := range strings.Split(p.inputBox(inner), "\n") {
		b.WriteString(" " + line + "\n")
	}
	left := p.sourceLines(bodyH, leftW)
	right := p.transcriptLines(bodyH, rightW)
	for i := 0; i < bodyH; i++ {
		b.WriteString(" " + fitLine(left[i], leftW) + spaces(spotPaneGap) + fitLine(right[i], rightW) + "\n")
	}
	if s := p.statusLine(); s != "" {
		b.WriteString(" " + st.KeyMenuDim.Render(fitLine(s, inner)) + "\n")
	}
	if chip := p.stalenessChip(); chip != "" {
		b.WriteString(" " + st.KeyMenuDim.Render(fitLine(chip, inner)) + "\n")
	}
	b.WriteString(" " + st.KeyMenuDim.Render(fitLine(p.footer(), inner)))

	return titledBoxHeight(st.DialogBody, sm.menuBoxWidth(), "ask", b.String(), sm.spotlightHeight())
}

// inputBox mirrors the list level's searchBox: a bordered field with a caret,
// so the two levels do not disagree about what "somewhere you type" looks like.
func (p *askPane) inputBox(w int) string {
	st := p.sm.m.styles
	return st.DialogBody.Width(w - 2).Render(st.KeyMenuDim.Render("> ") + st.Body.Render(p.input) + "█")
}

// sourceLines is the left column: a header, then one numbered row per hit.
// [n] is the hit's position in THIS turn's retrieval, restarting at 1 each
// turn -- the same rule ATM-d4ceed settled for the CLI's cited-sources footer,
// so a citation means the same thing on both surfaces.
//
// The cursor treatment mirrors renderListRow (spotlight_render.go) exactly --
// glyph, then text, then pad to width -- so SOURCES looks like just another
// list rather than inventing its own cursor idiom. There is no Styles.Selected
// field; sm.cursorStyle() is the real glyph style the list uses.
func (p *askPane) sourceLines(h, w int) []string {
	st := p.sm.m.styles
	out := make([]string, 0, h)
	out = append(out, st.KeyMenuDim.Render("SOURCES"))
	for i, hit := range p.sources {
		label := fmt.Sprintf("[%d] %s", i+1, hit.ID)
		if hit.Title != "" {
			label += " " + hit.Title
		}
		glyph, glyphStyle := "  ", st.Body
		if i == p.cursor {
			glyph, glyphStyle = "▸ ", p.sm.cursorStyle()
		}
		text := fitLine(label, w-lipgloss.Width(glyph))
		pad := w - lipgloss.Width(glyph) - lipgloss.Width(text)
		out = append(out, glyphStyle.Render(glyph)+st.Body.Render(text)+spaces(pad))
	}
	for len(out) < h {
		out = append(out, "")
	}
	return out[:h]
}

// transcriptLines is the right column, wrapped to width and windowed by the
// scroll offset.
func (p *askPane) transcriptLines(h, w int) []string {
	all := p.transcriptBody(w)
	if p.offset > len(all)-h {
		p.offset = len(all) - h
	}
	if p.offset < 0 {
		p.offset = 0
	}
	out := make([]string, 0, h)
	for i := p.offset; i < len(all) && len(out) < h; i++ {
		out = append(out, all[i])
	}
	for len(out) < h {
		out = append(out, "")
	}
	return out
}

// transcriptBody is every turn, oldest first, each question above its answer.
// The question stays because [n] restarts every turn: an older answer's numbers
// refer to a source list that is no longer on screen, and the question is what
// makes that answer legible without them.
func (p *askPane) transcriptBody(w int) []string {
	st := p.sm.m.styles
	var out []string
	appendWrapped := func(s string) {
		if s == "" {
			return
		}
		out = append(out, strings.Split(wordwrap.String(s, w), "\n")...)
	}
	for _, t := range p.turns {
		appendWrapped(st.KeyMenuDim.Render("> " + t.question))
		appendWrapped(t.answer)
		out = append(out, "")
	}
	// recorded mirrors applyTick's own append condition (spotlight_ask.go)
	// exactly, rather than approximating it with len(p.turns) == 0: that
	// approximation reads "any turn was ever recorded" as "the current turn
	// was recorded", which only holds while there is one turn. A SECOND turn
	// that ends canceled or degraded is never appended to p.turns, and with
	// the approximation the live block was suppressed anyway -- the question
	// and any partial answer vanished with no trace the user ever asked it.
	recorded := !p.canceled && strings.TrimSpace(p.transcript) != ""
	if p.streaming || !recorded {
		appendWrapped(st.KeyMenuDim.Render("> " + p.question))
		appendWrapped(p.transcript)
	}
	return out
}

func (p *askPane) transcriptHeight() int { return p.sm.blockHeight() }

// scrollToBottom pins the window to the tail.
func (p *askPane) scrollToBottom() {
	w := p.sm.innerWidth() - p.sm.leftPaneWidth() - spotPaneGap
	if n := len(p.transcriptBody(w)) - p.transcriptHeight(); n > 0 {
		p.offset = n
		return
	}
	p.offset = 0
}

// stalenessChip renders Retrieved.Behind. Deliberately NOT the word "behind":
// event.go:26 records that this count and the dock's event-log delta disagree
// for the same project at the same instant, and for a project with no index at
// all this one counts the whole corpus while the CLI shows nothing. Two
// surfaces printing different values for what reads as one idea is worse than
// one surface staying quiet.
func (p *askPane) stalenessChip() string {
	if p.behind <= 0 {
		return ""
	}
	return fmt.Sprintf("sources may lag · %d items still indexing", p.behind)
}

// statusLine says how the last turn ended. Three outcomes that must not look
// alike:
//
//	degraded    -- succeeded with no answer in it. Sources stand. When chat
//	               was never configured the hint names the command that would
//	               enable answers; when chat IS configured (an unreachable
//	               endpoint, or one that answered with nothing) that command
//	               would fix nothing, so the reason stands alone instead
//	               (engine.go:146 states the same rule: "calling it degraded
//	               would send a consumer to fix a chat config that is fine").
//	canceled    -- the user's own Esc. Not an error, and no retry offered:
//	               they chose to stop.
//	interrupted -- a disconnect or an expired deadline, deliberately
//	               indistinguishable at the event level because both warrant
//	               the same response. Only Reason says which.
func (p *askPane) statusLine() string {
	switch {
	case p.streaming:
		return ""
	case p.canceled:
		return "(canceled)"
	case p.failed:
		return "⚠ answer interrupted (" + p.failedReason + ") · ctrl+r to retry"
	case p.degraded:
		if !p.chatConfigured {
			return "no chat model configured · run `atm project set-chat` to enable answers"
		}
		if p.degradedReason != "" {
			return p.degradedReason
		}
		return "no answer generated"
	}
	return ""
}

// footer names what Enter means right now, because what Enter means depends on
// whether the input has anything in it.
func (p *askPane) footer() string {
	if strings.TrimSpace(p.input) != "" {
		return "enter ask · ⇅ scroll · esc back"
	}
	return "↑↓ sources · enter open · ⇅ scroll · esc back"
}
