package release

import (
	"fmt"
	"strings"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// Command mounts the release verb tree.
func (Cap) Command(env capability.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   CapabilityName,
		Short: "Release records: cut a version, include the work that shipped in it, ship it",
		Long: "The release capability is a REGISTRY, not a flow: no lanes, no " +
			"wiring, no inbox to triage. A release is an ordinary task whose " +
			"comment thread is its log; members carry its version label and " +
			"point back at it. Which work belongs in a release is the " +
			"decider's judgment, made against this capability's guide — the " +
			"verbs check only that the pieces fit together.",
	}
	env.BindActorFlag(cmd)
	cmd.AddCommand(newCutCmd(env))
	cmd.AddCommand(newMembershipCmd(env, true))
	cmd.AddCommand(newMembershipCmd(env, false))
	cmd.AddCommand(newShipCmd(env))
	cmd.AddCommand(newReportCmd(env))
	cmd.AddCommand(newSeedCmd(env))
	return cmd
}

func openRecorder(env capability.Env) (core.Service, *Recorder, error) {
	actor, err := env.RequireMutatingActor()
	if err != nil {
		return nil, nil, err
	}
	svc, err := env.OpenService()
	if err != nil {
		return nil, nil, err
	}
	return svc, &Recorder{Store: svc, Actor: actor}, nil
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

func newCutCmd(env capability.Env) *cobra.Command {
	var project, version string
	cmd := &cobra.Command{
		Use:   "cut",
		Short: "Create a release record for a version",
		Long: "The version becomes a label value: dots and underscores become " +
			"dashes (v1.2 -> release:v1-2), and anything the label grammar " +
			"cannot carry is refused rather than written and bounced.",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, rec, err := openRecorder(env)
			if err != nil {
				return err
			}
			if _, err := svc.GetProject(project); err != nil {
				return fmt.Errorf("project %q: %w", project, err)
			}
			t, err := rec.Cut(project, version)
			if err != nil {
				return err
			}
			value, _ := SanitizeVersion(version)
			return emitTask(env, svc, t.ID, "release "+version+" cut as "+VersionLabel(project, value), map[string]any{"version": value})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&version, "version", "", "version, as humans write it (e.g. v1.2)")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}

func newMembershipCmd(env capability.Env, include bool) *cobra.Command {
	var id, legacy, container string
	use, short := "include", "Add a task to a release"
	if !include {
		use, short = "exclude", "Remove a task from a release that has not shipped"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := env.ResolveTaskID(id, legacy)
			if err != nil {
				return err
			}
			svc, rec, err := openRecorder(env)
			if err != nil {
				return err
			}
			line := "included in " + container
			if include {
				err = rec.Include(taskID, container)
			} else {
				err, line = rec.Exclude(taskID, container), "excluded from "+container
			}
			if err != nil {
				return err
			}
			return emitTask(env, svc, taskID, line, map[string]any{"release": container})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&container, "release", "", "release container task id")
	_ = cmd.MarkFlagRequired("release")
	return cmd
}

func newShipCmd(env capability.Env) *cobra.Command {
	var container string
	cmd := &cobra.Command{
		Use:   "ship",
		Short: "Stamp the release and every member as shipped, and log it",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, rec, err := openRecorder(env)
			if err != nil {
				return err
			}
			stamped, err := rec.Ship(container)
			if err != nil {
				return err
			}
			return emitTask(env, svc, container, fmt.Sprintf("shipped; %d task(s) stamped", len(stamped)), map[string]any{"stamped": stamped})
		},
	}
	cmd.Flags().StringVar(&container, "release", "", "release container task id")
	_ = cmd.MarkFlagRequired("release")
	return cmd
}

func newReportCmd(env capability.Env) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "List the project's releases, their rosters, and each member's public labels (read-only)",
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
				for _, c := range rep.Containers {
					state := "open"
					if c.Shipped {
						state = "shipped"
					}
					fmt.Fprintf(w, "%s\trelease\t%s\t%s, %d member(s)\n", c.TaskID, c.Version, state, len(c.Members))
					for _, m := range c.Members {
						labels := strings.Join(m.Labels, " ")
						if m.Missing {
							labels = "MISSING"
						}
						fmt.Fprintf(w, "%s\tmember\t%s\t%s\n", m.TaskID, c.Version, labels)
					}
				}
				for _, f := range rep.Findings {
					fmt.Fprintf(w, "%s\tfinding\t%s\n", f.TaskID, f.Detail)
				}
				fmt.Fprintf(w, "%d release(s), %d finding(s)\n", len(rep.Containers), len(rep.Findings))
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
		Short: "Ensure the release vocabulary exists for a project",
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
			if _, err := EnsureVocabulary(svc, project, actor); err != nil {
				return err
			}
			return env.Emit(map[string]any{"project": project, "labels": []string{NamespaceLabel(project), ShippedLabel(project)}}, func() {
				fmt.Fprintf(env.Stdout(), "ensured release vocabulary for %s\n", project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
