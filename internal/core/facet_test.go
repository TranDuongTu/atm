package core

import (
	"reflect"
	"testing"
)

type item struct {
	name   string
	labels []string
}

func itemLabels(i item) []string { return i.labels }

// names flattens a node's items to their names for terse assertions.
func names(items []item) []string {
	var out []string
	for _, i := range items {
		out = append(out, i.name)
	}
	return out
}

func TestGroupNestedNoWildcardsReturnsNil(t *testing.T) {
	got := GroupNested([]item{{"a", []string{"ATM:status:open"}}}, itemLabels, nil)
	if got != nil {
		t.Errorf("no wildcards must yield nil, got %v", got)
	}
}

func TestGroupNestedSingleWildcardSortsKeysAndBucketsNoneLast(t *testing.T) {
	items := []item{
		{"open1", []string{"ATM:status:open"}},
		{"done1", []string{"ATM:status:done"}},
		{"none1", []string{"ATM:type:bug"}},
	}
	got := GroupNested(items, itemLabels, []string{"ATM:status:*"})
	if len(got) != 3 {
		t.Fatalf("want 3 nodes (done, open, none), got %d: %v", len(got), got)
	}
	if got[0].Label != "ATM:status:done" || got[1].Label != "ATM:status:open" {
		t.Errorf("keys must be sorted, got %q then %q", got[0].Label, got[1].Label)
	}
	if got[2].Label != "" {
		t.Errorf("(no matching labels) bucket must be last, got %q", got[2].Label)
	}
	if want := []string{"none1"}; !reflect.DeepEqual(names(got[2].Items), want) {
		t.Errorf("none bucket = %v, want %v", names(got[2].Items), want)
	}
}

func TestGroupNestedOmitsEmptyNoneBucket(t *testing.T) {
	items := []item{{"open1", []string{"ATM:status:open"}}}
	got := GroupNested(items, itemLabels, []string{"ATM:status:*"})
	if len(got) != 1 {
		t.Fatalf("want 1 node with no none-bucket, got %d", len(got))
	}
}

func TestGroupNestedMultiMembership(t *testing.T) {
	items := []item{{"both", []string{"ATM:status:open", "ATM:status:blocked"}}}
	got := GroupNested(items, itemLabels, []string{"ATM:status:*"})
	if len(got) != 2 {
		t.Fatalf("item carrying two matching labels must appear in both buckets, got %d", len(got))
	}
	for _, n := range got {
		if want := []string{"both"}; !reflect.DeepEqual(names(n.Items), want) {
			t.Errorf("node %q items = %v, want %v", n.Label, names(n.Items), want)
		}
	}
}

func TestGroupNestedRecursesAndAttachesItemsAtLeafOnly(t *testing.T) {
	items := []item{
		{"a", []string{"ATM:status:open", "ATM:type:bug"}},
		{"b", []string{"ATM:status:open", "ATM:type:chore"}},
		{"c", []string{"ATM:status:open"}}, // no type -> nested none-bucket
	}
	got := GroupNested(items, itemLabels, []string{"ATM:status:*", "ATM:type:*"})
	if len(got) != 1 || got[0].Label != "ATM:status:open" {
		t.Fatalf("want one status:open node, got %v", got)
	}
	if got[0].Items != nil {
		t.Errorf("non-leaf node must not carry items, got %v", names(got[0].Items))
	}
	kids := got[0].Children
	if len(kids) != 3 {
		t.Fatalf("want bug, chore, none children; got %d", len(kids))
	}
	if kids[0].Label != "ATM:type:bug" || kids[1].Label != "ATM:type:chore" || kids[2].Label != "" {
		t.Errorf("children = %q, %q, %q", kids[0].Label, kids[1].Label, kids[2].Label)
	}
	if want := []string{"c"}; !reflect.DeepEqual(names(kids[2].Items), want) {
		t.Errorf("nested none bucket = %v, want %v", names(kids[2].Items), want)
	}
}

func TestGroupNestedEmptyInput(t *testing.T) {
	if got := GroupNested(nil, itemLabels, []string{"ATM:status:*"}); got != nil {
		t.Errorf("empty input must yield nil, got %v", got)
	}
}

func TestGroupByWildcardNoWildcardsReturnsAllAsOthers(t *testing.T) {
	items := []item{{"a", []string{"ATM:status:open"}}, {"b", nil}}
	groups, others := GroupByWildcard(items, itemLabels, nil)
	if groups != nil {
		t.Errorf("no wildcards must yield no groups, got %v", groups)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(names(others), want) {
		t.Errorf("others = %v, want %v", names(others), want)
	}
}

func TestGroupByWildcardIsFlatAcrossAllWildcards(t *testing.T) {
	items := []item{{"a", []string{"ATM:status:open", "ATM:type:bug"}}}
	groups, others := GroupByWildcard(items, itemLabels, []string{"ATM:status:*", "ATM:type:*"})
	if len(groups) != 2 {
		t.Fatalf("want 2 flat groups, got %d", len(groups))
	}
	// Sorted keys.
	if groups[0].Label != "ATM:status:open" || groups[1].Label != "ATM:type:bug" {
		t.Errorf("groups = %q, %q", groups[0].Label, groups[1].Label)
	}
	if others != nil {
		t.Errorf("others must be empty, got %v", names(others))
	}
}

func TestGroupByWildcardOthersAreItemsMatchingNoWildcard(t *testing.T) {
	items := []item{
		{"has", []string{"ATM:status:open"}},
		{"hasnt", []string{"ATM:type:bug"}},
		{"bare", nil},
	}
	_, others := GroupByWildcard(items, itemLabels, []string{"ATM:status:*"})
	if want := []string{"hasnt", "bare"}; !reflect.DeepEqual(names(others), want) {
		t.Errorf("others = %v, want %v", names(others), want)
	}
}

// TestGroupByWildcardDedupesOverlappingWildcards covers the fix for the defect
// at store/query.go:174-185, where the item/wildcard/label loop nesting
// appended an item to one bucket once per matching wildcard. ATM:* and
// ATM:status:* both match ATM:status:open; "a" must land in that bucket once.
func TestGroupByWildcardDedupesOverlappingWildcards(t *testing.T) {
	items := []item{{"a", []string{"ATM:status:open"}}}
	groups, _ := GroupByWildcard(items, itemLabels, []string{"ATM:*", "ATM:status:*"})
	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(groups))
	}
	if want := []string{"a"}; !reflect.DeepEqual(names(groups[0].Items), want) {
		t.Errorf("items = %v, want %v", names(groups[0].Items), want)
	}
}

// TestGroupByWildcardDedupesRepeatedToken covers the same fix via a repeated
// filter token rather than two overlapping namespaces.
func TestGroupByWildcardDedupesRepeatedToken(t *testing.T) {
	items := []item{{"a", []string{"ATM:status:open"}}}
	groups, _ := GroupByWildcard(items, itemLabels, []string{"ATM:status:*", "ATM:status:*"})
	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(groups))
	}
	if want := []string{"a"}; !reflect.DeepEqual(names(groups[0].Items), want) {
		t.Errorf("items = %v, want %v", names(groups[0].Items), want)
	}
}

// TestGroupByWildcardKeepsMultiMembership guards the dedup against
// over-reaching: an item carrying two DIFFERENT matching labels still belongs
// to both buckets.
func TestGroupByWildcardKeepsMultiMembership(t *testing.T) {
	items := []item{{"a", []string{"ATM:status:open", "ATM:status:blocked"}}}
	groups, _ := GroupByWildcard(items, itemLabels, []string{"ATM:status:*"})
	if len(groups) != 2 {
		t.Fatalf("want 2 groups, got %d", len(groups))
	}
	for _, g := range groups {
		if want := []string{"a"}; !reflect.DeepEqual(names(g.Items), want) {
			t.Errorf("group %q items = %v, want %v", g.Label, names(g.Items), want)
		}
	}
}
