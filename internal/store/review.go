package store

import (
	"fmt"
	"sort"
)

type ReviewQueueTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type ReviewQueueGroup struct {
	Claimant string            `json:"claimant"`
	Tasks    []ReviewQueueTask `json:"tasks"`
}

type ReviewQueueResult struct {
	Groups []ReviewQueueGroup `json:"groups"`
}

type OpenFollowup struct {
	ID       string `json:"id"`
	Followup string `json:"followup"`
	Text     string `json:"text"`
	Assignee string `json:"assignee"`
}

type DashboardResult struct {
	Project       string             `json:"project"`
	ReviewQueue   ReviewQueueResult  `json:"review_queue"`
	OpenFollowups []OpenFollowup     `json:"open_followups"`
	GuideStatus   *GuideStatusResult `json:"guide_status"`
}

func (s *Store) RequestReview(id, actor string) error {
	if err := ValidateActorID(actor); err != nil {
		return err
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	return s.WithLock(code, func() error {
		if err := s.registerActorLazy(actor); err != nil {
			return err
		}
		t, err := s.GetTask(id)
		if err != nil {
			return err
		}
		if t.Status == "review" {
			return nil
		}
		if !allowedTransitions[t.Status]["review"] {
			return fmt.Errorf("%w: invalid transition %s -> review", ErrConflict, t.Status)
		}
		from := t.Status
		now := Now()
		t.Status = "review"
		t.UpdatedAt = now
		t.appendHistoryAt("status-changed", actor, now, map[string]any{"from": from, "to": "review"})
		t.appendHistoryAt("review-requested", actor, now, map[string]any{})
		return WriteJSON(s.taskPath(id), t)
	})
}

func (s *Store) ApproveReview(id, actor, comment string) (*Task, error) {
	return s.finishReview(id, actor, comment, "done", "approved")
}

func (s *Store) RejectReview(id, actor, comment string) (*Task, error) {
	return s.finishReview(id, actor, comment, "in-progress", "rejected")
}

func (s *Store) finishReview(id, actor, comment, targetStatus, action string) (*Task, error) {
	if err := ValidateActorID(actor); err != nil {
		return nil, err
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	var out *Task
	err := s.WithLock(code, func() error {
		if err := s.registerActorLazy(actor); err != nil {
			return err
		}
		t, err := s.GetTask(id)
		if err != nil {
			return err
		}
		if t.Status == targetStatus {
			out = t
			return nil
		}
		if !allowedTransitions[t.Status][targetStatus] {
			return fmt.Errorf("%w: invalid transition %s -> %s", ErrConflict, t.Status, targetStatus)
		}
		from := t.Status
		now := Now()
		t.Status = targetStatus
		t.UpdatedAt = now
		t.appendHistoryAt("status-changed", actor, now, map[string]any{"from": from, "to": targetStatus})
		if comment != "" {
			n := t.nextCounter("d")
			t.Discussions = append(t.Discussions, DiscussionEntry{
				ID:     fmt.Sprintf("d%d", n),
				Text:   comment,
				Author: actor,
				At:     now,
			})
			t.appendHistoryAt("discussion-added", actor, now, map[string]any{})
		}
		t.appendHistoryAt(action, actor, now, map[string]any{})
		if err := WriteJSON(s.taskPath(id), t); err != nil {
			return err
		}
		out = t
		return nil
	})
	return out, err
}

func (s *Store) ReviewQueue(projectCode string) (*ReviewQueueResult, error) {
	if projectCode != "" {
		if err := ValidateProjectCode(projectCode); err != nil {
			return nil, err
		}
	}
	tasks := s.ListTasks(QueryFilters{Project: projectCode, Status: "review"})
	groupsMap := map[string][]ReviewQueueTask{}
	var claimants []string
	for _, t := range tasks {
		claimant := ""
		if t.Claim != nil {
			claimant = t.Claim.Actor
		}
		if _, ok := groupsMap[claimant]; !ok {
			claimants = append(claimants, claimant)
		}
		groupsMap[claimant] = append(groupsMap[claimant], ReviewQueueTask{ID: t.ID, Title: t.Title})
	}
	sort.Strings(claimants)
	groups := make([]ReviewQueueGroup, 0, len(claimants))
	for _, c := range claimants {
		g := groupsMap[c]
		sort.SliceStable(g, func(i, j int) bool { return g[i].ID < g[j].ID })
		groups = append(groups, ReviewQueueGroup{Claimant: c, Tasks: g})
	}
	return &ReviewQueueResult{Groups: groups}, nil
}

func (s *Store) OpenFollowups(projectCode string) ([]OpenFollowup, error) {
	if projectCode != "" {
		if err := ValidateProjectCode(projectCode); err != nil {
			return nil, err
		}
	}
	tasks := s.ListTasks(QueryFilters{Project: projectCode})
	var out []OpenFollowup
	for _, t := range tasks {
		for _, f := range t.Followups {
			if f.Status != "open" {
				continue
			}
			out = append(out, OpenFollowup{
				ID:       t.ID,
				Followup: f.ID,
				Text:     f.Text,
				Assignee: f.Assignee,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Followup < out[j].Followup
	})
	return out, nil
}

func (s *Store) Dashboard(projectCode string) (*DashboardResult, error) {
	if err := ValidateProjectCode(projectCode); err != nil {
		return nil, err
	}
	queue, err := s.ReviewQueue(projectCode)
	if err != nil {
		return nil, err
	}
	followups, err := s.OpenFollowups(projectCode)
	if err != nil {
		return nil, err
	}
	gs, err := s.GuideStatus(projectCode)
	if err != nil {
		return nil, err
	}
	if followups == nil {
		followups = []OpenFollowup{}
	}
	return &DashboardResult{
		Project:       projectCode,
		ReviewQueue:   *queue,
		OpenFollowups: followups,
		GuideStatus:   gs,
	}, nil
}
