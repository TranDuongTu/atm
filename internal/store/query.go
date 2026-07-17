package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"atm/internal/core"
	"atm/internal/store/eventlog"
)

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
	restricting := core.RestrictingTokens(filters.Labels)
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var out []*Task
	formats := make(map[string]eventlog.StoreFormat, len(codes))
	for _, code := range codes {
		// The project-scoped list read has no freshness gate of its own: under
		// v1 every mutator write-throughs the cache inside the project lock, so
		// the rows could not lag the log. Under v2 an EXTERNAL append (a second
		// process, or a writer that died between the append commit point and its
		// reprojection) leaves the cache legitimately behind the event file, and
		// nothing else on this path would ever notice. Gate it.
		//
		// The lookup error is PROPAGATED, not swallowed: with store.json
		// unreadable a swallowed lookup yields f == "", skips the v2 branch
		// (and its freshness gate), and serves stale cache rows with a nil
		// error -- the same silent v2 -> ungated-v1 degradation textSearch and
		// ReindexOnce already refuse.
		f, err := s.eng.ProjectFormat(code)
		if err != nil {
			return nil, err
		}
		formats[code] = f
		if f == eventlog.StoreFormatV2 {
			if err := s.ensureV2CacheFresh(code); err != nil {
				// An integrity error is on-disk truth, not a cache-DB hiccup:
				// surface it (matching store show's core.ErrIntegrity) rather than
				// silently reporting "no tasks". The lenient continue below
				// is reserved for the error class it was actually written
				// for -- see the human ruling on Task 5 Findings 1/2 for the
				// integrity/non-integrity split this mirrors.
				if core.IsIntegrity(err) {
					return nil, err
				}
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
			n, err := core.ParseExpr(filters.Expr)
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
		ci, ni, _ := core.ParseTaskID(out[i].ID)
		cj, nj, _ := core.ParseTaskID(out[j].ID)
		if ci != cj {
			return ci < cj
		}
		// Within one project, v1 orders by the numeric alias segment: it is a
		// zero-padded creation counter, so id-asc IS creation order. A v2 alias
		// is a content hash, and a v2 project routinely holds BOTH generations
		// (numeric aliases carried over by the upgrade plus hash aliases born
		// after cutover), so id-asc there is meaningless noise -- it interleaves
		// them by hex luck. The projector stamps Task.Ordinal with the fold's
		// creation ordinal (TasksByCreation, i.e. the HLC creation stamp), which
		// the spec names as the true creation order; use it.
		if formats[ci] == eventlog.StoreFormatV2 {
			if out[i].Ordinal != out[j].Ordinal {
				return out[i].Ordinal < out[j].Ordinal
			}
			return out[i].ID < out[j].ID
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
	for _, w := range core.WildcardTokens(filters.Labels) {
		base := strings.TrimSuffix(w, ":*")
		if l, err := s.LabelShow(base); err == nil && l.Expr != "" {
			return nil, nil, fmt.Errorf("%w: %s", ErrBoardNotAFacet, base)
		}
	}
	inScope, err := s.ListTasksErr(filters)
	if err != nil {
		return nil, nil, err
	}
	wildcards := core.WildcardTokens(filters.Labels)
	if len(wildcards) == 0 {
		return nil, inScope, nil
	}
	groups, others := core.GroupByWildcard(inScope, taskLabels, wildcards)
	out := make([]LabelGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, LabelGroup{Label: g.Label, Tasks: g.Items})
	}
	return out, others, nil
}

// taskLabels is the core grouping accessor for tasks. It is all core needs to
// know about a Task — the type itself stays here until ATM-b9d83a.
func taskLabels(t *Task) []string { return t.Labels }

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
