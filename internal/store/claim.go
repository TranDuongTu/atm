package store

import (
	"fmt"
	"sort"
)

func (s *Store) Next(projectCode string, claim bool, actor string) (*Task, *Guide, error) {
	if err := ValidateProjectCode(projectCode); err != nil {
		return nil, nil, err
	}
	if claim {
		if err := ValidateActorID(actor); err != nil {
			return nil, nil, err
		}
	}

	var picked *Task
	var guide *Guide
	err := s.WithLock(projectCode, func() error {
		if claim {
			if err := s.registerActorLazy(actor); err != nil {
				return err
			}
		}
		p, err := s.GetProject(projectCode)
		if err != nil {
			return err
		}
		guide = p.Guide

		ids := s.listTaskIDs(projectCode)
		candidates := make([]nextCandidate, 0, len(ids))
		for _, id := range ids {
			t, err := s.GetTask(id)
			if err != nil {
				continue
			}
			if t.Status == "done" || t.Status == "cancelled" {
				continue
			}
			if t.Claim != nil {
				continue
			}
			if hasLabel(t.Labels, "kind:convention") {
				continue
			}
			bbc := s.blockedByCount(projectCode, t.ID)
			if bbc > 0 {
				continue
			}
			candidates = append(candidates, nextCandidate{task: t, blockedBy: bbc})
		}
		if len(candidates) == 0 {
			return nil
		}

		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].blockedBy != candidates[j].blockedBy {
				return candidates[i].blockedBy < candidates[j].blockedBy
			}
			return candidates[i].task.CreatedAt.Before(candidates[j].task.CreatedAt)
		})

		picked = candidates[0].task
		if claim {
			ts := Now()
			picked.Claim = &Claim{Actor: actor, At: ts}
			picked.UpdatedAt = ts
			picked.appendHistory("claimed", actor, map[string]any{})
			if err := WriteJSON(s.taskPath(picked.ID), picked); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return picked, guide, nil
}

type nextCandidate struct {
	task      *Task
	blockedBy int
}

func (s *Store) blockedByCount(projectCode, id string) int {
	ids := s.listTaskIDs(projectCode)
	count := 0
	for _, otherID := range ids {
		if otherID == id {
			continue
		}
		other, err := s.GetTask(otherID)
		if err != nil {
			continue
		}
		if other.Status == "done" || other.Status == "cancelled" {
			continue
		}
		for _, l := range other.Links {
			if l.Type == "blocks" && l.Target == id {
				count++
			}
		}
	}
	return count
}

func (s *Store) Claim(id, actor string) (*Task, error) {
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
		if t.Claim != nil && t.Claim.Actor != actor {
			return fmt.Errorf("%w: task %q already claimed by %s", ErrConflict, id, t.Claim.Actor)
		}
		if t.Claim != nil && t.Claim.Actor == actor {
			out = t
			return nil
		}
		ts := Now()
		t.Claim = &Claim{Actor: actor, At: ts}
		t.UpdatedAt = ts
		t.appendHistory("claimed", actor, map[string]any{})
		if err := WriteJSON(s.taskPath(id), t); err != nil {
			return err
		}
		out = t
		return nil
	})
	return out, err
}

func (s *Store) Unclaim(id, actor string) (*Task, error) {
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
		if t.Claim == nil {
			out = t
			return nil
		}
		t.Claim = nil
		t.UpdatedAt = Now()
		t.appendHistory("unclaimed", actor, map[string]any{})
		if err := WriteJSON(s.taskPath(id), t); err != nil {
			return err
		}
		out = t
		return nil
	})
	return out, err
}
