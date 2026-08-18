package workflowrpi

import (
	"fmt"

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
		Short: "Manager-oriented RPI lanes: backlog, product, pipeline, reject (the workflow_rpi perspective)",
		Long: "The workflow_rpi capability is a manager perspective over EVERY " +
			"task in the project: backlog is the unset set (NOT rpi:*), and the " +
			"product, pipeline, and reject lanes are stored labels the verbs keep " +
			"exclusive. Task topology (product_of, depends_on, relates_to, " +
			"covered_by) lives only in this capability's metadata key — there is " +
			"no ATM-wide link model and no task ID in a label. The store enforces " +
			"nothing. This is a paved road, not a fence.",
	}
	env.BindActorFlag(cmd)
	cmd.AddCommand(newProductCmd(env))
	cmd.AddCommand(newPipelineCmd(env))
	cmd.AddCommand(newRejectCmd(env))
	cmd.AddCommand(newReleaseCmd(env))
	cmd.AddCommand(newStatusCmd(env))
	cmd.AddCommand(newLinkCmd(env, true))
	cmd.AddCommand(newLinkCmd(env, false))
	cmd.AddCommand(newLinksCmd(env))
	cmd.AddCommand(newReportCmd(env))
	cmd.AddCommand(newSeedCmd(env))
	return cmd
}

// laneDisplay renders the empty lane as the word humans read.
func laneDisplay(v string) string {
	if v == "" {
		return "backlog"
	}
	return v
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

// emitLane prints the lane transition line and emits the updated task.
func emitLane(env capability.Env, svc core.Service, taskID, prior, lane, detail string) error {
	t, err := svc.GetTask(taskID)
	if err != nil {
		return err
	}
	return env.Emit(map[string]any{"task": env.TaskJSON(t), "lane": lane, "prior": laneDisplay(prior)}, func() {
		fmt.Fprintf(env.Stdout(), "%s: rpi %s -> %s (%s)\n", t.ID, laneDisplay(prior), lane, detail)
	})
}

func newProductCmd(env capability.Env) *cobra.Command {
	var id, legacy, status string
	cmd := &cobra.Command{
		Use:   "product",
		Short: "Move a task into the product roadmap lane (any lane → product)",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			prior, err := rec.Product(taskID, status)
			if err != nil {
				return err
			}
			s := status
			if s == "" {
				s = ProductUnclarified
			}
			return emitLane(env, svc, taskID, prior, LaneProduct, "status: "+s)
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&status, "status", "", "product status: unclarified|clarified (default unclarified)")
	return cmd
}

func newPipelineCmd(env capability.Env) *cobra.Command {
	var id, legacy, product, status string
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Move a task into the build pipeline lane, linked to its product task",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			prior, err := rec.Pipeline(taskID, product, status)
			if err != nil {
				return err
			}
			s := status
			if s == "" {
				s = DevClarified
			}
			return emitLane(env, svc, taskID, prior, LanePipeline, "product: "+product+", status: "+s)
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&product, "product", "", "product-lane task this work belongs to (required)")
	cmd.Flags().StringVar(&status, "status", "", "dev status: clarified|brainstormed|planned|implementing|review|done (default clarified)")
	_ = cmd.MarkFlagRequired("product")
	return cmd
}

func newRejectCmd(env capability.Env) *cobra.Command {
	var id, legacy, reason, coveredBy string
	cmd := &cobra.Command{
		Use:   "reject",
		Short: "Record a task as considered and rejected from the RPI perspective",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			prior, err := rec.Reject(taskID, reason, coveredBy)
			if err != nil {
				return err
			}
			rs := reason
			if rs == "" {
				rs = RejectNotWorthIt
			}
			detail := "reason: " + rs
			if coveredBy != "" {
				detail += ", covered by: " + coveredBy
			}
			return emitLane(env, svc, taskID, prior, LaneReject, detail)
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&reason, "reason", "", "reject reason: duplicate|out-of-scope|not-worth-it|covered-by (default not-worth-it)")
	cmd.Flags().StringVar(&coveredBy, "covered-by", "", "task that covers this one (required for reason covered-by)")
	return cmd
}

func newReleaseCmd(env capability.Env) *cobra.Command {
	var id, legacy, reason string
	cmd := &cobra.Command{
		Use:   "release",
		Short: "Return a task to RPI backlog: clear this capability's labels and metadata, log the reason",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			prior, err := rec.Release(taskID, reason)
			if err != nil {
				return err
			}
			t, err := svc.GetTask(taskID)
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": env.TaskJSON(t), "lane": "backlog", "prior": laneDisplay(prior)}, func() {
				fmt.Fprintf(env.Stdout(), "%s: rpi %s -> backlog (%s)\n", t.ID, laneDisplay(prior), reason)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&reason, "reason", "", "why the task leaves the RPI lanes (required)")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newStatusCmd(env capability.Env) *cobra.Command {
	var id, legacy, productStatus, devStatus string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Update a task's lane-local status in place (product or pipeline)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if (productStatus == "") == (devStatus == "") {
				return fmt.Errorf("exactly one of --product-status or --dev-status is required")
			}
			taskID, svc, rec, err := openRecorder(env, id, legacy)
			if err != nil {
				return err
			}
			lane, status := LaneProduct, productStatus
			if devStatus != "" {
				lane, status = LanePipeline, devStatus
				err = rec.SetDevStatus(taskID, devStatus)
			} else {
				err = rec.SetProductStatus(taskID, productStatus)
			}
			if err != nil {
				return err
			}
			t, err := svc.GetTask(taskID)
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": env.TaskJSON(t), "lane": lane, "status": status}, func() {
				fmt.Fprintf(env.Stdout(), "%s: rpi %s status -> %s\n", t.ID, lane, status)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&productStatus, "product-status", "", "product status: unclarified|clarified")
	cmd.Flags().StringVar(&devStatus, "dev-status", "", "dev status: clarified|brainstormed|planned|implementing|review|done")
	return cmd
}

func newLinkCmd(env capability.Env, link bool) *cobra.Command {
	var id, legacy, dependsOn, relatesTo string
	use, short := "link", "Record a task link (exactly one of --depends-on / --relates-to)"
	if !link {
		use, short = "unlink", "Remove a task link (exactly one of --depends-on / --relates-to)"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			if (dependsOn == "") == (relatesTo == "") {
				return fmt.Errorf("exactly one of --depends-on or --relates-to is required")
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
				err, verb, desc = rec.LinkRelatesTo(taskID, relatesTo), "linked", "relates_to "+relatesTo
			case dependsOn != "":
				err, verb, desc = rec.UnlinkDependsOn(taskID, dependsOn), "unlinked", "depends_on "+dependsOn
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
	cmd.Flags().StringVar(&dependsOn, "depends-on", "", "task this one depends on (execution dependency)")
	cmd.Flags().StringVar(&relatesTo, "relates-to", "", "related task (generic, semantics-free)")
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
				if l.ProductOf != "" {
					fmt.Fprintf(env.Stdout(), "product_of: %s\n", l.ProductOf)
				}
				if l.CoveredBy != "" {
					fmt.Fprintf(env.Stdout(), "covered_by: %s\n", l.CoveredBy)
				}
				for _, x := range l.DependsOn {
					fmt.Fprintf(env.Stdout(), "depends_on: %s\n", x)
				}
				for _, x := range l.RelatesTo {
					fmt.Fprintf(env.Stdout(), "relates_to: %s\n", x)
				}
				for _, x := range l.PipelineChildren {
					fmt.Fprintf(env.Stdout(), "pipeline_child: %s\n", x)
				}
				for _, x := range l.Dependents {
					fmt.Fprintf(env.Stdout(), "dependent: %s\n", x)
				}
				for _, x := range l.RelatedFrom {
					fmt.Fprintf(env.Stdout(), "related_from: %s\n", x)
				}
				for _, x := range l.Covered {
					fmt.Fprintf(env.Stdout(), "covers: %s\n", x)
				}
				if l.ProductOf == "" && l.CoveredBy == "" &&
					len(l.DependsOn)+len(l.RelatesTo)+len(l.PipelineChildren)+len(l.Dependents)+len(l.RelatedFrom)+len(l.Covered) == 0 {
					fmt.Fprintf(env.Stdout(), "no links\n")
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
		Short: "Report RPI lanes, the backlog count, and what is at risk (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			if _, err := svc.GetProject(project); err != nil {
				return fmt.Errorf("project %q: %w", project, err)
			}
			rep, err := (&Reporter{Store: svc}).Report(project)
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"report": rep}, func() {
				fmt.Fprintf(env.Stdout(), "backlog\t%d\n", rep.Backlog)
				for _, s := range rep.Product {
					fmt.Fprintf(env.Stdout(), "%s\tproduct\t%s\tchildren: %d\n", s.TaskID, s.Status, s.Children)
				}
				for _, s := range rep.Pipeline {
					parent := s.ProductOf
					if parent == "" {
						parent = "none"
					}
					fmt.Fprintf(env.Stdout(), "%s\tpipeline\t%s\tproduct: %s, deps: %d, blocked by: %d\n",
						s.TaskID, s.Status, parent, len(s.DependsOn), len(s.BlockedBy))
				}
				for _, s := range rep.Reject {
					fmt.Fprintf(env.Stdout(), "%s\treject\t%s\n", s.TaskID, s.Status)
				}
				for _, f := range rep.Findings {
					fmt.Fprintf(env.Stdout(), "%s\tat-risk\t%s\n", f.TaskID, f.Detail)
				}
				fmt.Fprintf(env.Stdout(), "%d product, %d pipeline, %d reject, %d backlog, %d at risk\n",
					len(rep.Product), len(rep.Pipeline), len(rep.Reject), rep.Backlog, len(rep.Findings))
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
		Short: "Ensure the workflow_rpi vocabulary and boards exist for a project",
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
				fmt.Fprintf(env.Stdout(), "ensured workflow_rpi boards for %s\n", project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
