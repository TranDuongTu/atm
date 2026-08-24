package qa

import (
	"fmt"
	"strings"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// Command mounts the qa verb tree. Every mutating verb goes through the
// recorder, so the originals-only finish guarantee has exactly one home.
func (Cap) Command(env capability.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   CapabilityName,
		Short: "QA flow: verify finished work through test scaffolds and certify the original",
		Long: "The qa capability absorbs finished development work as ORIGINALS " +
			"and spawns born-claimed test scaffolds beneath them. Its finish " +
			"socket, qa:done, is only ever stamped on an original — never on a " +
			"scaffold — which is what makes it a reliable downstream signal and " +
			"what makes release selection originals-only for free. The store " +
			"enforces nothing: this is a paved road, not a fence.",
	}
	env.BindActorFlag(cmd)
	cmd.AddCommand(newAbsorbCmd(env))
	cmd.AddCommand(newScaffoldCmd(env))
	cmd.AddCommand(newPassCmd(env))
	cmd.AddCommand(newEvictCmd(env))
	cmd.AddCommand(newReleaseCmd(env))
	cmd.AddCommand(newReportCmd(env))
	cmd.AddCommand(newSeedCmd(env))
	return cmd
}

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

func newAbsorbCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "absorb",
		Short: "Claim an inbox task as an original under verification",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			if err := rec.Absorb(taskID); err != nil {
				return err
			}
			return emitTask(env, svc, taskID, "absorbed for verification", map[string]any{"state": StateTesting})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newScaffoldCmd(env capability.Env) *cobra.Command {
	var id, legacy, title string
	cmd := &cobra.Command{
		Use:   "scaffold",
		Short: "Create a test scaffold born into qa's pipeline beneath an original",
		RunE: func(cmd *cobra.Command, args []string) error {
			originalID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			sc, err := rec.Scaffold(originalID, title)
			if err != nil {
				return err
			}
			return emitTask(env, svc, sc.ID, "scaffold of "+originalID, map[string]any{"part_of": originalID})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&title, "title", "", "scaffold title (e.g. \"staging run\")")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newPassCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "pass",
		Short: "Record successful verification (a scaffold gives up its claim; an original is certified)",
		Long: "An original is refused while any of its scaffolds is still under " +
			"test, and the refusal names them. A scaffold is never stamped done.",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			if err := rec.Pass(taskID); err != nil {
				return err
			}
			t, err := svc.GetTask(taskID)
			if err != nil {
				return err
			}
			code, _, _ := core.ParseTaskID(taskID)
			line := "scaffold passed"
			if StateOf(t, code) == StateDone {
				line = "verified: " + code + ":qa:done"
			}
			return emitTask(env, svc, taskID, line, map[string]any{"state": StateOf(t, code)})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newEvictCmd(env capability.Env) *cobra.Command {
	var id, legacy, reason, coveredBy string
	cmd := &cobra.Command{
		Use:   "evict",
		Short: "Settle a task out of qa with a reason (failed is the backward-flow signal)",
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
				r = OutNotRelevant
			}
			return emitTask(env, svc, taskID, "evicted: "+r, map[string]any{"reason": r, "covered_by": coveredBy})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&reason, "reason", "", "evict reason: "+strings.Join(OutReasons(), "|")+" (default "+OutNotRelevant+")")
	cmd.Flags().StringVar(&coveredBy, "covered-by", "", "task whose verification covers this one (required for reason "+OutCoveredBy+")")
	return cmd
}

func newReleaseCmd(env capability.Env) *cobra.Command {
	var id, legacy, reason string
	cmd := &cobra.Command{
		Use:   "release",
		Short: "Withdraw qa's perspective entirely; the task returns to the pool",
		Long: "Pair this with the UPSTREAM capability's reopen verb to re-spiral " +
			"work. The two verbs are never composed: no capability may un-stamp " +
			"a sibling.",
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
	cmd.Flags().StringVar(&reason, "reason", "", "why qa is letting this go")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newReportCmd(env capability.Env) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Read the project through the qa lens: lane rosters and findings (read-only)",
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
					kind := "original"
					if s.PartOf != "" {
						kind = "scaffold of " + s.PartOf
					}
					fmt.Fprintf(w, "%s\tpipeline\t%s · %s\tscaffolds: %d, live: %d\n",
						s.TaskID, kind, s.State, len(s.Scaffolds), len(s.Live))
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

func newSeedCmd(env capability.Env) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Ensure the qa vocabulary and lane boards exist for a project",
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
				fmt.Fprintf(env.Stdout(), "ensured qa lanes for %s\n", project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
