package store

import (
	"sort"
)

type Convention struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	MatchedLabels []string `json:"matched_labels"`
}

type ShowWithContextResult struct {
	Task    *Task          `json:"task"`
	Context ContextPayload `json:"context"`
}

type ContextPayload struct {
	LinksOut    []LinkEdge      `json:"links_out"`
	LinksIn     []LinkEdge      `json:"links_in"`
	Conventions []Convention    `json:"conventions"`
	Timeline    []TimelineEntry `json:"timeline"`
	Guide       *Guide          `json:"guide"`
}

func (s *Store) ShowWithContext(id string) (*ShowWithContextResult, error) {
	t, err := s.GetTask(id)
	if err != nil {
		return nil, err
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return nil, err
	}

	links, err := s.LinkList(id)
	if err != nil {
		return nil, err
	}

	p, err := s.GetProject(code)
	if err != nil {
		return nil, err
	}

	conventions := s.matchingConventions(code, p, t)

	timeline, err := s.TimelineList(id)
	if err != nil {
		return nil, err
	}

	return &ShowWithContextResult{
		Task: t,
		Context: ContextPayload{
			LinksOut:    links.Out,
			LinksIn:     links.In,
			Conventions: conventions,
			Timeline:    timeline,
			Guide:       p.Guide,
		},
	}, nil
}

func (s *Store) matchingConventions(code string, p *Project, t *Task) []Convention {
	ids := s.listTaskIDs(code)
	var out []Convention
	for _, cid := range ids {
		if cid == t.ID {
			continue
		}
		ct, err := s.GetTask(cid)
		if err != nil {
			continue
		}
		if !hasLabel(ct.Labels, "kind:convention") {
			continue
		}
		matched := intersectLabels(ct.Labels, t.Labels)
		if len(matched) == 0 {
			continue
		}
		out = append(out, Convention{
			ID:            ct.ID,
			Title:         ct.Title,
			MatchedLabels: matched,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if typeAxisScore(p.TypeAxis, out[i].MatchedLabels) != typeAxisScore(p.TypeAxis, out[j].MatchedLabels) {
			return typeAxisScore(p.TypeAxis, out[i].MatchedLabels) > typeAxisScore(p.TypeAxis, out[j].MatchedLabels)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func hasLabel(labels []string, label string) bool {
	for _, l := range labels {
		if l == label {
			return true
		}
	}
	return false
}

func intersectLabels(a, b []string) []string {
	set := make(map[string]bool, len(b))
	for _, l := range b {
		set[l] = true
	}
	var out []string
	for _, l := range a {
		if set[l] && !containsStr(out, l) {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func typeAxisScore(axis string, matched []string) int {
	if axis == "" {
		return 0
	}
	prefix := axis + ":"
	score := 0
	for _, l := range matched {
		if len(l) > len(prefix) && l[:len(prefix)] == prefix {
			score++
		}
	}
	return score
}
