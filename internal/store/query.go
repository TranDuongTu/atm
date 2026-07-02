package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type QueryFilters struct {
	Project string
	Labels  []string // AND-intersect; full label names; may include suffix-only
	// wildcards (e.g. "ATM:status:*", "ATM:*") which declare facets and do NOT restrict.
}

type LabelGroup struct {
	Label string
	Tasks []*Task
}

func (s *Store) ListTasks(filters QueryFilters) []*Task {
	var codes []string
	if filters.Project != "" {
		codes = []string{filters.Project}
	} else {
		for _, p := range s.ListProjects() {
			codes = append(codes, p.Code)
		}
	}
	restricting := restrictingTokens(filters.Labels)
	var out []*Task
	for _, code := range codes {
		for _, id := range s.listTaskIDs(code) {
			t, err := s.GetTask(id)
			if err != nil {
				continue
			}
			if !taskMatchesLabels(t, restricting) {
				continue
			}
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci, ni, _ := ParseTaskID(out[i].ID)
		cj, nj, _ := ParseTaskID(out[j].ID)
		if ci != cj {
			return ci < cj
		}
		return ni < nj
	})
	return out
}

func (s *Store) GroupTasks(filters QueryFilters) ([]LabelGroup, []*Task) {
	inScope := s.ListTasks(filters)
	wildcards := wildcardTokens(filters.Labels)
	if len(wildcards) == 0 {
		return nil, inScope
	}
	buckets := map[string][]*Task{}
	order := []string{}
	for _, t := range inScope {
		matched := false
		for _, w := range wildcards {
			for _, l := range t.Labels {
				if labelMatchesWildcard(l, w) {
					if _, exists := buckets[l]; !exists {
						order = append(order, l)
					}
					buckets[l] = append(buckets[l], t)
					matched = true
				}
			}
		}
		_ = matched
	}
	sort.Strings(order)
	var groups []LabelGroup
	for _, l := range order {
		groups = append(groups, LabelGroup{Label: l, Tasks: buckets[l]})
	}
	var others []*Task
	for _, t := range inScope {
		matched := false
		for _, w := range wildcards {
			for _, l := range t.Labels {
				if labelMatchesWildcard(l, w) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			others = append(others, t)
		}
	}
	return groups, others
}

func restrictingTokens(labels []string) []string {
	var out []string
	for _, l := range labels {
		if !isWildcard(l) {
			out = append(out, l)
		}
	}
	return out
}

func wildcardTokens(labels []string) []string {
	var out []string
	for _, l := range labels {
		if isWildcard(l) {
			out = append(out, l)
		}
	}
	return out
}

func isWildcard(l string) bool { return strings.HasSuffix(l, ":*") }

func labelMatchesWildcard(label, wildcard string) bool {
	prefix := strings.TrimSuffix(wildcard, "*")
	return strings.HasPrefix(label, prefix)
}

func taskMatchesLabels(t *Task, labels []string) bool {
	for _, want := range labels {
		found := false
		for _, l := range t.Labels {
			if l == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (s *Store) listTaskIDs(code string) []string {
	entries, err := os.ReadDir(s.tasksDir(code))
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
	}
	SortTaskIDs(ids)
	return ids
}