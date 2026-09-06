package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"atm/internal/core"
	"atm/internal/runtime"

	"github.com/spf13/cobra"
)

// newSessionCmd is the CLI face of the session registry (spec ATM-9339a7
// §9.5). Reads need the store only for its path; status and prune write the
// registry, never the ledger, so they take no --actor.
func newSessionCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Live agent sessions: who is on which task, where, and in what state",
		Long: "Every `atm --persona` launch registers a run under <store>/runtime/sessions/.\n" +
			"list and show read those records; status is what agent hooks call to report\n" +
			"working/idle/blocked/watching; prune applies the retention rules.",
	}
	cmd.AddCommand(newSessionListCmd(st), newSessionShowCmd(st), newSessionStatusCmd(st), newSessionPruneCmd(st))
	return cmd
}

func (st *cliState) openRegistry() (*runtime.Registry, error) {
	s, err := st.openStore()
	if err != nil {
		return nil, err
	}
	return runtime.Open(s.StorePath()), nil
}

// sessionRow is one listed run, flattened for text and JSON alike.
type sessionRow struct {
	RunID       string         `json:"run_id"`
	Liveness    string         `json:"liveness"`
	State       string         `json:"state"`
	StatusText  string         `json:"status_text,omitempty"`
	StatusAt    string         `json:"status_at,omitempty"`
	Project     string         `json:"project,omitempty"`
	Task        string         `json:"task,omitempty"`
	Persona     string         `json:"persona"`
	Checklist   string         `json:"checklist,omitempty"`
	Agent       string         `json:"agent"`
	Model       string         `json:"model,omitempty"`
	Surface     string         `json:"surface"`
	Host        string         `json:"host,omitempty"`
	StartedAt   string         `json:"started_at"`
	EndedAt     string         `json:"ended_at,omitempty"`
	ExitCode    *int           `json:"exit_code,omitempty"`
	ContextPath string         `json:"context_path,omitempty"`
	Watch       *runtime.Watch `json:"watch,omitempty"`
}

func rowOf(e runtime.Entry) sessionRow {
	r := e.Record
	return sessionRow{RunID: r.RunID, Liveness: string(e.Liveness), State: string(r.Status.State), StatusText: r.Status.Text, StatusAt: r.Status.At,
		Project: r.Project, Task: r.Task, Persona: r.Persona, Checklist: r.Checklist, Agent: r.Agent, Model: r.Model,
		Surface: formatSurface(r.Surface), Host: r.Host, StartedAt: r.StartedAt, EndedAt: r.EndedAt, ExitCode: r.ExitCode,
		ContextPath: r.ContextPath, Watch: r.Watch}
}

// formatSurface is the one-cell locator: kind plus the id a human would type
// into that surface's own CLI.
func formatSurface(s runtime.Surface) string {
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

// sessionOrder is the list order: blocked first, then live, lost, ended,
// unreadable; newest first inside each group (spec §9.1). Blocked is first
// because it is the only state that wants a human right now.
func sessionOrder(entries []runtime.Entry) {
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
		ri, rj := rank(entries[i]), rank(entries[j])
		if ri != rj {
			return ri < rj
		}
		return entries[i].Record.StartedAt > entries[j].Record.StartedAt
	})
}

// sessionAge is how long the run has been going (live/lost) or how long ago
// it ended.
func sessionAge(r runtime.Record, now time.Time) string {
	stamp := r.StartedAt
	if r.EndedAt != "" {
		stamp = r.EndedAt
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return "?"
	}
	d := now.Sub(t).Round(time.Minute)
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func newSessionListCmd(st *cliState) *cobra.Command {
	var project, task string
	var live bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered runs, blocked and live first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := st.openRegistry()
			if err != nil {
				return err
			}
			entries, err := reg.List()
			if err != nil {
				return err
			}
			var kept []runtime.Entry
			for _, e := range entries {
				if project != "" && e.Record.Project != project {
					continue
				}
				if task != "" && e.Record.Task != task {
					continue
				}
				if live && e.Liveness != runtime.LiveLive {
					continue
				}
				kept = append(kept, e)
			}
			sessionOrder(kept)
			if st.isJSON() {
				rows := make([]sessionRow, 0, len(kept))
				for _, e := range kept {
					rows = append(rows, rowOf(e))
				}
				return writeJSON(st.stdout(), map[string]any{"sessions": rows})
			}
			writeSessionTable(st, kept)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "only runs bound to this project")
	cmd.Flags().StringVar(&task, "task", "", "only runs bound to this task")
	cmd.Flags().BoolVar(&live, "live", false, "only live runs")
	return cmd
}

func writeSessionTable(st *cliState, entries []runtime.Entry) {
	if len(entries) == 0 {
		fmt.Fprintln(st.stdout(), "no sessions registered")
		return
	}
	now := core.Now()
	const format = "%-9s %-28s %-12s %-10s %-24s %-8s %-14s %-7s %s"
	line := func(cells ...any) {
		fmt.Fprintln(st.stdout(), strings.TrimRight(fmt.Sprintf(format, cells...), " "))
	}
	line("STATE", "RUN", "TASK", "PERSONA", "ACTION", "AGENT", "SURFACE", "AGE", "STATUS")
	for _, e := range entries {
		r := e.Record
		// A live run shows what it says it is doing; anything else shows
		// what it is, because a dead run's last self-report is a lie.
		state := string(e.Liveness)
		if e.Liveness == runtime.LiveLive {
			state = string(r.Status.State)
		}
		status := r.Status.Text
		if r.Watch != nil && r.Status.State == runtime.StateWatching {
			status = fmt.Sprintf("%s · last poll %s · %d new", r.Watch.Channel, orDefault(r.Watch.LastPollAt, "never"), r.Watch.ItemsSeen)
		}
		if e.Liveness == runtime.LiveEnded && r.ExitCode != nil {
			status = fmt.Sprintf("exit %d", *r.ExitCode)
		}
		line(state, r.RunID, orDefault(r.Task, "-"), r.Persona, orDefault(r.Checklist, "ad-hoc"), r.Agent, formatSurface(r.Surface), sessionAge(r, now), status)
	}
}

func newSessionShowCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "show <run-id>",
		Short: "Show one run's record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := st.openRegistry()
			if err != nil {
				return err
			}
			rec, err := reg.Get(args[0])
			if err != nil {
				return fmt.Errorf("%w: run %s %v", ErrUsage, args[0], err)
			}
			row := rowOf(runtime.Entry{Record: *rec, Liveness: reg.Liveness(*rec)})
			if st.isJSON() {
				return writeJSON(st.stdout(), row)
			}
			w := st.stdout()
			fmt.Fprintf(w, "%s  %s/%s\n", row.RunID, row.Liveness, row.State)
			fmt.Fprintf(w, "  project:  %s   task: %s\n", orDefault(row.Project, "-"), orDefault(row.Task, "-"))
			fmt.Fprintf(w, "  persona:  %s   action: %s   agent: %s:%s\n", row.Persona, orDefault(row.Checklist, "ad-hoc"), row.Agent, orDefault(row.Model, "unset"))
			fmt.Fprintf(w, "  surface:  %s   host: %s   pid: %d\n", row.Surface, orDefault(row.Host, "-"), rec.LauncherPID)
			fmt.Fprintf(w, "  started:  %s", row.StartedAt)
			if row.EndedAt != "" {
				fmt.Fprintf(w, "   ended: %s", row.EndedAt)
			}
			if row.ExitCode != nil {
				fmt.Fprintf(w, "   exit: %d", *row.ExitCode)
			}
			fmt.Fprintln(w)
			fmt.Fprintf(w, "  status:   %s %s (%s, %s)\n", row.State, row.StatusText, rec.Status.Source, rec.Status.At)
			if row.Watch != nil {
				fmt.Fprintf(w, "  watch:    %s · last %s · next %s · %d items\n", row.Watch.Channel, orDefault(row.Watch.LastPollAt, "never"), orDefault(row.Watch.NextPollAt, "-"), row.Watch.ItemsSeen)
			}
			fmt.Fprintf(w, "  context:  %s\n", row.ContextPath)
			return nil
		},
	}
}

// newSessionStatusCmd is the hook contract (spec §6): the three agent
// plugins call this on their own lifecycle events, so it must be cheap,
// quiet, and never fail loudly enough to disturb the agent.
func newSessionStatusCmd(st *cliState) *cobra.Command {
	var run, state, text, channel, lastPoll, nextPoll string
	var items int
	cmd := &cobra.Command{
		Use:   "status --state <working|idle|blocked|watching>",
		Short: "Report what this run is doing (called by agent hooks; --run defaults to $ATM_RUN_ID)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if run == "" {
				run = os.Getenv("ATM_RUN_ID")
			}
			if run == "" {
				return fmt.Errorf("%w: --run is required when ATM_RUN_ID is not set", ErrUsage)
			}
			// ended is not writable here: only the launcher ends a run, and
			// a hook claiming a run ended would strand it with no exit code.
			if !runtime.ValidState(state) || state == string(runtime.StateEnded) {
				return fmt.Errorf("%w: --state must be working, idle, blocked, or watching, got %q", ErrUsage, state)
			}
			reg, err := st.openRegistry()
			if err != nil {
				return err
			}
			var w *runtime.Watch
			if channel != "" || lastPoll != "" || nextPoll != "" || cmd.Flags().Changed("items") {
				w = &runtime.Watch{Channel: channel, LastPollAt: lastPoll, NextPollAt: nextPoll, ItemsSeen: items}
			}
			if err := reg.SetStatus(run, runtime.Status{State: runtime.State(state), Text: text, Source: "hook"}, w); err != nil {
				return fmt.Errorf("%w: run %s %v", ErrUsage, run, err)
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"run_id": run, "state": state, "text": text})
			}
			if !st.flags.quiet {
				fmt.Fprintf(st.stdout(), "%s: %s\n", run, state)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&run, "run", "", "run id (default: $ATM_RUN_ID)")
	cmd.Flags().StringVar(&state, "state", "", "working|idle|blocked|watching (required)")
	cmd.Flags().StringVar(&text, "text", "", "free-text status line")
	cmd.Flags().StringVar(&channel, "watch-channel", "", "channel handle a watching session polls")
	cmd.Flags().StringVar(&lastPoll, "last-poll", "", "RFC3339 time of the last poll")
	cmd.Flags().StringVar(&nextPoll, "next-poll", "", "RFC3339 time of the next planned poll")
	cmd.Flags().IntVar(&items, "items", 0, "items seen on the last poll")
	_ = cmd.MarkFlagRequired("state")
	return cmd
}

func newSessionPruneCmd(st *cliState) *cobra.Command {
	var all bool
	var olderThan string
	var keep int
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete ended and lost runs past the retention rules (never a live one)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := runtime.PruneOptions{All: all, Keep: keep}
			if olderThan != "" {
				d, err := time.ParseDuration(olderThan)
				if err != nil {
					return fmt.Errorf("%w: --older-than: %v", ErrUsage, err)
				}
				opts.OlderThan = d
			}
			reg, err := st.openRegistry()
			if err != nil {
				return err
			}
			res, err := reg.Prune(opts)
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"removed": res.Removed, "orphans": res.Orphans})
			}
			fmt.Fprintf(st.stdout(), "pruned %d run(s), swept %d orphan prompt file(s)\n", len(res.Removed), len(res.Orphans))
			for _, id := range res.Removed {
				fmt.Fprintln(st.stdout(), "  "+id)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "remove every non-live run regardless of age or cap")
	cmd.Flags().StringVar(&olderThan, "older-than", "", "age past which ended/lost runs go (default 24h)")
	cmd.Flags().IntVar(&keep, "keep", 0, "maximum non-live runs to keep (default 50)")
	return cmd
}
