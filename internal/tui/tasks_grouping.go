package tui

import (
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

// taskLabels is the core.GroupNested accessor for store tasks. It is the only
// thing core needs to know about a Task — the type itself stays in store
// until ATM-b9d83a.
func taskLabels(t *store.Task) []string { return t.Labels }

// splitUnmatchedTop separates the top-level `(no matching labels)` bucket from a
// core.GroupNested tree, returning the remaining nodes and that bucket's tasks:
// every task in `tasks` carrying no label that matches wildcards[0].
//
// That set STRICTLY CONTAINS the store's others (tasks matching no wildcard at
// all) whenever the filter carries two or more wildcards, so the store's others
// cannot stand in for it — dropping the node while rendering the store's others
// makes the difference vanish from the UI entirely. With a single wildcard the
// two coincide. The TUI renders this bucket under its own policy: hidden under
// a present focus, flat under focusOff (tasks_list.go's focusOff bucket).
// Nested `(no matching labels)` buckets are kept: nothing else represents them.
//
// The bucket is recomputed from `tasks` rather than read off the node: with two
// or more wildcards GroupNested has already split it into children, which
// re-bucket multi-label tasks and reorder them. The predicate here is
// GroupNested's own, so the two agree by construction and input order survives.
//
// GroupNested emits the bucket last and only when non-empty, so this splits off
// at most the final node.
func splitUnmatchedTop(nodes []core.Node[*store.Task], tasks []*store.Task, wildcards []string) ([]core.Node[*store.Task], []*store.Task) {
	n := len(nodes)
	if n == 0 {
		// No wildcards to facet by, so nothing matches one: every task is
		// unmatched. (GroupNested returns no nodes only when wildcards is
		// empty, or when there is nothing to group at all.)
		return nodes, tasks
	}
	if nodes[n-1].Label != "" {
		return nodes, nil
	}
	var unmatched []*store.Task
	for _, tk := range tasks {
		if !anyLabelMatches(tk.Labels, wildcards[0]) {
			unmatched = append(unmatched, tk)
		}
	}
	return nodes[:n-1], unmatched
}

// anyLabelMatches reports whether any label matches the wildcard.
func anyLabelMatches(labels []string, wildcard string) bool {
	for _, l := range labels {
		if core.LabelMatchesWildcard(l, wildcard) {
			return true
		}
	}
	return false
}

// nodesToGroups adapts core's rendering-agnostic facet tree into the TUI's
// taskGroup, attaching rows via toRow. Leaf rows live only at the deepest
// level, mirroring core.GroupNested's Items placement; collapsed defaults to
// false (expanded).
func nodesToGroups(nodes []core.Node[*store.Task], toRow func(*store.Task) taskRow) []taskGroup {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]taskGroup, 0, len(nodes))
	for _, n := range nodes {
		g := taskGroup{label: n.Label}
		if len(n.Children) > 0 {
			g.subgroups = nodesToGroups(n.Children, toRow)
		} else {
			rows := make([]taskRow, 0, len(n.Items))
			for _, tk := range n.Items {
				rows = append(rows, toRow(tk))
			}
			g.rows = rows
		}
		out = append(out, g)
	}
	return out
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
