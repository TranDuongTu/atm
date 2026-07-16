package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// facetCorpusEntry is one task in the shared characterization corpus.
type facetCorpusEntry struct {
	Title  string
	Labels []string
}

// facetCorpus is the shared characterization corpus for the faceting algebra.
// Its twin lives in internal/tui/tasks_grouping_test.go — keep the two in sync.
// Titles, not IDs, identify tasks in the goldens: a v2 task ID is a content
// hash and would make the golden unreadable.
var facetCorpus = []facetCorpusEntry{
	{"open-chore", []string{"ATM:status:open", "ATM:type:chore"}},
	{"open-bug", []string{"ATM:status:open", "ATM:type:bug"}},
	{"done-chore", []string{"ATM:status:done", "ATM:type:chore"}},
	// Multi-membership within one namespace: belongs to two status groups.
	{"multi-status", []string{"ATM:status:open", "ATM:status:blocked"}},
	// No status label: lands in the others / (no matching labels) bucket
	// when faceting by status.
	{"type-only", []string{"ATM:type:bug"}},
	// Bare (unnamespaced) tag.
	{"bare-tag", []string{"ATM:urgent"}},
	{"mixed-bare", []string{"ATM:status:open", "ATM:urgent"}},
	// No labels at all.
	{"unlabeled", nil},
}

// facetCases are the filter label sets exercised against the corpus. Its twin
// lives in internal/tui/tasks_grouping_test.go — keep the two in sync.
var facetCases = [][]string{
	{},                                      // zero wildcards
	{"ATM:status:*"},                        // one wildcard
	{"ATM:status:*", "ATM:type:*"},          // two wildcards
	{"ATM:status:*", "ATM:type:*", "ATM:*"}, // three wildcards
	{"ATM:*", "ATM:status:*"},               // overlapping: pins the duplicate append
	{"ATM:status:*", "ATM:status:*"},        // repeated token
	{"ATM:nosuch:*"},                        // matches nothing
	{"ATM:type:bug", "ATM:status:*"},        // restrictor AND facet
}

// checkGolden compares got against the golden file at path. Set
// ATM_UPDATE_GOLDEN=1 to rewrite it.
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

// dumpFlat renders a flat faceting result deterministically. Duplicate titles
// inside one group are meaningful: they record the duplicate-append defect.
func dumpFlat(groups []LabelGroup, others []*Task) string {
	var b strings.Builder
	for _, g := range groups {
		label := g.Label
		if label == "" {
			label = "(empty)"
		}
		fmt.Fprintf(&b, "  group %s\n", label)
		for _, tk := range g.Tasks {
			fmt.Fprintf(&b, "    %s\n", tk.Title)
		}
	}
	if len(others) > 0 {
		b.WriteString("  others\n")
		for _, tk := range others {
			fmt.Fprintf(&b, "    %s\n", tk.Title)
		}
	}
	if len(groups) == 0 && len(others) == 0 {
		b.WriteString("  (nothing)\n")
	}
	return b.String()
}

// TestGroupTasksCharacterization records today's flat faceting behavior
// bug-for-bug. It is the safety net for the move into internal/core: the
// golden must not change when the algebra moves (ATM-cca7b0 Tasks 3-4). Task 5
// updates it deliberately, when it fixes the duplicate append.
func TestGroupTasksCharacterization(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "characterization", testActor); err != nil {
		t.Fatalf("create project: %v", err)
	}
	for _, e := range facetCorpus {
		if _, err := s.CreateTask("ATM", e.Title, "", e.Labels, testActor); err != nil {
			t.Fatalf("create task %s: %v", e.Title, err)
		}
	}

	var b strings.Builder
	for _, labels := range facetCases {
		fmt.Fprintf(&b, "== filter: [%s]\n", strings.Join(labels, " "))
		groups, others, err := s.GroupTasksErr(QueryFilters{Project: "ATM", Labels: labels})
		if err != nil {
			fmt.Fprintf(&b, "  error: %v\n", err)
			continue
		}
		b.WriteString(dumpFlat(groups, others))
	}
	checkGolden(t, filepath.Join("testdata", "facet_flat.golden"), b.String())
}

// TestGroupTasksCharacterizationIsOrderStable guards the golden against map
// iteration order leaking into the output.
func TestGroupTasksCharacterizationIsOrderStable(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "characterization", testActor); err != nil {
		t.Fatalf("create project: %v", err)
	}
	for _, e := range facetCorpus {
		if _, err := s.CreateTask("ATM", e.Title, "", e.Labels, testActor); err != nil {
			t.Fatalf("create task %s: %v", e.Title, err)
		}
	}
	filters := QueryFilters{Project: "ATM", Labels: []string{"ATM:status:*", "ATM:type:*"}}
	groups, others, err := s.GroupTasksErr(filters)
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	first := dumpFlat(groups, others)
	for i := 0; i < 20; i++ {
		g, o, err := s.GroupTasksErr(filters)
		if err != nil {
			t.Fatalf("group: %v", err)
		}
		if got := dumpFlat(g, o); got != first {
			t.Fatalf("unstable output on run %d:\n%s\nvs\n%s", i, got, first)
		}
	}
	// Group labels must be sorted — the golden depends on it.
	var labels []string
	for _, g := range groups {
		labels = append(labels, g.Label)
	}
	if !sort.StringsAreSorted(labels) {
		t.Errorf("group labels not sorted: %v", labels)
	}
}
