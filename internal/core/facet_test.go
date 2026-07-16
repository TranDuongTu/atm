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
