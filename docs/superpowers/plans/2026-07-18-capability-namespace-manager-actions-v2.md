# Capability Namespace + Manager Actions v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move all capability commands under a discoverable `atm capability` namespace, shrink `atm conventions` to a minimal substrate primer, delete the parallel `seed.go` seeding path, and replace the capability-coupled manager action model with three semantic-agnostic actions (`brief`/`autopilot`/`ask`).

**Architecture:** The `Capability` interface shrinks to five methods (`Name`, `Summary`, `Guide`, `Command`, `EnsureVocabulary` returning the boards it owns); the registry mounts each capability's tree under `atm capability <Name()>` and enforces mount-by-name structurally. Seeding becomes capability-owned only (no `seed.go`, no `atm label seed`, no store-side birth seeding). The manager prompt stops composing capability role lists; it points at capability guides' `## Brief`/`## Autopilot` sections, scoped by `--capability`.

**Tech Stack:** Go, cobra, bubbletea (TUI), golden-file tests (`go test ./internal/cli -update`), two modules (root + `libs/eventsource`).

**Spec:** `docs/superpowers/specs/2026-07-18-capability-namespace-manager-actions-v2-design.md` — including its "Clarifications (2026-07-18 follow-up review)" section, which amends §2/§4/§7 (mount by `Name()`; `atm label seed` removed entirely; capabilities absorb `status:*`/`context:*` substrate labels).

## Global Constraints

- **Flag day (loud, deliberate, pre-1.0):** `atm workflow <verb>` → `atm capability workflow <verb>`, `atm context <verb>` → `atm capability contextmap <verb>`. No compat shims, no aliases. `atm manage` loses `--curate`/`--recall`/`--mapping`/`--onboarding` with no deprecated aliases (spec explicitly overrides the ATM-0113 never-hard-break convention here).
- Mount-by-name: the subcommand under `atm capability` is always the capability's `Name()` (`workflow`, `contextmap`).
- `EnsureVocabulary` returns **boards only** (`[]core.Label` where `Expr != ""`), is idempotent/additive, never overwrites a curated description (`LabelSeed` contract).
- Manager actions are exactly `brief`, `autopilot`, `ask`; default `autopilot`. `--capability` is single-valued; empty = all enabled.
- A fresh project seeds only what its enabled capabilities own. `comment:*` and `priority:*` are never seeded.
- Verification gate for every task: `go test ./...` (root module) green; final task also runs `go test ./...` in `libs/eventsource`.
- Golden regeneration: `go test ./internal/cli -update` (inspect the diff before committing — goldens are assertions, not noise).
- Commit style: `type(ATM-b678b0): summary` + the repo's standard trailer. Keep the ATM ledger current (comment on ATM-b678b0 at milestones).

## File Structure (what changes where)

| Area | Files |
|---|---|
| Capability seam | `internal/capability/capability.go` (interface, registry; drop `DefaultBoard`/`ManagerActions`/`ActionSpec`/`ManagerAction`; `Describe()` loses `Env` + `Command`) |
| Capabilities | `internal/capability/workflow/{vocabulary.go,command.go,guide.go,guide.md}`, `internal/capability/contextmap/{vocabulary.go,command.go,guide.go,guide.md,recorder.go}` |
| CLI | `internal/cli/capability.go` (NEW: namespace + list), `root.go`, `conventions.go`, `label.go`, `project.go`, `manager.go`, `tmux.go`, `init.go`, `task.go`, `comment.go`, `persona.go`, `activity.go`, `store.go`, `search.go` |
| Manager | `internal/manager/{context.go,context_v1.md,launcher.go}` |
| Seeding | DELETE `internal/seed/seed.go` + `seed_test.go` (keep `persona.go`); `internal/store/{label.go,project.go}`; `internal/core/service.go` |
| TUI | `internal/tui/{labels.go,help.go}` (`selectDefault`, `seedDefaults`, `conventionsTextTUI`) |
| Templates | `internal/developing/context_v1.md` |
| Docs | `docs/architecture/label-substrate-and-capabilities.md` |

---

### Task 1: Capabilities absorb their substrate labels

Workflow seeds `status:*` + the four status values; contextmap seeds the `context:*` descriptor and upgrades its thin per-kind descriptions to `seed.go`'s richer ones. Signatures unchanged in this task. (Command-path strings inside label descriptions also update here so these strings are touched once: `atm context supersede` → `atm capability contextmap supersede`.)

**Files:**
- Modify: `internal/capability/workflow/vocabulary.go`
- Modify: `internal/capability/contextmap/vocabulary.go`
- Test: `internal/capability/workflow/vocabulary_test.go`, `internal/capability/contextmap/vocabulary_test.go`

**Interfaces:**
- Consumes: `core.LabelService.LabelSeed(name, description, expr, actor string) error` (upserts only when absent).
- Produces: `workflow.EnsureVocabulary` / `contextmap.EnsureVocabulary` seed the full sets below (still returning `error` — the return type changes in Task 2).

- [ ] **Step 1: Write failing tests** — extend the existing vocabulary tests (they use a fake/in-memory `LabelService`; follow the established pattern in each `vocabulary_test.go`). New assertions:

```go
// workflow/vocabulary_test.go — add to the existing seeded-set test (or add):
func TestEnsureVocabularySeedsStatusLabels(t *testing.T) {
	svc := newFakeLabelService() // whatever fake the file already uses
	if err := EnsureVocabulary(svc, "ATM", "tester@claude:test"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ATM:status:*", "ATM:status:open", "ATM:status:in-progress",
		"ATM:status:blocked", "ATM:status:done",
	} {
		l, ok := svc.seeded[want]
		if !ok {
			t.Fatalf("EnsureVocabulary did not seed %s", want)
		}
		if l.desc == "" {
			t.Errorf("%s seeded without a description", want)
		}
		if l.expr != "" {
			t.Errorf("%s is a stored/namespace label, seeded with expr %q", want, l.expr)
		}
	}
}
```

```go
// contextmap/vocabulary_test.go:
func TestEnsureVocabularySeedsContextNamespaceDescriptor(t *testing.T) {
	svc := newFakeLabelService()
	if err := EnsureVocabulary(svc, "ATM", "tester@claude:test"); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.seeded["ATM:context:*"]; !ok {
		t.Fatal("EnsureVocabulary did not seed the context:* namespace descriptor")
	}
	// Rich kind descriptions (absorbed from seed.go), not the thin "context pointer kind: X".
	if d := svc.seeded["ATM:context:agent"].desc; !strings.Contains(d, "agent-direction") {
		t.Errorf("context:agent description not absorbed from seed.go: %q", d)
	}
	// Command paths in descriptions point at the capability namespace.
	if d := svc.seeded["ATM:knowledge:superseded"].desc; !strings.Contains(d, "atm capability contextmap supersede") {
		t.Errorf("superseded description still references the old command path: %q", d)
	}
}
```

Adapt fake-field names (`seeded`, `desc`, `expr`) to what the existing fakes actually expose — read the test file first; do not invent a second fake.

- [ ] **Step 2: Run to verify failure** — `go test ./internal/capability/... -run TestEnsureVocabulary -v` → FAIL (labels missing / descriptions thin).

- [ ] **Step 3: Implement.** `workflow/vocabulary.go` — extend `EnsureVocabulary`:

```go
// EnsureVocabulary seeds this capability's full vocabulary: the status:*
// namespace it owns (absorbed from the deleted internal/seed default set)
// and the four workflow boards. Idempotent: LabelSeed upserts only when the
// label is absent, so a human's curated description is never overwritten.
func EnsureVocabulary(s core.LabelService, code, actor string) error {
	stored := []struct{ suffix, desc string }{
		{"status:*", "lifecycle state of a task; exactly one status label should be present"},
		{"status:open", "workflow state: open; task is not started or is being considered"},
		{"status:in-progress", "workflow state: in-progress; someone is actively working on this"},
		{"status:blocked", "workflow state: blocked; task cannot proceed pending something else"},
		{"status:done", "workflow state: done; task is complete"},
	}
	for _, l := range stored {
		if err := s.LabelSeed(code+":"+l.suffix, l.desc, "", actor); err != nil {
			return err
		}
	}
	boards := []struct{ name, desc, expr string }{
		{BoardBacklog(code), "tasks with no status label: incoming jottings awaiting triage. Review queue alongside open-tasks.", backlogExpr()},
		{BoardOpenTasks(code), "every open task: the project's active work.", openTasksExpr()},
		{BoardInProgressTasks(code), "tasks someone is actively working on (status:in-progress).", inProgressTasksExpr()},
		{BoardAllTasks(code), "every task in the project, ordered by recent activity. Default board in the TUI.", allTasksExpr()},
	}
	for _, b := range boards {
		if err := s.LabelSeed(b.name, b.desc, b.expr, actor); err != nil {
			return err
		}
	}
	return nil
}
```

`contextmap/vocabulary.go` — replace the `want` slice:

```go
	want := []lbl{
		{code + ":context:*", "index tasks whose description is the payload: agent directions, repos, docs, questions", ""},
		{code + ":knowledge:*", "lifecycle of a piece of recorded knowledge; absence means current", ""},
		{LabelSuperseded(code), "this context pointer is obsolete; its successor is named in the description. Kept for history -- it retains its kind, narrative, and provenance stamps. Applied by `atm capability contextmap supersede`.", ""},
		{LabelProvenance(code), "task comment kind: a machine-written provenance stamp recording what a context pointer was derived from, and the evidence, at a moment in time. Written and read only by `atm capability contextmap` -- do not hand-edit.", ""},
		{BoardCurrent(code), "every context pointer that has not been superseded: the project's current knowledge. Agents read this board rather than the raw context:* namespace, so a query always returns the latest.", currentExpr()},
	}
	kindDesc := map[string]string{
		"agent":         "the task's description captures agent-direction notes for this project: build/test/lint commands, conventions, and gotchas a working agent must know",
		"repository":    "the task's description names a code repository (path or URL), what it contains, and how to work in it; a later agent reads this to orient",
		"documentation": "the task's description points at a specific document (file path or URL) and summarizes what it covers, so a later agent can decide whether to read it",
		"question":      "the task's description poses an open question or ambiguity about the project that a human or later agent should clarify; not a defect, not a work item, a gap in understanding",
	}
	for _, kind := range ContextKinds {
		want = append(want, lbl{LabelContextKind(code, kind), kindDesc[kind], ""})
	}
```

- [ ] **Step 4: Run** — `go test ./internal/capability/...` → PASS. Then `go test ./...`; existing tests asserting the thin kind descriptions or old command paths (grep `"context pointer kind"` and `` `atm context ` `` under `internal/`) update to the new strings.

- [ ] **Step 5: Commit** — `feat(ATM-b678b0): workflow/contextmap absorb their substrate labels into EnsureVocabulary`

---

### Task 2: `EnsureVocabulary` returns boards; `DefaultBoard` removed

Interface change: `EnsureVocabulary(svc, code, actor) ([]core.Label, error)` returning boards only; `DefaultBoard` deleted from interface + registry; TUI picks `<CODE>:all-tasks`-else-first. Every call site updates mechanically.

**Files:**
- Modify: `internal/capability/capability.go`
- Modify: `internal/capability/workflow/{vocabulary.go,command.go}` (also delete `DefaultBoard` method)
- Modify: `internal/capability/contextmap/{vocabulary.go,command.go,recorder.go}` (also delete `DefaultBoard` method)
- Modify: `internal/cli/{project.go,label.go}`, `internal/tui/{app.go,projects.go,labels.go}`
- Test: `internal/capability/*/vocabulary_test.go`, `internal/tui/labels_test.go`, plus any `env_test.go`/registry fakes implementing the interface

**Interfaces:**
- Produces: `Capability.EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error)`; `Registry.EnsureVocabulary(...)` same signature, aggregating boards across enabled capabilities in registration order. `Registry.DefaultBoard` GONE. Workflow returns its 4 boards; contextmap returns `[context-current]`.

- [ ] **Step 1: Write failing tests**

```go
// workflow/vocabulary_test.go:
func TestEnsureVocabularyReturnsBoards(t *testing.T) {
	svc := newFakeLabelService()
	boards, err := EnsureVocabulary(svc, "ATM", "tester@claude:test")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range boards {
		if b.Expr == "" {
			t.Errorf("returned non-board label %s", b.Name)
		}
		names = append(names, b.Name)
	}
	want := []string{"ATM:backlog", "ATM:open-tasks", "ATM:in-progress-tasks", "ATM:all-tasks"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("boards = %v, want %v", names, want)
	}
}
```

Contextmap analogue asserts exactly `[]string{"ATM:context-current"}`. In `internal/tui/labels_test.go`, find the existing `selectDefault` coverage and re-anchor it: default is `<CODE>:all-tasks` when present; when the ring lacks it (e.g. workflow disabled), the FIRST row is selected — no registry consultation.

- [ ] **Step 2: Run to verify failure** — signature mismatch = compile failure across packages is the expected "failing" state here; note it and move on.

- [ ] **Step 3: Implement.** `capability.go`:

```go
	// EnsureVocabulary seeds ALL the capability's labels (stored, namespace,
	// boards) for a project, idempotently, and returns the BOARD labels
	// (Expr != "") the capability owns. One call leaves the project fully
	// seeded for this capability.
	EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error)
```

Registry (replace both old methods; delete `DefaultBoard`):

```go
// EnsureVocabulary seeds every capability's vocabulary for the project,
// stopping at the first error, and returns the union of the boards the
// capabilities own, in registration order.
func (r *Registry) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error) {
	if r == nil {
		return nil, nil
	}
	var boards []core.Label
	for _, c := range r.caps {
		bs, err := c.EnsureVocabulary(svc, code, actor)
		if err != nil {
			return nil, err
		}
		boards = append(boards, bs...)
	}
	return boards, nil
}
```

Package funcs return the boards they seeded (workflow builds `out := make([]core.Label, 0, len(boards))` appending `core.Label{Name: b.name, Description: b.desc, Expr: b.expr}` inside the board loop; contextmap returns `[]core.Label{{Name: BoardCurrent(code), Description: <the board's desc string>, Expr: currentExpr()}}`). Update the `Cap` wrapper methods in both `command.go` files and DELETE both `DefaultBoard` methods.

Call-site updates (mechanical — result ignored where nothing consumes it yet):
- `contextmap/recorder.go` (3 sites): `if _, err := EnsureVocabulary(rec.Store, code, rec.Actor); err != nil {`
- `cli/project.go:62` and `:177`: `if _, err := ...EnsureVocabulary(...); err != nil {`
- `cli/label.go` seed cmd (still exists until Task 6): `if _, err := st.registry.EnsureVocabulary(s, project, actor); err != nil {`
- `tui/app.go:186`, `tui/projects.go:246`: `if _, err := ...; err != nil {`
- `workflow/command.go` `newSeedCmd`: consume the return — kills the hand-listed board names (and fixes the stale list that omitted `all-tasks`):

```go
			boards, err := EnsureVocabulary(svc, project, actor)
			if err != nil {
				return err
			}
			names := make([]string, 0, len(boards))
			for _, b := range boards {
				names = append(names, b.Name)
			}
			return env.Emit(map[string]any{"project": project, "boards": names}, func() {
				fmt.Fprintf(env.Stdout(), "ensured workflow boards for %s\n", project)
			})
```

- `tui/labels.go` `selectDefault` — replace the registry consult:

```go
	// UI policy, not a capability concern: all-tasks if the ring has it
	// (workflow enabled), else the first board any capability seeded.
	want := b.m.projectScope + ":all-tasks"
```

- [ ] **Step 4: Run** — `go test ./...` → PASS (update any interface fakes in `internal/cli/env_test.go` / capability test doubles the compiler flags; a `workflow seed` golden exists — regenerate with `go test ./internal/cli -update` and verify the diff now includes `all-tasks`).

- [ ] **Step 5: Commit** — `feat(ATM-b678b0): EnsureVocabulary returns owned boards; DefaultBoard removed, UI picks all-tasks-else-first`

---

### Task 3: `atm capability` namespace + `list`

New single mount point. `atm capability list` enumerates the FULL registry with per-project enabled flags; each ENABLED capability's tree mounts under `atm capability <Name()>` (hard gate preserved — disabled = unmounted). Root-level capability mounts disappear: the flag day.

**Files:**
- Create: `internal/cli/capability.go`
- Modify: `internal/cli/root.go`, `internal/capability/capability.go` (Describe/Commands), `internal/capability/contextmap/command.go` (`Use: "contextmap"`), `internal/cli/conventions.go` (interim: `capabilitiesSection` uses `d.Name`)
- Test: `internal/cli/capability_test.go` (NEW), update `internal/cli/{mount_test.go,workflow_test.go,context_test.go}`, `internal/capability/capability_test.go` if present
- Goldens: new `capability-list*.json`; existing goldens that captured root `atm workflow`/`atm context` output re-record under the new path

**Interfaces:**
- Consumes: `cliState.registry` (mount-narrowed), `cliState.fullRegistry` (complete), `Registry.Names()`, `Registry.Commands(env)`.
- Produces: `Registry.Describe() []Description` with `Description{Name, Summary string}` (no `Env` param, no `Command` field); `newCapabilityCmd(st *cliState) *cobra.Command`. JSON: `{"capabilities":[{"name":"workflow","summary":"…","enabled":true},…]}`.

- [ ] **Step 1: Write failing tests** (`internal/cli/capability_test.go`, golden-harness style — mirror `project_capability_test.go` patterns):

```go
func TestCapabilityListShowsDisabled(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1() // project ATM exists, all capabilities enabled
	h.run("project", "capability", "remove", "--project", "ATM", "--name", "contextmap", "--actor", "tester@claude:test")
	h.reset()
	stdout, _, code := h.run("capability", "list", "--project", "ATM")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	h.golden("capability-list-contextmap-disabled", stdout) // {"capabilities":[{workflow,true},{contextmap,false}]}
}

func TestCapabilityMountHardGate(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	h.run("project", "capability", "remove", "--project", "ATM", "--name", "contextmap", "--actor", "tester@claude:test")
	h.reset()
	_, _, code := h.run("capability", "contextmap", "check", "--project", "ATM")
	if code == 0 {
		t.Fatal("disabled capability's subtree still mounted under atm capability")
	}
	h.reset()
	if _, _, code := h.run("capability", "workflow", "status", "--project", "ATM"); code != 0 {
		t.Fatalf("enabled capability not mounted: exit %d", code)
	}
}

func TestCapabilityGuideMountedByName(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	stdout, _, code := h.run("capability", "contextmap", "guide")
	if code != 0 || !strings.Contains(stdout, "Context map capability") {
		t.Fatalf("atm capability contextmap guide: exit %d, out %q", code, stdout)
	}
}
```

(Adapt harness helper names to `harness_test.go`'s actual API — `run`, `reset`, golden compare — read it first.)

- [ ] **Step 2: Run to verify failure** — `go test ./internal/cli -run TestCapability -v` → FAIL ("unknown command \"capability\"").

- [ ] **Step 3: Implement.**

`internal/capability/capability.go` — simplify `Describe`, enforce mount-by-name in `Commands`:

```go
// Description is one capability's enumeration entry. The capability's Name
// IS its mounted command under `atm capability` — there is no separate
// command identity (Clarification 1 of the v2 spec).
type Description struct {
	Name    string
	Summary string
}

// Describe enumerates the registered capabilities in registration order.
func (r *Registry) Describe() []Description {
	if r == nil {
		return nil
	}
	out := make([]Description, 0, len(r.caps))
	for _, c := range r.caps {
		out = append(out, Description{Name: c.Name(), Summary: c.Summary()})
	}
	return out
}
```

In `Commands(env)`, after `cmd := c.Command(env)`, add:

```go
		// Mount-by-name is a structural invariant: whatever Use the
		// capability chose, the mounted command answers to Name().
		cmd.Use = c.Name()
```

`internal/capability/contextmap/command.go`: `Use: "context"` → `Use: "contextmap"` (keep Short/Long).

`internal/cli/capability.go` (new):

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newCapabilityCmd is the single mount point for capability commands and the
// discovery surface. Enabled capabilities' trees mount under
// `atm capability <name>`; disabled ones are unmounted (the hard gate) but
// still enumerated by `atm capability list`.
func newCapabilityCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capability",
		Short: "Discover and use the project's capabilities",
		Long: "Semantics beyond the substrate live in capabilities. Each owns a slice of " +
			"the label substrate, contributes verbs, and explains itself.\n\n" +
			"`atm capability list` enumerates registered capabilities (enabled + disabled " +
			"for the target project). `atm capability <name> -h` shows a capability's verbs; " +
			"`atm capability <name> guide` prints its full agent-facing semantics. Commands " +
			"for capabilities the project did not enable are not mounted.",
	}
	cmd.AddCommand(newCapabilityListCmd(st))
	for _, c := range st.registry.Commands(st) {
		cmd.AddCommand(c)
	}
	return cmd
}

func newCapabilityListCmd(st *cliState) *cobra.Command {
	var all bool
	var project string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Enumerate registered capabilities (enabled + disabled for the target project)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// st.registry was narrowed by the pre-parse mount when a project
			// was targeted (--project / --task / ATM_PROJECT); with no target
			// it IS the full registry, so everything reads enabled — matching
			// what is actually mounted.
			enabled := make(map[string]bool)
			names := st.registry.Names()
			if all {
				names = st.fullRegistry.Names()
			}
			for _, n := range names {
				enabled[n] = true
			}
			type row struct {
				Name    string `json:"name"`
				Summary string `json:"summary"`
				Enabled bool   `json:"enabled"`
			}
			descs := st.fullRegistry.Describe()
			rows := make([]row, 0, len(descs))
			for _, d := range descs {
				rows = append(rows, row{Name: d.Name, Summary: d.Summary, Enabled: enabled[d.Name]})
			}
			return st.emit(st.stdout(), map[string]any{"capabilities": rows}, func() {
				for _, r := range rows {
					fmt.Fprintf(st.stdout(), "%s\t%s\t%t\n", r.Name, r.Summary, r.Enabled)
				}
			})
		},
	}
	// Consumed by the pre-parse mount (mountRegistry); declared so cobra
	// accepts it and help documents it.
	cmd.Flags().StringVar(&project, "project", "", "project code; enabled column reflects this project's enabled set")
	cmd.Flags().BoolVar(&all, "all", false, "ignore the project and list every registered capability as enabled")
	return cmd
}
```

`internal/cli/root.go` — replace the root-level registry mount loop:

```go
	root.AddCommand(newCapabilityCmd(st))
```

(delete the `for _, c := range st.registry.Commands(st) { root.AddCommand(c) }` loop).

`internal/cli/conventions.go` — interim compile fix only (full rewrite is Task 5): `capabilitiesSection`/`conventionsStructured` take `[]capability.Description` without `Command`; consult pointer becomes `atm capability <Name> guide`:

```go
		fmt.Fprintf(&b, "- **%s** (`atm capability %s`) — %s Consult: `atm capability %s guide`.\n", d.Name, d.Name, d.Summary, d.Name)
```

and `newConventionsCmd` calls `st.registry.Describe()`.

- [ ] **Step 4: Update flag-day test paths.** In `internal/cli/workflow_test.go`, `context_test.go`, `mount_test.go`: every `h.run("workflow", …)` → `h.run("capability", "workflow", …)`; every `h.run("context", …)` → `h.run("capability", "contextmap", …)`. `mount_test.go` assertions about which commands exist at the ROOT for disabled capabilities now assert about the `capability` subtree instead. Regenerate goldens: `go test ./internal/cli -update`; inspect diff (workflow/context envelopes should be identical content under new invocation; conventions text gains `atm capability` consult lines).

- [ ] **Step 5: Run full suite** — `go test ./...` → PASS.

- [ ] **Step 6: Commit** — `feat(ATM-b678b0)!: capability commands mount under atm capability <name>; atm capability list enumerates (FLAG DAY: atm workflow/context are gone)`

---

### Task 4: Capability guides v2 (`## Brief` / `## Autopilot`)

Replace both `guide.md` files with the spec §7 drafts, with every `atm capability context …` read as `atm capability contextmap …` (Clarification 1).

**Files:**
- Modify: `internal/capability/workflow/guide.md` — exact content: spec §7 "internal/capability/workflow/guide.md" code block, verbatim.
- Modify: `internal/capability/contextmap/guide.md` — exact content: spec §7 "internal/capability/contextmap/guide.md" code block, with `s/atm capability context /atm capability contextmap /g`.
- Test: `internal/capability/workflow/guide_test.go`, `internal/capability/contextmap/guide_test.go`

**Interfaces:**
- Produces: both `Guide()` strings contain `## Brief` and `## Autopilot` sections (the manager prompt in Task 7 relies on these section names).

- [ ] **Step 1: Write failing tests** — in each `guide_test.go` add:

```go
func TestGuideHasBriefAndAutopilotSections(t *testing.T) {
	g := Cap{}.Guide()
	for _, section := range []string{"\n## Brief\n", "\n## Autopilot\n"} {
		if !strings.Contains(g, section) {
			t.Errorf("guide missing %q section", strings.TrimSpace(section))
		}
	}
	if strings.Contains(g, "Manager duty") {
		t.Error("guide still has the old Manager duty section")
	}
	if strings.Contains(g, "`atm workflow") || strings.Contains(g, "`atm context ") {
		t.Error("guide references pre-namespace command paths")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/capability/... -run TestGuideHas -v` → FAIL.
- [ ] **Step 3: Write both guide.md files** per the spec blocks (workflow verbatim; contextmap with the contextmap substitution). Fix any existing guide tests asserting old content.
- [ ] **Step 4: Run** — `go test ./internal/capability/...` then `go test ./internal/cli` (guide goldens if any) → PASS; regenerate with `-update` if a golden captures guide output.
- [ ] **Step 5: Commit** — `feat(ATM-b678b0): capability guides gain Brief/Autopilot sections under the capability namespace`

---

### Task 5: Conventions → minimal substrate primer

Rewrite `conventionsCoreText` to the spec §1 primer, rewrite the JSON envelope, drop the composed capabilities section and the `--project` flag. Update the two other prose surfaces that restate the old conventions: TUI help and the developing-session template.

**Files:**
- Modify: `internal/cli/conventions.go` (full rewrite of text + structured; drop `capabilitiesSection`, drop `seed` import, drop `--project` flag)
- Modify: `internal/tui/help.go` (`conventionsTextTUI`)
- Modify: `internal/developing/context_v1.md` (line 12)
- Test: `internal/cli/conventions_test.go`; goldens `conventions-text.json`, `conventions-json.json`, `determinism-conventions.json`

**Interfaces:**
- Produces: `conventionsStructured() map[string]any` (no args) with exactly the keys `what_atm_is`, `substrate`, `capabilities`, `actor_identity`. `newConventionsCmd` no longer consults the registry.

- [ ] **Step 1: Write failing test**

```go
func TestConventionsIsMinimalPrimer(t *testing.T) {
	h := newGoldenHarness(t)
	stdout, _, code := h.run("conventions")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"atm capability list", "atm capability <name> guide", "## Substrate", "## Actor identity"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("primer missing %q", want)
		}
	}
	for _, gone := range []string{"first-contact", "code-of-conduct", "First-time human sequence", "atm label seed"} {
		if strings.Contains(stdout, gone) {
			t.Errorf("primer still contains removed prose %q", gone)
		}
	}
}
```

And a JSON-envelope test asserting the four keys are present and `seeded_labels`/`code_of_conduct`/`capabilities`-as-list are absent (unmarshal `stdout` and check `conventions` map keys).

- [ ] **Step 2: Run to verify failure** — `go test ./internal/cli -run TestConventions -v` → FAIL.

- [ ] **Step 3: Implement.** `conventionsCoreText` becomes exactly the spec §1 block (Go raw string with backtick-escape composition as the current file does). `conventionsStructured`:

```go
func conventionsStructured() map[string]any {
	return map[string]any{
		"what_atm_is": "ATM (Agent Tasks Management) is a label-substrate task store. A project holds tasks; each task has free-form text (title, description) and a set of labels. No status field, no claims, no review queue, no state machine — status, type, priority, ownership, relationships are all labels, interpreted by the agent reading them. The store keeps the substrate legible; capabilities own the semantics.",
		"substrate": []map[string]string{
			{"namespace": "atm task", "summary": "tasks (ID, title, description, labels)"},
			{"namespace": "atm task comment", "summary": "per-task append-mostly thread, classified by a label"},
			{"namespace": "atm label", "summary": "labels (<CODE>:<ns>:<value> or <CODE>:<tag>); a label's description records its intention; three kinds: stored (asserted), namespace (prefix, emergent), board (computed from an expression)"},
			{"namespace": "atm project", "summary": "project lifecycle"},
			{"namespace": "atm persona", "summary": "actor identity"},
			{"namespace": "atm activity", "summary": "audit log"},
			{"namespace": "atm store", "summary": "store administration"},
			{"namespace": "atm search", "summary": "semantic search"},
		},
		"capabilities":   "Semantics beyond the substrate live in capabilities; a project enables a per-project subset, and commands for disabled capabilities are not mounted. Enumerate with `atm capability list`; discover one with `atm capability <name> -h` and `atm capability <name> guide`.",
		"actor_identity": "Every mutation stamps persona@agent:model (e.g. developer@claude:opus-4.8). See `atm persona -h`; built-ins developer, manager, admin. See `atm dev -h`.",
	}
}
```

`newConventionsCmd` RunE: JSON → `writeJSON(st.stdout(), map[string]any{"conventions": conventionsStructured()})`; text → `fmt.Fprint(st.stdout(), conventionsCoreText)` (the primer's last line already says "Conventions are advisory only." — drop the extra trailing advisory Fprint). Delete `capabilitiesSection`, the `--project` flag, and the `seed` + `capability` imports.

`internal/tui/help.go` — replace `conventionsTextTUI`'s content with the same primer sections (What ATM is / Substrate / Capabilities / Actor identity), keeping any TUI-specific key hints the surrounding render adds; delete the first-contact, code-of-conduct, first-time-human, and `atm label seed` blocks.

`internal/developing/context_v1.md` line 12 →

```
- `<ATM_BIN> conventions` — what ATM is, the substrate namespaces, and how to discover capabilities. Start here, then run `<ATM_BIN> capability list --project <CODE>` and each enabled capability's `guide`.
```

- [ ] **Step 4: Regenerate + run** — `go test ./internal/cli -update`, inspect conventions golden diffs (should shrink dramatically), then `go test ./...` → PASS (fix `internal/tui/help_test`-adjacent assertions and `internal/cli/developing` goldens if the template is embedded in one).
- [ ] **Step 5: Commit** — `feat(ATM-b678b0): conventions is a minimal substrate primer pointing at atm capability`

---

### Task 6: Delete `seed.go` and `atm label seed`

The only remaining consumers are the store birth path, `core.Service`, the `label seed` command, and the TUI [S] key. Capability `EnsureVocabulary` (already the richer set after Task 1) is now the only seeding path.

**Files:**
- Delete: `internal/seed/seed.go`, `internal/seed/seed_test.go` (all label tests; `persona.go` STAYS)
- Modify: `internal/core/service.go` (drop `SeedLabels` from `LabelService`)
- Modify: `internal/store/label.go` (drop `SeedLabels` method), `internal/store/project.go` (drop the `seed.Labels` loop in `createProjectV2` + the `seed` import; keep the `cs.CreateProject` root event)
- Modify: `internal/cli/label.go` (drop `newLabelSeedCmd` + its registration + `seed`/`sort` imports)
- Modify: `internal/tui/labels.go` (`seedDefaults` re-runs the registry ensure)
- Test: `internal/store/project_test.go` (drops `seed.Labels` references), `internal/cli/label_test.go`, `internal/tui/labels_test.go`; delete golden `label-seed.json`; regenerate determinism goldens

**Interfaces:**
- Produces: `core.LabelService` WITHOUT `SeedLabels`. A fresh project's labels = union of its enabled capabilities' `EnsureVocabulary` output; nothing else.

- [ ] **Step 1: Write failing test**

```go
// internal/cli/label_test.go (or project_test.go):
func TestFreshProjectSeedsOnlyCapabilityLabels(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "PRJ", "--name", "P", "--actor", "tester@claude:test")
	h.reset()
	stdout, _, _ := h.run("label", "list", "--project", "PRJ")
	for _, gone := range []string{"PRJ:comment:", "PRJ:priority:"} {
		if strings.Contains(stdout, gone) {
			t.Errorf("fresh project still seeds %s*", gone)
		}
	}
	for _, want := range []string{"PRJ:status:open", "PRJ:all-tasks", "PRJ:context:*", "PRJ:context-current"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("fresh project missing capability-owned %s", want)
		}
	}
}

func TestLabelSeedCommandGone(t *testing.T) {
	h := newGoldenHarness(t)
	if _, _, code := h.run("label", "seed", "--project", "ATM"); code == 0 {
		t.Fatal("atm label seed still exists")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/cli -run 'TestFreshProject|TestLabelSeed' -v` → FAIL (comment/priority still seeded; seed cmd exists).

- [ ] **Step 3: Implement the deletions** listed under Files. In `store/project.go` `createProjectV2`, the birth closure keeps `cs.CreateProject(name, actor)` + `reprojectTxn` + `getProjectLocked`; the label loop and its comment go. In `tui/labels.go`:

```go
func (b *boardsModel) seedDefaults() tea.Cmd {
	boards, err := b.m.regFor(b.m.projectScope).EnsureVocabulary(b.m.store, b.m.projectScope, b.m.actor)
	if err != nil {
		b.m.showToast("error: " + err.Error())
		return nil
	}
	b.m.showToast(fmt.Sprintf("ensured capability vocabulary in %s (%d boards)", b.m.projectScope, len(boards)))
	b.m.refreshAll()
	return nil
}
```

Also grep `internal/tui/help.go` + `keymap.go` for the [S] key description ("seed default labels" or similar) and reword to "re-ensure capability vocabulary". Remove the now-unused `seed` import from `tui/labels.go`.

- [ ] **Step 4: Run + regenerate** — `go test ./... ` → fix compile stragglers the grep finds (`grep -rn "SeedLabels\|seed\.Labels" internal/ cmd/` must return nothing outside `persona`); `go test ./internal/cli -update` for determinism/label goldens; `rm internal/cli/testdata/golden/label-seed.json`. Inspect determinism diffs: label lists lose `comment:*`/`priority:*`, keep status/context/boards.
- [ ] **Step 5: Commit** — `feat(ATM-b678b0)!: delete seed.go and atm label seed; capabilities are the only seeding path`

---

### Task 7: Manager action model — `brief` / `autopilot` / `ask`

Reshape `ContextData` + prompt, replace the flag surface, single argv flavor at the call site. (The `Launcher` interface itself and `ManagerActions` on the capability seam are deleted in Task 8 — this task just stops calling them.)

**Files:**
- Modify: `internal/manager/context.go`, `internal/manager/context_v1.md`
- Modify: `internal/cli/manager.go`, `internal/cli/tmux.go` (drop `tmuxLabelOnboarding`), `internal/cli/init.go:101`
- Test: `internal/manager/context_test.go`, `internal/cli/manager_test.go`; goldens `manage-codex-curate-launch.json` → replaced by autopilot/brief equivalents, `manage-codex-action-mapping-launch.json` → deleted

**Interfaces:**
- Produces: `manager.ContextData{Code, Name, ATMBin, Actor, RunID, Timestamp, Persona, PersonaPrompt, PersonaDescription, Action, Capability string}` (`CapabilityAction` type and `CapabilityActions`/`ActionConsult` fields DELETED). `validateManagerAction(action, capabilityName string, enabled, registered []string) error`. Env: `ATM_MANAGER_ACTION` ∈ {brief,autopilot,ask}; `ATM_MANAGER_CAPABILITY` always set (empty = all); `ATM_ONBOARD` gone.

- [ ] **Step 1: Write failing tests**

```go
// internal/manager/context_test.go:
func TestRenderContextActionBlocks(t *testing.T) {
	base := ContextData{Code: "ATM", Name: "Agent Tasks Management", ATMBin: "/usr/local/bin/atm", Actor: "manager@claude:test"}

	brief := base
	brief.Action = "brief"
	out := RenderContext(brief)
	if !strings.Contains(out, "Focus this session on **brief**") ||
		!strings.Contains(out, `"Brief" section`) ||
		!strings.Contains(out, "/usr/local/bin/atm capability list --project ATM") {
		t.Errorf("brief block wrong:\n%s", out)
	}

	scoped := base
	scoped.Action = "autopilot"
	scoped.Capability = "contextmap"
	out = RenderContext(scoped)
	if !strings.Contains(out, "the `contextmap` capability") || !strings.Contains(out, `"Autopilot" section`) {
		t.Errorf("scoped autopilot block wrong:\n%s", out)
	}

	if out := RenderContext(base); strings.Contains(out, "<CAPABILITY_ROLES>") || strings.Contains(out, "## Current manager action") {
		t.Errorf("action-less render leaked placeholders:\n%s", out)
	}
	if strings.Contains(RenderContext(base), "**Curate**") {
		t.Error("prompt still hardcodes the Curate role bullet")
	}
}
```

```go
// internal/cli/manager_test.go:
func TestValidateManagerAction(t *testing.T) {
	enabled := []string{"workflow"}
	registered := []string{"workflow", "contextmap"}
	if err := validateManagerAction("autopilot", "", enabled, registered); err != nil {
		t.Errorf("default action rejected: %v", err)
	}
	if err := validateManagerAction("curate", "", enabled, registered); err == nil {
		t.Error("curate accepted; want unknown-action error")
	}
	if err := validateManagerAction("brief", "nope", enabled, registered); err == nil || !strings.Contains(err.Error(), "registered: workflow, contextmap") {
		t.Errorf("unknown capability error wrong: %v", err)
	}
	if err := validateManagerAction("ask", "contextmap", enabled, registered); err == nil || !strings.Contains(err.Error(), "not enabled for project") {
		t.Errorf("not-enabled error wrong: %v", err)
	}
	if err := validateManagerAction("ask", "workflow", enabled, registered); err != nil {
		t.Errorf("enabled capability rejected: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure** — both test files FAIL/compile-fail.

- [ ] **Step 3: Implement `internal/manager/context.go`.** Delete `CapabilityAction`; `ContextData` as in Interfaces above (add `Capability string` under `Action`). Replace the `actionBlock`/`rolesBlock` construction in `RenderContext`:

```go
	actionBlock := ""
	if data.Action != "" {
		bin := binOr(data.ATMBin)
		code := data.Code
		if code == "" {
			code = "<CODE>"
		}
		scope := fmt.Sprintf("each enabled capability (`%s capability list --project %s` enumerates them)", bin, code)
		if data.Capability != "" {
			scope = fmt.Sprintf("the `%s` capability", data.Capability)
		}
		switch data.Action {
		case "brief":
			actionBlock = fmt.Sprintf("## Current manager action\n\nFocus this session on **brief**. For %s, run `%s capability <name> guide` and follow its \"Brief\" section — interview the human to set up that capability's territory.\n", scope, bin)
		case "autopilot":
			actionBlock = fmt.Sprintf("## Current manager action\n\nFocus this session on **autopilot**. For %s, run `%s capability <name> guide` and follow its \"Autopilot\" section — autonomously keep that capability's territory following its guide.\n", scope, bin)
		case "ask":
			actionBlock = fmt.Sprintf("## Current manager action\n\nFocus this session on **ask**. Standby for the human to ask questions; do not act proactively and do not mutate the ledger. Read the guide of %s (`%s capability <name> guide`) to be ready to answer.\n", scope, bin)
		default:
			actionBlock = fmt.Sprintf("## Current manager action\n\nFocus this session on **%s**.\n", data.Action)
		}
	}
```

Remove `<CAPABILITY_ROLES>` from the replacer pairs and its empty-value exception; delete `titleWord`. (Values are substituted into the template in a single pass, so the block must embed concrete `bin`/`code` — never `<ATM_BIN>`-style placeholders that the replacer would skip.)

`internal/manager/context_v1.md` — delete the `<CAPABILITY_ROLES>` line and replace the two role bullets, so `## Your Roles` reads:

```
## Your Roles

Capabilities own the operating procedures. Enumerate them with `<ATM_BIN> capability list --project <CODE>`; for each enabled capability run `<ATM_BIN> capability <name> guide` — its "Brief" section is the human-interview setup procedure, its "Autopilot" section the autonomous maintenance procedure, and the whole guide is your reference when the human asks questions. The current manager action (above) tells you which mode this session runs in. Whatever the mode: keep the ledger legible, ground every answer in cited task/comment IDs, and ask the human one-by-one when a task's intent is unclear.
```

- [ ] **Step 4: Implement `internal/cli/manager.go`.**
  - `managerOpts`: fields `Project, Integration, Persona, Agent string; DefaultArgs []string; Action, Capability string; ExtraArgs []string` (delete `Curate, Recall, Mapping, Onboarding`). Delete `managerCoreActions`.
  - `bindManagerActionFlags` →

```go
func bindManagerActionFlags(cmd *cobra.Command, opts *managerOpts) {
	cmd.Flags().StringVar(&opts.Action, "action", "autopilot", "manager action: brief (interview the human to set up each capability), autopilot (autonomously maintain each capability's territory), ask (read-only standby for questions)")
	cmd.Flags().StringVar(&opts.Capability, "capability", "", "scope the action to one enabled capability (default: all enabled)")
}
```

  - `validateManagerAction` →

```go
// validateManagerAction checks the semantic-agnostic action vocabulary and
// the optional capability scope. The scope is validated against the FULL
// registry first (typo → registered list), then the enabled set (known but
// disabled → how to enable it).
func validateManagerAction(action, capabilityName string, enabled, registered []string) error {
	switch action {
	case "brief", "autopilot", "ask":
	default:
		return fmt.Errorf("%w: unknown manager action %q (available: brief, autopilot, ask)", ErrUsage, action)
	}
	if capabilityName == "" {
		return nil
	}
	if !slices.Contains(registered, capabilityName) {
		return fmt.Errorf("%w: unknown capability %q (registered: %s)", ErrUsage, capabilityName, strings.Join(registered, ", "))
	}
	if !slices.Contains(enabled, capabilityName) {
		return fmt.Errorf("%w: capability %q is not enabled for this project; run `atm project capability add --project <CODE> --name %s` first", ErrUsage, capabilityName, capabilityName)
	}
	return nil
}
```

(import `slices`; `enabled` at the call site is `st.registry.Names()` — mount-narrowed by `--project` — and `registered` is `st.fullRegistry.Names()`.)
  - `runManager`: replace the `available := …ManagerActions` / `validateManagerAction(opts, available)` / `capActions` / `consult` block with:

```go
	if err := validateManagerAction(opts.Action, opts.Capability, st.registry.Names(), st.fullRegistry.Names()); err != nil {
		return err
	}
```

`RenderContext` call: drop `CapabilityActions`/`ActionConsult`, add `Capability: opts.Capability`. Replace the argv selection with unconditional `base := l.BuildArgvManage(contextPath)`; delete the `onboarding` bool, the `setTmuxWindowLabel(os.Stdout, tmuxLabelOnboarding)` call, and pass the new signature `managerEnvValues(opts.Project, atmBin, actor, runID, contextPath, effectivePersona, opts.Action, opts.Capability)`:

```go
func managerEnvValues(project, atmBin, actor, runID, contextPath, persona, action, capability string) map[string]string {
	return map[string]string{
		"ATM_ROLE":               "manager",
		"ATM_PROJECT":            project,
		"ATM_BIN":                atmBin,
		"ATM_ACTOR":              actor,
		"ATM_RUN_ID":             runID,
		"ATM_CONTEXT_FILE":       contextPath,
		"ATM_PERSONA":            persona,
		"ATM_MANAGER_ACTION":     action,
		"ATM_MANAGER_CAPABILITY": capability,
	}
}
```

  - `newManageContextCmd`: delete the `ManagerActions`/`capActions` block; `ContextData{Code: opts.Project, Actor: opts.Actor}` plus the existing ATMBin/Name resolution.
  - `internal/cli/tmux.go`: delete `tmuxLabelOnboarding` and its comment sentence.
  - `internal/cli/init.go:101`: `"Next: atm manage --project <CODE> --action brief"`.

- [ ] **Step 5: Run + regenerate** — `go test ./internal/manager ./internal/cli` → fix remaining assertions (launch-header goldens: `--curate` scenarios re-record as default/`--action` scenarios; delete `manage-codex-action-mapping-launch.json`, add `manage-codex-action-brief-launch.json` via a renamed test). `go test ./internal/cli -update`; inspect env diffs (`ATM_MANAGER_ACTION=autopilot`, new `ATM_MANAGER_CAPABILITY=`). Then `go test ./...` → PASS.
- [ ] **Step 6: Commit** — `feat(ATM-b678b0)!: manager actions are brief/autopilot/ask with --capability scope; role list and onboard flavor gone from the prompt`

---

### Task 8: Delete the dead seams — `BuildArgvOnboard` + `ManagerActions`

Nothing calls them after Task 7; remove them from the interfaces so no future implementation is forced to provide them.

**Files:**
- Modify: `internal/manager/launcher.go` (drop `BuildArgvOnboard` from `Launcher`, `staticLauncher`, `OllamaLauncher`)
- Modify: `internal/capability/capability.go` (drop `ActionSpec`, `ManagerAction`, `Capability.ManagerActions`, `Registry.ManagerActions`)
- Modify: `internal/capability/workflow/guide.go`, `internal/capability/contextmap/guide.go` (delete the `ManagerActions` methods + stale comments)
- Test: `internal/manager/launcher_test.go`, `internal/capability` tests, `internal/cli/env_test.go` fakes

**Interfaces:**
- Produces: `manager.Launcher{Name, NotFoundHint, BuildArgv, BuildArgvManage}`; `capability.Capability{Name, Summary, Guide, Command, EnsureVocabulary}` — the final v2 interface shapes.

- [ ] **Step 1: Delete the methods/types** listed above, plus `grep -rn "BuildArgvOnboard\|ManagerActions\|ActionSpec\|ATM_ONBOARD" internal/ cmd/` → must return nothing.
- [ ] **Step 2: Update tests** — launcher tests covering onboard argv are deleted; capability registry tests covering `ManagerActions` aggregation are deleted; any fake `Capability` in tests drops the method.
- [ ] **Step 3: Run** — `go test ./...` → PASS.
- [ ] **Step 4: Commit** — `refactor(ATM-b678b0): drop BuildArgvOnboard and ManagerActions from the launcher/capability seams`

---

### Task 9: Substrate command help fattening

Conventions now defers to `-h`; give each substrate namespace a genuinely informative `Long` (spec §8).

**Files:**
- Modify: `internal/cli/task.go` (root `atm task` cmd), `internal/cli/comment.go` (`atm task comment`), `internal/cli/label.go`, `internal/cli/project.go`, `internal/cli/persona.go`, `internal/cli/activity.go`, `internal/cli/store.go`, `internal/cli/search.go`
- Test: `internal/cli/root_test.go` (add one walk test)

- [ ] **Step 1: Write failing test**

```go
func TestSubstrateNamespacesHaveInformativeHelp(t *testing.T) {
	st := &cliState{}
	root := newRootCmdWithState(st)
	want := map[string]string{ // command path → a phrase its Long must contain
		"task":         "<CODE>-<hex>",
		"task comment": "kind",
		"label":        "board",
		"project":      "capabilit",
		"persona":      "persona@agent:model",
		"activity":     "--group-by",
		"store":        "prune-v1",
		"search":       "semantic",
	}
	for path, phrase := range want {
		cmd, _, err := root.Find(strings.Fields(path))
		if err != nil {
			t.Fatalf("find %q: %v", path, err)
		}
		if len(cmd.Long) < 100 {
			t.Errorf("atm %s -h Long is thin (%d chars)", path, len(cmd.Long))
		}
		if !strings.Contains(cmd.Long, phrase) {
			t.Errorf("atm %s -h Long missing %q", path, phrase)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**, then **Step 3: write the Long texts**. Content per namespace (write as flowing prose, 3–6 sentences each; these are the required facts):
  - `atm task` — a task = ID + title + description + label set; description is the running narrative agents append to; ID formats: `<CODE>-<hex>` for v2-born tasks, `<CODE>-<NNNN>` for v1 imports; key verbs list/show/create/set-*/comment/label.
  - `atm task comment` — append-mostly per-task thread; each comment is classified by a `<CODE>:comment:<kind>` label chosen by the writer (kinds are invented on demand, e.g. progress/decision/open-question); `--reply-to <COMMENT-ID>` threads within the task.
  - `atm label` — names are `<CODE>:<ns>:<value>` or `<CODE>:<tag>`; three kinds: stored (tasks assert it), namespace (`<ns>:*` prefix, emergent, describable), board (`--expr`, computed, never assigned directly); the description is the label's intention record — a label without one is a flag for human review.
  - `atm project` — minimal create (`--code` `^[A-Z]{3,6}$`, `--name`); per-project capability enablement (`project capability list/add/remove`) gates which `atm capability <name>` commands mount.
  - `atm persona` — actor format `persona@agent:model`; persona segment must be a registered persona; built-ins developer/manager/admin seeded on first use.
  - `atm activity` — audit log over the event stream; `--group-by persona|agent|model`.
  - `atm store` — event-log administration: `log`, `upgrade` (v1 import), `prune-v1`, `set-format`.
  - `atm search` — semantic search over tasks + comments when an embedding index exists (`atm project set-embedding` + `atm embed`), text fallback otherwise.
- [ ] **Step 4: Run** — `go test ./internal/cli` → PASS (help goldens, if any capture `-h`, regenerate with `-update`).
- [ ] **Step 5: Commit** — `docs(ATM-b678b0): substrate namespace help carries the conventions each agent needs`

---

### Task 10: Docs, doctrine, and whole-initiative verification

**Files:**
- Modify: `docs/architecture/label-substrate-and-capabilities.md`
- Test: full suites + scripted e2e sanity

- [ ] **Step 1: Amend the architecture doc.** Update the capability obligations to the v2 doctrine (spec "Doctrine notes"): EnsureVocabulary is the single self-setup seam returning owned boards (no `seed.go`, no `atm label seed` — fix the sentence at line ~58 that references it); capabilities mount under `atm capability <Name()>`; UI picks the default board; manager actions are brief/autopilot/ask walking guides' `## Brief`/`## Autopilot`; conventions is a minimal primer that does not enumerate capabilities.
- [ ] **Step 2: Full verification.**

```bash
go test ./...
(cd libs/eventsource && go test ./...)
go build -o /tmp/claude-1000/-home-ttran-projects-scyllas-atm/47343921-e794-4885-96e8-7a7e168ccc0a/scratchpad/atm ./cmd/atm
```

E2e sanity against a scratch store (`ATM_HOME=$(mktemp -d)`, actor `admin@cli:unset` is default; use `--actor developer@claude:<model>` on mutations):

```bash
atm=/tmp/claude-1000/-home-ttran-projects-scyllas-atm/47343921-e794-4885-96e8-7a7e168ccc0a/scratchpad/atm
export ATM_HOME=$(mktemp -d)
$atm project create --code DEM --name Demo --actor developer@claude:test
$atm capability list --project DEM                     # workflow true, contextmap true
$atm label list --project DEM                          # status:*+values, boards, context set; NO comment:*/priority:*
$atm capability workflow -h                            # verb tree mounts
ATM_PROJECT=DEM $atm capability contextmap guide | head -5       # guide serves
$atm project capability remove --project DEM --name contextmap --actor developer@claude:test
$atm capability list --project DEM                     # contextmap false
ATM_PROJECT=DEM $atm capability contextmap guide; echo "exit=$?" # unknown command (hard gate)
$atm manage-context --project DEM | grep -c CAPABILITY_ROLES     # 0
$atm workflow status --project DEM; echo "exit=$?"               # unknown command (flag day)
$atm label seed --project DEM; echo "exit=$?"                    # unknown command
$atm conventions | head -20                                       # primer
```

- [ ] **Step 3: Ledger + commit.** Comment progress on ATM-b678b0 (`atm task comment add --task ATM-b678b0 --label ATM:comment:progress --actor developer@claude:<model> --body "…"`), then `docs(ATM-b678b0): architecture doctrine updated for capability namespace + manager actions v2`.

---

## Execution notes

- **Order is load-bearing:** Task 5 (conventions rewrite) must precede Task 6 (seed deletion) because `conventions.go` imports `seed` until rewritten; Task 3 precedes 5 because the primer references `atm capability list`; Task 7 precedes 8 because the seam deletions require zero callers.
- **Golden discipline:** every `-update` run is followed by reading the diff. A golden that changed in a way the task didn't predict is a finding, not noise.
- **The TUI capability toggle** (`internal/tui/projects_capability_test.go` surface) is untouched by design — enablement semantics didn't change, only where commands mount.
- **Worktree:** execute on a feature branch (e.g. `worktree-capability-namespace-v2`) via superpowers:using-git-worktrees, merging to `main` only after whole-branch review, mirroring the phase 1–3 workflow.
