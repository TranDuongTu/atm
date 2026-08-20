package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
	"github.com/muesli/reflow/wrap"
)

// view is the ask level's whole box: the follow-up input across the top, then
// SOURCES beside the transcript, then the staleness chip and the footer.
//
// It goes through the same width and height helpers the list level does --
// "pushes a level over the same spotlight box" -- and those helpers now carry
// the ask level's own cases, because the list's are content-derived and this
// level has no rows and no preview lines to derive from. The width is the
// list's own, carried across on push (leftPaneWidth); the height is whatever
// the terminal leaves after the chrome (blockHeight), since the transcript is
// a scroll window rather than a block of content with a length.
//
// The line budget below is exact and worth counting before adding to it. The
// box is bodyH+6 rows, of which titledBoxHeight spends 2 on borders: bodyH+4
// lines of body. This spends 1 on the input where the list spends 3 on its
// search box, and those 2 spare lines are exactly what the status line and the
// staleness chip take when both are showing. One more unconditional row and
// titledBoxHeight's bodyLines[:innerH] silently eats the footer.
//
// A row does not have to be ADDED to overrun the budget -- an existing one
// growing does it just as well, and more quietly. The input is the one that
// can: it is a single line only because inputBox now bounds it (a lipgloss
// Width wraps rather than cuts, so a long enough follow-up used to render two
// rows and cost the footer). Anything here that takes user text has to be
// fitted to one line before it is counted.
func (p *askPane) view() string {
	sm := p.sm
	st := sm.m.styles
	inner := sm.innerWidth()
	leftW := sm.leftPaneWidth()
	rightW := p.transcriptWidth()
	bodyH := sm.blockHeight()

	var lines []string
	for _, line := range strings.Split(p.inputBox(inner), "\n") {
		lines = append(lines, " "+line)
	}
	left := p.sourceLines(bodyH, leftW)
	right := p.transcriptLines(bodyH, rightW)
	for i := 0; i < bodyH; i++ {
		lines = append(lines, " "+fitLine(left[i], leftW)+spaces(spotPaneGap)+fitLine(right[i], rightW))
	}
	// The status line, the chip and the footer sit together at the bottom, and
	// the two conditional ones must not shove the footer up off the last row
	// when they are absent. The list level's arithmetic is exact and its
	// footer never moves; pad to the same budget rather than letting this
	// level's footer drift by one or two rows with how the last turn ended.
	var tail []string
	if s := p.statusLine(); s != "" {
		tail = append(tail, " "+st.KeyMenuDim.Render(fitLine(s, inner)))
	}
	if chip := p.stalenessChip(); chip != "" {
		tail = append(tail, " "+st.KeyMenuDim.Render(fitLine(chip, inner)))
	}
	tail = append(tail, " "+st.KeyMenuDim.Render(fitLine(p.footer(), inner)))
	for len(lines) < bodyH+4-len(tail) {
		lines = append(lines, "")
	}
	lines = append(lines, tail...)

	return titledBoxHeight(st.DialogBody, sm.menuBoxWidth(), "ask", strings.Join(lines, "\n"), sm.spotlightHeight())
}

// inputBox is where a follow-up is typed: ONE unbordered line carrying the
// list level's searchBox prompt and caret, so the two levels agree about what
// "somewhere you type" looks like without agreeing about its height. The
// border is deliberately absent -- searchBox's three rows would cost the two
// spare lines the status line and the staleness chip live in (see view).
//
// One line is a guarantee, not a hope. lipgloss's Width WRAPS what it cannot
// fit rather than cutting it, so a follow-up longer than the box turned this
// into two rows, pushed the body one line past its budget, and
// titledBoxHeight dropped the last line it had -- the footer, the only thing
// naming this level's keys. A ~100-character question did it at 120x40.
//
// The window scrolls to the TAIL, exactly as searchBox does it (fitLineFrom,
// not fitLine or fitLineTail): both of those keep the HEAD, which in a field
// someone is typing into means the caret and the characters just typed are
// the first things to go. What you are typing has to be what you can see.
func (p *askPane) inputBox(w int) string {
	st := p.sm.m.styles
	// the style's own width, less "> " and the caret
	room := w - 2 - 2 - 1
	in := p.input
	if n := lipgloss.Width(in); n > room {
		in = fitLineFrom(in, n-room, room)
	}
	return st.DialogBody.Width(w - 2).Render(st.KeyMenuDim.Render("> ") + st.Body.Render(in) + "█")
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
	// The header takes a row; the rest is the sources'. blockHeight normally
	// leaves room for all of them, but on a terminal too short for eight hits
	// the column scrolls with the cursor rather than truncating: `down` walks
	// the cursor onto the last hit whether or not it is on screen, and Enter
	// opens -- and logs a click-through for -- whatever it lands on. A source
	// the cursor can reach has to be a source the user can see.
	start := 0
	if room := h - 1; room > 0 && p.cursor >= room {
		start = p.cursor - room + 1
	}
	for i := start; i < len(p.sources) && len(out) < h; i++ {
		hit := p.sources[i]
		// [n] is the hit's position in the whole retrieval, not in this
		// window: it is the number the answer cites.
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
		out = append(out, wrapAnswer(s, w)...)
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

// wrapAnswer wraps one paragraph of the transcript to w columns, and unlike a
// bare wordwrap.String it guarantees every line it returns FITS in w.
//
// Two reasons it cannot be the bare call the rest of the launcher makes.
//
// wordwrap's default breakpoints are ['-'], and on a break at a breakpoint it
// overshoots its own limit by a column. view then fitLines the line to the
// column width and a character of the model's answer is gone with nothing to
// say it was cut -- "…re-index on every keystroke" renders as "…re-index o"
// above "every keystroke". A dropped delta and a dropped column are the same
// hole to the reader, and this level already goes to some length over the
// first one. Clearing Breakpoints removes the overshoot at its cause, and it
// is what we want here anyway: a task id broken across two lines (`ATM-` /
// `3aafb4`) is neither readable nor copyable, and answers cite ids constantly.
//
// wrap is then the hard fallback, for the one case word-wrapping genuinely
// cannot serve: a single token longer than the column, which has to break
// somewhere. Composing the two is what reflow intends; wrap alone would break
// mid-word everywhere.
func wrapAnswer(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	ww := wordwrap.NewWriter(w)
	ww.Breakpoints = nil
	_, _ = ww.Write([]byte(s))
	_ = ww.Close()
	return strings.Split(wrap.String(ww.String(), w), "\n")
}

func (p *askPane) transcriptHeight() int { return p.sm.blockHeight() }

// transcriptWidth is the right column's measure: the inner width less the
// SOURCES column and the gap between them. One function because three callers
// wrapping to three independently-derived widths is how a scroll bound comes
// to disagree with what was actually rendered.
func (p *askPane) transcriptWidth() int {
	return p.sm.innerWidth() - p.sm.leftPaneWidth() - spotPaneGap
}

// scrollToBottom pins the window to the tail.
func (p *askPane) scrollToBottom() {
	if n := len(p.transcriptBody(p.transcriptWidth())) - p.transcriptHeight(); n > 0 {
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
