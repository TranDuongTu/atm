package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type QueryFilters struct {
	Project string
	// Labels AND-intersect; full label names. A name may be a board — a
	// computed label — in which case its expression is evaluated. Suffix
	// wildcards (e.g. "ATM:status:*", "ATM:*") declare facets and do NOT
	// restrict; see GroupTasks.
	Labels []string
	// Expr is an ad-hoc board expression (AND/OR/NOT/parens over bare label
	// names). Empty means no expression filter. ANDs with Labels.
	Expr string
}

type LabelGroup struct {
	Label string
	Tasks []*Task
}

func (s *Store) ListTasks(filters QueryFilters) []*Task {
	out, _ := s.ListTasksErr(filters)
	return out
}

// ListTasksErr is ListTasks plus the error, for callers that must surface a
// bad or cyclic expression instead of silently returning nothing.
func (s *Store) ListTasksErr(filters QueryFilters) ([]*Task, error) {
	var codes []string
	if filters.Project != "" {
		codes = []string{filters.Project}
	} else {
		for _, p := range s.ListProjects() {
			codes = append(codes, p.Code)
		}
	}
	restricting := restrictingTokens(filters.Labels)
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var out []*Task
	for _, code := range codes {
		// The project-scoped list read has no freshness gate of its own: under
		// v1 every mutator write-throughs the cache inside the project lock, so
		// the rows could not lag the log. Under v2 an EXTERNAL append (a second
		// process, or a writer that died between the append commit point and its
		// reprojection) leaves the cache legitimately behind the event file, and
		// nothing else on this path would ever notice. Gate it.
		if f, _ := s.projectFormat(code); f == StoreFormatV2 {
			if err := s.ensureV2CacheFresh(code); err != nil {
				continue // match the existing per-code lenient error posture
			}
		}
		tasks, err := cacheListTasksForProject(db, code)
		if err != nil {
			continue
		}
		r := newResolver(code, s.LabelList(code, ""))

		// Each restricting token becomes an atom; they AND together. A token
		// naming a board resolves through its expression, which is what makes
		// `--label ATM:next-sprint` work with no new flag.
		var nodes []Node
		for _, tok := range restricting {
			nodes = append(nodes, &AtomNode{Name: strings.TrimPrefix(tok, code+":")})
		}
		if filters.Expr != "" {
			n, err := ParseExpr(filters.Expr)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, n)
		}
		for _, t := range tasks {
			ok := true
			for _, n := range nodes {
				m, err := r.Matches(t, n)
				if err != nil {
					return nil, err
				}
				if !m {
					ok = false
					break
				}
			}
			if ok {
				out = append(out, t)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci, ni, _ := ParseTaskID(out[i].ID)
		cj, nj, _ := ParseTaskID(out[j].ID)
		if ci != cj {
			return ci < cj
		}
		if ni != nj {
			return ni < nj
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// ErrBoardNotAFacet is returned by GroupTasksErr when a wildcard token's base
// names a board: a board has no members, so faceting by one is meaningless.
var ErrBoardNotAFacet = errors.New("a board has no members and cannot be a facet")

func (s *Store) GroupTasks(filters QueryFilters) ([]LabelGroup, []*Task) {
	g, o, _ := s.GroupTasksErr(filters)
	return g, o
}

func (s *Store) GroupTasksErr(filters QueryFilters) ([]LabelGroup, []*Task, error) {
	// I5: faceting by a board is meaningless — it has no members.
	for _, w := range wildcardTokens(filters.Labels) {
		base := strings.TrimSuffix(w, ":*")
		if l, err := s.LabelShow(base); err == nil && l.Expr != "" {
			return nil, nil, fmt.Errorf("%w: %s", ErrBoardNotAFacet, base)
		}
	}
	inScope, err := s.ListTasksErr(filters)
	if err != nil {
		return nil, nil, err
	}
	wildcards := wildcardTokens(filters.Labels)
	if len(wildcards) == 0 {
		return nil, inScope, nil
	}
	buckets := map[string][]*Task{}
	order := []string{}
	for _, t := range inScope {
		for _, w := range wildcards {
			for _, l := range t.Labels {
				if labelMatchesWildcard(l, w) {
					if _, exists := buckets[l]; !exists {
						order = append(order, l)
					}
					buckets[l] = append(buckets[l], t)
				}
			}
		}
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
	return groups, others, nil
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

func (s *Store) listTaskIDs(code string) []string {
	db, err := s.cacheDB()
	if err != nil {
		return nil
	}
	ids, err := cacheListTaskIDs(db, code)
	if err != nil {
		return nil
	}
	return ids
}
