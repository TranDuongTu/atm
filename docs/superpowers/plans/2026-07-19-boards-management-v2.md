# Boards Management v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the TUI Boards ring capability-authored (enabled capabilities' `Exposed` labels + one synthetic "unmanaged" umbrella), with per-project display preferences (order/hidden/pins) in `config.json.boards`, and a CLI triage surface for the manager agent.

**Architecture:** The `Capability` contract gains two pure methods — `Vocabulary` (ownership) and `Exposed` (ring display) — fed by the same literal list `EnsureVocabulary` seeds. `Registry` gains `Exposed` (owner-tagged enumeration) and `Unmanaged` (LabelList minus owned set). Display preferences move to `core.ProjectConfig.Boards` (`pins.json` is lazily migrated then retired). The TUI's `buildBoardRows` is rewritten from emergent derivation to capability-authored population; the old emergent derivation survives only inside the umbrella's drill-in sub-table.

**Tech Stack:** Go, cobra (CLI), bubbletea/lipgloss (TUI), golden-file CLI tests, table-driven unit tests.

**Spec:** `docs/superpowers/specs/2026-07-19-boards-management-v2-design.md` (revised — see its §10). Tracking task: ATM-87b3ae.

## Global Constraints

- Umbrella sentinel FullName is `<CODE>:unmanaged` — never a real label; shadowed (suppressed) if a real `<CODE>:unmanaged` or `<CODE>:unmanaged:*` label exists.
- `Exposed ⊆ Vocabulary` for every capability; `EnsureVocabulary` seeds exactly `Vocabulary`.
- Ownership rule: a stored label is managed iff its FullName is in an enabled capability's `Vocabulary`, or it sits under an owned `:*` descriptor (`<CODE>:<ns>:<value>` with `<CODE>:<ns>:*` owned).
- `boards.order` is a partial override: listed-and-present names first in list order, everything else keeps registration order (umbrella defaults last). Unknown names in `order`/`hidden` are silently ignored.
- `hidden` beats `order` and pin candidacy; persists across capability disable/re-enable.
- Max 3 pins, enforced at write time (`SetProjectBoards` → `core.ErrUsage`) and at render time.
- No new store events; display preferences are config, not substrate state. No event-log entries.
- Umbrella ring selection applies **no** task filter; `selectDefault` never picks the umbrella.
- All mutating verbs require `--actor`; read-only verbs default it. Match existing `atm project` verb shapes.
- After each task's commit, journal progress: `atm task comment add --task ATM-87b3ae --actor developer@claude:fable-5 --label ATM:comment:progress --body "<what was done>"`.
- Run `go build ./... && go test ./<changed packages>` before every commit; full `go test ./...` at Tasks 7 and 10.

---

### Task 1: Capability contract — `Vocabulary` + `Exposed`

**Files:**
- Modify: `internal/capability/capability.go` (interface, ~line 47)
- Modify: `internal/capability/workflow/vocabulary.go` (refactor around one literal list)
- Modify: `internal/capability/workflow/command.go` (Cap methods)
- Modify: `internal/capability/contextmap/vocabulary.go` (same refactor)
- Modify: `internal/capability/contextmap/command.go` (Cap methods)
- Modify: `internal/capability/capability_test.go` (`fakeCap` gains the two methods)
- Modify: `internal/cli/env_test.go` (`fakeMountCap` gains the two methods)
- Test: `internal/capability/workflow/vocabulary_test.go`, `internal/capability/contextmap/vocabulary_test.go`

**Interfaces:**
- Consumes: existing `core.Label`, `core.LabelService`.
- Produces: `Capability.Vocabulary(code string) []core.Label`, `Capability.Exposed(code string) []core.Label`; package-level `workflow.Vocabulary/Exposed`, `contextmap.Vocabulary/Exposed`. Task 2's registry methods and Task 4's TUI depend on these exact names.

- [ ] **Step 1: Write the failing tests**

Append to `internal/capability/workflow/vocabulary_test.go`:

```go
func TestVocabularyAndExposedSets(t *testing.T) {
	vocab := Vocabulary("ATM")
	if len(vocab) != 13 {
		t.Fatalf("Vocabulary = %d labels, want 13", len(vocab))
	}
	byName := map[string]core.Label{}
	for _, l := range vocab {
		byName[l.Name] = l
	}
	exp := Exposed("ATM")
	wantOrder := []string{
		"ATM:all-tasks", "ATM:open-tasks", "ATM:in-progress-tasks", "ATM:backlog",
		"ATM:status:*", "ATM:priority:*",
	}
	if len(exp) != len(wantOrder) {
		t.Fatalf("Exposed = %d labels, want %d", len(exp), len(wantOrder))
	}
	for i, l := range exp {
		if l.Name != wantOrder[i] {
			t.Errorf("Exposed[%d] = %s, want %s", i, l.Name, wantOrder[i])
		}
		// Exposed ⊆ Vocabulary, with identical content.
		if v, ok := byName[l.Name]; !ok || v != l {
			t.Errorf("Exposed[%d] %s not identical to Vocabulary entry (%+v vs %+v)", i, l.Name, l, byName[l.Name])
		}
	}
	if exp[0].Expr == "" || exp[4].Expr != "" {
		t.Errorf("expected boards first (Expr set) then descriptors (Expr empty): %+v", exp)
	}
}

// TestEnsureVocabularySeedsExactlyVocabulary proves the seed-time verb and the
// pure ownership read are the same list: every seeded name is in Vocabulary
// and vice versa, and the board return equals Vocabulary's Expr subset.
func TestEnsureVocabularySeedsExactlyVocabulary(t *testing.T) {
	rec := &seedRecorder{}
	boards, err := EnsureVocabulary(rec, "ATM", "developer@claude:test")
	if err != nil {
		t.Fatal(err)
	}
	vocab := Vocabulary("ATM")
	if len(rec.seeded) != len(vocab) {
		t.Fatalf("seeded %d labels, Vocabulary has %d", len(rec.seeded), len(vocab))
	}
	for i, l := range vocab {
		if rec.seeded[i] != l.Name {
			t.Errorf("seeded[%d] = %s, want %s", i, rec.seeded[i], l.Name)
		}
	}
	var wantBoards []core.Label
	for _, l := range vocab {
		if l.Expr != "" {
			wantBoards = append(wantBoards, l)
		}
	}
	if len(boards) != len(wantBoards) {
		t.Fatalf("boards = %d, want %d", len(boards), len(wantBoards))
	}
	for i := range boards {
		if boards[i] != wantBoards[i] {
			t.Errorf("boards[%d] = %+v, want %+v", i, boards[i], wantBoards[i])
		}
	}
}
```

If `vocabulary_test.go` has no `seedRecorder`-like fake, add one (it only needs `core.LabelService`; unused methods can panic or return zero values):

```go
// seedRecorder is a minimal core.LabelService capturing LabelSeed order.
type seedRecorder struct{ seeded []string }

func (r *seedRecorder) LabelSeed(name, description, expr, actor string) error {
	r.seeded = append(r.seeded, name)
	return nil
}
func (r *seedRecorder) LabelAdd(name, description, expr, actor string) error { return nil }
func (r *seedRecorder) LabelList(project, namespace string) []core.Label     { return nil }
func (r *seedRecorder) LabelShow(name string) (core.Label, error)            { return core.Label{}, nil }
func (r *seedRecorder) LabelRemove(name, actor string) (*core.LabelRemoveResult, error) {
	return nil, nil
}
func (r *seedRecorder) LabelUsageGrouped(projectCode string) (map[string]int, error) {
	return nil, nil
}
```

(Check the existing `vocabulary_test.go` first — it likely already has a fake LabelService; reuse it and only add missing methods rather than duplicating.)

Mirror for contextmap in `internal/capability/contextmap/vocabulary_test.go`:

```go
func TestVocabularyAndExposedSets(t *testing.T) {
	vocab := Vocabulary("ATM")
	if len(vocab) != 9 {
		t.Fatalf("Vocabulary = %d labels, want 9", len(vocab))
	}
	exp := Exposed("ATM")
	if len(exp) != 1 || exp[0].Name != "ATM:context-current" || exp[0].Expr == "" {
		t.Fatalf("Exposed = %+v, want exactly the context-current board", exp)
	}
	found := false
	for _, l := range vocab {
		if l == exp[0] {
			found = true
		}
	}
	if !found {
		t.Error("Exposed[0] must be identical to its Vocabulary entry")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/... 2>&1 | head -20`
Expected: FAIL — `undefined: Vocabulary`, `undefined: Exposed`.

- [ ] **Step 3: Implement**

`internal/capability/capability.go` — extend the `Capability` interface (after `Guide()`, before `EnsureVocabulary`):

```go
	// Vocabulary declares every label this capability owns for the project:
	// stored labels, namespace descriptors, and boards — exactly the set
	// EnsureVocabulary seeds. Pure read, no store side effect. This is the
	// OWNERSHIP surface: Registry.Unmanaged subtracts it.
	Vocabulary(code string) []core.Label
	// Exposed declares the computed labels (boards + namespace descriptors)
	// this capability surfaces in the TUI ring for the project. Pure read,
	// no store side effect. Order within the slice is the capability's
	// preferred ring order; the registry preserves registration order across
	// capabilities. Invariant: Exposed ⊆ Vocabulary.
	Exposed(code string) []core.Label
```

`internal/capability/workflow/vocabulary.go` — replace the body of `EnsureVocabulary` and add the two lists. One literal list feeds all three (delete the old inline `stored`/`boards` literals; keep the `Board*`/`*Expr` helper funcs):

```go
// vocabulary is the single literal list every contract method derives from:
// stored/namespace labels first (Expr == ""), then the four boards, in seed
// order. Ownership (Vocabulary), ring display (Exposed), and seeding
// (EnsureVocabulary) all read this list, so they cannot diverge.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: code + ":status:*", Description: "lifecycle state of a task; exactly one status label should be present"},
		{Name: code + ":status:open", Description: "workflow state: open; task is not started or is being considered"},
		{Name: code + ":status:in-progress", Description: "workflow state: in-progress; someone is actively working on this"},
		{Name: code + ":status:blocked", Description: "workflow state: blocked; task cannot proceed pending something else"},
		{Name: code + ":status:done", Description: "workflow state: done; task is complete"},
		{Name: code + ":priority:*", Description: "urgency ranking for planning; at most one priority label per task, absent means default priority"},
		{Name: code + ":priority:high", Description: "planning priority: high; do this first, everything untagged is default priority"},
		{Name: code + ":priority:medium", Description: "planning priority: medium; do after high-priority work"},
		{Name: code + ":priority:low", Description: "planning priority: low; do when no higher-priority work remains"},
		{Name: BoardBacklog(code), Description: "tasks with no status label: incoming jottings awaiting triage. Review queue alongside open-tasks.", Expr: backlogExpr()},
		{Name: BoardOpenTasks(code), Description: "every open task: the project's active work.", Expr: openTasksExpr()},
		{Name: BoardInProgressTasks(code), Description: "tasks someone is actively working on (status:in-progress).", Expr: inProgressTasksExpr()},
		{Name: BoardAllTasks(code), Description: "every task in the project, ordered by recent activity. Default board in the TUI.", Expr: allTasksExpr()},
	}
}

// Vocabulary returns every label this capability owns for code. Pure.
func Vocabulary(code string) []core.Label { return vocabulary(code) }

// Exposed returns the labels this capability surfaces in the TUI ring, in
// preferred ring order: the four boards, then the two namespaces it owns.
func Exposed(code string) []core.Label {
	byName := map[string]core.Label{}
	for _, l := range vocabulary(code) {
		byName[l.Name] = l
	}
	names := []string{
		BoardAllTasks(code), BoardOpenTasks(code), BoardInProgressTasks(code), BoardBacklog(code),
		code + ":status:*", code + ":priority:*",
	}
	out := make([]core.Label, 0, len(names))
	for _, n := range names {
		out = append(out, byName[n])
	}
	return out
}
```

New `EnsureVocabulary` body (same doc comment, same signature and return semantics):

```go
func EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error) {
	var boards []core.Label
	for _, l := range vocabulary(code) {
		if err := s.LabelSeed(l.Name, l.Description, l.Expr, actor); err != nil {
			return nil, err
		}
		if l.Expr != "" {
			boards = append(boards, l)
		}
	}
	return boards, nil
}
```

`internal/capability/workflow/command.go` — add next to the existing `EnsureVocabulary` method:

```go
// Vocabulary implements capability.Capability (ownership surface).
func (Cap) Vocabulary(code string) []core.Label { return Vocabulary(code) }

// Exposed implements capability.Capability (TUI ring surface).
func (Cap) Exposed(code string) []core.Label { return Exposed(code) }
```

`internal/capability/contextmap/vocabulary.go` — same refactor. Hoist the existing `want` list (5 labels + 4 kind labels, built with `LabelSuperseded`/`LabelProvenance`/`LabelContextKind`/`BoardCurrent` and the existing description literals — copy them verbatim from the current `EnsureVocabulary`) into:

```go
// vocabulary is the single literal list every contract method derives from.
func vocabulary(code string) []core.Label {
	out := []core.Label{
		{Name: code + ":context:*", Description: "index tasks whose description is the payload: agent directions, repos, docs, questions"},
		{Name: code + ":knowledge:*", Description: "lifecycle of a piece of recorded knowledge; absence means current"},
		{Name: LabelSuperseded(code), Description: "this context pointer is obsolete; its successor is named in the description. Kept for history -- it retains its kind, narrative, and provenance stamps. Applied by `atm capability contextmap supersede`."},
		{Name: LabelProvenance(code), Description: "task comment kind: a machine-written provenance stamp recording what a context pointer was derived from, and the evidence, at a moment in time. Written and read only by `atm capability contextmap` -- do not hand-edit."},
		{Name: BoardCurrent(code), Description: "every context pointer that has not been superseded: the project's current knowledge. Agents read this board rather than the raw context:* namespace, so a query always returns the latest.", Expr: currentExpr()},
	}
	kindDesc := map[string]string{
		"agent":         "the task's description captures agent-direction notes for this project: build/test/lint commands, conventions, and gotchas a working agent must know",
		"repository":    "the task's description names a code repository (path or URL), what it contains, and how to work in it; a later agent reads this to orient",
		"documentation": "the task's description points at a specific document (file path or URL) and summarizes what it covers, so a later agent can decide whether to read it",
		"question":      "the task's description poses an open question or ambiguity about the project that a human or later agent should clarify; not a defect, not a work item, a gap in understanding",
	}
	for _, kind := range ContextKinds {
		out = append(out, core.Label{Name: LabelContextKind(code, kind), Description: kindDesc[kind]})
	}
	return out
}

// Vocabulary returns every label this capability owns for code. Pure.
func Vocabulary(code string) []core.Label { return vocabulary(code) }

// Exposed returns the ring surface: exactly the context-current board. The
// context:*/knowledge:* namespaces are deliberately not exposed — they are
// capability bookkeeping, not browsing surfaces.
func Exposed(code string) []core.Label {
	for _, l := range vocabulary(code) {
		if l.Name == BoardCurrent(code) {
			return []core.Label{l}
		}
	}
	return nil
}
```

and reduce `EnsureVocabulary` to the same seed-loop shape as workflow's. Add the same two `Cap` methods in `internal/capability/contextmap/command.go`.

Update the two fakes so the package compiles — in `internal/capability/capability_test.go` add fields + methods to `fakeCap`:

```go
	vocab   []core.Label
	exposed []core.Label
```
```go
func (f *fakeCap) Vocabulary(code string) []core.Label { return f.vocab }
func (f *fakeCap) Exposed(code string) []core.Label    { return f.exposed }
```

and identically for `fakeMountCap` in `internal/cli/env_test.go` (returning nil slices is fine there).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/capability/... ./internal/cli/ ./internal/tui/`
Expected: PASS (existing vocabulary tests still green — seeding behavior and board return are unchanged).

- [ ] **Step 5: Commit + journal**

```bash
git add internal/capability internal/cli/env_test.go
git commit -m "feat(ATM-87b3ae): capability contract gains pure Vocabulary + Exposed"
atm task comment add --task ATM-87b3ae --actor developer@claude:fable-5 --label ATM:comment:progress --body "Task 1: Vocabulary/Exposed added to the capability contract; workflow + contextmap refactored onto one literal list; EnsureVocabulary seeds exactly Vocabulary."
```

---

### Task 2: Registry surface — `Exposed`, `Unmanaged`, umbrella sentinel, `OrderFullNames`

**Files:**
- Modify: `internal/capability/capability.go`
- Test: `internal/capability/capability_test.go`

**Interfaces:**
- Consumes: Task 1's `Capability.Vocabulary/Exposed`.
- Produces (Tasks 4, 8, 9 depend on these exact signatures):
  - `type ExposedLabel struct { Label core.Label; Owner string }`
  - `func (r *Registry) Exposed(code string) []ExposedLabel`
  - `func (r *Registry) Unmanaged(svc core.LabelService, code string) ([]core.Label, error)`
  - `func UmbrellaFullName(code string) string` (returns `code + ":unmanaged"`)
  - `func OrderFullNames(effective, override []string) []string`

- [ ] **Step 1: Write the failing tests**

Append to `internal/capability/capability_test.go` (extend the existing `fakeCap`; add a tiny label-service fake):

```go
// listOnlyService is a core.LabelService whose only live method is LabelList.
type listOnlyService struct{ labels []core.Label }

func (s *listOnlyService) LabelList(project, namespace string) []core.Label { return s.labels }
func (s *listOnlyService) LabelAdd(name, description, expr, actor string) error  { return nil }
func (s *listOnlyService) LabelSeed(name, description, expr, actor string) error { return nil }
func (s *listOnlyService) LabelShow(name string) (core.Label, error)             { return core.Label{}, nil }
func (s *listOnlyService) LabelRemove(name, actor string) (*core.LabelRemoveResult, error) {
	return nil, nil
}
func (s *listOnlyService) LabelUsageGrouped(projectCode string) (map[string]int, error) {
	return nil, nil
}

func TestRegistryExposedTagsOwnerInRegistrationOrder(t *testing.T) {
	var calls []string
	reg := NewRegistry(
		&fakeCap{name: "workflow", calls: &calls, exposed: []core.Label{
			{Name: "ATM:all-tasks", Expr: "*"}, {Name: "ATM:status:*"},
		}},
		&fakeCap{name: "contextmap", calls: &calls, exposed: []core.Label{
			{Name: "ATM:context-current", Expr: "context:*"},
		}},
	)
	got := reg.Exposed("ATM")
	want := []ExposedLabel{
		{Label: core.Label{Name: "ATM:all-tasks", Expr: "*"}, Owner: "workflow"},
		{Label: core.Label{Name: "ATM:status:*"}, Owner: "workflow"},
		{Label: core.Label{Name: "ATM:context-current", Expr: "context:*"}, Owner: "contextmap"},
	}
	if len(got) != len(want) {
		t.Fatalf("Exposed = %+v, want %+v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Exposed[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	var nilReg *Registry
	if nilReg.Exposed("ATM") != nil {
		t.Error("nil registry must return nil")
	}
}

// TestRegistryUnmanagedSubtractsOwnership is the core diff: owned = vocabulary
// FullNames + members under owned :* descriptors. Capability-internal labels
// (status:open) and ad-hoc members of an owned namespace (status:wip) are
// managed; loose tags, unowned namespaces, and leftover descriptors are not.
func TestRegistryUnmanagedSubtractsOwnership(t *testing.T) {
	var calls []string
	wf := &fakeCap{name: "workflow", calls: &calls, vocab: []core.Label{
		{Name: "ATM:status:*"}, {Name: "ATM:status:open"},
		{Name: "ATM:all-tasks", Expr: "*"},
	}}
	svc := &listOnlyService{labels: []core.Label{
		{Name: "ATM:all-tasks", Expr: "*"},      // owned board
		{Name: "ATM:status:*"},                  // owned descriptor
		{Name: "ATM:status:open"},               // owned member (exact)
		{Name: "ATM:status:wip"},                // ad-hoc member of owned ns -> managed
		{Name: "ATM:type:bug"},                  // unowned ns member -> unmanaged
		{Name: "ATM:type:*"},                    // unowned descriptor -> unmanaged
		{Name: "ATM:urgent"},                    // loose tag -> unmanaged
		{Name: "ATM:my-board", Expr: "urgent"},  // user board -> unmanaged
	}}
	reg := NewRegistry(wf)
	got, err := reg.Unmanaged(svc, "ATM")
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"ATM:type:bug", "ATM:type:*", "ATM:urgent", "ATM:my-board"}
	if len(got) != len(wantNames) {
		t.Fatalf("Unmanaged = %+v, want names %v", got, wantNames)
	}
	for i, l := range got {
		if l.Name != wantNames[i] {
			t.Errorf("Unmanaged[%d] = %s, want %s", i, l.Name, wantNames[i])
		}
	}
	// Repeated calls are stable (pure read).
	again, _ := reg.Unmanaged(svc, "ATM")
	if len(again) != len(got) {
		t.Errorf("second call = %d labels, want %d", len(again), len(got))
	}
}

func TestRegistryUnmanagedEmptyWhenAllOwned(t *testing.T) {
	var calls []string
	wf := &fakeCap{name: "workflow", calls: &calls, vocab: []core.Label{{Name: "ATM:status:*"}}}
	svc := &listOnlyService{labels: []core.Label{{Name: "ATM:status:*"}, {Name: "ATM:status:open"}}}
	got, err := NewRegistry(wf).Unmanaged(svc, "ATM")
	if err != nil || len(got) != 0 {
		t.Fatalf("Unmanaged = (%v, %v), want empty", got, err)
	}
	// Disabled capability (empty registry): everything is unmanaged.
	all, _ := NewRegistry().Unmanaged(svc, "ATM")
	if len(all) != 2 {
		t.Fatalf("empty registry Unmanaged = %v, want both labels", all)
	}
}

func TestOrderFullNames(t *testing.T) {
	effective := []string{"a", "b", "c", "d"}
	got := OrderFullNames(effective, []string{"c", "zzz-stale", "a"})
	want := []string{"c", "a", "b", "d"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("OrderFullNames = %v, want %v", got, want)
		}
	}
	if len(OrderFullNames(nil, []string{"x"})) != 0 {
		t.Error("empty effective must return empty")
	}
	if g := OrderFullNames(effective, nil); g[0] != "a" || g[3] != "d" {
		t.Errorf("no override must keep original order, got %v", g)
	}
}

func TestUmbrellaFullName(t *testing.T) {
	if UmbrellaFullName("ATM") != "ATM:unmanaged" {
		t.Fatalf("UmbrellaFullName = %q", UmbrellaFullName("ATM"))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/ 2>&1 | head -10`
Expected: FAIL — `undefined: ExposedLabel`, `undefined: OrderFullNames`, etc.

- [ ] **Step 3: Implement**

Append to `internal/capability/capability.go` (add `"strings"` to imports):

```go
// UmbrellaFullName is the synthetic "unmanaged" umbrella row's identifier for
// a project: a TUI/CLI sentinel, never a real label. It is used only as a ring
// row FullName and as an order/hidden key in the project's boards config.
func UmbrellaFullName(code string) string { return code + ":unmanaged" }

// ExposedLabel is one ring entry: a capability-surfaced label tagged with the
// owning capability's name (rendered as the muted owner column in the TUI).
type ExposedLabel struct {
	Label core.Label
	Owner string
}

// Exposed enumerates every registered capability's exposed labels in
// registration order (each capability's own preferred order preserved
// within its block), tagged with the owner name.
func (r *Registry) Exposed(code string) []ExposedLabel {
	if r == nil {
		return nil
	}
	var out []ExposedLabel
	for _, c := range r.caps {
		for _, l := range c.Exposed(code) {
			out = append(out, ExposedLabel{Label: l, Owner: c.Name()})
		}
	}
	return out
}

// Unmanaged returns labels in the project's LabelList that no registered
// capability owns via Vocabulary. A label is owned when its FullName is in
// the vocabulary union, or when it sits under an owned namespace descriptor
// (<code>:<ns>:<value> with <code>:<ns>:* owned). Derived, not stored. The
// TUI renders these under the synthetic umbrella row; `atm capability
// unmanaged` exposes the same set to the manager agent for triage. Callers
// narrow to the enabled set first: reg.For(project).Unmanaged(...).
func (r *Registry) Unmanaged(svc core.LabelService, code string) ([]core.Label, error) {
	owned := map[string]bool{}
	var ownedPrefixes []string
	if r != nil {
		for _, c := range r.caps {
			for _, l := range c.Vocabulary(code) {
				owned[l.Name] = true
				if core.IsNamespaceName(l.Name) {
					// "<code>:<ns>:*" -> member prefix "<code>:<ns>:"
					ownedPrefixes = append(ownedPrefixes, strings.TrimSuffix(l.Name, "*"))
				}
			}
		}
	}
	var out []core.Label
	for _, l := range svc.LabelList(code, "") {
		if owned[l.Name] {
			continue
		}
		member := false
		for _, p := range ownedPrefixes {
			if strings.HasPrefix(l.Name, p) {
				member = true
				break
			}
		}
		if !member {
			out = append(out, l)
		}
	}
	return out, nil
}

// OrderFullNames applies a partial order override to an effective ring order:
// override names present in effective come first (override order, duplicates
// dropped), then every remaining effective name in its original order.
// Override entries naming nothing in effective are silently ignored —
// defensive against typos and stale entries after a capability is disabled.
func OrderFullNames(effective, override []string) []string {
	present := make(map[string]bool, len(effective))
	for _, n := range effective {
		present[n] = true
	}
	out := make([]string, 0, len(effective))
	taken := make(map[string]bool, len(effective))
	for _, n := range override {
		if present[n] && !taken[n] {
			out = append(out, n)
			taken[n] = true
		}
	}
	for _, n := range effective {
		if !taken[n] {
			out = append(out, n)
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/...`
Expected: PASS.

- [ ] **Step 5: Commit + journal**

```bash
git add internal/capability
git commit -m "feat(ATM-87b3ae): Registry.Exposed/Unmanaged, umbrella sentinel, OrderFullNames"
atm task comment add --task ATM-87b3ae --actor developer@claude:fable-5 --label ATM:comment:progress --body "Task 2: registry derived views landed — owner-tagged Exposed enumeration, ownership-based Unmanaged diff (vocab FullNames + owned :* prefixes), umbrella sentinel, OrderFullNames partial-override helper."
```

---

### Task 3: core/store — `BoardsConfig`, `GetBoardsConfig` (lazy pins fold-in), `SetProjectBoards`

**Files:**
- Modify: `internal/core/config.go` (add `BoardsConfig`, `MaxBoardPins`; `ProjectConfig.Boards`)
- Modify: `internal/core/service.go` (`ProjectService` gains the two methods; `PinService` stays until Task 7)
- Modify: `internal/store/config.go` (two new funcs; emptiness check)
- Test: `internal/store/config_test.go`

**Interfaces:**
- Consumes: existing `GetProjectConfig`, `WithLock`, `WriteFileAtomic`, `validateActor`, and (until Task 7) `Store.GetPins`.
- Produces (Tasks 4, 6, 9 depend on):
  - `core.BoardsConfig{Order, Hidden, Pins []string}` (json tags `order,omitempty`/`hidden,omitempty`/`pins,omitempty`)
  - `const core.MaxBoardPins = 3`
  - `GetBoardsConfig(code string) (*core.BoardsConfig, error)` — never returns a nil config on success
  - `SetProjectBoards(code string, b *core.BoardsConfig, actor string) error`

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/config_test.go`:

```go
func TestBoardsConfigRoundTripAndMerge(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	// Absent config: empty (non-nil) BoardsConfig.
	b, err := s.GetBoardsConfig("ATM")
	if err != nil || b == nil {
		t.Fatalf("GetBoardsConfig absent = (%v, %v), want empty non-nil", b, err)
	}
	if len(b.Order) != 0 || len(b.Hidden) != 0 || len(b.Pins) != 0 {
		t.Fatalf("absent config not empty: %+v", b)
	}
	want := &core.BoardsConfig{
		Order:  []string{"ATM:all-tasks", "ATM:unmanaged"},
		Hidden: []string{"ATM:context-current"},
		Pins:   []string{"ATM:all-tasks"},
	}
	if err := s.SetProjectBoards("ATM", want, testActor); err != nil {
		t.Fatalf("SetProjectBoards: %v", err)
	}
	got, err := s.GetBoardsConfig("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if got.Order[0] != "ATM:all-tasks" || got.Hidden[0] != "ATM:context-current" || got.Pins[0] != "ATM:all-tasks" {
		t.Errorf("round-trip = %+v, want %+v", got, want)
	}
	// Boards write preserves other config fields.
	cfg := EmbeddingConfig{Model: "m1", Endpoint: "http://x", Dim: 4, Threshold: 0.5}
	if err := s.SetEmbeddingConfig("ATM", cfg, testActor); err != nil {
		t.Fatal(err)
	}
	pc, _ := s.GetProjectConfig("ATM")
	if pc.Boards == nil || pc.Embedding == nil {
		t.Errorf("config lost a field after both writes: %+v", pc)
	}
}

// TestGetProjectConfigBoardsOnlyIsNotAbsent guards the emptiness check: a
// config carrying only a boards key must not read as nil.
func TestGetProjectConfigBoardsOnlyIsNotAbsent(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectBoards("ATM", &core.BoardsConfig{Hidden: []string{"ATM:backlog"}}, testActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProjectConfig("ATM")
	if err != nil || got == nil || got.Boards == nil {
		t.Fatalf("GetProjectConfig = (%+v, %v), want non-nil with Boards", got, err)
	}
}

func TestSetProjectBoardsValidation(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectBoards("ATM", nil, testActor); !core.IsUsage(err) {
		t.Errorf("nil boards: err = %v, want usage", err)
	}
	four := &core.BoardsConfig{Pins: []string{"ATM:a", "ATM:b", "ATM:c", "ATM:d"}}
	if err := s.SetProjectBoards("ATM", four, testActor); !core.IsUsage(err) {
		t.Errorf("4 pins: err = %v, want usage (MaxBoardPins=3)", err)
	}
	if err := s.SetProjectBoards("ATM", &core.BoardsConfig{}, ""); !core.IsUsage(err) {
		t.Errorf("missing actor: err = %v, want usage", err)
	}
}

// TestGetBoardsConfigFoldsLegacyPins is the migration read: boards nil +
// pins.json present -> Pins folded in (capped at 3); first SetProjectBoards
// persists, after which pins.json is ignored.
func TestGetBoardsConfigFoldsLegacyPins(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	legacy := &Pins{Actor: testActor, Boards: []string{"ATM:open-tasks", "ATM:backlog"}}
	if err := s.WritePins("ATM", legacy); err != nil {
		t.Fatal(err)
	}
	b, err := s.GetBoardsConfig("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Pins) != 2 || b.Pins[0] != "ATM:open-tasks" {
		t.Fatalf("legacy fold-in = %+v, want the pins.json boards", b.Pins)
	}
	// Persist with different pins; pins.json is now dead.
	if err := s.SetProjectBoards("ATM", &core.BoardsConfig{Pins: []string{"ATM:all-tasks"}}, testActor); err != nil {
		t.Fatal(err)
	}
	b2, _ := s.GetBoardsConfig("ATM")
	if len(b2.Pins) != 1 || b2.Pins[0] != "ATM:all-tasks" {
		t.Fatalf("after persist = %+v, pins.json must be ignored", b2.Pins)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'BoardsConfig|ProjectBoards' 2>&1 | head -10`
Expected: FAIL — `undefined: core.BoardsConfig` / method undefined.

- [ ] **Step 3: Implement**

`internal/core/config.go` — after `EmbeddingConfig`:

```go
// MaxBoardPins caps the pinned boards per project (Shift-1..3 slots).
const MaxBoardPins = 3

// BoardsConfig is the per-project boards display preference set, stored under
// config.json's "boards" key. Display preference, not substrate state: no
// event-log entry, and entries naming boards that don't exist are ignored by
// readers (defensive against typos and disabled capabilities).
type BoardsConfig struct {
	Order  []string `json:"order,omitempty"`  // ring order override (partial, FullName list)
	Hidden []string `json:"hidden,omitempty"` // hidden FullNames
	Pins   []string `json:"pins,omitempty"`   // pin-slot FullNames (max MaxBoardPins)
}
```

`ProjectConfig` gains:

```go
	Boards    *BoardsConfig     `json:"boards,omitempty"`
```

`internal/core/service.go` — extend `ProjectService` (after `GetProjectConfig`):

```go
	GetBoardsConfig(code string) (*BoardsConfig, error)
	SetProjectBoards(code string, b *BoardsConfig, actor string) error
```

`internal/store/config.go` — change the emptiness check in `GetProjectConfig` to:

```go
	if c.Embedding == nil && c.UpdatedAt == "" && len(c.Remotes) == 0 && c.Boards == nil {
		return nil, nil
	}
```

and append:

```go
// GetBoardsConfig returns the project's boards display preferences, never nil
// on success. While config.json carries no boards key, a legacy pins.json is
// folded into Pins — the read half of the pins.json migration. The merged
// value is persisted by the first SetProjectBoards write, after which
// config.json.boards is non-nil and pins.json is ignored forever. A malformed
// pins.json is treated as absent: display preferences are not worth failing a
// read over.
func (s *Store) GetBoardsConfig(code string) (*core.BoardsConfig, error) {
	c, err := s.GetProjectConfig(code)
	if err != nil {
		return nil, err
	}
	if c != nil && c.Boards != nil {
		return c.Boards, nil
	}
	b := &core.BoardsConfig{}
	if p, err := s.GetPins(code); err == nil && p != nil {
		b.Pins = p.Boards
		if len(b.Pins) > core.MaxBoardPins {
			b.Pins = b.Pins[:core.MaxBoardPins]
		}
	}
	return b, nil
}

// SetProjectBoards writes the project's boards display preferences under the
// project lock, read-modify-write like SetEmbeddingConfig, refreshing the
// updated_at/updated_by stamps. Enforces the MaxBoardPins cap. No store
// event: display preferences are config, not substrate state.
func (s *Store) SetProjectBoards(code string, b *core.BoardsConfig, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if b == nil || len(b.Pins) > core.MaxBoardPins {
		return core.ErrUsage
	}
	return s.WithLock(code, func() error {
		existing, err := s.GetProjectConfig(code)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		merged := &ProjectConfig{}
		if existing != nil {
			merged = existing
		}
		merged.Boards = b
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Commit + journal**

```bash
git add internal/core internal/store
git commit -m "feat(ATM-87b3ae): config.json boards preferences with lazy pins.json fold-in"
atm task comment add --task ATM-87b3ae --actor developer@claude:fable-5 --label ATM:comment:progress --body "Task 3: BoardsConfig on ProjectConfig; GetBoardsConfig folds legacy pins.json until first SetProjectBoards write; max-3 cap enforced at write; emptiness check covers boards-only configs."
```

---

### Task 4: TUI ring rewrite — capability-authored rows, owner column, umbrella row, hidden/order

**Files:**
- Modify: `internal/tui/labels.go` (`boardRow`, `boardsModel`, `refresh`, `buildBoardRows`, `selectDefault`, `applyFocus`, `renderTable`, `boardTableLine`, `togglePin`, `maxPins`)
- Test: `internal/tui/labels_test.go` (new tests + updates), plus updates in `internal/tui/fixedslot_test.go`, `thumbnails_test.go`, `grouped_footer_test.go`, `app_test.go`, `tasks_boards_authoring_test.go`, `board_editor_test.go`, `projects_test.go` where they assume the emergent ring

**Interfaces:**
- Consumes: `Registry.Exposed/Unmanaged` (Task 2), `GetBoardsConfig` (Task 3), existing `m.regFor(code)`.
- Produces: `boardRow.Owner string`, `boardRow.Umbrella bool`, `boardsModel.unmanaged []core.Label`, `boardsModel.boardsCfg *core.BoardsConfig` — Task 5's drill-in and Task 6's pins retarget read these fields.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/labels_test.go`. These use the existing `newTestModel`/`seedProject` helpers; note `newTestModelWithActor` registers only `workflow` — tests that need the ring must seed workflow's vocabulary first (call `m.boards.seedDefaults()` or `workflow.EnsureVocabulary(m.store, "ATM", m.actor)`).

```go
// TestBuildBoardRowsIsCapabilityAuthored: the ring is exactly what enabled
// capabilities expose (registration order) plus the umbrella when unmanaged
// labels exist — emergent namespaces no longer surface at L0.
func TestBuildBoardRowsIsCapabilityAuthored(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	// An ad-hoc namespace member: previously an emergent L0 row, now umbrella-only.
	if err := m.store.LabelAdd("ATM:type:bug", "", "", m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	wantRing := []string{
		"ATM:all-tasks", "ATM:open-tasks", "ATM:in-progress-tasks", "ATM:backlog",
		"ATM:status:*", "ATM:priority:*", "ATM:unmanaged",
	}
	if len(m.boards.rows) != len(wantRing) {
		t.Fatalf("ring = %+v, want %v", m.boards.rows, wantRing)
	}
	for i, r := range m.boards.rows {
		if r.FullName != wantRing[i] {
			t.Errorf("rows[%d] = %s, want %s", i, r.FullName, wantRing[i])
		}
	}
	if own := m.boards.rows[0].Owner; own != "workflow" {
		t.Errorf("all-tasks owner = %q, want workflow", own)
	}
	last := m.boards.rows[len(m.boards.rows)-1]
	if !last.Umbrella || last.Owner != "" || !last.Expandable {
		t.Errorf("umbrella row = %+v, want Umbrella+Expandable, no owner", last)
	}
}

// TestUmbrellaOmittedWhenNoUnmanaged: fully-owned label set -> no umbrella row.
func TestUmbrellaOmittedWhenNoUnmanaged(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	for _, r := range m.boards.rows {
		if r.Umbrella {
			t.Fatalf("umbrella present with no unmanaged labels: %+v", m.boards.rows)
		}
	}
}

// TestUmbrellaSuppressedByRealCollision: a real ATM:unmanaged tag (or
// ATM:unmanaged:* namespace) shadows the sentinel; the real label renders as
// a normal unmanaged label under no umbrella (the sentinel loses).
func TestUmbrellaSuppressedByRealCollision(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.LabelAdd("ATM:unmanaged", "hand-made collision", "", m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	for _, r := range m.boards.rows {
		if r.Umbrella {
			t.Fatalf("sentinel must lose to a real ATM:unmanaged label; ring = %+v", m.boards.rows)
		}
	}
}

// TestHiddenAndOrderApply: hidden rows leave the ring entirely; order is a
// partial override with unmatched rows appended in registration order.
func TestHiddenAndOrderApply(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	err := m.store.SetProjectBoards("ATM", &core.BoardsConfig{
		Order:  []string{"ATM:status:*", "ATM:open-tasks", "ATM:nosuch"},
		Hidden: []string{"ATM:backlog"},
	}, m.actor)
	if err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	want := []string{
		"ATM:status:*", "ATM:open-tasks", // ordered prefix
		"ATM:all-tasks", "ATM:in-progress-tasks", "ATM:priority:*", // rest, registration order
	}
	if len(m.boards.rows) != len(want) {
		t.Fatalf("ring = %+v, want %v", m.boards.rows, want)
	}
	for i, r := range m.boards.rows {
		if r.FullName != want[i] {
			t.Errorf("rows[%d] = %s, want %s", i, r.FullName, want[i])
		}
		if r.FullName == "ATM:backlog" {
			t.Error("hidden board must not appear in the ring")
		}
	}
}

// TestStoredDescriptionWinsOverExposedLiteral: a human-curated label
// description beats the capability's baked-in text.
func TestStoredDescriptionWinsOverExposedLiteral(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.LabelAdd("ATM:all-tasks", "our everything view", "*", m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	for _, r := range m.boards.rows {
		if r.FullName == "ATM:all-tasks" && r.Description != "our everything view" {
			t.Errorf("description = %q, want the stored (curated) one", r.Description)
		}
	}
}

// TestSelectDefaultSkipsUmbrella: umbrella is never the default selection;
// selecting it via cycleBoard applies no task filter.
func TestSelectDefaultSkipsUmbrella(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.LabelAdd("ATM:urgent", "", "", m.actor); err != nil { // makes umbrella appear
		t.Fatal(err)
	}
	m.boards.refresh()
	m.boards.selectDefault()
	if m.boards.selected != "ATM:all-tasks" {
		t.Fatalf("selected = %q, want ATM:all-tasks", m.boards.selected)
	}
	// Cycle to the umbrella (last row) and confirm no filter is applied.
	for m.boards.selected != "ATM:unmanaged" {
		m.boards.cycleBoard(1)
	}
	if m.tasks.focus.mode != focusOff || m.tasks.filterToken != "" {
		t.Errorf("umbrella selection must clear the task filter (focus=%v token=%q)", m.tasks.focus, m.tasks.filterToken)
	}
}
```

Adjust the last assertions to the actual `tasksModel` field names for focus/filter (check `setFocus` in `internal/tui/tasks.go` — use whatever fields `setFocus(taskFocus{mode: focusOff}, "")` sets).

Also add imports as needed (`atm/internal/capability/workflow`, `atm/internal/core`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'BuildBoardRowsIsCapabilityAuthored|Umbrella|HiddenAndOrder|StoredDescription|SelectDefaultSkips' 2>&1 | head -20`
Expected: FAIL (compile error on `boardRow.Umbrella`/`Owner`, then behavioral failures).

- [ ] **Step 3: Implement**

In `internal/tui/labels.go`:

(a) `boardRow` gains two fields:

```go
	Owner            string // owning capability name; "" for the umbrella
	Umbrella         bool   // the synthetic unmanaged umbrella row
```

(b) `boardsModel` gains two fields:

```go
	unmanaged []core.Label       // labels no enabled capability owns (umbrella contents)
	boardsCfg *core.BoardsConfig // per-project display preferences, loaded each refresh
```

(c) In `refresh()`, load the config before building rows (after the `projectScope == ""` early return):

```go
	cfg, err := b.m.store.GetBoardsConfig(scope)
	if err != nil || cfg == nil {
		cfg = &core.BoardsConfig{}
	}
	b.boardsCfg = cfg
```

(d) Replace `buildBoardRows` wholesale:

```go
// buildBoardRows constructs the flat L0 ring: every enabled capability's
// Exposed labels in registration order (owner-tagged), plus the synthetic
// "unmanaged" umbrella when any label no capability owns exists. The stored
// label's description wins over the Exposed literal (a human may have curated
// it). Hidden rows are dropped; the boards-config order override is applied
// as a partial reorder (unmatched rows keep registration order, umbrella
// last). The old emergent derivation lives on only inside the umbrella's
// drill-in (buildUmbrellaRows).
func (b *boardsModel) buildBoardRows(ls []core.Label) []boardRow {
	scope := b.m.projectScope
	stored := map[string]core.Label{}
	for _, l := range ls {
		stored[l.Name] = l
	}
	reg := b.m.regFor(scope)
	var out []boardRow
	seen := map[string]bool{}
	for _, e := range reg.Exposed(scope) {
		l := e.Label
		if seen[l.Name] {
			continue
		}
		seen[l.Name] = true
		if s, ok := stored[l.Name]; ok && s.Description != "" {
			l.Description = s.Description
		}
		suffix := strings.TrimPrefix(l.Name, scope+":")
		if core.IsNamespaceName(l.Name) {
			ns := strings.TrimSuffix(suffix, ":*")
			out = append(out, boardRow{
				Name:             ns,
				FullName:         l.Name,
				Description:      l.Description,
				Count:            b.namespaceTaskCount(ns),
				Expandable:       true,
				NeedsDescription: l.Description == "",
				Owner:            e.Owner,
			})
			continue
		}
		count, broken := b.boardCount(l.Name)
		out = append(out, boardRow{
			Name:        suffix,
			FullName:    l.Name,
			Description: l.Description,
			Expr:        l.Expr,
			Count:       count,
			Broken:      broken,
			Owner:       e.Owner,
		})
	}
	// Umbrella: present only when unmanaged labels exist AND no real label
	// shadows the sentinel (a hand-made ATM:unmanaged tag or ATM:unmanaged:*
	// namespace wins; it then renders as a normal unmanaged label instead).
	unmanaged, _ := reg.Unmanaged(b.m.store, scope)
	b.unmanaged = unmanaged
	sentinel := capability.UmbrellaFullName(scope)
	_, tagShadow := stored[sentinel]
	_, nsShadow := stored[sentinel+":*"]
	if len(unmanaged) > 0 && !tagShadow && !nsShadow {
		out = append(out, boardRow{
			Name:        "unmanaged",
			FullName:    sentinel,
			Description: "labels no capability owns; drill in to browse, triage via atm capability unmanaged",
			Count:       len(unmanaged),
			Expandable:  true,
			Umbrella:    true,
		})
	}
	// Hidden filter, then partial order override.
	hidden := map[string]bool{}
	for _, n := range b.boardsCfg.Hidden {
		hidden[n] = true
	}
	kept := out[:0:0]
	for _, r := range out {
		if !hidden[r.FullName] {
			kept = append(kept, r)
		}
	}
	out = kept
	if len(b.boardsCfg.Order) > 0 {
		effective := make([]string, len(out))
		for i, r := range out {
			effective[i] = r.FullName
		}
		pos := map[string]int{}
		for i, n := range capability.OrderFullNames(effective, b.boardsCfg.Order) {
			pos[n] = i
		}
		sort.SliceStable(out, func(i, j int) bool { return pos[out[i].FullName] < pos[out[j].FullName] })
	}
	return out
}
```

Add `"atm/internal/capability"` to the imports. Delete the now-unused emergent-namespace block (it moves to Task 5's `buildUmbrellaRows`; if the compiler flags unused helpers in between tasks, keep `namespaceTaskCount`/`boardCount` — both are still used).

(e) `selectDefault` — replace the fallback branch so the umbrella is skipped:

```go
	if len(b.rows) > 0 {
		for _, r := range b.rows {
			if r.Umbrella {
				continue
			}
			b.selected = r.FullName
			b.applyFocus()
			return
		}
	}
	b.selected = ""
	b.applyFocus()
```

(f) `applyFocus` — add before the `r.Expandable` branch:

```go
	if r.Umbrella {
		// The umbrella is not a filter: ATM:unmanaged is a sentinel, not a
		// label. Selecting it shows the whole project.
		b.m.tasks.setFocus(taskFocus{mode: focusOff}, "")
		return
	}
```

(g) `togglePin` — refuse the umbrella (after the empty-scope guard):

```go
	if idx := b.ringIndex(); idx >= 0 && b.rows[idx].Umbrella {
		return // the sentinel is not a pinnable board
	}
```

(h) Owner column. Change `boardTableLine` to take four columns:

```go
// boardTableLine renders one row of the flat Boards list: fixed-width name,
// flexible description, a narrow muted owner tag, and an 8-wide count.
// Padding is by display width (lipgloss.Width), not byte length.
func boardTableLine(width int, name, description, owner, count string) string {
	nameW := 16
	ownerW := 10
	countW := 8
	// leading space (1) + 3 inter-column separators (3) = 4
	descW := width - nameW - ownerW - countW - 4
	if descW < 8 {
		descW = 8
	}
	namePad := nameW - lipgloss.Width(name)
	if namePad < 0 {
		namePad = 0
	}
	desc := truncateRunes(description, descW)
	descPad := descW - lipgloss.Width(desc)
	if descPad < 0 {
		descPad = 0
	}
	ownerCell := truncateRunes(owner, ownerW)
	ownerPad := ownerW - lipgloss.Width(ownerCell)
	if ownerPad < 0 {
		ownerPad = 0
	}
	return fitLine(fmt.Sprintf(" %s%s %s%s %s%s %8s", name, spaces(namePad), desc, spaces(descPad), ownerCell, spaces(ownerPad), count), width)
}
```

In `renderTable`, the header becomes `boardTableLine(b.width, "BOARD", "DESCRIPTION", "OWNER", "COUNT")` and each row passes the owner:

```go
		owner := r.Owner
		if owner == "" {
			owner = "—"
		}
		line := boardTableLine(b.width, name, r.Description, b.m.styles.Muted.Render(owner), count)
```

(i) `maxPins` — replace the const value with the shared cap, keeping the doc comment:

```go
const maxPins = core.MaxBoardPins
```

- [ ] **Step 4: Update the existing TUI tests that assumed the emergent ring**

Run `go test ./internal/tui/ 2>&1 | head -50` and fix failures by these rules (do not weaken assertions — retarget them):

1. **Tests that created ad-hoc labels (`ATM:type:bug`, custom `ATM:board-XX` Expr labels) and expected them as L0 rows** now find them only via the umbrella (Task 5) — at this task, assert they are *absent* from `b.rows` and present in `b.unmanaged`. If a test's real subject was chart/detail behavior, seed workflow's vocabulary and use `status`-namespace labels instead (owned, still chartable via the ring's `status:*` row).
2. **Tests that asserted alphabetical L0 order** now assert capability-registration order (workflow's `Exposed` order: all-tasks, open-tasks, in-progress-tasks, backlog, status, priority).
3. **Pin tests that pinned custom `ATM:board-XX` labels** (e.g. `TestListContentHeightConstantAcrossPins` in `fixedslot_test.go`): seed workflow's vocabulary and pin exposed boards (`ATM:all-tasks`, `ATM:open-tasks`, `ATM:in-progress-tasks`) instead — custom boards are no longer ring rows, so they are no longer pin candidates.
4. **Tests calling `boardTableLine`** with the old 3-column signature: add the owner argument.
5. Any test constructing a model that needs ring rows must seed first: `workflow.EnsureVocabulary(m.store, "ATM", m.actor)` (registry-only exposure renders rows regardless, but their counts read broken until the labels exist — seed to keep assertions on Count/Broken meaningful).

- [ ] **Step 5: Run the full TUI package**

Run: `go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 6: Commit + journal**

```bash
git add internal/tui
git commit -m "feat(ATM-87b3ae): capability-authored Boards ring with owner column, umbrella row, hidden/order prefs"
atm task comment add --task ATM-87b3ae --actor developer@claude:fable-5 --label ATM:comment:progress --body "Task 4: TUI ring is now Exposed-driven with owner tags; umbrella row appears when unmanaged labels exist (sentinel shadow guard in place); hidden/order from config.json.boards applied; selectDefault/applyFocus/togglePin umbrella-aware. Existing tests retargeted."
```

---

### Task 5: TUI umbrella drill-in — sub-table level

**Files:**
- Modify: `internal/tui/labels.go` (`lLevel` enum, `boardsModel`, `drillIn`, `drillOut`, `focusCenter`, `handleKey`, `handleTableKey`, `enterTable`, `View`, `statusHint`; new `buildUmbrellaRows`, `enterUmbrella`, `handleUmbrellaKey`, `renderUmbrella`)
- Test: `internal/tui/labels_test.go`

**Interfaces:**
- Consumes: `boardsModel.unmanaged` (Task 4), existing `enterChart`/`enterDetail`/`chartRows`.
- Produces: `lLevelUmbrella` level; `boardsModel.umbrellaRows []boardRow`; `boardsModel.fromUmbrella bool` (chart/detail Esc returns to the sub-table when true).

- [ ] **Step 1: Write the failing tests**

```go
// TestUmbrellaDrillInShowsEmergentSubTable: drilling into the umbrella lists
// the OLD emergent derivation scoped to unmanaged labels only — namespace
// rows for unmanaged prefixes, plus loose labels/boards — and drilling a
// namespace row from there opens the normal chart, with Esc climbing back
// chart -> sub-table -> ring.
func TestUmbrellaDrillInShowsEmergentSubTable(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	for _, l := range []struct{ name, expr string }{
		{"ATM:type:bug", ""}, {"ATM:type:feature", ""}, {"ATM:urgent", ""}, {"ATM:my-board", "urgent"},
	} {
		if err := m.store.LabelAdd(l.name, "", l.expr, m.actor); err != nil {
			t.Fatal(err)
		}
	}
	m.boards.refresh()
	m.boards.selected = "ATM:unmanaged"
	m.boards.drillIn()
	if m.boards.level != lLevelUmbrella {
		t.Fatalf("level = %v, want lLevelUmbrella", m.boards.level)
	}
	// Emergent scoped derivation: my-board (board), type (namespace), urgent (loose tag).
	var names []string
	for _, r := range m.boards.umbrellaRows {
		names = append(names, r.Name)
	}
	want := []string{"my-board", "type", "urgent"}
	if len(names) != len(want) {
		t.Fatalf("umbrellaRows = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("umbrellaRows[%d] = %s, want %s", i, names[i], want[i])
		}
	}
	// Drill the "type" namespace row -> chart; Esc -> back to sub-table.
	for i, r := range m.boards.umbrellaRows {
		if r.Name == "type" {
			m.boards.cursor = i
		}
	}
	m.boards.handleUmbrellaKey(keyMsg("enter"))
	if m.boards.level != lLevelChart || m.boards.ns != "type" {
		t.Fatalf("enter on namespace row: level=%v ns=%q, want chart/type", m.boards.level, m.boards.ns)
	}
	m.boards.handleChartKey(keyMsg("esc"))
	if m.boards.level != lLevelUmbrella {
		t.Fatalf("esc from umbrella-entered chart: level = %v, want lLevelUmbrella", m.boards.level)
	}
	m.boards.handleUmbrellaKey(keyMsg("esc"))
	if m.boards.level != lLevelTable {
		t.Fatalf("esc from sub-table: level = %v, want lLevelTable", m.boards.level)
	}
}

// TestChartEscOutsideUmbrellaStillReturnsToTable guards the fromUmbrella flag:
// a chart entered from the ring (status:*) still Esc's to L0.
func TestChartEscOutsideUmbrellaStillReturnsToTable(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	m.boards.selected = "ATM:status:*"
	m.boards.drillIn()
	if m.boards.level != lLevelChart {
		t.Fatalf("level = %v, want chart", m.boards.level)
	}
	m.boards.handleChartKey(keyMsg("esc"))
	if m.boards.level != lLevelTable {
		t.Fatalf("esc = %v, want lLevelTable", m.boards.level)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'UmbrellaDrillIn|ChartEscOutside' 2>&1 | head -10`
Expected: FAIL — `undefined: lLevelUmbrella`.

- [ ] **Step 3: Implement**

(a) Extend the enum (order matters for nothing else; append):

```go
const (
	lLevelTable    lLevel = iota // L0 flat boards list
	lLevelChart                  // L1 per-namespace chart
	lLevelDetail                 // L2 label detail (or unset leaf)
	lLevelUmbrella               // L0.5: the umbrella's unmanaged sub-table
)
```

(b) `boardsModel` gains:

```go
	umbrellaRows []boardRow // emergent rows over unmanaged labels (umbrella drill-in)
	fromUmbrella bool       // current chart/detail was entered from the umbrella sub-table
```

(c) `buildUmbrellaRows` — the old emergent derivation, scoped to `b.unmanaged` (this is the code deleted from `buildBoardRows` in Task 4, with `ls` := `b.unmanaged`):

```go
// buildUmbrellaRows runs the pre-v2 emergent derivation over ONLY the
// unmanaged labels: every unmanaged label with an Expr is a board row, every
// unmanaged <ns>:<value> or <ns>:* introduces a namespace row. Sorted by
// display name, exactly like the old L0.
func (b *boardsModel) buildUmbrellaRows() []boardRow {
	scope := b.m.projectScope
	byName := map[string]core.Label{}
	for _, l := range b.unmanaged {
		byName[l.Name] = l
	}
	var out []boardRow
	seen := map[string]bool{}
	for _, l := range b.unmanaged {
		if l.Expr == "" {
			continue
		}
		name := strings.TrimPrefix(l.Name, scope+":")
		count, broken := b.boardCount(l.Name)
		seen[name] = true
		out = append(out, boardRow{
			Name: name, FullName: l.Name, Description: l.Description,
			Expr: l.Expr, Count: count, Broken: broken,
		})
	}
	nsOrder := []string{}
	nsSeen := map[string]bool{}
	for _, l := range b.unmanaged {
		suffix := strings.TrimPrefix(l.Name, scope+":")
		if core.IsNamespaceName(l.Name) {
			ns := strings.TrimSuffix(suffix, ":*")
			if !nsSeen[ns] {
				nsSeen[ns] = true
				nsOrder = append(nsOrder, ns)
			}
			continue
		}
		parts := strings.SplitN(suffix, ":", 2)
		if len(parts) == 2 {
			if ns := parts[0]; !nsSeen[ns] {
				nsSeen[ns] = true
				nsOrder = append(nsOrder, ns)
			}
		}
	}
	sort.Strings(nsOrder)
	for _, ns := range nsOrder {
		if seen[ns] {
			continue
		}
		descFull := scope + ":" + ns + ":*"
		descLabel, hasDesc := byName[descFull]
		desc := ""
		needsDesc := true
		if hasDesc && descLabel.Description != "" {
			desc = descLabel.Description
			needsDesc = false
		}
		out = append(out, boardRow{
			Name: ns, FullName: descFull, Description: desc,
			Count: b.namespaceTaskCount(ns), Expandable: true, NeedsDescription: needsDesc,
		})
	}
	// Loose tags (no namespace, no Expr): browsable as leaf details.
	for _, l := range b.unmanaged {
		suffix := strings.TrimPrefix(l.Name, scope+":")
		if l.Expr != "" || core.IsNamespaceName(l.Name) || strings.Contains(suffix, ":") {
			continue
		}
		usage, _ := b.m.store.LabelUsageGrouped(scope)
		out = append(out, boardRow{
			Name: suffix, FullName: l.Name, Description: l.Description,
			Count: usage[l.Name], NeedsDescription: l.Description == "",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
```

(Hoist the `LabelUsageGrouped` call out of the loop — call it once before the loose-tag loop.)

Note the old code treated loose tags as invisible at L0; the umbrella exists to surface *every* unmanaged label, so loose tags get leaf rows here.

(d) Enter/handle/render:

```go
// enterUmbrella opens the unmanaged sub-table. No task-filter change: the
// umbrella is a browsing surface, not a filter.
func (b *boardsModel) enterUmbrella() {
	b.tableCursor = b.cursor
	b.level = lLevelUmbrella
	b.umbrellaRows = b.buildUmbrellaRows()
	b.cursor = 0
	b.offset = 0
}

func (b *boardsModel) handleUmbrellaKey(k tea.KeyMsg) tea.Cmd {
	rows := b.umbrellaRows
	switch k.String() {
	case "j", "down":
		if b.cursor < len(rows)-1 {
			b.cursor++
		}
	case "k", "up":
		if b.cursor > 0 {
			b.cursor--
		}
	case "g":
		b.cursor = 0
	case "]":
		b.cursor += b.pageSize
		if b.cursor > len(rows)-1 {
			b.cursor = len(rows) - 1
		}
		if b.cursor < 0 {
			b.cursor = 0
		}
	case "[":
		b.cursor -= b.pageSize
		if b.cursor < 0 {
			b.cursor = 0
		}
	case "enter":
		if b.cursor < 0 || b.cursor >= len(rows) {
			return nil
		}
		r := rows[b.cursor]
		b.fromUmbrella = true
		if r.Expandable {
			b.enterChart(r.Name)
			return nil
		}
		b.level = lLevelDetail
		b.detail = labelDetailState{row: labelRow{
			suffix:      strings.TrimPrefix(r.FullName, b.m.projectScope+":"),
			full:        r.FullName,
			description: r.Description,
			usage:       r.Count,
		}}
	case "esc":
		b.level = lLevelTable
		b.cursor = b.tableCursor
		b.clampCursor()
	}
	return nil
}

// reenterUmbrella returns from a chart/detail entered via the umbrella.
func (b *boardsModel) reenterUmbrella() {
	b.level = lLevelUmbrella
	b.umbrellaRows = b.buildUmbrellaRows()
	b.cursor = 0
	b.ns = ""
	b.detail = labelDetailState{}
}
```

`renderUmbrella` mirrors `renderTable` over `b.umbrellaRows` (same header via `boardTableLine(b.width, "LABEL", "DESCRIPTION", "OWNER", "COUNT")`, owner cell always `—`, same cursor/window logic). Copy `renderTable`'s body, swap `b.rows` → `b.umbrellaRows`, and title the empty state `"no unmanaged labels"`.

(e) Wire the state machine:

- `handleKey`: add `case lLevelUmbrella: return b.handleUmbrellaKey(k)`.
- `View`: add `case lLevelUmbrella: return b.renderUmbrella()`.
- `drillIn`, `lLevelTable` case — umbrella check first:

```go
		if r.Umbrella {
			b.enterUmbrella()
			return
		}
```

Also add `case lLevelUmbrella:` delegating enter-like behavior only via `handleUmbrellaKey` (drillIn is called from Shift-→ paths; mirror the `enter` branch by extracting it into a helper if the diff is cleaner — acceptable to have drillIn at lLevelUmbrella call the same code as the `enter` case).

- `focusCenter`: change the `Expandable` branch to:

```go
		if r := b.rows[idx]; r.Umbrella {
			b.enterUmbrella()
		} else if r.Expandable {
			b.enterChart(r.Name)
		}
```

- `handleTableKey` `"enter"` case: same guard (`if r.Umbrella { b.enterUmbrella(); return nil }` before the `r.Expandable` check).
- `drillOut` / `handleChartKey` `"esc"` / `handleDetailKey` `"esc"`: when `b.fromUmbrella` is true, return to the sub-table instead of L0/chart:
  - `handleChartKey` esc: `if b.fromUmbrella { b.fromUmbrella = false; b.reenterUmbrella(); b.applyFocus(); return nil }` before `b.enterTable()`.
  - `drillOut` `lLevelChart` case: same guard.
  - `handleDetailKey`/`drillOut` `lLevelDetail`: when `b.ns != ""` keep the existing chart return (the chart will still carry `fromUmbrella`); when `b.ns == "" && b.fromUmbrella` (loose-tag detail), `b.fromUmbrella = false; b.reenterUmbrella()`.
- `resetDrill` and `reset`: add `b.fromUmbrella = false` and `b.umbrellaRows = nil`.
- `statusHint`: add `case lLevelUmbrella: return "[Enter]open [Esc]back"`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 5: Commit + journal**

```bash
git add internal/tui
git commit -m "feat(ATM-87b3ae): umbrella drill-in sub-table over unmanaged labels"
atm task comment add --task ATM-87b3ae --actor developer@claude:fable-5 --label ATM:comment:progress --body "Task 5: umbrella drill-in added as lLevelUmbrella — emergent derivation now lives only there, scoped to unmanaged labels (incl. loose-tag leaf rows); Esc chain chart/detail→sub-table→ring via fromUmbrella."
```

---

### Task 6: TUI pins retarget to `config.json.boards.pins`

**Files:**
- Modify: `internal/tui/labels.go` (`loadPins`, `persistPins`)
- Test: `internal/tui/labels_test.go`, `internal/tui/fixedslot_test.go` (fixtures move from `WritePins` to `SetProjectBoards`)

**Interfaces:**
- Consumes: `GetBoardsConfig`/`SetProjectBoards` (Task 3), `boardsModel.boardsCfg` (Task 4).
- Produces: nothing new — `b.pins []string` semantics unchanged.

- [ ] **Step 1: Write the failing test**

```go
// TestPinsPersistToBoardsConfig: toggling a pin writes config.json.boards.pins
// (not pins.json), preserving Order/Hidden, and loads back on refresh.
func TestPinsPersistToBoardsConfig(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetProjectBoards("ATM", &core.BoardsConfig{Hidden: []string{"ATM:backlog"}}, m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	m.boards.selected = "ATM:all-tasks"
	m.boards.togglePin()
	b, err := m.store.GetBoardsConfig("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Pins) != 1 || b.Pins[0] != "ATM:all-tasks" {
		t.Fatalf("pins = %v, want [ATM:all-tasks]", b.Pins)
	}
	if len(b.Hidden) != 1 || b.Hidden[0] != "ATM:backlog" {
		t.Fatalf("hidden clobbered by pin write: %+v", b)
	}
	m.boards.refresh()
	if len(m.boards.pins) != 1 || m.boards.pins[0] != "ATM:all-tasks" {
		t.Fatalf("pins after refresh = %v", m.boards.pins)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestPinsPersistToBoardsConfig 2>&1 | head`
Expected: FAIL — pins land in `pins.json`, `GetBoardsConfig` still folds them in BUT the Hidden-preservation assertion fails (WritePins doesn't touch config.json, so `b.Hidden` survives — the failing assertion is `b.Pins` staying legacy-only after a later `SetProjectBoards`; if the test passes accidentally via the legacy fold-in, tighten it by asserting `config.json.boards` is non-nil: `pc, _ := m.store.GetProjectConfig("ATM"); pc.Boards must contain the pin`).

- [ ] **Step 3: Implement**

`loadPins` — read from the already-loaded config (drop the `GetPins` call):

```go
// loadPins reads the project's pins from the boards config (legacy pins.json
// folds in via GetBoardsConfig until first write) and prunes any whose board
// is not in the ring. Clamped to maxPins.
func (b *boardsModel) loadPins() {
	b.pins = nil
	if b.m.projectScope == "" || b.boardsCfg == nil {
		return
	}
	live := map[string]bool{}
	for _, r := range b.rows {
		live[r.FullName] = true
	}
	for _, full := range b.boardsCfg.Pins {
		if live[full] {
			b.pins = append(b.pins, full)
		}
		if len(b.pins) >= maxPins {
			break
		}
	}
	b.syncPinFocus()
}
```

`persistPins` — read-modify-write the boards config:

```go
func (b *boardsModel) persistPins() {
	if b.m.projectScope == "" {
		return
	}
	cfg, err := b.m.store.GetBoardsConfig(b.m.projectScope)
	if err != nil || cfg == nil {
		cfg = &core.BoardsConfig{}
	}
	cfg.Pins = b.pins
	_ = b.m.store.SetProjectBoards(b.m.projectScope, cfg, b.m.actor)
	b.boardsCfg = cfg
}
```

- [ ] **Step 4: Update legacy-fixture tests + run**

In `internal/tui/labels_test.go` (lines ~193, ~427, ~516, ~1520) and any other TUI test using `m.store.WritePins(...)`/`m.store.GetPins(...)`: replace fixture writes with `m.store.SetProjectBoards("ATM", &core.BoardsConfig{Pins: boards}, m.actor)` and reads with `m.store.GetBoardsConfig("ATM")`. Keep ONE test exercising the legacy path (write via `WritePins`, assert the TUI still loads the pins) — it proves the fold-in end-to-end and will be retired with Task 7 only if `WritePins` is removed there (it is; that test then switches to writing a raw `pins.json` file with `os.WriteFile` — see Task 7 Step 3).

Run: `go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 5: Commit + journal**

```bash
git add internal/tui
git commit -m "feat(ATM-87b3ae): TUI pins persist to config.json.boards.pins"
atm task comment add --task ATM-87b3ae --actor developer@claude:fable-5 --label ATM:comment:progress --body "Task 6: TUI pin load/persist retargeted to boards config; legacy pins.json still folds in on read; hidden/order preserved across pin writes."
```

---

### Task 7: Retire the pins.json write path

**Files:**
- Delete: `internal/store/pins.go`, `internal/store/pins_test.go`
- Modify: `internal/core/config.go` (remove `Pins` type), `internal/core/service.go` (remove `PinService`, drop it from `Service`), `internal/store/types_compat.go` (remove `Pins` alias), `internal/store/config.go` (inline legacy pins reader)
- Modify: any remaining `GetPins`/`WritePins`/`core.Pins` references in tests (`internal/cli/env_test.go`, `task_test.go`, `store_test.go`, `internal/store/eventsource_views_test.go`, `internal/tui/labels_test.go` legacy test from Task 6)

**Interfaces:**
- Consumes: Task 3's `GetBoardsConfig`.
- Produces: none — pure removal. After this task `grep -rn "GetPins\|WritePins\|core.Pins" internal cmd` returns nothing outside comments.

- [ ] **Step 1: Inline the legacy reader**

In `internal/store/config.go`, replace the `s.GetPins(code)` call inside `GetBoardsConfig` with a private reader — the only code that still knows `pins.json` ever existed. `core.Pins` is being deleted, so read into a local struct:

```go
// legacyPinBoards reads the board list from a pre-boards pins.json, kept
// only for the lazy migration into config.json.boards.pins. Missing or
// malformed reads as nil: display preferences are not worth failing over.
func (s *Store) legacyPinBoards(code string) []string {
	var p struct {
		Boards []string `json:"boards"`
	}
	if err := ReadJSON(filepath.Join(s.projectDir(code), "pins.json"), &p); err != nil {
		return nil
	}
	return p.Boards
}
```

and in `GetBoardsConfig`:

```go
	b := &core.BoardsConfig{}
	if pins := s.legacyPinBoards(code); len(pins) > 0 {
		b.Pins = pins
		if len(b.Pins) > core.MaxBoardPins {
			b.Pins = b.Pins[:core.MaxBoardPins]
		}
	}
	return b, nil
```

Add `"path/filepath"` to config.go's imports.

- [ ] **Step 2: Delete the old surface**

- `git rm internal/store/pins.go`
- Remove the `Pins` struct from `internal/core/config.go` (and its comment).
- Remove `type Pins = core.Pins` from `internal/store/types_compat.go`.
- Remove the `PinService` interface from `internal/core/service.go` and delete `PinService` from the `Service` composite.

- [ ] **Step 3: Migrate remaining test references**

- `internal/store/pins_test.go`: delete it; port any still-valuable assertions (e.g. actor validation on write) into `config_test.go` against `SetProjectBoards` — actor validation is already covered by `TestSetProjectBoardsValidation`, so plain deletion is expected.
- Task 3's `TestGetBoardsConfigFoldsLegacyPins` used `WritePins`; rewrite the fixture as a raw file:

```go
	pinsPath := filepath.Join(s.StorePath(), "projects", "ATM", "pins.json")
	if err := os.WriteFile(pinsPath, []byte(`{"boards":["ATM:open-tasks","ATM:backlog"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
```

(Adjust the path join to the actual project-dir layout — check `projectDir` in `internal/store/store.go`; if `StorePath()` isn't the projects root, compute from the temp dir the test store was opened on.)
- Same raw-file fixture for the one legacy TUI test kept in Task 6, and for `internal/cli` / `internal/store` tests that referenced pins fixtures (`env_test.go`, `task_test.go`, `store_test.go`, `eventsource_views_test.go`) — most of these only *list* `pins.json` in expected-file inventories; update the inventories (pins.json no longer written by any code path).

- [ ] **Step 4: Full build + test sweep**

Run: `go build ./... && go test ./...`
Expected: PASS; then `grep -rn "GetPins\|WritePins" internal cmd --include="*.go"` → no hits.

- [ ] **Step 5: Commit + journal**

```bash
git add -A internal
git commit -m "refactor(ATM-87b3ae): retire pins.json — boards config is the only pin store"
atm task comment add --task ATM-87b3ae --actor developer@claude:fable-5 --label ATM:comment:progress --body "Task 7: pins.json write path removed (core.Pins, PinService, store/pins.go, types_compat alias); only legacyPinBoards remains, feeding the lazy migration read."
```

---

### Task 8: CLI — `atm capability unmanaged`

**Files:**
- Modify: `internal/cli/capability.go`
- Test: `internal/cli/capability_test.go` (+ goldens under `internal/cli/testdata/golden/`)

**Interfaces:**
- Consumes: `Registry.Unmanaged` (Task 2), `st.fullRegistry`, `s.LabelUsageGrouped`.
- Produces: JSON envelope `{"project": "<CODE>", "labels": [{"name","description","usage"}]}`.

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/capability_test.go`:

```go
// TestGoldenCapabilityUnmanaged: the manager's triage read — labels no
// enabled capability owns, with usage counts. Workflow-owned labels
// (status:open via the seeded vocabulary) must NOT appear.
func TestGoldenCapabilityUnmanaged(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "PCX", "--name", "cap demo",
		"--capabilities", "workflow", "--actor", "admin@cli:unset")
	h.run("label", "add", "--name", "PCX:type:bug", "--actor", "admin@cli:unset")
	h.run("label", "add", "--name", "PCX:urgent", "--actor", "admin@cli:unset")
	h.run("task", "create", "--project", "PCX", "--title", "t1",
		"--label", "PCX:type:bug", "--label", "PCX:status:open", "--actor", "admin@cli:unset")
	out, _, code := h.run("--output", "json", "capability", "unmanaged", "--project", "PCX")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if strings.Contains(out, "status:open") {
		t.Fatalf("workflow-owned label leaked into unmanaged: %s", out)
	}
	compareGolden(t, "capability-unmanaged", out)
}
```

(Match `label add` / `task create` flag names to the existing harness tests — copy from `seedScenario` helpers in `harness_test.go` if the flags differ.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestGoldenCapabilityUnmanaged 2>&1 | head`
Expected: FAIL — unknown command "unmanaged".

- [ ] **Step 3: Implement**

In `internal/cli/capability.go`, mount `cmd.AddCommand(newCapabilityUnmanagedCmd(st))` in `newCapabilityCmd` (before the registry loop), and add:

```go
// newCapabilityUnmanagedCmd is the manager's triage read: every label in the
// project that no ENABLED capability owns (the TUI's "unmanaged" umbrella).
// Read-only; the triage verbs are the existing substrate ones (task label
// add/remove, project boards hide).
func newCapabilityUnmanagedCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "unmanaged",
		Short: "List labels no enabled capability owns (the umbrella's contents)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.GetProject(project)
			if err != nil {
				return err
			}
			labels, err := st.fullRegistry.For(p).Unmanaged(s, project)
			if err != nil {
				return err
			}
			usage, err := s.LabelUsageGrouped(project)
			if err != nil {
				return err
			}
			type row struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Usage       int    `json:"usage"`
			}
			rows := make([]row, 0, len(labels))
			for _, l := range labels {
				rows = append(rows, row{Name: l.Name, Description: l.Description, Usage: usage[l.Name]})
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "labels": rows}, func() {
				for _, r := range rows {
					fmt.Fprintf(st.stdout(), "%s\t%d\t%s\n", r.Name, r.Usage, r.Description)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
```

- [ ] **Step 4: Generate golden + run**

Run: `go test ./internal/cli/ -run TestGoldenCapabilityUnmanaged -update && go test ./internal/cli/ -run TestGoldenCapabilityUnmanaged`
Inspect `internal/cli/testdata/golden/capability-unmanaged.json` — it must list exactly `PCX:type:bug` (usage 1) and `PCX:urgent` (usage 0); nothing workflow-owned, and no `PCX:type:*` row (no such stored label was created).
Expected: PASS. Also run the full package: `go test ./internal/cli/` (mount help goldens may need `-update` — inspect diffs before accepting).

- [ ] **Step 5: Commit + journal**

```bash
git add internal/cli
git commit -m "feat(ATM-87b3ae): atm capability unmanaged — the manager's triage read"
atm task comment add --task ATM-87b3ae --actor developer@claude:fable-5 --label ATM:comment:progress --body "Task 8: atm capability unmanaged lists ownership-diffed labels with usage counts (JSON + text goldens)."
```

---

### Task 9: CLI — `atm project boards reorder/hide/show`

**Files:**
- Modify: `internal/cli/project.go`
- Test: `internal/cli/project_test.go` (+ goldens)

**Interfaces:**
- Consumes: `GetBoardsConfig`/`SetProjectBoards` (Task 3), `Registry.Exposed` + `UmbrellaFullName` + `OrderFullNames` (Task 2).
- Produces: `atm project boards` verb trio writing `config.json.boards`.

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/project_test.go`:

```go
// TestGoldenProjectBoardsHideShowReorder: display preferences round-trip
// through config.json.boards; reorder materializes the effective ring order.
func TestGoldenProjectBoardsHideShowReorder(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "PBX", "--name", "boards demo",
		"--capabilities", "workflow", "--actor", "admin@cli:unset")

	out, _, code := h.run("--output", "json", "project", "boards", "hide",
		"--project", "PBX", "--name", "PBX:backlog", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("hide exit %d: %s", code, out)
	}
	compareGolden(t, "project-boards-hide", out)

	// Hiding is idempotent.
	if _, _, code := h.run("project", "boards", "hide", "--project", "PBX",
		"--name", "PBX:backlog", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("second hide exit %d", code)
	}

	out2, _, code := h.run("--output", "json", "project", "boards", "reorder",
		"--project", "PBX", "--name", "PBX:status:*", "--first", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("reorder exit %d: %s", code, out2)
	}
	compareGolden(t, "project-boards-reorder-first", out2)

	out3, _, code := h.run("--output", "json", "project", "boards", "show",
		"--project", "PBX", "--name", "PBX:backlog", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("show exit %d: %s", code, out3)
	}
	compareGolden(t, "project-boards-show", out3)
}

func TestProjectBoardsReorderValidation(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "PBX", "--name", "boards demo",
		"--capabilities", "workflow", "--actor", "admin@cli:unset")
	// Exactly one placement flag required.
	if _, _, code := h.run("project", "boards", "reorder", "--project", "PBX",
		"--name", "PBX:backlog", "--actor", "admin@cli:unset"); code == 0 {
		t.Fatal("reorder with no placement flag must fail")
	}
	if _, _, code := h.run("project", "boards", "reorder", "--project", "PBX",
		"--name", "PBX:backlog", "--first", "--last", "--actor", "admin@cli:unset"); code == 0 {
		t.Fatal("reorder with two placement flags must fail")
	}
	// Unknown board name errors (nothing to move).
	if _, _, code := h.run("project", "boards", "reorder", "--project", "PBX",
		"--name", "PBX:nosuch", "--first", "--actor", "admin@cli:unset"); code == 0 {
		t.Fatal("reorder of a name not in the effective ring must fail")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'ProjectBoards' 2>&1 | head`
Expected: FAIL — unknown command "boards".

- [ ] **Step 3: Implement**

In `internal/cli/project.go`, add `cmd.AddCommand(newProjectBoardsCmd(st))` to `newProjectCmd`, then:

```go
// newProjectBoardsCmd groups the per-project boards display preferences:
// ring order and hidden boards, written to config.json's boards key. Pins
// stay TUI-only. Display preference, not substrate state — no store event.
func newProjectBoardsCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "boards",
		Short: "Boards ring display preferences (order, hidden)",
	}
	cmd.AddCommand(newProjectBoardsReorderCmd(st))
	cmd.AddCommand(newProjectBoardsHideCmd(st))
	cmd.AddCommand(newProjectBoardsShowCmd(st))
	return cmd
}

// boardsConfigFor loads project p's boards config and the effective ring
// order (enabled capabilities' Exposed in registration order + the umbrella
// sentinel last, existing order override applied).
func boardsConfigFor(st *cliState, s core.Service, project string) (*core.Project, *core.BoardsConfig, []string, error) {
	p, err := s.GetProject(project)
	if err != nil {
		return nil, nil, nil, err
	}
	cfg, err := s.GetBoardsConfig(project)
	if err != nil {
		return nil, nil, nil, err
	}
	var effective []string
	for _, e := range st.fullRegistry.For(p).Exposed(project) {
		effective = append(effective, e.Label.Name)
	}
	effective = append(effective, capability.UmbrellaFullName(project))
	return p, cfg, capability.OrderFullNames(effective, cfg.Order), nil
}

func newProjectBoardsHideCmd(st *cliState) *cobra.Command {
	var project, name string
	cmd := &cobra.Command{
		Use:   "hide",
		Short: "Hide a board from the TUI ring (idempotent; persists across capability re-enable)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.RequireMutatingActor()
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			_, cfg, _, err := boardsConfigFor(st, s, project)
			if err != nil {
				return err
			}
			present := false
			for _, h := range cfg.Hidden {
				if h == name {
					present = true
				}
			}
			if !present {
				cfg.Hidden = append(cfg.Hidden, name)
				if err := s.SetProjectBoards(project, cfg, actor); err != nil {
					return err
				}
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "hidden": cfg.Hidden}, func() {
				fmt.Fprintf(st.stdout(), "%s: hidden %s\n", project, name)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&name, "name", "", "board FullName (e.g. ATM:open-tasks or ATM:status:*)")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newProjectBoardsShowCmd(st *cliState) *cobra.Command {
	var project, name string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Unhide a board (idempotent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.RequireMutatingActor()
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			_, cfg, _, err := boardsConfigFor(st, s, project)
			if err != nil {
				return err
			}
			kept := cfg.Hidden[:0:0]
			changed := false
			for _, h := range cfg.Hidden {
				if h == name {
					changed = true
					continue
				}
				kept = append(kept, h)
			}
			if changed {
				cfg.Hidden = kept
				if err := s.SetProjectBoards(project, cfg, actor); err != nil {
					return err
				}
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "hidden": cfg.Hidden}, func() {
				fmt.Fprintf(st.stdout(), "%s: shown %s\n", project, name)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&name, "name", "", "board FullName")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newProjectBoardsReorderCmd(st *cliState) *cobra.Command {
	var project, name, before, after string
	var first, last bool
	cmd := &cobra.Command{
		Use:   "reorder",
		Short: "Move a board within the TUI ring (materializes the full order)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.RequireMutatingActor()
			if err != nil {
				return err
			}
			placements := 0
			for _, on := range []bool{before != "", after != "", first, last} {
				if on {
					placements++
				}
			}
			if placements != 1 {
				return fmt.Errorf("%w: exactly one of --before, --after, --first, --last is required", ErrUsage)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			_, cfg, order, err := boardsConfigFor(st, s, project)
			if err != nil {
				return err
			}
			// Remove name from the materialized order; it must exist.
			idx := -1
			for i, n := range order {
				if n == name {
					idx = i
				}
			}
			if idx < 0 {
				return fmt.Errorf("%w: %q is not in the ring (exposed boards + %s)", ErrNotFound, name, capability.UmbrellaFullName(project))
			}
			order = append(order[:idx], order[idx+1:]...)
			insertAt := len(order)
			switch {
			case first:
				insertAt = 0
			case last:
				insertAt = len(order)
			case before != "":
				insertAt = indexOf(order, before)
			case after != "":
				insertAt = indexOf(order, after) + 1
			}
			if insertAt < 0 || (after != "" && insertAt == 0) {
				return fmt.Errorf("%w: anchor board not in the ring", ErrNotFound)
			}
			order = append(order[:insertAt], append([]string{name}, order[insertAt:]...)...)
			cfg.Order = order
			if err := s.SetProjectBoards(project, cfg, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "order": order}, func() {
				fmt.Fprintf(st.stdout(), "%s ring order:\n", project)
				for _, n := range order {
					fmt.Fprintf(st.stdout(), "  %s\n", n)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&name, "name", "", "board FullName to move")
	cmd.Flags().StringVar(&before, "before", "", "place immediately before this board")
	cmd.Flags().StringVar(&after, "after", "", "place immediately after this board")
	cmd.Flags().BoolVar(&first, "first", false, "place first in the ring")
	cmd.Flags().BoolVar(&last, "last", false, "place last in the ring")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// indexOf returns the index of s in list, or -1.
func indexOf(list []string, s string) int {
	for i, n := range list {
		if n == s {
			return i
		}
	}
	return -1
}
```

Add `"atm/internal/capability"` to project.go's imports if not present (it is — `resolveCapabilityChoice` uses it). Note the `--after` anchor-miss check: `indexOf` returns -1 → insertAt 0 → the guard rejects it; verify the guard logic compiles as written (`insertAt == 0` only errors when `after != ""` and the anchor was missing — if the anchor is genuinely at position -1+1=0 that means anchor was `order[?]`... simplify if needed to `if (before != "" || after != "") && indexOf(order, anchor) < 0 { return ErrNotFound }` computed *before* insertion).

- [ ] **Step 4: Generate goldens + run**

Run: `go test ./internal/cli/ -run 'ProjectBoards' -update && go test ./internal/cli/`
Inspect the three new goldens: hide shows `["PBX:backlog"]`; reorder-first shows the materialized order starting `PBX:status:*` with `PBX:unmanaged` last; show returns `[]`/empty hidden.
Expected: PASS (project help golden may need `-update` for the new subcommand; inspect the diff).

- [ ] **Step 5: Commit + journal**

```bash
git add internal/cli
git commit -m "feat(ATM-87b3ae): atm project boards reorder/hide/show display preferences"
atm task comment add --task ATM-87b3ae --actor developer@claude:fable-5 --label ATM:comment:progress --body "Task 9: project boards verb trio writes config.json.boards — hide/show idempotent, reorder materializes effective ring order (exposed + umbrella last) then moves one entry."
```

---

### Task 10: Docs — conventions + manager autopilot triage step

**Files:**
- Modify: `internal/cli/conventions.go` (both `conventionsCoreText` and `conventionsStructured`)
- Modify: `internal/manager/context_v1.md`
- Test: existing golden/content tests in `internal/cli/conventions_test.go`, `internal/manager/context_test.go` (update expectations)

**Interfaces:** none — prose only. The verb is the contract; no manager-specific code.

- [ ] **Step 1: Update conventions**

In `conventionsCoreText`'s Capabilities section, after the `guide` line, add:

```go
	"- `atm capability unmanaged --project <CODE>` — labels no enabled capability owns (the TUI's \"unmanaged\" umbrella); the manager triages these onto capability-owned labels or hides them (`atm project boards hide`).\n\n" +
```

(replacing the existing trailing `\n\n` on the guide line with `\n`). Mirror the same line in `conventionsStructured`'s capabilities entry (match its existing shape — likely a `[]map[string]string` of command/description pairs).

- [ ] **Step 2: Update the manager context**

In `internal/manager/context_v1.md`, append to the capabilities paragraph (the one starting "Capabilities own the operating procedures."):

```markdown
After the per-capability autopilot passes, triage the unmanaged tail: run `<ATM_BIN> capability unmanaged --project <CODE>`. For each unmanaged label, decide whether its tasks should carry a capability-owned label instead (replace via `<ATM_BIN> task label remove` + `<ATM_BIN> task label add`); hide namespaces you deliberately keep out of view with `<ATM_BIN> project boards hide --project <CODE> --name <CODE>:<ns>:*`. Re-run `capability unmanaged` to verify the tail shrank. Do not delete labels or hide boards the human curated without asking.
```

- [ ] **Step 3: Update affected tests + goldens**

Run: `go test ./internal/cli/ ./internal/manager/ 2>&1 | head -30` — fix content assertions / regenerate goldens with `-update` where the tests are golden-based (inspect every regenerated golden diff: only the new lines should appear).

- [ ] **Step 4: Full sweep**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit + journal + close out**

```bash
git add internal/cli internal/manager
git commit -m "docs(ATM-87b3ae): conventions + manager autopilot gain the unmanaged triage step"
atm task comment add --task ATM-87b3ae --actor developer@claude:fable-5 --label ATM:comment:progress --body "Task 10: conventions enumerate atm capability unmanaged; manager context_v1 autopilot gains the triage step. Full test sweep green. Implementation complete pending review."
```

---

## Plan Self-Review Notes (already applied)

- Spec §1–§9 each map to a task: §1→T1, §2→T2, §3→T3+T7, §4→T4+T5+T6, §5→T8+T9, §6→T10, §7→T1–T9 (layering respected: no new packages), §8→per-task test steps, §9→T3 (lazy migration) + T7 (retirement).
- Pins CLI deliberately absent (spec §10 correction) — Task 6 keeps pins TUI-only.
- Type-consistency: `GetBoardsConfig`/`SetProjectBoards` names used identically in T3/T4/T6/T7/T9; `ExposedLabel{Label, Owner}` in T2/T4/T9; `UmbrellaFullName` in T2/T4/T9; `boardRow.Umbrella` in T4/T5.
- Known judgment calls an implementer may hit: exact field names on `tasksModel` focus assertions (T4 Step 1) and the raw pins.json fixture path (T7 Step 3) are marked for verification against the tree rather than guessed.
