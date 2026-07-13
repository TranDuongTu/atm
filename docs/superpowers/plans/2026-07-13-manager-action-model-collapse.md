# Manager action model collapse — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collapse the manager's 6 action modes to 2 (Curate default, Recall), deleting the 5 old curation/asking flags; onboarding stays untouched for ATM-0085.

**Architecture:** Replace 6 `managerAction` constants + 6 bool flags + "exactly one" validation with 3 constants (`curate`/`recall`/`onboarding`) + 2 new bools (`Curate`/`Recall`; `Onboarding` stays) + Curate-default validation. Update the prompt template's "Your Roles" to 3 entries (glossary folded into Curate). Update conventions guide + README. No deprecated aliases; old flags are deleted.

**Tech Stack:** Go 1.22+, cobra CLI, table-driven tests, golden fixtures regenerated via `go test -update ./internal/cli/`.

## Global Constraints

- Go 1.22+ (`go build ./...`, `go vet ./...`).
- Verify gate: `make verify` (build + test + scripts-test).
- No emojis in code, comments, or commits.
- No comments in code unless asked.
- Golden fixtures regenerated with `go test -update ./internal/cli/` then committed.
- Follow existing style in neighboring files (e.g. `internal/cli/task.go:12-38` for the flag-binding shape, though this plan deletes flags rather than aliasing them).

**Spec:** `docs/superpowers/specs/2026-07-13-manager-action-model-collapse-design.md` (ATM-0120). The spec's "Onboard is deferred to ATM-0085" section is load-bearing: do NOT touch `--onboarding`, `managerActionOnboarding`, the `action == managerActionOnboarding` argv/env branch (`internal/cli/manager.go:296`), or the "Onboarding" role entry in `context_v1.md`.

---

## File map

- `internal/cli/manager.go` — collapse `managerAction` constants 6→3, `managerOpts` bools 6→2 (Curate, Recall; Onboarding stays), rewrite `bindManagerActionFlags`, rewrite `validateManagerAction` (Curate default), update error message. Onboarding argv/env branch untouched.
- `internal/manager/context_v1.md` — "Your Roles" 6→3 (Curate/Recall/Onboarding); glossary folded as a clause under Curate.
- `internal/cli/conventions.go` — 4 string spots (`--asking`→`--recall`, 6-flag list→3-flag list).
- `README.md` — 3 spots (lines ~30-34, ~105, ~120): old flag examples → `--curate`/`--recall`; `--onboarding` stays.
- `internal/cli/manager_test.go` — rename/repurpose 6 cases, add 3 new cases.
- `internal/manager/context_test.go` — `TestRenderContextActionCatalogPresent` assertion set.
- `internal/cli/testdata/golden/` — rename `manage-codex-planning-launch.json`→`manage-codex-curate-launch.json`; regenerate `conventions-text.json`, `conventions-json.json`, `determinism-conventions.json` via `-update`.

---

### Task 1: Collapse manager action constants, opts, and flag binding

**Files:**
- Modify: `internal/cli/manager.go:15-39` (opts struct + constants), `internal/cli/manager.go:174-207` (bind + validate)

**Interfaces:**
- Produces: `managerActionCurate`, `managerActionRecall`, `managerActionOnboarding` constants; `managerOpts.Curate`/`.Recall`/`.Onboarding` bools; `validateManagerAction(opts managerOpts) (managerAction, error)` with Curate default. Later tasks rely on these names.

- [ ] **Step 1: Write the failing test for Curate default + conflict**

Add to `internal/cli/manager_test.go` (replace the old `TestManageRequiresExactlyOneAction` — see Task 4 for the full rename; here add just these two new cases so the constant/validate changes drive a test cycle):

```go
func TestManageCurateIsDefault(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	out, _, code := h.run("manage", "--agent", "codex", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"ATM_MANAGER_ACTION": "curate"`) {
		t.Fatalf("default action should be curate; got:\n%s", out)
	}
	_ = c
}

func TestManageRejectsConflictingActions(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	for _, args := range [][]string{
		{"manage", "--agent", "codex", "--project", "FOO", "--curate", "--recall"},
		{"manage", "--agent", "codex", "--project", "FOO", "--recall", "--onboarding"},
		{"manage", "--agent", "codex", "--project", "FOO", "--curate", "--onboarding"},
	} {
		_, _, code := h.run(args...)
		if code == ExitSuccess {
			t.Fatalf("%v should fail (conflicting actions)", args)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestManageCurateIsDefault|TestManageRejectsConflictingActions' -v`
Expected: FAIL — `--curate`/`--recall` are unknown flags (not bound yet).

- [ ] **Step 3: Replace constants and opts struct**

In `internal/cli/manager.go`, replace lines 15-39:

```go
type managerOpts struct {
	Project     string
	Integration string
	Persona     string
	Agent       string
	DefaultArgs []string
	Curate      bool
	Recall      bool
	Onboarding  bool
	ExtraArgs   []string
}

type managerAction string

const (
	managerActionCurate     managerAction = "curate"
	managerActionRecall     managerAction = "recall"
	managerActionOnboarding managerAction = "onboarding"
)
```

- [ ] **Step 4: Replace bindManagerActionFlags and validateManagerAction**

In `internal/cli/manager.go`, replace lines 174-207:

```go
func bindManagerActionFlags(cmd *cobra.Command, opts *managerOpts) {
	cmd.Flags().BoolVar(&opts.Curate, "curate", false, "review backlog, triage, track handoffs, and maintain vocabulary (default)")
	cmd.Flags().BoolVar(&opts.Recall, "recall", false, "read-only synthesis grounded in ledger IDs; does not mutate")
	cmd.Flags().BoolVar(&opts.Onboarding, "onboarding", false, "learn a repo/project and organize it for later agents")
}

func validateManagerAction(opts managerOpts) (managerAction, error) {
	selected := []managerAction{}
	if opts.Curate {
		selected = append(selected, managerActionCurate)
	}
	if opts.Recall {
		selected = append(selected, managerActionRecall)
	}
	if opts.Onboarding {
		selected = append(selected, managerActionOnboarding)
	}
	if len(selected) > 1 {
		return "", fmt.Errorf("%w: choose one manager action: --curate, --recall, or --onboarding", ErrUsage)
	}
	if len(selected) == 1 {
		return selected[0], nil
	}
	return managerActionCurate, nil
}
```

- [ ] **Step 5: Run the new tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestManageCurateIsDefault|TestManageRejectsConflictingActions' -v`
Expected: PASS.

- [ ] **Step 6: Build to confirm the onboarding argv/env branch still compiles**

Run: `go build ./...`
Expected: succeeds. `managerActionOnboarding` is still referenced at `internal/cli/manager.go:296` (`onboarding := action == managerActionOnboarding`); the constant now refers to the same value.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/manager.go internal/cli/manager_test.go
git commit -m "refactor(ATM-0120): collapse manager action constants to curate/recall/onboarding

Curate is the default when no action flag is given; passing more than one
action flag errors. Onboarding flag and argv/env branch unchanged. Old
curation/asking constants and flags deleted."
```

---

### Task 2: Rename/repurpose the existing manager flag tests

The 6 existing cases in `internal/cli/manager_test.go` use the old `--planning`/`--grooming` flags. This task updates them to the new flag names so the whole file compiles and the existing behaviors (golden, env, persona, removed-command) keep passing.

**Files:**
- Modify: `internal/cli/manager_test.go` (6 cases)
- Rename: `internal/cli/testdata/golden/manage-codex-planning-launch.json` → `internal/cli/testdata/golden/manage-codex-curate-launch.json`

**Interfaces:**
- Consumes: `managerActionCurate` from Task 1.
- Produces: a passing `internal/cli/manager_test.go` over the new flag names; golden `manage-codex-curate-launch.json` with `ATM_MANAGER_ACTION: curate`.

- [ ] **Step 1: Update TestManageCodexPlanningLaunchJSON → TestManageCodexCurateLaunchJSON**

In `internal/cli/manager_test.go`, rename the function and swap the flag + golden name:

```go
func TestManageCodexCurateLaunchJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("manage", "--agent", "codex", "--project", "FOO", "--curate")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if c.name != "codex" {
		t.Fatalf("child name = %q, want codex", c.name)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manage-codex-curate-launch", got)
}
```

- [ ] **Step 2: Update TestManageLaunchAutoCreatesProject**

```go
	_, _, code := h.run("manage", "--agent", "codex", "--project", "FOO", "--curate")
```

- [ ] **Step 3: Repurpose TestManageRequiresExactlyOneAction → TestManageActionSelection**

The old "no flag" case now succeeds (Curate default); the old two-flag case becomes `--curate --recall` (conflict). Replace the function:

```go
func TestManageActionSelection(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)

	// No action flag: Curate is the default, so this succeeds.
	_, _, code := h.run("manage", "--agent", "codex", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("no-flag default should succeed (curate); exit=%d stderr=%s", code, h.stderr.String())
	}

	// Conflicting actions still error.
	for _, args := range [][]string{
		{"manage", "--agent", "codex", "--project", "FOO", "--curate", "--recall"},
		{"manage", "--agent", "codex", "--project", "FOO", "--recall", "--onboarding"},
	} {
		_, _, code := h.run(args...)
		if code == ExitSuccess {
			t.Fatalf("%v should fail (conflicting actions)", args)
		}
	}
}
```

(`TestManageRejectsConflictingActions` from Task 1 and `TestManageActionSelection` here overlap on the conflict cases; that is fine — keep both, the duplication is a deliberate regression guard. If you prefer to deduplicate, remove the conflict loop from `TestManageActionSelection` and keep only the no-flag default case here, leaving conflicts to `TestManageRejectsConflictingActions`.)

- [ ] **Step 4: Update TestManageRejectsDryRunAndActor**

Two flag swaps (`--planning` → `--curate`):

```go
		{"manage", "--agent", "codex", "--project", "FOO", "--curate", "--dry-run"},
		{"manage", "--agent", "codex", "--project", "FOO", "--curate", "--actor", "manager@codex:unset"},
```

- [ ] **Step 5: Update TestManagePersonaEnvAndActor**

Swap the flag and the assertion:

```go
	out, _, code := h.run("manage", "--agent", "claude", "--project", "FOO", "--curate", "--persona", "ops")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{
		`"ATM_PERSONA": "ops"`,
		`"ATM_MANAGER_ACTION": "curate"`,
		`"ATM_ACTOR": "ops@claude:unset"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("manager env missing %q:\n%s", want, out)
		}
	}
```

- [ ] **Step 6: Update TestManagerCommandRemoved**

```go
	_, _, code := h.run("manager", "codex", "--project", "FOO", "--curate")
```

- [ ] **Step 7: Create the renamed golden and remove the old one**

Regenerate the golden by running the renamed test with `-update`:

```bash
git mv internal/cli/testdata/golden/manage-codex-planning-launch.json internal/cli/testdata/golden/manage-codex-curate-launch.json
go test ./internal/cli/ -run 'TestManageCodexCurateLaunchJSON' -update -v
```

Then open `internal/cli/testdata/golden/manage-codex-curate-launch.json` and confirm `"ATM_MANAGER_ACTION": "planning"` became `"curate"` (the `-update` run rewrites the golden from live output, which Task 1 already drives to `curate`).

- [ ] **Step 8: Run the whole manager test file**

Run: `go test ./internal/cli/ -run 'TestManage' -v`
Expected: PASS (all `TestManage*` and `TestManager*` cases).

- [ ] **Step 9: Commit**

```bash
git add internal/cli/manager_test.go internal/cli/testdata/golden/manage-codex-curate-launch.json
git rm internal/cli/testdata/golden/manage-codex-planning-launch.json 2>/dev/null || true
git commit -m "test(ATM-0120): repurpose manager flag tests for curate/recall

Rename the planning golden to curate (ATM_MANAGER_ACTION=curate); the
no-action case now succeeds as the curate default."
```

---

### Task 3: Add the old-flags-removed regression guard

The 5 old flags must stay gone. This task adds a guard so a future re-addition fails a test.

**Files:**
- Modify: `internal/cli/manager_test.go` (append a new case)

**Interfaces:**
- Consumes: cobra's unknown-flag exit behavior (no constant needed).

- [ ] **Step 1: Write the failing-then-passing test**

Append to `internal/cli/manager_test.go`:

```go
func TestManageOldFlagsRemoved(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	for _, flag := range []string{"--planning", "--grooming", "--tracking", "--glossary", "--asking"} {
		_, stderr, code := h.run("manage", "--agent", "codex", "--project", "FOO", flag)
		if code == ExitSuccess {
			t.Fatalf("old flag %q should be unknown, but exit was 0", flag)
		}
		if !strings.Contains(stderr, "unknown flag") {
			t.Fatalf("old flag %q should error as 'unknown flag'; got stderr=%s", flag, stderr)
		}
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/cli/ -run 'TestManageOldFlagsRemoved' -v`
Expected: PASS (the flags were deleted in Task 1, so cobra reports `unknown flag`).

- [ ] **Step 3: Commit**

```bash
git add internal/cli/manager_test.go
git commit -m "test(ATM-0120): guard against re-adding the removed manager action flags"
```

---

### Task 4: Collapse "Your Roles" in the manager prompt template

**Files:**
- Modify: `internal/manager/context_v1.md:17-24`

**Interfaces:**
- Produces: a rendered prompt whose "Your Roles" section lists Curate, Recall, Onboarding. Task 5's test asserts these strings.

- [ ] **Step 1: Replace the "Your Roles" section**

In `internal/manager/context_v1.md`, replace lines 17-24 (the six bullets) with:

```
## Your Roles

- **Curate** — keep the ledger legible and current: review open backlog, triage unlabeled and under-described tasks, handle developing-agent handoffs, and maintain the project's shared vocabulary (recurring terms, short definitions, naming consistency) as you go. If you are not clear about what a Task should do, ask the user one by one to clarify.
- **Recall** — recall and link knowledge on request, grounded in cited IDs; you digest your own journal too, connecting related tasks and keeping them searchable. Read-only: synthesize and cite; do not mutate the ledger.
- **Onboarding** — when first introduced to a repo/project, learn it and organize it into a substrate a later agent can pick up.
```

Leave the "## Rules of Thumb" section (line 26 onward) and everything else unchanged.

- [ ] **Step 2: Update the context-test action catalog assertion**

In `internal/manager/context_test.go`, replace `TestRenderContextActionCatalogPresent` (lines 47-54):

```go
func TestRenderContextActionCatalogPresent(t *testing.T) {
	got := RenderContext(ContextData{Code: "ATM", Name: "ATM", ATMBin: "/bin/atm", Actor: "m"})
	for _, frag := range []string{"Curate", "Recall", "Onboarding"} {
		if !strings.Contains(got, frag) {
			t.Errorf("action catalog missing %q", frag)
		}
	}
	for _, old := range []string{"Tracking", "Asking", "Glossary", "Planning", "Grooming"} {
		if strings.Contains(got, old) {
			t.Errorf("rendered context still contains old role %q", old)
		}
	}
}
```

- [ ] **Step 3: Run the manager-package tests**

Run: `go test ./internal/manager/ -v`
Expected: PASS.

- [ ] **Step 4: Also run the manage-context CLI tests (they render the template)**

Run: `go test ./internal/cli/ -run 'TestManageContext' -v`
Expected: PASS. (`TestManageContextRendersPrompt` at `internal/cli/manager_test.go:143` asserts `{"ATM manager", "autonomous owner", "Tracking", "Asking", "Glossary", "Onboarding", "conventions"}` — update its `want` slice next.)

- [ ] **Step 5: Update TestManageContextRendersPrompt's want slice**

In `internal/cli/manager_test.go`, at `TestManageContextRendersPrompt` (around line 151), replace the `want` slice:

```go
	for _, want := range []string{"ATM manager", "autonomous owner", "Curate", "Recall", "Onboarding", "conventions"} {
```

And replace the `old` slice (around line 156):

```go
	for _, old := range []string{"Tracking", "Asking", "Glossary", "Planning", "Grooming"} {
```

- [ ] **Step 6: Re-run both test packages**

Run: `go test ./internal/manager/ ./internal/cli/ -run 'TestRenderContext|TestManageContext' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/manager/context_v1.md internal/manager/context_test.go internal/cli/manager_test.go
git commit -m "refactor(ATM-0120): collapse manager prompt roles to Curate/Recall/Onboarding

Glossary folds into Curate as a posture clause; Recall's read-only write
contract is made explicit. Onboarding role entry unchanged."
```

---

### Task 5: Update the conventions guide and its goldens

**Files:**
- Modify: `internal/cli/conventions.go:76,80,148,150`
- Regenerate: `internal/cli/testdata/golden/conventions-text.json`, `internal/cli/testdata/golden/conventions-json.json`, `internal/cli/testdata/golden/determinism-conventions.json`

**Interfaces:**
- Produces: conventions text + JSON with `--recall` and the 3-flag list. Goldens regenerated via `-update`.

- [ ] **Step 1: Update the conventions text (lines 76 and 80)**

In `internal/cli/conventions.go`, line 76, replace:

```
10. To get a synthesized answer, run a manager asking session with ` + "`atm manage --project <CODE> --asking`" + `.
```

with:

```
10. To get a synthesized answer, run a manager recall session with ` + "`atm manage --project <CODE> --recall`" + `.
```

On line 80, replace the substring:

```
Manager work starts with ` + "`atm manage --project <CODE> --planning|--grooming|--tracking|--asking|--glossary|--onboarding`" + `.
```

with:

```
Manager work starts with ` + "`atm manage --project <CODE> --curate|--recall|--onboarding`" + `.
```

Leave the rest of line 80 unchanged.

- [ ] **Step 2: Update the conventions JSON struct (lines 148 and 150)**

In `internal/cli/conventions.go`, line 148, replace:

```go
			"to get a synthesized answer, run a manager asking session with atm manage --project <CODE> --asking",
```

with:

```go
			"to get a synthesized answer, run a manager recall session with atm manage --project <CODE> --recall",
```

On line 150, in the `day_to_day_development` string, replace the substring:

```
Manager work starts with atm manage --project <CODE> --planning|--grooming|--tracking|--asking|--glossary|--onboarding.
```

with:

```
Manager work starts with atm manage --project <CODE> --curate|--recall|--onboarding.
```

Leave the rest of that string unchanged.

- [ ] **Step 3: Regenerate the three conventions goldens**

Run:

```bash
go test ./internal/cli/ -run 'TestConventionsText|TestConventionsJSON|TestDeterminismByteIdentical' -update -v
```

Then verify the three goldens actually changed in the right spots:

```bash
git diff -- internal/cli/testdata/golden/conventions-text.json internal/cli/testdata/golden/conventions-json.json internal/cli/testdata/golden/determinism-conventions.json
```

Expected: each diff shows `--asking` → `--recall` (and "manager asking session" → "manager recall session" where the prose changed), and `--planning|--grooming|--tracking|--asking|--glossary|--onboarding` → `--curate|--recall|--onboarding`. No other lines change.

- [ ] **Step 4: Run the conventions + determinism tests without -update**

Run: `go test ./internal/cli/ -run 'TestConventions|TestDeterminism' -v`
Expected: PASS (goldens now match live output).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/conventions.go internal/cli/testdata/golden/conventions-text.json internal/cli/testdata/golden/conventions-json.json internal/cli/testdata/golden/determinism-conventions.json
git commit -m "docs(ATM-0120): conventions guide uses --curate/--recall/--onboarding"
```

---

### Task 6: Update README

**Files:**
- Modify: `README.md` (lines ~30-34, ~105, ~120)

- [ ] **Step 1: Replace the 5-line flag block (lines 30-34)**

Replace:

```
atm manage --project ATM --planning     # review open work, keep statuses honest
atm manage --project ATM --grooming     # prioritize and shape the backlog
atm manage --project ATM --tracking     # curate progress, decisions, handoffs
atm manage --project ATM --asking       # answer project questions from the ledger
atm manage --project ATM --glossary     # maintain shared project language
```

with:

```
atm manage --project ATM                # curate the backlog (default action)
atm manage --project ATM --recall       # answer project questions from the ledger (read-only)
atm manage --project ATM --onboarding   # learn a repo and organize it for later agents
```

- [ ] **Step 2: Update the persona example (line 105)**

Replace:

```
atm manage --project ATM --planning --persona manager
```

with:

```
atm manage --project ATM --curate --persona manager
```

- [ ] **Step 3: Update the override example (line 120)**

Replace:

```
atm manage --project ATM --agent claude --planning -- --dangerously-skip-permission
```

with:

```
atm manage --project ATM --agent claude --curate -- --dangerously-skip-permission
```

- [ ] **Step 4: Build (no README test, but confirm no other doc references the old flags)**

Run:

```bash
go build ./...
rg -n "manage --project .* --planning|manage --project .* --grooming|manage --project .* --tracking|manage --project .* --asking|manage --project .* --glossary" README.md docs/ 2>&1 || true
```

Expected: build succeeds; the `rg` finds nothing (the only remaining `--onboarding` references are intentional).

- [ ] **Step 5: Commit**

```bash
git add README.md
git commit -m "docs(ATM-0120): README uses --curate/--recall/--onboarding"
```

---

### Task 7: Full verify gate

- [ ] **Step 1: Full build + vet**

Run:

```bash
make build
go vet ./...
```

Expected: both succeed.

- [ ] **Step 2: Full test suite**

Run: `make test`
Expected: PASS (all packages).

- [ ] **Step 3: Full verify**

Run: `make verify`
Expected: PASS (build + test + scripts-test).

- [ ] **Step 4: Grep the whole repo for any straggler old-flag reference**

Run:

```bash
rg -n '"--planning"|"--grooming"|"--tracking"|"--glossary"|"--asking"|managerActionPlanning|managerActionGrooming|managerActionTracking|managerActionGlossary|managerActionAsking' --type go
rg -n 'manage --project .* --planning|manage --project .* --grooming|manage --project .* --tracking|manage --project .* --asking|manage --project .* --glossary' README.md internal/ docs/
```

Expected: both empty. (If anything appears, it's a missed caller — fix it and re-run the relevant task's tests.)

- [ ] **Step 5: Confirm onboarding surface is untouched**

Run:

```bash
rg -n 'managerActionOnboarding|--onboarding|ATM_ONBOARD|BuildArgvOnboard' internal/cli/manager.go internal/manager/context_v1.md
```

Expected: all hits are the unchanged onboarding surface (flag bound in `bindManagerActionFlags`, the `action == managerActionOnboarding` branch, `ATM_ONBOARD=1` in env, the "Onboarding" role entry). No diff against the pre-task baseline in these spots.

- [ ] **Step 6: No commit needed (verify gate produces no artifacts)**

If Steps 1-5 all pass, the implementation is complete. If any step failed, fix and re-run from the failing task.