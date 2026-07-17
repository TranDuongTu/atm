package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"atm/internal/core"
	"atm/internal/store"

	"github.com/spf13/cobra"
)

// newStoreSyncCmds returns the store subcommands that manage sync remotes and
// run set-union sync against them: remote (add/list/remove), sync.
func newStoreSyncCmds(st *cliState) []*cobra.Command {
	remoteCmd := &cobra.Command{
		Use:   "remote",
		Short: "Manage a project's sync remotes",
	}
	bindActorFlag(remoteCmd, st)

	remoteAddCmd := &cobra.Command{
		Use:   "add <name> <url>",
		Short: "Add or update a project's sync remote (upsert)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, _ := cmd.Flags().GetString("project")
			if project == "" {
				return fmt.Errorf("%w: --project is required", store.ErrUsage)
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SetProjectRemote(project, args[0], args[1], actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "name": args[0], "url": args[1]}, func() {
				fmt.Fprintf(st.stdout(), "added remote %s -> %s (project %s)\n", args[0], args[1], project)
			})
		},
	}
	remoteAddCmd.Flags().String("project", "", "project code")
	remoteCmd.AddCommand(remoteAddCmd)

	remoteListCmd := &cobra.Command{
		Use:   "list",
		Short: "List a project's (or all projects') sync remotes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, _ := cmd.Flags().GetString("project")
			s, err := st.openStore()
			if err != nil {
				return err
			}
			rows, err := listProjectRemotes(s, project)
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), rows)
			}
			for _, r := range rows {
				if project != "" {
					fmt.Fprintf(st.stdout(), "%s\t%s\n", r.Name, r.URL)
				} else {
					fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", r.Project, r.Name, r.URL)
				}
			}
			return nil
		},
	}
	remoteListCmd.Flags().String("project", "", "project code (all projects with remotes if omitted)")
	remoteCmd.AddCommand(remoteListCmd)

	remoteRemoveCmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a project's sync remote",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, _ := cmd.Flags().GetString("project")
			if project == "" {
				return fmt.Errorf("%w: --project is required", store.ErrUsage)
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RemoveProjectRemote(project, args[0], actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "name": args[0]}, func() {
				fmt.Fprintf(st.stdout(), "removed remote %s (project %s)\n", args[0], project)
			})
		},
	}
	remoteRemoveCmd.Flags().String("project", "", "project code")
	remoteCmd.AddCommand(remoteRemoveCmd)

	syncCmd := &cobra.Command{
		Use:   "sync [<name-or-url>]",
		Short: "Reconcile a project's events with a sync remote",
		Long: "Reconcile a project's events with a sync remote. The optional " +
			"argument is a remote name from the project's config or an ad-hoc " +
			"URL; with none, the project's \"origin\" remote is used. Without " +
			"--project, every project that has at least one remote is synced " +
			"independently.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pull, _ := cmd.Flags().GetBool("pull")
			push, _ := cmd.Flags().GetBool("push")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			project, _ := cmd.Flags().GetString("project")
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			var arg string
			if len(args) == 1 {
				arg = args[0]
			}
			// Neither flag means both directions; both flags set is the same
			// (the sync engine treats Pull && Push as bidirectional too).
			opts := core.SyncOptions{Pull: pull, Push: push, DryRun: dryRun}

			codes, err := syncProjectCodes(s, project)
			if err != nil {
				return err
			}

			results := make([]syncResult, 0, len(codes))
			failed := false
			for _, code := range codes {
				res := runProjectSync(cmd.Context(), s, code, arg, opts, actor)
				if res.failed() {
					failed = true
				}
				results = append(results, res)
			}
			if err := st.emitSyncResults(results); err != nil {
				return err
			}
			if failed {
				return errSyncFailed
			}
			return nil
		},
	}
	syncCmd.Flags().String("project", "", "project code (all projects with a remote if omitted)")
	syncCmd.Flags().Bool("pull", false, "pull remote events we lack (default: both directions)")
	syncCmd.Flags().Bool("push", false, "push local events the remote lacks (default: both directions)")
	syncCmd.Flags().Bool("dry-run", false, "report what would move without changing anything")
	bindActorFlag(syncCmd, st)
	return []*cobra.Command{remoteCmd, syncCmd}
}

var errSyncFailed = errors.New("one or more projects failed to sync")

// syncProjectCodes resolves which projects `store sync` operates on: just the
// named project when project != "", otherwise every project that has at least
// one configured remote, sorted for deterministic output.
func syncProjectCodes(s *store.Store, project string) ([]string, error) {
	if project != "" {
		return []string{project}, nil
	}
	all, err := s.ProjectCodes()
	if err != nil {
		return nil, err
	}
	var codes []string
	for _, c := range all {
		remotes, err := s.ProjectRemotes(c)
		if err != nil {
			return nil, err
		}
		if len(remotes) > 0 {
			codes = append(codes, c)
		}
	}
	sort.Strings(codes)
	return codes, nil
}

// syncResult is one project's sync outcome. err holds a hard failure (remote
// resolution, target selection, or a fatal Sync error) and leaves report nil;
// a bidirectional sync whose push leg failed instead returns a report with a
// non-nil PushErr and no err. Both count as a failure for the exit code.
type syncResult struct {
	code   string
	report *core.SyncReport
	err    error
}

func (r syncResult) failed() bool {
	return r.err != nil || (r.report != nil && r.report.PushErr != "")
}

// runProjectSync resolves code's remote and runs one sync through the store
// (which owns transport selection and the set-union engine). On success against
// an ad-hoc URL that bootstrapped a brand-new project (I-5), it persists that
// URL as the project's "origin" so later syncs need no argument; an ad-hoc URL
// that merely updated an existing project is not persisted.
func runProjectSync(ctx context.Context, s *store.Store, code, arg string, opts core.SyncOptions, actor string) syncResult {
	url, adhoc, err := resolveSyncRemote(s, code, arg)
	if err != nil {
		return syncResult{code: code, err: err}
	}
	report, err := s.SyncProject(ctx, code, url, opts)
	if err != nil {
		return syncResult{code: code, err: err}
	}
	if adhoc && report.Bootstrapped && !opts.DryRun {
		if perr := s.SetProjectRemote(code, "origin", url, actor); perr != nil {
			return syncResult{code: code, report: report, err: perr}
		}
	}
	return syncResult{code: code, report: report}
}

// resolveSyncRemote maps the positional argument to a remote URL: a name found
// in the project's remotes yields its URL; any other value is treated as an
// ad-hoc URL. With no argument the project's "origin" remote is required.
func resolveSyncRemote(s *store.Store, code, arg string) (url string, adhoc bool, err error) {
	remotes, err := s.ProjectRemotes(code)
	if err != nil {
		return "", false, err
	}
	if arg == "" {
		u, ok := remotes["origin"]
		if !ok {
			return "", false, fmt.Errorf("%w: project %s has no \"origin\" remote (pass a name or URL)", store.ErrUsage, code)
		}
		return u, false, nil
	}
	if u, ok := remotes[arg]; ok {
		return u, false, nil
	}
	return arg, true, nil
}

// syncJSONReport is the JSON shape for one project's outcome. PushErr and a
// hard Sync error are rendered as strings (a raw error does not marshal
// usefully); Error is set only for a hard failure that produced no report.
type syncJSONReport struct {
	Project        string `json:"project"`
	Pulled         int    `json:"pulled"`
	Pushed         int    `json:"pushed"`
	Bootstrapped   bool   `json:"bootstrapped"`
	NewlyContested int    `json:"newly_contested"`
	RemoteAbsent   bool   `json:"remote_absent"`
	DryRun         bool   `json:"dry_run"`
	PushError      string `json:"push_error,omitempty"`
	Error          string `json:"error,omitempty"`
}

func (r syncResult) toJSON() syncJSONReport {
	j := syncJSONReport{Project: r.code}
	if r.err != nil {
		j.Error = r.err.Error()
	}
	if rep := r.report; rep != nil {
		j.Project = rep.Project
		j.Pulled = rep.Pulled
		j.Pushed = rep.Pushed
		j.Bootstrapped = rep.Bootstrapped
		j.NewlyContested = rep.NewlyContested
		j.RemoteAbsent = rep.RemoteAbsent
		j.DryRun = rep.DryRun
		if rep.PushErr != "" {
			j.PushError = rep.PushErr
		}
	}
	return j
}

// emitSyncResults renders every project's outcome. In JSON mode it emits one
// syncJSONReport per project (always an array); in text mode a `CODE: pulled N,
// pushed M` line per project, with bootstrap/dry-run tags and indented
// push-failure and contested notices.
func (st *cliState) emitSyncResults(results []syncResult) error {
	if st.isJSON() {
		out := make([]syncJSONReport, 0, len(results))
		for _, r := range results {
			out = append(out, r.toJSON())
		}
		return writeJSON(st.stdout(), out)
	}
	w := st.stdout()
	for _, r := range results {
		if r.err != nil {
			fmt.Fprintf(w, "%s: sync failed: %s\n", r.code, r.err)
			continue
		}
		rep := r.report
		line := fmt.Sprintf("%s: pulled %d, pushed %d", rep.Project, rep.Pulled, rep.Pushed)
		if rep.Bootstrapped {
			line += " (bootstrapped)"
		}
		if rep.DryRun {
			line += " (dry run)"
		}
		fmt.Fprintln(w, line)
		if rep.PushErr != "" {
			fmt.Fprintf(w, "  push failed: %s\n", rep.PushErr)
		}
		if rep.NewlyContested > 0 {
			fmt.Fprintf(w, "  %d newly contested slot(s) — review the contested board\n", rep.NewlyContested)
		}
	}
	return nil
}

// remoteRow is a project-qualified sync remote row, used for both the
// single-project and all-projects `store remote list` shapes: text output
// drops the Project column when --project narrows to one, but --json always
// includes it (Task 8 brief).
type remoteRow struct {
	Project string `json:"project"`
	Name    string `json:"name"`
	URL     string `json:"url"`
}

// listProjectRemotes returns the sync remotes for one project (code != ""),
// or for every project that has at least one remote (code == ""). Both
// projects and remote names are sorted for deterministic output.
func listProjectRemotes(s *store.Store, code string) ([]remoteRow, error) {
	codes := []string{code}
	if code == "" {
		var err error
		codes, err = s.ProjectCodes()
		if err != nil {
			return nil, err
		}
		sort.Strings(codes)
	}
	rows := make([]remoteRow, 0)
	for _, c := range codes {
		remotes, err := s.ProjectRemotes(c)
		if err != nil {
			return nil, err
		}
		if code == "" && len(remotes) == 0 {
			continue
		}
		names := make([]string, 0, len(remotes))
		for n := range remotes {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			rows = append(rows, remoteRow{Project: c, Name: n, URL: remotes[n]})
		}
	}
	return rows, nil
}
