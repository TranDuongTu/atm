# Capability Semantics Phase 3 — Capability-Driven Manager Scope

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The manager's scope is composed: `--curate`/`--recall` remain the irreducible substrate core; enabled capabilities contribute named actions (contextmap → `mapping`) selected via `atm manage --action <name>` and rendered into the manager prompt as an enumerated role list pointing at capability guides. `--mapping` survives as a deprecated alias.

**Architecture:** `Capability` gains `ManagerActions() []ActionSpec`; the registry aggregates them (with each capability's command name, for the consult pointer). `manager.ContextData` gains the action list and a consult field; `context_v1.md` gets a `<CAPABILITY_ROLES>` placeholder where the fixed Mapping role used to be. `internal/cli/manager.go` validates `--action` against the mount-narrowed registry (Phase 2's prescan already filters `st.registry` by the `--project` flag), so an action of a disabled capability is simply unavailable.

**Tech Stack:** Go, cobra, the golden harness.

**Spec:** `docs/superpowers/specs/2026-07-18-capability-semantics-initiative-design.md` (Phase 3 section). Ledger task: `ATM-b678b0`. **Prerequisites: Phases 1 and 2 merged.**

## Global Constraints

- Never hard-break a flag on a stable CLI surface (ATM-0113 precedent): `--mapping` and `--onboarding` stay as deprecated aliases for `--action mapping`.
- The manager core is irreducible: `curate` and `recall` are NOT capability actions and must work for every project regardless of enablement.
- Prompt composition rule: the rendered prompt enumerates and points (name, summary, consult verb); it never restates a capability's procedure.
- `ATM_MANAGER_ACTION` keeps carrying the action name verbatim; the launcher's `BuildArgvOnboard` path stays tied to the `mapping` action literal (a launcher-flavor nuance, documented in code, not generalized — YAGNI).
- Goldens: regenerate with `go test ./internal/cli -update`; inspect diffs.
- Commit style: `type(ATM-b678b0): message`, footer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: `ActionSpec` and `ManagerActions()` on the capability seam

**Files:**
- Modify: `internal/capability/capability.go`
- Modify: `internal/capability/workflow/guide.go` (add method)
- Modify: `internal/capability/contextmap/guide.go` (add method)
- Test: `internal/capability/actions_test.go` (new), plus one assertion each in the two capability packages' guide tests

**Interfaces:**
- Consumes: Phase 1's `fakeCap`/`fakeEnv` test doubles (extend `fakeCap` with the new method — the interface grows, so every fake must follow).
- Produces (exact; Task 2 and 3 rely on these):

```go
// In package capability:
type ActionSpec struct {
	Name    string // the --action value, e.g. "mapping"
	Summary string // one line for the flag help and the prompt role list
}

type ManagerAction struct {
	Capability string // capability name, e.g. "contextmap"
	Command    string // mounted command name, e.g. "context" (for the consult pointer)
	Name       string
	Summary    string
}

// Capability interface gains:
//   ManagerActions() []ActionSpec   // nil = contributes none

func (r *Registry) ManagerActions(env Env) []ManagerAction
```

- [ ] **Step 1: Write the failing test**

Create `internal/capability/actions_test.go`:

```go
package capability

import (
	"reflect"
	"testing"
)

type fakeActionCap struct {
	fakeCap
	actions []ActionSpec
}

func (c fakeActionCap) ManagerActions() []ActionSpec { return c.actions }

func TestManagerActionsAggregate(t *testing.T) {
	r := NewRegistry(
		fakeActionCap{fakeCap: fakeCap{name: "alpha", cmdName: "al"}},
		fakeActionCap{
			fakeCap: fakeCap{name: "beta", cmdName: "be"},
			actions: []ActionSpec{{Name: "sweep", Summary: "sweep the things"}},
		},
	)
	got := r.ManagerActions(&fakeEnv{})
	want := []ManagerAction{{Capability: "beta", Command: "be", Name: "sweep", Summary: "sweep the things"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ManagerActions = %+v, want %+v", got, want)
	}
	var nilr *Registry
	if nilr.ManagerActions(&fakeEnv{}) != nil {
		t.Fatal("nil registry must yield nil actions")
	}
}
```

Also add `func (c fakeCap) ManagerActions() []ActionSpec { return nil }` to the Phase 1 `fakeCap` in `guide_test.go` — the interface extension breaks it otherwise (that compile break is part of the expected failure).

Add to `internal/capability/contextmap/guide_test.go`:

```go
func TestManagerActionIsMapping(t *testing.T) {
	acts := Cap{}.ManagerActions()
	if len(acts) != 1 || acts[0].Name != "mapping" || acts[0].Summary == "" {
		t.Fatalf("contextmap must contribute exactly the mapping action, got %+v", acts)
	}
}
```

Add to `internal/capability/workflow/guide_test.go`:

```go
func TestNoManagerActions(t *testing.T) {
	if acts := Cap{}.ManagerActions(); acts != nil {
		t.Fatalf("workflow contributes no manager action, got %+v", acts)
	}
}
```

(Import `atm/internal/capability` in both capability-package test files for the `capability.ActionSpec` return type if the method signature references it — see Step 3.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/capability/... -v`
Expected: FAIL — `ActionSpec` undefined / interface not implemented.

- [ ] **Step 3: Implement**

(a) `internal/capability/capability.go` — add the types, extend the interface, aggregate:

```go
// ActionSpec is one manager action a capability contributes: a session mode
// the manager can be launched in. The procedure behind it lives in the
// capability's guide (Manager duty section), never in a prompt.
type ActionSpec struct {
	Name    string
	Summary string
}

// ManagerAction is an aggregated action entry: the ActionSpec plus which
// capability contributed it and the command an agent consults for it.
type ManagerAction struct {
	Capability string
	Command    string
	Name       string
	Summary    string
}
```

Add to the `Capability` interface:

```go
	// ManagerActions lists the manager session modes this capability
	// contributes (nil for none). The manager core (curate, recall) is not
	// a capability action — it exists for every project.
	ManagerActions() []ActionSpec
```

Add the registry aggregator:

```go
// ManagerActions aggregates the enabled capabilities' contributed manager
// actions in registration order. Command comes from the built tree, like
// Describe, so consult pointers cannot drift.
func (r *Registry) ManagerActions(env Env) []ManagerAction {
	if r == nil {
		return nil
	}
	var out []ManagerAction
	for _, c := range r.caps {
		specs := c.ManagerActions()
		if len(specs) == 0 {
			continue
		}
		cmdName := c.Command(env).Name()
		for _, s := range specs {
			out = append(out, ManagerAction{Capability: c.Name(), Command: cmdName, Name: s.Name, Summary: s.Summary})
		}
	}
	return out
}
```

(b) `internal/capability/workflow/guide.go`:

```go
// ManagerActions: none — status hygiene falls under the manager's core
// curate role (see guide.md "Manager duty").
func (Cap) ManagerActions() []capability.ActionSpec { return nil }
```

(c) `internal/capability/contextmap/guide.go`:

```go
// ManagerActions contributes the mapping session mode; its procedure is the
// guide's "Manager duty" section.
func (Cap) ManagerActions() []capability.ActionSpec {
	return []capability.ActionSpec{{
		Name:    "mapping",
		Summary: "reconcile the project's context map against the repo: verify drifted pointers, discover new territory",
	}}
}
```

(Both files import `atm/internal/capability` — allowed by the arch rules: capability packages may import the registry package.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/... ./tests/arch -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/capability/capability.go internal/capability/workflow/guide.go internal/capability/workflow/guide_test.go internal/capability/contextmap/guide.go internal/capability/contextmap/guide_test.go internal/capability/actions_test.go
git commit -m "feat(ATM-b678b0): capabilities contribute manager actions; registry aggregates with consult pointers"
```

---

### Task 2: Manager prompt composes the capability role list

**Files:**
- Modify: `internal/manager/context.go`
- Modify: `internal/manager/context_v1.md`
- Test: `internal/manager/context_test.go` (append)

**Interfaces:**
- Consumes: nothing from `internal/capability` — `internal/manager` stays decoupled; the CLI passes plain data.
- Produces (exact; Task 3 relies on these):

```go
// In package manager:
type CapabilityAction struct {
	Name    string // action name, e.g. "mapping"
	Summary string
	Command string // capability command for the consult pointer, e.g. "context"
}

// ContextData gains:
//   CapabilityActions []CapabilityAction // rendered into <CAPABILITY_ROLES>
//   ActionConsult     string             // command name when the selected Action is capability-contributed; "" for core actions
```

- [ ] **Step 1: Write the failing test**

Append to `internal/manager/context_test.go`:

```go
func TestRenderCapabilityRoles(t *testing.T) {
	rendered := RenderContext(ContextData{
		Code: "X", Name: "X", ATMBin: "atm", Actor: "a@b:c",
		CapabilityActions: []CapabilityAction{
			{Name: "mapping", Summary: "reconcile the context map", Command: "context"},
		},
	})
	for _, want := range []string{
		"**Mapping**",
		"reconcile the context map",
		"`atm context guide`",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered prompt missing %q", want)
		}
	}
	if strings.Contains(rendered, "<CAPABILITY_ROLES>") {
		t.Error("placeholder must be substituted when a project is rendered")
	}
}

func TestRenderNoCapabilityActions(t *testing.T) {
	rendered := RenderContext(ContextData{Code: "X", Name: "X", ATMBin: "atm", Actor: "a@b:c"})
	if strings.Contains(rendered, "<CAPABILITY_ROLES>") || strings.Contains(rendered, "**Mapping**") {
		t.Error("no contributed actions: role list must render empty, not leak placeholder or stale roles")
	}
	// The core roles survive composition untouched.
	for _, want := range []string{"**Curate**", "**Recall**"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("core role %q missing", want)
		}
	}
}

func TestRenderActionBlockConsult(t *testing.T) {
	rendered := RenderContext(ContextData{
		Code: "X", Name: "X", ATMBin: "atm", Actor: "a@b:c",
		Action: "mapping", ActionConsult: "context",
	})
	if !strings.Contains(rendered, "Focus this session on **mapping**") {
		t.Error("action block missing")
	}
	if !strings.Contains(rendered, "atm context guide") {
		t.Error("capability action block must point at the capability guide")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/manager -run 'TestRenderCapabilityRoles|TestRenderNoCapabilityActions|TestRenderActionBlockConsult' -v`
Expected: FAIL — `CapabilityAction` undefined.

- [ ] **Step 3: Implement**

(a) `internal/manager/context_v1.md` — replace the Mapping role bullet (which Phase 1 turned into a pointer line) with the placeholder, so the Roles section reads:

```markdown
## Your Roles

- **Curate** — keep the ledger legible and current: review open backlog, triage unlabeled and under-described tasks, handle developing-agent handoffs, and maintain the project's shared vocabulary (recurring terms, short definitions, naming consistency) as you go. If you are not clear about what a Task should do, ask the user one by one to clarify.
- **Recall** — recall and link knowledge on request, grounded in cited IDs; you digest your own journal too, connecting related tasks and keeping them searchable. Read-only: synthesize and cite; do not mutate the ledger.
<CAPABILITY_ROLES>
```

(Curate/Recall text stays byte-identical to what is there today.)

(b) `internal/manager/context.go`:

```go
type CapabilityAction struct {
	Name    string
	Summary string
	Command string
}
```

Add to `ContextData`:

```go
	// CapabilityActions are the manager actions the project's enabled
	// capabilities contribute; rendered as additional Roles bullets that
	// point at each capability's guide. The procedures live in the guides.
	CapabilityActions []CapabilityAction
	// ActionConsult is the capability command to consult when Action is a
	// capability-contributed action ("" for the core curate/recall).
	ActionConsult string
```

In `RenderContext`, build the roles block and extend the action block:

```go
	rolesBlock := ""
	for _, a := range data.CapabilityActions {
		rolesBlock += fmt.Sprintf("- **%s** — %s. Capability-contributed action: run `%s %s guide` and follow its \"Manager duty\" section for the operating procedure.\n",
			titleWord(a.Name), a.Summary, binOr(data.ATMBin), a.Command)
	}
```

With two small helpers:

```go
// binOr keeps the <ATM_BIN> placeholder alive in a generic render (no
// project), matching the replacer's convention for empty fields.
func binOr(bin string) string {
	if bin == "" {
		return "<ATM_BIN>"
	}
	return bin
}

// titleWord upper-cases the first byte of an ASCII action name for the role
// bullet ("mapping" -> "Mapping"). Not strings.Title (deprecated).
func titleWord(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
```

Extend the action block: after the existing `actionBlock` sentence, when `data.ActionConsult != ""` append:

```go
	if data.Action != "" && data.ActionConsult != "" {
		actionBlock += fmt.Sprintf("This action is capability-contributed: run `%s %s guide` and follow its \"Manager duty\" section.\n",
			binOr(data.ATMBin), data.ActionConsult)
	}
```

Add the substitution pair — `<CAPABILITY_ROLES>` joins `<PERSONA_BLOCK>`/`<ACTION_BLOCK>` in the "substitute even when empty" exception list in the replacer loop (empty list → the placeholder line disappears entirely):

```go
		"<CAPABILITY_ROLES>", rolesBlock,
```

and extend the exception condition:

```go
		if val == "" && key != "<PERSONA_BLOCK>" && key != "<ACTION_BLOCK>" && key != "<CAPABILITY_ROLES>" {
```

(A leftover blank line where the placeholder stood is acceptable; if the golden diff shows a double blank line, `strings.TrimRight` the block or strip `"\n<CAPABILITY_ROLES>"` → `rolesBlock` prefixed with `"\n"` when non-empty — pick whichever keeps the rendered markdown tidy.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/manager -count=1 -v`
Expected: PASS, including Phase 1's `TestTemplateMappingRolePointsAtGuide` — **update that test**: it asserted the static pointer line; the assertion "rendered contains `atm context guide`" now only holds when `CapabilityActions` are passed. Rewrite it to pass `CapabilityActions: []CapabilityAction{{Name: "mapping", Summary: "…", Command: "context"}}` and keep its banned-fragments list unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/manager/context.go internal/manager/context_v1.md internal/manager/context_test.go
git commit -m "feat(ATM-b678b0): manager prompt composes capability role list via <CAPABILITY_ROLES>; action block consults guides"
```

---

### Task 3: CLI — `--action` selects, registry validates, aliases preserved

**Files:**
- Modify: `internal/cli/manager.go`
- Test: `internal/cli/manager_test.go` (append) + regenerated goldens

**Interfaces:**
- Consumes: `st.registry.ManagerActions(st)` (Task 1; `st.registry` is already narrowed to the `--project`'s enabled set by Phase 2's prescan), `manager.CapabilityAction`/`ContextData` fields (Task 2).
- Produces:
  - `atm manage --project X --action mapping` — capability action; `--curate`/`--recall` unchanged; `--mapping`/`--onboarding` deprecated aliases for `--action mapping`.
  - Unknown or not-enabled action → usage error listing `curate, recall` + the available capability actions.
  - `managerOpts` gains `Action string`; `validateManagerAction` signature becomes `validateManagerAction(opts managerOpts, available []capability.ManagerAction) (string, *capability.ManagerAction, error)` (returns the selected action name and, when capability-contributed, its entry for the consult pointer).

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/manager_test.go`:

```go
func TestValidateManagerActionCapability(t *testing.T) {
	avail := []capability.ManagerAction{{Capability: "contextmap", Command: "context", Name: "mapping", Summary: "s"}}

	name, entry, err := validateManagerAction(managerOpts{Action: "mapping"}, avail)
	if err != nil || name != "mapping" || entry == nil || entry.Command != "context" {
		t.Fatalf("got (%q,%v,%v)", name, entry, err)
	}
	name, entry, err = validateManagerAction(managerOpts{}, avail)
	if err != nil || name != "curate" || entry != nil {
		t.Fatalf("default: got (%q,%v,%v)", name, entry, err)
	}
	name, entry, err = validateManagerAction(managerOpts{Mapping: true}, avail)
	if err != nil || name != "mapping" || entry == nil {
		t.Fatalf("--mapping alias: got (%q,%v,%v)", name, entry, err)
	}
	if _, _, err = validateManagerAction(managerOpts{Action: "nosuch"}, avail); err == nil {
		t.Fatal("unknown action must error")
	}
	// contextmap disabled for the project -> mapping not available.
	if _, _, err = validateManagerAction(managerOpts{Action: "mapping"}, nil); err == nil {
		t.Fatal("action of a disabled capability must error")
	}
	if _, _, err = validateManagerAction(managerOpts{Curate: true, Action: "mapping"}, avail); err == nil {
		t.Fatal("two selections must error")
	}
}
```

Add `"atm/internal/capability"` to the test file imports. Then a golden for the launch surface (mirrors the existing `manage-codex-curate-launch` golden test — locate it in `manager_test.go` and copy its harness setup exactly, substituting args):

```go
func TestGoldenManageActionMappingLaunch(t *testing.T) {
	// same harness + fake-launcher arrangement as the curate launch golden,
	// with args: manage --project ATM --action mapping ...
	// compareGolden(t, "manage-codex-action-mapping-launch", out)
}
```

(Write it concretely by copying the neighboring golden test's body — the launch plumbing, fake child runner, and env assertions are established there; only the action args and golden name differ. `ATM_MANAGER_ACTION` must come out `mapping` and `ATM_ONBOARD=1` must be present.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cli -run TestValidateManagerAction -v`
Expected: FAIL — signature mismatch / `Action` field undefined.

- [ ] **Step 3: Implement in `internal/cli/manager.go`**

(a) `managerOpts` gains `Action string`. Delete the `managerAction` string type and its three constants (replaced by plain strings; core names live in one place):

```go
// managerCoreActions are the manager's irreducible substrate duties. They
// exist for every project; capability actions come from the registry.
var managerCoreActions = []string{"curate", "recall"}
```

(b) `bindManagerActionFlags` — add the generic selector, demote `--mapping` to a deprecated alias:

```go
func bindManagerActionFlags(cmd *cobra.Command, opts *managerOpts) {
	cmd.Flags().StringVar(&opts.Action, "action", "", "manager action: curate, recall, or a capability-contributed action (see `atm manage --project <CODE> --help`)")
	cmd.Flags().BoolVar(&opts.Curate, "curate", false, "review backlog, triage, track handoffs, and maintain vocabulary (default)")
	cmd.Flags().BoolVar(&opts.Recall, "recall", false, "read-only synthesis grounded in ledger IDs; does not mutate")

	// Deprecated aliases (never hard-break a flag on a stable CLI surface,
	// ATM-0113): --mapping predates capability-contributed actions;
	// --onboarding predates --mapping.
	cmd.Flags().BoolVar(&opts.Mapping, "mapping", false, "")
	_ = cmd.Flags().MarkDeprecated("mapping", "use --action mapping")
	_ = cmd.Flags().MarkHidden("mapping")
	cmd.Flags().BoolVar(&opts.Onboarding, "onboarding", false, "")
	_ = cmd.Flags().MarkDeprecated("onboarding", "use --action mapping")
	_ = cmd.Flags().MarkHidden("onboarding")
}
```

(c) `validateManagerAction`:

```go
// validateManagerAction resolves the session's action: the core duties
// (curate, recall) always exist; capability actions must be contributed by
// an enabled capability (`available` comes from the mount-narrowed
// registry, so a disabled capability's action is simply absent).
func validateManagerAction(opts managerOpts, available []capability.ManagerAction) (string, *capability.ManagerAction, error) {
	var selected []string
	if opts.Curate {
		selected = append(selected, "curate")
	}
	if opts.Recall {
		selected = append(selected, "recall")
	}
	if opts.Mapping || opts.Onboarding {
		selected = append(selected, "mapping")
	}
	if opts.Action != "" {
		selected = append(selected, opts.Action)
	}
	if len(selected) > 1 {
		return "", nil, fmt.Errorf("%w: choose one manager action", ErrUsage)
	}
	if len(selected) == 0 {
		return "curate", nil, nil
	}
	name := selected[0]
	for _, core := range managerCoreActions {
		if name == core {
			return name, nil, nil
		}
	}
	for i := range available {
		if available[i].Name == name {
			return name, &available[i], nil
		}
	}
	names := append([]string{}, managerCoreActions...)
	for _, a := range available {
		names = append(names, a.Name)
	}
	return "", nil, fmt.Errorf("%w: unknown manager action %q (available: %s)", ErrUsage, name, strings.Join(names, ", "))
}
```

(Imports: `strings`, `atm/internal/capability`.)

(d) `runManager` — thread the registry through:

```go
	available := st.registry.ManagerActions(st)
	action, entry, err := validateManagerAction(opts, available)
	if err != nil {
		return err
	}
```

Build the render data additions:

```go
	capActions := make([]manager.CapabilityAction, 0, len(available))
	for _, a := range available {
		capActions = append(capActions, manager.CapabilityAction{Name: a.Name, Summary: a.Summary, Command: a.Command})
	}
	consult := ""
	if entry != nil {
		consult = entry.Command
	}
```

and pass `CapabilityActions: capActions, ActionConsult: consult` in the `manager.ContextData` literal. Replace `onboarding := action == managerActionMapping` with:

```go
	// The onboard argv flavor is a launcher nuance historically tied to the
	// mapping action; not generalized to other capability actions (YAGNI).
	onboarding := action == "mapping"
```

`Action: string(action)` becomes `Action: action`.

(e) `newManageContextCmd` — pass the registry's actions in the same way (`st.registry.ManagerActions(st)` → `CapabilityActions`) so the hidden context printer composes identically; with no `--project` the prescan leaves the full registry and the generic template renders all registered actions.

- [ ] **Step 4: Run tests, regenerate goldens**

Run: `go test ./internal/cli -run 'TestValidateManagerAction|TestGoldenManage' -v` → new golden via `-update` → full `go test ./internal/cli -count=1`. Inspect: existing `manage-*-launch` goldens change only by the composed Roles block / action flag help.
Expected: PASS.

- [ ] **Step 5: End-to-end sanity**

```bash
export TMPSTORE=$(mktemp -d)
go run ./cmd/atm --store "$TMPSTORE" project create --code A --name A --actor admin@cli:unset
go run ./cmd/atm --store "$TMPSTORE" manage-context --project A | grep -A4 'Your Roles'      # Curate, Recall, Mapping bullet w/ consult
go run ./cmd/atm --store "$TMPSTORE" project capability remove --project A --name contextmap --actor admin@cli:unset
go run ./cmd/atm --store "$TMPSTORE" manage-context --project A | grep -c '\*\*Mapping\*\*'  # 0 — disabled capability contributes nothing
```

- [ ] **Step 6: Commit**

```bash
git add internal/cli/manager.go internal/cli/manager_test.go internal/cli/testdata/golden/
git commit -m "feat(ATM-b678b0): manage --action selects capability-contributed actions; --mapping deprecated alias; scope composed per project"
```

---

### Task 4: Full verification, docs, ledger close-out

- [ ] **Step 1: Full suite**

Run: `go test ./... ./libs/...`
Expected: all `ok`.

- [ ] **Step 2: Update the conventions/day-to-day text**

`internal/cli/conventions.go` (`conventionsCoreText` + the structured `day_to_day_development` entry) still says `atm manage --project <CODE> --curate|--recall|--mapping` and `atm manage --project <CODE> --mapping` for first-run setup. Update both occurrences to `--curate|--recall|--action <name>` and `--action mapping` respectively; regenerate goldens (`go test ./internal/cli -update`, re-run, inspect).

```bash
git add internal/cli/conventions.go internal/cli/testdata/golden/
git commit -m "docs(ATM-b678b0): conventions reference --action for capability manager actions"
```

- [ ] **Step 3: Ledger close-out**

```bash
atm task comment add --task ATM-b678b0 --actor developer@claude:<your-model> \
  --label ATM:comment:progress \
  --body "Phase 3 (manage) implemented: ActionSpec/ManagerActions on the capability seam, <CAPABILITY_ROLES> composition in the manager prompt, manage --action validated against the project's enabled set (--mapping/--onboarding deprecated aliases). Initiative complete pending review/merge."
atm workflow complete --task ATM-b678b0 --actor developer@claude:<your-model>   # only after the branch is merged
```
