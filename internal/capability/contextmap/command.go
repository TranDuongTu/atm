package contextmap

import (
	"fmt"
	"io"
	"os"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// Cap is the contextmap capability: the `atm capability contextmap` verb tree
// over the recorder/check verbs in this package.
type Cap struct{}

// New returns the capability the composition root registers.
func New() capability.Capability { return Cap{} }

func (Cap) Name() string { return "contextmap" }

// EnsureVocabulary implements capability.Capability by delegating to this
// package's vocabulary bootstrap.
func (Cap) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error) {
	return EnsureVocabulary(svc, code, actor)
}

// Vocabulary implements capability.Capability (ownership surface).
func (Cap) Vocabulary(code string) []core.Label { return Vocabulary(code) }

// Exposed implements capability.Capability (TUI ring surface).
func (Cap) Exposed(code string) []core.Label { return Exposed(code) }

func (Cap) Command(env capability.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contextmap",
		Short: "Record and verify the project's context map",
		Long: "Record where each context pointer came from, and report which pointers have " +
			"drifted from reality.\n\n" +
			"add/stamp/retarget/supersede record; check only reports -- it never marks anything " +
			"stale. A changed file is not a wrong pointer: that judgement is yours.",
	}
	env.BindActorFlag(cmd)
	cmd.AddCommand(newAddCmd(env))
	cmd.AddCommand(newStampCmd(env))
	cmd.AddCommand(newRetargetCmd(env))
	cmd.AddCommand(newSupersedeCmd(env))
	cmd.AddCommand(newCheckCmd(env))
	return cmd
}

// newRecorder builds a Recorder rooted at the current working directory,
// which is the repo the manager is running in.
func newRecorder(env capability.Env, actor string) (*Recorder, error) {
	svc, err := env.OpenService()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return &Recorder{
		Store:    svc,
		Resolver: &Resolver{Repo: cwd},
		Actor:    actor,
	}, nil
}

func parseSources(raw []string) ([]Source, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: at least one --source is required", core.ErrUsage)
	}
	out := make([]Source, 0, len(raw))
	for _, r := range raw {
		src, err := ParseSource(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", core.ErrUsage, err)
		}
		out = append(out, src)
	}
	return out, nil
}

func newAddCmd(env capability.Env) *cobra.Command {
	var task, kind string
	var sources []string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Make a task a context pointer and stamp its provenance",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			srcs, err := parseSources(sources)
			if err != nil {
				return err
			}
			rec, err := newRecorder(env, actor)
			if err != nil {
				return err
			}
			if err := rec.Add(task, kind, srcs); err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": task, "kind": kind}, func() {
				fmt.Fprintf(env.Stdout(), "stamped %s as context:%s\n", task, kind)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	cmd.Flags().StringVar(&kind, "kind", "", "pointer kind: agent, repository, documentation, or convention")
	cmd.Flags().StringArrayVar(&sources, "source", nil,
		"kinded locator this pointer was derived from, repeatable: git:<path>, file:<path>, url:<url>, external:<system>/<id>")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("kind")
	return cmd
}

func newStampCmd(env capability.Env) *cobra.Command {
	var task string
	cmd := &cobra.Command{
		Use:   "stamp",
		Short: "Re-verify a pointer: its subject is unchanged in meaning",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			rec, err := newRecorder(env, actor)
			if err != nil {
				return err
			}
			if err := rec.Stamp(task); err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": task}, func() {
				fmt.Fprintf(env.Stdout(), "re-stamped %s\n", task)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newRetargetCmd(env capability.Env) *cobra.Command {
	var task string
	var sources []string
	cmd := &cobra.Command{
		Use:   "retarget",
		Short: "Point at new sources: the subject survived, but moved",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			srcs, err := parseSources(sources)
			if err != nil {
				return err
			}
			rec, err := newRecorder(env, actor)
			if err != nil {
				return err
			}
			if err := rec.Retarget(task, srcs); err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": task}, func() {
				fmt.Fprintf(env.Stdout(), "retargeted %s\n", task)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	cmd.Flags().StringArrayVar(&sources, "source", nil, "new kinded locator, repeatable")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newSupersedeCmd(env capability.Env) *cobra.Command {
	var task, by, reason string
	cmd := &cobra.Command{
		Use:   "supersede",
		Short: "Retire a pointer whose subject died; history is kept",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			rec, err := newRecorder(env, actor)
			if err != nil {
				return err
			}
			if err := rec.Supersede(task, by, reason); err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": task, "by": by}, func() {
				fmt.Fprintf(env.Stdout(), "superseded %s by %s\n", task, by)
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

func newCheckCmd(env capability.Env) *cobra.Command {
	var project, since string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Report which pointers drifted (read-only; mutates nothing)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := env.ResolveActor(false); err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			rep, err := Check(svc, &Resolver{Repo: cwd}, project, since)
			if err != nil {
				return err
			}
			return env.Emit(reportToJSON(rep), func() { printReport(env.Stdout(), rep) })
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "ATM project code")
	cmd.Flags().StringVar(&since, "since", "", "revision to scan for new territory (default: the newest stamp in the project)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func reportToJSON(rep Report) map[string]any {
	find := func(fs []Finding) []map[string]any {
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

func printReport(w io.Writer, rep Report) {
	section := func(name string, fs []Finding, gloss string) {
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
