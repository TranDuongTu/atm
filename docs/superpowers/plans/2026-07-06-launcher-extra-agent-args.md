# Launcher Extra Agent Args + Ollama Host Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users pass per-agent flags (e.g. `codex --yolo`, `claude --dangerously-skip-permission`) through ATM launchers via POSIX `--` passthrough and `ATM_<AGENT>_ARGS` env defaults, and add `ollama` as a supported host under `atm developing` and `atm manager` (parity with onboarding).

**Architecture:** Launcher packages keep their pure-data `BuildArgv()` / `BuildArgv(promptPath, title)` signatures. The CLI layer owns arg assembly via two shared helpers in `internal/cli/launcher_shared.go`: `agentEnvArgs(agent, integration)` reads `ATM_<AGENT>_ARGS` (with ollama-integration precedence) and `appendAgentArgs(base, envArgs, extraArgs)` concatenates base + env + `--` args (no dedup). Each agent subcommand captures post-`--` positionals via `cmd.Flags().Args()`. New `OllamaLauncher` types in `internal/developing` and `internal/manager` produce the interactive base argv `["ollama","launch","<integration>","--"]`; the CLI's ollama subcommands construct them directly (mirroring `internal/cli/onboarding.go`). Env sensitivity requires the golden harness to clear `ATM_*_ARGS` vars for test isolation.

**Tech Stack:** Go 1.22+, cobra/pflag (POSIX `--` handling), `strings.Fields` for env parsing, table-driven + golden tests via `make verify`.

## Global Constraints

- Go 1.22+; single binary `atm`.
- No emojis in code or commits.
- Keep the API surface stable and versioned; the `Launcher` interface and `BuildArgv()` signatures are unchanged.
- JSON output is deterministic (sorted keys, RFC3339 UTC); error envelope `{"error":{"code","message"}}`; exit codes 0 ok / 1 generic / 2 usage / 3 not-found / 5 integrity.
- Existing dry-run goldens must remain byte-identical (no `--`, no env ⇒ argv unchanged).
- Repo verify gate: `make verify` (= `make build && make test`).
- Spec: `docs/superpowers/specs/2026-07-06-launcher-extra-agent-args-design.md`.

## File Structure

- **Create** `internal/cli/launcher_shared_test.go` — unit tests for `agentEnvArgs` + `appendAgentArgs`.
- **Modify** `internal/cli/launcher_shared.go` — add `agentEnvArgs` + `appendAgentArgs`.
- **Modify** `internal/cli/harness_test.go` — clear `ATM_*_ARGS` env in `newGoldenHarness`/`newGoldenHarnessAt` for test isolation.
- **Modify** `internal/developing/launcher.go` — add `OllamaLauncher` (interactive form).
- **Modify** `internal/developing/launcher_test.go` — add `OllamaLauncher` unit tests.
- **Modify** `internal/manager/launcher.go` — add `OllamaLauncher` (interactive form).
- **Modify** `internal/manager/launcher_test.go` — add `OllamaLauncher` unit tests.
- **Modify** `internal/cli/developing.go` — add `--` passthrough + env to all agent subcommands; add `ollama` subcommand with `--integration`; `runDeveloping` uses `appendAgentArgs`.
- **Modify** `internal/cli/manager.go` — add `--` passthrough + env to all agent subcommands; add `ollama` subcommand with `--integration`; `runManager` uses `appendAgentArgs`.
- **Modify** `internal/cli/onboarding.go` — add `--` passthrough + env to both subcommands; `runOnboarding` uses `appendAgentArgs`.
- **Create** `internal/cli/testdata/golden/developing-dry-run-codex-extra.json`
- **Create** `internal/cli/testdata/golden/developing-dry-run-ollama.json`
- **Create** `internal/cli/testdata/golden/developing-dry-run-codex-env.json`
- **Create** `internal/cli/testdata/golden/manager-dry-run-claude-extra.json`
- **Create** `internal/cli/testdata/golden/manager-dry-run-ollama.json`
- **Create** `internal/cli/testdata/golden/onboarding-dry-run-opencode-extra.json`
- **Create** `internal/cli/testdata/golden/onboarding-dry-run-ollama-extra.json`
- **Modify** `internal/cli/developing_test.go`, `internal/cli/manager_test.go`, `internal/cli/onboarding_test.go` — new golden test funcs + normalizers.
- **Modify** `README.md` — document `--` + env + ollama subcommands for developing/manager/onboarding.
- **Modify** `internal/cli/conventions.go` — note extra-args mechanism in day-to-day text + structured output.

---

### Task 1: Shared env/args helpers + golden harness env isolation

**Files:**
- Modify: `internal/cli/launcher_shared.go`
- Create: `internal/cli/launcher_shared_test.go`
- Modify: `internal/cli/harness_test.go:36-56` (the `for _, k := range` env-clear loop in `newGoldenHarness`) and `harness_test.go:58-74` (`newGoldenHarnessAt`)

**Interfaces:**
- Produces: `agentEnvArgs(agent, integration string) []string` and `appendAgentArgs(base, envArgs, extraArgs []string) []string` in package `cli`. Consumed by Tasks 4, 5, 6.
- Produces: a golden harness that clears `ATM_<AGENT>_ARGS` vars; consumed (implicitly) by all golden tests so a developer's shell env cannot drift goldens.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/launcher_shared_test.go`:

```go
package cli

import (
	"os"
	"reflect"
	"testing"
)

func TestAgentEnvArgs_DirectHosts(t *testing.T) {
	t.Setenv("ATM_OPENCODE_ARGS", "--auto --foo bar")
	got := agentEnvArgs("opencode", "")
	want := []string{"--auto", "--foo", "bar"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("agentEnvArgs(opencode) = %v, want %v", got, want)
	}
}

func TestAgentEnvArgs_EmptyEnv(t *testing.T) {
	t.Setenv("ATM_CODEX_ARGS", "")
	if got := agentEnvArgs("codex", ""); got != nil {
		t.Errorf("agentEnvArgs(codex) with empty env = %v, want nil", got)
	}
}

func TestAgentEnvArgs_OllamaIntegrationPrecedence(t *testing.T) {
	t.Setenv("ATM_CODEX_ARGS", "--yolo")
	t.Setenv("ATM_OLLAMA_ARGS", "--generic")
	got := agentEnvArgs("ollama", "codex")
	want := []string{"--yolo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ollama integration precedence = %v, want %v", got, want)
	}
}

func TestAgentEnvArgs_OllamaFallbackToGeneric(t *testing.T) {
	t.Setenv("ATM_OLLAMA_ARGS", "--generic")
	if got := agentEnvArgs("ollama", "codex"); got != nil {
		// ATM_CODEX_ARGS unset => should NOT fall back; spec says integration
		// env wins, but when unset, generic ollama env is NOT used for v1
		// (only ATM_<INTEGRATION>_ARGS is consulted for ollama). Verify the
		// chosen behavior matches.
		_ = got
	}
}

func TestAgentEnvArgs_OllamaNoIntegration(t *testing.T) {
	t.Setenv("ATM_OLLAMA_ARGS", "--generic")
	got := agentEnvArgs("ollama", "")
	want := []string{"--generic"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ollama no integration = %v, want %v", got, want)
	}
}

func TestAppendAgentArgs_Order(t *testing.T) {
	base := []string{"codex"}
	env := []string{"--yolo"}
	extra := []string{"--auto"}
	got := appendAgentArgs(base, env, extra)
	want := []string{"codex", "--yolo", "--auto"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendAgentArgs order = %v, want %v", got, want)
	}
}

func TestAppendAgentArgs_NoDedup(t *testing.T) {
	base := []string{"codex"}
	env := []string{"--yolo"}
	extra := []string{"--yolo"}
	got := appendAgentArgs(base, env, extra)
	want := []string{"codex", "--yolo", "--yolo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendAgentArgs should not dedup = %v, want %v", got, want)
	}
}

func TestAppendAgentArgs_BothEmpty(t *testing.T) {
	base := []string{"codex"}
	got := appendAgentArgs(base, nil, nil)
	want := []string{"codex"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendAgentArgs both empty = %v, want %v", got, want)
	}
}

func TestAppendAgentArgs_DoesNotMutateBase(t *testing.T) {
	base := []string{"codex"}
	_ = appendAgentArgs(base, []string{"--x"}, []string{"--y"})
	if base[0] != "codex" || len(base) != 1 {
		t.Errorf("appendAgentArgs mutated base: %v", base)
	}
}
```

Note: the `TestAgentEnvArgs_OllamaFallbackToGeneric` test above is illustrative; delete it before running — it asserts the wrong thing. Keep only the five clear tests (DirectHosts, EmptyEnv, OllamaIntegrationPrecedence, OllamaNoIntegration, Order, NoDedup, BothEmpty, DoesNotMutateBase). Remove the `OllamaFallbackToGeneric` test entirely before Step 2.

**Decision baked into tests:** for ollama, when the integration env (`ATM_<INTEGRATION>_ARGS`) is unset, ATM falls back to `ATM_OLLAMA_ARGS`. `TestAgentEnvArgs_OllamaIntegrationPrecedence` proves integration wins; `TestAgentEnvArgs_OllamaNoIntegration` proves the generic fallback applies when no integration is supplied. This matches the spec's precedence rule.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestAgentEnvArgs|TestAppendAgentArgs' -v`
Expected: FAIL — `agentEnvArgs` and `appendAgentArgs` undefined (compile error).

- [ ] **Step 3: Write minimal implementation**

Add to the end of `internal/cli/launcher_shared.go` (the file already imports `os` and `strings`):

```go
// agentEnvArgs returns env-derived extra args for a host agent.
// For ollama hosts with an integration set, ATM_<INTEGRATION>_ARGS wins
// over the generic ATM_OLLAMA_ARGS. Parsed with strings.Fields (no quoting).
func agentEnvArgs(agent, integration string) []string {
	if agent == "ollama" && integration != "" {
		if v := os.Getenv("ATM_" + strings.ToUpper(integration) + "_ARGS"); v != "" {
			return strings.Fields(v)
		}
	}
	if v := os.Getenv("ATM_" + strings.ToUpper(agent) + "_ARGS"); v != "" {
		return strings.Fields(v)
	}
	return nil
}

// appendAgentArgs returns base + envArgs + extraArgs with no dedup.
// The host agent's flag parser resolves any conflicts.
func appendAgentArgs(base, envArgs, extraArgs []string) []string {
	out := make([]string, 0, len(base)+len(envArgs)+len(extraArgs))
	out = append(out, base...)
	out = append(out, envArgs...)
	out = append(out, extraArgs...)
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestAgentEnvArgs|TestAppendAgentArgs' -v`
Expected: PASS (all 7 tests).

- [ ] **Step 5: Isolate golden harness from `ATM_*_ARGS` env**

In `internal/cli/harness_test.go`, expand the env-clear loop in both `newGoldenHarness` (around line 38) and `newGoldenHarnessAt` (around line 60). Replace the existing loop:

```go
	for _, k := range []string{"ATM_ACTOR", "ATM_ROLE", "ATM_PROJECT", "ATM_BIN", "ATM_RUN_ID", "ATM_CONTEXT_FILE"} {
		t.Setenv(k, "")
	}
```

with (in BOTH functions):

```go
	for _, k := range []string{
		"ATM_ACTOR", "ATM_ROLE", "ATM_PROJECT", "ATM_BIN", "ATM_RUN_ID", "ATM_CONTEXT_FILE",
		"ATM_OPENCODE_ARGS", "ATM_CODEX_ARGS", "ATM_CLAUDE_ARGS", "ATM_OLLAMA_ARGS",
	} {
		t.Setenv(k, "")
	}
```

(The `strings` import is already present; `os` too. No new imports.)

- [ ] **Step 6: Run full cli test suite to confirm no regressions**

Run: `go test ./internal/cli/ -v`
Expected: PASS (all existing golden tests unchanged; the env-clear keeps them deterministic).

- [ ] **Step 7: Commit**

```bash
git add internal/cli/launcher_shared.go internal/cli/launcher_shared_test.go internal/cli/harness_test.go
git commit -m "Add shared agentEnvArgs + appendAgentArgs helpers; isolate golden harness from ATM_*_ARGS env"
```

---

### Task 2: OllamaLauncher in internal/developing

**Files:**
- Modify: `internal/developing/launcher.go`
- Modify: `internal/developing/launcher_test.go`

**Interfaces:**
- Produces: `developing.OllamaLauncher{Integration string}` implementing `developing.Launcher` with `BuildArgv() []string` returning `["ollama","launch","<Integration>","--"]`. Consumed by Task 4's CLI ollama subcommand.
- `LauncherFor("ollama")` stays returning ok=false (ollama is constructed directly by the CLI).

- [ ] **Step 1: Write the failing test**

Append to `internal/developing/launcher_test.go` (before the final `TestLauncherForUnknown`):

```go
func TestOllamaLauncherInteractiveArgv(t *testing.T) {
	l := OllamaLauncher{Integration: "codex"}
	if l.Name() != "ollama" {
		t.Errorf("Name = %q, want ollama", l.Name())
	}
	if l.NotFoundHint() != "https://ollama.com" {
		t.Errorf("NotFoundHint = %q, want https://ollama.com", l.NotFoundHint())
	}
	want := []string{"ollama", "launch", "codex", "--"}
	if got := l.BuildArgv(); !reflect.DeepEqual(got, want) {
		t.Errorf("OllamaLauncher BuildArgv = %v, want %v", got, want)
	}
}

func TestOllamaLauncherBuildArgvDoesNotMutate(t *testing.T) {
	l := OllamaLauncher{Integration: "opencode"}
	_ = l.BuildArgv()
	if l.Integration != "opencode" {
		t.Errorf("BuildArgv mutated Integration: %q", l.Integration)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/developing/ -run 'TestOllamaLauncher' -v`
Expected: FAIL — `OllamaLauncher` undefined.

- [ ] **Step 3: Write minimal implementation**

Add to the end of `internal/developing/launcher.go`:

```go
// OllamaLauncher execs `ollama launch <integration> --` for an interactive
// developing session. The `--` separator is ollama launch's documented
// passthrough; extra agent args append after it (on the integration side).
// ATM does not validate the integration name; unknown values fail at
// `ollama launch`'s door. Constructed directly by the CLI's ollama subcommand,
// mirroring internal/onboard. LauncherFor stays ok=false for "ollama" because
// the integration is not known at factory time.
type OllamaLauncher struct {
	Integration string
}

func (l OllamaLauncher) Name() string         { return "ollama" }
func (l OllamaLauncher) NotFoundHint() string { return "https://ollama.com" }
func (l OllamaLauncher) BuildArgv() []string {
	return []string{"ollama", "launch", l.Integration, "--"}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/developing/ -v`
Expected: PASS (all developing tests, including the unchanged `TestLauncherForUnknown` which still asserts `LauncherFor("ollama")` == ok=false).

- [ ] **Step 5: Commit**

```bash
git add internal/developing/launcher.go internal/developing/launcher_test.go
git commit -m "Add OllamaLauncher (interactive) to internal/developing"
```

---

### Task 3: OllamaLauncher in internal/manager

**Files:**
- Modify: `internal/manager/launcher.go`
- Modify: `internal/manager/launcher_test.go`

**Interfaces:**
- Produces: `manager.OllamaLauncher{Integration string}` mirroring Task 2. Consumed by Task 5.
- `LauncherFor("ollama")` stays ok=false.

- [ ] **Step 1: Write the failing test**

Append to `internal/manager/launcher_test.go` (before `TestLauncherForUnknown`):

```go
func TestOllamaLauncherInteractiveArgv(t *testing.T) {
	l := OllamaLauncher{Integration: "codex"}
	if l.Name() != "ollama" {
		t.Errorf("Name = %q, want ollama", l.Name())
	}
	if l.NotFoundHint() != "https://ollama.com" {
		t.Errorf("NotFoundHint = %q, want https://ollama.com", l.NotFoundHint())
	}
	want := []string{"ollama", "launch", "codex", "--"}
	if got := l.BuildArgv(); !reflect.DeepEqual(got, want) {
		t.Errorf("OllamaLauncher BuildArgv = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manager/ -run 'TestOllamaLauncher' -v`
Expected: FAIL — `OllamaLauncher` undefined.

- [ ] **Step 3: Write minimal implementation**

Add to the end of `internal/manager/launcher.go` (identical shape to developing's):

```go
// OllamaLauncher execs `ollama launch <integration> --` for an interactive
// manager session. Constructed directly by the CLI's ollama subcommand,
// mirroring internal/onboard. LauncherFor stays ok=false for "ollama".
type OllamaLauncher struct {
	Integration string
}

func (l OllamaLauncher) Name() string         { return "ollama" }
func (l OllamaLauncher) NotFoundHint() string { return "https://ollama.com" }
func (l OllamaLauncher) BuildArgv() []string {
	return []string{"ollama", "launch", l.Integration, "--"}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/manager/ -v`
Expected: PASS (all manager tests; `TestLauncherForUnknown` unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/manager/launcher.go internal/manager/launcher_test.go
git commit -m "Add OllamaLauncher (interactive) to internal/manager"
```

---

### Task 4: Wire `--` + env + ollama subcommand into `atm developing`

**Files:**
- Modify: `internal/cli/developing.go` (entire file)
- Modify: `internal/cli/developing_test.go` (add tests + normalizer ollama branch)
- Create: `internal/cli/testdata/golden/developing-dry-run-codex-extra.json`
- Create: `internal/cli/testdata/golden/developing-dry-run-ollama.json`
- Create: `internal/cli/testdata/golden/developing-dry-run-codex-env.json`

**Interfaces:**
- Consumes: `agentEnvArgs`, `appendAgentArgs` (Task 1); `developing.OllamaLauncher`, `developing.LauncherFor` (Task 2).
- Produces: `atm developing <agent> [-- <args>]` honoring `ATM_<AGENT>_ARGS`; new `atm developing ollama --integration <name> [-- <args>]`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/developing_test.go`:

```go
func TestDevelopingCodexExtraArgsDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("developing", "codex", "--project", "FOO", "--dry-run", "--", "--yolo", "--auto")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeDevelopingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "developing-dry-run-codex-extra", got)
}

func TestDevelopingOllamaDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("developing", "ollama", "--project", "FOO", "--integration", "codex", "--dry-run", "--", "--yolo")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeDevelopingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "developing-dry-run-ollama", got)
}

func TestDevelopingCodexEnvArgsDryRunJSON(t *testing.T) {
	t.Setenv("ATM_CODEX_ARGS", "--yolo")
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("developing", "codex", "--project", "FOO", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeDevelopingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "developing-dry-run-codex-env", got)
}

func TestDevelopingOllamaRequiresIntegration(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("developing", "ollama", "--project", "FOO", "--dry-run")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d (usage)", code, ExitUsage)
	}
}
```

Note: `normalizeDevelopingOutput` (existing) already normalizes run-id/ATM_BIN/store path; no change needed since the new goldens use the same shapes. The ollama golden uses agent "ollama" which the normalizer handles generically.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestDevelopingCodexExtraArgs|TestDevelopingOllama|TestDevelopingCodexEnv' -v`
Expected: FAIL — `unknown command "ollama" for "atm developing"` and goldens missing.

- [ ] **Step 3: Implement the developing CLI changes**

In `internal/cli/developing.go`:

3a. Add `ExtraArgs []string` and `Integration string` to the opts struct:

```go
type developingOpts struct {
	Project     string
	Actor       string
	Integration string
	DryRun      bool
	ExtraArgs   []string
}
```

3b. Change `newDevelopingCmd` to add both the existing agent subcommands AND a new ollama subcommand:

```go
func newDevelopingCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "developing",
		Short: "Launch an agent with ATM developing context",
	}
	cmd.AddCommand(newDevelopingPluginCmd(st))
	for _, name := range []string{"opencode", "codex", "claude"} {
		cmd.AddCommand(newDevelopingAgentCmd(st, name))
	}
	cmd.AddCommand(newDevelopingOllamaCmd(st))
	return cmd
}
```

3c. Update `newDevelopingAgentCmd` to set `cmd.Args = cobra.ArbitraryArgs` and capture post-`--` args. Replace the RunE body:

```go
func newDevelopingAgentCmd(st *cliState, agent string) *cobra.Command {
	var opts developingOpts
	cmd := &cobra.Command{
		Use:   agent,
		Short: "Launch " + agent + " with ATM developing context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, ok := developing.LauncherFor(agent)
			if !ok {
				return fmt.Errorf("%w: unknown developing agent %q", ErrUsage, agent)
			}
			opts.ExtraArgs = args
			opts.Actor = defaultDevelopingActor(l.Name(), st, opts.Actor)
			opts.Integration = ""
			return runDeveloping(st, l, agent, "", opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project to use as the work ledger")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id stamped into ATM commands (default <agent>-dev)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "render context + print argv/env; do not launch")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
```

3d. Add `newDevelopingOllamaCmd` (constructs `developing.OllamaLauncher` directly):

```go
func newDevelopingOllamaCmd(st *cliState) *cobra.Command {
	var opts developingOpts
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Launch ollama-backed agent with ATM developing context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ExtraArgs = args
			opts.Actor = defaultDevelopingActor("ollama", st, opts.Actor)
			l := developing.OllamaLauncher{Integration: opts.Integration}
			return runDeveloping(st, l, "ollama", opts.Integration, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project to use as the work ledger")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id stamped into ATM commands (default ollama-dev)")
	cmd.Flags().StringVar(&opts.Integration, "integration", "", "ollama integration name (e.g. opencode, codex, claude)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "render context + print argv/env; do not launch")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}
```

3e. Update `runDeveloping` signature to accept `agent` and `integration` for env lookup, and use `appendAgentArgs`. Replace the existing `runDeveloping`:

```go
func runDeveloping(st *cliState, l developing.Launcher, agent, integration string, opts developingOpts) error {
	s, err := st.openStore()
	if err != nil {
		return err
	}
	p, err := s.GetProject(opts.Project)
	if err != nil {
		return fmt.Errorf("%w: project %s not found; create it first:\n  atm project create --code %s --name \"...\"",
			ErrNotFound, opts.Project, opts.Project)
	}

	atmBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve atm binary: %w", err)
	}

	runID := newRunID(opts.Project)
	contextPath := filepath.Join(s.StorePath(), "developing", runID+".md")
	if err := os.MkdirAll(filepath.Dir(contextPath), 0o755); err != nil {
		return fmt.Errorf("create developing dir: %w", err)
	}

	rendered := developing.RenderContext(developing.ContextData{
		Code:      p.Code,
		Name:      p.Name,
		ATMBin:    atmBin,
		Actor:     opts.Actor,
		RunID:     runID,
		Timestamp: store.RFC3339UTC(time.Now().UTC()),
	})
	if err := os.WriteFile(contextPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write context file %s: %w", contextPath, err)
	}

	base := l.BuildArgv()
	envArgs := agentEnvArgs(agent, integration)
	argv := appendAgentArgs(base, envArgs, opts.ExtraArgs)
	envValues := developingEnvValues(opts.Project, atmBin, opts.Actor, runID, contextPath)
	env := assembleEnv(envValues)
	if err := emitLaunchHeader(st, "developing", opts.Project, runID, contextPath, l.Name(), argv, envValues); err != nil {
		return err
	}
	if opts.DryRun {
		return nil
	}

	exitCode, runErr := runChild(l.Name(), argv, env, l.NotFoundHint())
	if err := emitLaunchTail(st, "developing", opts.Project, runID, contextPath, l.Name(), exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}
```

(The `developingEnvValues` and `defaultDevelopingActor` helpers stay unchanged.)

- [ ] **Step 4: Generate the golden fixtures**

Run with `-update` to create the three new goldens:

```bash
go test ./internal/cli/ -run 'TestDevelopingCodexExtraArgsDryRunJSON|TestDevelopingOllamaDryRunJSON|TestDevelopingCodexEnvArgsDryRunJSON' -update
```

Then inspect the three created files:

`internal/cli/testdata/golden/developing-dry-run-codex-extra.json` should contain:
```json
{
  "agent": "codex",
  "argv": [
    "codex",
    "--yolo",
    "--auto"
  ],
  "context_path": "/STORE/developing/FOO-RUNID.md",
  "env": {
    "ATM_ACTOR": "codex-dev",
    "ATM_BIN": "/ATM_BIN",
    "ATM_CONTEXT_FILE": "/STORE/developing/FOO-RUNID.md",
    "ATM_PROJECT": "FOO",
    "ATM_ROLE": "developing",
    "ATM_RUN_ID": "FOO-RUNID"
  },
  "project": "FOO",
  "role": "developing",
  "run_id": "FOO-RUNID"
}
```

`internal/cli/testdata/golden/developing-dry-run-ollama.json` should contain:
```json
{
  "agent": "ollama",
  "argv": [
    "ollama",
    "launch",
    "codex",
    "--",
    "--yolo"
  ],
  "context_path": "/STORE/developing/FOO-RUNID.md",
  "env": {
    "ATM_ACTOR": "ollama-dev",
    "ATM_BIN": "/ATM_BIN",
    "ATM_CONTEXT_FILE": "/STORE/developing/FOO-RUNID.md",
    "ATM_PROJECT": "FOO",
    "ATM_ROLE": "developing",
    "ATM_RUN_ID": "FOO-RUNID"
  },
  "project": "FOO",
  "role": "developing",
  "run_id": "FOO-RUNID"
}
```

`internal/cli/testdata/golden/developing-dry-run-codex-env.json` should contain (env-set takes precedence over harness clear because `t.Setenv` is set after `newGoldenHarness`):

```json
{
  "agent": "codex",
  "argv": [
    "codex",
    "--yolo"
  ],
  "context_path": "/STORE/developing/FOO-RUNID.md",
  "env": {
    "ATM_ACTOR": "codex-dev",
    "ATM_BIN": "/ATM_BIN",
    "ATM_CONTEXT_FILE": "/STORE/developing/FOO-RUNID.md",
    "ATM_PROJECT": "FOO",
    "ATM_ROLE": "developing",
    "ATM_RUN_ID": "FOO-RUNID"
  },
  "project": "FOO",
  "role": "developing",
  "run_id": "FOO-RUNID"
}
```

**Important ordering note:** in `TestDevelopingCodexEnvArgsDryRunJSON`, call `t.Setenv("ATM_CODEX_ARGS", "--yolo")` BEFORE `newGoldenHarness(t)`. (`t.Setenv`/`t`-scoped env restore is LIFO and the harness's own `t.Setenv(k, "")` calls run later — `t.Setenv` does NOT permit setting the same key twice; the second call panics.) Therefore the harness must NOT clear `ATM_CODEX_ARGS` if the test intends to set it. Reconcile this with Task 1's harness clear: the harness clears `ATM_*_ARGS` to `""`, and a test that wants to SET one must NOT rely on the harness having cleared it afterward. Solution: in `TestDevelopingCodexEnvArgsDryRunJSON`, call `t.Setenv` AFTER `newGoldenHarness` is impossible because the harness already `t.Setenv`-ed the same key. So instead, the harness clear of `ATM_*_ARGS` must use `os.Setenv`-style via a recorded original-value approach, OR the test sets env before `newGoldenHarness` and the harness uses `t.Setenv` which will FAIL on the duplicate.

Resolution: do NOT add `ATM_*_ARGS` to the harness `t.Setenv` clear loop. Instead, in Task 1 Step 5, skip the harness clear for `ATM_*_ARGS`. Test isolation for the NON-env golden tests is still required (a dev shell with `ATM_CODEX_ARGS=--yolo` would break `TestDevelopingCodexDryRunJSON`). To isolate without blocking env-set tests: have each non-env golden test explicitly clear relevant `ATM_*_ARGS` at its start, OR keep the harness clear but have env-tests set the var before constructing the harness and use a harness clear that records/restores the pre-existing value rather than unconditional `""`.

**Chosen resolution (simplest):** In Task 1 Step 5, DO add `ATM_*_ARGS` to the harness clear loop (so non-env tests are isolated). For env-tests, set the env var AFTER the harness is constructed by using `os.Setenv` directly (not `t.Setenv`) and restore via `t.Cleanup`. Concretely, replace the `t.Setenv` in `TestDevelopingCodexEnvArgsDryRunJSON` with:

```go
	prev := os.Getenv("ATM_CODEX_ARGS")
	os.Setenv("ATM_CODEX_ARGS", "--yolo")
	t.Cleanup(func() { os.Setenv("ATM_CODEX_ARGS", prev) })
```

Place this AFTER `newGoldenHarness(t)` so the harness's `t.Setenv("")` already ran. Apply this same pattern to any env-test in Tasks 5/6. Update Task 1's Step 5 instruction to use this pattern in env-tests (it already does the harness clear, which is correct).

Apply that fix to `TestDevelopingCodexEnvArgsDryRunJSON` now (replace its `t.Setenv` line with the `os.Setenv` + cleanup block above), keeping the call after `newGoldenHarness(t)`.

- [ ] **Step 5: Run the new tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestDeveloping' -v`
Expected: PASS (all developing tests, old + new, incl. `TestDevelopingOllamaRequiresIntegration`).

- [ ] **Step 6: Run the full cli suite to confirm no regressions**

Run: `go test ./internal/cli/ -v`
Expected: PASS (existing `developing-dry-run-codex.json` etc. unchanged because no `--`/no env ⇒ argv unchanged).

- [ ] **Step 7: Update README developing section**

In `README.md`, replace the developing block (lines ~148-159) with:

````markdown
### Developing sessions

```
atm developing plugin install codex
atm developing codex --project <CODE>
atm developing ollama --project <CODE> --integration <name>
```

`atm developing <opencode|codex|claude> --project <CODE>` launches the
agent's normal interactive entrypoint with ATM environment variables and a
rendered context file. `atm developing ollama --project <CODE> --integration <name>`
launches an ollama-backed agent (the integration is one of ollama's supported
hosts: opencode, codex, claude, etc.). Bootstrap plugins are installed
explicitly with `atm developing plugin install`; the launcher never modifies
agent config silently.

To pass extra flags to the host agent (e.g. `codex --yolo`,
`claude --dangerously-skip-permission`), append them after `--`:

```
atm developing codex --project <CODE> -- --yolo
atm developing claude --project <CODE> -- --dangerously-skip-permission
atm developing ollama --project <CODE> --integration codex -- --yolo --auto
```

Default per-agent args can also be set via the `ATM_<AGENT>_ARGS` environment
variable (`ATM_OPENCODE_ARGS`, `ATM_CODEX_ARGS`, `ATM_CLAUDE_ARGS`,
`ATM_OLLAMA_ARGS`). For ollama hosts, `ATM_<INTEGRATION>_ARGS` (e.g.
`ATM_CODEX_ARGS`) takes precedence over `ATM_OLLAMA_ARGS`. Env args are
applied first; `--` args append after them with no dedup. Args are passed
through verbatim; ATM does not validate or sanitize them.
````

- [ ] **Step 8: Commit**

```bash
git add internal/cli/developing.go internal/cli/developing_test.go internal/cli/testdata/golden/developing-dry-run-codex-extra.json internal/cli/testdata/golden/developing-dry-run-ollama.json internal/cli/testdata/golden/developing-dry-run-codex-env.json README.md
git commit -m "Wire '--' passthrough + ATM_<AGENT>_ARGS + ollama host into atm developing"
```

---

### Task 5: Wire `--` + env + ollama subcommand into `atm manager`

**Files:**
- Modify: `internal/cli/manager.go`
- Modify: `internal/cli/manager_test.go`
- Create: `internal/cli/testdata/golden/manager-dry-run-claude-extra.json`
- Create: `internal/cli/testdata/golden/manager-dry-run-ollama.json`

**Interfaces:**
- Consumes: `agentEnvArgs`, `appendAgentArgs` (Task 1); `manager.OllamaLauncher`, `manager.LauncherFor` (Task 3).
- Produces: `atm manager <agent> [-- <args>]` honoring env; new `atm manager ollama --integration <name> [-- <args>]`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/manager_test.go`:

```go
func TestManagerClaudeExtraArgsDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("manager", "claude", "--project", "FOO", "--dry-run", "--", "--dangerously-skip-permission")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manager-dry-run-claude-extra", got)
}

func TestManagerOllamaDryRunJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("manager", "ollama", "--project", "FOO", "--integration", "opencode", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manager-dry-run-ollama", got)
}

func TestManagerOllamaRequiresIntegration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("manager", "ollama", "--project", "FOO", "--dry-run")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d (usage)", code, ExitUsage)
	}
}
```

Note the ollama golden tests set `HOME` to a temp dir to suppress the manager-plugin-missing warning noise (parallel to `TestManagerLaunchWarnsWhenPluginMissing`), keeping the golden output deterministic.

The existing `normalizeManagerOutput` (in `manager_test.go`) handles run-id/ATM_BIN/store path; verify it does not need an ollama-specific branch. If `normalizeManagerOutput` only regexes run-id and `ATM_BIN`, it works for ollama as-is.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestManagerClaudeExtraArgs|TestManagerOllama' -v`
Expected: FAIL — `unknown command "ollama"` and goldens missing.

- [ ] **Step 3: Implement the manager CLI changes**

Mirror Task 4 in `internal/cli/manager.go`:

3a. Extend `managerOpts`:

```go
type managerOpts struct {
	Project     string
	Actor       string
	Integration string
	DryRun      bool
	ExtraArgs   []string
}
```

3b. `newManagerCmd` adds the ollama subcommand:

```go
func newManagerCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manager",
		Short: "Launch an ATM manager session or render manager context",
	}
	cmd.AddCommand(newManagerPluginCmd(st))
	cmd.AddCommand(newManagerRenderContextCmd(st))
	for _, name := range []string{"opencode", "codex", "claude"} {
		cmd.AddCommand(newManagerAgentCmd(st, name))
	}
	cmd.AddCommand(newManagerOllamaCmd(st))
	return cmd
}
```

3c. `newManagerAgentCmd` captures `--` args and passes agent/integration (empty integration) to `runManager`:

```go
func newManagerAgentCmd(st *cliState, agent string) *cobra.Command {
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
			opts.Actor = defaultManagerActor(l.Name(), st, opts.Actor)
			opts.Integration = ""
			return runManager(st, l, agent, "", opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project the manager owns")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id stamped into ATM commands (default <agent>-manager)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "render context + print argv/env; do not launch")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
```

3d. Add `newManagerOllamaCmd`:

```go
func newManagerOllamaCmd(st *cliState) *cobra.Command {
	var opts managerOpts
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Launch ollama-backed agent with ATM manager context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ExtraArgs = args
			opts.Actor = defaultManagerActor("ollama", st, opts.Actor)
			l := manager.OllamaLauncher{Integration: opts.Integration}
			return runManager(st, l, "ollama", opts.Integration, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project the manager owns")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id stamped into ATM commands (default ollama-manager)")
	cmd.Flags().StringVar(&opts.Integration, "integration", "", "ollama integration name (e.g. opencode, codex, claude)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "render context + print argv/env; do not launch")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}
```

3e. Update `runManager` signature to `(st, l, agent, integration string, opts)` and use `appendAgentArgs`. Inside the existing `runManager`, locate the line `argv := l.BuildArgv()` and replace the argv assembly block:

```go
	base := l.BuildArgv()
	envArgs := agentEnvArgs(agent, integration)
	argv := appendAgentArgs(base, envArgs, opts.ExtraArgs)
	envValues := managerEnvValues(opts.Project, atmBin, opts.Actor, runID, contextPath)
	env := assembleEnv(envValues)
```

Leave the rest of `runManager` (project lookup, plugin-missing warning, context file render, header/tail emit, child exec) unchanged except the signature.

Full updated `runManager` header:

```go
func runManager(st *cliState, l manager.Launcher, agent, integration string, opts managerOpts) error {
```

- [ ] **Step 4: Generate the golden fixtures**

```bash
go test ./internal/cli/ -run 'TestManagerClaudeExtraArgsDryRunJSON|TestManagerOllamaDryRunJSON' -update
```

Inspect:

`internal/cli/testdata/golden/manager-dry-run-claude-extra.json`:
```json
{
  "agent": "claude",
  "argv": [
    "claude",
    "--dangerously-skip-permission"
  ],
  "context_path": "/STORE/manager/FOO-RUNID.md",
  "env": {
    "ATM_ACTOR": "claude-manager",
    "ATM_BIN": "/ATM_BIN",
    "ATM_CONTEXT_FILE": "/STORE/manager/FOO-RUNID.md",
    "ATM_PROJECT": "FOO",
    "ATM_ROLE": "manager",
    "ATM_RUN_ID": "FOO-RUNID"
  },
  "project": "FOO",
  "role": "manager",
  "run_id": "FOO-RUNID"
}
```

`internal/cli/testdata/golden/manager-dry-run-ollama.json`:
```json
{
  "agent": "ollama",
  "argv": [
    "ollama",
    "launch",
    "opencode",
    "--"
  ],
  "context_path": "/STORE/manager/FOO-RUNID.md",
  "env": {
    "ATM_ACTOR": "ollama-manager",
    "ATM_BIN": "/ATM_BIN",
    "ATM_CONTEXT_FILE": "/STORE/manager/FOO-RUNID.md",
    "ATM_PROJECT": "FOO",
    "ATM_ROLE": "manager",
    "ATM_RUN_ID": "FOO-RUNID"
  },
  "project": "FOO",
  "role": "manager",
  "run_id": "FOO-RUNID"
}
```

(The plugin-missing warning goes to stderr, not stdout, so it does not appear in the stdout golden.)

- [ ] **Step 5: Run the new tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestManager' -v`
Expected: PASS (all manager tests, incl. `TestManagerOllamaRequiresIntegration`).

- [ ] **Step 6: Run the full cli suite for regressions**

Run: `go test ./internal/cli/ -v`
Expected: PASS (existing `manager-dry-run-codex.json` unchanged).

- [ ] **Step 7: Add README manager section**

In `README.md`, after the developing section (and before "## Conventions"), insert:

````markdown
### Manager sessions

```
atm manager plugin install opencode
atm manager opencode --project <CODE>
atm manager ollama --project <CODE> --integration <name>
```

`atm manager <host> --project <CODE>` launches an interactive ATM-ledger-owner
session (see `docs/superpowers/specs/2026-07-06-atm-manager-subagent-design.md`).
`--integration <name>` is required for the `ollama` host. Extra agent args pass
through `--`, and `ATM_<AGENT>_ARGS` defaults apply the same way as
`atm developing` (see above).
````

- [ ] **Step 8: Commit**

```bash
git add internal/cli/manager.go internal/cli/manager_test.go internal/cli/testdata/golden/manager-dry-run-claude-extra.json internal/cli/testdata/golden/manager-dry-run-ollama.json README.md
git commit -m "Wire '--' passthrough + ATM_<AGENT>_ARGS + ollama host into atm manager"
```

---

### Task 6: Wire `--` + env into `atm onboarding`

**Files:**
- Modify: `internal/cli/onboarding.go`
- Modify: `internal/cli/onboarding_test.go`
- Create: `internal/cli/testdata/golden/onboarding-dry-run-opencode-extra.json`
- Create: `internal/cli/testdata/golden/onboarding-dry-run-ollama-extra.json`

**Interfaces:**
- Consumes: `agentEnvArgs`, `appendAgentArgs` (Task 1); existing `onboard.OpencodeLauncher` / `onboard.OllamaLauncher`.
- Produces: `atm onboarding opencode|ollama [-- <args>]` honoring `ATM_<AGENT>_ARGS`. For onboarding's ollama, integration is already required; env precedence is `ATM_<INTEGRATION>_ARGS` over `ATM_OLLAMA_ARGS`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/onboarding_test.go`:

```go
func TestOnboardingOpencodeExtraArgsDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("onboarding", "opencode", "--project", "FOO", "--dry-run", "--", "--foo")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeOnboardingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "onboarding-dry-run-opencode-extra", got)
}

func TestOnboardingOllamaExtraArgsDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("onboarding", "ollama", "--project", "FOO", "--integration", "codex", "--dry-run", "--", "--bar")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeOnboardingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "onboarding-dry-run-ollama-extra", got)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestOnboardingOpencodeExtraArgs|TestOnboardingOllamaExtraArgs' -v`
Expected: FAIL — `unknown flag: --foo` (pflag rejects flags after `--` when `cmd.Args` is not set to accept positionals) and goldens missing.

- [ ] **Step 3: Implement the onboarding CLI changes**

In `internal/cli/onboarding.go`:

3a. Add `ExtraArgs []string` to `onboardingOpts`:

```go
type onboardingOpts struct {
	Project       string
	Actor         string
	PromptVersion string
	DryRun        bool
	ExtraArgs     []string
}
```

3b. In `newOnboardingOpencodeCmd`, set `Args: cobra.ArbitraryArgs` and capture `--` args. Replace the RunE:

```go
func newOnboardingOpencodeCmd(st *cliState) *cobra.Command {
	var opts onboardingOpts
	cmd := &cobra.Command{
		Use:   "opencode",
		Short: "Onboard via opencode run --auto",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := onboard.OpencodeLauncher{}
			opts.ExtraArgs = args
			opts.Actor = defaultActor(l.Name(), st, opts.Actor)
			return runOnboarding(st, l, "opencode", "", opts)
		},
	}
	addOnboardingFlags(cmd, &opts)
	return cmd
}
```

3c. In `newOnboardingOllamaCmd`, set `Args: cobra.ArbitraryArgs` and pass integration to `runOnboarding`:

```go
func newOnboardingOllamaCmd(st *cliState) *cobra.Command {
	var opts onboardingOpts
	var integration string
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Onboard via ollama launch <integration> -- run --auto",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l := onboard.OllamaLauncher{Integration: integration}
			opts.ExtraArgs = args
			opts.Actor = defaultActor(l.Name(), st, opts.Actor)
			return runOnboarding(st, l, "ollama", integration, opts)
		},
	}
	addOnboardingFlags(cmd, &opts)
	cmd.Flags().StringVar(&integration, "integration", "", "ollama integration name (e.g. opencode, codex, claude)")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}
```

3d. Update `runOnboarding` signature to `(st, l, agent, integration string, opts)` and assemble argv with `appendAgentArgs`. The only change inside is the argv line (around the current line `argv := l.BuildArgv(promptPath, title)`). Replace it with:

```go
	base := l.BuildArgv(promptPath, title)
	envArgs := agentEnvArgs(agent, integration)
	argv := appendAgentArgs(base, envArgs, opts.ExtraArgs)
```

Full updated header:

```go
func runOnboarding(st *cliState, l onboard.Launcher, agent, integration string, opts onboardingOpts) error {
```

(The rest of `runOnboarding` is unchanged; `emitOnboardingHeader(... argv)` already receives the merged argv.)

- [ ] **Step 4: Generate the golden fixtures**

```bash
go test ./internal/cli/ -run 'TestOnboardingOpencodeExtraArgsDryRunJSON|TestOnboardingOllamaExtraArgsDryRunJSON' -update
```

Inspect:

`internal/cli/testdata/golden/onboarding-dry-run-opencode-extra.json`:
```json
{
  "agent": "opencode",
  "argv": [
    "opencode",
    "--auto",
    "--prompt",
    "Read the onboarding instructions in the file at /STORE/onboarding/FOO-RUNID.md and follow them exactly.",
    "--foo"
  ],
  "project": "FOO",
  "prompt_path": "/STORE/onboarding/FOO-RUNID.md",
  "prompt_version": "v1",
  "run_id": "FOO-RUNID"
}
```

`internal/cli/testdata/golden/onboarding-dry-run-ollama-extra.json`:
```json
{
  "agent": "ollama",
  "argv": [
    "ollama",
    "launch",
    "codex",
    "--",
    "--auto",
    "--prompt",
    "Read the onboarding instructions in the file at /STORE/onboarding/FOO-RUNID.md and follow them exactly.",
    "--bar"
  ],
  "project": "FOO",
  "prompt_path": "/STORE/onboarding/FOO-RUNID.md",
  "prompt_version": "v1",
  "run_id": "FOO-RUNID"
}
```

- [ ] **Step 5: Run the new tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestOnboarding' -v`
Expected: PASS (all onboarding tests, old + new; existing goldens unchanged because no `--`/no env ⇒ argv unchanged).

- [ ] **Step 6: Update README onboarding flags list**

In `README.md`, in the onboarding Flags list (around line 290), append two bullets after the `--integration` bullet:

```
- `-- <agent args...>` — everything after `--` is appended verbatim to the host agent's argv (e.g. `atm onboarding opencode --project FOO -- --yolo`).
- `ATM_<AGENT>_ARGS` (env) — default args applied on every launch; for ollama, `ATM_<INTEGRATION>_ARGS` takes precedence over `ATM_OLLAMA_ARGS`.
```

- [ ] **Step 7: Commit**

```bash
git add internal/cli/onboarding.go internal/cli/onboarding_test.go internal/cli/testdata/golden/onboarding-dry-run-opencode-extra.json internal/cli/testdata/golden/onboarding-dry-run-ollama-extra.json README.md
git commit -m "Wire '--' passthrough + ATM_<AGENT>_ARGS into atm onboarding"
```

---

### Task 7: Document extra-args mechanism in `atm conventions`

**Files:**
- Modify: `internal/cli/conventions.go` (the `conventionsText` const day-to-day paragraph + Notes; and `conventionsStructured()` `day_to_day_development` + a new note)
- Modify: `internal/cli/testdata/golden/conventions-text.json` and `conventions-json.json` (regenerate via `-update`)
- Modify: `internal/cli/conventions_test.go` if needed (no new test; existing goldens get updated)

**Interfaces:**
- Produces: documented `atm conventions` output mentioning the `--` + env mechanism (human-facing contract alongside README).

- [ ] **Step 1: Update `conventionsText`**

In `internal/cli/conventions.go`, find the day-to-day paragraph (around line 49):

```
For day-to-day development, start the agent through `atm developing <agent> --project <CODE>` after installing the ATM developing plugin. The command preserves the agent's normal workflow and adds ATM ledger context for the session.
```

Replace it with:

```
For day-to-day development, start the agent through `atm developing <agent> --project <CODE>` after installing the ATM developing plugin. The command preserves the agent's normal workflow and adds ATM ledger context for the session. To pass per-agent flags (e.g. `codex --yolo`, `claude --dangerously-skip-permission`), append them after `--` (e.g. `atm developing codex --project <CODE> -- --yolo`); default per-agent args can also be set via `ATM_<AGENT>_ARGS` (e.g. `ATM_CODEX_ARGS`), and `atm developing ollama --project <CODE> --integration <name>` launches an ollama-backed host.
```

In the Notes section (around line 70-71), add a new bullet after the Plugins/skills bullet:

```
- Extra agent args: pass host-agent flags after `--` (e.g. `atm developing codex --project <CODE> -- --yolo`); defaults may also be set via `ATM_<AGENT>_ARGS`. ATM passes args through verbatim and does not validate them.
```

- [ ] **Step 2: Update `conventionsStructured`**

In the same file, update the `day_to_day_development` value (around line 112) to:

```go
		"day_to_day_development": "Start the agent through atm developing <agent> --project <CODE> after installing the ATM developing plugin. The command preserves the agent's normal workflow and adds ATM ledger context for the session. To pass per-agent flags (e.g. codex --yolo, claude --dangerously-skip-permission), append them after -- (e.g. atm developing codex --project <CODE> -- --yolo); defaults may also be set via ATM_<AGENT>_ARGS, and atm developing ollama --project <CODE> --integration <name> launches an ollama-backed host.",
```

- [ ] **Step 3: Update the conventions goldens**

```bash
go test ./internal/cli/ -run 'TestConventions' -update
```

Inspect the diffed `internal/cli/testdata/golden/conventions-text.json` and `conventions-json.json` to confirm the new sentences appear and nothing else changed.

- [ ] **Step 4: Run conventions tests to verify**

Run: `go test ./internal/cli/ -run 'TestConventions' -v`
Expected: PASS.

- [ ] **Step 5: Run the full cli suite for regressions**

Run: `go test ./internal/cli/ -v`
Expected: PASS. (Only conventions goldens changed; all other goldens byte-identical.)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/conventions.go internal/cli/testdata/golden/conventions-text.json internal/cli/testdata/golden/conventions-json.json
git commit -m "Document launcher extra-args + ollama host in atm conventions"
```

---

### Task 8: Final verification gate

**Files:** none (verification only)

**Interfaces:** none.

- [ ] **Step 1: Run the full repo verify gate**

Run: `make verify`
Expected: PASS (`make build && make test` succeed). If the pre-existing `TestVersionText`/`TestVersionJSON` golden mismatch (ATM-0033, unrelated to this work) fails, confirm it fails the same way on the pre-feature tree and proceed — it is not caused by this change. Do not fix ATM-0033 here.

- [ ] **Step 2: Smoke-test the new commands manually (optional, no commit)**

```
make build
./bin/atm developing codex --project ATM --dry-run -- --yolo --auto
./bin/atm developing ollama --project ATM --integration codex --dry-run -- --yolo
./bin/atm manager claude --project ATM --dry-run -- --dangerously-skip-permission
./bin/atm manager ollama --project ATM --integration opencode --dry-run
./bin/atm onboarding opencode --project ATM --dry-run -- --foo
ATM_CODEX_ARGS=--yolo ./bin/atm developing codex --project ATM --dry-run
```

Confirm each dry-run's printed `argv` array matches the spec's expectations (env-then-`--`, no dedup, ollama integration on the correct side of `--`).

- [ ] **Step 3: Comment the ATM task with completion evidence**

```sh
/usr/local/bin/atm task comment add --task ATM-0034 --body "Implementation complete. make verify passes (modulo pre-existing ATM-0033 version-golden mismatch). New goldens: developing-dry-run-codex-extra, developing-dry-run-ollama, developing-dry-run-codex-env, manager-dry-run-claude-extra, manager-dry-run-ollama, onboarding-dry-run-opencode-extra, onboarding-dry-run-ollama-extra. Existing dry-run goldens byte-identical. Manual smoke dry-runs confirmed argv shape." --actor opencode-dev
```

- [ ] **Step 4: Done**

No commit in this task. The feature is complete when `make verify` passes.

---

## Self-Review

**1. Spec coverage:**
- Scope v1 `--` passthrough to developing/manager/onboarding → Tasks 4, 5, 6. ✓
- `ATM_<AGENT>_ARGS` env with `strings.Fields` → Task 1 (`agentEnvArgs`). ✓
- Env-first then `--`, no dedup → Task 1 (`appendAgentArgs`) + tests `TestAppendAgentArgs_Order`/`NoDedup`. ✓
- Ollama host under developing & manager with `--integration` → Tasks 2, 3, 4, 5. ✓
- Ollama-integration env precedence (`ATM_<INTEGRATION>_ARGS` over `ATM_OLLAMA_ARGS`) → Task 1 `TestAgentEnvArgs_OllamaIntegrationPrecedence`. ✓
- CLI owns assembly; launcher packages keep shape → Tasks 2, 3 add `BuildArgv()` only; Tasks 4, 5, 6 assemble in CLI. ✓
- Dry-run argv reflects merged args; no envelope schema change → goldens in Tasks 4, 5, 6 verify argv arrays. ✓
- Existing goldens byte-identical → confirmed by Tasks 4/5/6 Step 6 + harness env-isolation (Task 1 Step 5). ✓
- README + `atm conventions` docs → Tasks 4 (developing README), 5 (manager README), 6 (onboarding README), 7 (conventions). ✓
- Error handling: missing `--integration` → cobra required-flag → Tasks 4, 5 `TestDevelopingOllamaRequiresIntegration` / `TestManagerOllamaRequiresIntegration`. Unknown integration not validated (unchanged). ✓
- `make verify` gate → Task 8. ✓

**2. Placeholder scan:** No TBD/TODO/vague steps. Every code step shows the actual code; every golden step shows the expected JSON. The one illustrative-but-wrong test (`TestAgentEnvArgs_OllamaFallbackToGeneric`) is explicitly called out for deletion in Task 1 Step 1. ✓

**3. Type consistency:**
- `agentEnvArgs(agent, integration string) []string` — same in Tasks 1, 4, 5, 6. ✓
- `appendAgentArgs(base, envArgs, extraArgs []string) []string` — same everywhere. ✓
- `developing.OllamaLauncher{Integration string}` (Task 2) used in Task 4 as `developing.OllamaLauncher{Integration: opts.Integration}`. ✓
- `manager.OllamaLauncher{Integration string}` (Task 3) used in Task 5. ✓
- `runDeveloping(st, l, agent, integration string, opts)` (Task 4) and `runManager(st, l, agent, integration string, opts)` (Task 5) and `runOnboarding(st, l, agent, integration string, opts)` (Task 6) — consistent signatures. ✓
- `developingOpts`/`managerOpts`/`onboardingOpts` each gain `ExtraArgs []string` (+`Integration string` for developing/manager). ✓

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-06-launcher-extra-agent-args.md`.