package store

import (
	"fmt"
	"os"
	"sort"
)

var validLinkTypes = map[string]bool{
	"blocks":     true,
	"related-to": true,
	"implements": true,
	"documents":  true,
}

type LinkEdge struct {
	Link      Link   `json:"link"`
	Direction string `json:"direction"`
}

type LinkListResult struct {
	Out   []LinkEdge `json:"out"`
	In    []LinkEdge `json:"in"`
	Stale []LinkEdge `json:"stale,omitempty"`
}

func (s *Store) LinkAdd(id, linkType, target, actor string) error {
	if !validLinkTypes[linkType] {
		return fmt.Errorf("%w: invalid link type %q", ErrUsage, linkType)
	}
	if id == target {
		return fmt.Errorf("%w: cannot link task to itself", ErrUsage)
	}
	if _, _, ok := ParseTaskID(id); !ok {
		return fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	if _, _, ok := ParseTaskID(target); !ok {
		return fmt.Errorf("%w: invalid target task id %q", ErrUsage, target)
	}
	return s.mutateTask(id, actor, "link-added", func(t *Task) {
		for _, l := range t.Links {
			if l.Type == linkType && l.Target == target {
				return
			}
		}
		if linkType == "related-to" {
			if rev, err := s.GetTask(target); err == nil {
				for _, l := range rev.Links {
					if l.Type == "related-to" && l.Target == id {
						return
					}
				}
			}
		}
		t.Links = append(t.Links, Link{Type: linkType, Target: target})
	})
}

func (s *Store) LinkRemove(id, linkType, target, actor string) error {
	return s.mutateTask(id, actor, "link-removed", func(t *Task) {
		out := t.Links[:0]
		for _, l := range t.Links {
			if l.Type == linkType && l.Target == target {
				continue
			}
			out = append(out, l)
		}
		t.Links = out
	})
}

func (s *Store) LinkList(id string) (*LinkListResult, error) {
	t, err := s.GetTask(id)
	if err != nil {
		return nil, err
	}
	code, _, _ := ParseTaskID(id)
	res := &LinkListResult{
		Out: []LinkEdge{},
		In:  []LinkEdge{},
	}
	for _, l := range t.Links {
		edge := LinkEdge{Link: l, Direction: "out"}
		_, err := s.GetTask(l.Target)
		if os.IsNotExist(err) || (err != nil && IsNotFound(err)) {
			res.Stale = append(res.Stale, edge)
		}
		res.Out = append(res.Out, edge)
	}

	tasksInProject := s.listTaskIDs(code)
	for _, otherID := range tasksInProject {
		if otherID == id {
			continue
		}
		other, err := s.GetTask(otherID)
		if err != nil {
			continue
		}
		for _, l := range other.Links {
			if l.Target != id {
				continue
			}
			res.In = append(res.In, LinkEdge{
				Link:      Link{Type: l.Type, Target: otherID},
				Direction: "in",
			})
			if l.Type == "blocks" {
				res.In = append(res.In, LinkEdge{
					Link:      Link{Type: "blocked-by", Target: otherID},
					Direction: "in",
				})
			}
		}
	}

	for _, l := range t.Links {
		if l.Type != "related-to" {
			continue
		}
		_, err := s.GetTask(l.Target)
		if err != nil {
			continue
		}
		found := false
		for _, e := range res.In {
			if e.Link.Type == "related-to" && e.Link.Target == l.Target {
				found = true
				break
			}
		}
		if !found {
			res.In = append(res.In, LinkEdge{
				Link:      Link{Type: "related-to", Target: l.Target},
				Direction: "in",
			})
		}
	}

	sort.SliceStable(res.Out, func(i, j int) bool {
		if res.Out[i].Link.Type != res.Out[j].Link.Type {
			return res.Out[i].Link.Type < res.Out[j].Link.Type
		}
		return res.Out[i].Link.Target < res.Out[j].Link.Target
	})
	sort.SliceStable(res.In, func(i, j int) bool {
		if res.In[i].Link.Type != res.In[j].Link.Type {
			return res.In[i].Link.Type < res.In[j].Link.Type
		}
		return res.In[i].Link.Target < res.In[j].Link.Target
	})
	return res, nil
}

func (s *Store) listTaskIDs(code string) []string {
	entries, err := os.ReadDir(s.tasksDir(code))
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || len(e.Name()) < 6 || e.Name()[len(e.Name())-5:] != ".json" {
			continue
		}
		ids = append(ids, e.Name()[:len(e.Name())-5])
	}
	SortTaskIDs(ids)
	return ids
}
