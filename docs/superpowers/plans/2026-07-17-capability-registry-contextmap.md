# Capability Registry (Refactor Step 5, ATM-08db6e) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce the capability registry seam: contextmap and workflow become self-contained packages under `internal/capability/*` that own their cobra commands, assembled into a registry by `cmd/atm`; `internal/cli` and `internal/tui` consume only the registry and stop naming any capability.

**Architecture:** A new `internal/capability` package defines the `Capability` interface, the `Registry`, and the `Env` seam (implemented by `cliState` with one-line delegations). The two capability packages move under it, flip to core role interfaces, and absorb their cobra layers from `cli/context.go` and `cli/workflow.go`. The composition root (`cmd/atm`) builds `capability.NewRegistry(workflow.New(), contextmap.New())` and injects it into both adapters. Spec: `docs/superpowers/specs/2026-07-17-capability-registry-contextmap-design.md`.

**Tech Stack:** Go 1.x multi-module workspace (root module `atm` + `libs/eventsource` via go.work), cobra, Bubble Tea, `go test` golden harness with `-update` flag.

## Global Constraints

- Branch: `atm-08db6e-capability-registry` (already created; spec committed as 768670c). Work at repo root `/home/ttran/projects/scyllas/atm`.
- Every task ends green: `go build ./... && go test ./...` from the repo root must pass before each commit.
- `atm context ...` and `atm workflow ...` behavior stays byte-identical through the cobra-layer moves (Tasks 5-6), verified by help-surface diffs. The ONLY allowed output changes are: path prose updates made deliberately in Task 3 (before the parity baseline is captured in Task 5), and eager vocabulary seeding in Task 7 (accepted in the spec, decision 2).
- Import rules (enforced by Task 8's arch tests): `internal/capability` imports only `core` internally; `internal/capability/{contextmap,workflow}` import only `capability` + `core` internally; `internal/cli` and `internal/tui` production files import neither capability package (registry only). Test files (`_test.go`) are exempt.
- Usage errors inside capability packages wrap `core.ErrUsage` (NOT `cli.ErrUsage`, which capabilities cannot import). `cli.CodeForError` already maps it via `store.ErrUsage = core.ErrUsage` (`internal/store/core_aliases.go:13`), and both sentinels carry the message text "usage", so JSON error envelopes are unchanged.
- NEVER run a dev-built `atm` against `~/.config/atm` (a schema-changing build would break the installed binary). Smoke tests use `ATM_HOME=$(mktemp -d)` or a copy.
- Commit messages: `refactor(ATM-08db6e): ...` / `test(ATM-08db6e): ...` / `docs(ATM-08db6e): ...`, each ending with the Co-Authored-By and Claude-Session trailers used on 768670c.
- Markdown files: prose as single un-wrapped lines (no hard wrapping).

---

### Task 1: `internal/capability` — the registry package

**Files:**
- Create: `internal/capability/capability.go`
- Test: `internal/capability/capability_test.go`

**Interfaces:**
- Consumes: `core.LabelService`, `core.Service`, `core.Task` from `internal/core` (exist since step 4).
- Produces: `capability.Env` (interface), `capability.Capability` (interface), `capability.Registry` with `NewRegistry(caps ...Capability) *Registry`, `(*Registry).Commands(env Env) []*cobra.Command`, `(*Registry).EnsureVocabulary(svc core.LabelService, code, actor string) error`, `(*Registry).DefaultBoard(code string) string`. All `*Registry` methods are nil-receiver safe. Every later task depends on these exact names.

- [ ] **Step 1: Write the failing test**

Create `internal/capability/capability_test.go`:

```go
package capability

import (
	"errors"
	"testing"

	"atm/internal/core"

	"github.com/spf13/cobra"
)

// fakeCap records EnsureVocabulary calls into a shared slice so tests can
// assert call order across a registry.
type fakeCap struct {
	name   string
	board  string
	ensure error
	calls  *[]string
}

func (f *fakeCap) Name() string { return f.name }

func (f *fakeCap) EnsureVocabulary(svc core.LabelService, code, actor string) error {
	*f.calls = append(*f.calls, f.name+"/"+code+"/"+actor)
	return f.ensure
}

func (f *fakeCap) Command(env Env) *cobra.Command { return &cobra.Command{Use: f.name} }

func (f *fakeCap) DefaultBoard(code string) string { return f.board }

func TestCommandsPreserveRegistrationOrder(t *testing.T) {
	var calls []string
	reg := NewRegistry(
		&fakeCap{name: "workflow", calls: &calls},
		&fakeCap{name: "contextmap", calls: &calls},
	)
	cmds := reg.Commands(nil)
	if len(cmds) != 2 || cmds[0].Use != "workflow" || cmds[1].Use != "contextmap" {
		t.Fatalf("Commands = %v, want [workflow contextmap]", cmds)
	}
}

func TestEnsureVocabularyLoopsAllInOrder(t *testing.T) {
	var calls []string
	reg := NewRegistry(
		&fakeCap{name: "workflow", calls: &calls},
		&fakeCap{name: "contextmap", calls: &calls},
	)
	if err := reg.EnsureVocabulary(nil, "ATM", "tester"); err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}
	if len(calls) != 2 || calls[0] != "workflow/ATM/tester" || calls[1] != "contextmap/ATM/tester" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestEnsureVocabularyStopsAtFirstError(t *testing.T) {
	var calls []string
	boom := errors.New("boom")
	reg := NewRegistry(
		&fakeCap{name: "workflow", ensure: boom, calls: &calls},
		&fakeCap{name: "contextmap", calls: &calls},
	)
	if err := reg.EnsureVocabulary(nil, "ATM", "tester"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %v, want only workflow", calls)
	}
}

func TestDefaultBoardFirstNonEmptyWins(t *testing.T) {
	var calls []string
	reg := NewRegistry(
		&fakeCap{name: "contextmap", board: "", calls: &calls},
		&fakeCap{name: "workflow", board: "ATM:open-tasks", calls: &calls},
	)
	if got := reg.DefaultBoard("ATM"); got != "ATM:open-tasks" {
		t.Fatalf("DefaultBoard = %q, want ATM:open-tasks", got)
	}
}

func TestNilRegistryIsSafeAndEmpty(t *testing.T) {
	var reg *Registry
	if got := reg.Commands(nil); got != nil {
		t.Fatalf("Commands on nil = %v, want nil", got)
	}
	if err := reg.EnsureVocabulary(nil, "ATM", "tester"); err != nil {
		t.Fatalf("EnsureVocabulary on nil = %v, want nil", err)
	}
	if got := reg.DefaultBoard("ATM"); got != "" {
		t.Fatalf("DefaultBoard on nil = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/capability/`
Expected: FAIL — `no required module provides package` / undefined `NewRegistry`, `Env`, `Registry` (package does not exist yet).

- [ ] **Step 3: Write the implementation**

Create `internal/capability/capability.go`:

```go
// Package capability defines the registry seam between the composition root
// and the capability commands (docs/architecture/logical-components.md;
// docs/superpowers/specs/2026-07-17-capability-registry-contextmap-design.md).
// A capability owns a slice of the label substrate, exposes intent verbs, and
// registers its cobra command tree; the adapters (cli, tui) consume only this
// package, never a specific capability. Enable/disable is editing the slice
// the composition root passes to NewRegistry.
package capability

import (
	"io"

	"atm/internal/core"

	"github.com/spf13/cobra"
)

// Env is the surface a capability's cobra layer builds on. internal/cli's
// cliState implements it; every method is a thin delegation to an existing
// cli helper, so a command behaves identically whether its cobra layer lives
// in cli or in a capability package.
type Env interface {
	// OpenService opens the store as the core service composite.
	OpenService() (core.Service, error)
	Stdout() io.Writer
	Stderr() io.Writer
	// Emit writes v as JSON in --output json mode, else runs textFn.
	Emit(v any, textFn func()) error
	// RequireMutatingActor errors unless --actor/ATM_ACTOR was given.
	RequireMutatingActor() (string, error)
	// ResolveActor defaults a missing actor for read-only verbs.
	ResolveActor(required bool) (string, error)
	// BindActorFlag registers the persistent --actor flag on cmd.
	BindActorFlag(cmd *cobra.Command)
	// BindTaskIDFlags registers --task and the hidden deprecated --id alias.
	BindTaskIDFlags(cmd *cobra.Command, id, legacy *string)
	// ResolveTaskID folds a deprecated --id value into --task, warning on
	// stderr; errors when neither was given.
	ResolveTaskID(id, legacy string) (string, error)
	// TaskJSON renders a task in the CLI's canonical JSON envelope shape.
	TaskJSON(t *core.Task) any
}

// Capability is one registered capability command: it owns its label slice,
// seeds its own vocabulary, and mounts its cobra verb tree.
type Capability interface {
	// Name is the stable identifier ("contextmap", "workflow").
	Name() string
	// EnsureVocabulary seeds the capability's labels and boards for a
	// project. Idempotent; never overwrites curated descriptions.
	EnsureVocabulary(svc core.LabelService, code, actor string) error
	// Command returns the capability's cobra verb tree, built over env.
	Command(env Env) *cobra.Command
	// DefaultBoard nominates the board a UI should select by default for
	// the project, or "" when this capability nominates none.
	DefaultBoard(code string) string
}

// Registry is an ordered collection of capabilities. All methods are
// nil-receiver safe: a nil *Registry behaves as an empty one, so adapters
// and tests constructed without capabilities keep working.
type Registry struct {
	caps []Capability
}

// NewRegistry builds a registry; order is significant (mount order,
// EnsureVocabulary order, DefaultBoard precedence).
func NewRegistry(caps ...Capability) *Registry { return &Registry{caps: caps} }

// Commands returns each capability's command tree in registration order.
func (r *Registry) Commands(env Env) []*cobra.Command {
	if r == nil {
		return nil
	}
	out := make([]*cobra.Command, 0, len(r.caps))
	for _, c := range r.caps {
		out = append(out, c.Command(env))
	}
	return out
}

// EnsureVocabulary seeds every capability's vocabulary for the project,
// stopping at the first error.
func (r *Registry) EnsureVocabulary(svc core.LabelService, code, actor string) error {
	if r == nil {
		return nil
	}
	for _, c := range r.caps {
		if err := c.EnsureVocabulary(svc, code, actor); err != nil {
			return err
		}
	}
	return nil
}

// DefaultBoard returns the first non-empty default-board nomination in
// registration order, or "" when no capability nominates one.
func (r *Registry) DefaultBoard(code string) string {
	if r == nil {
		return ""
	}
	for _, c := range r.caps {
		if b := c.DefaultBoard(code); b != "" {
			return b
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/capability/`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/capability/
git commit -m "refactor(ATM-08db6e): capability registry package — Capability, Registry, Env seam"
```

---

### Task 2: Move contextmap under `internal/capability/`; flip to core role interfaces

**Files:**
- Move: `internal/contextmap/` → `internal/capability/contextmap/` (all files, `git mv`)
- Create: `internal/capability/contextmap/service.go`
- Modify: `internal/capability/contextmap/{vocabulary.go,recorder.go,check.go}`, `internal/cli/context.go` (import path only)

**Interfaces:**
- Consumes: `core.TaskService`, `core.CommentService`, `core.LabelService`, `core.QueryFilters` from `internal/core`.
- Produces: `contextmap.Service` (composite interface: `core.TaskService` + `core.CommentService` + `core.LabelService`); `Recorder.Store` field typed `Service`; `Check(s Service, r *Resolver, code, since string) (Report, error)`; `EnsureVocabulary(s core.LabelService, code, actor string) error`; `LatestStamp(s core.CommentService, taskID, code string) (Stamp, bool, error)`. Task 5 builds the cobra layer on exactly these.

- [ ] **Step 1: Move the package**

```bash
git mv internal/contextmap internal/capability/contextmap
grep -rl '"atm/internal/contextmap"' --include='*.go' . | xargs sed -i 's|atm/internal/contextmap|atm/internal/capability/contextmap|g'
```

The only production importer is `internal/cli/context.go`; the sed also fixes any test importers.

- [ ] **Step 2: Verify the tree still builds and tests pass (move only)**

Run: `go build ./... && go test ./internal/capability/... ./internal/cli/`
Expected: PASS. If anything fails, the sed missed an importer — `grep -rn "atm/internal/contextmap" .` must return nothing.

- [ ] **Step 3: Define the consumer composite**

Create `internal/capability/contextmap/service.go`:

```go
package contextmap

import "atm/internal/core"

// Service is the slice of core this capability consumes: tasks it labels
// and describes, comments it writes provenance stamps into, and labels it
// seeds. The concrete *store.Store satisfies it structurally; nothing here
// names persistence.
type Service interface {
	core.TaskService
	core.CommentService
	core.LabelService
}
```

- [ ] **Step 4: Flip the three store-typed signatures to core interfaces**

In `internal/capability/contextmap/vocabulary.go`: change the import from `"atm/internal/store"` to `"atm/internal/core"` and the signature:

```go
func EnsureVocabulary(s core.LabelService, code, actor string) error {
```

(body unchanged — it only calls `s.LabelSeed`).

In `internal/capability/contextmap/recorder.go`: replace the `"atm/internal/store"` import with `"atm/internal/core"`; change the field and the helper signature:

```go
type Recorder struct {
	Store    Service
	Resolver *Resolver
	Actor    string
}
```

```go
func LatestStamp(s core.CommentService, taskID, code string) (Stamp, bool, error) {
```

(bodies unchanged — `Recorder` calls only `GetTask`/`SetDescription`/`TaskLabelAdd`/`CreateComment` plus `EnsureVocabulary`/`LatestStamp`).

In `internal/capability/contextmap/check.go`: replace the `"atm/internal/store"` import with `"atm/internal/core"`; change the signature and the one store-typed literal:

```go
func Check(s Service, r *Resolver, code, since string) (Report, error) {
```

and inside it `store.QueryFilters{...}` becomes:

```go
	tasks, err := s.ListTasksErr(core.QueryFilters{
		Project: code,
		Labels:  []string{BoardCurrent(code)},
	})
```

- [ ] **Step 5: Verify no store import remains in production files**

Run: `grep -rn '"atm/internal/store"' internal/capability/contextmap/*.go | grep -v _test`
Expected: no output. (Test files may keep constructing a real `*store.Store` — it satisfies `Service` structurally.)

- [ ] **Step 6: Build and test**

Run: `go build ./... && go test ./internal/capability/... ./internal/cli/`
Expected: PASS — `cli/context.go` still compiles because `*store.Store` satisfies `Service` and `core.LabelService` at every call site.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor(ATM-08db6e): contextmap -> internal/capability/contextmap, typed on core role interfaces"
```

---

### Task 3: Move workflow under `internal/capability/`; update path prose

**Files:**
- Move: `internal/workflow/` → `internal/capability/workflow/` (all files, `git mv`)
- Modify (import path only): `internal/cli/{workflow.go,label.go,project.go}`, `internal/tui/{app.go,labels.go,projects.go}`, `internal/tui/{app_test.go,labels_test.go,tasks_test.go,grouped_footer_test.go,thumbnails_test.go,fixedslot_test.go}`, `tests/arch/imports_test.go`, `internal/capability/workflow/*_test.go`
- Modify (prose): `internal/cli/workflow.go` (cobra `Long` text), `internal/cli/conventions.go` (three `internal/workflow` mentions)
- Regenerate: `internal/cli/testdata/golden/conventions-*.json` (if the path string appears in them)

**Interfaces:**
- Consumes: nothing new.
- Produces: the package at its final path `atm/internal/capability/workflow` with unchanged API: `Recorder{Store core.TaskService; Actor string}` with `Start/Open/Block/Complete/SetStatus`, `Reporter{Store core.TaskService}` with `Status`, `EnsureVocabulary(s core.LabelService, code, actor string) error`, `BoardOpenTasks/BoardBacklog/BoardInProgressTasks(code string) string`. Tasks 6-7 rely on these names at this path.

- [ ] **Step 1: Move the package and rewrite import paths**

```bash
git mv internal/workflow internal/capability/workflow
grep -rl '"atm/internal/workflow"' --include='*.go' . | xargs sed -i 's|atm/internal/workflow|atm/internal/capability/workflow|g'
grep -rn "atm/internal/workflow" . --include='*.go'
```

Expected: the final grep returns nothing.

- [ ] **Step 2: Update the arch test's directory reference**

In `tests/arch/imports_test.go`, `TestWorkflowDoesNotImportStore` globs `"internal/workflow"` — change the argument to `"internal/capability/workflow"`:

```go
func TestWorkflowDoesNotImportStore(t *testing.T) {
	for f, imps := range internalImports(t, "internal/capability/workflow") {
```

(Task 8 replaces this test with the full capability rules; this keeps it honest meanwhile.)

- [ ] **Step 3: Update path prose (deliberate output change, made BEFORE the Task-5 parity baseline)**

In `internal/cli/workflow.go`, the `newWorkflowCmd` `Long` string says "Status transitions live in the internal/workflow capability." — change to "Status transitions live in the internal/capability/workflow capability.".

In `internal/cli/conventions.go`, replace all occurrences of `internal/workflow` with `internal/capability/workflow` (they appear in the text conventions body around lines 15 and 69, and the JSON map values around lines 138 and 146 — `grep -n "internal/workflow" internal/cli/conventions.go` to find them all).

- [ ] **Step 4: Build, test, regenerate conventions goldens if needed**

Run: `go build ./... && go test ./internal/capability/... ./internal/cli/ ./internal/tui/ ./tests/arch/`
If `conventions` golden comparisons fail on the new path string: `go test ./internal/cli/ -run Conventions -update`, then re-run without `-update` and inspect `git diff internal/cli/testdata/` — the only changes must be `internal/workflow` → `internal/capability/workflow`.
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor(ATM-08db6e): workflow -> internal/capability/workflow"
```

---

### Task 4: `cliState` implements `capability.Env`; registry plumbing through `Deps`

**Files:**
- Create: `internal/cli/env.go`
- Modify: `internal/cli/root.go` (Deps, cliState, newRootCmdWithState, Execute), `internal/cli/context.go` (remove `requireMutatingActor` — it moves to env.go), `internal/cli/harness_test.go` (wire a registry), `cmd/atm/main.go` (construct and inject the registry)
- Test: `internal/cli/env_test.go`

**Interfaces:**
- Consumes: `capability.Env`, `capability.Registry`, `capability.NewRegistry` from Task 1.
- Produces: `cliState` satisfies `capability.Env` (compile-time asserted); `cliState.registry *capability.Registry` field; `Deps.Registry *capability.Registry`; `newRootCmdWithState` mounts `st.registry.Commands(st)` after the built-in commands; the test harness exposes `func testRegistry() *capability.Registry` in `harness_test.go` (extended in Tasks 5-6).

- [ ] **Step 1: Write the failing test**

Create `internal/cli/env_test.go`:

```go
package cli

import (
	"bytes"
	"strings"
	"testing"

	"atm/internal/capability"
)

// TestCLIStateImplementsEnv pins the Env contract: the compile-time
// assertion in env.go is the real gate; this test exercises the two
// delegations with observable behavior.
func TestCLIStateImplementsEnv(t *testing.T) {
	var _ capability.Env = (*cliState)(nil)

	out := &bytes.Buffer{}
	st := &cliState{flags: globalFlags{output: outputJSON}, out: out}
	if err := st.Emit(map[string]any{"k": "v"}, func() { t.Fatal("textFn must not run in JSON mode") }); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !strings.Contains(out.String(), `"k": "v"`) && !strings.Contains(out.String(), `"k":"v"`) {
		t.Fatalf("Emit JSON = %q", out.String())
	}

	st2 := &cliState{}
	if _, err := st2.RequireMutatingActor(); err == nil {
		t.Fatal("RequireMutatingActor with no actor must error")
	}
	st2.flags.actor = "dev"
	actor, err := st2.RequireMutatingActor()
	if err != nil {
		t.Fatalf("RequireMutatingActor: %v", err)
	}
	if actor != "dev@cli:unset" {
		t.Fatalf("actor = %q, want dev@cli:unset", actor)
	}
}

// TestRegistryCommandsMount pins that a registry handed to Execute's state
// mounts its command trees on the root command.
func TestRegistryCommandsMount(t *testing.T) {
	st := &cliState{registry: capability.NewRegistry(&fakeMountCap{name: "fakecap"})}
	root := newRootCmdWithState(st)
	for _, c := range root.Commands() {
		if c.Use == "fakecap" {
			return
		}
	}
	t.Fatal("registry command not mounted on root")
}
```

And append the fake to the same file (it must use the real `capability.Capability` signatures):

```go
type fakeMountCap struct {
	name string
}

func (f *fakeMountCap) Name() string { return f.name }

func (f *fakeMountCap) EnsureVocabulary(svc core.LabelService, code, actor string) error {
	return nil
}

func (f *fakeMountCap) Command(env capability.Env) *cobra.Command {
	return &cobra.Command{Use: f.name}
}

func (f *fakeMountCap) DefaultBoard(code string) string { return "" }
```

with `"atm/internal/core"` and `"github.com/spf13/cobra"` added to the test file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCLIStateImplementsEnv|TestRegistryCommandsMount'`
Expected: FAIL to compile — `*cliState` does not implement `capability.Env` (missing `OpenService` etc.), `cliState` has no field `registry`.

- [ ] **Step 3: Implement env.go and the plumbing**

Create `internal/cli/env.go`:

```go
package cli

import (
	"fmt"
	"io"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// cliState implements capability.Env. Every method is a one-line delegation
// to an existing helper, so a command behaves identically whether its cobra
// layer lives in this package or in a capability package.
var _ capability.Env = (*cliState)(nil)

func (s *cliState) OpenService() (core.Service, error) {
	st, err := s.openStore()
	if err != nil {
		return nil, err
	}
	return st, nil
}

func (s *cliState) Stdout() io.Writer { return s.stdout() }

func (s *cliState) Stderr() io.Writer { return s.stderr() }

func (s *cliState) Emit(v any, textFn func()) error { return s.emit(s.stdout(), v, textFn) }

// RequireMutatingActor enforces that a mutating verb has an explicit actor.
// The shared resolveActor helper deliberately defaults a missing actor to
// admin@cli:unset (so read-only commands and the TUI still work), so mutating
// verbs that must attribute their work check the flag directly.
func (s *cliState) RequireMutatingActor() (string, error) {
	if s.flags.actor == "" {
		return "", fmt.Errorf("%w: mutating command requires --actor or ATM_ACTOR", ErrUsage)
	}
	return s.resolveActor(true)
}

func (s *cliState) ResolveActor(required bool) (string, error) { return s.resolveActor(required) }

func (s *cliState) BindActorFlag(cmd *cobra.Command) { bindActorFlag(cmd, s) }

func (s *cliState) BindTaskIDFlags(cmd *cobra.Command, id, legacy *string) {
	bindTaskIDFlags(cmd, id, legacy)
}

func (s *cliState) ResolveTaskID(id, legacy string) (string, error) {
	return resolveTaskID(s, id, legacy)
}

func (s *cliState) TaskJSON(t *core.Task) any { return taskToJSON(t, nil) }
```

Note `OpenService` does NOT `return s.openStore()` directly: the nil `*store.Store` on the error path must become a true nil interface, and the two-value return types differ.

In `internal/cli/context.go`: DELETE the `requireMutatingActor` method (lines 48-57, the block commented "requireMutatingActor enforces...") — env.go's exported `RequireMutatingActor` replaces it. Update its two callers in context.go and the callers in workflow.go from `st.requireMutatingActor()` to `st.RequireMutatingActor()`:

```bash
grep -rln 'st.requireMutatingActor()' internal/cli/ | xargs sed -i 's|st.requireMutatingActor()|st.RequireMutatingActor()|g'
```

In `internal/cli/root.go`:

```go
type Deps struct {
	RunTUI func(storePath, actor string) error
	// Registry holds the capability commands the composition root enabled;
	// nil behaves as empty (no capability commands mount).
	Registry *capability.Registry
}
```

Add the field to `cliState` (after `storeOpts []store.Option`):

```go
	// registry is the capability registry the composition root injected;
	// nil-safe (behaves as empty).
	registry *capability.Registry
```

In `newRootCmdWithState`, immediately after the `root.AddCommand(newVersionCmd(st))` line:

```go
	for _, c := range st.registry.Commands(st) {
		root.AddCommand(c)
	}
```

In `Execute`:

```go
	st := &cliState{runTUI: deps.RunTUI, registry: deps.Registry}
```

Add `"atm/internal/capability"` to root.go's imports.

In `internal/cli/harness_test.go`, add next to `updateGolden`:

```go
// testRegistry mirrors cmd/atm's production registry so golden tests
// exercise the same command surface the binary ships. Tasks 5-6 of the
// step-5 plan add the capabilities as their cobra layers move.
func testRegistry() *capability.Registry {
	return capability.NewRegistry()
}
```

and in BOTH `newGoldenHarness` and `newGoldenHarnessAt`, change the state construction line to:

```go
	st := &cliState{flags: globalFlags{output: outputJSON}, registry: testRegistry()}
```

(add `"atm/internal/capability"` to the imports).

In `cmd/atm/main.go`:

```go
package main

import (
	"os"

	"atm/internal/capability"
	"atm/internal/cli"
	"atm/internal/store"
	"atm/internal/tui"
)

// main is the composition root: it constructs the concrete store, assembles
// the capability registry, and hands the adapters their dependencies. No
// domain or presentation logic here.
func main() {
	reg := capability.NewRegistry()
	runTUI := func(storePath, actor string) error {
		root := store.ResolveStorePath(storePath)
		s, err := store.Open(root)
		if err != nil {
			return err
		}
		return tui.Run(s, actor)
	}
	os.Exit(cli.Execute(cli.Deps{RunTUI: runTUI, Registry: reg}))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/cli/ ./internal/capability/`
Expected: PASS, including the two new tests. The full cli suite must stay green — nothing user-visible changed (the registry is still empty).

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor(ATM-08db6e): cliState implements capability.Env; registry injected via Deps"
```

---

### Task 5: Contextmap's cobra layer moves into the capability; register it

**Files:**
- Create: `internal/capability/contextmap/command.go`
- Delete: `internal/cli/context.go`
- Modify: `internal/cli/root.go` (drop `newContextCmd` mount), `internal/cli/harness_test.go` (registry gains contextmap), `cmd/atm/main.go` (registry gains contextmap)

**Interfaces:**
- Consumes: `capability.Env` (Task 1), `contextmap.Service`/`Recorder`/`Check`/`EnsureVocabulary`/`ParseSource`/`Source`/`Report`/`Finding`/`Resolver` (Task 2), `core.ErrUsage`.
- Produces: `contextmap.Cap` struct and `contextmap.New() capability.Capability`. Task 9's smoke test and the cli golden tests exercise the mounted commands.

- [ ] **Step 1: Capture the parity baseline (before any edit in this task)**

```bash
SCRATCH=/tmp/claude-1000/-home-ttran-projects-scyllas-atm/616ceb42-5717-4c5c-b341-a7d7841ff309/scratchpad
go build -o "$SCRATCH/atm-before" ./cmd/atm
for c in "context" "context add" "context stamp" "context retarget" "context supersede" "context check" "workflow" "workflow start" "workflow open" "workflow block" "workflow complete" "workflow status" "workflow seed"; do echo "=== atm $c --help ==="; "$SCRATCH/atm-before" $c --help; done > "$SCRATCH/capability-help-before.txt" 2>&1
wc -l "$SCRATCH/capability-help-before.txt"
```

Expected: a few hundred lines captured. This file is the byte-identical target for Tasks 5 AND 6.

- [ ] **Step 2: Write the capability's command layer**

Create `internal/capability/contextmap/command.go` — this is `cli/context.go` transformed mechanically (`st *cliState` → `env capability.Env`; `st.emit(st.stdout(), ...)` → `env.Emit(...)`; `st.stdout()` → `env.Stdout()`; `ErrUsage` → `core.ErrUsage`; `st.recorder` → `newRecorder`; command constructors renamed `newContextXCmd` → `newXCmd`). Full file:

```go
package contextmap

import (
	"fmt"
	"io"
	"os"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// Cap is the contextmap capability: the `atm context` verb tree over the
// recorder/check verbs in this package.
type Cap struct{}

// New returns the capability the composition root registers.
func New() capability.Capability { return Cap{} }

func (Cap) Name() string { return "contextmap" }

// DefaultBoard nominates no board: context-current is a knowledge surface,
// not a work queue.
func (Cap) DefaultBoard(code string) string { return "" }

// EnsureVocabulary implements capability.Capability by delegating to this
// package's vocabulary bootstrap.
func (Cap) EnsureVocabulary(svc core.LabelService, code, actor string) error {
	return EnsureVocabulary(svc, code, actor)
}

func (Cap) Command(env capability.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Record and verify the project's context map",
		Long: "Record where each context pointer came from, and report which pointers have " +
			"drifted from reality.\n\n" +
			"add/stamp/retarget/supersede record; check only reports -- it never marks anything " +
			"stale. A changed file is not a wrong pointer: that judgement is yours.",
	}
	env.BindActorFlag(cmd)
	cmd.AddCommand(newAddCmd(env))
	cmd.AddCommand(newStampCmd(env))
	cmd.AddCommand(newRetargetCmd(env))
	cmd.AddCommand(newSupersedeCmd(env))
	cmd.AddCommand(newCheckCmd(env))
	return cmd
}

// newRecorder builds a Recorder rooted at the current working directory,
// which is the repo the manager is running in.
func newRecorder(env capability.Env, actor string) (*Recorder, error) {
	svc, err := env.OpenService()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return &Recorder{
		Store:    svc,
		Resolver: &Resolver{Repo: cwd},
		Actor:    actor,
	}, nil
}

func parseSources(raw []string) ([]Source, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: at least one --source is required", core.ErrUsage)
	}
	out := make([]Source, 0, len(raw))
	for _, r := range raw {
		src, err := ParseSource(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", core.ErrUsage, err)
		}
		out = append(out, src)
	}
	return out, nil
}

func newAddCmd(env capability.Env) *cobra.Command {
	var task, kind string
	var sources []string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Make a task a context pointer and stamp its provenance",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			srcs, err := parseSources(sources)
			if err != nil {
				return err
			}
			rec, err := newRecorder(env, actor)
			if err != nil {
				return err
			}
			if err := rec.Add(task, kind, srcs); err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": task, "kind": kind}, func() {
				fmt.Fprintf(env.Stdout(), "stamped %s as context:%s\n", task, kind)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	cmd.Flags().StringVar(&kind, "kind", "", "pointer kind: agent, repository, documentation, or question")
	cmd.Flags().StringArrayVar(&sources, "source", nil,
		"kinded locator this pointer was derived from, repeatable: git:<path>, file:<path>, url:<url>, external:<system>/<id>")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("kind")
	return cmd
}

func newStampCmd(env capability.Env) *cobra.Command {
	var task string
	cmd := &cobra.Command{
		Use:   "stamp",
		Short: "Re-verify a pointer: its subject is unchanged in meaning",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			rec, err := newRecorder(env, actor)
			if err != nil {
				return err
			}
			if err := rec.Stamp(task); err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": task}, func() {
				fmt.Fprintf(env.Stdout(), "re-stamped %s\n", task)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newRetargetCmd(env capability.Env) *cobra.Command {
	var task string
	var sources []string
	cmd := &cobra.Command{
		Use:   "retarget",
		Short: "Point at new sources: the subject survived, but moved",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			srcs, err := parseSources(sources)
			if err != nil {
				return err
			}
			rec, err := newRecorder(env, actor)
			if err != nil {
				return err
			}
			if err := rec.Retarget(task, srcs); err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": task}, func() {
				fmt.Fprintf(env.Stdout(), "retargeted %s\n", task)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	cmd.Flags().StringArrayVar(&sources, "source", nil, "new kinded locator, repeatable")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newSupersedeCmd(env capability.Env) *cobra.Command {
	var task, by, reason string
	cmd := &cobra.Command{
		Use:   "supersede",
		Short: "Retire a pointer whose subject died; history is kept",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			rec, err := newRecorder(env, actor)
			if err != nil {
				return err
			}
			if err := rec.Supersede(task, by, reason); err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": task, "by": by}, func() {
				fmt.Fprintf(env.Stdout(), "superseded %s by %s\n", task, by)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id to retire")
	cmd.Flags().StringVar(&by, "by", "", "task id that replaces it")
	cmd.Flags().StringVar(&reason, "reason", "", "why it was superseded")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("by")
	return cmd
}

func newCheckCmd(env capability.Env) *cobra.Command {
	var project, since string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Report which pointers drifted (read-only; mutates nothing)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := env.ResolveActor(false); err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			rep, err := Check(svc, &Resolver{Repo: cwd}, project, since)
			if err != nil {
				return err
			}
			return env.Emit(reportToJSON(rep), func() { printReport(env.Stdout(), rep) })
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "ATM project code")
	cmd.Flags().StringVar(&since, "since", "", "revision to scan for new territory (default: the newest stamp in the project)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func reportToJSON(rep Report) map[string]any {
	find := func(fs []Finding) []map[string]any {
		out := make([]map[string]any, 0, len(fs))
		for _, f := range fs {
			m := map[string]any{
				"task":    f.TaskID,
				"title":   f.Title,
				"source":  f.Source.String(),
				"verdict": string(f.Verdict),
				"detail":  f.Detail,
			}
			if f.AgeDays > 0 {
				m["age_days"] = f.AgeDays
			}
			out = append(out, m)
		}
		return out
	}
	return map[string]any{
		"drift":      find(rep.Drift),
		"age":        find(rep.Age),
		"unverified": find(rep.Unverified),
		"skipped":    find(rep.Skipped),
		"ok":         find(rep.OK),
		"new":        rep.New,
		"since":      rep.Since,
	}
}

func printReport(w io.Writer, rep Report) {
	section := func(name string, fs []Finding, gloss string) {
		if len(fs) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s (%d)\t%s\n", name, len(fs), gloss)
		for _, f := range fs {
			fmt.Fprintf(w, "  %s\t%s\t%s\n", f.TaskID, f.Source, f.Detail)
		}
	}
	if rep.Since != "" {
		fmt.Fprintf(w, "  (new territory since %s)\n", rep.Since)
	}
	section("DRIFT", rep.Drift, "provable content change")
	if len(rep.New) > 0 {
		fmt.Fprintf(w, "\nNEW (%d)\tchanged in git, claimed by no pointer\n", len(rep.New))
		for _, p := range rep.New {
			fmt.Fprintf(w, "  %s\n", p)
		}
	}
	section("AGE", rep.Age, "unprovable; re-verify by hand")
	section("UNVERIFIED", rep.Unverified, "no provenance stamp")
	section("SKIPPED", rep.Skipped, "could not witness")
	fmt.Fprintf(w, "\nOK (%d)\n", len(rep.OK))
}
```

- [ ] **Step 3: Unmount from cli, register in the composition root and harness**

Delete `internal/cli/context.go`:

```bash
git rm internal/cli/context.go
```

In `internal/cli/root.go`, delete the line `root.AddCommand(newContextCmd(st))` and remove the now-unused `"atm/internal/capability/contextmap"` import if present.

In `cmd/atm/main.go`, change the registry line to:

```go
	reg := capability.NewRegistry(contextmap.New())
```

adding `"atm/internal/capability/contextmap"` to the imports.

In `internal/cli/harness_test.go`, change `testRegistry` to:

```go
func testRegistry() *capability.Registry {
	return capability.NewRegistry(contextmap.New())
}
```

adding `"atm/internal/capability/contextmap"` to the test imports.

- [ ] **Step 4: Build, test, verify help parity for context**

Run: `go build ./... && go test ./internal/cli/ ./internal/capability/...`
Expected: PASS — `cli/context_test.go` keeps passing unchanged (it drives commands through the harness's root command, which now mounts contextmap via the registry).

```bash
go build -o "$SCRATCH/atm-after5" ./cmd/atm
for BIN in atm-before atm-after5; do
  for c in "context" "context add" "context stamp" "context retarget" "context supersede" "context check"; do echo "=== atm $c --help ==="; "$SCRATCH/$BIN" $c --help; done > "$SCRATCH/context-help-$BIN.txt" 2>&1
done
diff "$SCRATCH/context-help-atm-before.txt" "$SCRATCH/context-help-atm-after5.txt"
```

Both files are generated by the same loop from the two binaries, so the diff is a direct byte-parity check. Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor(ATM-08db6e): contextmap owns its cobra layer; cli mounts it via the registry"
```

---

### Task 6: Workflow's cobra layer moves into the capability; register it

**Files:**
- Create: `internal/capability/workflow/command.go`
- Delete: `internal/cli/workflow.go`
- Modify: `internal/cli/root.go` (drop `newWorkflowCmd` mount), `internal/cli/harness_test.go`, `cmd/atm/main.go` (registry gains workflow, FIRST)

**Interfaces:**
- Consumes: `capability.Env`, `workflow.Recorder`/`Reporter`/`EnsureVocabulary`/`BoardOpenTasks`/`BoardBacklog`/`BoardInProgressTasks` (Task 3), `env.TaskJSON`/`env.ResolveTaskID`/`env.BindTaskIDFlags`.
- Produces: `workflow.Cap` struct and `workflow.New() capability.Capability`; `Cap.DefaultBoard(code)` returns `BoardOpenTasks(code)`. Task 7's TUI changes rely on `DefaultBoard` through the registry.

- [ ] **Step 1: Write the capability's command layer**

Create `internal/capability/workflow/command.go` — `cli/workflow.go` transformed (`st *cliState` → `env capability.Env`; `resolveTaskID(st, ...)` → `env.ResolveTaskID(...)`; `bindTaskIDFlags` → `env.BindTaskIDFlags`; `taskToJSON(t, nil)` → `env.TaskJSON(t)`; `workflow.X` → `X`; constructors renamed `newWorkflowXCmd` → `newXCmd`). Full file:

```go
package workflow

import (
	"fmt"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// Cap is the workflow capability: the `atm workflow` verb tree over the
// status recorder/reporter in this package.
type Cap struct{}

// New returns the capability the composition root registers.
func New() capability.Capability { return Cap{} }

func (Cap) Name() string { return "workflow" }

// DefaultBoard nominates Open Tasks: the default board surface a UI selects
// for a project.
func (Cap) DefaultBoard(code string) string { return BoardOpenTasks(code) }

// EnsureVocabulary implements capability.Capability by delegating to this
// package's vocabulary bootstrap.
func (Cap) EnsureVocabulary(svc core.LabelService, code, actor string) error {
	return EnsureVocabulary(svc, code, actor)
}

func (Cap) Command(env capability.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Status-transition verbs (the paved road for task status)",
		Long: "Status transitions live in the internal/capability/workflow capability. " +
			"Each verb swaps the task's status:* label (removes any existing one, " +
			"adds the target), so exactly-one-status is an invariant the capability " +
			"maintains. The store still enforces nothing; raw `atm task label " +
			"add/remove --label <CODE>:status:<value>` works. This is a paved road, " +
			"not a fence.",
	}
	env.BindActorFlag(cmd)
	cmd.AddCommand(newStartCmd(env))
	cmd.AddCommand(newOpenCmd(env))
	cmd.AddCommand(newBlockCmd(env))
	cmd.AddCommand(newCompleteCmd(env))
	cmd.AddCommand(newStatusCmd(env))
	cmd.AddCommand(newSeedCmd(env))
	return cmd
}

// runStatusVerb is the shared body for the four mutating status verbs. It
// resolves the task, requires an explicit actor, runs the swap, then prints
// the transition line and emits the updated task JSON.
//
// SetStatus returns prior == target ONLY on its no-op path (it returns early
// before making any store call in that case), so prior == now precisely
// identifies "the task already carried this status as its sole status" per
// the design spec, which calls for a no-op message rather than a transition
// line that would misleadingly read as if something happened (e.g.
// "status done -> done").
func runStatusVerb(env capability.Env, id, legacy string, fn func(*Recorder, string) (string, error)) error {
	taskID, err := env.ResolveTaskID(id, legacy)
	if err != nil {
		return err
	}
	actor, err := env.RequireMutatingActor()
	if err != nil {
		return err
	}
	svc, err := env.OpenService()
	if err != nil {
		return err
	}
	rec := &Recorder{Store: svc, Actor: actor}
	prior, err := fn(rec, taskID)
	if err != nil {
		return err
	}
	t, err := svc.GetTask(taskID)
	if err != nil {
		return err
	}
	now, err := (&Reporter{Store: svc}).Status(taskID)
	if err != nil {
		return err
	}
	return env.Emit(map[string]any{"task": env.TaskJSON(t)}, func() {
		switch {
		case prior == now:
			// No-op: the task already carried this status as its sole status.
			fmt.Fprintf(env.Stdout(), "%s: already %s\n", t.ID, now)
		case prior == "":
			fmt.Fprintf(env.Stdout(), "%s: status -> %s\n", t.ID, now)
		default:
			fmt.Fprintf(env.Stdout(), "%s: status %s -> %s\n", t.ID, prior, now)
		}
	})
}

func newStartCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Transition a task to in-progress",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(env, id, legacy, func(r *Recorder, tid string) (string, error) {
				return r.Start(tid)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newOpenCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Transition a task to open",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(env, id, legacy, func(r *Recorder, tid string) (string, error) {
				return r.Open(tid)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newBlockCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "block",
		Short: "Transition a task to blocked",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(env, id, legacy, func(r *Recorder, tid string) (string, error) {
				return r.Block(tid)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newCompleteCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "complete",
		Short: "Transition a task to done",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(env, id, legacy, func(r *Recorder, tid string) (string, error) {
				return r.Complete(tid)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newStatusCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print the task's current status (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := env.ResolveTaskID(id, legacy)
			if err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			rep := &Reporter{Store: svc}
			value, err := rep.Status(taskID)
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": taskID, "status": value}, func() {
				if value == "" {
					fmt.Fprintf(env.Stdout(), "untriaged\n")
					return
				}
				fmt.Fprintf(env.Stdout(), "%s\n", value)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newSeedCmd(env capability.Env) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Ensure the workflow boards (backlog, open-tasks, in-progress-tasks) exist",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			if err := EnsureVocabulary(svc, project, actor); err != nil {
				return err
			}
			return env.Emit(map[string]any{
				"project": project,
				// Board names come from the capability's helpers, never rebuilt
				// here: this package owns these names exclusively, and a
				// hand-built string would silently drift from what
				// EnsureVocabulary actually seeds if a board is ever renamed.
				"boards": []string{
					BoardBacklog(project),
					BoardOpenTasks(project),
					BoardInProgressTasks(project),
				},
			}, func() {
				fmt.Fprintf(env.Stdout(), "ensured workflow boards for %s\n", project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
```

- [ ] **Step 2: Unmount from cli, register in the composition root and harness**

```bash
git rm internal/cli/workflow.go
```

In `internal/cli/root.go`, delete the line `root.AddCommand(newWorkflowCmd(st))`.

In `cmd/atm/main.go` (workflow FIRST — DefaultBoard precedence, spec decision 3):

```go
	reg := capability.NewRegistry(workflow.New(), contextmap.New())
```

adding `workflow "atm/internal/capability/workflow"` to the imports.

In `internal/cli/harness_test.go`:

```go
func testRegistry() *capability.Registry {
	return capability.NewRegistry(workflow.New(), contextmap.New())
}
```

adding the workflow import to the test file.

- [ ] **Step 3: Build, test, verify the full help-parity diff**

Run: `go build ./... && go test ./internal/cli/ ./internal/capability/...`
Expected: PASS — `cli/workflow_test.go` keeps passing through the harness registry.

```bash
go build -o "$SCRATCH/atm-after6" ./cmd/atm
for c in "context" "context add" "context stamp" "context retarget" "context supersede" "context check" "workflow" "workflow start" "workflow open" "workflow block" "workflow complete" "workflow status" "workflow seed"; do echo "=== atm $c --help ==="; "$SCRATCH/atm-after6" $c --help; done > "$SCRATCH/capability-help-after.txt" 2>&1
diff "$SCRATCH/capability-help-before.txt" "$SCRATCH/capability-help-after.txt"
```

Expected: no output — byte-identical.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "refactor(ATM-08db6e): workflow owns its cobra layer; registry order workflow, contextmap"
```

---

### Task 7: Adapters go registry-only — EnsureVocabulary loops and DefaultBoard

**Files:**
- Modify: `internal/cli/project.go`, `internal/cli/label.go`, `internal/tui/app.go`, `internal/tui/projects.go`, `internal/tui/labels.go`, `internal/tui/run.go`, `cmd/atm/main.go`
- Modify (tests): `internal/tui/*_test.go` model-construction helpers; regenerate cli goldens affected by eager seeding

**Interfaces:**
- Consumes: `(*Registry).EnsureVocabulary`, `(*Registry).DefaultBoard` (Task 1); registry field on `cliState` (Task 4).
- Produces: `tui.NewModelOpts` gains `Registry *capability.Registry`; `Model` gains field `reg *capability.Registry`; `tui.Run(svc core.Service, actor string, reg *capability.Registry) error` (signature change — `cmd/atm` is the only production caller). After this task, no production file in cli or tui imports either capability package.

- [ ] **Step 1: cli call sites**

In `internal/cli/project.go` (project create RunE), replace:

```go
			if err := workflow.EnsureVocabulary(s, p.Code, actor); err != nil {
				return err
			}
```

with:

```go
			if err := st.registry.EnsureVocabulary(s, p.Code, actor); err != nil {
				return err
			}
```

In `internal/cli/label.go` (label seed RunE), replace:

```go
			if err := workflow.EnsureVocabulary(s, project, actor); err != nil {
				return err
			}
```

with:

```go
			if err := st.registry.EnsureVocabulary(s, project, actor); err != nil {
				return err
			}
```

Remove the now-unused `"atm/internal/capability/workflow"` import from both files.

- [ ] **Step 2: tui plumbing**

In `internal/tui/run.go`:

```go
func Run(svc core.Service, actor string, reg *capability.Registry) error {
	m, err := NewModel(NewModelOpts{Service: svc, Actor: actor, Registry: reg})
	...
```

(add `"atm/internal/capability"` import; rest of the function unchanged).

In `internal/tui/app.go`:

- `NewModelOpts` gains the field:

```go
type NewModelOpts struct {
	Service  core.Service
	Actor    string
	Registry *capability.Registry
}
```

- `Model` struct gains a field next to `store`: `reg *capability.Registry` with the comment `// reg is the capability registry the composition root injected; nil-safe.`
- In `NewModel`, inside the `m := &Model{...}` literal add `reg: opts.Registry,`.
- Replace the `workflow.EnsureVocabulary(m.store, m.projectScope, m.actor)` call in the defensive block near the end of `NewModel` with `m.reg.EnsureVocabulary(m.store, m.projectScope, m.actor)` (toast message string unchanged).
- Remove the `"atm/internal/capability/workflow"` import; add `"atm/internal/capability"`.

In `internal/tui/projects.go`, replace `workflow.EnsureVocabulary(p.m.store, r.code, p.m.actor)` with `p.m.reg.EnsureVocabulary(p.m.store, r.code, p.m.actor)` and drop the workflow import.

In `internal/tui/labels.go` `selectDefault`, replace `want := workflow.BoardOpenTasks(b.m.projectScope)` with `want := b.m.reg.DefaultBoard(b.m.projectScope)` and drop the workflow import. An empty `want` matches no row, so the existing first-board fallback covers a registry with no nomination — no other logic changes.

In `cmd/atm/main.go`, the `runTUI` closure captures the registry:

```go
	runTUI := func(storePath, actor string) error {
		root := store.ResolveStorePath(storePath)
		s, err := store.Open(root)
		if err != nil {
			return err
		}
		return tui.Run(s, actor, reg)
	}
```

(`reg` must now be declared before `runTUI`.)

- [ ] **Step 3: Wire tui tests with the workflow-only registry (preserves current expectations)**

Every `NewModelOpts{...}` literal in `internal/tui/*_test.go` (find them: `grep -rn "NewModelOpts{" internal/tui/*_test.go`) gains `Registry: capability.NewRegistry(workflow.New())`. Add whichever of `"atm/internal/capability"` and `workflow "atm/internal/capability/workflow"` each test file lacks (most board-exercising test files already import workflow; `bench_lag_test.go` and `refresh_tick_test.go` may not). Using workflow-only (not the full production registry) keeps board-ring expectations exactly as today; the production two-capability behavior is covered by the cli golden harness.

- [ ] **Step 4: Run the tui and cli suites; regenerate goldens changed by eager seeding**

Run: `go test ./internal/tui/`
Expected: PASS with no expectation changes (workflow-only registry reproduces today's seeding).

Run: `go test ./internal/cli/`
Expected: FAILURES ONLY in goldens whose flows run `project create` or `label seed` — the harness registry now seeds contextmap's vocabulary eagerly (spec decision 2). Note `project create` already seeds a default label set that includes the `context:<kind>` labels, and `LabelSeed` upserts only absent labels, so the eager-seed delta is the contextmap-only vocabulary: `<CODE>:knowledge:*`, `<CODE>:knowledge:superseded`, `<CODE>:comment:provenance`, the `<CODE>:context-current` board, and any `context:<kind>` the seed set lacks — plus the event-log/sequence shifts those writes cause. Regenerate and audit:

```bash
go test ./internal/cli/ -update
go test ./internal/cli/
git diff --stat internal/cli/testdata/
```

Expected: second run PASS. Inspect `git diff internal/cli/testdata/` — every hunk must be attributable to the eager contextmap vocabulary (new label names above, and event-log/sequence shifts they cause). Any other diff is a regression: STOP and investigate before committing.

- [ ] **Step 5: Confirm the adapters are capability-clean**

```bash
grep -rn "atm/internal/capability/" internal/cli/ internal/tui/ --include='*.go' | grep -v _test
```

Expected: no output (the bare registry import `"atm/internal/capability"` doesn't match the trailing-slash pattern; only package-specific imports would).

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor(ATM-08db6e): cli and tui consume only the capability registry; eager vocabulary seeding"
```

---

### Task 8: Tighten the arch tests; amend the architecture doc

**Files:**
- Modify: `tests/arch/imports_test.go`, `docs/architecture/logical-components.md`

**Interfaces:**
- Consumes: the final package layout from Tasks 1-7.
- Produces: enforced import rules for the capability layer.

- [ ] **Step 1: Write the new arch tests (they should pass immediately — the boundary already holds; a failure means Tasks 1-7 left a stray edge)**

In `tests/arch/imports_test.go`:

Replace `TestWorkflowDoesNotImportStore` (now subsumed) with:

```go
// TestCapabilityRegistryImportsOnlyCore pins the registry package as a
// near-leaf: it may import only the domain core (plus cobra externally).
func TestCapabilityRegistryImportsOnlyCore(t *testing.T) {
	for f, imps := range internalImports(t, "internal/capability") {
		for _, p := range imps {
			if p != "atm/internal/core" {
				t.Errorf("%s imports %q; internal/capability may import only atm/internal/core", f, p)
			}
		}
	}
}

// TestCapabilityPackagesImportOnlyRegistryAndCore is refactor step 5's
// boundary: a capability owns its label slice and its cobra command, and
// reaches nothing but the registry seam and the domain leaf — never the
// store, the cli, or the tui.
func TestCapabilityPackagesImportOnlyRegistryAndCore(t *testing.T) {
	for _, dir := range []string{"internal/capability/contextmap", "internal/capability/workflow"} {
		for f, imps := range internalImports(t, dir) {
			for _, p := range imps {
				if p != "atm/internal/capability" && p != "atm/internal/core" {
					t.Errorf("%s imports %q; capability packages may import only the registry and core", f, p)
				}
			}
		}
	}
}

// TestAdaptersDoNotImportCapabilityPackages pins the other side of the
// seam: cli and tui consume capabilities only through the registry the
// composition root assembles — neither adapter names a capability.
func TestAdaptersDoNotImportCapabilityPackages(t *testing.T) {
	for _, dir := range []string{"internal/cli", "internal/tui"} {
		for f, imps := range internalImports(t, dir) {
			for _, p := range imps {
				if strings.HasPrefix(p, "atm/internal/capability/") {
					t.Errorf("%s imports %q; adapters consume only the registry (atm/internal/capability)", f, p)
				}
			}
		}
	}
}
```

Also update the tui comment block above `TestTUIDoesNotImportStore`: the sentence "Relocating a capability (workflow) belongs to step 5 (ATM-08db6e)" is now stale — replace with "Step 5 (ATM-08db6e) relocated the capabilities and put the TUI on the registry; the satellites (activity, seed, embed) remain acknowledged thin leaves." Keep `TestCoreIsAPureLeaf`, `TestVersionImportsNoInternalPackage`, `TestTUIDoesNotImportStore`, `TestCLIDoesNotImportTUI` as they are.

- [ ] **Step 2: Run the arch tests**

Run: `go test ./tests/arch/`
Expected: PASS. A failure here is a real stray edge from Tasks 1-7 — fix the edge, not the test.

- [ ] **Step 3: Amend the architecture doc**

In `docs/architecture/logical-components.md`:

- In the import-rules table, change the `internal/cli` row's "May import (internal)" to: `core`, `capability` (registry only), satellites; `store` only until step 6 moves the remaining admin surface behind interfaces.
- Change the `internal/tui` row to: `core`, `capability` (registry only), `tui/components` — plus the acknowledged satellites until they are purged.
- Change the `internal/capability/*` row to: `capability`, `core`. Add a row `internal/capability` → nothing internal but `core`.
- In the component table, update the `internal/cli` "Responsibility" cell: the capability registry is now assembled by `cmd/atm` and consumed by cli, not hosted in it — reword "Hosts the capability registry so plugged commands mount without `cli` importing their internals" to "Mounts the capability registry's commands; the registry itself is assembled by `cmd/atm`".
- In the migration table, no change to the step-5 row (it describes this work).

Keep prose as single un-wrapped lines.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "test(ATM-08db6e): enforce capability-layer import rules; amend architecture doc"
```

---

### Task 9: Full verification, smoke test, ledger close-out

**Files:**
- No production changes. Ledger comments via `atm`.

- [ ] **Step 1: Full verify**

Run: `make verify`
Expected: build OK, `go test ./... ./libs/eventsource/...` all ok, script tests pass (26/26 at last count). Fix anything red before proceeding.

- [ ] **Step 2: Behavioral smoke test against a throwaway store**

Verified bootstrap (2026-07-17): `project create` auto-inits a fresh `--store` dir, and the actor MUST be `admin@...` — persona enforcement rejects unregistered persona actors.

```bash
SCRATCH=/tmp/claude-1000/-home-ttran-projects-scyllas-atm/616ceb42-5717-4c5c-b341-a7d7841ff309/scratchpad
go build -o "$SCRATCH/atm-smoke" ./cmd/atm
SMOKE=$(mktemp -d)
"$SCRATCH/atm-smoke" project create --code DEMO --name Demo --actor admin@cli:test --store "$SMOKE/store"
"$SCRATCH/atm-smoke" label list --project DEMO --store "$SMOKE/store"
```

Expected: the label list shows BOTH capabilities' vocabulary after a bare `project create` — workflow's three boards (`DEMO:backlog`, `DEMO:open-tasks`, `DEMO:in-progress-tasks`) AND contextmap's `DEMO:knowledge:*`, `DEMO:knowledge:superseded`, `DEMO:comment:provenance`, `DEMO:context-current` — this is the eager-seeding acceptance check. Then (task create prints the new ID, e.g. `created task DEMO-d6895c`):

```bash
"$SCRATCH/atm-smoke" task create --project DEMO --title "smoke" --actor admin@cli:test --store "$SMOKE/store"
"$SCRATCH/atm-smoke" workflow start --task <ID-from-previous-output> --actor admin@cli:test --store "$SMOKE/store"
"$SCRATCH/atm-smoke" workflow status --task <ID> --store "$SMOKE/store"
"$SCRATCH/atm-smoke" context add --task <ID> --kind documentation --source file:README.md --actor admin@cli:test --store "$SMOKE/store"
"$SCRATCH/atm-smoke" context check --project DEMO --store "$SMOKE/store"
rm -rf "$SMOKE"
```

Expected: `<ID>: status -> in-progress`, then `in-progress`, then `stamped <ID> as context:documentation`, then a check report with the pointer in OK or AGE. NEVER point the smoke binary at `~/.config/atm`.

- [ ] **Step 3: Ledger close-out**

```bash
atm task comment add --task ATM-08db6e --actor developer@claude:fable-5 --label ATM:comment:progress --body "Implementation complete on branch atm-08db6e-capability-registry: internal/capability registry (Capability/Registry/Env), contextmap and workflow relocated with their cobra layers, cli+tui registry-only, arch tests tightened, architecture doc amended. make verify green; help surface byte-identical; eager vocabulary seeding verified in smoke test. Ready for merge review."
```

Do NOT set the task's status to done yet — that happens after merge (superpowers:finishing-a-development-branch).

- [ ] **Step 4: Final commit if the ledger step touched tracked files (it should not)**

Run: `git status --short`
Expected: clean tree.

---

## Self-Review Notes (already applied)

- Spec coverage: package layout (T1-3), interface+registry (T1), Env (T4), cobra moves + byte-identical surface (T5-6), registry-only adapters + DefaultBoard + eager seeding (T7), import rules + doc amendment (T8), verification (T9). Decision 3's registration order (workflow first) lands in T6 step 2.
- Type consistency: `Service` composite (T2) is what `newRecorder` assigns a `core.Service` into (T5) — legal, `Service`'s method set is a subset of `core.Service`'s. `Cap`/`New()` names are identical in both capability packages (different packages, no collision). `testRegistry()` evolves T4→T5→T6; each task shows its full body.
- The `-update` golden regeneration in T7 is deliberately followed by a clean re-run plus a manual diff audit — eager seeding is the only sanctioned diff source.
