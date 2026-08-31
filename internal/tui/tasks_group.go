package tui

import "sort"

// groupNode is one task in the grouped view's tree: its row plus the rows
// whose ParentOf named it. Built fresh at every refresh, never stored.
type groupNode struct {
	row      taskRow
	children []*groupNode
}

// parentIDOf asks the current capability for the row's parent. "" when no
// registry (no scope), no hook, or no parent.
func (t *tasksModel) parentIDOf(r taskRow) string {
	if t.annReg == nil || r.task == nil {
		return ""
	}
	return t.annReg.ParentOf(t.m.capability.current, *r.task)
}

// syntheticParentRow fetches an out-of-set parent and marks it synthetic —
// a real, selectable row that is not part of the lane's own result set.
// Nil when the id dangles (deleted or foreign): the child stays top-level.
func (t *tasksModel) syntheticParentRow(id string) *groupNode {
	tk, err := t.m.store.GetTask(id)
	if err != nil {
		return nil
	}
	row := t.toRow(tk)
	row.synthetic = true
	return &groupNode{row: row}
}

// applyGrouping arranges rows into parent→child trees and flattens them
// back into one list, stamping depth. Groups and orphans are ordered by the
// active comparator applied to each subtree's BEST row, so a fresh child
// lifts its stale parent; siblings sort by the same comparator. Cycle
// members that no root can reach are appended flat, in input order.
func (t *tasksModel) applyGrouping(rows []taskRow) []taskRow {
	less := t.spec().less
	byID := make(map[string]*groupNode, len(rows))
	order := make([]*groupNode, 0, len(rows))
	for _, r := range rows {
		n := &groupNode{row: r}
		byID[r.id] = n
		order = append(order, n)
	}
	var top []*groupNode
	attached := make(map[*groupNode]bool, len(rows))
	for _, n := range order {
		pid := t.parentIDOf(n.row)
		if pid == "" || pid == n.row.id {
			top = append(top, n)
			continue
		}
		parent, ok := byID[pid]
		if !ok {
			if syn := t.syntheticParentRow(pid); syn != nil {
				byID[pid] = syn
				top = append(top, syn)
				parent, ok = syn, true
			}
		}
		if !ok {
			top = append(top, n)
			continue
		}
		parent.children = append(parent.children, n)
		attached[n] = true
	}
	sortNodes(top, less)
	out := make([]taskRow, 0, len(byID))
	visited := make(map[*groupNode]bool, len(byID))
	var flatten func(n *groupNode, depth int)
	flatten = func(n *groupNode, depth int) {
		if visited[n] {
			return
		}
		visited[n] = true
		n.row.depth = depth
		out = append(out, n.row)
		for _, c := range n.children {
			flatten(c, depth+1)
		}
	}
	for _, n := range top {
		flatten(n, 0)
	}
	// Cycle members: attached to a parent but reachable from no root.
	for _, n := range order {
		if !visited[n] {
			flatten(n, 0)
		}
	}
	return out
}

// sortNodes orders siblings (and, transitively, their subtrees) by the
// comparator applied to each subtree's best row.
func sortNodes(ns []*groupNode, less func(a, b taskRow) bool) {
	for _, n := range ns {
		sortNodes(n.children, less)
	}
	sort.SliceStable(ns, func(i, j int) bool {
		return less(bestRow(ns[i], less), bestRow(ns[j], less))
	})
}

// bestRow is the row in n's subtree that sorts first under less.
func bestRow(n *groupNode, less func(a, b taskRow) bool) taskRow {
	best := n.row
	for _, c := range n.children {
		if cb := bestRow(c, less); less(cb, best) {
			best = cb
		}
	}
	return best
}
