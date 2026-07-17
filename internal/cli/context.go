package cli

import (
	"fmt"
	"os"

	"atm/internal/capability/contextmap"

	"github.com/spf13/cobra"
)

func newContextCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Record and verify the project's context map",
		Long: "Record where each context pointer came from, and report which pointers have " +
			"drifted from reality.\n\n" +
			"add/stamp/retarget/supersede record; check only reports -- it never marks anything " +
			"stale. A changed file is not a wrong pointer: that judgement is yours.",
	}
	bindActorFlag(cmd, st)
	cmd.AddCommand(newContextAddCmd(st))
	cmd.AddCommand(newContextStampCmd(st))
	cmd.AddCommand(newContextRetargetCmd(st))
	cmd.AddCommand(newContextSupersedeCmd(st))
	cmd.AddCommand(newContextCheckCmd(st))
	return cmd
}

// recorder builds a Recorder rooted at the current working directory, which is
// the repo the manager is running in.
func (st *cliState) recorder(actor string) (*contextmap.Recorder, error) {
	s, err := st.openStore()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return &contextmap.Recorder{
		Store:    s,
		Resolver: &contextmap.Resolver{Repo: cwd},
		Actor:    actor,
	}, nil
}

func parseSources(raw []string) ([]contextmap.Source, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: at least one --source is required", ErrUsage)
	}
	out := make([]contextmap.Source, 0, len(raw))
	for _, r := range raw {
		src, err := contextmap.ParseSource(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUsage, err)
		}
		out = append(out, src)
	}
	return out, nil
}

func newContextAddCmd(st *cliState) *cobra.Command {
	var task, kind string
	var sources []string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Make a task a context pointer and stamp its provenance",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.RequireMutatingActor()
			if err != nil {
				return err
			}
			srcs, err := parseSources(sources)
			if err != nil {
				return err
			}
			rec, err := st.recorder(actor)
			if err != nil {
				return err
			}
			if err := rec.Add(task, kind, srcs); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": task, "kind": kind}, func() {
				fmt.Fprintf(st.stdout(), "stamped %s as context:%s\n", task, kind)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	cmd.Flags().StringVar(&kind, "kind", "", "pointer kind: agent, repository, documentation, or question")
	cmd.Flags().StringArrayVar(&sources, "source", nil,
		"kinded locator this pointer was derived from, repeatable: git:<path>, file:<path>, url:<url>, external:<system>/<id>")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("kind")
	return cmd
}

func newContextStampCmd(st *cliState) *cobra.Command {
	var task string
	cmd := &cobra.Command{
		Use:   "stamp",
		Short: "Re-verify a pointer: its subject is unchanged in meaning",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.RequireMutatingActor()
			if err != nil {
				return err
			}
			rec, err := st.recorder(actor)
			if err != nil {
				return err
			}
			if err := rec.Stamp(task); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": task}, func() {
				fmt.Fprintf(st.stdout(), "re-stamped %s\n", task)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newContextRetargetCmd(st *cliState) *cobra.Command {
	var task string
	var sources []string
	cmd := &cobra.Command{
		Use:   "retarget",
		Short: "Point at new sources: the subject survived, but moved",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.RequireMutatingActor()
			if err != nil {
				return err
			}
			srcs, err := parseSources(sources)
			if err != nil {
				return err
			}
			rec, err := st.recorder(actor)
			if err != nil {
				return err
			}
			if err := rec.Retarget(task, srcs); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": task}, func() {
				fmt.Fprintf(st.stdout(), "retargeted %s\n", task)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	cmd.Flags().StringArrayVar(&sources, "source", nil, "new kinded locator, repeatable")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newContextSupersedeCmd(st *cliState) *cobra.Command {
	var task, by, reason string
	cmd := &cobra.Command{
		Use:   "supersede",
		Short: "Retire a pointer whose subject died; history is kept",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.RequireMutatingActor()
			if err != nil {
				return err
			}
			rec, err := st.recorder(actor)
			if err != nil {
				return err
			}
			if err := rec.Supersede(task, by, reason); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": task, "by": by}, func() {
				fmt.Fprintf(st.stdout(), "superseded %s by %s\n", task, by)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id to retire")
	cmd.Flags().StringVar(&by, "by", "", "task id that replaces it")
	cmd.Flags().StringVar(&reason, "reason", "", "why it was superseded")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("by")
	return cmd
}

func newContextCheckCmd(st *cliState) *cobra.Command {
	var project, since string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Report which pointers drifted (read-only; mutates nothing)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := st.resolveActor(false); err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			rep, err := contextmap.Check(s, &contextmap.Resolver{Repo: cwd}, project, since)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), reportToJSON(rep), func() { printReport(st, rep) })
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "ATM project code")
	cmd.Flags().StringVar(&since, "since", "", "revision to scan for new territory (default: the newest stamp in the project)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func reportToJSON(rep contextmap.Report) map[string]any {
	find := func(fs []contextmap.Finding) []map[string]any {
		out := make([]map[string]any, 0, len(fs))
		for _, f := range fs {
			m := map[string]any{
				"task":    f.TaskID,
				"title":   f.Title,
				"source":  f.Source.String(),
				"verdict": string(f.Verdict),
				"detail":  f.Detail,
			}
			if f.AgeDays > 0 {
				m["age_days"] = f.AgeDays
			}
			out = append(out, m)
		}
		return out
	}
	return map[string]any{
		"drift":      find(rep.Drift),
		"age":        find(rep.Age),
		"unverified": find(rep.Unverified),
		"skipped":    find(rep.Skipped),
		"ok":         find(rep.OK),
		"new":        rep.New,
		"since":      rep.Since,
	}
}

func printReport(st *cliState, rep contextmap.Report) {
	w := st.stdout()
	section := func(name string, fs []contextmap.Finding, gloss string) {
		if len(fs) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s (%d)\t%s\n", name, len(fs), gloss)
		for _, f := range fs {
			fmt.Fprintf(w, "  %s\t%s\t%s\n", f.TaskID, f.Source, f.Detail)
		}
	}
	if rep.Since != "" {
		fmt.Fprintf(w, "  (new territory since %s)\n", rep.Since)
	}
	section("DRIFT", rep.Drift, "provable content change")
	if len(rep.New) > 0 {
		fmt.Fprintf(w, "\nNEW (%d)\tchanged in git, claimed by no pointer\n", len(rep.New))
		for _, p := range rep.New {
			fmt.Fprintf(w, "  %s\n", p)
		}
	}
	section("AGE", rep.Age, "unprovable; re-verify by hand")
	section("UNVERIFIED", rep.Unverified, "no provenance stamp")
	section("SKIPPED", rep.Skipped, "could not witness")
	fmt.Fprintf(w, "\nOK (%d)\n", len(rep.OK))
}
