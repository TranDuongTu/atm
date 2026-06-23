package store

import "sort"

type QueryFilters struct {
	Project  string
	Labels   []string
	Status   string
	Assignee string
	Claimant string
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
	var out []*Task
	for _, code := range codes {
		for _, id := range s.listTaskIDs(code) {
			t, err := s.GetTask(id)
			if err != nil {
				continue
			}
			if !taskMatchesFilters(t, filters) {
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

func taskMatchesFilters(t *Task, f QueryFilters) bool {
	if f.Status != "" && t.Status != f.Status {
		return false
	}
	if f.Claimant != "" {
		if t.Claim == nil || t.Claim.Actor != f.Claimant {
			return false
		}
	}
	if f.Assignee != "" {
		found := false
		for _, fu := range t.Followups {
			if fu.Assignee == f.Assignee && fu.Status == "open" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, want := range f.Labels {
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
