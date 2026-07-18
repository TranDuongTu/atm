package workflow

import (
	"fmt"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// Cap is the workflow capability: the `atm workflow` verb tree over the
// status recorder/reporter in this package.
type Cap struct{}

// New returns the capability the composition root registers.
func New() capability.Capability { return Cap{} }

func (Cap) Name() string { return "workflow" }

// EnsureVocabulary implements capability.Capability by delegating to this
// package's vocabulary bootstrap.
func (Cap) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error) {
	return EnsureVocabulary(svc, code, actor)
}

func (Cap) Command(env capability.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Status-transition verbs (the paved road for task status)",
		Long: "Status transitions live in the internal/capability/workflow capability. " +
			"Each verb swaps the task's status:* label (removes any existing one, " +
			"adds the target), so exactly-one-status is an invariant the capability " +
			"maintains. The store still enforces nothing; raw `atm task label " +
			"add/remove --label <CODE>:status:<value>` works. This is a paved road, " +
			"not a fence.",
	}
	env.BindActorFlag(cmd)
	cmd.AddCommand(newStartCmd(env))
	cmd.AddCommand(newOpenCmd(env))
	cmd.AddCommand(newBlockCmd(env))
	cmd.AddCommand(newCompleteCmd(env))
	cmd.AddCommand(newStatusCmd(env))
	cmd.AddCommand(newSeedCmd(env))
	return cmd
}

// runStatusVerb is the shared body for the four mutating status verbs. It
// resolves the task, requires an explicit actor, runs the swap, then prints
// the transition line and emits the updated task JSON.
//
// SetStatus returns prior == target ONLY on its no-op path (it returns early
// before making any store call in that case), so prior == now precisely
// identifies "the task already carried this status as its sole status" per
// the design spec, which calls for a no-op message rather than a transition
// line that would misleadingly read as if something happened (e.g.
// "status done -> done").
func runStatusVerb(env capability.Env, id, legacy string, fn func(*Recorder, string) (string, error)) error {
	taskID, err := env.ResolveTaskID(id, legacy)
	if err != nil {
		return err
	}
	actor, err := env.RequireMutatingActor()
	if err != nil {
		return err
	}
	svc, err := env.OpenService()
	if err != nil {
		return err
	}
	rec := &Recorder{Store: svc, Actor: actor}
	prior, err := fn(rec, taskID)
	if err != nil {
		return err
	}
	t, err := svc.GetTask(taskID)
	if err != nil {
		return err
	}
	now, err := (&Reporter{Store: svc}).Status(taskID)
	if err != nil {
		return err
	}
	return env.Emit(map[string]any{"task": env.TaskJSON(t)}, func() {
		switch {
		case prior == now:
			// No-op: the task already carried this status as its sole status.
			fmt.Fprintf(env.Stdout(), "%s: already %s\n", t.ID, now)
		case prior == "":
			fmt.Fprintf(env.Stdout(), "%s: status -> %s\n", t.ID, now)
		default:
			fmt.Fprintf(env.Stdout(), "%s: status %s -> %s\n", t.ID, prior, now)
		}
	})
}

func newStartCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Transition a task to in-progress",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(env, id, legacy, func(r *Recorder, tid string) (string, error) {
				return r.Start(tid)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newOpenCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Transition a task to open",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(env, id, legacy, func(r *Recorder, tid string) (string, error) {
				return r.Open(tid)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newBlockCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "block",
		Short: "Transition a task to blocked",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(env, id, legacy, func(r *Recorder, tid string) (string, error) {
				return r.Block(tid)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newCompleteCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "complete",
		Short: "Transition a task to done",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(env, id, legacy, func(r *Recorder, tid string) (string, error) {
				return r.Complete(tid)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newStatusCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print the task's current status (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := env.ResolveTaskID(id, legacy)
			if err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			rep := &Reporter{Store: svc}
			value, err := rep.Status(taskID)
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": taskID, "status": value}, func() {
				if value == "" {
					fmt.Fprintf(env.Stdout(), "untriaged\n")
					return
				}
				fmt.Fprintf(env.Stdout(), "%s\n", value)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newSeedCmd(env capability.Env) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Ensure the workflow boards (backlog, open-tasks, in-progress-tasks, all-tasks) exist",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			boards, err := EnsureVocabulary(svc, project, actor)
			if err != nil {
				return err
			}
			names := make([]string, 0, len(boards))
			for _, b := range boards {
				names = append(names, b.Name)
			}
			return env.Emit(map[string]any{"project": project, "boards": names}, func() {
				fmt.Fprintf(env.Stdout(), "ensured workflow boards for %s\n", project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
