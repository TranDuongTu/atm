package tui

import (
	"sort"

	"atm/internal/core"
	"atm/internal/store"
)

type taskGroup struct {
	label string
	rows  []taskRow
	// subgroups holds nested facets for multi-wildcard filters (depth =
	// number of wildcards). Empty for the single-wildcard flat path and for
	// the deepest level. A task appears in every sub-group whose key it
	// carries (multi-membership preserved). Tasks in this group that match
	// no deeper wildcard land in a sub-`(no matching labels)` bucket (label
	// == "").
	subgroups []taskGroup
	// collapsed controls group-header expand/collapse.
	collapsed bool
}

// parseFilter splits the filter string on spaces; tokens ending `:*` are
// wildcards (facets), others are exact restrictors.
func (t *tasksModel) parseFilter() []string { return core.ParseFilter(t.filter) }

func (t *tasksModel) hasWildcard() bool {
	for _, tok := range t.parseFilter() {
		if core.IsWildcard(tok) {
			return true
		}
	}
	return false
}

// grouped reports whether the list should render as grouped facets (vs a flat
// row list). Only a real-namespace present focus (or a legacy focusOff wildcard
// filter) groups; absent/unlabeled/bare-tag focuses are always flat, even
// though their filter may still carry a wildcard token.
func (t *tasksModel) grouped() bool {
	switch t.focus.mode {
	case focusPresent:
		return !t.focus.bareTags
	case focusOff:
		return t.hasWildcard()
	default:
		return false
	}
}

// buildNestedGroups buckets `tasks` by the concrete labels they carry that
// match the given wildcards, recursing for each remaining wildcard. This is
// the TUI-side nesting pass that turns the store's flat per-concrete-label
// groups into the nested facet tree (mockup Screen 7, two-wildcard case).
//
// Multi-membership: a task appears in every sub-group whose key it carries.
// Tasks matching no label for the current wildcard land in a sub-
// `(no matching labels)` bucket (label == ""), consistent with the top-level
// pattern. At the deepest level (no remaining wildcards), the caller already
// holds the leaf rows; this helper only recurses while wildcards remain.
func buildNestedGroups(tasks []*store.Task, wildcards []string, toRow func(*store.Task) taskRow) []taskGroup {
	if len(wildcards) == 0 {
		return nil
	}
	w := wildcards[0]
	// Bucket tasks by each concrete label they carry matching w, preserving
	// discovery order then alphabetical (store.GroupTasks already sorts;
	// we sort here for determinism independent of input order).
	buckets := map[string][]*store.Task{}
	var keys []string
	matched := map[*store.Task]bool{}
	for _, t := range tasks {
		for _, l := range t.Labels {
			if core.LabelMatchesWildcard(l, w) {
				if _, exists := buckets[l]; !exists {
					keys = append(keys, l)
				}
				buckets[l] = append(buckets[l], t)
				matched[t] = true
			}
		}
	}
	sort.Strings(keys)
	// (no matching labels) sub-bucket: tasks matching no label for w.
	var noneMatched []*store.Task
	for _, t := range tasks {
		if !matched[t] {
			noneMatched = append(noneMatched, t)
		}
	}
	var groups []taskGroup
	for _, k := range keys {
		rows := make([]taskRow, 0, len(buckets[k]))
		for _, tk := range buckets[k] {
			rows = append(rows, toRow(tk))
		}
		g := taskGroup{label: k}
		if len(wildcards) >= 2 {
			g.subgroups = buildNestedGroups(buckets[k], wildcards[1:], toRow)
			// Leaf rows live only at the deepest level.
			g.rows = nil
		} else {
			g.rows = rows
		}
		groups = append(groups, g)
	}
	// Sub-`(no matching labels)` bucket, rendered last within this level.
	if len(noneMatched) > 0 {
		rows := make([]taskRow, 0, len(noneMatched))
		for _, tk := range noneMatched {
			rows = append(rows, toRow(tk))
		}
		g := taskGroup{label: ""} // "" == (no matching labels)
		if len(wildcards) >= 2 {
			g.subgroups = buildNestedGroups(noneMatched, wildcards[1:], toRow)
		} else {
			g.rows = rows
		}
		groups = append(groups, g)
	}
	return groups
}

// groupLineCount returns the logical lines contributed by one group and its
// (possibly nested) sub-groups: 1 for the header, plus its leaf rows or the
// recursive count of expanded sub-groups. A collapsed group contributes only
// its header.
func groupLineCount(g taskGroup) int {
	n := 1 // header
	if g.collapsed {
		return n
	}
	if len(g.subgroups) > 0 {
		for _, sg := range g.subgroups {
			n += groupLineCount(sg)
		}
	} else {
		n += len(g.rows)
	}
	return n
}

// rowInGroup walks one group's flattened lines looking for a leaf row at the
// flattened index `cursor` (relative to `start`). Returns (row, true, _) when
// the cursor sits on a leaf row; (zero, false, next) otherwise, where next is
// the flattened index after this group's contribution.
func rowInGroup(g taskGroup, start, cursor int) (row taskRow, ok bool, next int) {
	idx := start
	if idx == cursor {
		return taskRow{}, false, idx // header, not a row
	}
	idx++ // header
	if g.collapsed {
		return taskRow{}, false, idx
	}
	if len(g.subgroups) > 0 {
		for _, sg := range g.subgroups {
			if r, ok, next := rowInGroup(sg, idx, cursor); ok {
				return r, true, next
			} else {
				idx = next
			}
		}
	} else {
		if cursor >= idx && cursor < idx+len(g.rows) {
			return g.rows[cursor-idx], true, idx + len(g.rows)
		}
		idx += len(g.rows)
	}
	return taskRow{}, false, idx
}

// toggleInGroup walks one group (and its nested sub-groups) looking for the
// header at the flattened index `cursor` (relative to `start`). If found it
// toggles collapse and returns (true, _). Otherwise it returns (false, nextIdx)
// where nextIdx is the flattened index after this group's contribution.
func toggleInGroup(g *taskGroup, start, cursor int) (done bool, next int) {
	idx := start
	if idx == cursor {
		g.collapsed = !g.collapsed
		return true, idx
	}
	idx++ // header
	if g.collapsed {
		return false, idx
	}
	if len(g.subgroups) > 0 {
		for i := range g.subgroups {
			if done, next := toggleInGroup(&g.subgroups[i], idx, cursor); done {
				return true, next
			} else {
				idx = next
			}
		}
	} else {
		idx += len(g.rows)
	}
	return false, idx
}

// groupLeafCount returns the total leaf rows reachable from a group, summing
// across nested sub-groups (expanded or not — collapse hides rows from the
// view but the header count still reflects the true bucket size).
func groupLeafCount(g taskGroup) int {
	if len(g.subgroups) > 0 {
		n := 0
		for _, sg := range g.subgroups {
			n += groupLeafCount(sg)
		}
		return n
	}
	return len(g.rows)
}
