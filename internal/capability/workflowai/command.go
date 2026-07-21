package workflowai

import (
	"fmt"
	"os"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// New returns the capability the composition root registers.
func New() capability.Capability { return Cap{} }

func (Cap) Name() string { return CapabilityName }

// Vocabulary implements capability.Capability (ownership surface).
func (Cap) Vocabulary(code string) []core.Label { return Vocabulary(code) }

// Exposed implements capability.Capability (TUI ring surface).
func (Cap) Exposed(code string) []core.Label { return Exposed(code) }

// EnsureVocabulary implements capability.Capability by delegating to this
// package's vocabulary bootstrap.
func (Cap) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error) {
	return EnsureVocabulary(svc, code, actor)
}

func (Cap) Command(env capability.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   CapabilityName,
		Short: "AI-native task cycle: stage ladder, links, plan tracking (the workflow_ai paved road)",
		Long: "The workflow_ai capability climbs tasks through brainstorm → clarify → " +
			"plan → ready → done over the stage:* namespace, one rung at a time; " +
			"exactly-one-stage is an invariant the verbs maintain, and machine state " +
			"(plan locator, links, demotion breadcrumb) lives in this capability's " +
			"metadata key. The store enforces nothing. This is a paved road, not a fence.",
	}
	env.BindActorFlag(cmd)
	cmd.AddCommand(newStageCmd(env, "brainstorm", "Mark the idea brainstormed (new → brainstormed)", (*Recorder).Brainstorm))
	cmd.AddCommand(newStageCmd(env, "clarify", "Mark scope settled (brainstormed → clarified)", (*Recorder).Clarify))
	cmd.AddCommand(newPlanCmd(env))
	cmd.AddCommand(newStageCmd(env, "ready", "Clear for implementation (planned → implementable; requires a plan record)", (*Recorder).Ready))
	cmd.AddCommand(newStageCmd(env, "done", "Close the cycle (implementable → done)", (*Recorder).Done))
	cmd.AddCommand(newDemoteCmd(env))
	cmd.AddCommand(newLinkCmd(env, true))
	cmd.AddCommand(newLinkCmd(env, false))
	cmd.AddCommand(newStageReportCmd(env))
	cmd.AddCommand(newLinksCmd(env))
	cmd.AddCommand(newSeedCmd(env))
	return cmd
}

// stageDisplay renders StageNew as the word humans read.
func stageDisplay(v string) string {
	if v == StageNew {
		return "new"
	}
	return v
}

// runStageVerb is the shared body for the mutating stage verbs: resolve
// task and actor, run the transition, print the transition line, emit the
// updated task JSON. prior == now identifies the idempotent no-op path.
func runStageVerb(env capability.Env, id, legacy string, fn func(*Recorder, string) (string, error)) error {
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
	now, err := (&Reporter{Store: svc}).Stage(taskID)
	if err != nil {
		return err
	}
	return env.Emit(map[string]any{"task": env.TaskJSON(t)}, func() {
		if prior == now {
			fmt.Fprintf(env.Stdout(), "%s: already %s\n", t.ID, stageDisplay(now))
			return
		}
		fmt.Fprintf(env.Stdout(), "%s: stage %s -> %s\n", t.ID, stageDisplay(prior), stageDisplay(now))
	})
}

func newStageCmd(env capability.Env, use, short string, fn func(*Recorder, string) (string, error)) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStageVerb(env, id, legacy, fn)
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newPlanCmd(env capability.Env) *cobra.Command {
	var id, legacy, kind, ref string
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Record the plan locator (clarified → planned; from planned/implementable updates it in place)",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			prior, err := rec.Plan(taskID, kind, ref)
			if err != nil {
				return err
			}
			t, err := svc.GetTask(taskID)
			if err != nil {
				return err
			}
			now, err := (&Reporter{Store: svc}).Stage(taskID)
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": env.TaskJSON(t), "plan": map[string]string{"kind": kind, "ref": ref}}, func() {
				if prior == now {
					fmt.Fprintf(env.Stdout(), "%s: plan updated (%s %s); stage %s unchanged\n", t.ID, kind, ref, stageDisplay(now))
					return
				}
				fmt.Fprintf(env.Stdout(), "%s: stage %s -> %s (plan: %s %s)\n", t.ID, stageDisplay(prior), stageDisplay(now), kind, ref)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&kind, "kind", "", "plan locator kind: file|commit|ephemeral")
	cmd.Flags().StringVar(&ref, "ref", "", "plan locator: repo-relative path, git revision, or ephemeral note")
	_ = cmd.MarkFlagRequired("kind")
	_ = cmd.MarkFlagRequired("ref")
	return cmd
}

func newDemoteCmd(env capability.Env) *cobra.Command {
	var id, legacy, reason string
	cmd := &cobra.Command{
		Use:   "demote",
		Short: "Reset the task to new (any stage; clears the plan record, keeps links, logs the reason)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStageVerb(env, id, legacy, func(r *Recorder, tid string) (string, error) {
				return r.Demote(tid, reason)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&reason, "reason", "", "why the task is demoted (recorded as a task comment)")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newLinkCmd(env capability.Env, link bool) *cobra.Command {
	use, short := "link", "Record a task link (exactly one of --revision-of / --relates-to)"
	if !link {
		use, short = "unlink", "Remove a task link (exactly one of --revision-of / --relates-to)"
	}
	var id, legacy, revisionOf, relatesTo string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := env.ResolveTaskID(id, legacy)
			if err != nil {
				return err
			}
			if (revisionOf == "") == (relatesTo == "") {
				return fmt.Errorf("exactly one of --revision-of or --relates-to is required")
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
			var verb, desc string
			switch {
			case link && revisionOf != "":
				err, verb, desc = rec.LinkRevisionOf(taskID, revisionOf), "linked", "revision_of "+revisionOf
			case link:
				err, verb, desc = rec.LinkRelatesTo(taskID, relatesTo), "linked", "relates_to "+relatesTo
			case revisionOf != "":
				err, verb, desc = rec.UnlinkRevisionOf(taskID, revisionOf), "unlinked", "revision_of "+revisionOf
			default:
				err, verb, desc = rec.UnlinkRelatesTo(taskID, relatesTo), "unlinked", "relates_to "+relatesTo
			}
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": taskID, "link": desc, "verb": verb}, func() {
				fmt.Fprintf(env.Stdout(), "%s: %s %s\n", taskID, verb, desc)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&revisionOf, "revision-of", "", "parent task this task is a revision follow-up of")
	cmd.Flags().StringVar(&relatesTo, "relates-to", "", "related task (generic, semantics-free)")
	return cmd
}

func newStageReportCmd(env capability.Env) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Check every planned/implementable task's plan locator; list what is at risk (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			if _, err := svc.GetProject(project); err != nil {
				return fmt.Errorf("project %q: %w", project, err)
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			findings, healthy, err := (&Reporter{Store: svc}).PlanCheck(project, DefaultVerifier(dir))
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"project": project, "findings": findings, "healthy": healthy}, func() {
				for _, f := range findings {
					fmt.Fprintf(env.Stdout(), "%s\t%s\t%s\n", f.TaskID, f.Stage, f.Detail)
				}
				fmt.Fprintf(env.Stdout(), "%d at risk, %d healthy (verified from %s)\n", len(findings), healthy, dir)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newLinksCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "links",
		Short: "Show a task's link topology, outbound and inbound (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := env.ResolveTaskID(id, legacy)
			if err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			l, err := (&Reporter{Store: svc}).Links(taskID)
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": taskID, "links": l}, func() {
				if l.RevisionOf != "" {
					fmt.Fprintf(env.Stdout(), "revision_of: %s\n", l.RevisionOf)
				}
				for _, x := range l.RelatesTo {
					fmt.Fprintf(env.Stdout(), "relates_to: %s\n", x)
				}
				for _, x := range l.Revisions {
					fmt.Fprintf(env.Stdout(), "revision: %s\n", x)
				}
				for _, x := range l.RelatedFrom {
					fmt.Fprintf(env.Stdout(), "related_from: %s\n", x)
				}
				if l.RevisionOf == "" && len(l.RelatesTo)+len(l.Revisions)+len(l.RelatedFrom) == 0 {
					fmt.Fprintf(env.Stdout(), "no links\n")
				}
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
		Short: "Ensure the workflow_ai vocabulary and boards exist for a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			if _, err := svc.GetProject(project); err != nil {
				return fmt.Errorf("project %q: %w", project, err)
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
				fmt.Fprintf(env.Stdout(), "ensured workflow_ai boards for %s\n", project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
