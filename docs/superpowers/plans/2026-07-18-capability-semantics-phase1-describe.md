# Capability Semantics Phase 1 — Capabilities Describe Themselves

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Capabilities carry their own agent-facing semantics: a one-line `Summary()`, a full `Guide()` served by a uniform `guide` subcommand, `atm conventions` shrunk to substrate core + capability enumeration, and the mapping procedure moved out of the manager prompt into contextmap's guide.

**Architecture:** Extend the `internal/capability.Capability` interface with `Summary()`/`Guide()`; the `Registry` mounts a uniform `guide` subcommand on every capability command tree and exposes `Describe(env)` for enumeration. `internal/cli/conventions.go` loses its hand-written workflow/context prose and renders the enumeration from the registry. `internal/manager/context_v1.md` points at `atm context guide` instead of embedding the mapping procedure. No store changes, no gating.

**Tech Stack:** Go, cobra, `go:embed`, the existing golden-test harness (`internal/cli/harness_test.go`).

**Spec:** `docs/superpowers/specs/2026-07-18-capability-semantics-initiative-design.md` (Phase 1 section). Ledger task: `ATM-b678b0`.

## Global Constraints

- Arch tests (`tests/arch/imports_test.go`) pin: `internal/capability` imports only `atm/internal/core` (+cobra); capability packages import only `atm/internal/capability` + `atm/internal/core`; cli/tui never import capability packages directly. Do not add imports that violate these.
- The golden harness registry (`internal/cli/harness_test.go:81` `testRegistry()`) must stay in sync with `cmd/atm/main.go:19`. This phase changes neither list.
- Regenerate goldens with `go test ./internal/cli -update` (the `-update` flag is defined in `harness_test.go`), then re-run without `-update` to verify.
- Commit style: `type(ATM-b678b0): message`, footer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Run `gofmt -l` (or `go vet ./...`) before each commit; all new files gofmt-clean.
- Guide text is agent-facing prose. It must never hardcode a label string a verb owns exclusively (e.g. `knowledge:superseded` may be *explained* in the contextmap guide as owned-by-verbs, but instructions must say "use the verbs", mirroring the current conventions wording).

---

### Task 1: Workflow capability guide + summary

**Files:**
- Create: `internal/capability/workflow/guide.md`
- Create: `internal/capability/workflow/guide.go`
- Test: `internal/capability/workflow/guide_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: methods `(Cap) Summary() string` and `(Cap) Guide() string` on the existing `workflow.Cap` type (`internal/capability/workflow/command.go:14`). Task 3 promotes these to the `capability.Capability` interface.

- [ ] **Step 1: Write the failing test**

```go
package workflow

import (
	"strings"
	"testing"
)

func TestSummaryIsOneLine(t *testing.T) {
	s := Cap{}.Summary()
	if s == "" || strings.Contains(s, "\n") {
		t.Fatalf("Summary must be one non-empty line, got %q", s)
	}
}

// The guide is the single source of the capability's semantics: it must name
// every verb and the invariant the capability maintains, and carry a manager
// section (the composed manager prompt points here).
func TestGuideCarriesSemantics(t *testing.T) {
	g := Cap{}.Guide()
	for _, want := range []string{
		"atm workflow start", "atm workflow open", "atm workflow block",
		"atm workflow complete", "atm workflow status", "atm workflow seed",
		"exactly-one-status", "backlog", "open-tasks", "in-progress-tasks",
		"all-tasks", "## Manager duty", "paved road, not a fence",
	} {
		if !strings.Contains(g, want) {
			t.Errorf("guide missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/capability/workflow -run 'TestSummary|TestGuide' -v`
Expected: FAIL — `Cap{}.Summary undefined` (compile error).

- [ ] **Step 3: Write the guide content**

Create `internal/capability/workflow/guide.md` with exactly this content (prose relocated from `internal/cli/conventions.go` "Workflow verbs" + boards paragraph; the `all-tasks` board from the workflow capability design):

````markdown
# Workflow capability — agent guide

Status transitions for tasks: the paved road for the `status:*` namespace.

## What it means

Status transitions are exposed as four mutating verbs — `atm workflow start` (in-progress), `atm workflow open`, `atm workflow block` (blocked), `atm workflow complete` (done) — plus a read-only `atm workflow status` reporter and `atm workflow seed` to ensure the boards. Each mutating verb swaps the task's `status:*` label (adds the target, then removes any other), so exactly-one-status is an invariant the capability maintains. The store still enforces nothing: raw `atm task label add/remove --label <CODE>:status:<value>` works and a human may hand-assign, rename, or delete any status label. This capability is a paved road, not a fence — a project can replace it with a different transition model.

## Vocabulary

`status:open` means not done; `status:in-progress` means someone is on it; `status:blocked` means stuck; `status:done` means stop.

Boards ensured on project create / label seed / TUI use:

- `<CODE>:backlog` (`NOT status:*`) — untriaged jottings.
- `<CODE>:open-tasks` (`status:open`) — active work.
- `<CODE>:in-progress-tasks` (`status:in-progress`).
- `<CODE>:all-tasks` — every task; the default board a UI selects.

In an older project where a board is absent, the expression fallback applies (`--label <CODE>:status:open` etc.).

## How to use it

Prefer the verbs over raw `task label add/remove --label status:*`. Check where a task stands with `atm workflow status --task <ID>`; claim work with `atm workflow start --task <ID>`; finish with `atm workflow complete --task <ID>`. Review untriaged jottings via `atm task list --project <CODE> --label <CODE>:backlog`.

## Manager duty

None. This capability contributes no dedicated manager action: status hygiene (untriaged tasks, stale in-progress work) falls under the manager's core curate role.
````

- [ ] **Step 4: Implement Summary/Guide**

Create `internal/capability/workflow/guide.go`:

```go
package workflow

import _ "embed"

//go:embed guide.md
var guideText string

// Summary is the capability's one-line description, used wherever
// capabilities are enumerated (conventions, manager prompt).
func (Cap) Summary() string {
	return "Status-transition verbs and boards — the paved road for task status."
}

// Guide is the capability's full agent-facing semantics; `atm workflow guide`
// prints it. The capability explains itself: this text is the single source,
// composed surfaces (conventions, manager prompt) only point here.
func (Cap) Guide() string { return guideText }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/capability/workflow -v`
Expected: PASS (all package tests, not just the new ones).

- [ ] **Step 6: Commit**

```bash
git add internal/capability/workflow/guide.md internal/capability/workflow/guide.go internal/capability/workflow/guide_test.go
git commit -m "feat(ATM-b678b0): workflow capability describes itself (Summary/Guide)"
```

---

### Task 2: Contextmap capability guide + summary

**Files:**
- Create: `internal/capability/contextmap/guide.md`
- Create: `internal/capability/contextmap/guide.go`
- Test: `internal/capability/contextmap/guide_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `(Cap) Summary() string`, `(Cap) Guide() string` on `contextmap.Cap` (`internal/capability/contextmap/command.go:17`). The guide's `## Manager duty` section absorbs the mapping procedure currently hardcoded in `internal/manager/context_v1.md:22-30` (Task 5 deletes it there).

- [ ] **Step 1: Write the failing test**

```go
package contextmap

import (
	"strings"
	"testing"
)

func TestSummaryIsOneLine(t *testing.T) {
	s := Cap{}.Summary()
	if s == "" || strings.Contains(s, "\n") {
		t.Fatalf("Summary must be one non-empty line, got %q", s)
	}
}

// The guide absorbs the mapping procedure that used to live in the manager
// prompt template: verbs, the check report vocabulary, and the three-step
// manager duty must all be present here, because nothing else states them.
func TestGuideCarriesSemanticsAndManagerDuty(t *testing.T) {
	g := Cap{}.Guide()
	for _, want := range []string{
		"atm context add", "atm context stamp", "atm context retarget",
		"atm context supersede", "atm context check",
		"DRIFT", "AGE", "UNVERIFIED", "NEW",
		"context-current",
		"## Manager duty",
		"1. **Verify.**", "2. **Discover.**", "3. **Close.**",
	} {
		if !strings.Contains(g, want) {
			t.Errorf("guide missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/capability/contextmap -run 'TestSummary|TestGuide' -v`
Expected: FAIL — `Cap{}.Summary undefined` (compile error).

- [ ] **Step 3: Write the guide content**

Create `internal/capability/contextmap/guide.md` with exactly this content (the "What it means / How to use it" prose relocated from conventions' "The context map" section; the "Manager duty" steps relocated from `internal/manager/context_v1.md:23-30` with `<ATM_BIN>` rendered as plain `atm`):

````markdown
# Context map capability — agent guide

Context pointers with provenance: record what knowledge derives from, so drift can be detected.

## What it means

Context pointers record what they were derived from, so drift can be detected. `atm context check --project <CODE>` reports which pointers have gone stale against the repo (DRIFT), which name external systems nobody has re-read lately (AGE), and which were never verified (UNVERIFIED). It is read-only — it tells you where to look, never what it means.

## How to use it

Record a pointer with `atm context add`; re-verify one with `atm context stamp`; repoint a subject that moved with `atm context retarget`; retire one whose subject died with `atm context supersede`. These verbs own the context vocabulary — do not hand-assign the labels or hand-edit a provenance comment.

Read the project's current knowledge from the `<CODE>:context-current` board (`atm task list --project <CODE> --label <CODE>:context-current`): agent directions, repository pointers, and documentation pointers that have not been superseded. Read this board rather than a raw namespace; membership is computed, so it is always the latest. Narrow by kind with an extra `--label <CODE>:context:agent`.

## Manager duty

Mapping — reconcile the project's context map against reality. Repeatable, and meant to be run often; the first run in a fresh repo is just the case where there is nothing yet to verify.

1. **Verify.** Run `atm context check --project <CODE>`. Work the report:
   - `DRIFT` — read the pointer's description against the actual change. If the description still tells the truth, `atm context stamp --task <ID>`. If the subject survived but moved, `atm context retarget --task <ID> --source <kinded-locator>`. If the subject died or was replaced, create the successor and `atm context supersede --task <ID> --by <NEW-ID> --reason "..."`.
   - `AGE` — an external source (Jira, Notion) that nothing can witness locally. Re-read it with your own tools, then `stamp`.
   - `UNVERIFIED` — a pointer someone wrote by hand. Read it, confirm it is true, then `atm context add --task <ID> --kind <kind> --source <kinded-locator>`.
2. **Discover.** Work the `NEW` list: territory that changed in git and that no pointer claims. For each thing worth knowing, create a task and `atm context add` it. Ignore what is not worth a pointer — that is a judgement, and it is yours.
3. **Close.** Everything reported is now stamped, retargeted, superseded, or deliberately ignored.

`check` never marks anything stale: a changed file is not a wrong pointer. It tells you where to look; you decide what it means.
````

- [ ] **Step 4: Implement Summary/Guide**

Create `internal/capability/contextmap/guide.go`:

```go
package contextmap

import _ "embed"

//go:embed guide.md
var guideText string

// Summary is the capability's one-line description, used wherever
// capabilities are enumerated (conventions, manager prompt).
func (Cap) Summary() string {
	return "Context pointers with provenance — record what knowledge derives from, detect drift."
}

// Guide is the capability's full agent-facing semantics; `atm context guide`
// prints it. Its Manager-duty section is the mapping procedure the manager
// prompt used to hardcode — the prompt now points here.
func (Cap) Guide() string { return guideText }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/capability/contextmap -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/capability/contextmap/guide.md internal/capability/contextmap/guide.go internal/capability/contextmap/guide_test.go
git commit -m "feat(ATM-b678b0): contextmap capability describes itself; guide absorbs the mapping procedure"
```

---

### Task 3: Interface extension, registry Describe, uniform guide subcommand

**Files:**
- Modify: `internal/capability/capability.go`
- Test: `internal/capability/guide_test.go` (new file)

**Interfaces:**
- Consumes: `workflow.Cap`/`contextmap.Cap` already have `Summary()`/`Guide()` (Tasks 1–2), so extending the interface compiles.
- Produces (later tasks rely on these exact names):
  - `Capability` interface gains `Summary() string` and `Guide() string`.
  - `type Description struct { Name, Command, Summary string }`
  - `func (r *Registry) Describe(env Env) []Description` — nil-safe, registration order; `Command` is the mounted cobra command name (`workflow`, `context`).
  - Every tree returned by `Registry.Commands(env)` now contains a `guide` subcommand printing `Guide()`.

- [ ] **Step 1: Write the failing test**

Create `internal/capability/guide_test.go`. If any existing test fake in `internal/capability/*_test.go` implements `Capability`, add `Summary`/`Guide` methods to it in this same task (compile break is expected and is the failing state).

```go
package capability

import (
	"bytes"
	"io"
	"testing"

	"atm/internal/core"

	"github.com/spf13/cobra"
)

type fakeEnv struct{ out bytes.Buffer }

func (f *fakeEnv) OpenService() (core.Service, error)          { return nil, nil }
func (f *fakeEnv) Stdout() io.Writer                           { return &f.out }
func (f *fakeEnv) Stderr() io.Writer                           { return io.Discard }
func (f *fakeEnv) Emit(v any, textFn func()) error             { textFn(); return nil }
func (f *fakeEnv) RequireMutatingActor() (string, error)       { return "t@t:t", nil }
func (f *fakeEnv) ResolveActor(bool) (string, error)           { return "t@t:t", nil }
func (f *fakeEnv) BindActorFlag(*cobra.Command)                {}
func (f *fakeEnv) BindTaskIDFlags(*cobra.Command, *string, *string) {}
func (f *fakeEnv) ResolveTaskID(id, _ string) (string, error)  { return id, nil }
func (f *fakeEnv) TaskJSON(t *core.Task) any                   { return t }

type fakeCap struct{ name, cmdName, summary, guide string }

func (c fakeCap) Name() string    { return c.name }
func (c fakeCap) Summary() string { return c.summary }
func (c fakeCap) Guide() string   { return c.guide }
func (c fakeCap) EnsureVocabulary(core.LabelService, string, string) error { return nil }
func (c fakeCap) Command(env Env) *cobra.Command { return &cobra.Command{Use: c.cmdName} }
func (c fakeCap) DefaultBoard(string) string     { return "" }

func TestDescribeEnumeratesInRegistrationOrder(t *testing.T) {
	r := NewRegistry(
		fakeCap{name: "alpha", cmdName: "al", summary: "does alpha"},
		fakeCap{name: "beta", cmdName: "be", summary: "does beta"},
	)
	got := r.Describe(&fakeEnv{})
	want := []Description{
		{Name: "alpha", Command: "al", Summary: "does alpha"},
		{Name: "beta", Command: "be", Summary: "does beta"},
	}
	if len(got) != len(want) {
		t.Fatalf("Describe len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Describe[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDescribeNilRegistry(t *testing.T) {
	var r *Registry
	if got := r.Describe(&fakeEnv{}); got != nil {
		t.Fatalf("nil registry Describe = %v, want nil", got)
	}
}

func TestCommandsMountGuideSubcommand(t *testing.T) {
	env := &fakeEnv{}
	r := NewRegistry(fakeCap{name: "alpha", cmdName: "al", summary: "does alpha", guide: "GUIDE BODY\n"})
	cmds := r.Commands(env)
	if len(cmds) != 1 {
		t.Fatalf("Commands len = %d, want 1", len(cmds))
	}
	var guide *cobra.Command
	for _, sub := range cmds[0].Commands() {
		if sub.Name() == "guide" {
			guide = sub
		}
	}
	if guide == nil {
		t.Fatal("no guide subcommand mounted")
	}
	if err := guide.RunE(guide, nil); err != nil {
		t.Fatalf("guide RunE: %v", err)
	}
	if env.out.String() != "GUIDE BODY\n" {
		t.Errorf("guide output = %q, want the Guide() text", env.out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/capability -v`
Expected: FAIL — `r.Describe undefined`, `fakeCap` does not implement `Capability` only if interface already extended; at minimum the Describe/guide assertions fail to compile.

- [ ] **Step 3: Implement**

In `internal/capability/capability.go`:

(a) Extend the interface — add to `Capability` (after `Name()`):

```go
	// Summary is a one-line description for enumeration surfaces
	// (conventions, manager prompt). No trailing newline.
	Summary() string
	// Guide is the capability's full agent-facing semantics: vocabulary
	// meaning, verb usage, operating procedure, and a "Manager duty"
	// section. Served verbatim by the uniform `guide` subcommand.
	Guide() string
```

(b) Add the Description type and Describe method:

```go
// Description is one capability's enumeration entry: its stable name, the
// cobra command it mounts as (what an agent types), and its one-line summary.
type Description struct {
	Name    string
	Command string
	Summary string
}

// Describe enumerates the registered capabilities in registration order.
// Command is taken from the built command tree so the consult instruction
// can never drift from what is actually mounted.
func (r *Registry) Describe(env Env) []Description {
	if r == nil {
		return nil
	}
	out := make([]Description, 0, len(r.caps))
	for _, c := range r.caps {
		out = append(out, Description{Name: c.Name(), Command: c.Command(env).Name(), Summary: c.Summary()})
	}
	return out
}
```

(c) Mount the guide subcommand uniformly — replace the body of `Commands`:

```go
// Commands returns each capability's command tree in registration order.
// The registry, not the capability, mounts the uniform `guide` subcommand,
// so its shape is identical everywhere and cannot be forgotten.
func (r *Registry) Commands(env Env) []*cobra.Command {
	if r == nil {
		return nil
	}
	out := make([]*cobra.Command, 0, len(r.caps))
	for _, c := range r.caps {
		cmd := c.Command(env)
		cmd.AddCommand(newGuideCmd(c, env))
		out = append(out, cmd)
	}
	return out
}

// newGuideCmd is the uniform read-only guide printer. It opens no store.
func newGuideCmd(c Capability, env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "guide",
		Short: "Print this capability's agent guide (semantics and operating mode)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return env.Emit(map[string]any{
				"capability": c.Name(),
				"summary":    c.Summary(),
				"guide":      c.Guide(),
			}, func() {
				fmt.Fprint(env.Stdout(), c.Guide())
			})
		},
	}
}
```

Add `"fmt"` to the imports.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/... ./tests/arch -v`
Expected: PASS (arch tests confirm no forbidden imports were added).

- [ ] **Step 5: Verify the real commands end-to-end**

Run: `go run ./cmd/atm workflow guide | head -3` and `go run ./cmd/atm context guide | head -3`
Expected: each prints its guide.md heading. Also `go run ./cmd/atm --output json context guide | head -1` prints a JSON envelope with `capability`, `summary`, `guide` keys.

- [ ] **Step 6: Commit**

```bash
git add internal/capability/capability.go internal/capability/guide_test.go
git commit -m "feat(ATM-b678b0): Capability interface gains Summary/Guide; registry mounts uniform guide subcommand and Describe()"
```

---

### Task 4: Conventions = substrate core + capability enumeration

**Files:**
- Modify: `internal/cli/conventions.go`
- Modify: `internal/cli/conventions_test.go`
- Modify: `internal/cli/testdata/golden/conventions-text.json`, `internal/cli/testdata/golden/conventions-json.json` (regenerated)

**Interfaces:**
- Consumes: `st.registry.Describe(st)` (Task 3; `cliState` implements `capability.Env`, see `internal/cli/env.go:16`).
- Produces: `conventionsText` renamed to `conventionsCoreText` (capability prose removed); new `func capabilitiesSection(descs []capability.Description) string`; `conventionsStructured(descs []capability.Description) map[string]any` (signature change). Text output = core + section; JSON gains `"capabilities"` key, loses `"workflow_verbs"` and `"context_map"`.

- [ ] **Step 1: Write the failing tests**

In `internal/cli/conventions_test.go`, replace the workflow/context substring tests (`TestConventionsMentionWorkflowVerbs`, `TestConventionsPointAtCurrentKnowledgeBoard`, and any other test asserting workflow/context prose — read the file and migrate each) with:

```go
// Conventions enumerate capabilities; they no longer restate any
// capability's semantics. The enumeration is rendered from the registry, so
// a fake registry must surface its own entries verbatim.
func TestConventionsEnumerateCapabilities(t *testing.T) {
	h := newGoldenHarness(t)
	out, _, code := h.run("conventions")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{
		"## Capabilities",
		"workflow", "`atm workflow guide`",
		"contextmap", "`atm context guide`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("conventions missing %q", want)
		}
	}
}

// The de-restatement guarantee: capability-owned prose is gone from the
// static text. These fragments now live only in the capability guides.
func TestConventionsCarryNoCapabilityProse(t *testing.T) {
	for _, banned := range []string{
		"atm workflow start", "exactly-one-status",
		"atm context stamp", "atm context supersede",
		"DRIFT", "UNVERIFIED",
	} {
		if strings.Contains(conventionsCoreText, banned) {
			t.Errorf("conventionsCoreText still restates capability prose %q", banned)
		}
	}
}

func TestConventionsJSONEnumeratesCapabilities(t *testing.T) {
	h := newGoldenHarness(t)
	out, _, code := h.run("--output", "json", "conventions")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var envl struct {
		Conventions map[string]any `json:"conventions"`
	}
	if err := json.Unmarshal([]byte(out), &envl); err != nil {
		t.Fatal(err)
	}
	caps, ok := envl.Conventions["capabilities"].([]any)
	if !ok || len(caps) != 2 {
		t.Fatalf("capabilities = %v, want 2 entries", envl.Conventions["capabilities"])
	}
	for _, gone := range []string{"workflow_verbs", "context_map"} {
		if _, exists := envl.Conventions[gone]; exists {
			t.Errorf("JSON still carries removed key %q", gone)
		}
	}
}
```

Add `"encoding/json"` and `"strings"` to the test file's imports if absent.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli -run TestConventions -v`
Expected: FAIL (compile: `conventionsCoreText` undefined; enumeration assertions fail).

- [ ] **Step 3: Rework `internal/cli/conventions.go`**

(a) Rename `conventionsText` → `conventionsCoreText` and edit the string:

1. **Delete** the entire `## The context map` section (lines 61-65 of the current file).
2. **Delete** the entire `## Workflow verbs (status transitions)` section including the boards paragraph (lines 67-71).
3. In `## Agent first-contact sequence`, replace steps 3, 4, 5 and renumber. The sequence becomes:

```
1. Read this guide, including the code-of-conduct below.
2. `atm label list --project <CODE>` — read every label's description first to learn the project's vocabulary before touching tasks. Labels are the project's language; knowing them makes every task query meaningful. Do not assume the seeded defaults are all the labels this project uses.
3. Read the Capabilities section of this guide, then run each listed capability's guide (`atm <command> guide`) before operating in its territory. Capabilities own their semantics — how to read the project's knowledge, how to move work through status, whatever else the project enabled — and this document does not restate them.
4. `atm task list --project <CODE>` — see the project's tasks; use the boards the label list revealed for the groupings the project actually uses.
5. `atm store log <CODE>` — read the project's append-only audit log to observe recent activity.
6. `atm task comment list --task <ID>` — read the running narrative on a task before acting on it.
7. `atm index models --project <CODE>` — see which embedding models have a vector index. If none, configure one with `atm project set-embedding` then `atm embed --project <CODE>`.
8. `atm search --project <CODE> "query"` — semantic search over tasks + comments (text fallback if no index).
9. To get a synthesized answer, run a manager recall session with `atm manage --project <CODE> --recall`.
```

4. In `## How to read a task and its labels`, delete the sentence fragment restating status semantics (`` `status:open` means not done; `status:in-progress` means someone is on it; `status:done` means stop.``) — that is workflow vocabulary, now stated in its guide. Keep the rest of the paragraph.
5. Leave everything else (labels, tasks, comments, search, boards, actor identity, code-of-conduct, human sequence, notes, the advisory last line) unchanged.

(b) Add the enumeration renderer:

```go
// capabilitiesSection renders the enumeration of registered capabilities.
// It is composed from the registry — never hand-written — so adding a
// capability changes conventions without touching this file.
func capabilitiesSection(descs []capability.Description) string {
	var b strings.Builder
	b.WriteString("## Capabilities\n\n")
	b.WriteString("Semantics beyond the substrate live in capabilities. Each capability owns a slice of the label substrate and explains itself: consult its guide before operating in its territory.\n\n")
	for _, d := range descs {
		fmt.Fprintf(&b, "- **%s** (`atm %s`) — %s Consult: `atm %s guide`.\n", d.Name, d.Command, d.Summary, d.Command)
	}
	return b.String()
}
```

Add `"strings"` and `"atm/internal/capability"` to the imports (the arch test `TestAdaptersDoNotImportCapabilityPackages` allows the registry package itself — only `capability/workflow` and `capability/contextmap` are forbidden).

(c) In `newConventionsCmd`, render core + section, and thread descriptions into the JSON:

```go
func newConventionsCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conventions",
		Short: "Print the onboarding guide and suggested label namespaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			descs := st.registry.Describe(st)
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"conventions": conventionsStructured(descs)})
			}
			fmt.Fprint(st.stdout(), conventionsCoreText)
			fmt.Fprint(st.stdout(), "\n"+capabilitiesSection(descs))
			return nil
		},
	}
	return cmd
}
```

Note: `conventionsCoreText` currently ends with the advisory line. Move the advisory line AFTER the capabilities section instead: delete the final line `Conventions are advisory only — nothing in the store validates or special-cases the documented namespaces.` from the core text and append it in the renderer:

```go
			fmt.Fprint(st.stdout(), conventionsCoreText)
			fmt.Fprint(st.stdout(), "\n"+capabilitiesSection(descs))
			fmt.Fprint(st.stdout(), "\nConventions are advisory only — nothing in the store validates or special-cases the documented namespaces.\n")
```

(d) In `conventionsStructured`, change the signature to `func conventionsStructured(descs []capability.Description) map[string]any`, delete the `"workflow_verbs"` and `"context_map"` entries, delete the status-semantics sentence from `"how_to_read_a_task_and_its_labels"`, replace the `"agent_first_contact_sequence"` list to mirror the new 9-step text sequence exactly, and add:

```go
		"capabilities": func() []map[string]string {
			out := make([]map[string]string, 0, len(descs))
			for _, d := range descs {
				out = append(out, map[string]string{
					"name":    d.Name,
					"command": d.Command,
					"summary": d.Summary,
					"consult": "atm " + d.Command + " guide",
				})
			}
			return out
		}(),
```

- [ ] **Step 4: Run tests, regenerate goldens**

Run: `go test ./internal/cli -run TestConventions -v` — the new substring tests PASS, the two golden tests FAIL on diff.
Run: `go test ./internal/cli -update` then `go test ./internal/cli`
Expected: all PASS. Inspect the regenerated `conventions-text.json` diff: the Capabilities section present, workflow/context sections gone, advisory line last.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/conventions.go internal/cli/conventions_test.go internal/cli/testdata/golden/
git commit -m "feat(ATM-b678b0): conventions = substrate core + capability enumeration; capability prose moves to guides"
```

---

### Task 5: Manager prompt points at the contextmap guide

**Files:**
- Modify: `internal/manager/context_v1.md`
- Modify: `internal/manager/context_test.go`
- Possibly regenerated: `internal/cli/testdata/golden/manage-*.json` (launch goldens; only if they embed rendered context)

**Interfaces:**
- Consumes: `atm context guide` exists (Task 3) and carries the procedure (Task 2).
- Produces: `context_v1.md` whose Mapping role is a pointer, not a procedure. `RenderContext` (`internal/manager/context.go`) is unchanged in this task.

- [ ] **Step 1: Write the failing test**

Append to `internal/manager/context_test.go`:

```go
// The mapping procedure lives in the contextmap guide (capability obligation
// 4: it explains itself). The prompt template must point at the guide and
// must not restate any step of the procedure — restated prose is the drift
// class this initiative removes (see ATM-0114 for the original bug).
func TestTemplateMappingRolePointsAtGuide(t *testing.T) {
	rendered := RenderContext(ContextData{Code: "X", Name: "X", ATMBin: "atm", Actor: "a@b:c"})
	if !strings.Contains(rendered, "atm context guide") {
		t.Error("Mapping role must tell the manager to consult `atm context guide`")
	}
	for _, banned := range []string{"context stamp", "context retarget", "context supersede", "DRIFT", "UNVERIFIED", "**Verify.**", "**Discover.**"} {
		if strings.Contains(rendered, banned) {
			t.Errorf("template still restates mapping procedure fragment %q", banned)
		}
	}
}
```

Add `"strings"` to imports if absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manager -run TestTemplateMappingRole -v`
Expected: FAIL on the banned fragments.

- [ ] **Step 3: Edit the template**

In `internal/manager/context_v1.md`, replace the whole Mapping role entry (line 21 through line 30 — the bullet, the numbered Verify/Discover/Close steps, and the closing `check` paragraph) with exactly:

```markdown
- **Mapping** — reconcile the project's context map against reality; this is the contextmap capability's manager duty. Run `<ATM_BIN> context guide` and follow its "Manager duty" section for the operating procedure. Repeatable, and meant to be run often; the first run in a fresh repo is just the case where there is nothing yet to verify.
```

Leave Curate and Recall untouched.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/manager -v` — PASS (fix any older assertion in `context_test.go` that pinned the deleted steps: delete those assertions, they are superseded by the guide tests of Task 2).
Run: `go test ./internal/cli -run 'TestGoldenManage|TestManage' -v` — if a golden embeds the rendered context and diffs, regenerate: `go test ./internal/cli -update`, re-run, inspect the diff is only the Mapping role text.

- [ ] **Step 5: Commit**

```bash
git add internal/manager/context_v1.md internal/manager/context_test.go internal/cli/testdata/golden/
git commit -m "feat(ATM-b678b0): manager prompt points at contextmap guide; mapping procedure de-hardcoded"
```

---

### Task 6: Full verification + ledger

**Files:**
- None new; ledger comment via `atm` CLI.

- [ ] **Step 1: Full suite**

Run: `go test ./...` (from the worktree root; expect ~15 min — cli/store/tui are slow).
Expected: every package `ok`.

- [ ] **Step 2: End-to-end sanity**

Run in a throwaway store:

```bash
export TMPSTORE=$(mktemp -d)
go run ./cmd/atm --store "$TMPSTORE" project create --code ZZ --name demo --actor admin@cli:unset
go run ./cmd/atm --store "$TMPSTORE" conventions | grep -A6 '## Capabilities'
go run ./cmd/atm --store "$TMPSTORE" workflow guide | head -2
go run ./cmd/atm --store "$TMPSTORE" context guide | grep 'Manager duty'
```

Expected: enumeration lists workflow + contextmap with consult lines; guides print.

- [ ] **Step 3: Record progress in the ledger**

```bash
atm task comment add --task ATM-b678b0 --actor developer@claude:<your-model> \
  --label ATM:comment:progress \
  --body "Phase 1 (describe) implemented: Capability.Summary/Guide, uniform guide subcommand, registry Describe; conventions composed as core+enumeration; manager prompt Mapping role points at atm context guide. Full suite green."
```

- [ ] **Step 4: Commit any stragglers**

```bash
git status --short   # expect clean; commit leftovers if any
```
