package tui

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"atm/internal/core"
	"atm/internal/dispatch"
	"atm/internal/runtime"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// sessionsModel is the Sessions overlay (spec ATM-9339a7 §9.1): one row per
// registered run, blocked first. Enter jumps to the run's surface, v shows
// its prompt, t opens its task, p prunes. It reads the registry on
// refreshAll's tick so the status bar counts are always current; it never
// writes except prune.
type sessionsModel struct {
	m       *Model
	reg     *runtime.Registry
	entries []runtime.Entry
	loadErr string
	open    bool
	cursor  int
	// prompt is the context-file view over the cursor row; offset scrolls it.
	prompt      bool
	promptLines []string
	offset      int
	// taskFilter narrows the list to one task (opened from task detail).
	taskFilter string
}

// refresh re-reads the registry. Called from refreshAll.
func (s *sessionsModel) refresh() {
	if s.reg == nil {
		s.reg = runtime.Open(s.m.store.StorePath())
	}
	entries, err := s.reg.List()
	if err != nil {
		s.loadErr = err.Error()
		return
	}
	s.loadErr = ""
	sessionsOrder(entries)
	s.entries = entries
	if s.cursor >= len(s.rows()) {
		s.cursor = 0
	}
}

// sessionsOrder: blocked, live, lost, ended, unreadable; newest first within.
func sessionsOrder(entries []runtime.Entry) {
	rank := func(e runtime.Entry) int {
		switch {
		case e.Liveness == runtime.LiveLive && e.Record.Status.State == runtime.StateBlocked:
			return 0
		case e.Liveness == runtime.LiveLive:
			return 1
		case e.Liveness == runtime.LiveLost:
			return 2
		case e.Liveness == runtime.LiveEnded:
			return 3
		}
		return 4
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if ri, rj := rank(entries[i]), rank(entries[j]); ri != rj {
			return ri < rj
		}
		return entries[i].Record.StartedAt > entries[j].Record.StartedAt
	})
}

// rows is the visible list: everything, or the filtered task's runs.
func (s *sessionsModel) rows() []runtime.Entry {
	if s.taskFilter == "" {
		return s.entries
	}
	return s.forTask(s.taskFilter)
}

// forTask is the task-centric view (spec §9.2).
func (s *sessionsModel) forTask(taskID string) []runtime.Entry {
	var out []runtime.Entry
	for _, e := range s.entries {
		if e.Record.Task == taskID {
			out = append(out, e)
		}
	}
	return out
}

// counts feeds the status bar (spec §9.3).
func (s *sessionsModel) counts() (live, blocked int) {
	for _, e := range s.entries {
		if e.Liveness != runtime.LiveLive {
			continue
		}
		live++
		if e.Record.Status.State == runtime.StateBlocked {
			blocked++
		}
	}
	return live, blocked
}

func (s *sessionsModel) openOverlay(taskFilter string) {
	s.taskFilter = taskFilter
	s.refresh()
	s.open, s.prompt, s.offset, s.cursor = true, false, 0, 0
}

func (s *sessionsModel) current() (runtime.Entry, bool) {
	rows := s.rows()
	if s.cursor < 0 || s.cursor >= len(rows) {
		return runtime.Entry{}, false
	}
	return rows[s.cursor], true
}

func (s *sessionsModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		if s.prompt {
			s.prompt = false
			return nil
		}
		s.open = false
	case "j", "down":
		if s.prompt {
			s.offset++
		} else if s.cursor < len(s.rows())-1 {
			s.cursor++
		}
	case "k", "up":
		if s.prompt {
			if s.offset > 0 {
				s.offset--
			}
		} else if s.cursor > 0 {
			s.cursor--
		}
	case "g":
		s.offset = 0
		if !s.prompt {
			s.cursor = 0
		}
	case "r":
		s.refresh()
	case "enter":
		s.jump()
	case "v":
		s.viewPrompt()
	case "t":
		s.openTask()
	case "p":
		res, err := s.reg.Prune(runtime.PruneOptions{})
		if err != nil {
			s.m.showToast("prune: " + err.Error())
			return nil
		}
		s.refresh()
		s.m.showToast(fmt.Sprintf("pruned %d run(s), swept %d orphan prompt(s)", len(res.Removed), len(res.Orphans)))
	}
	return nil
}

// jump brings the cursor row's surface to the front. A surface with no focus
// API is not an error to the user: the toast shows where it is.
func (s *sessionsModel) jump() {
	e, ok := s.current()
	if !ok {
		return
	}
	if s.m.dispatcher == nil {
		s.m.showToast("dispatch unavailable in this build")
		return
	}
	err := s.m.dispatcher.Focus(e.Record.Surface)
	switch {
	case err == nil:
		s.m.showToast("focused " + e.Record.RunID + " · " + surfaceCell(e.Record.Surface))
	case errors.Is(err, dispatch.ErrFocusUnsupported):
		s.m.showToast(surfaceCell(e.Record.Surface) + " has no remote focus API — open it by hand")
	default:
		s.m.showToast("focus " + e.Record.RunID + ": " + err.Error())
	}
}

func (s *sessionsModel) viewPrompt() {
	e, ok := s.current()
	if !ok {
		return
	}
	if e.Record.ContextPath == "" {
		s.m.showToast("no context file recorded for " + e.Record.RunID)
		return
	}
	b, err := os.ReadFile(e.Record.ContextPath)
	if err != nil {
		s.m.showToast("prompt gone: " + err.Error())
		return
	}
	s.promptLines = renderMarkdown(string(b), s.boxWidth()-4)
	s.prompt, s.offset = true, 0
}

// openTask closes the overlay and opens the run's task detail when the task
// belongs to the selected project; the detail pane is project-scoped.
func (s *sessionsModel) openTask() {
	e, ok := s.current()
	if !ok || e.Record.Task == "" {
		return
	}
	if e.Record.Project != s.m.projectScope {
		s.m.showToast("select project " + e.Record.Project + " first")
		return
	}
	s.open = false
	s.m.focused = paneTasks
	s.m.tasks.openDetail(e.Record.Task)
}

// boxWidth: wider than the other overlays on purpose. This one is a TABLE —
// at 70% the STATUS column, the only cell whose content the user cannot
// predict, is the first thing clipped.
func (s *sessionsModel) boxWidth() int {
	bw := s.m.width * 85 / 100
	if bw < 72 {
		bw = 72
	}
	if bw > s.m.width-4 {
		bw = s.m.width - 4
	}
	return bw
}

func (s *sessionsModel) title() string {
	if s.taskFilter != "" {
		return "Sessions · " + s.taskFilter
	}
	return "Sessions"
}

func (s *sessionsModel) renderOverlay() string {
	styles := s.m.styles
	bw := s.boxWidth()
	if s.prompt {
		height := s.m.height - 8
		if height < 8 {
			height = 8
		}
		if s.offset > len(s.promptLines)-1 {
			s.offset = len(s.promptLines) - 1
		}
		if s.offset < 0 {
			s.offset = 0
		}
		end := s.offset + height - 3
		if end > len(s.promptLines) {
			end = len(s.promptLines)
		}
		var body strings.Builder
		for _, ln := range s.promptLines[s.offset:end] {
			body.WriteString(fitLine(ln, bw-4) + "\n")
		}
		body.WriteString("\n" + styles.KeyMenuDim.Render("[j/k]scroll  [Esc]back"))
		e, _ := s.current()
		return titledBoxHeight(styles.DialogBody, bw, "Prompt · "+e.Record.RunID, body.String(), height)
	}
	var body strings.Builder
	body.WriteString(s.previewBody(bw-4) + "\n")
	body.WriteString("\n" + styles.KeyMenuDim.Render("[↑/↓]move  [Enter]jump  [v]prompt  [t]task  [p]prune  [r]reload  [Esc]close"))
	return titledBoxHeight(styles.DialogBody, bw, s.title(), body.String(), len(s.rows())+6)
}

// previewBody is the row list at width w; the spotlight preview renders it
// directly so a preview never shows something the overlay does not.
func (s *sessionsModel) previewBody(w int) string {
	if s.loadErr != "" {
		return fitLine("read sessions: "+s.loadErr, w)
	}
	rows := s.rows()
	if len(rows) == 0 {
		return fitLine("no sessions registered — dispatch one with [D]", w)
	}
	now := core.Now()
	action, age := sessionsColumns(w)
	var b strings.Builder
	b.WriteString(s.m.styles.Muted.Render(fitLine(sessionRowLine("", "STATE", "RUN", "TASK", "PERSONA", "ACTION", "AGENT", "SURFACE", "AGE", "STATUS", action, age), w)) + "\n")
	for i, e := range rows {
		r := e.Record
		line := sessionRowLine(sessionGlyph(e), sessionState(e), shortRunID(r.RunID), orDash(r.Task), r.Persona,
			orDash(r.Checklist), r.Agent, surfaceCell(r.Surface), sessionAge(r, now), sessionStatusText(e), action, age)
		line = fitLine(line, w)
		if i == s.cursor {
			line = s.m.styles.RowCursor.Render(line)
		} else {
			line = toneForSession(s.m, e).Render(line)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// The row's fixed columns. STATUS takes whatever is left, because it is the
// only cell whose content the user cannot predict — "permission: Bash(rm)"
// is the reason the row is worth looking at.
const (
	sessColState   = 8
	sessColRun     = 7
	sessColTask    = 11
	sessColPersona = 9
	sessColAction  = 16
	sessColAgent   = 8
	sessColSurface = 14
	sessColAge     = 7
	// sessStatusFloor: below this, STATUS is too clipped to say anything.
	sessStatusFloor = 24
)

// sessionsColumns decides which optional columns fit. Narrow drops right to
// left — ACTION first, then AGE — the way the setup table does, because a
// clipped STATUS costs the user the one thing the row is for.
func sessionsColumns(w int) (action, age bool) {
	base := 2 + (sessColState + 1) + (sessColRun + 1) + (sessColTask + 1) + (sessColPersona + 1) + (sessColAgent + 1) + (sessColSurface + 1)
	if w-base-(sessColAction+1)-(sessColAge+1) >= sessStatusFloor {
		return true, true
	}
	if w-base-(sessColAge+1) >= sessStatusFloor {
		return false, true
	}
	return false, false
}

func sessionRowLine(glyph, state, run, task, persona, action, agent, surface, age, status string, showAction, showAge bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-1s %-*s %-*s %-*s %-*s", glyph,
		sessColState, truncCell(state, sessColState),
		sessColRun, truncCell(run, sessColRun),
		sessColTask, truncCell(task, sessColTask),
		sessColPersona, truncCell(persona, sessColPersona))
	if showAction {
		fmt.Fprintf(&b, " %-*s", sessColAction, truncCell(action, sessColAction))
	}
	fmt.Fprintf(&b, " %-*s %-*s", sessColAgent, truncCell(agent, sessColAgent), sessColSurface, truncCell(surface, sessColSurface))
	if showAge {
		fmt.Fprintf(&b, " %-*s", sessColAge, truncCell(age, sessColAge))
	}
	b.WriteString(" " + status)
	return b.String()
}

func truncCell(v string, w int) string {
	if len(v) > w {
		return v[:w]
	}
	return v
}

// shortRunID is the minted suffix — the only part that differs between two
// runs of the same project on the same day, and what a human reads a row by.
func shortRunID(runID string) string {
	if i := strings.LastIndex(runID, "-"); i >= 0 && i < len(runID)-1 {
		return runID[i+1:]
	}
	return runID
}

func orDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

func sessionGlyph(e runtime.Entry) string {
	switch {
	case e.Liveness == runtime.LiveLive && e.Record.Status.State == runtime.StateBlocked:
		return "⚠"
	case e.Liveness == runtime.LiveLive:
		return "●"
	case e.Liveness == runtime.LiveLost:
		return "✗"
	case e.Liveness == runtime.LiveEnded:
		return "○"
	}
	return "?"
}

// sessionState: a live run reports what it says it is doing; anything else
// reports what it is, because a dead run's last self-report is a lie.
func sessionState(e runtime.Entry) string {
	if e.Liveness == runtime.LiveLive {
		return string(e.Record.Status.State)
	}
	return string(e.Liveness)
}

func sessionStatusText(e runtime.Entry) string {
	r := e.Record
	switch {
	case e.Liveness == runtime.LiveEnded && r.ExitCode != nil:
		return fmt.Sprintf("exit %d", *r.ExitCode)
	case r.Watch != nil && r.Status.State == runtime.StateWatching:
		last := "never"
		if r.Watch.LastPollAt != "" {
			last = r.Watch.LastPollAt
		}
		return fmt.Sprintf("%s · last poll %s · %d new", r.Watch.Channel, last, r.Watch.ItemsSeen)
	}
	return r.Status.Text
}

func toneForSession(m *Model, e runtime.Entry) lipgloss.Style {
	switch {
	case e.Liveness == runtime.LiveLive && e.Record.Status.State == runtime.StateBlocked:
		return m.styles.Warning
	case e.Liveness == runtime.LiveLost:
		return m.styles.Error
	case e.Liveness == runtime.LiveEnded, e.Liveness == runtime.LiveUnreadable:
		return m.styles.Muted
	}
	return m.styles.Body
}

// surfaceCell: kind plus the id a human would type into that surface's CLI.
// Re-implemented here rather than shared with internal/cli: tui must not
// import cli.
func surfaceCell(s runtime.Surface) string {
	switch s.Kind {
	case "tmux":
		return "tmux " + s.TmuxPane
	case "herdr":
		return "herdr " + s.HerdrPane
	case "kitty":
		return "kitty " + s.KittyWindow
	case "wezterm":
		return "wezterm " + s.WeztermPane
	}
	if s.TTY != "" {
		return "tty " + s.TTY
	}
	return "terminal"
}

// sessionAge: running time for live/lost, time since end for ended.
func sessionAge(r runtime.Record, now time.Time) string {
	stamp := r.StartedAt
	if r.EndedAt != "" {
		stamp = r.EndedAt
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return "?"
	}
	return relTime(t, now)
}
