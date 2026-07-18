# Capability Semantics Phase 2 — Per-Project Enablement, Hard Gate

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A project's enabled capability set is a per-project fact in the v2 event log, chosen at `atm project create`, editable via `atm project capability add|remove|list` and a TUI toggle; commands of capabilities a project has not enabled are **not mounted** for invocations targeting that project (fence on the tooling surface — the store still validates nothing).

**Architecture:** Two new membership events on the project entity (`project.capability-enabled|disabled`) fold into `ProjectState.Capabilities` / `core.Project.Capabilities` (`nil` = no capability event ever → legacy project → all built-ins enabled). The registry gains `Names()` and a `For(*core.Project)` filter. The CLI pre-parses argv (`--project`, `--task`/`--id` prefix, `ATM_PROJECT`) *before* building the cobra tree and mounts only the enabled capabilities; any resolution failure degrades open (mount everything). The TUI filters per selected project at runtime.

**Tech Stack:** Go, the `libs/eventsource` fold (slot/maximal-writer model), `internal/store/eventlog` authoring, cobra, bubbletea TUI.

**Spec:** `docs/superpowers/specs/2026-07-18-capability-semantics-initiative-design.md` (Phase 2 + doctrine sections). Ledger task: `ATM-b678b0`. **Prerequisite: Phase 1 merged** (guides + Describe exist).

## Global Constraints

- Depends on Phase 1 (`docs/superpowers/plans/2026-07-18-capability-semantics-phase1-describe.md`) being merged.
- Degrade, never reject: any failure while resolving the mount project (no store, unknown project, unreadable log) mounts the FULL registry. The gate only narrows on a successful read.
- The store validates nothing new: enable/disable events constrain command *mounting*, never what the store accepts. Hand-assigned labels of a disabled capability remain legal.
- Fold determinism: fold output never reads map iteration order — new fold code iterates the already-sorted `keys` slice (see `libs/eventsource/fold.go` Pass 2). `Capabilities` comes out sorted because slot keys are sorted.
- Action-table twins: the action literal must be added in BOTH `libs/eventsource/action.go` (exported) and `internal/store/eventlog/author.go` (unexported engine copies) — they are deliberately independent across the carve seam.
- Arch tests to keep green: `tests/arch/imports_test.go` — notably `TestOnlyEventlogImportsEventsourceLib` and the capability import rules. No new packages are created in this phase.
- Sync sites when touching registration: `cmd/atm/main.go:19` and `internal/cli/harness_test.go` `testRegistry()`.
- Goldens: regenerate with `go test ./internal/cli -update`; always re-run without the flag and inspect diffs.
- Commit style: `type(ATM-b678b0): message`, footer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: Eventsource — capability membership slots on the project entity

**Files:**
- Modify: `libs/eventsource/action.go`
- Modify: `libs/eventsource/fold.go`
- Test: `libs/eventsource/fold_capability_test.go` (new)

**Interfaces:**
- Consumes: existing slot machinery (`writesOf`, `member`, Pass 2 in `Fold`).
- Produces (exact, later tasks rely on these):
  - `ActionProjectCapabilityEnabled = "project.capability-enabled"`, `ActionProjectCapabilityDisabled = "project.capability-disabled"` (payload key `"capability"`, subject = the project entity, same shape as `project.name-changed`).
  - `ProjectState.Capabilities []string` — sorted; `nil` means "no capability event ever recorded"; non-nil-but-empty means "explicitly all disabled".
  - Field-prefix helpers `capabilityField(name string) string` / `isCapabilityField(field string) bool` with prefix `"capability!"` (the `!` cannot appear in a label name, so capability slots can never collide with label-membership slots).

- [ ] **Step 1: Write the failing test**

Create `libs/eventsource/fold_capability_test.go`. Mirror the event-construction helper the existing `fold` tests in this package use (open `libs/eventsource/fold_test.go` first; it has a helper that builds a `project.created` root plus child events — reuse it verbatim rather than inventing construction). The assertions to express, exactly:

```go
package eventsource

import (
	"reflect"
	"testing"
)

// Build (with this package's existing test event helpers):
//   e1 project.created         (code P, name P)
//   e2 project.capability-enabled  parents [e1], payload {"capability": "workflow"}
//   e3 project.capability-enabled  parents [e2], payload {"capability": "contextmap"}
//   e4 project.capability-disabled parents [e3], payload {"capability": "contextmap"}
// and fold. Then:

func TestCapabilityMembershipFolds(t *testing.T) {
	st := foldCapabilityFixture(t) // the fixture described above
	p := singleProject(t, st)
	if got, want := p.Capabilities, []string{"workflow"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Capabilities = %v, want %v", got, want)
	}
}

func TestNoCapabilityEventsMeansNil(t *testing.T) {
	// fold a set with only project.created
	st := foldProjectOnlyFixture(t)
	p := singleProject(t, st)
	if p.Capabilities != nil {
		t.Fatalf("Capabilities = %v, want nil (legacy project: no capability event ever)", p.Capabilities)
	}
}

func TestAllDisabledIsEmptyNotNil(t *testing.T) {
	// enable workflow then disable workflow
	st := foldEnableThenDisableFixture(t)
	p := singleProject(t, st)
	if p.Capabilities == nil || len(p.Capabilities) != 0 {
		t.Fatalf("Capabilities = %v, want non-nil empty (explicitly disabled)", p.Capabilities)
	}
}
```

Write `foldCapabilityFixture` / `foldProjectOnlyFixture` / `foldEnableThenDisableFixture` / `singleProject` in this test file on top of the package's existing event-builder helpers (they construct `*Event` values and call `FoldEvents`). `singleProject` asserts exactly one non-tombstoned project and returns it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./libs/eventsource -run TestCapability -v` (and `TestNoCapabilityEvents|TestAllDisabled`)
Expected: FAIL — `ActionProjectCapabilityEnabled` undefined.

- [ ] **Step 3: Implement**

(a) `libs/eventsource/action.go` — append inside the const block:

```go
	ActionProjectCapabilityEnabled  = "project.capability-enabled"
	ActionProjectCapabilityDisabled = "project.capability-disabled"
```

(b) `libs/eventsource/fold.go`:

Add near the slot-kind constants:

```go
// capabilityFieldPrefix namespaces capability membership slots on the
// project entity away from label membership slots. '!' cannot occur in a
// label name, so the two families can never collide.
const capabilityFieldPrefix = "capability!"

func capabilityField(name string) string  { return capabilityFieldPrefix + name }
func isCapabilityField(field string) bool { return strings.HasPrefix(field, capabilityFieldPrefix) }
```

In `writesOf`, add cases (next to `ActionProjectNameChanged`):

```go
	case ActionProjectCapabilityEnabled:
		for _, c := range e.PayloadStringOrList("capability") {
			out = append(out, w(e.Subject.ID, SlotMembership, capabilityField(c), "add"))
		}
	case ActionProjectCapabilityDisabled:
		for _, c := range e.PayloadStringOrList("capability") {
			out = append(out, w(e.Subject.ID, SlotMembership, capabilityField(c), "remove"))
		}
```

In `ProjectState`, add the field:

```go
type ProjectState struct {
	EntityMeta
	Code string
	Name string
	// Capabilities is the enabled capability set. nil = no capability event
	// was ever recorded (a legacy project — callers treat nil as "all
	// built-ins enabled"); non-nil empty = explicitly none. Sorted.
	Capabilities []string
}
```

In `Fold` Pass 2, handle capability slots BEFORE the computed-label check (capability fields are not label names; they must not be routed through `computed()` or task/comment membership). Replace the `if k.kind == SlotMembership { ... }` block with:

```go
		if k.kind == SlotMembership && isCapabilityField(k.field) {
			// Capability membership on the project entity. The presence of
			// ANY maximal writer marks the set as explicitly recorded
			// (non-nil), even when the resolution is "not a member".
			if p := st.Projects[k.entity]; p != nil {
				if p.Capabilities == nil {
					p.Capabilities = []string{}
				}
				if member(ws) {
					p.Capabilities = append(p.Capabilities, strings.TrimPrefix(k.field, capabilityFieldPrefix))
				}
			}
		} else if k.kind == SlotMembership {
			if computed(k.field) {
				continue
			}
			if member(ws) {
				switch {
				case st.Tasks[k.entity] != nil:
					st.Tasks[k.entity].Labels = append(st.Tasks[k.entity].Labels, k.field)
				case st.Comments[k.entity] != nil:
					st.Comments[k.entity].Labels = append(st.Comments[k.entity].Labels, k.field)
				}
			}
		}
```

Keep the contested-reporting block after it untouched (contested capability slots report like any slot; note the existing `continue` for computed labels also skips contested reporting for them — preserve exactly that behavior by keeping the `continue` where it is today).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./libs/eventsource -v`
Expected: PASS, including all pre-existing fold/DAG tests (determinism suites must stay green).

- [ ] **Step 5: Commit**

```bash
git add libs/eventsource/action.go libs/eventsource/fold.go libs/eventsource/fold_capability_test.go
git commit -m "feat(ATM-b678b0): fold capability membership slots on the project entity (nil=legacy, empty=explicit)"
```

---

### Task 2: Eventlog authoring + core types + store facade

**Files:**
- Modify: `internal/store/eventlog/author.go` (action constants)
- Modify: `internal/store/eventlog/changeset.go` (writer methods)
- Modify: `internal/store/eventlog/snapshot.go` (`projectFromV2`)
- Modify: `internal/core/types.go` (`Project.Capabilities`)
- Modify: `internal/core/repository.go` (`ProjectWriter`)
- Modify: `internal/core/service.go` (`ProjectService`)
- Modify: `internal/store/project.go` (facade methods)
- Test: `internal/store/project_capability_test.go` (new)

**Interfaces:**
- Consumes: Task 1's actions and fold behavior.
- Produces (exact):
  - `core.Project` gains `Capabilities []string \`json:"capabilities,omitempty"\`` (nil = legacy/all).
  - `core.ProjectWriter` gains `EnableCapability(name, actor string) error` and `DisableCapability(name, actor string) error`.
  - `core.ProjectService` gains `EnableProjectCapability(code, name, actor string) error` and `DisableProjectCapability(code, name, actor string) error`.
  - `*store.Store` implements both.

- [ ] **Step 1: Write the failing test**

Create `internal/store/project_capability_test.go`, mirroring the setup pattern of the existing `internal/store/project_test.go` (open it and copy its store-fixture constructor — the deterministic open used by neighboring tests):

```go
package store

import (
	"reflect"
	"testing"
)

func TestProjectCapabilityEnableDisable(t *testing.T) {
	s := newTestStore(t) // reuse this package's existing test-store constructor (see project_test.go)
	actor := "admin@cli:unset"
	if _, err := s.CreateProject("PC", "cap demo", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableProjectCapability("PC", "workflow", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableProjectCapability("PC", "contextmap", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.DisableProjectCapability("PC", "contextmap", actor); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetProject("PC")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := p.Capabilities, []string{"workflow"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Capabilities = %v, want %v", got, want)
	}
}

func TestProjectWithoutCapabilityEventsReadsNil(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("PL", "legacy-like", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetProject("PL")
	if err != nil {
		t.Fatal(err)
	}
	if p.Capabilities != nil {
		t.Fatalf("Capabilities = %v, want nil (no capability events recorded)", p.Capabilities)
	}
}
```

(If the package's fixture constructor has a different name than `newTestStore`, use the actual one — the assertion bodies stay as written.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store -run TestProjectCapability -v`
Expected: FAIL — `s.EnableProjectCapability` undefined.

- [ ] **Step 3: Implement**

(a) `internal/store/eventlog/author.go` — add to the unexported action const block:

```go
	actionProjectCapabilityEnabled  = "project.capability-enabled"
	actionProjectCapabilityDisabled = "project.capability-disabled"
```

(b) `internal/store/eventlog/changeset.go` — add after `SetProjectName` (same shape: resolve the project ref, append against the project identity, never the code):

```go
// EnableCapability / DisableCapability emit the project's capability
// membership events. The store enforces nothing about the name — which
// capabilities exist is the composition root's knowledge; the log just
// records the choice.
func (cs *changeSet) EnableCapability(name, actor string) error {
	return cs.capabilityEvent(actionProjectCapabilityEnabled, name, actor)
}

func (cs *changeSet) DisableCapability(name, actor string) error {
	return cs.capabilityEvent(actionProjectCapabilityDisabled, name, actor)
}

func (cs *changeSet) capabilityEvent(action, name, actor string) error {
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return err
	}
	ref, err := ctx.resolveProjectRef(cs.code)
	if err != nil {
		return err
	}
	_, err = cs.e.appendLocked(cs.code, draft{
		Actor:   actor,
		Action:  action,
		Subject: eventsource.Subject{Kind: "project", ID: ref, Code: cs.code},
		Payload: map[string]any{"capability": name},
	})
	return err
}
```

(c) `internal/store/eventlog/snapshot.go` — in `projectFromV2`, copy the set:

```go
func projectFromV2(p *eventsource.ProjectState) *core.Project {
	// A v2 project has no per-project ordinal (only tasks/comments/labels do),
	// so Ordinal is left 0 here.
	return &core.Project{
		Code:         p.Code,
		Name:         p.Name,
		Capabilities: p.Capabilities,
		CreatedAt:    p.CreatedAt,
		CreatedBy:    p.CreatedBy,
		UpdatedAt:    p.UpdatedAt,
		UpdatedBy:    p.UpdatedBy,
	}
}
```

(d) `internal/core/types.go` — add to `Project` (after `Name`):

```go
	// Capabilities is the project's enabled capability set. nil means no
	// capability choice was ever recorded — a legacy project — which
	// consumers (the capability registry) read as "all built-ins enabled".
	// A non-nil empty slice means explicitly none.
	Capabilities []string `json:"capabilities,omitempty"`
```

(e) `internal/core/repository.go` — add to `ProjectWriter`:

```go
	// EnableCapability / DisableCapability record the project's capability
	// choice as membership events on the project entity.
	EnableCapability(name, actor string) error
	DisableCapability(name, actor string) error
```

(f) `internal/core/service.go` — add to `ProjectService`:

```go
	EnableProjectCapability(code, name, actor string) error
	DisableProjectCapability(code, name, actor string) error
```

(g) `internal/store/project.go` — add facade methods following the exact transaction shape `SetProjectName` uses in this file (open it; it wraps `engine.WithProjectWrite(code, func(cs core.ChangeSet) error { ... })` — reuse whatever field/receiver names the file actually uses):

```go
// EnableProjectCapability records that the project enabled a capability.
func (s *Store) EnableProjectCapability(code, name, actor string) error {
	return s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		return cs.EnableCapability(name, actor)
	})
}

// DisableProjectCapability records that the project disabled a capability.
func (s *Store) DisableProjectCapability(code, name, actor string) error {
	return s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		return cs.DisableCapability(name, actor)
	})
}
```

(`s.eng` — substitute the Store's actual engine field name as used by `setProjectNameV2` in the same file. If validation helpers guard actor format on sibling mutators, apply the same guard here.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store ./internal/store/eventlog ./libs/eventsource ./internal/core -count=1`
Expected: PASS. (`internal/store` is slow — a few minutes.)

- [ ] **Step 5: Commit**

```bash
git add internal/store/eventlog/author.go internal/store/eventlog/changeset.go internal/store/eventlog/snapshot.go internal/core/types.go internal/core/repository.go internal/core/service.go internal/store/project.go internal/store/project_capability_test.go
git commit -m "feat(ATM-b678b0): project capability enable/disable events end-to-end (changeset, fold copy-out, service facade)"
```

---

### Task 3: Registry filtering — `Names()` and `For(*core.Project)`

**Files:**
- Modify: `internal/capability/capability.go`
- Test: `internal/capability/filter_test.go` (new)

**Interfaces:**
- Consumes: `core.Project.Capabilities` (Task 2).
- Produces (exact):
  - `func (r *Registry) Names() []string` — registration order, nil-safe.
  - `func (r *Registry) For(p *core.Project) *Registry` — nil registry → nil; `p == nil` or `p.Capabilities == nil` → the receiver unchanged (legacy = all); otherwise a new Registry keeping only capabilities whose `Name()` is in the set, registration order preserved.

- [ ] **Step 1: Write the failing test**

Create `internal/capability/filter_test.go` (reuses `fakeCap` from Phase 1's `guide_test.go` — same package):

```go
package capability

import (
	"reflect"
	"testing"

	"atm/internal/core"
)

func TestNames(t *testing.T) {
	r := NewRegistry(fakeCap{name: "alpha"}, fakeCap{name: "beta"})
	if got, want := r.Names(), []string{"alpha", "beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Names = %v, want %v", got, want)
	}
	var nilr *Registry
	if nilr.Names() != nil {
		t.Fatal("nil registry Names must be nil")
	}
}

func TestForFiltersByEnabledSet(t *testing.T) {
	r := NewRegistry(fakeCap{name: "alpha"}, fakeCap{name: "beta"})

	if got := r.For(nil); got != r {
		t.Error("For(nil project) must return the receiver (all enabled)")
	}
	legacy := &core.Project{Code: "L"} // Capabilities nil
	if got := r.For(legacy); got != r {
		t.Error("For(legacy project) must return the receiver (all enabled)")
	}
	narrowed := r.For(&core.Project{Code: "P", Capabilities: []string{"beta"}})
	if got, want := narrowed.Names(), []string{"beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("narrowed Names = %v, want %v", got, want)
	}
	none := r.For(&core.Project{Code: "P", Capabilities: []string{}})
	if got := none.Names(); len(got) != 0 {
		t.Fatalf("explicitly-none Names = %v, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/capability -run 'TestNames|TestFor' -v`
Expected: FAIL — `r.Names` undefined.

- [ ] **Step 3: Implement**

Append to `internal/capability/capability.go`:

```go
// Names lists the registered capability names in registration order.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.caps))
	for _, c := range r.caps {
		out = append(out, c.Name())
	}
	return out
}

// For narrows the registry to the project's enabled set. A nil project or a
// project with no recorded capability choice (Capabilities == nil — every
// project born before enablement existed) keeps the full registry: legacy
// projects read as "all built-ins enabled", with no migration event. The
// fence is on the tooling surface only; the store keeps accepting anything.
func (r *Registry) For(p *core.Project) *Registry {
	if r == nil || p == nil || p.Capabilities == nil {
		return r
	}
	enabled := make(map[string]bool, len(p.Capabilities))
	for _, n := range p.Capabilities {
		enabled[n] = true
	}
	kept := make([]Capability, 0, len(r.caps))
	for _, c := range r.caps {
		if enabled[c.Name()] {
			kept = append(kept, c)
		}
	}
	return &Registry{caps: kept}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability ./tests/arch -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/capability/capability.go internal/capability/filter_test.go
git commit -m "feat(ATM-b678b0): registry Names() and For(project) enabled-set filter; nil capabilities = legacy all-enabled"
```

---

### Task 4: CLI — create with capabilities; `project capability list|add|remove`

**Files:**
- Modify: `internal/cli/project.go`
- Test: `internal/cli/project_capability_test.go` (new) + regenerated goldens

**Interfaces:**
- Consumes: `ProjectService` methods (Task 2), `Registry.Names()` (Task 3), `st.registry`.
- Produces:
  - `atm project create --code X --name N [--capabilities workflow,contextmap]` — default: every registered capability. Unknown names → usage error listing valid names. After `CreateProject`, one `EnableProjectCapability` per chosen name, then `EnsureVocabulary` for the chosen capabilities only.
  - `atm project capability list --project X` (read-only; text: one name per line, or `(all — no explicit choice recorded)` for legacy nil; JSON: `{"project": "X", "capabilities": [...], "explicit": bool}`).
  - `atm project capability add|remove --project X --name workflow` (mutating; requires actor; `add` also runs that capability's `EnsureVocabulary`).

- [ ] **Step 1: Write the failing golden/behavior tests**

Create `internal/cli/project_capability_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestGoldenProjectCreateWithCapabilities(t *testing.T) {
	h := newGoldenHarness(t)
	out, _, code := h.run("--output", "json", "project", "create",
		"--code", "PC", "--name", "cap demo",
		"--capabilities", "workflow",
		"--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	compareGolden(t, "project-create-with-capabilities", out)
}

func TestProjectCreateRejectsUnknownCapability(t *testing.T) {
	h := newGoldenHarness(t)
	_, stderr, code := h.run("project", "create", "--code", "PX", "--name", "x",
		"--capabilities", "nosuch", "--actor", "admin@cli:unset")
	if code == 0 {
		t.Fatal("expected usage error")
	}
	if !strings.Contains(stderr, "nosuch") || !strings.Contains(stderr, "workflow") {
		t.Errorf("error must name the unknown capability and the valid names, got %q", stderr)
	}
}

func TestGoldenProjectCapabilityListAddRemove(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "PC", "--name", "cap demo",
		"--capabilities", "workflow", "--actor", "admin@cli:unset")
	out, _, code := h.run("--output", "json", "project", "capability", "list", "--project", "PC")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	compareGolden(t, "project-capability-list", out)

	if _, _, code := h.run("project", "capability", "add", "--project", "PC", "--name", "contextmap", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("add exit %d", code)
	}
	if _, _, code := h.run("project", "capability", "remove", "--project", "PC", "--name", "workflow", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("remove exit %d", code)
	}
	out2, _, _ := h.run("--output", "json", "project", "capability", "list", "--project", "PC")
	compareGolden(t, "project-capability-list-after-add-remove", out2)
}

func TestProjectCapabilityListLegacyNil(t *testing.T) {
	h := newGoldenHarness(t)
	// seedScenario1 creates project ATM without any capability events.
	seedScenario1ForHarness(t, h) // use the harness's existing scenario seeding helper name
	out, _, code := h.run("project", "capability", "list", "--project", "ATM")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "all") {
		t.Errorf("legacy project must report the all-enabled default, got %q", out)
	}
}
```

(Substitute the actual scenario-seeding helper name from `harness_test.go` — `seedScenario1` per its current definition. **Important:** after this task, `seedScenario1`'s `project create` acquires default enable events; goldens that embed store logs may shift — regenerate and inspect.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cli -run TestProjectCreateRejects -v`
Expected: FAIL — unknown `--capabilities` flag.

- [ ] **Step 3: Implement in `internal/cli/project.go`**

(a) In `newProjectCreateCmd`, add the flag and wire enablement. The current body (`internal/cli/project.go:27-58`) calls `s.CreateProject(code, name, actor)` then `st.registry.EnsureVocabulary(s, p.Code, actor)`. Change to:

```go
	var capabilities []string
	// ... inside RunE, after actor/store resolution, BEFORE CreateProject:
	chosen, err := resolveCapabilityChoice(st.registry, capabilities)
	if err != nil {
		return err
	}
	p, err := s.CreateProject(code, name, actor)
	if err != nil {
		return err
	}
	for _, cname := range chosen {
		if err := s.EnableProjectCapability(p.Code, cname, actor); err != nil {
			return err
		}
	}
	proj, err := s.GetProject(p.Code)
	if err != nil {
		return err
	}
	if err := st.registry.For(proj).EnsureVocabulary(s, p.Code, actor); err != nil {
		return err
	}
	// ... existing emit unchanged
```

and register the flag next to `--code`/`--name`:

```go
	cmd.Flags().StringSliceVar(&capabilities, "capabilities", nil,
		"capabilities to enable for the project (default: all registered)")
```

(b) Add the shared validator:

```go
// resolveCapabilityChoice validates requested capability names against the
// registry; nil/empty request means every registered capability. New
// projects always record an explicit choice — only pre-enablement projects
// read as nil/all.
func resolveCapabilityChoice(reg *capability.Registry, requested []string) ([]string, error) {
	known := reg.Names()
	if len(requested) == 0 {
		return known, nil
	}
	valid := make(map[string]bool, len(known))
	for _, n := range known {
		valid[n] = true
	}
	for _, r := range requested {
		if !valid[r] {
			return nil, fmt.Errorf("%w: unknown capability %q (registered: %s)", ErrUsage, r, strings.Join(known, ", "))
		}
	}
	return requested, nil
}
```

(Imports: `strings`, `atm/internal/capability` if not present.)

(c) Add the `capability` subcommand tree; mount it from `newProjectCmd`:

```go
func newProjectCapabilityCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capability",
		Short: "View or change the project's enabled capability set",
	}
	cmd.AddCommand(newProjectCapabilityListCmd(st))
	cmd.AddCommand(newProjectCapabilityAddCmd(st))
	cmd.AddCommand(newProjectCapabilityRemoveCmd(st))
	return cmd
}

func newProjectCapabilityListCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the project's enabled capabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.GetProject(project)
			if err != nil {
				return err
			}
			explicit := p.Capabilities != nil
			enabled := p.Capabilities
			if !explicit {
				enabled = st.registry.Names()
			}
			return st.emit(st.stdout(), map[string]any{
				"project": project, "capabilities": enabled, "explicit": explicit,
			}, func() {
				if !explicit {
					fmt.Fprintln(st.stdout(), "(all — no explicit choice recorded)")
				}
				for _, n := range enabled {
					fmt.Fprintln(st.stdout(), n)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newProjectCapabilityAddCmd(st *cliState) *cobra.Command {
	var project, name string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Enable a capability for the project (seeds its vocabulary)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.RequireMutatingActor()
			if err != nil {
				return err
			}
			if _, err := resolveCapabilityChoice(st.registry, []string{name}); err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.EnableProjectCapability(project, name, actor); err != nil {
				return err
			}
			p, err := s.GetProject(project)
			if err != nil {
				return err
			}
			if err := st.registry.For(p).EnsureVocabulary(s, project, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "enabled": name}, func() {
				fmt.Fprintf(st.stdout(), "%s: enabled %s\n", project, name)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&name, "name", "", "capability name")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newProjectCapabilityRemoveCmd(st *cliState) *cobra.Command {
	var project, name string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Disable a capability for the project (vocabulary and labels stay)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.RequireMutatingActor()
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.DisableProjectCapability(project, name, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "disabled": name}, func() {
				fmt.Fprintf(st.stdout(), "%s: disabled %s\n", project, name)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&name, "name", "", "capability name")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}
```

Note `remove` deliberately skips registry validation: disabling a name that is no longer registered must work (degrade, never reject). Bind the actor flag the way sibling mutating project subcommands do in this file (check how `newProjectCreateCmd` gets `--actor` — via `bindActorFlag` on the parent `project` command; keep that).

- [ ] **Step 4: Run tests, regenerate goldens**

Run: `go test ./internal/cli -run 'TestProjectCreate|TestProjectCapability|TestGoldenProject' -v` → new goldens missing → `go test ./internal/cli -update` → re-run full `go test ./internal/cli`. Inspect every changed pre-existing golden: only additive enable events / capability arrays may appear.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/project.go internal/cli/project_capability_test.go internal/cli/testdata/golden/
git commit -m "feat(ATM-b678b0): project create --capabilities; project capability list/add/remove"
```

---

### Task 5: The hard gate — pre-parse project resolution mounts only enabled capabilities

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/conventions.go` (add `--project` flag)
- Modify: `internal/cli/harness_test.go` (route runs through the same mount path)
- Test: `internal/cli/mount_test.go` (new)

**Interfaces:**
- Consumes: `Registry.For` (Task 3), `Deps.OpenService`.
- Produces (exact):
  - `func mountProjectCode(args []string, getenv func(string) string) string` — pure. Resolution order: (1) `--project X` / `--project=X`; (2) `--task X` / `--task=X` / `--id X` — the project code is everything before the LAST `-` in the task ID; (3) `getenv("ATM_PROJECT")`. First hit wins; malformed values yield `""`.
  - `func mountRegistry(deps Deps, args []string, getenv func(string) string) *capability.Registry` — resolves the code, opens the store via `deps.OpenService(flagValue(args, "--store"))`, `GetProject`, returns `deps.Registry.For(p)`; ANY failure returns `deps.Registry` (degrade open).
  - `Execute` computes the mount before building the root command. `atm conventions` gains an optional `--project` flag (value used by the prescan; the enumeration then reflects the enabled set because `st.registry` is already narrowed).

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/mount_test.go`:

```go
package cli

import "testing"

func TestMountProjectCode(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	cases := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{"project flag", []string{"workflow", "guide", "--project", "ATM"}, nil, "ATM"},
		{"project eq flag", []string{"--project=ATM", "conventions"}, nil, "ATM"},
		{"task id prefix", []string{"workflow", "start", "--task", "ATM-3b873c"}, nil, "ATM"},
		{"task id eq", []string{"workflow", "start", "--task=MY-PROJ-4f"}, nil, "MY-PROJ"},
		{"legacy id flag", []string{"workflow", "start", "--id", "ATM-0042"}, nil, "ATM"},
		{"env fallback", []string{"conventions"}, map[string]string{"ATM_PROJECT": "ENV"}, "ENV"},
		{"flag beats env", []string{"--project", "FLAG"}, map[string]string{"ATM_PROJECT": "ENV"}, "FLAG"},
		{"nothing", []string{"conventions"}, nil, ""},
		{"task id no dash", []string{"workflow", "start", "--task", "nodash"}, nil, ""},
		{"dangling flag", []string{"workflow", "start", "--task"}, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mountProjectCode(c.args, env(c.env)); got != c.want {
				t.Fatalf("mountProjectCode(%v) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

// The gate end-to-end: a project that disabled workflow does not get the
// workflow command mounted; a project that kept it does; resolution failure
// mounts everything (degrade open).
func TestHardGateMountsOnlyEnabledCapabilities(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "NC", "--name", "no caps",
		"--capabilities", "contextmap", "--actor", "admin@cli:unset")

	_, stderr, code := h.run("workflow", "seed", "--project", "NC", "--actor", "admin@cli:unset")
	if code == 0 {
		t.Fatalf("workflow must be unmounted for NC; stderr=%q", stderr)
	}
	if _, _, code := h.run("context", "check", "--project", "NC"); code != 0 {
		// context check may legitimately fail on a non-repo cwd; the point is
		// the command must be FOUND. Assert the failure is not "unknown command".
		if strings.Contains(stderr, "unknown command") {
			t.Fatal("context must stay mounted for NC")
		}
	}
	// Unknown project: degrade open — workflow help must be found.
	if _, stderr, _ := h.run("workflow", "--help", "--project", "NOPE"); strings.Contains(stderr, "unknown command") {
		t.Fatal("resolution failure must mount the full registry")
	}
}
```

Add `"strings"` to imports. (Adjust the second test's plumbing to the harness's actual stderr capture shape — `h.run` returns `(stdout, stderr string, code int)`.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cli -run 'TestMountProjectCode|TestHardGate' -v`
Expected: FAIL — `mountProjectCode` undefined.

- [ ] **Step 3: Implement in `internal/cli/root.go`**

(a) The pure helpers:

```go
// flagValue scans argv for `--name v` / `--name=v` without cobra: the mount
// decision happens BEFORE the command tree exists. First occurrence wins.
func flagValue(args []string, name string) string {
	eq := name + "="
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, eq) {
			return strings.TrimPrefix(a, eq)
		}
	}
	return ""
}

// mountProjectCode resolves which project an invocation targets, pre-parse.
// Order: --project, then --task/--id (project = everything before the task
// ID's last '-'), then ATM_PROJECT. Empty string = no target (mount all).
func mountProjectCode(args []string, getenv func(string) string) string {
	if v := flagValue(args, "--project"); v != "" {
		return v
	}
	for _, f := range []string{"--task", "--id"} {
		if v := flagValue(args, f); v != "" {
			if i := strings.LastIndex(v, "-"); i > 0 {
				return v[:i]
			}
			return ""
		}
	}
	return getenv("ATM_PROJECT")
}

// mountRegistry narrows the capability registry to the target project's
// enabled set. Every failure path returns the full registry: the gate only
// narrows on a successful read (degrade, never reject).
func mountRegistry(deps Deps, args []string, getenv func(string) string) *capability.Registry {
	code := mountProjectCode(args, getenv)
	if code == "" || deps.OpenService == nil {
		return deps.Registry
	}
	svc, err := deps.OpenService(flagValue(args, "--store"))
	if err != nil {
		return deps.Registry
	}
	p, err := svc.GetProject(code)
	if err != nil {
		return deps.Registry
	}
	return deps.Registry.For(p)
}
```

(b) Split `Execute` so the harness and production share the path:

```go
func Execute(deps Deps) int { return executeArgs(deps, os.Args[1:]) }

func executeArgs(deps Deps, args []string) int {
	st := &cliState{runTUI: deps.RunTUI, registry: mountRegistry(deps, args, os.Getenv), openServiceFn: deps.OpenService, openAdminFn: deps.OpenAdmin}
	root := newRootCmdWithState(st)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		if st.isJSON() {
			env := NewErrorEnvelopeFromError(err)
			fmt.Fprintln(st.stderr(), env.String())
		} else {
			fmt.Fprintln(st.stderr(), "error:", err)
		}
		return ExitCodeForError(err)
	}
	return ExitSuccess
}
```

Note: the TUI keeps the FULL registry — `Deps.RunTUI` was wired in `cmd/atm/main.go` with `reg` directly and is untouched by the mount (Task 6 filters per selected project inside the TUI).

(c) Harness wiring — in `internal/cli/harness_test.go`, the `run` helper builds a root per invocation. Make it recompute the mount per call: where `run` constructs/executes the root command, set `st.registry = mountRegistry(h.deps(), args, func(string) string { return "" })` before building the root (or refactor `run` to call `executeArgs`-equivalent plumbing with the harness's deterministic `cliState`). Preserve the harness's seeded openers: build a `Deps{Registry: testRegistry(), OpenService: <harness opener>, OpenAdmin: <harness opener>}` value once in the harness constructor and reuse it. The env getter returns `""` always — goldens must not see the invoking shell's `ATM_PROJECT` (the harness already clears it).

(d) `internal/cli/conventions.go` — register the flag so cobra accepts it (the prescan consumed the value before parse):

```go
	var project string
	cmd.Flags().StringVar(&project, "project", "",
		"project code; narrows the capability enumeration to the project's enabled set")
```

(The RunE body does not read `project`: when the flag is present, `st.registry` was already narrowed by `mountRegistry`. Add a comment in the code saying exactly that.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli -count=1` (full package: the mount change touches every golden's execution path — expect zero golden diffs because no harness invocation carries a project with an explicit narrowed set except the new tests).
Expected: PASS.

- [ ] **Step 5: End-to-end sanity**

```bash
export TMPSTORE=$(mktemp -d)
go run ./cmd/atm --store "$TMPSTORE" project create --code NC --name demo --capabilities contextmap --actor admin@cli:unset
go run ./cmd/atm --store "$TMPSTORE" workflow seed --project NC --actor admin@cli:unset; echo "exit=$?"   # unknown command, nonzero
go run ./cmd/atm --store "$TMPSTORE" conventions --project NC | grep -A4 '## Capabilities'                 # contextmap only
go run ./cmd/atm --store "$TMPSTORE" conventions | grep -A5 '## Capabilities'                              # both (no target)
```

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/conventions.go internal/cli/harness_test.go internal/cli/mount_test.go
git commit -m "feat(ATM-b678b0): hard gate — pre-parse project resolution mounts only enabled capability commands; conventions --project narrows enumeration"
```

---

### Task 6: TUI — per-project filtering, capability display + toggle

**Files:**
- Modify: `internal/tui/app.go` (per-project registry helper)
- Modify: `internal/tui/projects.go` (select-time filtering, detail display, toggle keys)
- Modify: `internal/tui/labels.go` (DefaultBoard through the filter)
- Test: `internal/tui/projects_capability_test.go` (new; mirror the package's existing model-driven test style — construct the model, feed key msgs, assert view strings)

**Interfaces:**
- Consumes: `Registry.For`, `Registry.Names`, `ProjectService.Enable/DisableProjectCapability`, `core.Project.Capabilities`.
- Produces:
  - `func (m *Model) regFor(code string) *capability.Registry` on the root model: `p, err := m.store.GetProject(code); if err != nil { return m.reg }; return m.reg.For(p)`.
  - Call sites swapped: `internal/tui/projects.go:242` and `internal/tui/app.go:186` use `m.regFor(code).EnsureVocabulary(...)`; `internal/tui/labels.go:257` uses `b.m.regFor(b.m.projectScope).DefaultBoard(...)`.
  - Projects detail view renders a `capabilities:` line — each registered name prefixed `[x]`/`[ ]` (legacy nil = all `[x]`, suffixed `(default)`).
  - In the detail view, key `c` cycles the capability cursor; `space` toggles the highlighted capability (enable ↔ disable) via the service, then refreshes the row.

- [ ] **Step 1: Write the failing test**

Model-level test (no teatest needed if the package tests drive `Update`/`View` directly — mirror the neighboring style in `internal/tui/projects_*_test.go` or the closest existing projects-pane test):

```go
package tui

import (
	"strings"
	"testing"
)

// Build the model over a store fixture with:
//   project EX  — capabilities recorded: ["workflow"]
//   project LG  — no capability events (legacy)
// using this package's existing test-store/model constructors.

func TestDetailViewRendersCapabilities(t *testing.T) {
	m := newCapabilityFixtureModel(t) // fixture described above
	openProjectDetail(t, m, "EX")
	v := m.View()
	if !strings.Contains(v, "[x] workflow") || !strings.Contains(v, "[ ] contextmap") {
		t.Fatalf("detail view must render the enabled set, got:\n%s", v)
	}
	openProjectDetail(t, m, "LG")
	v = m.View()
	if !strings.Contains(v, "(default)") {
		t.Fatalf("legacy project must render the all-enabled default marker, got:\n%s", v)
	}
}

func TestToggleCapabilityFromDetail(t *testing.T) {
	m := newCapabilityFixtureModel(t)
	openProjectDetail(t, m, "EX")
	sendKeys(t, m, "c", " ") // focus first capability (workflow), toggle it off
	p, err := modelStore(m).GetProject("EX")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Capabilities {
		if c == "workflow" {
			t.Fatal("workflow should have been disabled by the toggle")
		}
	}
}
```

Write `newCapabilityFixtureModel` / `openProjectDetail` / `sendKeys` / `modelStore` on top of the package's existing test helpers (open the neighboring projects-pane tests first and reuse their constructors and key-message plumbing; only the fixture's capability events are new).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui -run 'TestDetailViewRendersCapabilities|TestToggleCapability' -v`
Expected: FAIL (no capabilities line rendered; `c`/space keys unhandled).

- [ ] **Step 3: Implement**

(a) `internal/tui/app.go` — add:

```go
// regFor narrows the registry to the project's enabled set, degrading to
// the full registry when the project cannot be read (never blank a pane
// over a read failure).
func (m *Model) regFor(code string) *capability.Registry {
	p, err := m.store.GetProject(code)
	if err != nil {
		return m.reg
	}
	return m.reg.For(p)
}
```

Swap `app.go:186` from `m.reg.EnsureVocabulary(m.store, m.projectScope, m.actor)` to `m.regFor(m.projectScope).EnsureVocabulary(m.store, m.projectScope, m.actor)`.

(b) `internal/tui/projects.go` — swap line 242 the same way (`p.m.regFor(r.code).EnsureVocabulary(...)`).

(c) `internal/tui/labels.go:257` — `want := b.m.regFor(b.m.projectScope).DefaultBoard(b.m.projectScope)`.

(d) `internal/tui/projects.go` — detail rendering + toggle. Add to `projectsModel`: `capCursor int`. In `renderDetailView`, after the existing fields, render:

```go
	// capabilities: [x] workflow  [ ] contextmap   — cursor underlined when focused
	names := p.m.reg.Names()
	enabled := map[string]bool{}
	proj, err := p.m.store.GetProject(code)
	explicit := err == nil && proj.Capabilities != nil
	if explicit {
		for _, n := range proj.Capabilities {
			enabled[n] = true
		}
	}
	var parts []string
	for i, n := range names {
		mark := "[ ]"
		if !explicit || enabled[n] {
			mark = "[x]"
		}
		cell := fmt.Sprintf("%s %s", mark, n)
		if i == p.capCursor {
			cell = focusedStyle.Render(cell) // reuse the pane's existing focused/selected lipgloss style name
		}
		parts = append(parts, cell)
	}
	suffix := ""
	if !explicit {
		suffix = "  (default)"
	}
	line := "capabilities: " + strings.Join(parts, "  ") + suffix
```

and append `line` where the detail body lines are assembled (match the surrounding rendering code's exact style helpers — reuse, don't invent).

(e) `handleDetailKey` — add cases:

```go
	case "c":
		p.capCursor = (p.capCursor + 1) % len(p.m.reg.Names())
		return p, nil
	case " ":
		names := p.m.reg.Names()
		if len(names) == 0 {
			return p, nil
		}
		name := names[p.capCursor%len(names)]
		proj, err := p.m.store.GetProject(code)
		if err != nil {
			return p, nil
		}
		isEnabled := proj.Capabilities == nil // legacy: everything enabled
		for _, n := range proj.Capabilities {
			if n == name {
				isEnabled = true
			}
		}
		if isEnabled {
			_ = p.m.store.DisableProjectCapability(code, name, p.m.actor)
		} else {
			_ = p.m.store.EnableProjectCapability(code, name, p.m.actor)
		}
		return p, p.refreshCmd() // reuse the pane's existing refresh command
```

**Legacy-nil toggle semantics:** disabling one capability of a legacy (nil) project first makes the choice explicit — before the `Disable` call, when `proj.Capabilities == nil`, enable every OTHER registered name explicitly:

```go
		if proj.Capabilities == nil && isEnabled {
			for _, n := range names {
				if n != name {
					_ = p.m.store.EnableProjectCapability(code, n, p.m.actor)
				}
			}
		}
```

(Adapt receiver/field names — `p.m` vs `m` — and the refresh mechanism to what `projects.go` actually uses at the anchor points named above; the logic is as written.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui -count=1` (slow, ~6 min).
Expected: PASS, including pre-existing pane tests.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/app.go internal/tui/projects.go internal/tui/labels.go internal/tui/projects_capability_test.go
git commit -m "feat(ATM-b678b0): TUI filters capabilities per project; detail view renders and toggles the enabled set"
```

---

### Task 7: Full verification, docs touch-up, ledger

**Files:**
- Modify: `docs/architecture/label-substrate-and-capabilities.md` (status note only, if desired — the doctrine text already describes this phase)
- None else.

- [ ] **Step 1: Full suite**

Run: `go test ./... ./libs/...`
Expected: all `ok`.

- [ ] **Step 2: End-to-end scenario**

```bash
export TMPSTORE=$(mktemp -d)
go run ./cmd/atm --store "$TMPSTORE" project create --code A --name A --actor admin@cli:unset            # default: all
go run ./cmd/atm --store "$TMPSTORE" project create --code B --name B --capabilities workflow --actor admin@cli:unset
go run ./cmd/atm --store "$TMPSTORE" project capability list --project A
go run ./cmd/atm --store "$TMPSTORE" project capability list --project B
go run ./cmd/atm --store "$TMPSTORE" context check --project B; echo "exit=$?"                            # unknown command (contextmap unmounted for B)
go run ./cmd/atm --store "$TMPSTORE" project capability add --project B --name contextmap --actor admin@cli:unset
go run ./cmd/atm --store "$TMPSTORE" conventions --project B | grep -A5 '## Capabilities'                 # now both
```

- [ ] **Step 3: Ledger + spec status**

```bash
atm task comment add --task ATM-b678b0 --actor developer@claude:<your-model> \
  --label ATM:comment:progress \
  --body "Phase 2 (enable) implemented: project.capability-enabled/disabled events, core.Project.Capabilities (nil=legacy all), registry For() filter, project create --capabilities + project capability list/add/remove, pre-parse hard gate (mountProjectCode: --project > --task/--id prefix > ATM_PROJECT; degrade open), TUI per-project filtering + toggle. Full suite green."
```

- [ ] **Step 4: Note the deliberate spec deviation**

The spec's phase-2 outline mentions `atm init` selecting a default set "for the projects it creates" — `atm init` creates no projects (it installs plugins/selects agents; see `internal/cli/init.go`), so there is nothing for it to record and no store-wide default exists (per-project enablement was the settled decision). This plan therefore adds no init step; the default is "all registered" at `project create`. Record this in the ledger comment above and, if the human confirms, amend the spec's phase-2 outline sentence.
