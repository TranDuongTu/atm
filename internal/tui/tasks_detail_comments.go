package tui

import (
	"fmt"
	"sort"
	"strings"

	"atm/internal/core"
	"github.com/charmbracelet/lipgloss"
)

const (
	// detailCommentsShown is how many comments the DETAILS page digests. It
	// shrinks on a short terminal (see commentsShown) so the sections under
	// it stay on the page; it never grows, because the page is a digest and
	// the thread view is the transcript.
	detailCommentsShown = 3
	// detailThreadHint advertises the thread view from the older-count row.
	detailThreadHint = "[C]"
	// detailCursorMark marks the row the cursor is on.
	detailCursorMark = "▶"
	// detailNoKind is what a comment carrying no ATM:comment:<kind> label
	// reports. Saying so beats an empty column that reads as a render bug.
	detailNoKind = "(no kind)"
)

var (
	detailThreadFooterHints  = []string{"j/k move", "enter open", "esc back"}
	detailCommentFooterHints = []string{"j/k scroll", "H history", "esc back"}
	detailDescFooterHints    = []string{"j/k scroll", "esc back"}
)

// partOfRows is the one topology edge the page carries: the parent named by
// the current capability's Parenter hook — the same pure call the list's
// grouping makes, so the two views cannot drift. No section at all when
// there is no parent; the page does not render empty structure.
func (t *tasksModel) partOfRows(tk *core.Task) []string {
	pid := t.parentOf(tk)
	if pid == "" {
		return nil
	}
	// One GetTask, on the parent the payload already names — never a scan.
	// A parent that no longer resolves still gets its row: the edge is real
	// and the dangling id is the only handle a reader has on it.
	parent, err := t.m.store.GetTask(pid)
	if err != nil {
		return []string{detailRow(t.m.styles, "PART-OF", t.m.styles.Muted.Render(pid+"  (not found)"))}
	}
	value := pid + "  " + parent.Title
	return []string{detailRow(t.m.styles, "PART-OF", truncateRunes(value, t.detailValueWidth()))}
}

// commentsSection appends the COMMENTS digest to page: the caption, the
// latest rows newest-first, and the older-count row that opens the thread.
// Rows are registered as cursor targets as they are written, so the line
// index a row reports is the line it actually occupies.
func (t *tasksModel) commentsSection(page *drillPage, level *drillLevel, tk *core.Task, fixed int) {
	cs, err := t.m.store.ListComments(tk.ID)
	page.lines = append(page.lines, t.captionRows(fmt.Sprintf("COMMENTS  %d", len(cs)))...)
	if err != nil {
		// A listing that failed is a fact about the store, not an absence of
		// comments: rendering "(no comments)" here would state something the
		// page does not know.
		page.lines = append(page.lines,
			taskDetailIndent+t.m.styles.Warning.Render("(comments unavailable: "+err.Error()+")"))
		return
	}
	if len(cs) == 0 {
		page.lines = append(page.lines, taskDetailIndent+t.m.styles.Muted.Render("(no comments)"))
		return
	}
	sortCommentsNewestFirst(cs)
	n := t.commentsShown(fixed, len(cs))
	for _, c := range cs[:n] {
		page.addRow(drillRow{kind: drillComment, id: c.ID}, t.collapsedCommentRows(c, level.cursor == len(page.rows)))
	}
	if older := len(cs) - n; older > 0 {
		page.addRow(drillRow{kind: drillThread, id: tk.ID}, []string{t.olderCommentsRow(older, level.cursor == len(page.rows))})
	}
}

// commentsShown is how many comments fit above the sections that must stay
// visible. fixed is every line the page spends outside the digest; the three
// subtracted lines are the caption pair and the older-count row.
func (t *tasksModel) commentsShown(fixed, total int) int {
	n := detailCommentsShown
	if n > total {
		n = total
	}
	budget := t.drillContentHeight() - fixed - 3
	for n > 1 && n*detailCommentRowLines > budget {
		n--
	}
	return n
}

// detailCommentRowLines is how tall one collapsed comment is: its header row
// and the single body line under it.
const detailCommentRowLines = 2

// collapsedCommentRows renders one comment as the digest shows it: who, when,
// what kind, and the first line of what they said. Never more — the whole
// point of the digest is that reading a comment is a drill-in.
func (t *tasksModel) collapsedCommentRows(c *core.Comment, cursor bool) []string {
	head := fmt.Sprintf("%s   %s   %s", c.CreatedBy, relTime(c.CreatedAt, core.Now()), commentKind(c))
	body := firstLine(c.Body)
	if body == "" {
		body = t.m.styles.Muted.Render("(empty)")
	}
	w := t.detailContentWidth()
	return []string{
		t.cursorPrefix(cursor) + truncateRunes(head, w-len(detailRowIndent)),
		detailRowIndent + "    " + truncateRunes(body, w-len(detailRowIndent)-4),
	}
}

// olderCommentsRow counts what the digest is not showing and points at the
// view that shows it. It is a cursor row itself: enter on it opens the thread.
func (t *tasksModel) olderCommentsRow(older int, cursor bool) string {
	label := fmt.Sprintf("── %d older comments ──", older)
	pad := t.detailContentWidth() - len(detailRowIndent) - lipgloss.Width(label) - lipgloss.Width(detailThreadHint)
	if pad < 1 {
		pad = 1
	}
	return t.cursorPrefix(cursor) + t.m.styles.Muted.Render(label) + spaces(pad) + t.m.styles.FieldHint.Render(detailThreadHint)
}

// detailRowIndent is where a comment row's own text starts. The cursor mark
// is written INTO that indent rather than in front of it, so a marked row
// does not shift sideways against its neighbours.
const detailRowIndent = taskDetailIndent + "  "

// cursorPrefix is the row indent, carrying the cursor mark when the row is
// the selected one.
func (t *tasksModel) cursorPrefix(cursor bool) string {
	if cursor {
		return taskDetailIndent + t.m.styles.HeaderLabel.Render(detailCursorMark) + " "
	}
	return detailRowIndent
}

// threadPage is every comment on the task, in the same collapsed form the
// digest uses, each one a cursor row that drills into the comment.
func (t *tasksModel) threadPage(level *drillLevel) drillPage {
	page := drillPage{}
	cs, err := t.m.store.ListComments(level.id)
	page.lines = append(page.lines, "")
	page.lines = append(page.lines, t.captionRows(fmt.Sprintf("THREAD  %d comments", len(cs)))...)
	switch {
	case err != nil:
		page.lines = append(page.lines,
			taskDetailIndent+t.m.styles.Warning.Render("(comments unavailable: "+err.Error()+")"))
	case len(cs) == 0:
		page.lines = append(page.lines, taskDetailIndent+t.m.styles.Muted.Render("(no comments)"))
	default:
		sortCommentsNewestFirst(cs)
		for _, c := range cs {
			page.addRow(drillRow{kind: drillComment, id: c.ID}, t.collapsedCommentRows(c, level.cursor == len(page.rows)))
		}
	}
	page.lines = append(page.lines, "")
	page.lines = append(page.lines, t.footerRows(detailThreadFooterHints)...)
	return page
}

// commentDrillLines is one comment read in full: its facts, its body as
// rendered markdown, and — when the level's toggle is on — its audit rows.
func (t *tasksModel) commentDrillLines(level *drillLevel) []string {
	c, err := t.m.store.GetComment(level.id)
	if err != nil {
		return []string{"", taskDetailIndent + t.m.styles.Warning.Render("(comment unavailable: "+err.Error()+")")}
	}
	out := []string{""}
	out = append(out, detailRow(t.m.styles, "TASK", c.TaskID))
	out = append(out, detailRow(t.m.styles, "ACTOR", c.CreatedBy))
	out = append(out, detailRow(t.m.styles, "WHEN", core.RFC3339UTC(c.CreatedAt)+"  ("+relTime(c.CreatedAt, core.Now())+")"))
	if c.ReplyTo != "" {
		out = append(out, detailRow(t.m.styles, "REPLY-TO", c.ReplyTo))
	}
	out = append(out, detailRow(t.m.styles, "LABELS", commentKind(c)))
	out = append(out, "")
	out = append(out, t.captionRows("BODY")...)
	out = append(out, t.markdownRows(c.Body)...)
	if level.history {
		out = append(out, "")
		out = append(out, t.captionRows("HISTORY")...)
		out = append(out, t.commentHistoryRows(c.ID)...)
	}
	out = append(out, "")
	out = append(out, t.footerRows(detailCommentFooterHints)...)
	return out
}

// commentHistoryRows is the comment's own audit trail, in the same
// "[seq] time actor action" shape taskHistoryLines uses for a task.
func (t *tasksModel) commentHistoryRows(id string) []string {
	code, _, _, ok := core.ParseCommentID(id)
	if !ok {
		return []string{taskDetailIndent + t.m.styles.Muted.Render("(no history)")}
	}
	hv := t.m.store.History(code, core.Subject{Kind: "comment", ID: id})
	if len(hv) == 0 {
		return []string{taskDetailIndent + t.m.styles.Muted.Render("(no history)")}
	}
	out := make([]string, 0, len(hv))
	for _, e := range hv {
		out = append(out, fmt.Sprintf("%s[%d] %s %s %s",
			taskDetailIndent, e.Seq, core.RFC3339UTC(e.At), e.Actor, e.Action))
	}
	return out
}

// markdownRows renders a body for the drill-in width, at the page indent.
func (t *tasksModel) markdownRows(body string) []string {
	w := t.detailContentWidth() - len(taskDetailIndent)
	lines := renderMarkdown(body, w)
	if len(lines) == 0 {
		return []string{taskDetailIndent + t.m.styles.Muted.Render("(empty)")}
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, taskDetailIndent+line)
	}
	return out
}

// commentKind is the comment's ATM:comment:<kind> label — the one label the
// digest has room for. Comments carry at most one in practice; the first is
// taken rather than guessing among several.
func commentKind(c *core.Comment) string {
	for _, l := range c.Labels {
		if strings.Contains(l, ":comment:") {
			return l
		}
	}
	if len(c.Labels) > 0 {
		return c.Labels[0]
	}
	return detailNoKind
}

// firstLine is the first non-empty line of a body, whitespace-normalised so a
// wrapped paragraph reads as one line in the digest.
func firstLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}

// sortCommentsNewestFirst orders a task's comments for reading. The store
// returns them in id order, which is creation order only under v1 — a v2
// comment id is a content hash, so newest-first has to come from the
// timestamps, with the creation ordinal breaking same-second ties.
func sortCommentsNewestFirst(cs []*core.Comment) {
	sort.SliceStable(cs, func(i, j int) bool {
		if !cs[i].CreatedAt.Equal(cs[j].CreatedAt) {
			return cs[i].CreatedAt.After(cs[j].CreatedAt)
		}
		return cs[i].Ordinal > cs[j].Ordinal
	})
}
