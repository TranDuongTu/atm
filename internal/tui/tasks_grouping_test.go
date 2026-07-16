package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/core"
	"atm/internal/store"
)

// facetCorpusEntry is one task in the shared characterization corpus.
type facetCorpusEntry struct {
	Title  string
	Labels []string
}

// facetCorpus mirrors internal/store/query_facet_test.go's corpus — keep the
// two in sync. It cannot be shared: the twin lives in another package, and a
// test-only package to hold it would cost more than the duplication saves.
var facetCorpus = []facetCorpusEntry{
	{"open-chore", []string{"ATM:status:open", "ATM:type:chore"}},
	{"open-bug", []string{"ATM:status:open", "ATM:type:bug"}},
	{"done-chore", []string{"ATM:status:done", "ATM:type:chore"}},
	{"multi-status", []string{"ATM:status:open", "ATM:status:blocked"}},
	{"type-only", []string{"ATM:type:bug"}},
	{"bare-tag", []string{"ATM:urgent"}},
	{"mixed-bare", []string{"ATM:status:open", "ATM:urgent"}},
	{"unlabeled", nil},
}

// facetCases mirrors internal/store/query_facet_test.go's cases — keep in sync.
var facetCases = [][]string{
	{},
	{"ATM:status:*"},
	{"ATM:status:*", "ATM:type:*"},
	{"ATM:status:*", "ATM:type:*", "ATM:*"},
	{"ATM:*", "ATM:status:*"},
	{"ATM:status:*", "ATM:status:*"},
	{"ATM:nosuch:*"},
	{"ATM:type:bug", "ATM:status:*"},
}

func checkGolden(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("ATM_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with ATM_UPDATE_GOLDEN=1 to create)", path, err)
	}
	if got != string(want) {
		t.Errorf("golden %s mismatch\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// newFacetStore builds a real store holding the corpus. It mirrors
// newTestStore in package store, which is unexported and so cannot be reused
// here; store.Open + Init is exactly what that helper does, and four TUI tests
// already stand up stores this way (app_test.go:35, labels_test.go:469).
func newFacetStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := s.CreateProject("ATM", "characterization", testActor); err != nil {
		t.Fatalf("create project: %v", err)
	}
	for _, e := range facetCorpus {
		if _, err := s.CreateTask("ATM", e.Title, "", e.Labels, testActor); err != nil {
			t.Fatalf("create task %s: %v", e.Title, err)
		}
	}
	return s
}

// dumpTree renders a taskGroup tree deterministically.
func dumpTree(groups []taskGroup, indent string) string {
	var b strings.Builder
	for _, g := range groups {
		label := g.label
		if label == "" {
			label = "(no matching labels)"
		}
		fmt.Fprintf(&b, "%sgroup %s\n", indent, label)
		for _, r := range g.rows {
			fmt.Fprintf(&b, "%s  %s\n", indent, r.title)
		}
		b.WriteString(dumpTree(g.subgroups, indent+"  "))
	}
	return b.String()
}

// TestFacetTreeCharacterization records the TUI group tree as rendered,
// reproducing the COMPOSITION at tasks.go's focusPresent case — core.GroupNested
// builds the whole tree from wildcards[0], and the others bucket comes
// alongside it. The tasks are drawn from the real store, so the query half of
// the seam stays covered.
//
// The golden must not change when the algebra moves into internal/core
// (ATM-cca7b0 Tasks 3-4). Task 5 updates it (dedup) and Task 6 updates it
// again (tree shape); both are deliberate.
func TestFacetTreeCharacterization(t *testing.T) {
	s := newFacetStore(t)
	var b strings.Builder
	for _, filters := range facetCases {
		fmt.Fprintf(&b, "== filter: [%s]\n", strings.Join(filters, " "))
		tasks := s.ListTasks(store.QueryFilters{Project: "ATM", Labels: filters})
		wildcards := core.WildcardTokens(filters)
		groups := nodesToGroups(dropUnmatchedTop(core.GroupNested(tasks, taskLabels, wildcards)), toRowTest)
		_, others := core.GroupByWildcard(tasks, taskLabels, wildcards)
		if len(groups) == 0 {
			b.WriteString("  (no groups)\n")
		}
		b.WriteString(dumpTree(groups, "  "))
		if len(others) > 0 {
			b.WriteString("  others\n")
			for _, tk := range others {
				fmt.Fprintf(&b, "    %s\n", tk.Title)
			}
		}
	}
	checkGolden(t, filepath.Join("testdata", "facet_tree.golden"), b.String())
}
