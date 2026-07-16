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
