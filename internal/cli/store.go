package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"atm/internal/store"
	eventsync "atm/libs/eventsource/sync"

	"github.com/spf13/cobra"
)

func newStoreCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Store inspection commands",
	}
	pathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print the resolved store path",
		RunE: func(cmd *cobra.Command, args []string) error {
			root := store.ResolveStorePath(st.flags.store)
			s, err := store.Open(root, st.storeOpts...)
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"store": s.StorePath()})
			}
			fmt.Fprintln(st.stdout(), s.StorePath())
			return nil
		},
	}
	cmd.AddCommand(pathCmd)

	logCmd := &cobra.Command{
		Use:   "log <CODE>",
		Short: "Stream the project's audit log",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			from, _ := cmd.Flags().GetInt("from")
			to, _ := cmd.Flags().GetInt("to")
			// A project's truth is events.v2.jsonl; --from/--to filter the
			// DISPLAY ordinal.
			events, err := s.ReadV2LogForDisplay(args[0])
			if err != nil {
				return err
			}
			filtered := make([]store.V2LogView, 0, len(events))
			for _, e := range events {
				if from != 0 && e.Ordinal < from {
					continue
				}
				if to != 0 && e.Ordinal > to {
					continue
				}
				filtered = append(filtered, e)
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), filtered)
			}
			for _, e := range filtered {
				fmt.Fprintf(st.stdout(), "%d\t%s\t%s\t%s\t%s\t%s\n", e.Ordinal, store.RFC3339UTC(e.At), e.Actor, e.Action, e.Subject, e.ID)
			}
			return nil
		},
	}
	logCmd.Flags().Int("from", 0, "start seq (inclusive, 0 = start)")
	logCmd.Flags().Int("to", 0, "end seq (inclusive, 0 = end)")
	cmd.AddCommand(logCmd)

	verifyCmd := &cobra.Command{
		Use:   "verify [CODE]",
		Short: "Replay logs against caches and report divergence",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			repair, _ := cmd.Flags().GetBool("repair")
			if len(args) == 1 {
				r, err := s.VerifyProject(args[0])
				if err != nil {
					return err
				}
				if repair && r.Diverged {
					_, _ = s.Rebuild()
					r2, _ := s.VerifyProject(args[0])
					r = r2
				}
				if err := st.emitVerify(r); err != nil {
					return err
				}
				return reportIntegrityError([]store.VerifyReport{*r})
			}
			reports, err := s.Verify()
			if err != nil {
				return err
			}
			if repair {
				for _, r := range reports {
					if r.Diverged {
						_, _ = s.Rebuild()
						break
					}
				}
				reports, _ = s.Verify()
			}
			if err := st.emitVerifyAll(reports); err != nil {
				return err
			}
			return reportIntegrityError(reports)
		},
	}
	verifyCmd.Flags().Bool("repair", false, "regenerate caches from logs on divergence")
	cmd.AddCommand(verifyCmd)

	rebuildCmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Regenerate all cache files from the logs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			rep, err := s.Rebuild()
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), rep)
			}
			fmt.Fprintf(st.stdout(), "rebuilt: projects=%d tasks=%d labels=%d\n", rep.Projects, rep.Tasks, rep.Labels)
			return nil
		},
	}
	cmd.AddCommand(rebuildCmd)

	upgradeCmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade v1 project logs to side-by-side EventSource v2 storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			all, _ := cmd.Flags().GetBool("all")
			if all == (project != "") {
				return fmt.Errorf("%w: pass exactly one of --project or --all", store.ErrUsage)
			}
			if all {
				reps, err := s.UpgradeAllToV2()
				if err != nil {
					return err
				}
				if st.isJSON() {
					return writeJSON(st.stdout(), reps)
				}
				for _, r := range reps {
					if r.AlreadyV2 {
						// Retry after a partial failure: UpgradeAllToV2
						// skipped this project because it is already
						// v2-active (gating rule; see Task 4).
						fmt.Fprintf(st.stdout(), "skipped\t%s\t%s\talready v2-active\n", r.Project, r.Format)
						continue
					}
					fmt.Fprintf(st.stdout(), "upgraded\t%s\t%s\tevents=%d\n", r.Project, r.Format, r.Events)
				}
				// UpgradeAllToV2 flipped the store default: new projects
				// are born v2 from here on. Surface that.
				fmt.Fprintln(st.stdout(), "active format: v2")
				return nil
			}
			rep, err := s.UpgradeProjectToV2(project)
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), rep)
			}
			fmt.Fprintf(st.stdout(), "upgraded\t%s\t%s\tevents=%d\n", rep.Project, rep.Format, rep.Events)
			return nil
		},
	}
	upgradeCmd.Flags().String("project", "", "project code to upgrade")
	upgradeCmd.Flags().Bool("all", false, "upgrade all projects")
	cmd.AddCommand(upgradeCmd)

	pruneCmd := &cobra.Command{
		Use:   "prune-v1",
		Short: "Retire upgraded projects' frozen v1 log.jsonl (archive by default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			all, _ := cmd.Flags().GetBool("all")
			del, _ := cmd.Flags().GetBool("delete")
			if all == (project != "") {
				return fmt.Errorf("%w: pass exactly one of --project or --all", store.ErrUsage)
			}
			codes := []string{project}
			if all {
				codes, err = s.ProjectCodes()
				if err != nil {
					return err
				}
			}
			// A per-project error stops the loop but must not discard the
			// reports already collected: PruneProjectV1's archive/delete move
			// for those projects already happened on disk, so hiding them
			// behind a mid-batch failure would make a successful `--all` prune
			// look like it did nothing. Print what was collected so far, then
			// return the error.
			reps := make([]*store.PruneReport, 0, len(codes))
			var loopErr error
			for _, c := range codes {
				rep, err := s.PruneProjectV1(c, del)
				if err != nil {
					loopErr = fmt.Errorf("project %q: %w", c, err)
					break
				}
				reps = append(reps, rep)
			}
			if st.isJSON() {
				if err := writeJSON(st.stdout(), reps); err != nil {
					return err
				}
				return loopErr
			}
			for _, r := range reps {
				switch {
				case r.Deleted:
					fmt.Fprintf(st.stdout(), "pruned\t%s\tdeleted\n", r.Project)
				case r.Pruned:
					fmt.Fprintf(st.stdout(), "pruned\t%s\tarchived %s\n", r.Project, r.Archived)
				default:
					fmt.Fprintf(st.stdout(), "skipped\t%s\t%s\n", r.Project, r.Reason)
				}
			}
			return loopErr
		},
	}
	pruneCmd.Flags().String("project", "", "project code to prune")
	pruneCmd.Flags().Bool("all", false, "prune all eligible projects")
	pruneCmd.Flags().Bool("delete", false, "delete log.jsonl instead of archiving it")
	cmd.AddCommand(pruneCmd)

	setFormatCmd := &cobra.Command{
		Use:   "set-format",
		Short: "Set the store default format (governs project birth and the legacy default only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			format, _ := cmd.Flags().GetString("format")
			if err := s.SetActiveFormat(store.StoreFormat(format)); err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"active_format": format})
			}
			fmt.Fprintf(st.stdout(), "active format: %s\n", format)
			return nil
		},
	}
	setFormatCmd.Flags().String("format", "", "v1 or v2; v2 is refused while any project lacks an explicit format entry")
	cmd.AddCommand(setFormatCmd)

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

	cmd.AddCommand(remoteCmd)

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
			// (eventsync.Sync treats Pull && Push as bidirectional too).
			opts := eventsync.Options{Pull: pull, Push: push, DryRun: dryRun}

			codes, err := syncProjectCodes(s, project)
			if err != nil {
				return err
			}

			remotesDir := filepath.Join(s.StorePath(), "remotes")
			results := make([]syncResult, 0, len(codes))
			failed := false
			for _, code := range codes {
				res := runProjectSync(cmd.Context(), s, remotesDir, code, arg, opts, actor)
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
	cmd.AddCommand(syncCmd)

	return cmd
}

// errSyncFailed is the command-level sentinel returned when any project's sync
// failed. The per-project detail is already printed in the report; this only
// drives a non-zero exit. It maps to the generic exit code.
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
	report *eventsync.Report
	err    error
}

func (r syncResult) failed() bool {
	return r.err != nil || (r.report != nil && r.report.PushErr != nil)
}

// runProjectSync resolves code's remote, selects a transport, and runs one
// sync. On success against an ad-hoc URL that bootstrapped a brand-new project
// (I-5), it persists that URL as the project's "origin" so later syncs need no
// argument; an ad-hoc URL that merely updated an existing project is not
// persisted.
func runProjectSync(ctx context.Context, s *store.Store, remotesDir, code, arg string, opts eventsync.Options, actor string) syncResult {
	url, adhoc, err := resolveSyncRemote(s, code, arg)
	if err != nil {
		return syncResult{code: code, err: err}
	}
	target, err := eventsync.SelectTarget(remotesDir, url)
	if err != nil {
		return syncResult{code: code, err: err}
	}
	report, err := eventsync.Sync(ctx, s, target, code, opts)
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
		if rep.PushErr != nil {
			j.PushError = rep.PushErr.Error()
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
		if rep.PushErr != nil {
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

func (st *cliState) emitVerify(r *store.VerifyReport) error {
	if st.isJSON() {
		return writeJSON(st.stdout(), r)
	}
	fmt.Fprintf(st.stdout(), "project: %s\nformat: %s\nlog_entries: %d\nlog_ok: %t\ntruncated: %d\ndiverged: %t\n", r.Project, r.Format, r.LogEntries, r.LogOK, r.Truncated, r.Diverged)
	for _, c := range r.Caches {
		fmt.Fprintf(st.stdout(), "  %s\t%s:%s\tcache=%d last=%d\n", c.Status, c.Kind, c.ID, c.CacheLogSeq, c.LastEventSeq)
	}
	return nil
}

func (st *cliState) emitVerifyAll(rs []store.VerifyReport) error {
	if st.isJSON() {
		return writeJSON(st.stdout(), rs)
	}
	for _, r := range rs {
		if err := st.emitVerify(&r); err != nil {
			return err
		}
	}
	return nil
}

func reportIntegrityError(reports []store.VerifyReport) error {
	for _, r := range reports {
		if r.Diverged || !r.LogOK {
			return fmt.Errorf("%w: project %s has integrity issues (diverged=%v, log_ok=%v)", store.ErrIntegrity, r.Project, r.Diverged, r.LogOK)
		}
	}
	return nil
}
