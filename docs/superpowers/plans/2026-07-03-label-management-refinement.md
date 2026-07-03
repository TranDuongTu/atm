# Label Management Refinement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refine ATM's label management surface: dedicated Labels tab, multi-label task creation, default-label seeding on project create + on demand, rewritten conventions with an agent code-of-conduct and "understand labels first" sequence.

**Architecture:** A new `internal/seed` package holds the default label set (data only). `internal/store` imports it and gains `LabelSeed`/`SeedLabels`; `CreateProject` auto-seeds. `internal/cli` gains `atm label seed` and a rewritten `conventions.go`. `internal/tui` gains a 4th tab (Labels), drops the Projects-detail labels section, and adds a `labels` field to the task-create form.

**Tech Stack:** Go 1.22+, cobra, Bubble Tea, table-driven tests, golden-file pattern via `compareGolden`.

## Global Constraints

- Go 1.22+; module path `atm`; single binary `atm`.
- No emojis in code, commits, or docs.
- Verification gate: `make verify` (runs `make build && make test`); must be green before each commit.
- Label-name regex (full): `^[A-Z]{3,6}(:[a-z0-9][a-z0-9-]*){1,2}$`; suffix regex (user-typed, prefix auto-prepended): `^[a-z0-9][a-z0-9-]*(:[a-z0-9][a-z0-9-]*)?$`.
- Project code regex: `^[A-Z]{3,6}$`.
- Golden files live at `internal/cli/testdata/golden/*.json`; regenerate with `go test ./internal/cli -update`.
- History is an immutable system invariant: every mutation appends a `HistoryEntry`; no command edits/deletes history.
- Actor is a free-form string; required on mutating commands, optional on reads; TUI defaults to `"default"` when launched without `--actor`.
- The v2 spec's data model (Project, Task, Label, HistoryEntry) is unchanged; this plan adds store methods and TUI surface only.

---

## File Structure

**New files:**
- `internal/seed/seed.go` — the default label set + `Label` struct (data only; no store import).
- `internal/seed/seed_test.go` — validates the seed list (non-empty, valid suffixes, no dupes, descriptions non-empty).
- `internal/tui/labels.go` — the Labels tab model (list + detail), forms (add/describe/remove), seed key handler.
- `internal/tui/labels_test.go` — Labels tab tests.

**Modified files:**
- `internal/store/label.go` — add `LabelSeed` and `SeedLabels`; import `internal/seed`.
- `internal/store/project.go` — `CreateProject` calls `SeedLabels` after committing the project file.
- `internal/store/label_test.go` — add `TestLabelSeedPreservesExistingDescription`, `TestLabelSeedSetsDescriptionOnCreate`.
- `internal/store/project_test.go` — add `TestCreateProjectSeedsLabels`, `TestSeedLabelsIdempotentPreservesDescriptions`.
- `internal/cli/conventions.go` — rewrite `conventionsText` + `conventionsStructured()` (code-of-conduct, new context labels, "read labels first" sequence, `seeded_labels` JSON key).
- `internal/cli/label.go` — add `atm label seed --project <CODE>` subcommand.
- `internal/cli/label_test.go` — add `TestGoldenLabelSeed`.
- `internal/cli/harness_test.go` — `seedScenario1` swaps `ATM:context:start-here` for a seeded label or a project-specific custom label (the seeded ones now exist post-create); see Task 7 for the exact fixture change.
- `internal/cli/determinism_test.go` — `seedDeterminismStore` updated to reflect auto-seeding (the `label add` calls for seeded labels become no-ops / are removed); see Task 7.
- `internal/cli/testdata/golden/*.json` — regenerated via `go test ./internal/cli -update` after Task 7.
- `internal/tui/app.go` — `numPanes` 3→4; new `paneLabels`; new `labelsModel` field; `formLabelDescribe` form kind; tab bar renders 4 tabs; `submitForm` routes `formLabelDescribe`; `renderBody`/`statusHint`/`handleKey` dispatch the Labels pane.
- `internal/tui/projects.go` — remove `L`/`l` from `handleDetailKey`; remove the LABELS section from `renderDetail`; remove `openLabelAddForm`/`openLabelRemoveForm`/`doLabelAdd`/`doLabelRemove`; update `statusHint`.
- `internal/tui/tasks.go` — `openCreateForm` adds a `labels` field; `doTaskCreate` parses it into a slice.
- `internal/tui/keymap.go` — remove `L/l` Projects-detail rows; add Labels-tab rows.
- `internal/tui/help.go` — update `parityTable` and `conventionsTextTUI`.
- `internal/tui/app_test.go` — update `TestTabSwitching`/`TestTabBarShowsNumbers` for 4 tabs; update `TestProjectDetailLabelsGrouped` (assert no LABELS section); update label-add/remove tests to use the Labels tab; update `TestHelpTabReadOnly` keys.
- `scripts/dogfood.sh` — drop the manual `seed_labels` loop (create now seeds); swap `ATM:context:start-here` for `ATM:context:agent` in the bootstrap task.
- `README.md` — update the conventions section to match the rewritten `conventionsText`.

---

## Task 1: Create the `internal/seed` package

**Files:**
- Create: `internal/seed/seed.go`
- Test: `internal/seed/seed_test.go`

**Interfaces:**
- Produces: `seed.Label` struct `{Suffix, Description string}`; `seed.Labels` slice (the 17 default labels). No imports beyond the standard library.

- [ ] **Step 1: Write the failing test**

Create `internal/seed/seed_test.go`:

```go
package seed

import (
	"regexp"
	"testing"
)

var suffixRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(:[a-z0-9][a-z0-9-]*)?$`)

func TestLabelsNonEmpty(t *testing.T) {
	if len(Labels) == 0 {
		t.Fatal("seed.Labels is empty")
	}
}

func TestLabelsAllValidSuffixes(t *testing.T) {
	for _, l := range Labels {
		if !suffixRe.MatchString(l.Suffix) {
			t.Errorf("suffix %q does not match the suffix regex", l.Suffix)
		}
	}
}

func TestLabelsNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, l := range Labels {
		if seen[l.Suffix] {
			t.Errorf("duplicate suffix %q", l.Suffix)
		}
		seen[l.Suffix] = true
	}
}

func TestLabelsDescriptionsNonEmpty(t *testing.T) {
	for _, l := range Labels {
		if l.Description == "" {
			t.Errorf("label %q has empty description (code-of-conduct requires non-empty)", l.Suffix)
		}
	}
}

func TestLabelsCountIs17(t *testing.T) {
	if len(Labels) != 17 {
		t.Fatalf("seed.Labels has %d entries, want 17", len(Labels))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/seed/...`
Expected: FAIL — package not found / `Labels` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/seed/seed.go`:

```go
// Package seed holds the default label set applied when a project is
// created and re-applied on demand by `atm label seed` / the Labels tab
// [S] key. It is the single source of truth for the seeded label names
// and their descriptions (the agent code-of-conduct requires every
// seeded label to carry a description so a fresh agent reading
// `atm label list --project <CODE>` sees meaningful text immediately).
//
// This package holds data only — no store import. The store imports it
// and implements the apply logic (LabelSeed/SeedLabels) to avoid a
// circular dependency.
package seed

// Label is one default label to seed into a new project. Suffix is the
// "<namespace>:<value>" or "<tag>" portion; the project code prefix is
// prepended at apply time. Description is the intention statement
// surfaced in the Labels tab and read by agents during first-contact.
type Label struct {
	Suffix      string
	Description string
}

// Labels is the single source of truth for the default label set seeded
// on project create and re-applied by `atm label seed` / the Labels tab
// [S] key. Templated namespaces (repo:<name>, doc:<name>,
// claimed-by:<agent>, blocks:<ID>, related:<ID>) are intentionally NOT
// seeded as concrete labels — they depend on project-specific values
// and are created on demand.
var Labels = []Label{
	{"status:open", "workflow state: open; task is not started or is being considered"},
	{"status:todo", "workflow state: todo; task is queued for work"},
	{"status:in-progress", "workflow state: in-progress; someone is actively working on this"},
	{"status:done", "workflow state: done; task is complete"},
	{"status:blocked", "workflow state: blocked; task cannot proceed pending something else"},
	{"status:review", "workflow state: review; task is awaiting review/approval"},
	{"type:bug", "task categorization: bug; a defect to fix"},
	{"type:feature", "task categorization: feature; new functionality to add"},
	{"type:task", "task categorization: task; general work item"},
	{"type:chore", "task categorization: chore; maintenance, refactoring, tooling"},
	{"priority:high", "optional prioritization: high"},
	{"priority:medium", "optional prioritization: medium"},
	{"priority:low", "optional prioritization: low"},
	{"context:documentation", "the labeled task contains documentation about the project"},
	{"context:repository", "the labeled task contains a pointer to a code repository"},
	{"context:agent", "agent direction when navigating the project; read these to understand how to work in this project"},
	{"context:fixit", "something on this task should be reviewed, updated, or altered"},
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/seed/...`
Expected: PASS (all 5 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/seed/seed.go internal/seed/seed_test.go
git commit -m "seed: add default label set (17 labels with descriptions)"
```

---

## Task 2: Add `LabelSeed` and `SeedLabels` to the store

**Files:**
- Modify: `internal/store/label.go`
- Test: `internal/store/label_test.go`

**Interfaces:**
- Consumes: `seed.Labels` (from Task 1), existing `ValidateLabelName`, `labelProjectExists`, `WithLock`, `loadLabels`, `writeLabels`.
- Produces: `Store.LabelSeed(name, description, actor string) error` — upserts, sets description only on create (never overwrites). `Store.SeedLabels(code, actor string) error` — iterates `seed.Labels`, calls `LabelSeed`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/store/label_test.go` (append at end of file, before the closing — note the file already imports `testing` only):

```go
func TestLabelSeedSetsDescriptionOnCreate(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	if err := s.LabelSeed("ATM:custom:x", "seed desc", "claude"); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LabelShow("ATM:custom:x")
	if l.Description != "seed desc" {
		t.Fatalf("description = %q want \"seed desc\"", l.Description)
	}
}

func TestLabelSeedPreservesExistingDescription(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_ = s.LabelAdd("ATM:type:bug", "human edited", "claude")
	if err := s.LabelSeed("ATM:type:bug", "seed default", "claude"); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LabelShow("ATM:type:bug")
	if l.Description != "human edited" {
		t.Fatalf("LabelSeed overwrote description: got %q want \"human edited\"", l.Description)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store -run 'TestLabelSeed' -v`
Expected: FAIL — `s.LabelSeed undefined`.

- [ ] **Step 3: Implement `LabelSeed`**

Add to `internal/store/label.go` (after the existing `LabelAdd` function, around line 46). First add `"atm/internal/seed"` to the import block at the top of the file.

The `LabelSeed` body mirrors `LabelAdd` but the inner upsert branch is a no-op when the label exists:

```go
// LabelSeed upserts a label but only sets the description when the label
// is newly created. Existing labels keep their descriptions — this
// preserves human edits when SeedLabels re-applies the default set. Used
// by SeedLabels (project create + on-demand seed). Contrast with
// LabelAdd, which overwrites the description when the new one is
// non-empty and differs.
func (s *Store) LabelSeed(name, description, actor string) error {
	if err := ValidateLabelName(name); err != nil {
		return err
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	if err := s.labelProjectExists(name); err != nil {
		return err
	}
	return s.WithLock(labelProject(name), func() error {
		lf, err := s.loadLabels()
		if err != nil {
			return err
		}
		for _, l := range lf.Labels {
			if l.Name == name {
				// Exists: preserve the existing description (no-op).
				return nil
			}
		}
		lf.Labels = append(lf.Labels, Label{Name: name, Description: description})
		sort.SliceStable(lf.Labels, func(i, j int) bool { return lf.Labels[i].Name < lf.Labels[j].Name })
		return s.writeLabels(lf)
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store -run 'TestLabelSeed' -v`
Expected: PASS.

- [ ] **Step 5: Write the failing test for `SeedLabels`**

Add to `internal/store/project_test.go` (append at end):

```go
func TestSeedLabelsAppliesAllDefaults(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	// CreateProject already seeds; calling SeedLabels again is idempotent.
	if err := s.SeedLabels("ATM", "claude"); err != nil {
		t.Fatal(err)
	}
	ls := s.LabelList("ATM", "")
	if len(ls) != 17 {
		t.Fatalf("SeedLabels left %d labels, want 17", len(ls))
	}
	// Spot-check a description.
	l, _ := s.LabelShow("ATM:context:agent")
	if l.Description == "" {
		t.Error("ATM:context:agent has empty description after seed")
	}
}

func TestSeedLabelsPreservesEditedDescriptions(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	// Edit one label's description (human curates).
	_ = s.LabelAdd("ATM:type:bug", "human edited", "claude")
	if err := s.SeedLabels("ATM", "claude"); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LabelShow("ATM:type:bug")
	if l.Description != "human edited" {
		t.Fatalf("SeedLabels overwrote edited description: got %q want \"human edited\"", l.Description)
	}
	// The other 16 keep their seed descriptions.
	l2, _ := s.LabelShow("ATM:status:open")
	if l2.Description == "" {
		t.Error("ATM:status:open lost its description after re-seed")
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/store -run 'TestSeedLabels' -v`
Expected: FAIL — `s.SeedLabels undefined` (and `TestSeedLabelsAppliesAllDefaults` may also fail because `CreateProject` doesn't seed yet — that's Task 3; for this task the test only needs `SeedLabels` to exist).

Note: `TestSeedLabelsAppliesAllDefaults` asserts `CreateProject` already seeds — it will fail until Task 3. To keep this task's test cycle green, adjust the assertion in Step 5 to not rely on `CreateProject` seeding. Replace the body of `TestSeedLabelsAppliesAllDefaults` with:

```go
func TestSeedLabelsAppliesAllDefaults(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	// SeedLabels applies all 17 defaults (CreateProject seeding is wired in Task 3).
	if err := s.SeedLabels("ATM", "claude"); err != nil {
		t.Fatal(err)
	}
	ls := s.LabelList("ATM", "")
	if len(ls) != 17 {
		t.Fatalf("SeedLabels left %d labels, want 17", len(ls))
	}
	l, _ := s.LabelShow("ATM:context:agent")
	if l.Description == "" {
		t.Error("ATM:context:agent has empty description after seed")
	}
}
```

- [ ] **Step 7: Implement `SeedLabels`**

Add to `internal/store/label.go` (after `LabelSeed`):

```go
// SeedLabels applies the default seed labels (internal/seed.Labels) to the
// project. Idempotent — preserves existing descriptions (via LabelSeed).
// Called by CreateProject and by the CLI/TUI on-demand seed path.
func (s *Store) SeedLabels(code, actor string) error {
	for _, l := range seed.Labels {
		full := code + ":" + l.Suffix
		if err := s.LabelSeed(full, l.Description, actor); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/store -run 'TestSeedLabels|TestLabelSeed' -v`
Expected: PASS (both `TestSeedLabelsAppliesAllDefaults` and `TestSeedLabelsPreservesEditedDescriptions`).

- [ ] **Step 9: Run the full store test suite to confirm no regressions**

Run: `go test ./internal/store/...`
Expected: PASS (all existing tests still green — `CreateProject` seeding is not yet wired, so existing label-list tests that expect a clean slate still pass).

- [ ] **Step 10: Commit**

```bash
git add internal/store/label.go internal/store/label_test.go internal/store/project_test.go
git commit -m "store: add LabelSeed (no-overwrite upsert) and SeedLabels (apply defaults)"
```

---

## Task 3: Wire `CreateProject` to seed defaults

**Files:**
- Modify: `internal/store/project.go`
- Test: `internal/store/project_test.go`

**Interfaces:**
- Consumes: `s.SeedLabels` (from Task 2).
- Produces: `CreateProject` auto-seeds all 17 default labels after committing the project file.

- [ ] **Step 1: Write the failing test**

Add to `internal/store/project_test.go`:

```go
func TestCreateProjectSeedsLabels(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "x", "claude"); err != nil {
		t.Fatal(err)
	}
	ls := s.LabelList("ATM", "")
	if len(ls) != 17 {
		t.Fatalf("after CreateProject, ATM has %d labels, want 17 (seeded defaults)", len(ls))
	}
	// Every seeded label has a non-empty description.
	for _, l := range ls {
		if l.Description == "" {
			t.Errorf("seeded label %q has empty description", l.Name)
		}
	}
	// Spot-check a known seed label is present.
	if _, err := s.LabelShow("ATM:context:agent"); err != nil {
		t.Errorf("ATM:context:agent missing after seed: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/store -run 'TestCreateProjectSeedsLabels' -v`
Expected: FAIL — `after CreateProject, ATM has 0 labels, want 17`.

- [ ] **Step 3: Wire seeding into `CreateProject`**

Edit `internal/store/project.go` `CreateProject`. After the `WithLock` block returns `created` successfully (after `if err != nil { return nil, err }` at line 44-46, before `return created, nil`), call `SeedLabels`. Seeding occurs **outside** the `WithLock` block; `SeedLabels` takes its own project lock.

Current tail of `CreateProject`:

```go
	err := s.WithLock(code, func() error {
		// ... writes project file, sets created = p
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}
```

Change to:

```go
	err := s.WithLock(code, func() error {
		// ... writes project file, sets created = p
	})
	if err != nil {
		return nil, err
	}
	// Seed the default label set (idempotent; outside the project-create
	// lock — SeedLabels takes its own project lock). A fresh project has
	// all 17 default labels with descriptions the moment it exists.
	if seedErr := s.SeedLabels(code, actor); seedErr != nil {
		return nil, seedErr
	}
	return created, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store -run 'TestCreateProjectSeedsLabels' -v`
Expected: PASS.

- [ ] **Step 5: Run the full store suite — expect existing tests to break**

Run: `go test ./internal/store/...`
Expected: FAILURES in existing tests that assert specific label counts after `CreateProject` (e.g. `TestLabelListFiltersByProjectAndNamespace` creates ATM and SCY, then adds 3 labels and expects 2 for ATM — now ATM has 17+2=19; `TestLabelRemoveSoftRetainsUsage` expects a specific label set). These are expected; we fix them in this step.

The fix policy: existing store tests that called `CreateProject` and then asserted on `LabelList` counts must now account for the 17 seeded labels. For tests that need a clean slate (no seeded labels), they cannot get one via `CreateProject` anymore — instead, they should remove the seeded labels first, OR assert against the new baseline.

Review each failing store test:

- `TestLabelListFiltersByProjectAndNamespace` (`label_test.go:70`): creates ATM, SCY, adds 3 labels. Currently asserts `LabelList("ATM","")` == 2. After seeding, ATM has 17 + 2 added (ATM:type:bug, ATM:status:open) = 19. The test's intent is "filter by project works". Update the assertions to account for the 17 seed labels: `LabelList("ATM","")` should now be 19; `LabelList("ATM","status")` should be 1 (seeded) + 0 added = the seeded `ATM:status:open`... wait — `ATM:status:open` IS one of the seeded labels, and the test adds `ATM:status:open` again (upsert → no new entry). So ATM has 17 seed + `ATM:type:bug` (also seeded, upsert) = 17 total; the only non-seeded addition is... actually `ATM:type:bug` and `ATM:status:open` are both seeded. SCY gets `SCY:type:bug` added (SCY is also seeded on create → 17 + 1 = 18). The test must be rewritten to use non-seeded labels to validate filtering. Change the added labels to `ATM:custom:a`, `ATM:custom:b`, `SCY:custom:a`:

```go
func TestLabelListFiltersByProjectAndNamespace(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_, _ = s.CreateProject("SCY", "y", "claude")
	_ = s.LabelAdd("ATM:custom:a", "", "claude")
	_ = s.LabelAdd("ATM:custom:b", "", "claude")
	_ = s.LabelAdd("SCY:custom:a", "", "claude")
	// ATM has 17 seeded + 2 custom = 19.
	if got := len(s.LabelList("ATM", "")); got != 19 {
		t.Fatalf("ATM labels = %d want 19", got)
	}
	// Filter to the custom namespace.
	if got := len(s.LabelList("ATM", "custom")); got != 2 {
		t.Fatalf("ATM:custom labels = %d want 2", got)
	}
}
```

- `TestLabelRemoveSoftRetainsUsage` (`label_test.go:39`): creates ATM, creates a task with `ATM:type:bug` (seeded), removes `ATM:type:bug`. The `LabelRemove` returns `retained_usage=1` (the task carries it). This still works — seeding doesn't change removal semantics. But the label is seeded with a description, and the test doesn't assert on description, so it should still pass. Verify by re-running after the other fixes.

- `TestLabelAddUpsertPreservesDescription` (`label_test.go:23`): creates ATM, adds `ATM:type:bug` with "first". But `ATM:type:bug` is now seeded with a default description on `CreateProject`. So `LabelAdd("ATM:type:bug","first")` overwrites the seed description to "first". Then `LabelAdd("ATM:type:bug","")` preserves "first". Then `LabelAdd("ATM:type:bug","second")` overwrites to "second". The test still passes as-is (it only checks the description ends up as "first" then "second"). Verify.

- `TestNamespacesDistinctSorted` (`label_test.go:85`): creates ATM, adds `ATM:status:open`, `ATM:type:bug`, `ATM:hot`. But `status` and `type` are now seeded namespaces. The test asserts `Namespaces("ATM")` == `["status","type"]` (sorted). After seeding, namespaces include `status`, `type`, `priority`, `context` (4 seeded) + any added. Adding `ATM:status:open`/`ATM:type:bug` (already seeded → no new namespace) + `ATM:hot` (a tag → no namespace). So `Namespaces("ATM")` == `["context","priority","status","type"]`. Update:

```go
func TestNamespacesDistinctSorted(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_ = s.LabelAdd("ATM:hot", "", "claude") // unnamespaced tag
	_ = s.LabelAdd("ATM:custom:x", "", "claude")
	got := s.Namespaces("ATM")
	want := []string{"context", "custom", "priority", "status", "type"}
	if len(got) != 5 || got[0] != "context" || got[4] != "type" {
		t.Fatalf("Namespaces = %v want %v", got, want)
	}
}
```

Run after edits: `go test ./internal/store/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/project.go internal/store/project_test.go internal/store/label_test.go
git commit -m "store: CreateProject seeds default labels; fix store tests for seeded baseline"
```

---

## Task 4: Rewrite `conventions.go`

**Files:**
- Modify: `internal/cli/conventions.go`
- Test: `internal/cli/conventions_test.go`, golden files regenerated.

**Interfaces:**
- Consumes: `seed.Labels` (from Task 1) for the `seeded_labels` JSON key.
- Produces: rewritten `conventionsText` (string), rewritten `conventionsStructured()` (map with `code_of_conduct`, updated `namespaces`, updated `agent_first_contact_sequence`, new `seeded_labels`).

- [ ] **Step 1: Update the conventions test assertions first**

Edit `internal/cli/conventions_test.go`. Add new assertions to `TestConventionsText` and `TestConventionsJSON` (keep the existing `compareGolden` calls — goldens are regenerated in Step 4):

```go
func TestConventionsText(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	out, _, code := h.run("conventions")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, "Suggested seed namespaces") {
		t.Fatalf("expected 'Suggested seed namespaces' in text output: %s", out)
	}
	if !strings.Contains(out, "Agent code-of-conduct") {
		t.Fatalf("expected 'Agent code-of-conduct' in text output")
	}
	if !strings.Contains(out, "read every label's description first") {
		t.Fatalf("expected 'read every label's description first' in text output")
	}
	compareGolden(t, "conventions-text", out)
}

func TestConventionsJSON(t *testing.T) {
	h := newGoldenHarness(t)
	out, _, code := h.run("conventions")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"conventions"`) {
		t.Fatalf("expected 'conventions' key in JSON output: %s", out)
	}
	if !strings.Contains(out, `"code_of_conduct"`) {
		t.Fatalf("expected 'code_of_conduct' key in JSON output")
	}
	if !strings.Contains(out, `"seeded_labels"`) {
		t.Fatalf("expected 'seeded_labels' key in JSON output")
	}
	compareGolden(t, "conventions-json", out)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli -run 'TestConventions' -v`
Expected: FAIL — new assertions missing in current output.

- [ ] **Step 3: Rewrite `conventions.go`**

Replace the entire contents of `internal/cli/conventions.go` with:

```go
package cli

import (
	"fmt"

	"atm/internal/seed"

	"github.com/spf13/cobra"
)

const conventionsVersion = "v2.1"

const conventionsText = `# ATM Conventions (advisory)

Version: ` + conventionsVersion + `

v2's vision is that workflow lives outside the system — in agent prompts and
human habits, not in the store. But a fresh agent landing in a project needs a
deterministic entry point, and a fresh project needs its labels seeded.
Onboarding is the act of seeding index tasks and seed labels; the conventions
below tell humans and agents how to do that using only the label substrate.
No bootstrap command, no reserved labels, no repo-side files, no store-side
repo paths. The conventions are documented, not code-reserved: the system
treats ATM:context:agent identically to ATM:type:bug.

## Suggested seed namespaces

A fresh project is auto-seeded with the 17 default labels below on ` + "`atm project create`" + `
(and re-applied idempotently by ` + "`atm label seed --project <CODE>`" + ` / the Labels tab [S] key).
Templated namespaces (repo:<name>, doc:<name>, claimed-by:<agent>, blocks:<ID>,
related:<ID>) are created on demand — they depend on project-specific values
and are NOT seeded as concrete labels.

| Namespace | Examples | Purpose |
|--------------------|-----------------------------|------------------------------------------------------------------|
| status:            | open, todo, in-progress, done, blocked, review | workflow states — labels only, no state machine |
| type:              | bug, feature, task, chore   | task categorization |
| priority:          | high, medium, low           | optional prioritization |
| context:documentation | ATM:context:documentation | the labeled task contains documentation about the project |
| context:repository | ATM:context:repository      | the labeled task contains a pointer to a code repository |
| context:agent      | ATM:context:agent           | agent direction when navigating the project |
| context:fixit      | ATM:context:fixit           | something on this task should be reviewed, updated, or altered |
| repo:<name>        | ATM:repo:atm                | index task pointing at a repo — created on demand, not seeded |
| doc:<name>         | ATM:doc:architecture        | index task pointing at a doc/resource — created on demand, not seeded |
| claimed-by:<agent> | ATM:claimed-by:claude       | who's working on what — last-writer-wins, no conflict detection |
| blocks:<ID>, related:<ID> | ATM:blocks:ATM-0002  | task relationships via labels — created on demand, not seeded |

## Agent code-of-conduct (label hygiene)

Agents working in an ATM project follow these rules to keep the label substrate
legible for humans and other agents:

1. Read before you write. Run ` + "`atm label list --project <CODE>`" + ` and read every
   label's description before introducing any new label. The existing labels
   are the project's vocabulary; reuse them whenever one fits your intent.
2. Default setup is the baseline. The seeded labels (status, type, priority,
   context) cover the common cases. Prefer them. Do not reinvent status:open
   as state:open or wf:open.
3. Invent only when nothing fits. If no existing label captures your intent,
   you may create a new one — agents are free to self-organize. But before you
   do, ask yourself: would a human reviewing the Labels tab understand why this
   label exists?
4. State the intention in the label description. When you create a new label,
   also call ` + "`atm label add --name <CODE>:<ns>:<value> --description \"<one sentence: why this label exists>\"`" + `.
   The description is the intention record. A label with no description is a
   flag for human review: "agent introduced this but didn't explain why."
5. One label, one meaning. Don't use the same label string to mean different
   things across tasks. If your intent diverges from an existing label's
   description, create a new label with a distinct name and a description that
   distinguishes it.
6. Humans reconcile. The Labels tab is the human's review surface. If you see
   labels that overlap, contradict, or lack descriptions, edit or remove them
   there. Agents follow the rules above; humans curate.

## First-time human sequence

1. atm tui (auto-inits the store)
2. Create the project (Add in the Projects tab). Project create auto-seeds the
   17 default labels with descriptions, so the Labels tab is populated from
   the start.
3. Create seed index tasks (context:agent, context:repository,
   context:documentation) and initial work tasks, labeling as you go. The
   human curates labels in the Labels tab.

## Agent first-contact sequence

1. atm conventions — read this guide, including the code-of-conduct.
2. atm label list --project <CODE> — read every label's description first to
   understand the project's vocabulary before exploring tasks. Labels are the
   project's language; knowing them makes every task query meaningful.
3. task list --project <CODE> --label <CODE>:context:agent — get agent
   directions for working in this project.
4. task list --project <CODE> --label <CODE>:context:repository /
   :context:documentation — discover repository pointers and documentation.
5. task list --project <CODE> --label <CODE>:status:open — get open work.

A fresh agent that does not yet know the project's namespaces runs the
label-list step first and follows the descriptions.

## Notes

- Plugins/skills: ATM ships only the doc + the conventions command. Plugins or
  agent skills may wrap the first-contact sequence; ATM itself has no plugin
  mechanism.
- Re-seeding defaults: ` + "`atm label seed --project <CODE>`" + ` or the Labels tab [S] key
  re-applies the default set idempotently — existing descriptions are
  preserved, and any new defaults introduced in a release are added.

Conventions are advisory only — nothing in the store validates or
special-cases the documented namespaces.
`

func conventionsStructured() map[string]any {
	namespaces := []map[string]string{
		{"namespace": "status:", "examples": "open, todo, in-progress, done, blocked, review", "purpose": "workflow states — labels only, no state machine"},
		{"namespace": "type:", "examples": "bug, feature, task, chore", "purpose": "task categorization"},
		{"namespace": "priority:", "examples": "high, medium, low", "purpose": "optional prioritization"},
		{"namespace": "context:documentation", "examples": "ATM:context:documentation", "purpose": "the labeled task contains documentation about the project"},
		{"namespace": "context:repository", "examples": "ATM:context:repository", "purpose": "the labeled task contains a pointer to a code repository"},
		{"namespace": "context:agent", "examples": "ATM:context:agent", "purpose": "agent direction when navigating the project"},
		{"namespace": "context:fixit", "examples": "ATM:context:fixit", "purpose": "something on this task should be reviewed, updated, or altered"},
		{"namespace": "repo:<name>", "examples": "ATM:repo:atm", "purpose": "index task pointing at a repo — created on demand, not seeded"},
		{"namespace": "doc:<name>", "examples": "ATM:doc:architecture", "purpose": "index task pointing at a doc/resource — created on demand, not seeded"},
		{"namespace": "claimed-by:<agent>", "examples": "ATM:claimed-by:claude", "purpose": "who's working on what — last-writer-wins, no conflict detection"},
		{"namespace": "blocks:<ID>, related:<ID>", "examples": "ATM:blocks:ATM-0002", "purpose": "task relationships via labels — created on demand, not seeded"},
	}
	codeOfConduct := []string{
		"Read before you write. Run atm label list --project <CODE> and read every label's description before introducing any new label.",
		"Default setup is the baseline. Prefer seeded labels; do not reinvent them under new names.",
		"Invent only when nothing fits. Agents are free to self-organize, but a human reviewing the Labels tab should understand why a new label exists.",
		"State the intention in the label description. A label with no description is a flag for human review.",
		"One label, one meaning. If intent diverges from an existing label's description, create a new label with a distinct name.",
		"Humans reconcile. The Labels tab is the human's review surface for overlapping, contradictory, or undescribed labels.",
	}
	seeded := make([]map[string]string, 0, len(seed.Labels))
	for _, l := range seed.Labels {
		seeded = append(seeded, map[string]string{"suffix": l.Suffix, "description": l.Description})
	}
	return map[string]any{
		"version": conventionsVersion,
		"namespaces": namespaces,
		"code_of_conduct": codeOfConduct,
		"seeded_labels": seeded,
		"first_time_human_sequence": []string{
			"atm tui (auto-inits the store)",
			"create the project (Add in the Projects tab); project create auto-seeds the 17 default labels with descriptions",
			"create seed index tasks (context:agent, context:repository, context:documentation) and initial work tasks, labeling as you go",
		},
		"agent_first_contact_sequence": []string{
			"atm conventions — read this guide, including the code-of-conduct",
			"atm label list --project <CODE> — read every label's description first to understand the project's vocabulary before exploring tasks",
			"task list --project <CODE> --label <CODE>:context:agent — get agent directions for working in this project",
			"task list --project <CODE> --label <CODE>:context:repository / :context:documentation — discover repository pointers and documentation",
			"task list --project <CODE> --label <CODE>:status:open — get open work",
		},
		"advisory": "Conventions are advisory only — nothing in the store validates or special-cases the documented namespaces.",
	}
}

func newConventionsCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conventions",
		Short: "Print the onboarding guide and suggested label namespaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"conventions": conventionsStructured()})
			}
			fmt.Fprint(st.stdout(), conventionsText)
			return nil
		},
	}
	return cmd
}
```

- [ ] **Step 4: Regenerate the golden files**

Run: `go test ./internal/cli -run 'TestConventions' -update`
Then run again without `-update` to confirm they match:
Run: `go test ./internal/cli -run 'TestConventions' -v`
Expected: PASS.

- [ ] **Step 5: Run the full CLI test suite — expect determinism + harness goldens to break**

Run: `go test ./internal/cli/...`
Expected: FAILURES in `TestDeterminismByteIdentical` and any golden that captures a label-list / task-list after `CreateProject` (the seeded labels now appear). These are fixed in Task 7 (golden regeneration + fixture updates). For now, commit the conventions rewrite; the broken tests are expected and will be resolved by Task 7.

Actually — to keep the tree green per the AGENTS.md "make verify must be green before each commit" rule, we must fix the fixtures in the same commit as the conventions rewrite if conventions touches the seed scenario. But conventions doesn't touch the seed scenario; the determinism/golden failures come from Task 3's `CreateProject` seeding, which we already committed. So those failures exist NOW, before this task. We must fix them before this task can commit green.

**Correction:** Task 3's commit left `make verify` red (the CLI goldens break because `CreateProject` now seeds). Per the plan's ordering, Task 3 should have included the CLI fixture/golden updates. Re-scope: fold the CLI fixture + golden regeneration into Task 3's commit. Return to Task 3 and complete Step 7 below before proceeding.

- [ ] **Step 6: (Belongs to Task 3) Fix CLI test fixtures for auto-seeding**

Edit `internal/cli/harness_test.go` `seedScenario1`. The current fixture adds `ATM:type:epic`, `ATM:type:bug`, `ATM:status:open` and creates tasks with `ATM:type:bug`, `ATM:status:open`, `ATM:context:start-here`. After auto-seeding, `ATM:status:open` and `ATM:type:bug` already exist (seeded); `ATM:type:epic` is a non-seeded addition; `ATM:context:start-here` no longer exists as a seed label (replaced by `context:agent`/`context:documentation`/`context:repository`/`context:fixit`). Replace `ATM:context:start-here` usage with a seeded label or a custom one. To keep the scenario's intent (a task with a context label), use `ATM:context:agent`:

```go
func (h *goldenHarness) seedScenario1() {
	h.t.Helper()
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "claude")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "claude")
	h.run("label", "add", "--store", sp, "--name", "ATM:type:epic", "--actor", "claude")
	h.run("label", "add", "--store", sp, "--name", "ATM:type:bug", "--description", "Bug fix", "--actor", "claude")
	h.run("label", "add", "--store", sp, "--name", "ATM:status:open", "--actor", "claude")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Fix label reconciliation",
		"--label", "ATM:type:bug", "--label", "ATM:status:open", "--actor", "claude")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Seed index tasks",
		"--label", "ATM:context:agent", "--actor", "claude")
	h.reset()
}
```

Edit `internal/cli/determinism_test.go` `seedDeterminismStore`: replace the `ATM:context:start-here` task label with `ATM:context:agent`:

```go
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Seed index tasks",
		"--label", "ATM:context:agent", "--actor", "claude")
```

The `label add` calls for `ATM:type:epic`, `ATM:type:bug`, `ATM:status:open`, `DEMO:status:open` become upserts (no-ops for the seeded ones, keep `ATM:type:epic` as a custom add). Leave them — they're idempotent.

- [ ] **Step 7: (Belongs to Task 3) Regenerate ALL CLI goldens**

Run: `go test ./internal/cli -update`
Then verify:
Run: `go test ./internal/cli/... -v`
Expected: PASS (all goldens regenerated to include seeded labels).

- [ ] **Step 8: (Belongs to Task 3) Amend the Task 3 commit**

Since Task 3 was already committed with broken CLI tests, amend it to include the fixture + golden updates:

```bash
git add internal/cli/harness_test.go internal/cli/determinism_test.go internal/cli/testdata/golden/
git commit --amend --no-edit
```

Now `make verify` is green after Task 3. Return to Task 4.

- [ ] **Step 9: Verify conventions tests pass and commit Task 4**

Run: `make verify`
Expected: PASS.

```bash
git add internal/cli/conventions.go internal/cli/conventions_test.go internal/cli/testdata/golden/conventions-text.json internal/cli/testdata/golden/conventions-json.json internal/cli/testdata/golden/determinism-conventions.json
git commit -m "cli: rewrite conventions — agent code-of-conduct, context:* relabel, seeded_labels JSON, v2.1"
```

---

## Task 5: Add `atm label seed --project <CODE>`

**Files:**
- Modify: `internal/cli/label.go`
- Test: `internal/cli/label_test.go`, golden file `label-seed.json`.

**Interfaces:**
- Consumes: `store.SeedLabels` (from Task 2).
- Produces: a new cobra subcommand `atm label seed --project <CODE> [--actor <id>]`; text output `seeded 17 labels into <CODE>`; JSON `{"project":"<CODE>","seeded":17,"labels":[...17 full names...]}`.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/label_test.go`:

```go
func TestGoldenLabelSeed(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "claude")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "claude")
	// Remove one seed label, then re-seed to confirm idempotency.
	h.run("label", "remove", "--store", sp, "--name", "ATM:context:fixit", "--actor", "claude")
	out, _, code := h.run("label", "seed", "--store", sp, "--project", "ATM", "--actor", "claude")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"seeded": 17`) {
		t.Fatalf("missing seeded: 17 in JSON output: %s", out)
	}
	if !strings.Contains(out, `"ATM:context:fixit"`) {
		t.Fatalf("missing ATM:context:fixit in seed output: %s", out)
	}
	compareGolden(t, "label-seed", out)
}

func TestLabelSeedTextOutput(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "claude")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "claude")
	out, _, code := h.run("label", "seed", "--store", sp, "--project", "ATM", "--actor", "claude")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, "seeded 17 labels into ATM") {
		t.Fatalf("text output missing 'seeded 17 labels into ATM': %s", out)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli -run 'TestGoldenLabelSeed|TestLabelSeedTextOutput' -v`
Expected: FAIL — unknown command `seed`.

- [ ] **Step 3: Implement the subcommand**

Add to `internal/cli/label.go`. First add `"atm/internal/seed"` to the imports, and `"sort"` if not present (it isn't). Then register the subcommand in `newLabelCmd`:

```go
func newLabelCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "label",
		Short: "Label registry commands",
	}
	cmd.AddCommand(newLabelAddCmd(st))
	cmd.AddCommand(newLabelRemoveCmd(st))
	cmd.AddCommand(newLabelListCmd(st))
	cmd.AddCommand(newLabelShowCmd(st))
	cmd.AddCommand(newLabelSeedCmd(st))
	return cmd
}
```

Append the new command function at the end of `label.go`:

```go
func newLabelSeedCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Apply the default seed labels to a project (idempotent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SeedLabels(project, actor); err != nil {
				return err
			}
			names := make([]string, 0, len(seed.Labels))
			for _, l := range seed.Labels {
				names = append(names, project+":"+l.Suffix)
			}
			sort.Strings(names)
			return st.emit(st.stdout(), map[string]any{
				"project": project,
				"seeded":  len(seed.Labels),
				"labels":  names,
			}, func() {
				fmt.Fprintf(os.Stdout, "seeded %d labels into %s\n", len(seed.Labels), project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code to seed")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
```

The imports block of `label.go` becomes:

```go
import (
	"fmt"
	"os"
	"sort"

	"atm/internal/seed"

	"github.com/spf13/cobra"
)
```

- [ ] **Step 4: Generate the golden and run the test**

Run: `go test ./internal/cli -run 'TestGoldenLabelSeed' -update`
Run: `go test ./internal/cli -run 'TestGoldenLabelSeed|TestLabelSeedTextOutput' -v`
Expected: PASS.

- [ ] **Step 5: Run full CLI suite + verify**

Run: `make verify`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/label.go internal/cli/label_test.go internal/cli/testdata/golden/label-seed.json
git commit -m "cli: add atm label seed --project (idempotent default-label apply)"
```

---

## Task 6: Drop the Projects-tab label section (TUI)

**Files:**
- Modify: `internal/tui/projects.go`
- Modify: `internal/tui/app.go` (form kind comments only — `formLabelAdd`/`formLabelRemove` stay but move to labels.go in Task 8; for now just keep them so the build doesn't break)
- Test: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: existing `labelSuffixRe`, form machinery.
- Produces: project detail view with no LABELS section; `L`/`l` keys are no-ops in project detail; `openLabelAddForm`/`openLabelRemoveForm`/`doLabelAdd`/`doLabelRemove` removed from `projects.go` (they will be re-added in `labels.go` in Task 8 — but to keep the build green, we move them to `labels.go` in the same commit as this task, or keep them temporarily and remove in Task 8. Simplest: move them now into a new `internal/tui/labels.go` stub that just holds these helpers, and Task 8 builds the full Labels tab on top).

Decision: create `internal/tui/labels.go` in this task as a minimal file holding the relocated helpers (`openLabelAddForm`, `openLabelRemoveForm`, `doLabelAdd`, `doLabelRemove`, `labelSuffixRe` moved from `projects.go`). The full Labels tab model is added in Task 8.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/app_test.go` (and update existing tests). First, update `TestProjectDetailLabelsGrouped` to assert the LABELS section is GONE:

```go
// TestProjectDetailNoLabelsSection verifies the project detail no longer
// renders a LABELS section (label management moved to the Labels tab).
func TestProjectDetailNoLabelsSection(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedLabel(t, m, "ATM:custom:x", "custom")
	update(t, m, "enter") // open ATM detail (cursor on ATM)
	v := m.View()
	mustContain(t, v, "PROJECT")
	mustNotContain(t, v, "LABELS")
	// The labels count fact line stays.
	mustContain(t, v, "labels")
}

// TestProjectDetailLabelKeysNoOp verifies L and l do nothing in project detail.
func TestProjectDetailLabelKeysNoOp(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "enter")
	update(t, m, "L")
	update(t, m, "l")
	if m.form != nil {
		t.Errorf("L/l opened a form in project detail (should be a no-op)")
	}
}
```

Remove the old `TestProjectDetailLabelsGrouped` (it asserted the labels section exists). Also update `TestLabelAddFormValidation`, `TestLabelAddFormUpsert`, `TestLabelRemoveFormRetainedUsage` — these currently drive label add/remove via the Projects detail `L`/`l` keys. They will move to the Labels tab in Task 8. For this task, delete them from `app_test.go` (Task 8 re-creates them as `labels_test.go`). If `make verify` breaks because the tests reference removed helpers, that's expected — they're reinstated in Task 8.

Actually — deleting tests and re-adding them in Task 8 leaves a window where coverage drops but the build stays green. That's acceptable for an incremental plan. Delete the three label-form tests from `app_test.go` now.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui -run 'TestProjectDetailNoLabelsSection|TestProjectDetailLabelKeysNoOp' -v`
Expected: FAIL — `LABELS` still present; `L` still opens a form.

- [ ] **Step 3: Move label helpers to `internal/tui/labels.go`**

Create `internal/tui/labels.go` with the relocated helpers and `labelSuffixRe`. Cut from `projects.go`: the `labelSuffixRe` var (line 380), `openLabelAddForm` (line 382), `openLabelRemoveForm` (line 407), `doLabelAdd` (line 475), `doLabelRemove` (line 489). Paste into `labels.go`, adjusting receivers: `openLabelAddForm`/`openLabelRemoveForm` were on `*projectsModel`; they need a receiver that has `m *Model`. Make them `*Model` methods or stand-alone functions taking `m *Model`. Simplest: make them methods on `*Model`:

```go
package tui

import (
	"fmt"
	"regexp"

	"github.com/charmbracelet/bubbletea"
)

// labelSuffixRe validates the suffix the user types in the label add/remove
// forms. The fixed "<CODE>:" prefix is prepended by the form submit handler,
// so the suffix is "<namespace>:<value>" or "<tag>" with NO leading colon.
var labelSuffixRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(:[a-z0-9][a-z0-9-]*)?$`)

// openLabelAddForm opens the add-label form bound to the given project code.
// Used by the Labels tab (Task 8).
func (m *Model) openLabelAddForm(code string) {
	validator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>, e.g. status:open")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Hint: "<namespace>:<value> or <tag>, e.g. status:open", Validator: validator},
	}
	f := NewForm(fmt.Sprintf("Add label  %s:", code), fields)
	f.Title = fmt.Sprintf("Add label  %s:", code)
	m.form = f
	m.formKind = formLabelAdd
	m.formPayload = code
}

// openLabelRemoveForm opens the remove-label form bound to the given project code.
func (m *Model) openLabelRemoveForm(code string) {
	validator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Hint: "<namespace>:<value> or <tag>", Validator: validator},
	}
	f := NewForm(fmt.Sprintf("Remove label  %s:", code), fields)
	f.Title = fmt.Sprintf("Remove label  %s:", code)
	m.form = f
	m.formKind = formLabelRemove
	m.formPayload = code
}

// doLabelAdd handles submit of the add-label form.
func (m *Model) doLabelAdd(vals map[string]string) tea.Cmd {
	code := m.formPayload
	suffix := vals["name"]
	full := code + ":" + suffix
	if err := m.store.LabelAdd(full, vals["desc"], m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	return nil
}

// doLabelRemove handles submit of the remove-label form.
func (m *Model) doLabelRemove(vals map[string]string) tea.Cmd {
	code := m.formPayload
	suffix := vals["name"]
	full := code + ":" + suffix
	res, err := m.store.LabelRemove(full, m.actor)
	if err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.showToast(fmt.Sprintf("removed label %s (retained usage: %d)", full, res.RetainedUsage))
	m.refreshAll()
	return nil
}
```

Note: the old `doLabelAdd` called `m.projects.openDetail(code)` at the end — remove that (no project detail reopen in the Labels tab flow). The old `doLabelRemove` did the same — remove.

- [ ] **Step 4: Edit `projects.go` to drop the labels section and keys**

In `internal/tui/projects.go`:

(a) `handleDetailKey` — remove `case "L":` and `case "l":` (lines 135-138).

(b) `renderDetail` — delete the entire LABELS block: the `"LABELS\n"` write, its separator, the namespace grouping loop, and the tags block (approximately lines 189-224). Keep the `labels    %d` fact line (line 184) — it stays as a count.

(c) `statusHint` detail case — change from `"[N]name [L]add label [l]remove label [H]history [x]remove [Esc]back"` to `"[N]name [H]history [x]remove [Esc]back"`.

(d) Remove `openLabelAddForm`, `openLabelRemoveForm`, `doLabelAdd`, `doLabelRemove`, and the `labelSuffixRe` var from `projects.go` (they are now in `labels.go`).

- [ ] **Step 5: Verify the build**

Run: `go build ./...`
Expected: SUCCESS.

- [ ] **Step 6: Run tests to verify**

Run: `go test ./internal/tui -run 'TestProjectDetailNoLabelsSection|TestProjectDetailLabelKeysNoOp' -v`
Expected: PASS.

Run: `go test ./internal/tui/...`
Expected: FAILURES only in tests that referenced `L`/`l` in project detail (we deleted those in Step 1) — confirm no other regressions.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/projects.go internal/tui/labels.go internal/tui/app_test.go
git commit -m "tui: drop Labels section from Projects detail; relocate label helpers to labels.go"
```

---

## Task 7: Add the `labels` field to the task-create form

**Files:**
- Modify: `internal/tui/tasks.go`
- Test: `internal/tui/tasks_test.go`

**Interfaces:**
- Consumes: `labelSuffixRe` (from Task 6's `labels.go`), `store.CreateTask`.
- Produces: task-create form with a third `labels` field; `doTaskCreate` parses space-separated suffixes into a `[]string` of full names and passes them to `CreateTask`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/tasks_test.go` (check the file exists and its package/import style first; mirror it):

```go
func TestTaskCreateWithLabelsField(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")    // select ATM
	update(t, m, "2")    // Tasks tab
	update(t, m, "a")    // open create form
	if m.form == nil {
		t.Fatalf("create form not open")
	}
	// Verify the labels field exists.
	found := false
	for _, f := range m.form.Fields {
		if f.Label == "labels" {
			found = true
		}
	}
	if !found {
		t.Fatalf("create form has no 'labels' field; fields = %+v", m.form.Fields)
	}
	// Type a title.
	for _, r := range "Multi-label task" {
		update(t, m, string(r))
	}
	update(t, m, "tab") // title -> description
	// Skip description (leave empty), tab to labels.
	update(t, m, "tab") // description -> labels
	for _, r := range "status:open type:bug" {
		update(t, m, string(r))
	}
	update(t, m, "enter") // submit (last field)
	if m.form != nil {
		t.Fatalf("form should be closed after submit")
	}
	// The task should exist with both labels.
	ts := m.store.ListTasks(store.QueryFilters{Project: "ATM"})
	if len(ts) != 1 {
		t.Fatalf("expected 1 task, got %d", len(ts))
	}
	tk := ts[0]
	if !containsLabel(tk.Labels, "ATM:status:open") || !containsLabel(tk.Labels, "ATM:type:bug") {
		t.Fatalf("task labels = %v, want ATM:status:open + ATM:type:bug", tk.Labels)
	}
	// Both labels should be in the registry (auto-registered). ATM:status:open
	// and ATM:type:bug are seeded, so they already exist; the test confirms
	// CreateTask accepted them.
}
```

Note: `tasks_test.go` may not import `store` — check and add `"atm/internal/store"` to its imports if needed. The helper `containsLabel` lives in `internal/store/label_test.go` (store package) and is NOT accessible from the `tui` package. Add a local helper in `tasks_test.go`:

```go
func containsLabelTUI(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}
```

Use `containsLabelTUI` in the test above.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui -run 'TestTaskCreateWithLabelsField' -v`
Expected: FAIL — no `labels` field in the form.

- [ ] **Step 3: Add the `labels` field to `openCreateForm`**

Edit `internal/tui/tasks.go` `openCreateForm` (around line 914). Add a labels-field validator and a third field:

```go
func (t *tasksModel) openCreateForm() {
	labelsValidator := func(field, value string) error {
		if value == "" {
			return nil
		}
		for _, tok := range strings.Fields(value) {
			if !labelSuffixRe.MatchString(tok) {
				return fmt.Errorf("bad label %q: use <namespace>:<value> or <tag>", tok)
			}
		}
		return nil
	}
	fields := []formField{
		{Label: "title", Required: true, Hint: "task title"},
		{Label: "description", Required: false, Hint: "optional; multi-line later"},
		{Label: "labels", Required: false, Hint: "space-separated suffixes, e.g. 'status:open type:bug' (prefix auto-added)", Validator: labelsValidator},
	}
	f := NewForm("New task  "+t.m.projectScope+":", fields)
	f.Title = "New task  " + t.m.projectScope + ":"
	t.m.form = f
	t.m.formKind = formTaskCreate
}
```

Ensure `strings` is imported in `tasks.go` (it already is).

- [ ] **Step 4: Parse the labels field in `doTaskCreate`**

Edit `internal/tui/tasks.go` `doTaskCreate` (around line 1006):

```go
func (m *Model) doTaskCreate(vals map[string]string) tea.Cmd {
	title := vals["title"]
	desc := vals["description"]
	var labels []string
	for _, tok := range strings.Fields(vals["labels"]) {
		labels = append(labels, m.projectScope+":"+tok)
	}
	tk, err := m.store.CreateTask(m.projectScope, title, desc, labels, m.actor)
	if err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	if tk != nil {
		m.tasks.openDetail(tk.ID)
	}
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/tui -run 'TestTaskCreateWithLabelsField' -v`
Expected: PASS.

- [ ] **Step 6: Run full TUI suite + verify**

Run: `make verify`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/tasks.go internal/tui/tasks_test.go
git commit -m "tui: task-create form collects multiple labels (space-separated suffixes)"
```

---

## Task 8: Build the Labels tab

**Files:**
- Modify: `internal/tui/labels.go` (expand from Task 6's stub)
- Modify: `internal/tui/app.go` (4 tabs, `paneLabels`, `formLabelDescribe`, routing)
- Modify: `internal/tui/keymap.go`
- Modify: `internal/tui/help.go` (parity table, conventions text)
- Test: `internal/tui/labels_test.go` (new)
- Test: `internal/tui/app_test.go` (update tab-switching tests for 4 tabs)

**Interfaces:**
- Consumes: `store.LabelList`, `store.LabelAdd`, `store.LabelRemove`, `store.LabelShow`, `store.LabelUsage`, `store.Namespaces`, `store.SeedLabels`, `labelSuffixRe`, `Form`, form kinds.
- Produces: `labelsModel` with list + detail views; `[a]`/`[d]`/`[l]`/`[S]`/`Enter` keys; `formLabelDescribe` form kind + `doLabelDescribe` handler; 4-tab TUI shell.

This is the largest task. Break it into sub-steps.

- [ ] **Step 1: Write the failing tests for the Labels tab**

Create `internal/tui/labels_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

// --- Labels tab tests ---

func TestLabelsTabEmptyStateNoProject(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "3") // switch to Labels tab
	if m.focused != paneLabels {
		t.Fatalf("focus = %v want paneLabels", m.focused)
	}
	v := m.View()
	mustContain(t, v, "no project selected")
	mustContain(t, v, "press [s] in the Projects tab")
}

func TestLabelsTabListSeededLabels(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s") // select ATM
	update(t, m, "3") // Labels tab
	v := m.View()
	// Namespace headings for seeded namespaces.
	mustContain(t, v, "context:")
	mustContain(t, v, "status:")
	mustContain(t, v, "type:")
	mustContain(t, v, "priority:")
	// A seeded label's description is rendered.
	mustContain(t, v, "workflow state: open")
}

func TestLabelsTabAddLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	update(t, m, "a") // add label form
	if m.form == nil {
		t.Fatalf("add-label form not open")
	}
	for _, r := range "patch:urgent" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	if m.form != nil {
		t.Fatalf("form should be closed after submit")
	}
	// The label is now in the registry.
	if _, err := m.store.LabelShow("ATM:patch:urgent"); err != nil {
		t.Errorf("ATM:patch:urgent not in registry after add: %v", err)
	}
}

func TestLabelsTabDescribeLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	update(t, m, "d") // describe form
	if m.form == nil {
		t.Fatalf("describe form not open")
	}
	// First field is the label name (suffix).
	for _, r := range "status:open" {
		update(t, m, string(r))
	}
	update(t, m, "tab")
	for _, r := range "curated description" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	l, _ := m.store.LabelShow("ATM:status:open")
	if l.Description != "curated description" {
		t.Fatalf("description = %q want \"curated description\"", l.Description)
	}
}

func TestLabelsTabRemoveLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	// Attach the label to a task so retained_usage > 0.
	seedTask(t, m, "ATM", "t", "ATM:status:open")
	update(t, m, "s")
	update(t, m, "3")
	update(t, m, "l") // remove form
	if m.form == nil {
		t.Fatalf("remove form not open")
	}
	for _, r := range "status:open" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	if !strings.Contains(m.toastMsg, "retained usage") {
		t.Fatalf("toast = %q, want retained usage", m.toastMsg)
	}
}

func TestLabelsTabSeedKey(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	// Remove a seed label.
	_, _ = m.store.LabelRemove("ATM:context:fixit", "claude")
	m.refreshAll()
	update(t, m, "s")
	update(t, m, "3")
	update(t, m, "S") // seed key
	if !strings.Contains(m.toastMsg, "seeded 17 labels into ATM") {
		t.Fatalf("toast = %q, want seeded 17 labels into ATM", m.toastMsg)
	}
	// The removed label is back.
	if _, err := m.store.LabelShow("ATM:context:fixit"); err != nil {
		t.Errorf("ATM:context:fixit not restored after seed: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui -run 'TestLabelsTab' -v`
Expected: FAIL — `paneLabels` undefined; `labelsModel` undefined; key `3` still maps to Help.

- [ ] **Step 3: Add the `labelsModel` to `internal/tui/labels.go`**

Append the model, list/detail rendering, and key handling to `labels.go` (after the helpers from Task 6). This is the bulk of the task.

```go
type labelsModel struct {
	m       *Model
	rows    []labelRow
	cursor  int
	offset  int
	view    lView
	detail  labelDetailState
}

type lView int

const (
	lViewList lView = iota
	lViewDetail
)

type labelRow struct {
	suffix      string
	full        string
	description string
	usage       int
}

type labelDetailState struct {
	row labelRow
}

func newLabelsModel(m *Model) labelsModel {
	return labelsModel{m: m}
}

func (l *labelsModel) SetSize(w, h int) {
	_ = w
	_ = h
}

func (l *labelsModel) refresh() {
	l.rows = nil
	if l.m.projectScope == "" {
		return
	}
	ls := l.m.store.LabelList(l.m.projectScope, "")
	for _, lab := range ls {
		usage, _ := l.m.store.LabelUsage(l.m.projectScope, lab.Name)
		suffix := strings.TrimPrefix(lab.Name, l.m.projectScope+":")
		l.rows = append(l.rows, labelRow{
			suffix:      suffix,
			full:        lab.Name,
			description: lab.Description,
			usage:       usage,
		})
	}
	if l.cursor >= len(l.rows) && len(l.rows) > 0 {
		l.cursor = len(l.rows) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

func (l *labelsModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch l.view {
	case lViewList:
		return l.handleListKey(k)
	case lViewDetail:
		return l.handleDetailKey(k)
	}
	return nil
}

func (l *labelsModel) handleListKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		if l.cursor < len(l.rows)-1 {
			l.cursor++
		}
	case "k", "up":
		if l.cursor > 0 {
			l.cursor--
		}
	case "g":
		l.cursor = 0
	case "a":
		if l.m.projectScope == "" {
			return nil
		}
		l.m.openLabelAddForm(l.m.projectScope)
	case "d":
		if l.m.projectScope == "" {
			return nil
		}
		l.m.openLabelDescribeForm()
	case "l":
		if l.m.projectScope == "" {
			return nil
		}
		l.m.openLabelRemoveForm(l.m.projectScope)
	case "S":
		if l.m.projectScope == "" {
			return nil
		}
		return l.seedDefaults()
	case "enter":
		if r, ok := l.selected(); ok {
			l.detail = labelDetailState{row: r}
			l.view = lViewDetail
		}
	}
	return nil
}

func (l *labelsModel) handleDetailKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
	case "k", "up":
	case "d":
		l.m.openLabelDescribeFormFor(l.detail.row.suffix)
	case "l":
		l.m.openLabelRemoveForm(l.m.projectScope)
		// Pre-fill would require form.Value support; the user retypes.
	case "esc":
		l.view = lViewList
	}
	return nil
}

func (l *labelsModel) selected() (labelRow, bool) {
	if l.cursor < 0 || l.cursor >= len(l.rows) {
		return labelRow{}, false
	}
	return l.rows[l.cursor], true
}

func (l *labelsModel) seedDefaults() tea.Cmd {
	if err := l.m.store.SeedLabels(l.m.projectScope, l.m.actor); err != nil {
		l.m.showToast("error: " + err.Error())
		return nil
	}
	l.m.showToast(fmt.Sprintf("seeded %d labels into %s", lenSeedLabels(), l.m.projectScope))
	l.m.refreshAll()
	return nil
}

// lenSeedLabels returns the seed-label count without importing the seed
// package into the TUI (the store already imports it; we mirror the count
// via the store's SeedLabels output). To avoid a tui->seed dependency, we
// hard-count from the registry after a seed by re-reading, OR we import
// seed here. The spec allows tui to import seed (it's a leaf data pkg).
// Importing seed is cleanest.
```

**Decision on the seed count:** Import `atm/internal/seed` in `labels.go` for `len(seed.Labels)`. It's a leaf data package with no store import, so no cycle. Add it to the imports of `labels.go` and replace `lenSeedLabels()` with `len(seed.Labels)`.

Continue the labels.go additions:

```go
func (l *labelsModel) View() string {
	switch l.view {
	case lViewList:
		return l.renderList()
	case lViewDetail:
		return l.renderDetail()
	}
	return ""
}

func (l *labelsModel) renderList() string {
	var b strings.Builder
	if l.m.projectScope == "" {
		lines := []string{
			emptyHeadStyle.Render("no project selected"),
			"",
			emptyTextStyle.Render(fmt.Sprintf("press %s in the Projects tab to scope this view", emptyKeyStyle.Render("[s]"))),
		}
		return padToHeight(centerLinesBoth(lines, l.m.width, l.m.contentHeight), l.m.contentHeight)
	}
	if len(l.rows) == 0 {
		return padToHeight("no labels", l.m.contentHeight)
	}
	// Group by namespace.
	byNS := map[string][]labelRow{}
	var tags []labelRow
	var nsOrder []string
	seenNS := map[string]bool{}
	for _, r := range l.rows {
		parts := strings.SplitN(r.suffix, ":", 2)
		if len(parts) == 2 {
			if !seenNS[parts[0]] {
				seenNS[parts[0]] = true
				nsOrder = append(nsOrder, parts[0])
			}
			byNS[parts[0]] = append(byNS[parts[0]], r)
		} else {
			tags = append(tags, r)
		}
	}
	sort.Strings(nsOrder)
	b.WriteString(headerLabelStyle.Render(fmt.Sprintf(" %-30s %8s  %s", "LABEL", "USAGE", "DESCRIPTION")))
	b.WriteString("\n")
	b.WriteString(sepLine("─", 78, l.m.width, 2))
	b.WriteString("\n")
	rowIdx := 0
	for _, ns := range nsOrder {
		fmt.Fprintf(&b, "%s:\n", ns)
		for _, r := range byNS[ns] {
			line := fmt.Sprintf(" %-30s %5d %s  %s", r.full, r.usage, pluralTasks(r.usage), r.description)
			if rowIdx == l.cursor {
				line = " " + rowCursorStyle.Render(strings.TrimPrefix(line, " "))
			} else {
				line = " " + line
			}
			b.WriteString(line)
			b.WriteString("\n")
			rowIdx++
		}
	}
	if len(tags) > 0 {
		b.WriteString("tags:\n")
		for _, r := range tags {
			line := fmt.Sprintf(" %-30s %5d %s  %s", r.full, r.usage, pluralTasks(r.usage), r.description)
			if rowIdx == l.cursor {
				line = " " + rowCursorStyle.Render(strings.TrimPrefix(line, " "))
			} else {
				line = " " + line
			}
			b.WriteString(line)
			b.WriteString("\n")
			rowIdx++
		}
	}
	return padToHeight(b.String(), l.m.contentHeight)
}

func (l *labelsModel) renderDetail() string {
	r := l.detail.row
	var b strings.Builder
	b.WriteString("LABEL\n")
	b.WriteString(sepLine("─", 78, l.m.width, 2))
	b.WriteString("\n")
	fmt.Fprintf(&b, "name        %s\n", r.full)
	fmt.Fprintf(&b, "usage       %d %s\n", r.usage, pluralTasks(r.usage))
	fmt.Fprintf(&b, "description %s\n", r.description)
	b.WriteString("\n")
	b.WriteString(keyMenuDimStyle.Render("[d]esc  [l]remove  [Esc]back"))
	return padToHeight(b.String(), l.m.contentHeight)
}

func (l *labelsModel) statusHint() string {
	if l.m.projectScope == "" {
		return "[?]keys"
	}
	if l.view == lViewDetail {
		return "[d]esc [l]remove [Esc]back"
	}
	return "[a]dd [d]esc [l]remove [S]eed [Enter]detail [?]keys"
}
```

Add the imports needed: `"sort"`, `"atm/internal/seed"`. The `labels.go` imports block becomes:

```go
import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"atm/internal/seed"

	"github.com/charmbracelet/bubbletea"
)
```

Add the describe-form openers (used by `[d]`):

```go
// openLabelDescribeForm opens a form with name + description fields. The
// user types the label suffix and a new description; submit calls LabelAdd
// (the upsert that overwrites the description).
func (m *Model) openLabelDescribeForm() {
	f := m.newLabelDescribeForm("", "")
	m.form = f
	m.formKind = formLabelDescribe
	m.formPayload = m.projectScope
}

// openLabelDescribeFormFor opens the describe form pre-filled with a known
// suffix and its current description (used from the label detail view).
func (m *Model) openLabelDescribeFormFor(suffix, currentDesc string) {
	f := m.newLabelDescribeForm(suffix, currentDesc)
	m.form = f
	m.formKind = formLabelDescribe
	m.formPayload = m.projectScope
}

func (m *Model) newLabelDescribeForm(suffix, desc string) *Form {
	nameValidator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Value: suffix, Hint: "<namespace>:<value> or <tag>", Validator: nameValidator},
		{Label: "description", Required: false, Value: desc, Hint: "new description (overwrites)"},
	}
	f := NewForm(fmt.Sprintf("Describe label  %s:", m.projectScope), fields)
	f.Title = fmt.Sprintf("Describe label  %s:", m.projectScope)
	return f
}

// doLabelDescribe handles submit of the describe-label form.
func (m *Model) doLabelDescribe(vals map[string]string) tea.Cmd {
	code := m.formPayload
	suffix := vals["name"]
	full := code + ":" + suffix
	desc := vals["description"]
	if err := m.store.LabelAdd(full, desc, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	return nil
}
```

- [ ] **Step 4: Wire the Labels tab into `app.go`**

Edit `internal/tui/app.go`:

(a) `workspacePane` const block — add `paneLabels` and bump `numPanes`:

```go
const (
	paneProjects workspacePane = iota
	paneTasks
	paneLabels
	paneHelp
)

const numPanes = 4
```

(b) `formAction` — add `formLabelDescribe`:

```go
const (
	formNone formAction = iota
	formProjectCreate
	formLabelAdd
	formLabelRemove
	formLabelDescribe
	formTaskCreate
	formTaskSetTitle
	formTaskSetDescription
	formTaskLabelAdd
	formTaskLabelRemove
	formProjectSetName
)
```

(c) `Model` struct — add `labels labelsModel`.

(d) `NewModel` — after `m.tasks = newTasksModel(m)` add `m.labels = newLabelsModel(m)`.

(e) `SetSize` — add `m.labels.SetSize(contentW, m.contentHeight)`.

(f) `refreshAll` — add `m.labels.refresh()`.

(g) `handleKey` — update tab switching: `3` → `paneLabels`, `4` → `paneHelp`. Add a Labels case to the pane dispatch:

```go
	switch k.String() {
	case "1":
		m.focused = paneProjects
		return nil
	case "2":
		m.focused = paneTasks
		return nil
	case "3":
		m.focused = paneLabels
		return nil
	case "4":
		m.focused = paneHelp
		return nil
	case "?":
		m.keymapOverlayOn = true
		return nil
	}
```

And in the Esc handling, add a Labels detail back-to-list:

```go
	if k.String() == "esc" {
		if m.focused == paneProjects && m.projects.view == pViewDetail {
			m.projects.backToList()
			return nil
		}
		if m.focused == paneTasks {
			if m.tasks.view == tViewDetail {
				m.tasks.backToList()
				return nil
			}
			if m.tasks.filterEditing {
				m.tasks.cancelFilterEdit()
				return nil
			}
		}
		if m.focused == paneLabels && m.labels.view == lViewDetail {
			m.labels.view = lViewList
			return nil
		}
		return nil
	}
```

Add the pane dispatch case:

```go
	switch m.focused {
	case paneProjects:
		return m.projects.handleKey(k)
	case paneTasks:
		return m.tasks.handleKey(k)
	case paneLabels:
		return m.labels.handleKey(k)
	case paneHelp:
		return m.help.handleKey(k)
	}
```

(h) `submitForm` — add `formLabelDescribe`:

```go
	case formLabelDescribe:
		return m.doLabelDescribe(vals)
```

(i) `renderTabBar` — names become `["Projects", "Tasks", "Labels", "Help"]`.

(j) `renderBody` — add `case paneLabels: return m.labels.View()`.

(k) `statusHint` — add `case paneLabels: return m.labels.statusHint()`.

- [ ] **Step 5: Update `keymap.go`**

Remove the `L/l` row's Projects column entry (set it to `"-"`) and add Labels-tab rows. Edit the `keymapRows` slice: the `L/l` row becomes `{"L/l", "-", "-", "-", "-", "-"}` (or remove it). Add new rows for the Labels tab. Since the table has columns `Key|Projects|Tasks|Help|Detail`, and Labels shares the same key column, add the Labels bindings into the existing rows where the key overlaps:

```go
var keymapRows = []keyEntry{
	{"1/2/3/4", "switch tab", "switch tab", "switch tab", "switch tab", "switch tab"},
	{"j/k", "move cursor", "move cursor", "move cursor", "scroll", "scroll"},
	{"g", "top of list", "top of list", "top of list", "top", "top"},
	{"Enter", "open detail", "open detail / toggle group", "open label detail", "-", "confirm overlay"},
	{"Esc", "back", "back / cancel filter", "back", "-", "back / cancel overlay"},
	{"/", "-", "edit filter", "-", "-", "-"},
	{"s", "select project", "cycle sort", "-", "-", "-"},
	{"S", "-", "-", "seed default labels", "-", "-"},
	{"a", "add project", "add task", "add label", "-", "-"},
	{"d", "-", "-", "describe label", "-", "edit description (task)"},
	{"l", "-", "-", "remove label", "-", "-"},
	{"x", "remove project (confirm)", "-", "-", "-", "remove task (confirm)"},
	{"e", "-", "-", "-", "-", "edit title (task)"},
	{"b/B", "-", "-", "-", "-", "add/remove label (task)"},
	{"N", "set name (project detail)", "-", "-", "-", "-"},
	{"H", "toggle history (project detail)", "-", "-", "-", "-"},
	{"?", "toggle keymap overlay", "toggle keymap overlay", "toggle keymap overlay", "-", "toggle keymap overlay"},
	{"q / ctrl+c", "quit", "quit", "quit", "quit", "quit"},
	{"PgDn/Space", "-", "next page", "next page", "next page", "scroll down"},
	{"PgUp", "-", "prev page", "prev page", "-", "-"},
}
```

This adds a `Labels` column. The `keyEntry` struct needs a `Labels` field. Edit `keymap.go`:

```go
type keyEntry struct {
	Key      string
	Projects string
	Tasks    string
	Labels   string
	Help     string
	Detail   string
}
```

Update `help.go` `keymapTable` to render the new column (widen the format string; adjust the separator width).

- [ ] **Step 6: Update `help.go`**

(a) `parityTable` — update the label rows to point at the Labels tab and add a seed row:

```
atm label add --name --desc           Labels tab  [a]dd / [d]esc
atm label remove --name               Labels tab  [l]
atm label seed --project              Labels tab  [S]
atm label list [--project] [--ns]     Labels tab  (list)
atm label show --name                 — (CLI only)
```

And the task-create row notes labels:

```
atm task create --project --title [--label]   Tasks tab  [a]dd (labels field)
```

(b) `conventionsTextTUI` — replace the whole var with the new conventions text mirroring `cli/conventionsText` (the code-of-conduct, new context labels, "read labels first" sequence). Copy the text from `conventions.go`'s `conventionsText` constant, prefixing each line with appropriate indentation to match the existing `conventionsTextTUI` style.

(c) `keymapTable` — update the format string to include the Labels column:

```go
func keymapTable() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-12s %-22s %-24s %-22s %-10s %-26s\n", "Key", "Projects", "Tasks", "Labels", "Help", "Detail")
	b.WriteString(strings.Repeat("-", min(100, 12+1+22+1+24+1+22+1+10+1+26)))
	b.WriteString("\n")
	for _, r := range keymapRows {
		fmt.Fprintf(&b, "%-12s %-22s %-24s %-22s %-10s %-26s\n", r.Key, r.Projects, r.Tasks, r.Labels, r.Help, r.Detail)
	}
	return b.String()
}
```

- [ ] **Step 7: Update `app_test.go` tab-switching tests**

Edit `TestTabSwitching`:

```go
func TestTabSwitching(t *testing.T) {
	m := newTestModel(t)
	if m.focused != paneProjects {
		t.Fatalf("default focus = %v want paneProjects", m.focused)
	}
	m = update(t, m, "2")
	if m.focused != paneTasks {
		t.Fatalf("after 2: focus = %v want paneTasks", m.focused)
	}
	m = update(t, m, "3")
	if m.focused != paneLabels {
		t.Fatalf("after 3: focus = %v want paneLabels", m.focused)
	}
	m = update(t, m, "4")
	if m.focused != paneHelp {
		t.Fatalf("after 4: focus = %v want paneHelp", m.focused)
	}
	m = update(t, m, "1")
	if m.focused != paneProjects {
		t.Fatalf("after 1: focus = %v want paneProjects", m.focused)
	}
}
```

Edit `TestTabBarShowsNumbers`:

```go
func TestTabBarShowsNumbers(t *testing.T) {
	m := newTestModel(t)
	bar := m.renderTabBar()
	for _, want := range []string{"1", "2", "3", "4", "Projects", "Tasks", "Labels", "Help"} {
		if !strings.Contains(bar, want) {
			t.Errorf("tab bar missing %q\nbar: %s", want, bar)
		}
	}
}
```

Edit `TestHelpTabReadOnly` keys to include `S` and `d`:

```go
	for _, k := range []string{"a", "x", "L", "l", "N", "H", "s", "S", "d"} {
```

Edit `TestHelpTabConventions` — replace the assertion `"Suggested seed namespaces"` if the new heading differs; the new `conventionsTextTUI` should still contain `"Conventions"`, `"advisory"`, and `"Suggested seed namespaces"` (keep that heading).

Edit any test that switches to Help via `3` — now Help is `4`. Grep `app_test.go` for `update(t, m, "3")` and change Help-tab switches to `4`:

```bash
# Inspect: rg 'update\(t, m, "3"\)' internal/tui/app_test.go
```

For each match where the intent is "switch to Help", change to `"4"`. The Labels-tab tests (new file) use `"3"` for Labels. Context: `TestHelpTabParityTable`, `TestHelpTabConventions`, `TestHelpTabReadOnly`, `TestHelpTabKeymap` all use `update(t, m, "3")` — change those to `"4"`.

- [ ] **Step 8: Build and run the Labels-tab tests**

Run: `go build ./...`
Expected: SUCCESS.

Run: `go test ./internal/tui -run 'TestLabelsTab' -v`
Expected: PASS.

Run: `go test ./internal/tui -run 'TestTabSwitching|TestTabBarShowsNumbers|TestHelpTab' -v`
Expected: PASS.

- [ ] **Step 9: Run the full suite + verify**

Run: `make verify`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/tui/labels.go internal/tui/labels_test.go internal/tui/app.go internal/tui/keymap.go internal/tui/help.go internal/tui/app_test.go
git commit -m "tui: add Labels tab (scoped list + detail, add/describe/remove/seed keys)"
```

---

## Task 9: Update `scripts/dogfood.sh` and `README.md`

**Files:**
- Modify: `scripts/dogfood.sh`
- Modify: `README.md`

**Interfaces:** None (documentation/script).

- [ ] **Step 1: Update `scripts/dogfood.sh`**

The `seed_labels` loop (lines 54-69) is now redundant — `project create` seeds. Remove it. The bootstrap task (line 74) uses `ATM:context:start-here` — replace with `ATM:context:agent`. Replace lines 52-69 with a comment noting that create auto-seeds, and update the tasks array:

```bash
# 3. labels are auto-seeded by project create (v2.1: 17 default labels with
#    descriptions). To re-apply defaults after an upgrade, run:
#      atm label seed --project ATM
#    (idempotent; preserves edited descriptions).

# 4. seed tasks (idempotent by title).
declare -a tasks=(
  "Bootstrap v2 store|ATM:status:open,ATM:type:task,ATM:context:agent"
  "Finish TUI parity with CLI|ATM:status:todo,ATM:type:task"
  "Document v2 conventions in README|ATM:status:todo,ATM:type:task"
  "Add cross-project label search|ATM:status:todo,ATM:type:feature"
)
```

Renumber the subsequent comment from `# 4.` to keep sequential (the file already had `# 4.` for tasks — now it stays `# 4.`).

- [ ] **Step 2: Update `README.md`**

Find the conventions section (around lines 134-179) and replace it with content mirroring the new `conventionsText`: the updated seed-namespace table (with `context:documentation`/`context:repository`/`context:agent`/`context:fixit` replacing `context:always-read`/`context:start-here`), the agent code-of-conduct, the "read labels first" first-contact sequence, and a note that project create auto-seeds the 17 defaults. Also update any mention of `atm label seed` not existing.

- [ ] **Step 3: Verify**

Run: `make verify`
Expected: PASS (README and dogfood.sh are not tested by `make verify`, but confirm no broken references).

Sanity-run the dogfood script against a temp store (optional, not gated):
```bash
ATM_HOME=/tmp/atm-dogfood-plan scripts/dogfood.sh bin/atm
```
Expected: completes, `task list --project ATM` shows the bootstrap task with `ATM:context:agent`.

- [ ] **Step 4: Commit**

```bash
git add scripts/dogfood.sh README.md
git commit -m "docs: update dogfood script and README for auto-seeding + new context labels"
```

---

## Self-Review

**1. Spec coverage:**

- Section 1 (Labels tab): Task 8.
- Section 2 (Projects tab loses labels): Task 6.
- Section 3 (Task create multi-label): Task 7.
- Section 4 (Seeding — `internal/seed`, `LabelSeed`/`SeedLabels`, `CreateProject` seeds, `atm label seed`, `[S]` key): Tasks 1, 2, 3, 5, 8.
- Section 5 (Conventions rewrite): Task 4.
- Section 6 (Help + keymap): Task 8 (Steps 5-6).
- Data model: unchanged — no task needed (correct).
- Store API additions: Task 2.
- CLI surface: Task 5.
- TUI surface: Tasks 6, 7, 8.
- Testing: each task carries its tests; golden regeneration in Tasks 4, 5, and (folded into Task 3) the fixture updates.
- Rollout: incremental commits per task, `make verify` green at each.
- Out of scope: no tasks (correct).

All spec sections covered.

**2. Placeholder scan:** No "TBD"/"TODO"/"implement later". Each code step has complete code. The "Belongs to Task 3" renumbering in Task 4 is explicit cross-task coordination, not a placeholder — it documents that the CLI fixture updates must land in Task 3's commit to keep `make verify` green. Verified each step has exact code or exact commands.

**3. Type consistency:**
- `seed.Label` struct `{Suffix, Description string}` — used consistently in Tasks 1, 2, 4, 5, 8.
- `Store.LabelSeed(name, description, actor) error` — defined Task 2, used Task 3.
- `Store.SeedLabels(code, actor) error` — defined Task 2, used Tasks 3, 5, 8.
- `formLabelDescribe` — defined Task 8 (Step 4b), used Task 8 (Step 4h).
- `labelsModel`, `labelRow`, `labelDetailState`, `lView` — defined Task 8 Step 3, used Task 8 Step 4 and `labels_test.go`.
- `paneLabels` — defined Task 8 Step 4a, used in tests and routing.
- `openLabelAddForm`/`openLabelRemoveForm` — moved to `*Model` methods in Task 6, used by Labels tab in Task 8.
- `labelSuffixRe` — moved to `labels.go` in Task 6, used in Tasks 7 and 8.

No signature drift detected.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-03-label-management-refinement.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?