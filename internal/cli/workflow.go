package cli

import (
	"fmt"

	"atm/internal/workflow"

	"github.com/spf13/cobra"
)

func newWorkflowCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Status-transition verbs (the paved road for task status)",
		Long: "Status transitions live in the internal/workflow capability. " +
			"Each verb swaps the task's status:* label (removes any existing one, " +
			"adds the target), so exactly-one-status is an invariant the capability " +
			"maintains. The store still enforces nothing; raw `atm task label " +
			"add/remove --label <CODE>:status:<value>` works. This is a paved road, " +
			"not a fence.",
	}
	bindActorFlag(cmd, st)
	cmd.AddCommand(newWorkflowStartCmd(st))
	cmd.AddCommand(newWorkflowOpenCmd(st))
	cmd.AddCommand(newWorkflowBlockCmd(st))
	cmd.AddCommand(newWorkflowCompleteCmd(st))
	cmd.AddCommand(newWorkflowStatusCmd(st))
	cmd.AddCommand(newWorkflowSeedCmd(st))
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
func runStatusVerb(st *cliState, id, legacy string, fn func(*workflow.Recorder, string) (string, error)) error {
	taskID, err := resolveTaskID(st, id, legacy)
	if err != nil {
		return err
	}
	actor, err := st.requireMutatingActor()
	if err != nil {
		return err
	}
	s, err := st.openStore()
	if err != nil {
		return err
	}
	rec := &workflow.Recorder{Store: s, Actor: actor}
	prior, err := fn(rec, taskID)
	if err != nil {
		return err
	}
	t, err := s.GetTask(taskID)
	if err != nil {
		return err
	}
	now, err := (&workflow.Reporter{Store: s}).Status(taskID)
	if err != nil {
		return err
	}
	return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t, nil)}, func() {
		switch {
		case prior == now:
			// No-op: the task already carried this status as its sole status.
			fmt.Fprintf(st.stdout(), "%s: already %s\n", t.ID, now)
		case prior == "":
			fmt.Fprintf(st.stdout(), "%s: status -> %s\n", t.ID, now)
		default:
			fmt.Fprintf(st.stdout(), "%s: status %s -> %s\n", t.ID, prior, now)
		}
	})
}

func newWorkflowStartCmd(st *cliState) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Transition a task to in-progress",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(st, id, legacy, func(r *workflow.Recorder, tid string) (string, error) {
				return r.Start(tid)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newWorkflowOpenCmd(st *cliState) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Transition a task to open",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(st, id, legacy, func(r *workflow.Recorder, tid string) (string, error) {
				return r.Open(tid)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newWorkflowBlockCmd(st *cliState) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "block",
		Short: "Transition a task to blocked",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(st, id, legacy, func(r *workflow.Recorder, tid string) (string, error) {
				return r.Block(tid)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newWorkflowCompleteCmd(st *cliState) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "complete",
		Short: "Transition a task to done",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(st, id, legacy, func(r *workflow.Recorder, tid string) (string, error) {
				return r.Complete(tid)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newWorkflowStatusCmd(st *cliState) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print the task's current status (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := resolveTaskID(st, id, legacy)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			rep := &workflow.Reporter{Store: s}
			value, err := rep.Status(taskID)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskID, "status": value}, func() {
				if value == "" {
					fmt.Fprintf(st.stdout(), "untriaged\n")
					return
				}
				fmt.Fprintf(st.stdout(), "%s\n", value)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newWorkflowSeedCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Ensure the workflow boards (backlog, open-tasks, in-progress-tasks) exist",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.requireMutatingActor()
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := workflow.EnsureVocabulary(s, project, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{
				"project": project,
				"boards":  []string{project + ":backlog", project + ":open-tasks", project + ":in-progress-tasks"},
			}, func() {
				fmt.Fprintf(st.stdout(), "ensured workflow boards for %s\n", project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
