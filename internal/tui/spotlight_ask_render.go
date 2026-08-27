package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
	"github.com/muesli/reflow/wrap"
)

// view is the ask level's whole box: the follow-up input across the top, then
// two titled panes -- Conversation on the left, Results on the right -- then
// the staleness chip and the footer (ATM-62adc9).
//
// The box is terminal-derived (menuBoxWidth's levelAsk case): nothing here is
// content-sized, so the level takes the terminal's width the same way it
// already takes its height (blockHeight). The Conversation pane holds a
// readable prose measure and the Results pane gets every remaining column --
// titles are what need the room.
//
// The line budget below is exact and worth counting before adding to it. The
// box is bodyH+6 rows, of which titledBoxHeight spends 2 on borders: bodyH+4
// lines of body. This spends 1 on the input where the list spends 3 on its
// search box, and those 2 spare lines are exactly what the status line and the
// staleness chip take when both are showing. One more unconditional row and
// titledBoxHeight's bodyLines[:innerH] silently eats the footer. The two panes
// spend their own borders INSIDE the bodyH rows they are given, so they cannot
// move it.
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
	convW := p.conversationPaneWidth()
	resW := inner - convW - spotPaneGap
	bodyH := sm.blockHeight()

	var lines []string
	for _, line := range strings.Split(p.inputBox(inner), "\n") {
		lines = append(lines, " "+line)
	}
	conv := p.paneLines(convW, "Conversation", p.transcriptLines(bodyH-2, p.transcriptWidth()), bodyH)
	res := p.paneLines(resW, "Results", p.resultLines(bodyH-2, resW-4), bodyH)
	for i := 0; i < bodyH; i++ {
		lines = append(lines, " "+conv[i]+spaces(spotPaneGap)+res[i])
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

// paneLines renders one titled pane and hands back its rows for side-by-side
// composition -- the same idiom previewPanel uses at the list level, so the
// ask level's panes look like the launcher's panes rather than inventing a
// frame of their own. The quiet border style on purpose: this level has no
// pane focus to advertise, so no frame gets to be the loud one. Body rows get
// a one-column gutter; titledBoxChars pads the right edge itself.
func (p *askPane) paneLines(w int, title string, body []string, h int) []string {
	for i, l := range body {
		body[i] = " " + l
	}
	box := titledBoxHeight(p.sm.m.styles.KeyMenuDim, w, title, strings.Join(body, "\n"), h)
	return strings.Split(box, "\n")
}

// resultLines is the Results pane's body: one numbered row per hit, no header
// -- the pane's title is the header now. [n] is the hit's position in THIS
// turn's retrieval, restarting at 1 each turn -- the same rule ATM-d4ceed
// settled for the CLI's cited-sources footer, so a citation means the same
// thing on both surfaces. The kind sits between the number and the id,
// straight from Hit.Kind: results are not only tasks, and a future searchable
// entity displays here with no new code.
//
// blockHeight normally leaves room for every hit, but on a terminal too short
// for eight the pane scrolls with the cursor rather than truncating: `down`
// walks the cursor onto the last hit whether or not it is on screen, and
// Enter opens -- and logs a click-through for -- whatever it lands on. A
// result the cursor can reach has to be a result the user can see.
//
// The cursor treatment mirrors renderListRow (spotlight_render.go) exactly --
// glyph, then text, then pad to width -- so Results looks like just another
// list rather than inventing its own cursor idiom. There is no Styles.Selected
// field; sm.cursorStyle() is the real glyph style the list uses.
func (p *askPane) resultLines(h, w int) []string {
	st := p.sm.m.styles
	out := make([]string, 0, h)
	start := 0
	if h > 0 && p.cursor >= h {
		start = p.cursor - h + 1
	}
	for i := start; i < len(p.sources) && len(out) < h; i++ {
		hit := p.sources[i]
		// [n] is the hit's position in the whole retrieval, not in this
		// window: it is the number the answer cites.
		label := fmt.Sprintf("[%d] %s %s", i+1, hit.Kind, hit.ID)
		if hit.Title != "" {
			label += " " + hit.Title
		}
		glyph, glyphStyle := "  ", st.Body
		if i == p.cursor {
			glyph, glyphStyle = "▸ ", p.sm.cursorStyle()
		}
		// fitLineTail, not fitLine: a title that does not fit ends in an
		// ellipsis, so it reads as cropped rather than as broken mid-word.
		text := fitLineTail(label, w-lipgloss.Width(glyph))
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

// transcriptBody is the LATEST exchange only: the current question, dim,
// above whatever stands in for its answer -- the streamed text, or the
// degraded reply. Older turns do not render (ATM-62adc9): the memory is the
// engine's, which still replays p.turns as history on every follow-up, not
// the pane's. p.transcript survives its turn's Done and is only reset by the
// next start(), so a completed answer stays on screen until a follow-up
// replaces it.
func (p *askPane) transcriptBody(w int) []string {
	st := p.sm.m.styles
	var out []string
	appendWrapped := func(s string) {
		if s == "" {
			return
		}
		out = append(out, wrapAnswer(s, w)...)
	}
	appendWrapped(st.KeyMenuDim.Render("> " + p.question))
	appendWrapped(p.transcript)
	// A degraded turn leaves this pane blank, and the status line alone is
	// one dim row at the bottom in the footer's own style -- quiet enough
	// that the pane read as a plain search-result list (ATM-bc717f). The
	// outcome stands here too, under the question, where the answer would
	// have been: the one place the user is looking. It renders unstyled,
	// exactly as an answer would, because it is written as one -- this level
	// is a conversation, and a terse system line in the answer's place reads
	// as chrome, not as a reply.
	if !p.streaming && p.degraded {
		appendWrapped(p.degradedTranscriptMessage())
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

// transcriptHeight is the Conversation pane's content rows: the block less
// the pane's own two border rows, which it spends inside its allotment.
func (p *askPane) transcriptHeight() int {
	h := p.sm.blockHeight() - 2
	if h < 1 {
		h = 1
	}
	return h
}

// conversationPaneWidth is the Conversation pane's total width, borders
// included. It holds the launcher's prose measure (spotPreviewCols) --
// readability is a property of line length, not of the terminal -- and on a
// terminal too narrow for that it cedes ground, but never takes more than
// 3/5 of the inside: the Results pane is what needs the remaining columns,
// because titles are what the user reads there (ATM-62adc9).
func (p *askPane) conversationPaneWidth() int {
	w := spotPreviewCols + 4 // the measure, two borders, two gutter columns
	if max := p.sm.innerWidth() * 3 / 5; w > max {
		w = max
	}
	if w < 8 {
		w = 8
	}
	return w
}

// transcriptWidth is the Conversation pane's prose measure: its width less
// two borders and two gutter columns. One function because three callers
// wrapping to three independently-derived widths is how a scroll bound comes
// to disagree with what was actually rendered.
func (p *askPane) transcriptWidth() int {
	w := p.conversationPaneWidth() - 4
	if w < 1 {
		w = 1
	}
	return w
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
		return p.degradedMessage()
	}
	return ""
}

// degradedMessage is the status line's terse record of a degraded turn. The
// transcript speaks the same outcome in the assistant's voice
// (degradedTranscriptMessage); both are derived from the same three fields, so
// they cannot disagree about WHAT happened -- only about register. When chat
// was never configured it names the command that would enable answers; when
// chat IS configured that command would fix nothing, so the reason stands
// alone (see statusLine's outcome table).
func (p *askPane) degradedMessage() string {
	if !p.chatConfigured {
		return "no chat model configured · run `atm project set-chat` to enable answers"
	}
	if p.degradedReason != "" {
		return p.degradedReason
	}
	return "no answer generated"
}

// degradedTranscriptMessage is the same outcome written as the reply it stands
// in for: first what WAS delivered (the sources, counted, or the honest
// nothing), then why there is no answer under it. An empty retrieval is a
// sentence, not a count -- "0 related items" narrates the data structure, not
// the situation.
func (p *askPane) degradedTranscriptMessage() string {
	var lead string
	switch n := len(p.sources); n {
	case 0:
		lead = "I looked through this project's ledger and found nothing close enough to answer from."
	case 1:
		lead = "I found 1 related item — it's under Results on the right."
	default:
		lead = fmt.Sprintf("I found %d related items — they're under Results on the right.", n)
	}
	if !p.chatConfigured {
		return lead + " I can't write an answer yet: this project has no chat model configured. Run `atm project set-chat` to give me one."
	}
	if p.degradedReason != "" {
		return lead + " I couldn't generate an answer, though: " + p.degradedReason + "."
	}
	return lead + " I couldn't generate an answer, though."
}

// footer names what Enter means right now, because what Enter means depends on
// whether the input has anything in it.
func (p *askPane) footer() string {
	if strings.TrimSpace(p.input) != "" {
		return "enter ask · ⇅ scroll · esc back"
	}
	return "↑↓ results · enter open · ⇅ scroll · esc back"
}
