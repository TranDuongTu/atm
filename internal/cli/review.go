package cli

import (
	"fmt"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

func newReviewCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Review (human coordinator) commands",
	}
	cmd.AddCommand(newReviewRequestCmd(st))
	cmd.AddCommand(newReviewApproveCmd(st))
	cmd.AddCommand(newReviewRejectCmd(st))
	cmd.AddCommand(newReviewQueueCmd(st))
	cmd.AddCommand(newReviewFollowupsCmd(st))
	cmd.AddCommand(newReviewDashboardCmd(st))
	return cmd
}

func newReviewRequestCmd(st *cliState) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "request",
		Short: "Request a review (status -> review)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RequestReview(id, actor); err != nil {
				return err
			}
			t, err := s.GetTask(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s status -> %s\n", t.ID, t.Status)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newReviewApproveCmd(st *cliState) *cobra.Command {
	var id, comment string
	cmd := &cobra.Command{
		Use:   "approve",
		Short: "Approve a review (status -> done)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			t, err := s.ApproveReview(id, actor, comment)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s approved -> done\n", t.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&comment, "comment", "", "review comment (recorded as discussion)")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newReviewRejectCmd(st *cliState) *cobra.Command {
	var id, comment string
	cmd := &cobra.Command{
		Use:   "reject",
		Short: "Reject a review (status -> in-progress, comment recorded)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			t, err := s.RejectReview(id, actor, comment)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s rejected -> %s\n", t.ID, t.Status)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&comment, "comment", "", "review comment (recorded as discussion)")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newReviewQueueCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "List tasks in review grouped by claimant",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			res, err := s.ReviewQueue(project)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"groups": reviewQueueGroupsToJSON(res.Groups)}, func() {
				for _, g := range res.Groups {
					fmt.Fprintf(st.stdout(), "%s:\n", g.Claimant)
					for _, t := range g.Tasks {
						fmt.Fprintf(st.stdout(), "  %s %s\n", t.ID, t.Title)
					}
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	return cmd
}

func newReviewFollowupsCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "followups",
		Short: "List open followups",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			out, err := s.OpenFollowups(project)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"followups": openFollowupsToJSON(out)}, func() {
				for _, f := range out {
					fmt.Fprintf(st.stdout(), "%s %s %s\n", f.ID, f.Followup, f.Text)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	return cmd
}

func newReviewDashboardCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Coordinator dashboard: review queue + open followups + guide status",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			d, err := s.Dashboard(project)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), dashboardToJSON(d), func() {
				fmt.Fprintf(st.stdout(), "project: %s\n", d.Project)
				fmt.Fprintf(st.stdout(), "review queue: %d group(s)\n", len(d.ReviewQueue.Groups))
				fmt.Fprintf(st.stdout(), "open followups: %d\n", len(d.OpenFollowups))
				if d.GuideStatus != nil {
					fmt.Fprintf(st.stdout(), "guide: %d section(s)\n", d.GuideStatus.Coverage.TotalSections)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

type jsonReviewQueueTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type jsonReviewQueueGroup struct {
	Claimant string                `json:"claimant"`
	Tasks    []jsonReviewQueueTask `json:"tasks"`
}

type jsonOpenFollowup struct {
	ID       string `json:"id"`
	Followup string `json:"followup"`
	Text     string `json:"text"`
	Assignee string `json:"assignee"`
}

type jsonDashboard struct {
	Project       string             `json:"project"`
	ReviewQueue   jsonReviewQueue    `json:"review_queue"`
	OpenFollowups []jsonOpenFollowup `json:"open_followups"`
	GuideStatus   map[string]any     `json:"guide_status"`
}

type jsonReviewQueue struct {
	Groups []jsonReviewQueueGroup `json:"groups"`
}

func reviewQueueGroupsToJSON(groups []store.ReviewQueueGroup) []jsonReviewQueueGroup {
	out := make([]jsonReviewQueueGroup, 0, len(groups))
	for _, g := range groups {
		tasks := make([]jsonReviewQueueTask, 0, len(g.Tasks))
		for _, t := range g.Tasks {
			tasks = append(tasks, jsonReviewQueueTask{ID: t.ID, Title: t.Title})
		}
		out = append(out, jsonReviewQueueGroup{Claimant: g.Claimant, Tasks: tasks})
	}
	return out
}

func openFollowupsToJSON(fus []store.OpenFollowup) []jsonOpenFollowup {
	out := make([]jsonOpenFollowup, 0, len(fus))
	for _, f := range fus {
		out = append(out, jsonOpenFollowup{ID: f.ID, Followup: f.Followup, Text: f.Text, Assignee: f.Assignee})
	}
	return out
}

func guideStatusToJSON(gs *store.GuideStatusResult) map[string]any {
	if gs == nil {
		return nil
	}
	empty := []string{}
	if gs.Coverage.EmptySections != nil {
		empty = gs.Coverage.EmptySections
	}
	freshness := make([]map[string]any, 0, len(gs.Freshness))
	for _, f := range gs.Freshness {
		entry := map[string]any{
			"section": f.Section,
			"kind":    f.Kind,
			"target":  f.Target,
			"state":   f.State,
		}
		if f.UpdatedAt != "" {
			entry["updated_at"] = f.UpdatedAt
		}
		freshness = append(freshness, entry)
	}
	return map[string]any{
		"coverage": map[string]any{
			"empty_sections": empty,
			"total_sections": gs.Coverage.TotalSections,
			"total_refs":     gs.Coverage.TotalRefs,
		},
		"freshness": freshness,
	}
}

func dashboardToJSON(d *store.DashboardResult) jsonDashboard {
	return jsonDashboard{
		Project:       d.Project,
		ReviewQueue:   jsonReviewQueue{Groups: reviewQueueGroupsToJSON(d.ReviewQueue.Groups)},
		OpenFollowups: openFollowupsToJSON(d.OpenFollowups),
		GuideStatus:   guideStatusToJSON(d.GuideStatus),
	}
}
