# CLI User Surface Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace ATM's launcher-facing CLI with `atm`, `atm <agent> --project <CODE>`, and `atm manage <agent> --project <CODE> --<action>`, then rewrite README around those user actions.

**Architecture:** Keep the existing store/API commands intact, but remove the old `tui`, `developing`, and `manager` command trees from root registration. Reuse the current developing and manager launcher internals behind new root-level developer commands and the new `manage` tree. Add a launcher execution test seam so tests no longer depend on removed `--dry-run`.

**Tech Stack:** Go 1.22+, Cobra CLI, existing golden test harness, Bubble Tea TUI, markdown docs.

## Global Constraints

- This is a breaking CLI cleanup: no alias/deprecation period for `atm tui`, `atm developing`, or `atm manager`.
- `atm` with no args opens the TUI.
- Developer sessions are `atm codex|claude|opencode --project <CODE> [--persona <NAME>] [-- <agent args...>]` and `atm ollama --project <CODE> --integration <HOST> [--persona <NAME>] [-- <agent args...>]`.
- Manager sessions are `atm manage <agent> --project <CODE> --planning|--grooming|--tracking|--asking|--glossary|--onboarding [--persona <NAME>] [-- <agent args...>]`.
- Manager commands require exactly one action flag.
- User-facing launcher commands expose `--persona`; they do not expose `--actor` or `--dry-run`.
- Built-in persona defaults remain `developer` for developer sessions, `manager` for manager sessions, and `admin` for direct TUI/human CLI actions.
- README must be minimal and action-first; low-level task/label/store/persona/search/index commands are advanced/API surface discoverable through help.

---

## File Structure

- `internal/cli/root.go` — root command registration, root no-args TUI behavior, demoted actor flag helper, test seams for TUI/child launching.
- `internal/cli/tui.go` — delete after root handles no-args TUI directly.
- `internal/cli/developing.go` — remove `developing` command group; expose top-level developer agent commands; remove launcher `--actor`/`--dry-run`; use child-runner seam.
- `internal/cli/manager.go` — rename user-facing tree to `manage`; add manager action flags and persona support; remove launcher `--actor`/`--dry-run`; replace `manager render-context` with hidden `manage-context` for installed plugin bootstraps.
- `internal/manager/context.go` and `internal/manager/context_v1.md` — add action rendering and rename manager actions to Tracking, Asking, Glossary.
- `internal/cli/*_test.go` — update launcher tests for new command names and the child-runner/TUI seams; remove plugin/dry-run command tests for deleted user-facing trees.
- `internal/cli/testdata/golden/*.json` — update or delete launcher goldens affected by command removal and prompt text.
- `README.md` — rewrite around user actions first, reasons second.

---

### Task 1: Root No-Args TUI, Actor Flag Demotion, And Launcher Test Seams

**Files:**
- Modify: `internal/cli/root.go`
- Delete: `internal/cli/tui.go`
- Modify: `internal/cli/harness_test.go`
- Create: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: existing `tui.Run(storePath, actor string) error` and existing `runChild(name string, argv []string, env []string, notFoundHint string) (int, error)`.
- Produces:
  - `type childRunner func(name string, argv []string, env []string, notFoundHint string) (int, error)`
  - `type tuiRunner func(storePath, actor string) error`
  - `func bindActorFlag(cmd *cobra.Command, st *cliState)`
  - `func (s *cliState) launchTUI() error`
  - `func (s *cliState) runChild(...) (int, error)`
  - Root `RunE` opens the TUI when no subcommand is provided.

- [ ] **Step 1: Write failing root behavior tests**

Create `internal/cli/root_test.go`:

```go
package cli

import (
	"errors"
	"testing"
)

func TestRootNoArgsLaunchesTUI(t *testing.T) {
	h := newGoldenHarness(t)
	var gotStore, gotActor string
	h.st.runTUI = func(storePath, actor string) error {
		gotStore = storePath
		gotActor = actor
		return nil
	}

	_, _, code := h.run()
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if gotStore != h.store.StorePath() {
		t.Fatalf("tui store = %q, want %q", gotStore, h.store.StorePath())
	}
	if gotActor != "admin@tui:unset" {
		t.Fatalf("tui actor = %q, want admin@tui:unset", gotActor)
	}
}

func TestRootNoArgsLaunchesTUIWithEnvActor(t *testing.T) {
	h := newGoldenHarness(t)
	t.Setenv("ATM_ACTOR", "staff@tui:unset")
	var gotActor string
	h.st.runTUI = func(storePath, actor string) error {
		gotActor = actor
		return nil
	}

	_, _, code := h.run()
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if gotActor != "staff@tui:unset" {
		t.Fatalf("tui actor = %q, want staff@tui:unset", gotActor)
	}
}

func TestRootNoArgsTUIErrorPropagates(t *testing.T) {
	h := newGoldenHarness(t)
	h.st.runTUI = func(storePath, actor string) error {
		return errors.New("boom")
	}

	_, stderr, code := h.run()
	if code != ExitGeneric {
		t.Fatalf("exit = %d, want %d", code, ExitGeneric)
	}
	if stderr == "" {
		t.Fatalf("stderr empty, want error envelope")
	}
}

func TestTUICommandRemoved(t *testing.T) {
	h := newGoldenHarness(t)
	_, _, code := h.run("tui")
	if code == ExitSuccess {
		t.Fatalf("atm tui should be removed")
	}
}

func TestRootActorFlagNotGlobal(t *testing.T) {
	h := newGoldenHarness(t)
	_, _, code := h.run("--actor", "admin@cli:unset", "version")
	if code == ExitSuccess {
		t.Fatalf("root --actor should not be accepted as a global flag")
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/cli -run 'TestRootNoArgs|TestTUICommandRemoved|TestRootActorFlagNotGlobal' -count=1
```

Expected: FAIL because root has no `RunE`, `atm tui` is still registered, and `--actor` is still a global persistent flag.

- [ ] **Step 3: Implement root no-args TUI and demote root actor flag**

Modify `internal/cli/root.go`:

```go
import (
	"fmt"
	"io"
	"os"
	"strings"

	"atm/internal/store"
	"atm/internal/tui"
	"atm/internal/version"

	"github.com/spf13/cobra"
)

type childRunner func(name string, argv []string, env []string, notFoundHint string) (int, error)
type tuiRunner func(storePath, actor string) error
```

Extend `cliState`:

```go
type cliState struct {
	flags globalFlags
	out   io.Writer
	err   io.Writer

	runChildFn childRunner
	runTUI     tuiRunner
}
```

In `newRootCmdWithState`, add `Args`/`RunE`, remove the root actor persistent flag, remove `newDevelopingCmd`, `newManagerCmd`, and `newTUICmd` registration. In this task, only remove `newTUICmd(st)` and keep the old launcher registration until Task 2/3 adds the replacement commands:

```go
root := &cobra.Command{
	Use:           "atm",
	Short:         "Agent Tasks Management",
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return st.launchTUI()
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if v := os.Getenv("ATM_ACTOR"); v != "" && st.flags.actor == "" {
			st.flags.actor = v
		}
		if st.flags.output != "" && st.flags.output != outputJSON && st.flags.output != outputText {
			return fmt.Errorf("%w: --output must be json or text", ErrUsage)
		}
		if st.flags.output == "" {
			st.flags.output = outputText
		}
		return nil
	},
}
root.PersistentFlags().StringVar(&st.flags.store, "store", "", "path to the store directory (overrides ATM_HOME)")
root.PersistentFlags().StringVar(&st.flags.output, "output", "", "output format: json|text (default text)")
root.PersistentFlags().BoolVar(&st.flags.quiet, "quiet", false, "suppress non-essential stdout in text mode")
```

Add helpers to `root.go`:

```go
func bindActorFlag(cmd *cobra.Command, st *cliState) {
	cmd.PersistentFlags().StringVar(&st.flags.actor, "actor", "", "actor id (free-form; env ATM_ACTOR)")
}

func (s *cliState) launchTUI() error {
	root := store.ResolveStorePath(s.flags.store)
	actor := s.flags.actor
	if actor == "" {
		actor = "admin@tui:unset"
	} else if !strings.Contains(actor, "@") {
		actor += "@tui:unset"
	}
	run := s.runTUI
	if run == nil {
		run = tui.Run
	}
	return run(root, actor)
}

func (s *cliState) runChild(name string, argv []string, env []string, notFoundHint string) (int, error) {
	if s.runChildFn != nil {
		return s.runChildFn(name, argv, env, notFoundHint)
	}
	return runChild(name, argv, env, notFoundHint)
}
```

Add `bindActorFlag(cmd, st)` to mutating/API parent command constructors:

```go
func newProjectCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{Use: "project", Short: "Project commands"}
	bindActorFlag(cmd, st)
	...
}
```

Apply the same parent-level binding in:

- `internal/cli/project.go` inside `newProjectCmd`
- `internal/cli/label.go` inside `newLabelCmd`
- `internal/cli/task.go` inside `newTaskCmd`
- `internal/cli/persona.go` inside `newPersonaCmd`
- `internal/cli/vocabulary.go` inside `newVocabularyCmd`

Do not add actor binding to launch commands, `search`, `index`, `embed`, `store`, `activity`, `conventions`, `version`, or root.

- [ ] **Step 4: Delete old TUI command file**

Delete `internal/cli/tui.go`. Root now owns TUI launch behavior.

- [ ] **Step 5: Stabilize the test harness for command-local actor flags**

Modify `internal/cli/harness_test.go` in `(*goldenHarness).run`:

```go
func (h *goldenHarness) run(args ...string) (string, string, int) {
	h.reset()
	h.st.flags.actor = ""
	root := newRootCmdWithState(h.st)
	root.SilenceUsage = true
	root.SilenceErrors = true
	h.st.flags.store = h.store.StorePath()
	h.st.flags.output = h.output
	root.SetArgs(args)
	...
}
```

This prevents one command-local `--actor` parse from leaking into the next `h.run`.

- [ ] **Step 6: Run root and smoke CLI tests**

Run:

```bash
go test ./internal/cli -run 'TestRootNoArgs|TestTUICommandRemoved|TestRootActorFlagNotGlobal|TestProject|TestTask|TestLabel' -count=1
```

Expected: PASS after adding actor flags to the API parent command groups.

- [ ] **Step 7: Commit Task 1**

```bash
git add internal/cli/root.go internal/cli/harness_test.go internal/cli/root_test.go internal/cli/project.go internal/cli/label.go internal/cli/task.go internal/cli/persona.go internal/cli/vocabulary.go internal/cli/tui.go
git commit -m "cli: make bare atm launch the TUI"
```

---

### Task 2: Replace `atm developing` With Top-Level Developer Commands

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/developing.go`
- Modify: `internal/cli/developing_test.go`
- Modify/delete: `internal/cli/testdata/golden/developing-*.json`

**Interfaces:**
- Consumes: `cliState.runChild`, `developing.LauncherFor`, `developing.OllamaLauncher`, `developingEnvValues`.
- Produces:
  - `func newDeveloperAgentCmd(st *cliState, agent string) *cobra.Command`
  - `func newDeveloperOllamaCmd(st *cliState) *cobra.Command`
  - top-level `atm codex`, `atm claude`, `atm opencode`, `atm ollama`
  - no registered `atm developing`

- [ ] **Step 1: Replace developing launcher tests with new command tests**

Rewrite `internal/cli/developing_test.go` around the new commands. Keep helper functions such as `normalizeDevelopingOutput`.

Use this test helper at the top of the file:

```go
type capturedChild struct {
	name string
	argv []string
	env  []string
}

func captureChild(h *goldenHarness) *capturedChild {
	var c capturedChild
	h.st.runChildFn = func(name string, argv []string, env []string, notFoundHint string) (int, error) {
		c.name = name
		c.argv = append([]string(nil), argv...)
		c.env = append([]string(nil), env...)
		return 0, nil
	}
	return &c
}
```

Add/replace tests:

```go
func TestDeveloperCodexLaunchJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("codex", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if c.name != "codex" {
		t.Fatalf("child name = %q, want codex", c.name)
	}
	got := normalizeDevelopingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "developer-codex-launch", got)
}

func TestDeveloperCodexExtraArgs(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("codex", "--project", "FOO", "--", "--yolo", "--auto")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	want := []string{"codex", "--yolo", "--auto"}
	if !reflect.DeepEqual(c.argv, want) {
		t.Fatalf("argv = %v, want %v", c.argv, want)
	}
}

func TestDeveloperOllamaLaunch(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("ollama", "--project", "FOO", "--integration", "codex", "--", "--yolo")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	want := []string{"ollama", "launch", "codex", "--", "--yolo"}
	if !reflect.DeepEqual(c.argv, want) {
		t.Fatalf("argv = %v, want %v", c.argv, want)
	}
}

func TestDeveloperPersonaEnvAndActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("persona", "create", "--name", "staff", "--prompt", "high bar", "--actor", "admin@cli:unset")
	captureChild(h)
	h.reset()

	out, _, code := h.run("claude", "--project", "FOO", "--persona", "staff")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{
		`"ATM_PERSONA": "staff"`,
		`"ATM_AGENT": "claude"`,
		`"ATM_ACTOR": "staff@claude:unset"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("persona launch env missing %q:\n%s", want, out)
		}
	}
}

func TestDevelopingCommandRemoved(t *testing.T) {
	h := newGoldenHarness(t)
	_, _, code := h.run("developing", "codex", "--project", "FOO")
	if code == ExitSuccess {
		t.Fatalf("atm developing should be removed")
	}
}

func TestDeveloperLaunchRejectsDryRunAndActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	for _, args := range [][]string{
		{"codex", "--project", "FOO", "--dry-run"},
		{"codex", "--project", "FOO", "--actor", "developer@codex:unset"},
	} {
		_, _, code := h.run(args...)
		if code == ExitSuccess {
			t.Fatalf("%v should fail", args)
		}
	}
}
```

Add imports:

```go
import (
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/cli -run 'TestDeveloper|TestDevelopingCommandRemoved' -count=1
```

Expected: FAIL because top-level developer commands are not registered and `atm developing` still exists.

- [ ] **Step 3: Register top-level developer commands**

Modify `internal/cli/root.go` registration:

```go
for _, name := range []string{"opencode", "codex", "claude"} {
	root.AddCommand(newDeveloperAgentCmd(st, name))
}
root.AddCommand(newDeveloperOllamaCmd(st))
```

Remove:

```go
root.AddCommand(newDevelopingCmd(st))
```

- [ ] **Step 4: Replace developing command constructors**

Modify `internal/cli/developing.go`:

```go
type developingOpts struct {
	Project     string
	Integration string
	Persona     string
	ExtraArgs   []string
}
```

Delete `newDevelopingCmd`, `newDevelopingPluginCmd`, `newDevelopingPluginStatusCmd`, `newDevelopingPluginInstallCmd`, and `developingPluginAgents` from the registered surface. If plugin install helpers become unused, remove the unused imports (`fmt` may no longer be needed by this file).

Rename and simplify agent constructors:

```go
func newDeveloperAgentCmd(st *cliState, agent string) *cobra.Command {
	var opts developingOpts
	cmd := &cobra.Command{
		Use:   agent,
		Short: "Launch " + agent + " with ATM developer context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, ok := developing.LauncherFor(agent)
			if !ok {
				return fmt.Errorf("%w: unknown developer agent %q", ErrUsage, agent)
			}
			opts.ExtraArgs = args
			opts.Integration = ""
			return runDeveloping(st, l, agent, "", opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project to use as the work ledger")
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name; injects its prompt and defaults actor to <persona>@<agent>:unset")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newDeveloperOllamaCmd(st *cliState) *cobra.Command {
	var opts developingOpts
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Launch ollama-backed agent with ATM developer context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ExtraArgs = args
			l := developing.OllamaLauncher{Integration: opts.Integration}
			return runDeveloping(st, l, "ollama", opts.Integration, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project to use as the work ledger")
	cmd.Flags().StringVar(&opts.Integration, "integration", "", "ollama integration name (e.g. opencode, codex, claude)")
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name; injects its prompt and defaults actor to <persona>@<agent>:unset")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}
```

- [ ] **Step 5: Remove actor/dry-run logic from `runDeveloping`**

In `runDeveloping`, replace actor defaulting with:

```go
actor := effectivePersona + "@" + l.Name() + ":unset"
```

Use `actor` everywhere the function currently uses `opts.Actor`.

Remove:

```go
if opts.Actor == "" {
	opts.Actor = effectivePersona + "@" + l.Name() + ":unset"
}
...
if opts.DryRun {
	return nil
}
exitCode, runErr := runChild(...)
```

Replace child execution with the seam:

```go
exitCode, runErr := st.runChild(l.Name(), argv, env, l.NotFoundHint())
```

Keep `emitLaunchHeader` and `emitLaunchTail`.

- [ ] **Step 6: Update developing env call**

Change:

```go
envValues := developingEnvValues(opts.Project, atmBin, opts.Actor, runID, contextPath, l.Name(), opts.Persona)
```

to:

```go
envValues := developingEnvValues(opts.Project, atmBin, actor, runID, contextPath, l.Name(), effectivePersona)
```

This ensures default developer launches still set `ATM_PERSONA=developer`.

- [ ] **Step 7: Run developer tests and update goldens**

Run:

```bash
go test ./internal/cli -run 'TestDeveloper|TestDevelopingCommandRemoved' -count=1 -update
go test ./internal/cli -run 'TestDeveloper|TestDevelopingCommandRemoved' -count=1
```

Expected: PASS. The new `developer-codex-launch.json` golden is created; old developing dry-run goldens can be removed if no test references them.

- [ ] **Step 8: Commit Task 2**

```bash
git add internal/cli/root.go internal/cli/developing.go internal/cli/developing_test.go internal/cli/testdata/golden
git commit -m "cli: replace developing launcher commands"
```

---

### Task 3: Replace `atm manager` With `atm manage` Action Commands

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/manager.go`
- Modify: `internal/manager/context.go`
- Modify: `internal/manager/context_v1.md`
- Modify: `internal/cli/manager_test.go`
- Modify/delete: `internal/cli/testdata/golden/manager-*.json`

**Interfaces:**
- Consumes: `cliState.runChild`, `manager.LauncherFor`, `manager.OllamaLauncher`.
- Produces:
  - `type managerAction string`
  - `func newManageCmd(st *cliState) *cobra.Command`
  - `func validateManagerAction(opts managerOpts) (managerAction, error)`
  - `ATM_MANAGER_ACTION=<action>` in manager env
  - `<ACTION_BLOCK>` in manager context render
  - no registered `atm manager`

- [ ] **Step 1: Rewrite manager tests for `manage` and action flags**

Replace launch tests in `internal/cli/manager_test.go` with action-based tests:

```go
func TestManageCodexPlanningLaunchJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("manage", "codex", "--project", "FOO", "--planning")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if c.name != "codex" {
		t.Fatalf("child name = %q, want codex", c.name)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manage-codex-planning-launch", got)
}

func TestManageRequiresExactlyOneAction(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	for _, args := range [][]string{
		{"manage", "codex", "--project", "FOO"},
		{"manage", "codex", "--project", "FOO", "--planning", "--grooming"},
	} {
		_, _, code := h.run(args...)
		if code == ExitSuccess {
			t.Fatalf("%v should fail", args)
		}
	}
}

func TestManageRejectsDryRunAndActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	for _, args := range [][]string{
		{"manage", "codex", "--project", "FOO", "--planning", "--dry-run"},
		{"manage", "codex", "--project", "FOO", "--planning", "--actor", "manager@codex:unset"},
	} {
		_, _, code := h.run(args...)
		if code == ExitSuccess {
			t.Fatalf("%v should fail", args)
		}
	}
}

func TestManageOllamaOnboarding(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("manage", "ollama", "--project", "FOO", "--integration", "opencode", "--onboarding")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(strings.Join(c.argv, " "), "--auto --prompt") {
		t.Fatalf("onboarding argv = %v, want non-interactive prompt argv", c.argv)
	}
	if !strings.Contains(strings.Join(c.env, "\n"), "ATM_ONBOARD=1") {
		t.Fatalf("onboarding env missing ATM_ONBOARD=1:\n%s", strings.Join(c.env, "\n"))
	}
}

func TestManagePersonaEnvAndActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("persona", "create", "--name", "ops", "--prompt", "curate well", "--actor", "admin@cli:unset")
	captureChild(h)
	h.reset()

	out, _, code := h.run("manage", "claude", "--project", "FOO", "--planning", "--persona", "ops")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{
		`"ATM_PERSONA": "ops"`,
		`"ATM_MANAGER_ACTION": "planning"`,
		`"ATM_ACTOR": "ops@claude:unset"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("manager env missing %q:\n%s", want, out)
		}
	}
}

func TestManagerCommandRemoved(t *testing.T) {
	h := newGoldenHarness(t)
	_, _, code := h.run("manager", "codex", "--project", "FOO", "--planning")
	if code == ExitSuccess {
		t.Fatalf("atm manager should be removed")
	}
}
```

Update `TestManagerRenderContextTextHasPrompt` to use new words:

```go
for _, want := range []string{"ATM manager", "autonomous owner", "Tracking", "Asking", "Glossary", "Onboarding", "conventions"} {
	if !strings.Contains(got, want) {
		t.Errorf("render-context output missing %q", want)
	}
}
for _, old := range []string{"Tracking request", "Inquiry", "Vocabulary"} {
	if strings.Contains(got, old) {
		t.Errorf("render-context output still contains old term %q", old)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/cli -run 'TestManage|TestManagerCommandRemoved|TestManagerRenderContext' -count=1
```

Expected: FAIL because `manage` is not registered and prompt terms are old.

- [ ] **Step 3: Add manager action types and options**

Modify `internal/cli/manager.go`:

```go
type managerAction string

const (
	managerActionPlanning   managerAction = "planning"
	managerActionGrooming   managerAction = "grooming"
	managerActionTracking   managerAction = "tracking"
	managerActionAsking     managerAction = "asking"
	managerActionGlossary   managerAction = "glossary"
	managerActionOnboarding managerAction = "onboarding"
)

type managerOpts struct {
	Project     string
	Integration string
	Persona     string
	Planning    bool
	Grooming    bool
	Tracking    bool
	Asking      bool
	Glossary    bool
	Onboarding  bool
	ExtraArgs   []string
}
```

Add validation helper:

```go
func validateManagerAction(opts managerOpts) (managerAction, error) {
	selected := []managerAction{}
	if opts.Planning {
		selected = append(selected, managerActionPlanning)
	}
	if opts.Grooming {
		selected = append(selected, managerActionGrooming)
	}
	if opts.Tracking {
		selected = append(selected, managerActionTracking)
	}
	if opts.Asking {
		selected = append(selected, managerActionAsking)
	}
	if opts.Glossary {
		selected = append(selected, managerActionGlossary)
	}
	if opts.Onboarding {
		selected = append(selected, managerActionOnboarding)
	}
	if len(selected) != 1 {
		return "", fmt.Errorf("%w: choose exactly one manager action: --planning, --grooming, --tracking, --asking, --glossary, or --onboarding", ErrUsage)
	}
	return selected[0], nil
}
```

- [ ] **Step 4: Register `manage` and remove old manager root**

In `internal/cli/root.go`, replace:

```go
root.AddCommand(newManagerCmd(st))
```

with:

```go
root.AddCommand(newManageCmd(st))
```

In `internal/cli/manager.go`, rename `newManagerCmd` to:

```go
func newManageCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manage",
		Short: "Launch an ATM manager session",
	}
	for _, name := range []string{"opencode", "codex", "claude"} {
		cmd.AddCommand(newManageAgentCmd(st, name))
	}
	cmd.AddCommand(newManageOllamaCmd(st))
	return cmd
}
```

Do not register plugin or render-context under `manage`.

- [ ] **Step 5: Replace manager agent constructors**

Rename and simplify constructors:

```go
func bindManagerActionFlags(cmd *cobra.Command, opts *managerOpts) {
	cmd.Flags().BoolVar(&opts.Planning, "planning", false, "review backlog readiness, blocked work, and in-flight work")
	cmd.Flags().BoolVar(&opts.Grooming, "grooming", false, "prioritize and shape the backlog")
	cmd.Flags().BoolVar(&opts.Tracking, "tracking", false, "curate progress, decisions, questions, and handoffs")
	cmd.Flags().BoolVar(&opts.Asking, "asking", false, "answer project questions grounded in ledger IDs")
	cmd.Flags().BoolVar(&opts.Glossary, "glossary", false, "maintain shared project language")
	cmd.Flags().BoolVar(&opts.Onboarding, "onboarding", false, "learn a repo/project and organize it for later agents")
}

func newManageAgentCmd(st *cliState, agent string) *cobra.Command {
	var opts managerOpts
	cmd := &cobra.Command{
		Use:   agent,
		Short: "Launch " + agent + " with ATM manager context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, ok := manager.LauncherFor(agent)
			if !ok {
				return fmt.Errorf("%w: unknown manager agent %q", ErrUsage, agent)
			}
			opts.ExtraArgs = args
			opts.Integration = ""
			return runManager(st, l, agent, "", opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project the manager owns")
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name; defaults actor to <persona>@<agent>:unset")
	bindManagerActionFlags(cmd, &opts)
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newManageOllamaCmd(st *cliState) *cobra.Command {
	var opts managerOpts
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Launch ollama-backed agent with ATM manager context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ExtraArgs = args
			l := manager.OllamaLauncher{Integration: opts.Integration}
			return runManager(st, l, "ollama", opts.Integration, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project the manager owns")
	cmd.Flags().StringVar(&opts.Integration, "integration", "", "ollama integration name (e.g. opencode, codex, claude)")
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name; defaults actor to <persona>@ollama:unset")
	bindManagerActionFlags(cmd, &opts)
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}
```

- [ ] **Step 6: Update `runManager` for actions and persona**

At the top of `runManager`, after project load:

```go
action, err := validateManagerAction(opts)
if err != nil {
	return err
}
effectivePersona := opts.Persona
if effectivePersona == "" {
	effectivePersona = "manager"
}
mp, err := s.GetPersona(effectivePersona)
if err != nil {
	return err
}
actor := effectivePersona + "@" + l.Name() + ":unset"
```

Remove `defaultManagerActor`, `opts.Actor`, `opts.DryRun`, and hardcoded `s.GetPersona("manager")`.

When rendering context:

```go
rendered := manager.RenderContext(manager.ContextData{
	Code:               p.Code,
	Name:               p.Name,
	ATMBin:             atmBin,
	Actor:              actor,
	RunID:              runID,
	Timestamp:          store.RFC3339UTC(time.Now().UTC()),
	Persona:            effectivePersona,
	PersonaPrompt:      mp.Prompt,
	PersonaDescription: mp.Description,
	Action:             string(action),
})
```

Select argv:

```go
onboarding := action == managerActionOnboarding
if onboarding {
	base = l.BuildArgvOnboard(contextPath)
} else {
	base = l.BuildArgv()
}
```

Env:

```go
envValues := managerEnvValues(opts.Project, atmBin, actor, runID, contextPath, onboarding, effectivePersona, string(action))
```

Execute:

```go
exitCode, runErr := st.runChild(l.Name(), argv, env, l.NotFoundHint())
```

- [ ] **Step 7: Extend manager env values**

Change signature:

```go
func managerEnvValues(project, atmBin, actor, runID, contextPath string, onboard bool, persona string, action string) map[string]string
```

Add:

```go
"ATM_PERSONA":        persona,
"ATM_MANAGER_ACTION": action,
```

Keep `ATM_ONBOARD=1` only when `onboard` is true.

- [ ] **Step 8: Add action rendering to manager context**

Modify `internal/manager/context.go`:

```go
type ContextData struct {
	...
	Action string
}
```

Add helper:

```go
func actionBlock(action string) string {
	if action == "" {
		return ""
	}
	return fmt.Sprintf("## Current manager action\n\nFocus this session on **%s**. Use the matching responsibility below as the primary goal, while still preserving ledger correctness.\n", action)
}
```

Add replacer pair:

```go
"<ACTION_BLOCK>", actionBlock(data.Action),
```

Modify `internal/manager/context_v1.md` after `<PERSONA_BLOCK>`:

```md
<ACTION_BLOCK>
```

Rename the `## What you do` bullets to:

```md
- **Planning** — review your open backlog and keep statuses honest: what is ready, what needs more information, what is blocked (and by what), what is in flight.
- **Grooming** — prioritize the backlog so the most important work surfaces first.
- **Tracking** — a developing agent hands you progress, decisions, questions, or friction mid-work; find the task it extends and curate it (comment, or create/split/label as the work demands).
- **Asking** — recall and link knowledge on request, grounded in cited IDs; you digest your own journal too, connecting related tasks and keeping them searchable.
- **Glossary** — maintain the project's shared language: recurring domain terms, short definitions, and naming consistency across tasks, comments, labels, and docs.
- **Onboarding** — when first introduced to a repo/project, learn it and organize it into a substrate a later agent can pick up.
```

- [ ] **Step 9: Add hidden `manage-context` for installed manager plugins**

The spec removes `atm manager`, so `atm manager render-context` cannot remain. The installed manager plugin currently calls `atm manager render-context`, so replace it with a hidden root command that keeps plugin dispatch working without adding user-facing help surface.

Rename `newManagerRenderContextCmd` to `newManageContextCmd`, keep the existing render logic, and register it at root:

```go
root.AddCommand(newManageContextCmd(st))
```

Use hidden command metadata:

```go
cmd := &cobra.Command{
	Use:    "manage-context",
	Short:  "Print the ATM manager system prompt to stdout",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		data := manager.ContextData{
			Code:  opts.Project,
			Actor: opts.Actor,
		}
		if opts.Project != "" {
			data.ATMBin = atmBinPath()
			data.Name = opts.Project
			if s, err := st.openStore(); err == nil {
				if p, err := s.GetProject(opts.Project); err == nil {
					data.Name = p.Name
				}
			}
		}
		rendered := manager.RenderContext(data)
		return st.emit(st.stdout(), map[string]any{"context": rendered}, func() {
			fmt.Fprint(st.stdout(), rendered)
		})
	},
}
cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project code (optional; when absent, placeholders are left for env-driven use)")
cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id (optional)")
```

Add tests:

```go
func TestManageContextHiddenFromRootHelp(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	out, _, code := h.run("--help")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if strings.Contains(out, "manage-context") {
		t.Fatalf("manage-context should be hidden from root help:\n%s", out)
	}
}

func TestManageContextRendersPrompt(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	_, _, code := h.run("manage-context", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(h.stdout.String(), "ATM manager") {
		t.Fatalf("manage-context did not render manager prompt:\n%s", h.stdout.String())
	}
}
```

- [ ] **Step 10: Run manager tests and update goldens**

Run:

```bash
go test ./internal/cli -run 'TestManage|TestManagerCommandRemoved|TestManagerRenderContext|TestManagerOnboard' -count=1 -update
go test ./internal/cli -run 'TestManage|TestManagerCommandRemoved|TestManagerRenderContext|TestManagerOnboard' -count=1
```

Expected: PASS after old manager tests are removed or updated. Old manager dry-run goldens can be removed if no test references them.

- [ ] **Step 11: Commit Task 3**

```bash
git add internal/cli/root.go internal/cli/manager.go internal/manager/context.go internal/manager/context_v1.md internal/cli/manager_test.go internal/cli/testdata/golden
git commit -m "cli: replace manager launcher with manage actions"
```

---

### Task 4: Rewrite README And User-Facing Help Text

**Files:**
- Modify: `README.md`
- Modify: `internal/cli/conventions.go`
- Modify: `internal/cli/conventions_test.go`
- Modify: `internal/cli/testdata/golden/conventions-*.json`

**Interfaces:**
- Consumes: finalized user-facing commands from Tasks 1-3.
- Produces: action-first README; conventions text references new commands and new manager terms.

- [ ] **Step 1: Replace README with minimal action-first content**

Overwrite `README.md` with:

```md
# ATM — Agent Tasks Management

ATM is an append-only task ledger for people who work through coding agents.

## User Actions

Open the TUI:

```sh
atm
```

Start a developer agent on a project:

```sh
atm codex --project ATM
atm claude --project ATM
atm opencode --project ATM
atm ollama --project ATM --integration codex
```

Start a manager session for a specific management action:

```sh
atm manage codex --project ATM --planning
atm manage codex --project ATM --grooming
atm manage codex --project ATM --tracking
atm manage codex --project ATM --asking
atm manage codex --project ATM --glossary
atm manage codex --project ATM --onboarding
```

All agent launchers accept `--persona <name>` and pass host-agent arguments after `--`:

```sh
atm codex --project ATM --persona developer -- --yolo
atm manage claude --project ATM --planning --persona manager -- --dangerously-skip-permission
```

## Manager Actions

- `--planning` reviews open work and keeps statuses honest.
- `--grooming` prioritizes and shapes the backlog.
- `--tracking` curates progress, decisions, questions, and handoffs.
- `--asking` answers project questions from the ledger with cited task/comment IDs.
- `--glossary` maintains shared project language.
- `--onboarding` learns a repo/project and organizes it for later agents.

## Why I Built ATM

I work across multiple projects at once, and some projects span multiple repositories.

I use multiple coding agents and switch between them regularly to manage cost, context, and token usage.

I need to resume or hand off work across agents with minimal guidance.

I switch machines frequently, so I need a centralized, immutable, append-only ledger that can be shared.

I do not want a traditional Jira-style ticket system built around human browsing workflows. I want to ask my agents and have them work from the ledger.

## Store

ATM stores plain files under `ATM_HOME`, or `~/.config/atm` by default. A project is not the same thing as a repository; one project can cover multiple repos.

## Build And Verify

```sh
make build
make test
make verify
```

## Advanced/API Surface

The lower-level task, label, project, store, search, index, persona, and activity commands remain available for agents and scripts. Discover them with:

```sh
atm help
atm conventions
```
```

- [ ] **Step 2: Update conventions command text**

Modify `internal/cli/conventions.go`:

- Replace `atm developing <agent> --project <CODE> --persona <name>` with `atm <agent> --project <CODE> --persona <name>`.
- Replace day-to-day development text with `atm <agent> --project <CODE>`.
- Replace manager/inquiry/vocabulary language with `manage`, `asking`, and `glossary`.
- Remove text that says explicit `--actor` wins for launchers.

Use this day-to-day sentence:

```go
For day-to-day development, start the agent through ` + "`atm <agent> --project <CODE>`" + `. To pass per-agent flags, append them after ` + "`--`" + ` (e.g. ` + "`atm codex --project ATM -- --yolo`" + `). Manager work starts with ` + "`atm manage <agent> --project <CODE> --planning|--grooming|--tracking|--asking|--glossary|--onboarding`" + `.
```

- [ ] **Step 3: Run conventions tests and update goldens**

Run:

```bash
go test ./internal/cli -run 'TestConventions' -count=1 -update
go test ./internal/cli -run 'TestConventions' -count=1
```

Expected: PASS. Inspect the golden diff manually and verify no old `atm developing`, `atm manager`, `Tracking request`, `Inquiry`, or `Vocabulary` wording remains except where describing advanced internals.

- [ ] **Step 4: Commit Task 4**

```bash
git add README.md internal/cli/conventions.go internal/cli/conventions_test.go internal/cli/testdata/golden
git commit -m "docs: focus README on user actions"
```

---

### Task 5: Full Cleanup, Compatibility Removal, And Verification

**Files:**
- Modify: `internal/cli/*_test.go`
- Modify: `internal/manager/plugin_assets/*/atm-manager.md`
- Modify: `internal/manager/plugins_test.go`
- Modify: `internal/developing/plugins_test.go`

**Interfaces:**
- Consumes: all prior tasks.
- Produces: no stale references in tests/docs/plugin assets; full verification green.

- [ ] **Step 1: Search for stale user-facing commands and old manager terms**

Run:

```bash
rg -n "atm tui|atm developing|atm manager|Tracking request|Inquiry|Vocabulary|--dry-run|--actor" README.md internal docs/superpowers/specs/2026-07-11-cli-user-surface-simplification-design.md internal/cli internal/manager internal/developing -g '*.go' -g '*.md' -g '*.json'
```

Expected: remaining matches should be one of:

- historical design text in older specs/plans
- low-level API tests that intentionally use `--actor`
- plugin install internals if still intentionally supported

No matches should remain in README, current manager prompt, new launcher help, or new launcher tests.

- [ ] **Step 2: Update plugin asset references**

Update each `internal/manager/plugin_assets/*/atm-manager.md` file so the bootstrap calls the hidden replacement command:

```sh
atm manage-context --project "$ATM_PROJECT" --actor "$ATM_ACTOR"
```

Update manager plugin tests so expected installed bytes reference `manage-context`, not `manager render-context`.

- [ ] **Step 3: Run full CLI tests**

Run:

```bash
go test ./internal/cli/... -count=1
```

Expected: PASS.

- [ ] **Step 4: Run full verification**

Run:

```bash
make verify
```

Expected: PASS.

- [ ] **Step 5: Inspect command help manually**

Run:

```bash
go run ./cmd/atm --help
go run ./cmd/atm codex --help
go run ./cmd/atm manage codex --help
```

Expected:

- root help shows `codex`, `claude`, `opencode`, `ollama`, and `manage`
- root help does not show `tui`, `developing`, or `manager`
- `codex --help` shows `--project`, `--persona`, and global `--store`, `--output`, `--quiet`
- `codex --help` does not show `--actor` or `--dry-run`
- `manage codex --help` shows `--project`, `--persona`, and the six action flags
- `manage codex --help` does not show `--actor` or `--dry-run`

- [ ] **Step 6: Record ATM progress through manager**

Ask the ATM manager subagent to add a progress comment to `ATM-0084` summarizing:

```text
Implemented CLI hard replacement: bare atm opens TUI; developer sessions use atm <agent>; manager sessions use atm manage <agent> with explicit planning/grooming/tracking/asking/glossary/onboarding actions; README now leads with user actions.
```

- [ ] **Step 7: Commit final cleanup**

Commit any files changed by Steps 1-6:

```bash
git add README.md internal docs/superpowers/plans/2026-07-11-cli-user-surface-simplification.md
git commit -m "test: verify CLI user surface simplification"
```

---

## Self-Review

**Spec coverage:** The plan covers bare `atm` TUI behavior (Task 1), top-level developer launchers (Task 2), `manage` action launchers and manager prompt renames (Task 3), README action-first rewrite (Task 4), and hard removal/verification (Task 5). Lower-level commands remain intact except for demoting `--actor` from root to API command groups so launchers cannot accept it.

**Placeholder scan:** No placeholder work remains. Plugin compatibility is explicit: Task 3 adds hidden `atm manage-context`, and Task 5 updates manager plugin assets to call it.

**Type consistency:** `managerAction`, `managerOpts`, `bindManagerActionFlags`, `validateManagerAction`, `cliState.runChildFn`, `cliState.runTUI`, `bindActorFlag`, and `launchTUI` are named consistently across tasks.
