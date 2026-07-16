# internal/core Faceting Unification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create `internal/core` as a pure domain leaf owning the faceting/wildcard algebra, delete the duplicate copies in `store` and `tui`, and fix the two defects the move exposes.

**Architecture:** `internal/core` is a leaf package importing nothing internal. Its grouping functions are generic over a `labelsOf func(T) []string` accessor, so `core` never names `Task` and the type move stays step 4's scope (ATM-b9d83a). Characterization goldens pin today's behavior first; the move is proven neutral by leaving those goldens untouched; the fixes land last with their golden updates visible in the diff.

**Tech Stack:** Go 1.25 (module `atm`, `go.work` also uses `./libs/eventsource`), standard library only in `core`. Tests are stdlib `testing`.

**Specification:** `docs/superpowers/specs/2026-07-16-internal-core-faceting-design.md` — read it before Task 1.

**Ledger:** ATM-cca7b0. Record progress with `atm task comment add --task ATM-cca7b0 --actor 'developer@claude:opus-4.8' --label ATM:comment:progress --body '...'`.

## Global Constraints

- **`internal/core` imports nothing internal.** Verify with:
  `go list -deps ./internal/core | grep '^atm/' | grep -v '^atm/internal/core$' || echo "LEAF OK"`
  Note the second grep: `go list -deps` lists the package ITSELF among its output, so a bare
  `grep atm/` always matches `atm/internal/core` and reports a false violation. Exclude it.
  Standard library only.
- **`store.Task` does not move.** No package outside `store` gains a dependency on a core domain type. That is step 4 (ATM-b9d83a).
- **`GroupTasksErr` keeps its signature:** `func (s *Store) GroupTasksErr(filters QueryFilters) ([]LabelGroup, []*Task, error)`. `internal/cli` must not change.
- **Only Tasks 5 and 6 change behavior.** Tasks 1-4 preserve today's observable behavior exactly, including the duplicate-append bug. Task 5 carries the dedup fix; Task 6 carries the TUI tree fix. Nothing else may change behavior.
- **Goldens are untouched by Tasks 3-4.** If a golden changes there, that is a defect, not a rebaseline. Tasks 5 and 6 update them deliberately, and each names exactly which lines may move.
- **Work happens on branch `worktree-atm-cca7b0-core-faceting`, never `main`.**
- Run `make verify` (build + test + scripts-test) before the final task's commit. Per-task, `go test ./internal/...` is sufficient.
- Actor string for all ATM mutations: `developer@claude:opus-4.8`.

## File Structure

| File | Responsibility |
|---|---|
| `internal/core/label.go` | Create. Label-string algebra: wildcard predicates, token partitioning, facet token, bare-tag predicate. |
| `internal/core/filter.go` | Create. The filter string as a label query: parse, has/add/remove token. |
| `internal/core/facet.go` | Create. `Group[T]`/`Node[T]` plus `GroupByWildcard` (flat) and `GroupNested` (recursive). |
| `internal/core/label_test.go`, `filter_test.go`, `facet_test.go` | Create. Direct table-driven unit tests. |
| `internal/store/query.go` | Modify. Delete four helpers; delegate bucketing to `core`. |
| `internal/store/query_facet_test.go` | Create. Characterization of flat faceting → golden. |
| `internal/store/testdata/facet_flat.golden` | Create. |
| `internal/tui/tasks_grouping.go` | Modify. Lose the algebra; keep viewport state; gain the `core.Node` → `taskGroup` adapter. |
| `internal/tui/tasks.go` | Modify. Call sites. |
| `internal/tui/tasks_test.go` | Modify. Four tests whose subjects move to `core`. |
| `internal/tui/tasks_grouping_test.go` | Create. Characterization of the composed tree → golden. |
| `internal/tui/testdata/facet_tree.golden` | Create. |

## Task Map

| Task | Deliverable | Goldens |
|---|---|---|
| 1 | Store characterization golden | created |
| 2 | TUI characterization golden (composed path, real store) | created |
| 3 | `core` label + filter algebra; both packages point at it | untouched |
| 4 | `core.GroupNested`; TUI's `buildNestedGroups` deleted | untouched |
| 5 | `core.GroupByWildcard` **+ dedup fix**; store's bucketing deleted | **both updated** (dedup only) |
| 6 | TUI tree nests from `wildcards[0]` | **tree updated** (shape only) |

Tasks 3-5 refine the spec's "move" phase into three reviewable commits; Tasks 1-2 refine its "pin" phase into two.

Tasks 3 and 4 are provably neutral — their goldens must not move. Task 5 is not: it carries the dedup fix, so its golden changes. That is deliberate (see the spec's Behavior policy: shipping a knowingly-defective function, and a test asserting defective output, was judged worse than losing a commit-level neutrality proof). Task 5 recovers the attribution with an intermediate checkpoint — port faithfully, confirm the goldens hold, *then* fix.

---

### Task 1: Store characterization golden

**Files:**
- Create: `internal/store/query_facet_test.go`
- Create: `internal/store/testdata/facet_flat.golden`

**Interfaces:**
- Consumes: `newTestStore(t *testing.T) *Store` and `const testActor = "admin@cli:test"` from `internal/store/project_test.go`. `s.CreateProject(code, desc, actor)`, `s.CreateTask(code, title, desc string, labels []string, actor string)`, `s.GroupTasksErr(QueryFilters) ([]LabelGroup, []*Task, error)`.
- Produces: `testdata/facet_flat.golden`, the byte-for-byte record of today's flat faceting. Tasks 3-4 must leave it unchanged; Task 5 updates it when it applies the dedup fix.

**Context the implementer needs:** `store.CreateTask` does not validate that labels exist, so the corpus can use label names freely. Task IDs under the v2 format are content hashes — **never put an ID in the golden**, use titles, which are stable and readable. The corpus deliberately includes `ATM:*` + `ATM:status:*` together: that pins the duplicate-append bug at `query.go:174-185`, and the golden is expected to show a task listed twice in one group. That is not a mistake in the test; it is the defect being recorded.

- [ ] **Step 1: Write the characterization test**

Create `internal/store/query_facet_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to watch it fail on the missing golden**

Run: `go test ./internal/store/ -run TestGroupTasksCharacterization -v 2>&1 | tail -20`

Expected: FAIL — `read golden testdata/facet_flat.golden: ... no such file or directory (run with ATM_UPDATE_GOLDEN=1 to create)`. The order-stability test should PASS already.

- [ ] **Step 3: Generate the golden**

Run: `ATM_UPDATE_GOLDEN=1 go test ./internal/store/ -run TestGroupTasksCharacterization`

- [ ] **Step 4: Read the golden and confirm it records the defect**

Run: `cat internal/store/testdata/facet_flat.golden`

**This is a required review step, not a formality.** Confirm by eye:
- Under `== filter: [ATM:* ATM:status:*]`, at least one group lists the same title **twice** — that is the duplicate append being pinned. If no title repeats, the bug is not being characterized; stop and re-check the corpus.
- Under `== filter: []`, every task appears under `others` and there are no groups.
- Under `== filter: [ATM:status:*]`, `multi-status` appears in **both** `ATM:status:blocked` and `ATM:status:open`.
- `type-only`, `bare-tag`, `unlabeled` appear in `others`.

- [ ] **Step 5: Verify the test now passes against the golden**

Run: `go test ./internal/store/ -run TestGroupTasksCharacterization -v 2>&1 | tail -5`
Expected: PASS (both tests).

- [ ] **Step 6: Commit**

```bash
git add internal/store/query_facet_test.go internal/store/testdata/facet_flat.golden
git commit -m "test(ATM-cca7b0): characterize store flat faceting before the core move

Pins today's GroupTasksErr behavior bug-for-bug against a shared corpus,
including the duplicate append at query.go:174-185 that fires when two
wildcards match one label (ATM:* plus ATM:status:*). The golden is the
safety net for moving the algebra into internal/core: it must not change
when the seam moves.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: TUI characterization golden (the composed path)

**Files:**
- Create: `internal/tui/tasks_grouping_test.go`
- Create: `internal/tui/testdata/facet_tree.golden`

**Interfaces:**
- Consumes: `toRowTest(tk *store.Task) taskRow` and `const testActor = "developer@claude:test"` from `internal/tui/tasks_test.go` / `app_test.go:21`; `buildNestedGroups(tasks []*store.Task, wildcards []string, toRow func(*store.Task) taskRow) []taskGroup`; `wildcardTokens(labels []string) []string`; and from `internal/store`: `Open(root string, opts ...Option) (*Store, error)`, `(*Store).Init(storePath string) error`, `(*Store).CreateProject`, `(*Store).CreateTask`, `(*Store).GroupTasks`.
- Produces: `testdata/facet_tree.golden`. Tasks 3-4 must leave it unchanged; Task 5 updates it (dedup) and Task 6 updates it again (tree shape).

**Context the implementer needs:** This test must reproduce **the composition**, not `buildNestedGroups` alone. `internal/tui/tasks.go:142` takes the top level from the real `store.GroupTasks` (flat, any-wildcard) and passes only `wildcards[1:]` to `buildNestedGroups`. The divergence between the two algorithms lives in that seam, so a test of `buildNestedGroups` in isolation would miss exactly what we need pinned.

**Drive a real store.** Call `store.GroupTasks` for level 1 — do not transcribe store's bucketing into a local helper. A copy would duplicate a logic block and, worse, could drift from the thing it claims to characterize. Four TUI tests already build real stores this way (`app_test.go:35`, `labels_test.go:469`): `store.Open(t.TempDir())` then `s.Init("")`. `newTestStore` is unexported and lives in package `store`, so it cannot be reused here; `store.Open` + `Init` is exactly what it does.

Reproduce only the **wiring** of `tasks.go:142-156` locally, not a whole Bubble Tea model: the model needs a live `*Model` for `store.Now()`/`relTime`, which is why `toRowTest` exists.

Do **not** import `internal/store`'s test corpus — it is in another package and unexported. Copy the literal; the twin comment on each keeps them honest.

- [ ] **Step 1: Write the characterization test**

Create `internal/tui/tasks_grouping_test.go`:

```go
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// TestFacetTreeCharacterization records today's TUI group tree as rendered,
// reproducing the COMPOSITION at tasks.go:142 — the real store.GroupTasks
// supplies a flat level 1, buildNestedGroups handles wildcards[1:]. The
// divergence between the flat and nested algorithms lives in that seam, so
// characterizing buildNestedGroups alone would miss it.
//
// The golden must not change when the algebra moves into internal/core
// (ATM-cca7b0 Tasks 3-4). Task 5 updates it (dedup) and Task 6 updates it
// again (tree shape); both are deliberate.
func TestFacetTreeCharacterization(t *testing.T) {
	s := newFacetStore(t)
	var b strings.Builder
	for _, filters := range facetCases {
		fmt.Fprintf(&b, "== filter: [%s]\n", strings.Join(filters, " "))
		wildcards := wildcardTokens(filters)
		// Mirror tasks.go:142-156 (focusPresent) against the real store.
		flat, others := s.GroupTasks(store.QueryFilters{Project: "ATM", Labels: filters})
		var groups []taskGroup
		for _, g := range flat {
			rows := make([]taskRow, 0, len(g.Tasks))
			for _, tk := range g.Tasks {
				rows = append(rows, toRowTest(tk))
			}
			tg := taskGroup{label: g.Label, rows: rows}
			if len(wildcards) >= 2 {
				tg.subgroups = buildNestedGroups(g.Tasks, wildcards[1:], toRowTest)
				tg.rows = nil
			}
			groups = append(groups, tg)
		}
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
```

**Import note:** this file needs `fmt`, `os`, `path/filepath`, `strings`, `testing`, and `atm/internal/store`. It does **not** need `mkTask` — the corpus goes through the real store now.

- [ ] **Step 2: Run the test to watch it fail on the missing golden**

Run: `go test ./internal/tui/ -run TestFacetTreeCharacterization -v 2>&1 | tail -20`
Expected: FAIL — `read golden testdata/facet_tree.golden: ... no such file or directory`.

If instead it fails to **compile** with `checkGolden redeclared`, the helper collides with an existing one in package `tui` — rename this one `checkFacetGolden` here and in its call site, and continue.

- [ ] **Step 3: Generate the golden**

Run: `ATM_UPDATE_GOLDEN=1 go test ./internal/tui/ -run TestFacetTreeCharacterization`

- [ ] **Step 4: Read the golden and confirm it records the divergence**

Run: `cat internal/tui/testdata/facet_tree.golden`

**Required review.** Confirm by eye under `== filter: [ATM:status:* ATM:type:*]`:
- Top-level groups include **both** `ATM:status:...` **and** `ATM:type:...` entries. A correct nested tree would show only `status` at the top. This is the composition defect being pinned — it is expected here.
- Under a top-level `ATM:type:chore` group there are `ATM:type:*` **subgroups** — i.e. type nested under type.

If the top level shows only `status` groups, the composition is not being reproduced; stop and check that level 1 really comes from `s.GroupTasks` and that only `wildcards[1:]` reaches `buildNestedGroups`.

- [ ] **Step 5: Verify the test passes**

Run: `go test ./internal/tui/ -run TestFacetTreeCharacterization -v 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/tasks_grouping_test.go internal/tui/testdata/facet_tree.golden
git commit -m "test(ATM-cca7b0): characterize the composed TUI facet tree

Pins the tree the TUI actually renders, reproducing the composition at
tasks.go:142 where store's flat grouping supplies level 1 and
buildNestedGroups handles only wildcards[1:]. The golden records the
resulting divergence (top level faceted by every namespace at once, type
nested under type) so the move into internal/core can be proven neutral
against it.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: `core` label and filter algebra

**Files:**
- Create: `internal/core/label.go`, `internal/core/filter.go`
- Create: `internal/core/label_test.go`, `internal/core/filter_test.go`
- Modify: `internal/store/query.go` (delete `isWildcard`, `labelMatchesWildcard`, `restrictingTokens`, `wildcardTokens`)
- Modify: `internal/tui/tasks_grouping.go` (delete `isWildcardTUI`, `labelMatchesWildcardTUI`, `wildcardTokens`, `facetToken`, `filterHasToken`, `filterAddToken`, `filterRemoveToken`, `taskHasBareTag`; `parseFilter` delegates)
- Modify: `internal/tui/tasks.go` (call sites), `internal/tui/labels.go` (call sites)
- Modify: `internal/tui/tasks_test.go` (`TestFilterTokenHelpers`, `TestTaskHasBareTag` move to `core`)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces, in package `core` (import path `atm/internal/core`):
  ```go
  func IsWildcard(label string) bool
  func LabelMatchesWildcard(label, wildcard string) bool
  func WildcardTokens(labels []string) []string
  func RestrictingTokens(labels []string) []string
  func FacetToken(scope, ns string) string
  func HasBareTag(scope string, labels []string) bool
  func ParseFilter(s string) []string
  func FilterHasToken(filter, token string) bool
  func FilterAddToken(filter, token string) string
  func FilterRemoveToken(filter, token string) string
  ```

**Context the implementer needs:** `taskHasBareTag(scope string, t *store.Task)` becomes `core.HasBareTag(scope string, labels []string)` — it takes labels, because `core` may not know `store.Task`. `parseFilter` stays a method on `*tasksModel` (it reads `t.filter`) but its body becomes a single `core.ParseFilter` call. `store.GroupTasksErr` still calls `wildcardTokens` twice and `ListTasksErr` calls `restrictingTokens` — those become `core.` calls now; the bucketing body is Task 5's job.

- [ ] **Step 1: Write `core`'s failing tests**

Create `internal/core/label_test.go`:

```go
package core

import (
	"reflect"
	"testing"
)

func TestIsWildcard(t *testing.T) {
	for _, tc := range []struct {
		label string
		want  bool
	}{
		{"ATM:status:*", true},
		{"ATM:*", true},
		{"ATM:status:open", false},
		{"ATM:urgent", false},
		{"", false},
		{"*", false},        // no ":" prefix — not a wildcard token
		{"ATM:status:", false},
	} {
		if got := IsWildcard(tc.label); got != tc.want {
			t.Errorf("IsWildcard(%q) = %v, want %v", tc.label, got, tc.want)
		}
	}
}

func TestLabelMatchesWildcard(t *testing.T) {
	for _, tc := range []struct {
		label, wildcard string
		want            bool
	}{
		{"ATM:status:open", "ATM:status:*", true},
		{"ATM:status:open", "ATM:*", true},
		{"ATM:type:bug", "ATM:status:*", false},
		{"OTHER:status:open", "ATM:status:*", false},
		// Prefix semantics: the namespace segment is not required to end at
		// a colon boundary. Documents today's behavior.
		{"ATM:statuses:open", "ATM:status:*", false},
		{"ATM:status:open:sub", "ATM:status:*", true},
	} {
		if got := LabelMatchesWildcard(tc.label, tc.wildcard); got != tc.want {
			t.Errorf("LabelMatchesWildcard(%q, %q) = %v, want %v", tc.label, tc.wildcard, got, tc.want)
		}
	}
}

func TestWildcardAndRestrictingTokensPartition(t *testing.T) {
	labels := []string{"ATM:status:*", "ATM:type:bug", "ATM:*", "ATM:urgent"}
	if got, want := WildcardTokens(labels), []string{"ATM:status:*", "ATM:*"}; !reflect.DeepEqual(got, want) {
		t.Errorf("WildcardTokens = %v, want %v", got, want)
	}
	if got, want := RestrictingTokens(labels), []string{"ATM:type:bug", "ATM:urgent"}; !reflect.DeepEqual(got, want) {
		t.Errorf("RestrictingTokens = %v, want %v", got, want)
	}
	if WildcardTokens(nil) != nil || RestrictingTokens(nil) != nil {
		t.Error("nil input must yield nil output")
	}
}

func TestFacetToken(t *testing.T) {
	if got := FacetToken("ATM", "status"); got != "ATM:status:*" {
		t.Errorf("FacetToken = %q, want ATM:status:*", got)
	}
}

func TestHasBareTag(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels []string
		want   bool
	}{
		{"namespaced only", []string{"ATM:status:open"}, false},
		{"bare", []string{"ATM:urgent"}, true},
		{"none", nil, false},
		{"mixed", []string{"ATM:status:open", "ATM:urgent"}, true},
		{"foreign scope", []string{"OTHER:urgent"}, false},
	} {
		if got := HasBareTag("ATM", tc.labels); got != tc.want {
			t.Errorf("%s: HasBareTag(ATM, %v) = %v, want %v", tc.name, tc.labels, got, tc.want)
		}
	}
}
```

Create `internal/core/filter_test.go`:

```go
package core

import (
	"reflect"
	"testing"
)

func TestParseFilter(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"ATM:status:*", []string{"ATM:status:*"}},
		{"ATM:status:* ATM:type:*", []string{"ATM:status:*", "ATM:type:*"}},
		{"  ATM:status:*   ATM:type:*  ", []string{"ATM:status:*", "ATM:type:*"}},
	} {
		if got := ParseFilter(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseFilter(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestFilterTokenOps(t *testing.T) {
	if !FilterHasToken("ATM:status:* ATM:type:*", "ATM:type:*") {
		t.Error("present token must be found")
	}
	if FilterHasToken("ATM:status:*", "ATM:type:*") {
		t.Error("absent token must not be found")
	}
	if got := FilterAddToken("ATM:status:*", "ATM:type:*"); got != "ATM:status:* ATM:type:*" {
		t.Errorf("FilterAddToken = %q", got)
	}
	if got := FilterAddToken("ATM:status:*", "ATM:status:*"); got != "ATM:status:*" {
		t.Errorf("FilterAddToken must dedupe, got %q", got)
	}
	if got := FilterAddToken("", "ATM:status:*"); got != "ATM:status:*" {
		t.Errorf("FilterAddToken on empty = %q", got)
	}
	if got := FilterRemoveToken("ATM:status:* ATM:type:*", "ATM:status:*"); got != "ATM:type:*" {
		t.Errorf("FilterRemoveToken = %q", got)
	}
	if got := FilterRemoveToken("ATM:status:*", "ATM:status:*"); got != "" {
		t.Errorf("FilterRemoveToken to empty = %q", got)
	}
}
```

- [ ] **Step 2: Run to verify the tests fail**

Run: `go test ./internal/core/ 2>&1 | tail -5`
Expected: FAIL — build failure, no Go files / undefined identifiers.

- [ ] **Step 3: Write `core/label.go`**

```go
// Package core is ATM's domain leaf: the label algebra every adapter shares.
//
// It imports nothing from this repository and nothing outside the standard
// library. That is a hard rule, not a preference — see
// docs/architecture/logical-components.md. In particular core does not know
// what a Task is; the grouping functions take a labelsOf accessor so the
// caller keeps its own type.
package core

import "strings"

// IsWildcard reports whether a label is a facet declaration — a token ending
// in ":*", e.g. "ATM:status:*" or "ATM:*". A wildcard declares a facet and
// does NOT restrict a query; see RestrictingTokens.
func IsWildcard(label string) bool { return strings.HasSuffix(label, ":*") }

// LabelMatchesWildcard reports whether label falls under wildcard, e.g.
// "ATM:status:open" matches both "ATM:status:*" and "ATM:*". The match is a
// plain prefix test against the wildcard minus its "*", so it does not require
// the prefix to end on a segment boundary.
func LabelMatchesWildcard(label, wildcard string) bool {
	return strings.HasPrefix(label, strings.TrimSuffix(wildcard, "*"))
}

// WildcardTokens returns the facet-declaring tokens of labels, in order.
func WildcardTokens(labels []string) []string {
	var out []string
	for _, l := range labels {
		if IsWildcard(l) {
			out = append(out, l)
		}
	}
	return out
}

// RestrictingTokens returns the query-restricting (non-wildcard) tokens of
// labels, in order. Together with WildcardTokens it partitions the input.
func RestrictingTokens(labels []string) []string {
	var out []string
	for _, l := range labels {
		if !IsWildcard(l) {
			out = append(out, l)
		}
	}
	return out
}

// FacetToken returns the wildcard label that facets a scope by a namespace:
// FacetToken("ATM", "status") == "ATM:status:*".
func FacetToken(scope, ns string) string { return scope + ":" + ns + ":*" }

// HasBareTag reports whether labels contains at least one unnamespaced (bare)
// label within scope — one whose suffix after "<scope>:" holds no further
// colon, e.g. "ATM:urgent".
func HasBareTag(scope string, labels []string) bool {
	for _, full := range labels {
		if !strings.HasPrefix(full, scope+":") {
			continue
		}
		if !strings.Contains(strings.TrimPrefix(full, scope+":"), ":") {
			return true
		}
	}
	return false
}
```

**Note on `HasBareTag`:** the original `taskHasBareTag` called `strings.TrimPrefix` without first checking the prefix matched, so a foreign-scope label like `OTHER:urgent` was tested whole, found no colon after the no-op trim, and counted as a bare tag. The `HasPrefix` guard above fixes that. If the store/TUI goldens change because of it, stop — that means the case is reachable and the change belongs in Task 6, not here. (The corpus uses only `ATM:`-scoped labels, so it should not fire.)

- [ ] **Step 4: Write `core/filter.go`**

```go
package core

import "strings"

// ParseFilter splits a filter string into its label tokens. Tokens ending
// ":*" are facets; the rest restrict. Empty or blank input yields nil.
func ParseFilter(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Fields(s)
}

// FilterHasToken reports whether token is one of filter's space-separated
// fields.
func FilterHasToken(filter, token string) bool {
	for _, f := range strings.Fields(filter) {
		if f == token {
			return true
		}
	}
	return false
}

// FilterAddToken appends token to filter, single-space separated, unless it is
// already present.
func FilterAddToken(filter, token string) string {
	if FilterHasToken(filter, token) {
		return filter
	}
	if strings.TrimSpace(filter) == "" {
		return token
	}
	return filter + " " + token
}

// FilterRemoveToken removes every occurrence of token from filter, rejoining
// the remainder with single spaces.
func FilterRemoveToken(filter, token string) string {
	var kept []string
	for _, f := range strings.Fields(filter) {
		if f != token {
			kept = append(kept, f)
		}
	}
	return strings.Join(kept, " ")
}
```

- [ ] **Step 5: Run core's tests**

Run: `go test ./internal/core/ -v 2>&1 | tail -12`
Expected: PASS — all of `TestIsWildcard`, `TestLabelMatchesWildcard`, `TestWildcardAndRestrictingTokensPartition`, `TestFacetToken`, `TestHasBareTag`, `TestParseFilter`, `TestFilterTokenOps`.

- [ ] **Step 6: Verify the leaf rule mechanically**

Run: `go list -deps ./internal/core | grep '^atm/' | grep -v '^atm/internal/core$' || echo "LEAF OK"`
Expected: `LEAF OK`.

- [ ] **Step 7: Point `store` at core**

In `internal/store/query.go`: add `"atm/internal/core"` to the import block. Delete these four functions entirely (lines ~212-237):

```go
func restrictingTokens(labels []string) []string { ... }
func wildcardTokens(labels []string) []string { ... }
func isWildcard(l string) bool { ... }
func labelMatchesWildcard(label, wildcard string) bool { ... }
```

Then replace their uses:
- Line ~43: `restricting := restrictingTokens(filters.Labels)` → `restricting := core.RestrictingTokens(filters.Labels)`
- Line ~158: `for _, w := range wildcardTokens(filters.Labels) {` → `for _, w := range core.WildcardTokens(filters.Labels) {`
- Line ~168: `wildcards := wildcardTokens(filters.Labels)` → `wildcards := core.WildcardTokens(filters.Labels)`
- Lines ~177 and ~196 (inside the bucketing loops, which stay until Task 5): `labelMatchesWildcard(l, w)` → `core.LabelMatchesWildcard(l, w)`

Check for other users: `grep -rn "isWildcard(\|labelMatchesWildcard(\|wildcardTokens(\|restrictingTokens(" internal/store/` must return only `core.`-prefixed hits afterwards.

- [ ] **Step 8: Point `tui` at core**

In `internal/tui/tasks_grouping.go`: add `"atm/internal/core"` to imports; drop `"sort"`/`"strings"` only if they become unused (`buildNestedGroups` still needs `sort`). Delete `taskHasBareTag`, `isWildcardTUI`, `facetToken`, `filterHasToken`, `filterAddToken`, `filterRemoveToken`, `wildcardTokens`, `labelMatchesWildcardTUI`. Keep `buildNestedGroups` for now (Task 4) and rewrite its match call to `core.LabelMatchesWildcard`. Rewrite:

```go
func (t *tasksModel) parseFilter() []string { return core.ParseFilter(t.filter) }

func (t *tasksModel) hasWildcard() bool {
	for _, tok := range t.parseFilter() {
		if core.IsWildcard(tok) {
			return true
		}
	}
	return false
}
```

Then fix the call sites:
- `internal/tui/tasks.go:134`: `taskHasBareTag(scope, tk)` → `core.HasBareTag(scope, tk.Labels)`
- `internal/tui/tasks.go:144,165`: `wildcardTokens(filters)` → `core.WildcardTokens(filters)`
- `internal/tui/labels.go:552,732,778`: `facetToken(...)` → `core.FacetToken(...)`
- `internal/tui/labels.go:749`: `facetToken(scope, b.ns)` → `core.FacetToken(scope, b.ns)`
- Any `filterHasToken`/`filterAddToken`/`filterRemoveToken` call → `core.Filter*`. Find them with:
  `grep -rn "filterHasToken(\|filterAddToken(\|filterRemoveToken(\|facetToken(\|taskHasBareTag(\|wildcardTokens(\|isWildcardTUI(\|labelMatchesWildcardTUI(" internal/tui/ --include='*.go' | grep -v _test.go`

Add the `"atm/internal/core"` import to every file you touch.

- [ ] **Step 9: Move the two TUI unit tests to core**

In `internal/tui/tasks_test.go`, **delete** `TestFilterTokenHelpers` (line ~191) and `TestTaskHasBareTag` (line ~218) in full. Their subjects now live in `core` and are covered by `TestFilterTokenOps`, `TestFacetToken`, and `TestHasBareTag` from Step 1. Deleting rather than rewriting is correct: a TUI test that only exercises `core.FilterAddToken` tests the wrong package.

- [ ] **Step 9b: Repair Task 2's characterization test, which this task breaks**

`internal/tui/tasks_grouping_test.go` (Task 2) calls `wildcardTokens`, which Step 8 just deleted. It will not compile. Repair it — **without changing what it characterizes**:

- In `TestFacetTreeCharacterization`: `wildcards := wildcardTokens(filters)` → `wildcards := core.WildcardTokens(filters)`
- Add `"atm/internal/core"` to the file's imports.

Nothing else in that file changes: it drives a real store, so it has no local copy of the algebra to update.

The golden must not move. That this is a pure rename is the point: same algebra, new home.

- [ ] **Step 10: Build and run the full internal suite**

Run: `go build ./... && go test ./internal/core/ ./internal/store/ ./internal/tui/ ./internal/cli/ 2>&1 | tail -10`
Expected: all PASS.

**The two goldens must be untouched.** Verify:

Run: `git status --porcelain internal/store/testdata/ internal/tui/testdata/`
Expected: empty output. If a golden shows as modified, a behavior change slipped in — most likely the `HasBareTag` prefix guard. Investigate before committing; do not rebaseline.

- [ ] **Step 11: Commit**

```bash
git add internal/core/ internal/store/query.go internal/tui/tasks_grouping.go \
        internal/tui/tasks.go internal/tui/labels.go internal/tui/tasks_test.go
git commit -m "refactor(ATM-cca7b0): create internal/core; unify label and filter algebra

Creates the domain leaf named by docs/architecture/logical-components.md and
moves the label-string algebra into it: wildcard predicates, token
partitioning, facet tokens, the bare-tag predicate, and the filter-token ops.
store and tui now share one copy; the character-identical *TUI-suffixed twins
are gone.

taskHasBareTag becomes core.HasBareTag(scope, labels) — core takes labels, not
a Task, so the Task move stays step 4's scope (ATM-b9d83a).

Characterization goldens are untouched: behavior is unchanged.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: `core.GroupNested`; delete `buildNestedGroups`

**Files:**
- Create: `internal/core/facet.go` (the `Node`/`GroupNested` half; `GroupByWildcard` arrives in Task 5)
- Create: `internal/core/facet_test.go`
- Modify: `internal/tui/tasks_grouping.go` (delete `buildNestedGroups`, add `nodesToGroups` + `taskLabels`)
- Modify: `internal/tui/tasks.go:152,174` (call sites)
- Modify: `internal/tui/tasks_test.go` (`TestBuildNestedGroupsTwoWildcards`, `TestBuildNestedGroupsThreeWildcards`)

**Interfaces:**
- Consumes: `core.LabelMatchesWildcard` from Task 3.
- Produces:
  ```go
  // package core
  type Node[T any] struct {
      Label    string   // "" is the (no matching labels) bucket
      Items    []T      // leaf level only
      Children []Node[T]
  }
  func GroupNested[T any](items []T, labelsOf func(T) []string, wildcards []string) []Node[T]

  // package tui
  func taskLabels(t *store.Task) []string
  func nodesToGroups(nodes []core.Node[*store.Task], toRow func(*store.Task) taskRow) []taskGroup
  ```

**Context the implementer needs:** `GroupNested` is a **faithful port** of `buildNestedGroups`, not an improvement. It must keep: bucketing by `wildcards[0]` only; alphabetically sorted keys; the `""` (no matching labels) bucket emitted last and only when non-empty; recursion while `len(wildcards) >= 2`; and items attached at the deepest level only. The call site keeps passing `wildcards[1:]` — the composition oddity stays until Task 6, which is the task that fixes it. Moving the seam is the whole job here.

One deliberate difference: the original tracked matched tasks with `map[*store.Task]bool` (pointer identity). `T` is not constrained to `comparable`, so the port tracks by index with `[]bool`. These agree for distinct pointers, which is what the callers pass.

- [ ] **Step 1: Write the failing test**

Create `internal/core/facet_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/core/ -run TestGroupNested 2>&1 | tail -5`
Expected: FAIL — `undefined: GroupNested`.

- [ ] **Step 3: Write `core/facet.go`**

```go
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
```

- [ ] **Step 4: Run core's tests**

Run: `go test ./internal/core/ -v -run TestGroupNested 2>&1 | tail -10`
Expected: PASS — all six.

- [ ] **Step 5: Replace `buildNestedGroups` with the adapter**

In `internal/tui/tasks_grouping.go`, delete `buildNestedGroups` entirely (and `labelMatchesWildcardTUI` if Task 3 left it). Drop the now-unused `"sort"` import. Add:

```go
// taskLabels is the core.GroupNested accessor for store tasks. It is the only
// thing core needs to know about a Task — the type itself stays in store
// until ATM-b9d83a.
func taskLabels(t *store.Task) []string { return t.Labels }

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
```

- [ ] **Step 6: Update the two call sites, preserving the composition**

`internal/tui/tasks.go:152` and `:174` — both currently read:

```go
tg.subgroups = buildNestedGroups(g.Tasks, wildcards[1:], t.toRow)
```

Replace each with:

```go
tg.subgroups = nodesToGroups(core.GroupNested(g.Tasks, taskLabels, wildcards[1:]), t.toRow)
```

**Keep `wildcards[1:]`.** The top level still comes from `store.GroupTasks`. Changing this to `wildcards` is Task 6's job and would break the golden here.

- [ ] **Step 7: Port the two TUI nesting tests**

In `internal/tui/tasks_test.go`, `TestBuildNestedGroupsTwoWildcards` (line ~30) and `TestBuildNestedGroupsThreeWildcards` (line ~90) call the deleted function. Rewrite **only** their call lines, leaving each test's corpus and assertions exactly as they are — they assert the tree shape, which has not changed:

```go
// was: subs := buildNestedGroups(openTasks, wildcards, toRowTest)
subs := nodesToGroups(core.GroupNested(openTasks, taskLabels, wildcards), toRowTest)
```

```go
// was: subs := buildNestedGroups(tasks, wildcards, toRowTest)
subs := nodesToGroups(core.GroupNested(tasks, taskLabels, wildcards), toRowTest)
```

Add `"atm/internal/core"` to the file's imports. That these tests pass unchanged is evidence the port is faithful — if an assertion fails, the port is wrong, not the test.

- [ ] **Step 8: Verify — tests pass and goldens are untouched**

Run: `go build ./... && go test ./internal/core/ ./internal/tui/ ./internal/store/ 2>&1 | tail -6`
Expected: PASS.

Run: `git status --porcelain internal/store/testdata/ internal/tui/testdata/`
Expected: empty. A modified golden here means the port is not faithful — fix the port, do not rebaseline.

Run: `grep -rn "buildNestedGroups" internal/ || echo "GONE"`
Expected: `GONE`.

- [ ] **Step 9: Commit**

```bash
git add internal/core/facet.go internal/core/facet_test.go \
        internal/tui/tasks_grouping.go internal/tui/tasks.go internal/tui/tasks_test.go
git commit -m "refactor(ATM-cca7b0): move nested faceting into core.GroupNested

buildNestedGroups is deleted; the TUI now calls core.GroupNested through a
nodesToGroups adapter that attaches rows and collapse state. GroupNested is
generic over a labelsOf accessor, so core still does not name Task.

A faithful port, deliberately: the call site keeps passing wildcards[1:] on
top of store's flat level 1, preserving today's composition. The existing
nesting tests pass unchanged and both characterization goldens are untouched.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `core.GroupByWildcard` (with the dedup fix); delete store's bucketing

**Files:**
- Modify: `internal/core/facet.go` (add `Group[T]` and `GroupByWildcard`)
- Modify: `internal/core/facet_test.go` (add its tests)
- Modify: `internal/store/query.go:156-209` (`GroupTasksErr` body)
- Modify: `internal/store/testdata/facet_flat.golden`, `internal/tui/testdata/facet_tree.golden` (**expected** to change — dedup only)

**Interfaces:**
- Consumes: `core.LabelMatchesWildcard`, `core.WildcardTokens` from Task 3.
- Produces:
  ```go
  // package core
  type Group[T any] struct {
      Label string
      Items []T
  }
  func GroupByWildcard[T any](items []T, labelsOf func(T) []string, wildcards []string) (groups []Group[T], others []T)

  // package store
  func taskLabels(t *Task) []string
  ```

**Context the implementer needs:** This task moves store's flat bucketing into `core` **and fixes the duplicate-append defect in the same commit**. Both halves matter, and the order is not optional.

The defect: `store/query.go:174-185` nests its loops `for item { for wildcard { for label } }`, so an item whose label matches two wildcards (e.g. `ATM:*` and `ATM:status:*`, or a repeated token) is appended to that one bucket once per matching wildcard. The fix is a loop-nesting swap — labels outer, wildcards inner — so each (item, label) pair is considered exactly once.

**Do the port and the fix as two separate acts, in order** (Steps 3 and 5 below). Port faithfully first and confirm the goldens do **not** move; that check is what tells you the port is right, and it is the only place attribution between "move" and "fix" happens, since the commit carries both. Then apply the dedup and let the goldens change. Do not skip the intermediate check to save a test run — it is the task's main safeguard.

An earlier draft of this plan shipped the buggy port as its own commit so the golden would prove neutrality. That was rejected deliberately: it would put a function documented as defective, and a test asserting defective output, into the branch's history. The check survives without the bad commit.

`GroupTasksErr` keeps its signature and its `ErrBoardNotAFacet` guard; only the bucketing body is replaced. Note `LabelGroup` (`{Label string; Tasks []*Task}`) is `store`'s exported type and part of the CLI's JSON contract — it stays; the code maps `core.Group` onto it.

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/facet_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/core/ -run TestGroupByWildcard 2>&1 | tail -5`
Expected: FAIL — `undefined: GroupByWildcard`.

- [ ] **Step 3: Add `Group` and the FAITHFUL port to `core/facet.go`**

Transcribe `store/query.go:172-208` as it stands — **including the defect**. This is a temporary state that you will fix in Step 5 before committing; nothing here reaches a commit.

```go
// Group is one flat facet bucket: every item carrying Label.
type Group[T any] struct {
	Label string
	Items []T
}

// GroupByWildcard buckets items under every concrete label they carry that
// matches ANY of wildcards — one flat level, keys sorted. Items carrying no
// matching label are returned in others.
func GroupByWildcard[T any](items []T, labelsOf func(T) []string, wildcards []string) (groups []Group[T], others []T) {
	if len(wildcards) == 0 {
		return nil, items
	}
	buckets := map[string][]T{}
	var order []string
	for _, it := range items {
		for _, w := range wildcards { // faithful port: fixed in Step 5
			for _, l := range labelsOf(it) {
				if !LabelMatchesWildcard(l, w) {
					continue
				}
				if _, exists := buckets[l]; !exists {
					order = append(order, l)
				}
				buckets[l] = append(buckets[l], it)
			}
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
```

- [ ] **Step 4: Rewrite `GroupTasksErr`'s body**

See Step 4b below for the exact code. Apply it now, while the port is still faithful.

- [ ] **Step 5: NEUTRALITY CHECKPOINT — confirm the port changed nothing**

Run only the characterization tests (the three dedupe tests from Step 1 are *expected* to fail at this point — the fix is not in yet):

```
go test ./internal/store/ -run TestGroupTasksCharacterization && go test ./internal/tui/ -run TestFacetTreeCharacterization
git status --porcelain internal/store/testdata/ internal/tui/testdata/
```

Expected: both PASS and `git status` prints **nothing**.

**This checkpoint is the task's main safeguard.** The commit carries both a move and a fix, so this is the only moment that isolates them. If a golden moves here, the port is not faithful — fix the port before going on. Do not proceed with a dirty golden, and do not regenerate the goldens here.

- [ ] **Step 6: Apply the dedup fix**

Now swap the loop nesting and update the doc comment. Replace the whole function:

```go
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
```

Run: `go test ./internal/core/ -v -run TestGroupByWildcard 2>&1 | tail -8`
Expected: PASS — all six, including the three dedupe tests that failed at Step 4.

**Reference code for Step 4 — the `GroupTasksErr` body:**

In `internal/store/query.go`, replace lines ~156-209 (from `func (s *Store) GroupTasksErr` to its closing brace) with:

```go
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
```

Note the early `len(wildcards) == 0` return stays: it returns `nil, inScope, nil`, which `core.GroupByWildcard` would also produce, but keeping it preserves the exact `[]LabelGroup(nil)` (not empty-slice) return the golden and the CLI's JSON see.

Check `"sort"` is still used in `query.go` (it is — `ListTasksErr` sorts); leave the import.

- [ ] **Step 7: Regenerate both goldens and read the diff**

The dedup changes what the goldens record, so they must be regenerated now:

```
ATM_UPDATE_GOLDEN=1 go test ./internal/store/ -run TestGroupTasksCharacterization
ATM_UPDATE_GOLDEN=1 go test ./internal/tui/ -run TestFacetTreeCharacterization
git diff internal/store/testdata/facet_flat.golden internal/tui/testdata/facet_tree.golden
```

**Required review — this is the deliverable.** The diff must contain **only** removals of duplicated titles (pure deletions; zero insertions), and only under filters whose wildcards **overlap** — i.e. where two tokens can match the same label. There are three such cases in the corpus:
- `== filter: [ATM:* ATM:status:*]` — `ATM:*` overlaps `ATM:status:*`
- `== filter: [ATM:status:* ATM:status:*]` — a token overlaps itself
- `== filter: [ATM:status:* ATM:type:* ATM:*]` — `ATM:*` overlaps **both** other tokens

The control that proves the port faithful: `== filter: [ATM:status:* ATM:type:*]` has two wildcards but they are **disjoint**, so it must be byte-identical. Likewise every single-facet case. If a disjoint or single-facet case moved, the port is not faithful — go back to Step 5's checkpoint rather than accepting the new golden.

No group, ordering, or `others` membership may change anywhere, and the TUI tree **shape** must not change (that is Task 6).

- [ ] **Step 8: Verify the rest**

Run: `go build ./... && go test ./internal/core/ ./internal/store/ ./internal/tui/ ./internal/cli/ 2>&1 | tail -8`
Expected: all PASS.

Run: `grep -rn "isWildcard(\|labelMatchesWildcard(\|wildcardTokens(\|restrictingTokens(" internal/store/*.go | grep -v "core\." || echo "NO LOCAL COPIES"`
Expected: `NO LOCAL COPIES`.

- [ ] **Step 9: Confirm the leaf rule still holds**

Run: `go list -deps ./internal/core | grep '^atm/' | grep -v '^atm/internal/core$' || echo "LEAF OK"`
Expected: `LEAF OK`.

- [ ] **Step 10: Commit**

```bash
git add internal/core/facet.go internal/core/facet_test.go internal/store/query.go \
        internal/store/testdata/facet_flat.golden internal/tui/testdata/facet_tree.golden
git commit -m "refactor(ATM-cca7b0): move flat faceting into core; dedupe overlapping facets

GroupTasksErr keeps its signature, its ErrBoardNotAFacet guard, and its
LabelGroup return shape — only the bucketing body moves to core, completing the
unification: the faceting algebra now has exactly one copy.

The move carries a fix. store/query.go iterated wildcards inside labels, so an
item whose label matched two wildcards was appended to that bucket once per
match — ATM:* plus ATM:status:* listed a task twice in one group. Reachable
from documented input: the QueryFilters doc comment names ATM:* as a facet.
Labels outer / wildcards inner considers each (item, label) pair once.
Multi-membership across DIFFERENT labels is unaffected and now has a regression
test.

The port was verified faithful before the fix was applied — goldens unchanged
against the straight transcription — so the golden diff here is the dedup
alone: duplicate titles removed under the two overlapping-wildcard filters,
every other case byte-identical.

cli --facets keeps its groups/others JSON shape; its content changes only where
a task was previously listed twice.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Nest the TUI's tree from `wildcards[0]`

**Files:**
- Modify: `internal/tui/tasks.go:141-186` (source the tree from `wildcards[0]`)
- Modify: `internal/tui/tasks_grouping_test.go` (mirror the new wiring)
- Modify: `internal/tui/testdata/facet_tree.golden` (**expected** to change)

**Interfaces:**
- Consumes: `core.GroupNested`, `core.GroupByWildcard`, `core.WildcardTokens` (Tasks 3-5); `nodesToGroups`, `taskLabels` (Task 4).
- Produces: no new signatures.

**Context the implementer needs:** This is the last behavior change in the series, and the TUI golden exists for it. The golden diff must be **read and understood** before committing — that is the deliverable, not a side effect.

Today the TUI takes its top level from `store.GroupTasks` (flat, bucketing by *any* matching wildcard) and passes only `wildcards[1:]` to the nested pass. A two-wildcard filter therefore facets the top level by every namespace at once and hangs `type:*` subgroups under the `type:` groups themselves. It should nest from `wildcards[0]`, which is what `buildNestedGroups`' original doc comment and mockup Screen 7 always described.

The TUI keeps calling `GroupTasksErr` — **discarding its groups** — for two things it still needs: the `others` bucket (used by `focusAbsent` and `focusOff`) and the `ErrBoardNotAFacet` guard, which today it inherits by `GroupTasks` swallowing the error and returning `nil, nil`. Dropping that call would silently start faceting by boards.

`internal/store` is not touched by this task.

- [ ] **Step 1: Fix the TUI's tree — `focusPresent`/`focusAbsent`**

In `internal/tui/tasks.go`, replace the `case focusPresent, focusAbsent:` body's grouping half (lines ~141-161, from `filters := t.parseFilter()` to the end of that case) with:

```go
		filters := t.parseFilter()
		wildcards := core.WildcardTokens(filters)
		// GroupTasksErr is still the source of the others bucket and of the
		// board-as-facet guard (a board has no members). Its groups are
		// discarded: the tree below nests from wildcards[0] rather than
		// taking a flat level 1.
		_, others, gerr := t.m.store.GroupTasksErr(store.QueryFilters{Project: scope, Labels: filters})
		if gerr != nil {
			break // matches the old GroupTasks, which swallowed the error and rendered nothing
		}
		if t.focus.mode == focusPresent {
			inScope := t.m.store.ListTasks(store.QueryFilters{Project: scope, Labels: filters})
			t.groups = nodesToGroups(core.GroupNested(inScope, taskLabels, wildcards), t.toRow)
		} else {
			for _, tk := range t.applySort(others) {
				t.rows = append(t.rows, t.toRow(tk))
			}
		}
```

- [ ] **Step 2: Fix the TUI's tree — `focusOff`**

Replace the `default: // focusOff` body (lines ~162-187) with:

```go
	default: // focusOff
		filters := t.parseFilter()
		wildcards := core.WildcardTokens(filters)
		if len(wildcards) == 0 {
			for _, tk := range t.applySort(t.m.store.ListTasks(store.QueryFilters{Project: scope, Labels: filters})) {
				t.rows = append(t.rows, t.toRow(tk))
			}
			break
		}
		_, others, gerr := t.m.store.GroupTasksErr(store.QueryFilters{Project: scope, Labels: filters})
		if gerr != nil {
			break
		}
		inScope := t.m.store.ListTasks(store.QueryFilters{Project: scope, Labels: filters})
		t.groups = nodesToGroups(core.GroupNested(inScope, taskLabels, wildcards), t.toRow)
		for _, tk := range others {
			t.others = append(t.others, t.toRow(tk))
		}
	}
```

- [ ] **Step 3: Update the TUI characterization test to the new wiring**

`TestFacetTreeCharacterization` in `internal/tui/tasks_grouping_test.go` reproduces the **old** composition, so it must now mirror the new one. Replace its per-case body (inside the `for _, filters := range facetCases` loop) with:

```go
		fmt.Fprintf(&b, "== filter: [%s]\n", strings.Join(filters, " "))
		wildcards := core.WildcardTokens(filters)
		groups := nodesToGroups(core.GroupNested(tasks, taskLabels, wildcards), toRowTest)
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
```

The `store` import stays — `newFacetStore` still uses it. Drop any import that falls unused.

- [ ] **Step 4: Regenerate the TUI golden**

Only the tree changes in this task; the store golden must not move.

Run: `ATM_UPDATE_GOLDEN=1 go test ./internal/tui/ -run TestFacetTreeCharacterization`

- [ ] **Step 5: Read the golden diff — the deliverable**

Run: `git status --porcelain internal/store/testdata/`
Expected: **empty**. This task does not touch `internal/store`; if its golden moved, something is wrong — stop.

Run: `git diff internal/tui/testdata/facet_tree.golden`

**Required review.** The diff must show exactly these changes and nothing else:
- Under two- and three-wildcard filters, the top level now lists **only** `ATM:status:...` groups (plus the `(no matching labels)` bucket) — no `ATM:type:...` at the top.
- No group is nested under a group of its own namespace.
- The single-wildcard and zero-wildcard cases are **unchanged** — nesting from `wildcards[0]` is identical to the old path when there is only one wildcard.

If anything else moved, stop and investigate before committing.

- [ ] **Step 6: Full verification**

Run: `make verify 2>&1 | tail -20`
Expected: build, test, and scripts-test all green.

Run: `go list -deps ./internal/core | grep '^atm/' | grep -v '^atm/internal/core$' || echo "LEAF OK"`
Expected: `LEAF OK`.

Confirm no string surgery on `:*` tokens is left in the TUI:

Run: `grep -rn 'HasSuffix\|TrimSuffix' internal/tui/*.go | grep -v _test.go | grep '\*' || echo "NO WILDCARD SURGERY IN TUI"`
Expected: `NO WILDCARD SURGERY IN TUI`. (`hasWildcard` and `grouped` survive by name — they are focus-mode policy and delegate to `core`. A bare grep for `wildcard` is not the criterion.)

- [ ] **Step 7: Confirm the CLI contract across the whole series**

`cli/task.go:108-119` only serializes `GroupTasksErr`'s return through `groupsToJSON`/`tasksToJSON`, so `facet_flat.golden` *is* the `--facets` payload. Its history therefore tells you whether the contract held — do not build duplicate binaries to re-derive it.

Run: `go test ./internal/cli/ 2>&1 | tail -3`
Expected: PASS.

Run: `git log --oneline -- internal/store/testdata/facet_flat.golden`
Expected: exactly **two** commits — Task 1 (created) and Task 5 (dedup). If a commit from Task 3, 4, or 6 appears, a task changed behavior it had no business changing; investigate before this lands.

The `--facets` **shape** never changed. Its content changed once, in Task 5, and only where a task was previously listed twice.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/tasks.go internal/tui/tasks_grouping_test.go \
        internal/tui/testdata/facet_tree.golden
git commit -m "fix(ATM-cca7b0): nest the TUI facet tree from wildcards[0]

The TUI took its top level from store's flat grouping and nested only
wildcards[1:], so a filter with two or more wildcards faceted the top level by
every namespace at once and hung type subgroups under the type groups
themselves. It now calls core.GroupNested from wildcards[0] — the tree
buildNestedGroups' own doc comment and mockup Screen 7 always described.

GroupTasksErr is still called, with its groups discarded, for the two things
the TUI still needs from it: the others bucket and the board-as-facet guard,
which it previously inherited by GroupTasks swallowing the error.

Only the tree golden moves, and only for multi-wildcard filters; single- and
zero-wildcard rendering is byte-identical. internal/store and the cli --facets
contract are untouched by this commit.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 9: Record completion on the ledger**

```bash
atm task comment add --task ATM-cca7b0 --actor 'developer@claude:opus-4.8' \
  --label ATM:comment:progress \
  --body 'Step 3 complete on worktree-atm-cca7b0-core-faceting. internal/core exists as a pure leaf (go list -deps shows no atm/internal). Faceting/wildcard algebra has exactly one copy; store/query.go and tui/tasks_grouping.go both delegate. make verify green. Two defects found during the move are fixed with their golden diffs visible: the duplicate append (ATM:* + ATM:status:*) lands with the flat-grouping move, and the TUI top-level flat/nested mismatch lands last. cli --facets JSON shape unchanged throughout; its content changed once, where a task was previously listed twice. Task type did NOT move — that stays ATM-b9d83a (step 4).'
```

---

## Notes for the reviewer

**What "neutral" means here.** Tasks 3 and 4 must leave `internal/store/testdata/facet_flat.golden` and `internal/tui/testdata/facet_tree.golden` byte-identical. They do change `internal/tui/tasks_test.go` — four tests whose subjects move into `core` — which is expected and not a neutrality violation. Test churn is fine; golden churn is not.

**Task 5 mixes a move with a fix, deliberately.** An earlier draft ported `GroupByWildcard` bug-for-bug in Task 5 and fixed it in Task 6, so an unchanged golden would prove the move neutral. That was rejected: it puts a function documented as defective — and a test asserting defective output — into the branch history, where a reviewer cannot distinguish a deliberate port from a mistake. Task 5 instead checks neutrality *before* applying the fix (its Step 5) and commits once. If you are reviewing Task 5, the golden diff should contain only duplicate-title removals under the two overlapping-wildcard filters; anything more means the port was not faithful.

**If Task 6 proves contentious** it can be dropped entirely. Tasks 1-5 stand alone: `core` exists, the duplication is gone, and the dedup bug is fixed. Only the TUI tree shape would remain to re-file.
