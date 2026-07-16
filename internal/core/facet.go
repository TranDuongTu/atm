package core

import "sort"

// Node is one level of a nested facet tree. Label is the concrete label that
// keys the bucket; the empty string is the "(no matching labels)" bucket,
// which a renderer names for itself. Items are attached at the deepest level
// only — an interior node carries Children instead.
type Node[T any] struct {
	Label    string
	Items    []T
	Children []Node[T]
}

// GroupNested buckets items by the concrete labels they carry matching
// wildcards[0], then recurses into each bucket with wildcards[1:]. Keys are
// sorted; the "(no matching labels)" bucket is emitted last and only when
// non-empty. An item carrying several matching labels appears in every bucket
// it keys (multi-membership). With no wildcards the result is nil.
func GroupNested[T any](items []T, labelsOf func(T) []string, wildcards []string) []Node[T] {
	if len(wildcards) == 0 {
		return nil
	}
	w := wildcards[0]
	buckets := map[string][]T{}
	var keys []string
	matched := make([]bool, len(items))
	for i, it := range items {
		for _, l := range labelsOf(it) {
			if !LabelMatchesWildcard(l, w) {
				continue
			}
			if _, exists := buckets[l]; !exists {
				keys = append(keys, l)
			}
			buckets[l] = append(buckets[l], it)
			matched[i] = true
		}
	}
	sort.Strings(keys)

	var none []T
	for i, it := range items {
		if !matched[i] {
			none = append(none, it)
		}
	}

	var out []Node[T]
	for _, k := range keys {
		out = append(out, newNode(k, buckets[k], labelsOf, wildcards))
	}
	if len(none) > 0 {
		out = append(out, newNode("", none, labelsOf, wildcards))
	}
	return out
}

// Group is one flat facet bucket: every item carrying Label.
type Group[T any] struct {
	Label string
	Items []T
}

// GroupByWildcard buckets items under every concrete label they carry that
// matches ANY of wildcards — one flat level, keys sorted. Items carrying no
// matching label are returned in others. With no wildcards there are no groups
// and every item is an "other".
//
// An item appears at most once per bucket, however many wildcards match that
// label. It still appears in every bucket it keys: an item carrying two
// different matching labels belongs to both (multi-membership).
func GroupByWildcard[T any](items []T, labelsOf func(T) []string, wildcards []string) (groups []Group[T], others []T) {
	if len(wildcards) == 0 {
		return nil, items
	}
	buckets := map[string][]T{}
	var order []string
	for _, it := range items {
		// Labels outer, wildcards inner (inside labelMatchesAny): each
		// (item, label) pair is considered exactly once, so a label matched by
		// several wildcards buckets the item once rather than once per wildcard.
		for _, l := range labelsOf(it) {
			if !labelMatchesAny(l, wildcards) {
				continue
			}
			if _, exists := buckets[l]; !exists {
				order = append(order, l)
			}
			buckets[l] = append(buckets[l], it)
		}
	}
	sort.Strings(order)
	for _, l := range order {
		groups = append(groups, Group[T]{Label: l, Items: buckets[l]})
	}
	for _, it := range items {
		if !matchesAny(labelsOf(it), wildcards) {
			others = append(others, it)
		}
	}
	return groups, others
}

// labelMatchesAny reports whether label falls under any of wildcards.
func labelMatchesAny(label string, wildcards []string) bool {
	for _, w := range wildcards {
		if LabelMatchesWildcard(label, w) {
			return true
		}
	}
	return false
}

// matchesAny reports whether any of labels falls under any of wildcards.
func matchesAny(labels []string, wildcards []string) bool {
	for _, l := range labels {
		if labelMatchesAny(l, wildcards) {
			return true
		}
	}
	return false
}

// newNode builds one node: an interior node recurses on the remaining
// wildcards, a leaf carries the items.
func newNode[T any](label string, items []T, labelsOf func(T) []string, wildcards []string) Node[T] {
	n := Node[T]{Label: label}
	if len(wildcards) >= 2 {
		n.Children = GroupNested(items, labelsOf, wildcards[1:])
	} else {
		n.Items = items
	}
	return n
}
