package codereview

import (
	"fmt"
	"strings"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// Command mounts the codereview verb tree.
func (Cap) Command(env capability.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   CapabilityName,
		Short: "Code review flow: schedule, run, and record reviews against pull requests",
		Long: "The codereview capability tracks review through scheduled -> " +
			"reviewing -> done. absorb REQUIRES the pull request: a task whose " +
			"PR nobody can find stays in the inbox, and the swelling inbox " +
			"count is the only warning surface this capability needs. The " +
			"review conversation lives on the PR; the payload only says where " +
			"to look. The store enforces nothing: a paved road, not a fence.",
	}
	env.BindActorFlag(cmd)
	cmd.AddCommand(newAbsorbCmd(env))
	cmd.AddCommand(newBeginCmd(env))
	cmd.AddCommand(newFollowUpCmd(env))
	cmd.AddCommand(newFinishCmd(env))
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
	var id, legacy, pr string
	cmd := &cobra.Command{
		Use:   "absorb",
		Short: "Schedule a review, recording the pull request (required)",
		Long: "--pr is required by design. A task with no discoverable pull " +
			"request is LEFT in the inbox; that is the warning, and there is " +
			"no other.",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			if err := rec.Absorb(taskID, pr); err != nil {
				return err
			}
			return emitTask(env, svc, taskID, "review scheduled for "+pr, map[string]any{"state": StateScheduled, "pr": pr})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&pr, "pr", "", "pull request URL or number")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}

func newFollowUpCmd(env capability.Env) *cobra.Command {
	var id, legacy, title string
	cmd := &cobra.Command{
		Use:   "follow-up",
		Short: "Leave a tracked item on the board for a finding worth action beyond the artifact",
		Long: "A finding worth fixing but not worth blocking on belongs on the board, " +
			"not in another round of review. The item is born into the pipeline " +
			"beneath the review and knows which review it came from; the review " +
			"knows its items. An open item does NOT hold the review open — that " +
			"is the endless cycle this verb exists to break.",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			item, err := rec.FollowUp(taskID, title)
			if err != nil {
				return err
			}
			return emitTask(env, svc, item.ID, "follow-up "+item.ID+" from review "+taskID,
				map[string]any{"state": StateScheduled, "part_of": taskID})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&title, "title", "", "what needs doing")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newBeginCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "begin",
		Short: "Move a scheduled review to under way",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			if err := rec.Begin(taskID); err != nil {
				return err
			}
			return emitTask(env, svc, taskID, "review under way", map[string]any{"state": StateReviewing})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newFinishCmd(env capability.Env) *cobra.Command {
	var id, legacy, report string
	cmd := &cobra.Command{
		Use:   "finish",
		Short: "Stamp the finish socket and optionally record where the report lives",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			if err := rec.Finish(taskID, report); err != nil {
				return err
			}
			return emitTask(env, svc, taskID, "reviewed", map[string]any{"state": StateDone, "report": report})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&report, "report", "", "locator for the review report (optional)")
	return cmd
}

func newEvictCmd(env capability.Env) *cobra.Command {
	var id, legacy, reason string
	cmd := &cobra.Command{
		Use:   "evict",
		Short: "Settle a task out of codereview with a reason",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			if err := rec.Evict(taskID, reason); err != nil {
				return err
			}
			r := reason
			if r == "" {
				r = OutNotWarranted
			}
			return emitTask(env, svc, taskID, "evicted: "+r, map[string]any{"reason": r})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&reason, "reason", "", "evict reason: "+strings.Join(OutReasons(), "|")+" (default "+OutNotWarranted+")")
	return cmd
}

func newReleaseCmd(env capability.Env) *cobra.Command {
	var id, legacy, reason string
	cmd := &cobra.Command{
		Use:   "release",
		Short: "Withdraw codereview's perspective entirely; the task returns to the pool",
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
	cmd.Flags().StringVar(&reason, "reason", "", "why codereview is letting this go")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newReportCmd(env capability.Env) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Read the project through the codereview lens: lane rosters and findings (read-only)",
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
					fmt.Fprintf(w, "%s\tinbox\tno PR found yet\n", id)
				}
				for _, s := range rep.Pipeline {
					pr := s.PR
					if pr == "" {
						pr = "none"
					}
					fmt.Fprintf(w, "%s\tpipeline\t%s\tpr: %s\n", s.TaskID, s.State, pr)
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
		Short: "Ensure the codereview vocabulary and lane boards exist for a project",
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
				fmt.Fprintf(env.Stdout(), "ensured codereview lanes for %s\n", project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
