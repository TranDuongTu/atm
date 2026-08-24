package scrum

import (
	"fmt"
	"strings"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// Command mounts the scrum verb tree. Every mutating verb goes through the
// recorder, so the label invariants have exactly one home.
func (Cap) Command(env capability.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   CapabilityName,
		Short: "Scrum flow: absorb work from the pool and build it as EPIC -> Story -> Task/Bug/Design",
		Long: "The scrum capability is the first stage of the flow. Its inbox is " +
			"whatever the project wires as eligible and scrum has not decided " +
			"about; absorb claims a task into the pipeline with a type, evict " +
			"settles it out with a reason, and release withdraws scrum's " +
			"perspective entirely. Unit topology (part_of, depends_on, " +
			"covered_by) and the spec/plan locators live only in this " +
			"capability's metadata key. The store enforces nothing: this is a " +
			"paved road, not a fence.",
	}
	env.BindActorFlag(cmd)
	cmd.AddCommand(newAbsorbCmd(env))
	cmd.AddCommand(newAddCmd(env))
	cmd.AddCommand(newStageCmd(env))
	cmd.AddCommand(newEvictCmd(env))
	cmd.AddCommand(newReleaseCmd(env))
	cmd.AddCommand(newReopenCmd(env))
	cmd.AddCommand(newLinkCmd(env, true))
	cmd.AddCommand(newLinkCmd(env, false))
	cmd.AddCommand(newLocatorCmd(env, "spec"))
	cmd.AddCommand(newLocatorCmd(env, "plan"))
	cmd.AddCommand(newLinksCmd(env))
	cmd.AddCommand(newReportCmd(env))
	cmd.AddCommand(newSeedCmd(env))
	return cmd
}

// openRecorder resolves task, actor, and service for a mutating verb.
func openRecorder(env capability.Env, id, legacy string) (string, core.Service, *Recorder, error) {
	taskID, err := env.ResolveTaskID(id, legacy)
	if err != nil {
		return "", nil, nil, err
	}
	actor, err := env.RequireMutatingActor()
	if err != nil {
		return "", nil, nil, err
	}
	svc, err := env.OpenService()
	if err != nil {
		return "", nil, nil, err
	}
	return taskID, svc, &Recorder{Store: svc, Actor: actor}, nil
}

// emitTask prints one line about what changed and emits the updated task.
func emitTask(env capability.Env, svc core.Service, taskID, line string, extra map[string]any) error {
	t, err := svc.GetTask(taskID)
	if err != nil {
		return err
	}
	payload := map[string]any{"task": env.TaskJSON(t)}
	for k, v := range extra {
		payload[k] = v
	}
	return env.Emit(payload, func() {
		fmt.Fprintf(env.Stdout(), "%s: %s\n", t.ID, line)
	})
}

func typeFlagHelp() string  { return "unit type: " + strings.Join(Types(), "|") }
func stageFlagHelp() string { return "working stage: " + strings.Join(Stages(), "|") }

func newAbsorbCmd(env capability.Env) *cobra.Command {
	var id, legacy, typ, stage string
	cmd := &cobra.Command{
		Use:   "absorb",
		Short: "Claim an inbox task into scrum's pipeline with a type",
		Long: "Absorbing at a stage is deliberate: --stage done reads already-" +
			"finished work into scrum without pretending it still has to be built.",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			if err := rec.Absorb(taskID, typ, stage); err != nil {
				return err
			}
			line := "absorbed as " + typ
			if stage != "" {
				line += " at stage " + stage
			}
			return emitTask(env, svc, taskID, line, map[string]any{"type": typ, "stage": stage})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&typ, "type", "", typeFlagHelp())
	cmd.Flags().StringVar(&stage, "stage", "", stageFlagHelp()+" (optional)")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

func newAddCmd(env capability.Env) *cobra.Command {
	var project, title, typ, partOf, stage string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Create a task born into scrum's pipeline (decomposition)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			rec := &Recorder{Store: svc, Actor: actor}
			t, err := rec.Add(project, title, typ, partOf, stage)
			if err != nil {
				return err
			}
			line := "created as " + typ
			if partOf != "" {
				line += ", part of " + partOf
			}
			return emitTask(env, svc, t.ID, line, map[string]any{"type": typ, "part_of": partOf})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&title, "title", "", "task title")
	cmd.Flags().StringVar(&typ, "type", "", typeFlagHelp())
	cmd.Flags().StringVar(&partOf, "part-of", "", "parent task this unit is part of (must already be claimed by scrum)")
	cmd.Flags().StringVar(&stage, "stage", "", stageFlagHelp()+" (optional)")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("title")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

func newStageCmd(env capability.Env) *cobra.Command {
	var id, legacy, stage string
	cmd := &cobra.Command{
		Use:   "stage",
		Short: "Move a claimed unit along the working stage axis",
		Long: "Stamping done is the finish socket downstream capabilities wire " +
			"to. A story or epic is refused until every live child is done.",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			if err := rec.Stage(taskID, stage); err != nil {
				return err
			}
			return emitTask(env, svc, taskID, "stage -> "+stage, map[string]any{"stage": stage})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&stage, "stage", "", stageFlagHelp())
	_ = cmd.MarkFlagRequired("stage")
	return cmd
}

func newEvictCmd(env capability.Env) *cobra.Command {
	var id, legacy, reason, coveredBy string
	cmd := &cobra.Command{
		Use:   "evict",
		Short: "Settle a task out of scrum with a reason",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			if err := rec.Evict(taskID, reason, coveredBy); err != nil {
				return err
			}
			r := reason
			if r == "" {
				r = OutNotWorthIt
			}
			return emitTask(env, svc, taskID, "evicted: "+r, map[string]any{"reason": r, "covered_by": coveredBy})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&reason, "reason", "", "evict reason: "+strings.Join(OutReasons(), "|")+" (default "+OutNotWorthIt+")")
	cmd.Flags().StringVar(&coveredBy, "covered-by", "", "task that covers this one (required for reason "+OutCoveredBy+")")
	return cmd
}

func newReleaseCmd(env capability.Env) *cobra.Command {
	var id, legacy, reason string
	cmd := &cobra.Command{
		Use:   "release",
		Short: "Withdraw scrum's perspective entirely; the task returns to the pool",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			if err := rec.Release(taskID, reason); err != nil {
				return err
			}
			return emitTask(env, svc, taskID, "released to the pool: "+reason, map[string]any{"reason": reason})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&reason, "reason", "", "why scrum is letting this go")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newReopenCmd(env capability.Env) *cobra.Command {
	var id, legacy, reason string
	cmd := &cobra.Command{
		Use:   "reopen",
		Short: "Un-finish a done unit (the upstream half of the backward-flow pair)",
		Long: "Pair this with the DOWNSTREAM capability's own release verb. The " +
			"two verbs are never composed: no capability may un-stamp a sibling.",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			if err := rec.Reopen(taskID, reason); err != nil {
				return err
			}
			return emitTask(env, svc, taskID, "reopened: "+reason, map[string]any{"reason": reason})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&reason, "reason", "", "why the unit is being reopened")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newLinkCmd(env capability.Env, link bool) *cobra.Command {
	var id, legacy, dependsOn, partOf string
	use, short := "link", "Record a unit link (exactly one of --depends-on / --part-of)"
	if !link {
		use, short = "unlink", "Remove a unit link (exactly one of --depends-on / --part-of)"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			if (dependsOn == "") == (partOf == "") {
				return fmt.Errorf("exactly one of --depends-on or --part-of is required")
			}
			taskID, _, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			var verb, desc string
			switch {
			case link && dependsOn != "":
				err, verb, desc = rec.LinkDependsOn(taskID, dependsOn), "linked", "depends_on "+dependsOn
			case link:
				err, verb, desc = rec.SetPartOf(taskID, partOf), "linked", "part_of "+partOf
			case dependsOn != "":
				err, verb, desc = rec.UnlinkDependsOn(taskID, dependsOn), "unlinked", "depends_on "+dependsOn
			default:
				err, verb, desc = rec.ClearPartOf(taskID, partOf), "unlinked", "part_of "+partOf
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
	cmd.Flags().StringVar(&dependsOn, "depends-on", "", "task this one depends on (execution dependency)")
	cmd.Flags().StringVar(&partOf, "part-of", "", "parent task this unit is part of")
	return cmd
}

func newLocatorCmd(env capability.Env, kind string) *cobra.Command {
	var id, legacy, path string
	cmd := &cobra.Command{
		Use:   kind,
		Short: "Record this unit's " + kind + " locator (a pointer, not content)",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			set := rec.SetSpec
			if kind == "plan" {
				set = rec.SetPlan
			}
			if err := set(taskID, path); err != nil {
				return err
			}
			return emitTask(env, svc, taskID, kind+" -> "+path, map[string]any{kind: path})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&path, "path", "", "repo-relative locator")
	_ = cmd.MarkFlagRequired("path")
	return cmd
}

func newSeedCmd(env capability.Env) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Ensure the scrum vocabulary and lane boards exist for a project",
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
				fmt.Fprintf(env.Stdout(), "ensured scrum lanes for %s\n", project)
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
		Short: "Show a unit's topology, outbound and inbound (read-only)",
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
				w := env.Stdout()
				if l.PartOf != "" {
					fmt.Fprintf(w, "%s\tpart_of\t%s\n", taskID, l.PartOf)
				}
				if l.CoveredBy != "" {
					fmt.Fprintf(w, "%s\tcovered_by\t%s\n", taskID, l.CoveredBy)
				}
				for _, x := range l.DependsOn {
					fmt.Fprintf(w, "%s\tdepends_on\t%s\n", taskID, x)
				}
				for _, x := range l.Children {
					fmt.Fprintf(w, "%s\tchild\t%s\n", taskID, x)
				}
				for _, x := range l.Dependents {
					fmt.Fprintf(w, "%s\tdependent\t%s\n", taskID, x)
				}
				for _, x := range l.Covered {
					fmt.Fprintf(w, "%s\tcovers\t%s\n", taskID, x)
				}
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newReportCmd(env capability.Env) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Read the project through the scrum lens: lane rosters and findings (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			rep, err := (&Reporter{Store: svc}).Report(project)
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"report": rep}, func() {
				w := env.Stdout()
				for _, id := range rep.Inbox {
					fmt.Fprintf(w, "%s\tinbox\tawaiting a decision\n", id)
				}
				for _, s := range rep.Pipeline {
					stage := s.Stage
					if stage == "" {
						stage = "-"
					}
					fmt.Fprintf(w, "%s\tpipeline\t%s · %s\tchildren: %d, blocked by: %d\n",
						s.TaskID, s.Type, stage, s.Children, len(s.BlockedBy))
				}
				for _, s := range rep.Out {
					fmt.Fprintf(w, "%s\tout\t%s\n", s.TaskID, s.Reason)
				}
				for _, f := range rep.Findings {
					fmt.Fprintf(w, "%s\tfinding\t%s\n", f.TaskID, f.Detail)
				}
				fmt.Fprintf(w, "%d inbox, %d pipeline, %d out, %d findings\n",
					len(rep.Inbox), len(rep.Pipeline), len(rep.Out), len(rep.Findings))
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
