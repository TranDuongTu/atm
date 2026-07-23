# TUI Agent Dispatch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Dispatch manager/developer agent sessions from the TUI into a detected terminal surface (herdr → tmux → terminal tab), hand developer sessions a task via `--task`/`ATM_TASK`, and add a read-only personas overlay.

**Architecture:** A new near-leaf `internal/dispatch` package detects the surface and spawns `atm --persona … --agent … [--task …]`; the TUI gets two thin dispatch dialogs (dedicated overlay sub-models following the `capabilityModel` pattern, NOT `form.go`) and a personas overlay; the session launcher grows a `--task` flag that rides the rendered context file.

**Tech Stack:** Go, bubbletea/lipgloss (existing TUI), cobra (existing CLI). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-07-23-tui-agent-dispatch-design.md` · **Task:** ATM-4b7e24

## Global Constraints

- Detection precedence is exactly **herdr → tmux → terminal**; config `terminal_cmd` overrides only the terminal step.
- Fire-and-forget: no session registry, no tracking state anywhere.
- Dialog/window/pane/tab title format: `<CODE> · <persona>[ · <task-id>]` (separator is ` · `, U+00B7 middot with spaces).
- `internal/dispatch` may import only stdlib (keeps it usable from tui without violating `tests/arch/imports_test.go` rules; `internal/tui` must never import `atm/internal/store`).
- `internal/core` and `skills` are pure leaves — do not add imports to them.
- Every ATM ledger mutation is stamped actor `developer@claude:<your-model>`.
- Commit messages follow repo convention: `<type>(ATM-4b7e24): <summary>`.
- Run `go build ./...` before every commit; a task is not done with a broken build.

---

### Task 1: Context render — assigned-task block

**Files:**
- Modify: `internal/session/context.go`
- Modify: `internal/session/context_v1.md`
- Test: `internal/session/context_test.go`

**Interfaces:**
- Produces: `session.ContextData.Task string` field — set it and `RenderContext` emits an `## Assigned task` block; empty means no block. Task 2 consumes this.

- [ ] **Step 1: Write the failing test**

Append to `internal/session/context_test.go` (match the file's existing imports; it already imports `strings`, `testing`, and `atm/skills` — add any missing):

```go
// TestRenderContextTaskBlock verifies ContextData.Task renders an assigned-task
// block naming the task, and that an empty Task renders neither the block nor
// a literal placeholder.
func TestRenderContextTaskBlock(t *testing.T) {
	spec, ok := skills.Persona("developer")
	if !ok {
		t.Fatal("built-in developer persona missing")
	}
	out := RenderContext(ContextData{
		Code: "ATM", Name: "Agent Tasks Management",
		Actor: "developer@claude:unset", Spec: spec, Task: "ATM-4b7e24",
	})
	if !strings.Contains(out, "## Assigned task") {
		t.Fatalf("missing assigned-task block:\n%s", out)
	}
	if !strings.Contains(out, "`ATM-4b7e24`") || !strings.Contains(out, "atm task show ATM-4b7e24") {
		t.Fatalf("task block must name the task and the show command:\n%s", out)
	}
	if strings.Contains(out, "<TASK_BLOCK>") {
		t.Fatalf("literal placeholder leaked:\n%s", out)
	}

	out = RenderContext(ContextData{Code: "ATM", Name: "x", Actor: "a", Spec: spec})
	if strings.Contains(out, "## Assigned task") || strings.Contains(out, "<TASK_BLOCK>") {
		t.Fatalf("no-task render must omit block and placeholder:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestRenderContextTaskBlock -v`
Expected: FAIL — `unknown field Task in struct literal` (compile error).

- [ ] **Step 3: Implement**

In `internal/session/context.go`:

1. Add to `ContextData` (after `Capability`):

```go
	// Task assigns the session a single task; "" renders no assignment block.
	Task string
```

2. In `RenderContext`, after the `modeBlock` computation, add:

```go
	taskBlock := ""
	if d.Task != "" {
		taskBlock = fmt.Sprintf("## Assigned task\n\nThis session is assigned task `%s`. Read it first (`atm task show %s`), record your intent as a task comment before any design or code work, and keep every change in this session serving it.\n", d.Task, d.Task)
	}
```

3. Add the pair `"<TASK_BLOCK>", taskBlock,` to the `pairs` slice (after `"<MODE_BLOCK>", modeBlock,`), and extend the keep-placeholder exception so an empty task block renders empty rather than literal:

```go
		if val == "" && key != "<PERSONA_BLOCK>" && key != "<MODE_BLOCK>" && key != "<TASK_BLOCK>" {
```

4. In `internal/session/context_v1.md`, add `<TASK_BLOCK>` on its own line directly under `<MODE_BLOCK>`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/session/ -v`
Expected: all PASS (including pre-existing render tests — the empty-task path must not change existing golden output beyond a possible blank line; if an existing exact-match test fails on the added blank line, update that test's expected text to include it).

- [ ] **Step 5: Commit**

```bash
git add internal/session/context.go internal/session/context_v1.md internal/session/context_test.go
git commit -m "feat(ATM-4b7e24): assigned-task block in session context render"
```

---

### Task 2: CLI `--task` flag, validation, cache key, `ATM_TASK`

**Files:**
- Modify: `internal/cli/root.go` (flag), `internal/cli/session.go` (sessionOpts, launchSession, sessionEnvValues, session-context cmd), `internal/cli/launcher_shared.go` (cacheKey/contextCachePath)
- Test: `internal/cli/launcher_shared_test.go`, `internal/cli/session_test.go`

**Interfaces:**
- Consumes: `session.ContextData.Task` (Task 1).
- Produces: `atm --persona <p> --project <CODE> --agent <a> --task <id>` CLI surface — Task 6's dialogs spawn exactly this argv. Env var `ATM_TASK=<id>`. `cacheKey(persona, mode, capability, task string)` and `contextCachePath(storePath, code, persona, mode, capability, task string)` new signatures.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/launcher_shared_test.go`:

```go
// TestCacheKeyWithTask verifies the task id joins the cache key so two
// concurrent sessions on different tasks never share a context file.
func TestCacheKeyWithTask(t *testing.T) {
	if got, want := cacheKey("developer", "", "", "ATM-4b7e24"), "session-developer-atm-4b7e24"; got != want {
		t.Fatalf("cacheKey = %q, want %q", got, want)
	}
	if got, want := cacheKey("developer", "", "", ""), "session-developer"; got != want {
		t.Fatalf("cacheKey no-task = %q, want %q", got, want)
	}
}
```

Append to `internal/cli/session_test.go` (uses the file's existing `newGoldenHarness`/`captureChild`/`stubLookPath` helpers — see `TestPersonaDeveloperLaunchesHookStyle` at the top of that file for the pattern):

```go
// TestSessionTaskAssignment verifies --task validates the task, exports
// ATM_TASK, keys the context cache on the task, and renders the assignment.
func TestSessionTaskAssignment(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	out, _, code := h.run("task", "add", "--project", "ATM", "--title", "dispatch work", "--actor", "admin@cli:unset", "--output", "json")
	if code != ExitSuccess {
		t.Fatalf("task add failed: %d", code)
	}
	m := regexp.MustCompile(`"id":\s*"(ATM-[0-9a-f]+)"`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no task id in: %s", out)
	}
	taskID := m[1]
	c := captureChild(h)
	stubLookPath(h)
	h.reset()

	_, _, code = h.run("--persona", "developer", "--project", "ATM", "--agent", "claude", "--task", taskID)
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	joined := strings.Join(c.env, "\n")
	if !strings.Contains(joined, "ATM_TASK="+taskID) {
		t.Errorf("env missing ATM_TASK=%s:\n%s", taskID, joined)
	}
	re := regexp.MustCompile(`ATM_CONTEXT_FILE=(\S+)`)
	cm := re.FindStringSubmatch(joined)
	if cm == nil {
		t.Fatalf("no ATM_CONTEXT_FILE in env:\n%s", joined)
	}
	if !strings.Contains(cm[1], "session-developer-atm-") {
		t.Errorf("context cache key must include task: %s", cm[1])
	}
	b, err := os.ReadFile(cm[1])
	if err != nil {
		t.Fatalf("read context: %v", err)
	}
	if !strings.Contains(string(b), "## Assigned task") || !strings.Contains(string(b), taskID) {
		t.Errorf("context missing assignment block:\n%s", b)
	}
}

// TestSessionTaskValidation verifies bad --task values fail before launch.
func TestSessionTaskValidation(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "ATM", "--name", "A", "--actor", "admin@cli:unset")
	h.run("project", "create", "--code", "OTH", "--name", "B", "--actor", "admin@cli:unset")
	out, _, _ := h.run("task", "add", "--project", "OTH", "--title", "other", "--actor", "admin@cli:unset", "--output", "json")
	m := regexp.MustCompile(`"id":\s*"(OTH-[0-9a-f]+)"`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no task id in: %s", out)
	}
	captureChild(h)
	stubLookPath(h)
	h.reset()

	if _, _, code := h.run("--persona", "developer", "--project", "ATM", "--agent", "claude", "--task", "ATM-ffffff"); code == ExitSuccess {
		t.Error("missing task must fail")
	}
	h.reset()
	if _, _, code := h.run("--persona", "developer", "--project", "ATM", "--agent", "claude", "--task", m[1]); code == ExitSuccess {
		t.Error("task from another project must fail")
	}
	h.reset()
	if _, _, code := h.run("--persona", "concierge", "--agent", "claude", "--task", "ATM-ffffff"); code == ExitSuccess {
		t.Error("--task without --project must fail")
	}
}
```

Adjust the `task add` invocation flags if they differ — check with `go run ./cmd/atm task add --help` (or the existing `task_test.go`) and use the real flag names for title/project.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCacheKeyWithTask|TestSessionTask' -v`
Expected: FAIL — `too many arguments in call to cacheKey`, then `unknown flag: --task`.

- [ ] **Step 3: Implement**

1. `internal/cli/launcher_shared.go` — extend both helpers:

```go
func contextCachePath(storePath, code, persona, mode, capability, task string) string {
	key := cacheKey(persona, mode, capability, task)
	...
}

// cacheKey builds the filename stem:
// session-<persona>[-<mode>][-<capability>][-<task>].
func cacheKey(persona, mode, capability, task string) string {
	parts := []string{"session", persona}
	if mode != "" {
		parts = append(parts, mode)
	}
	if capability != "" {
		parts = append(parts, capability)
	}
	if task != "" {
		parts = append(parts, task)
	}
	...
}
```

Fix all existing callers/tests to pass `""` for the new parameter (compiler finds them).

2. `internal/cli/session.go`:
   - Add `Task string` to `sessionOpts`.
   - In `launchSession`, after the project-resolution block (after `code, projName = p.Code, p.Name`), add:

```go
	if opts.Task != "" {
		if code == "" {
			return fmt.Errorf("%w: --task requires --project", ErrUsage)
		}
		t, err := s.GetTask(opts.Task)
		if err != nil {
			return err
		}
		if t.ProjectCode != code {
			return fmt.Errorf("%w: task %s belongs to project %s, not %s", ErrUsage, t.ID, t.ProjectCode, code)
		}
		opts.Task = t.ID
	}
```

   - Pass the task through: `contextCachePath(..., opts.Capability, opts.Task)`; `session.ContextData{..., Task: opts.Task}`; `sessionEnvValues(..., opts.Capability, opts.Task, timestamp)` — add a `task string` parameter to `sessionEnvValues` (before `timestamp`) and inside it:

```go
	if task != "" {
		m["ATM_TASK"] = task
	}
```

   - `newSessionContextCmd`: add `Task` to its opts struct, a `--task` flag (`"assign the session a task (rendered into the prompt; not validated here)"`), and pass it through `renderSessionContext` (add a `task string` parameter; set `data.Task = task`). Update `newManageContextCmd`'s call with `""`.

3. `internal/cli/root.go` — after the `--agent` flag line:

```go
	root.Flags().StringVar(&opts.Task, "task", "", "assign the session a task from the project (exported as ATM_TASK and rendered into the session prompt)")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/cli/ ./internal/session/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/session.go internal/cli/launcher_shared.go internal/cli/launcher_shared_test.go internal/cli/session_test.go
git commit -m "feat(ATM-4b7e24): --task session assignment — validation, ATM_TASK, task-keyed context cache"
```

---

### Task 3: `internal/dispatch` core — Spec, Env, shell quoting, config, tmux target

**Files:**
- Create: `internal/dispatch/dispatch.go`, `internal/dispatch/shell.go`, `internal/dispatch/config.go`, `internal/dispatch/tmux.go`
- Test: `internal/dispatch/dispatch_test.go`

**Interfaces:**
- Produces (consumed by Tasks 4–6):

```go
type Spec struct{ Title string; Argv []string; Dir string }
type Env struct {
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Run      func(argv []string) (string, error) // trimmed stdout
}
func OSEnv() Env
type Target interface{ Name() string; Describe() string; Spawn(Spec) error }
func ShellCommand(argv []string) string
type Config struct{ TerminalCmd string `json:"terminal_cmd,omitempty"` }
func LoadConfig(path string) (Config, error)
func tmuxAvailable(env Env) bool
type tmuxTarget struct{ env Env }
```

- [ ] **Step 1: Write the failing tests**

Create `internal/dispatch/dispatch_test.go`:

```go
package dispatch

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fakeEnv builds an Env from a var map and a set of available binaries,
// recording every Run invocation into calls.
func fakeEnv(vars map[string]string, bins map[string]bool, calls *[][]string) Env {
	return Env{
		Getenv: func(k string) string { return vars[k] },
		LookPath: func(bin string) (string, error) {
			if bins[bin] {
				return "/usr/bin/" + bin, nil
			}
			return "", errors.New("not found")
		},
		Run: func(argv []string) (string, error) {
			*calls = append(*calls, argv)
			return "", nil
		},
	}
}

func TestShellCommandQuotes(t *testing.T) {
	got := ShellCommand([]string{"atm", "--persona", "developer", "--task", "it's"})
	want := `'atm' '--persona' 'developer' '--task' 'it'\''s'`
	if got != want {
		t.Fatalf("ShellCommand = %s, want %s", got, want)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	if c, err := LoadConfig(filepath.Join(dir, "absent.json")); err != nil || c.TerminalCmd != "" {
		t.Fatalf("missing file must be zero config, got %+v err %v", c, err)
	}
	p := filepath.Join(dir, "dispatch.json")
	os.WriteFile(p, []byte(`{"terminal_cmd":"kitty @ launch -- {cmd}"}`), 0o644)
	c, err := LoadConfig(p)
	if err != nil || c.TerminalCmd != "kitty @ launch -- {cmd}" {
		t.Fatalf("config = %+v, err %v", c, err)
	}
	os.WriteFile(p, []byte(`{nope`), 0o644)
	if _, err := LoadConfig(p); err == nil {
		t.Fatal("malformed config must error")
	}
}

func TestTmuxAvailability(t *testing.T) {
	var calls [][]string
	if tmuxAvailable(fakeEnv(map[string]string{}, map[string]bool{"tmux": true}, &calls)) {
		t.Fatal("no $TMUX must be unavailable")
	}
	if tmuxAvailable(fakeEnv(map[string]string{"TMUX": "/tmp/t,1,0"}, map[string]bool{}, &calls)) {
		t.Fatal("missing tmux binary must be unavailable")
	}
	if !tmuxAvailable(fakeEnv(map[string]string{"TMUX": "/tmp/t,1,0"}, map[string]bool{"tmux": true}, &calls)) {
		t.Fatal("inside tmux with binary must be available")
	}
}

func TestTmuxSpawnArgv(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{"TMUX": "x"}, map[string]bool{"tmux": true}, &calls)
	spec := Spec{Title: "ATM · manager", Argv: []string{"atm", "--persona", "manager", "--project", "ATM", "--agent", "claude"}, Dir: "/work"}
	if err := (tmuxTarget{env: env}).Spawn(spec); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"tmux", "new-window", "-n", "ATM · manager", "-c", "/work",
		`'atm' '--persona' 'manager' '--project' 'ATM' '--agent' 'claude'`}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/dispatch/ -v`
Expected: FAIL — package does not exist / undefined symbols.

- [ ] **Step 3: Implement**

`internal/dispatch/dispatch.go`:

```go
// Package dispatch spawns agent sessions on a separate terminal surface:
// a herdr pane, a tmux window, or a new terminal tab/window. It composes
// no session logic — the Argv it spawns is always the atm launcher.
package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Spec describes one session spawn.
type Spec struct {
	Title string   // surface label: "<CODE> · <persona>[ · <task-id>]"
	Argv  []string // the atm launcher invocation
	Dir   string   // working directory
}

// Env abstracts process environment and execution so targets are testable
// without real processes.
type Env struct {
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Run      func(argv []string) (string, error)
}

// OSEnv is the production Env: real environment, PATH, and process runner.
// Run returns trimmed stdout; on failure stderr is folded into the error.
func OSEnv() Env {
	return Env{
		Getenv:   os.Getenv,
		LookPath: exec.LookPath,
		Run: func(argv []string) (string, error) {
			out, err := exec.Command(argv[0], argv[1:]...).Output()
			if err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) && len(ee.Stderr) > 0 {
					return "", fmt.Errorf("%s: %s", argv[0], strings.TrimSpace(string(ee.Stderr)))
				}
				return "", fmt.Errorf("%s: %w", argv[0], err)
			}
			return strings.TrimSpace(string(out)), nil
		},
	}
}

// Target is one dispatch surface.
type Target interface {
	Name() string     // "herdr" | "tmux" | "terminal"
	Describe() string // human preview, e.g. "tmux · new window"
	Spawn(Spec) error
}
```

(Add `"errors"` to imports.)

`internal/dispatch/shell.go`:

```go
package dispatch

import "strings"

// ShellCommand renders argv as one POSIX shell command string, each argument
// single-quoted; embedded single quotes use the '\'' idiom.
func ShellCommand(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}
```

`internal/dispatch/config.go`:

```go
package dispatch

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config is the user-level dispatch configuration, stored as dispatch.json
// at the store root (sibling of agents.json). Hand-edited; no writer here.
type Config struct {
	// TerminalCmd overrides terminal detection: a command template run via
	// `sh -c` with {cmd}, {dir}, {title} placeholders.
	TerminalCmd string `json:"terminal_cmd,omitempty"`
}

// LoadConfig reads path; a missing file is a zero Config, a malformed file
// is an error naming the path.
func LoadConfig(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return c, nil
}
```

`internal/dispatch/tmux.go`:

```go
package dispatch

// tmuxAvailable reports whether we are inside a tmux session and the tmux
// binary resolves.
func tmuxAvailable(env Env) bool {
	if env.Getenv("TMUX") == "" {
		return false
	}
	_, err := env.LookPath("tmux")
	return err == nil
}

type tmuxTarget struct{ env Env }

func (t tmuxTarget) Name() string     { return "tmux" }
func (t tmuxTarget) Describe() string { return "tmux · new window" }

// Spawn opens a named tmux window in dir. tmux runs its shell-command
// argument via the user's shell, so argv is passed as one quoted string.
func (t tmuxTarget) Spawn(s Spec) error {
	_, err := t.env.Run([]string{"tmux", "new-window", "-n", s.Title, "-c", s.Dir, ShellCommand(s.Argv)})
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dispatch/ -v`
Expected: PASS (all four tests).

- [ ] **Step 5: Commit**

```bash
git add internal/dispatch/
git commit -m "feat(ATM-4b7e24): internal/dispatch core — Spec/Env/Target, shell quoting, config, tmux target"
```

---

### Task 4: Terminal targets — config template + emulator spawn table

**Files:**
- Create: `internal/dispatch/terminal.go`
- Test: `internal/dispatch/terminal_test.go`

**Interfaces:**
- Consumes: `Spec`, `Env`, `Config`, `ShellCommand`, `Target` (Task 3).
- Produces: `func terminalTarget(cfg Config, env Env) (Target, bool)` — Task 5's `Detect` calls this last.

- [ ] **Step 1: Write the failing tests**

Create `internal/dispatch/terminal_test.go`:

```go
package dispatch

import (
	"reflect"
	"strings"
	"testing"
)

func TestTemplateTargetSubstitutes(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{}, map[string]bool{}, &calls)
	tgt, ok := terminalTarget(Config{TerminalCmd: "kitty @ launch --type=tab --cwd {dir} --tab-title {title} -- {cmd}"}, env)
	if !ok {
		t.Fatal("config template must always yield a target")
	}
	spec := Spec{Title: "ATM · manager", Argv: []string{"atm", "--persona", "manager"}, Dir: "/w"}
	if err := tgt.Spawn(spec); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"sh", "-c", `kitty @ launch --type=tab --cwd /w --tab-title ATM · manager -- 'atm' '--persona' 'manager'`}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestEmulatorDetection(t *testing.T) {
	var calls [][]string
	// kitty fingerprint + binary present.
	env := fakeEnv(map[string]string{"KITTY_LISTEN_ON": "unix:/tmp/k"}, map[string]bool{"kitty": true}, &calls)
	tgt, ok := terminalTarget(Config{}, env)
	if !ok || !strings.Contains(tgt.Describe(), "kitty") {
		t.Fatalf("kitty must be detected, got ok=%v %v", ok, tgt)
	}
	// Fingerprint without binary: skipped.
	env = fakeEnv(map[string]string{"KITTY_LISTEN_ON": "unix:/tmp/k"}, map[string]bool{}, &calls)
	if _, ok := terminalTarget(Config{}, env); ok {
		t.Fatal("fingerprint without binary must not detect")
	}
	// Nothing detected, no template.
	env = fakeEnv(map[string]string{}, map[string]bool{}, &calls)
	if _, ok := terminalTarget(Config{}, env); ok {
		t.Fatal("no emulator and no template must fail")
	}
}

func TestEmulatorSpawnArgv(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{"WEZTERM_UNIX_SOCKET": "/tmp/w"}, map[string]bool{"wezterm": true}, &calls)
	tgt, ok := terminalTarget(Config{}, env)
	if !ok {
		t.Fatal("wezterm must be detected")
	}
	spec := Spec{Title: "ATM · manager", Argv: []string{"atm", "--persona", "manager"}, Dir: "/w"}
	if err := tgt.Spawn(spec); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"wezterm", "cli", "spawn", "--cwd", "/w", "--", "atm", "--persona", "manager"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestConfigTemplateWinsOverDetection(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{"KITTY_LISTEN_ON": "u"}, map[string]bool{"kitty": true}, &calls)
	tgt, _ := terminalTarget(Config{TerminalCmd: "echo {cmd}"}, env)
	if tgt.Describe() != "terminal · configured command" {
		t.Fatalf("template must win over detection, got %q", tgt.Describe())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/dispatch/ -run 'TestTemplate|TestEmulator|TestConfigTemplate' -v`
Expected: FAIL — `undefined: terminalTarget`.

- [ ] **Step 3: Implement**

Create `internal/dispatch/terminal.go`:

```go
package dispatch

import "strings"

// templateTarget runs the user-configured terminal_cmd via `sh -c` after
// placeholder substitution.
type templateTarget struct {
	env  Env
	tmpl string
}

func (t templateTarget) Name() string     { return "terminal" }
func (t templateTarget) Describe() string { return "terminal · configured command" }

func (t templateTarget) Spawn(s Spec) error {
	line := strings.NewReplacer(
		"{cmd}", ShellCommand(s.Argv),
		"{dir}", s.Dir,
		"{title}", s.Title,
	).Replace(t.tmpl)
	_, err := t.env.Run([]string{"sh", "-c", line})
	return err
}

// emulator is one spawn-table row: an env fingerprint plus the argv that
// opens a new tab (window where the emulator has no tabs).
type emulator struct {
	name        string
	bin         string
	fingerprint func(Env) bool
	argv        func(Spec) []string
}

var emulators = []emulator{
	{
		name: "kitty", bin: "kitty",
		fingerprint: func(e Env) bool { return e.Getenv("KITTY_LISTEN_ON") != "" },
		argv: func(s Spec) []string {
			return append([]string{"kitty", "@", "launch", "--type=tab", "--tab-title", s.Title, "--cwd", s.Dir, "--"}, s.Argv...)
		},
	},
	{
		name: "wezterm", bin: "wezterm",
		fingerprint: func(e Env) bool { return e.Getenv("WEZTERM_UNIX_SOCKET") != "" },
		argv: func(s Spec) []string {
			return append([]string{"wezterm", "cli", "spawn", "--cwd", s.Dir, "--"}, s.Argv...)
		},
	},
	{
		name: "gnome-terminal", bin: "gnome-terminal",
		fingerprint: func(e Env) bool { return e.Getenv("GNOME_TERMINAL_SCREEN") != "" },
		argv: func(s Spec) []string {
			return append([]string{"gnome-terminal", "--tab", "--title", s.Title, "--working-directory", s.Dir, "--"}, s.Argv...)
		},
	},
	{
		name: "konsole", bin: "konsole",
		fingerprint: func(e Env) bool { return e.Getenv("KONSOLE_VERSION") != "" },
		argv: func(s Spec) []string {
			return append([]string{"konsole", "--new-tab", "--workdir", s.Dir, "-e"}, s.Argv...)
		},
	},
	{
		name: "alacritty", bin: "alacritty",
		fingerprint: func(e Env) bool { return e.Getenv("ALACRITTY_SOCKET") != "" },
		argv: func(s Spec) []string {
			return append([]string{"alacritty", "msg", "create-window", "--working-directory", s.Dir, "-e"}, s.Argv...)
		},
	},
	{
		name: "foot", bin: "foot",
		fingerprint: func(e Env) bool { return strings.HasPrefix(e.Getenv("TERM"), "foot") },
		argv: func(s Spec) []string {
			return append([]string{"foot", "--title", s.Title, "--working-directory", s.Dir}, s.Argv...)
		},
	},
}

type emulatorTarget struct {
	env Env
	em  emulator
}

func (t emulatorTarget) Name() string     { return "terminal" }
func (t emulatorTarget) Describe() string { return "terminal · " + t.em.name }
func (t emulatorTarget) Spawn(s Spec) error {
	_, err := t.env.Run(t.em.argv(s))
	return err
}

// terminalTarget resolves the terminal step of detection: a configured
// template always wins; otherwise the first emulator whose fingerprint
// matches and whose binary resolves.
func terminalTarget(cfg Config, env Env) (Target, bool) {
	if cfg.TerminalCmd != "" {
		return templateTarget{env: env, tmpl: cfg.TerminalCmd}, true
	}
	for _, em := range emulators {
		if !em.fingerprint(env) {
			continue
		}
		if _, err := env.LookPath(em.bin); err != nil {
			continue
		}
		return emulatorTarget{env: env, em: em}, true
	}
	return nil, false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dispatch/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dispatch/terminal.go internal/dispatch/terminal_test.go
git commit -m "feat(ATM-4b7e24): terminal dispatch targets — config template + emulator spawn table"
```

---

### Task 5: herdr target, `Detect` precedence, `Service` facade

**Files:**
- Create: `internal/dispatch/herdr.go`, `internal/dispatch/service.go`
- Modify: `internal/dispatch/dispatch.go` (add `Detect`)
- Test: `internal/dispatch/detect_test.go`
- Modify: `tests/arch/imports_test.go` (add `internal/dispatch` to the eventsource-import enumeration list)

**Interfaces:**
- Consumes: Tasks 3–4 symbols.
- Produces (consumed by Task 6):

```go
func Detect(cfg Config, env Env) (Target, error)
type Service struct{ ... }
func NewService(configPath string) (*Service, error)
func (s *Service) Preview() (string, error)  // Describe() of the detected target
func (s *Service) Spawn(spec Spec) error
```

- [ ] **Step 1: Write the failing tests**

Create `internal/dispatch/detect_test.go`:

```go
package dispatch

import (
	"reflect"
	"strings"
	"testing"
)

func TestDetectPrecedence(t *testing.T) {
	var calls [][]string
	all := map[string]string{"HERDR_ENV": "1", "TMUX": "x", "KITTY_LISTEN_ON": "u"}
	bins := map[string]bool{"herdr": true, "tmux": true, "kitty": true}

	tgt, err := Detect(Config{}, fakeEnv(all, bins, &calls))
	if err != nil || tgt.Name() != "herdr" {
		t.Fatalf("herdr must win: %v %v", tgt, err)
	}
	noHerdr := map[string]string{"TMUX": "x", "KITTY_LISTEN_ON": "u"}
	tgt, err = Detect(Config{}, fakeEnv(noHerdr, bins, &calls))
	if err != nil || tgt.Name() != "tmux" {
		t.Fatalf("tmux must be second: %v %v", tgt, err)
	}
	kittyOnly := map[string]string{"KITTY_LISTEN_ON": "u"}
	tgt, err = Detect(Config{}, fakeEnv(kittyOnly, bins, &calls))
	if err != nil || tgt.Name() != "terminal" {
		t.Fatalf("terminal must be last: %v %v", tgt, err)
	}
	if _, err = Detect(Config{}, fakeEnv(map[string]string{}, map[string]bool{}, &calls)); err == nil {
		t.Fatal("nothing available must error")
	} else if !strings.Contains(err.Error(), "terminal_cmd") {
		t.Fatalf("error must name terminal_cmd: %v", err)
	}
}

func TestHerdrDetectionViaSocketPath(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{"HERDR_SOCKET_PATH": "/tmp/h.sock"}, map[string]bool{"herdr": true}, &calls)
	tgt, err := Detect(Config{}, env)
	if err != nil || tgt.Name() != "herdr" {
		t.Fatalf("HERDR_SOCKET_PATH must detect herdr: %v %v", tgt, err)
	}
}

func TestHerdrSpawnTwoStep(t *testing.T) {
	calls := [][]string{}
	env := Env{
		Getenv:   func(k string) string { return map[string]string{"HERDR_ENV": "1"}[k] },
		LookPath: func(string) (string, error) { return "/usr/bin/herdr", nil },
		Run: func(argv []string) (string, error) {
			calls = append(calls, argv)
			if argv[1] == "pane" && argv[2] == "split" {
				return "w1:p3", nil
			}
			return "", nil
		},
	}
	spec := Spec{Title: "ATM · developer · ATM-1", Argv: []string{"atm", "--persona", "developer"}, Dir: "/w"}
	if err := (herdrTarget{env: env}).Spawn(spec); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"herdr", "pane", "split", "--cwd", "/w", "--label", "ATM · developer · ATM-1"},
		{"herdr", "pane", "run", "w1:p3", `'atm' '--persona' 'developer'`},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/dispatch/ -run 'TestDetect|TestHerdr' -v`
Expected: FAIL — `undefined: Detect`, `undefined: herdrTarget`.

- [ ] **Step 3: Implement**

Create `internal/dispatch/herdr.go`:

```go
package dispatch

import (
	"fmt"
	"strings"
)

// herdrAvailable reports whether we run inside a herdr-managed pane (herdr
// injects HERDR_ENV=1 and HERDR_SOCKET_PATH) with the binary resolvable.
func herdrAvailable(env Env) bool {
	if env.Getenv("HERDR_ENV") != "1" && env.Getenv("HERDR_SOCKET_PATH") == "" {
		return false
	}
	_, err := env.LookPath("herdr")
	return err == nil
}

type herdrTarget struct{ env Env }

func (h herdrTarget) Name() string     { return "herdr" }
func (h herdrTarget) Describe() string { return "herdr · new pane" }

// Spawn creates a pane then runs the launcher in it. `herdr pane split`
// prints the new pane id; the last whitespace-separated token is taken so
// both bare-id and labeled outputs parse.
func (h herdrTarget) Spawn(s Spec) error {
	out, err := h.env.Run([]string{"herdr", "pane", "split", "--cwd", s.Dir, "--label", s.Title})
	if err != nil {
		return fmt.Errorf("herdr pane split: %w", err)
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return fmt.Errorf("herdr pane split printed no pane id")
	}
	if _, err := h.env.Run([]string{"herdr", "pane", "run", fields[len(fields)-1], ShellCommand(s.Argv)}); err != nil {
		return fmt.Errorf("herdr pane run: %w", err)
	}
	return nil
}
```

Add to `internal/dispatch/dispatch.go`:

```go
// Detect returns the first available target in precedence order
// herdr → tmux → terminal. The config template affects only the terminal
// step — a tmux session still wins over a configured terminal_cmd.
func Detect(cfg Config, env Env) (Target, error) {
	if herdrAvailable(env) {
		return herdrTarget{env: env}, nil
	}
	if tmuxAvailable(env) {
		return tmuxTarget{env: env}, nil
	}
	if t, ok := terminalTarget(cfg, env); ok {
		return t, nil
	}
	return nil, errors.New(`no dispatch target: not inside herdr or tmux and no known terminal detected — set "terminal_cmd" in dispatch.json at the store root`)
}
```

Create `internal/dispatch/service.go`:

```go
package dispatch

// Service is the TUI-facing facade. Config is loaded once at construction;
// detection runs per call so environment changes are reflected.
type Service struct {
	cfg Config
	env Env
}

func NewService(configPath string) (*Service, error) {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return &Service{cfg: cfg, env: OSEnv()}, nil
}

// Preview describes what Spawn would do, e.g. "tmux · new window".
func (s *Service) Preview() (string, error) {
	t, err := Detect(s.cfg, s.env)
	if err != nil {
		return "", err
	}
	return t.Describe(), nil
}

func (s *Service) Spawn(spec Spec) error {
	t, err := Detect(s.cfg, s.env)
	if err != nil {
		return err
	}
	return t.Spawn(spec)
}
```

In `tests/arch/imports_test.go`, add `"internal/dispatch"` to the directory enumeration in `TestOnlyEventlogImportsEventsourceLib` (the fixed dir list), keeping the new package under the no-eventsource rule.

- [ ] **Step 4: Verify herdr CLI flags against the installed binary (if present)**

Run: `command -v herdr && herdr pane split --help && herdr pane run --help || echo "herdr not installed — skip"`
Expected: if herdr is installed and its flags differ from `--cwd`/`--label` or the split output shape differs, adjust `herdr.go` and `TestHerdrSpawnTwoStep` to the real interface in this same task. If not installed, proceed — the seam is fully covered by the fake Env either way.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/dispatch/ ./tests/... `
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/dispatch/ tests/arch/imports_test.go
git commit -m "feat(ATM-4b7e24): herdr target, Detect precedence, dispatch Service facade"
```

---

### Task 6: TUI dispatch dialogs + `D` keybinding + composition-root wiring

**Files:**
- Create: `internal/tui/dispatch.go`
- Modify: `internal/tui/app.go` (Model fields, NewModelOpts, key routing, View), `internal/tui/tasks_list.go` (selectedRow helper), `internal/tui/run.go` (Run signature), `internal/tui/keymap.go` (keymapRows), `cmd/atm/main.go` (wiring)
- Test: `internal/tui/dispatch_test.go`

**Interfaces:**
- Consumes: `dispatch.Spec`, `*dispatch.Service` (Task 5); `agent.Catalog()`, `agent.Status(e, home, lookPath)`, `Readiness.Ready()/.String()` (existing `internal/agent`); `atm --persona … --task …` argv contract (Task 2).
- Produces: `tui.Dispatcher` interface; `tui.Run(svc core.Service, actor string, reg *capability.Registry, d Dispatcher) error` (breaking signature change — fix `cmd/atm/main.go` and any `run.go` callers/tests in this task).

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/dispatch_test.go`. Follow the construction pattern of `app_test.go`'s `newTestModel` (which builds a `NewModelOpts{Service: s, Actor: …, Registry: …}` model over a temp store) — reuse those helpers; the snippets below assume `m := newTestModel(t)` yields a model whose store contains at least one project with code `"ATM"` selectable in the projects pane, and use the existing test utilities for delivering key messages (look at `capabilities_test.go` for how overlay keys are driven; use the same `tea.KeyMsg` construction it uses).

```go
package tui

import (
	"strings"
	"testing"

	"atm/internal/dispatch"
	tea "github.com/charmbracelet/bubbletea"
)

type fakeDispatcher struct {
	preview    string
	previewErr error
	spawned    []dispatch.Spec
	spawnErr   error
}

func (f *fakeDispatcher) Preview() (string, error)     { return f.preview, f.previewErr }
func (f *fakeDispatcher) Spawn(s dispatch.Spec) error { f.spawned = append(f.spawned, s); return f.spawnErr }

func testAgents() []agentOption {
	return []agentOption{
		{name: "claude", ready: true},
		{name: "codex", ready: false, hint: "missing bin: codex (https://developers.openai.com/codex)"},
	}
}

// key delivers one key press to the model, mirroring capabilities_test.go.
func dispatchKey(m *Model, s string) {
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

func TestDispatchManagerFromProjectsPane(t *testing.T) {
	m := newTestModel(t) // seeded with project ATM, projects pane focused
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if m.dispatchDlg.kind != dispatchManager {
		t.Fatal("D on projects pane must open the manager dialog")
	}
	view := m.dispatchDlg.renderOverlay()
	for _, want := range []string{"Dispatch manager", "claude", "tmux · new window"} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay missing %q:\n%s", want, view)
		}
	}

	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("enter on ready agent must spawn")
	}
	got := fd.spawned[0]
	wantArgv := []string{"atm", "--persona", "manager", "--project", "ATM", "--agent", "claude"}
	if strings.Join(got.Argv, " ") != strings.Join(wantArgv, " ") {
		t.Errorf("argv = %v, want %v", got.Argv, wantArgv)
	}
	if got.Title != "ATM · manager" {
		t.Errorf("title = %q, want ATM · manager", got.Title)
	}
	if m.dispatchDlg.kind != dispatchNone {
		t.Error("dialog must close after dispatch")
	}
}

func TestDispatchDeveloperFromTaskRow(t *testing.T) {
	m := newTestModel(t)
	// Seed a task and focus the tasks pane on it; use the same seeding path
	// existing tasks_test.go tests use (create via m.store, then refresh).
	task, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	fd := &fakeDispatcher{preview: "herdr · new pane"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if m.dispatchDlg.kind != dispatchDeveloper {
		t.Fatal("D on tasks pane must open the developer dialog")
	}
	if m.dispatchDlg.taskID != task.ID {
		t.Fatalf("task = %q, want %q", m.dispatchDlg.taskID, task.ID)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	if !strings.Contains(argv, "--persona developer") || !strings.Contains(argv, "--task "+task.ID) {
		t.Errorf("argv = %s", argv)
	}
	if want := "ATM · developer · " + task.ID; fd.spawned[0].Title != want {
		t.Errorf("title = %q, want %q", fd.spawned[0].Title, want)
	}
}

func TestDispatchUnreadyAgentRefused(t *testing.T) {
	m := newTestModel(t)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyRight}) // move to codex (unready)
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 0 {
		t.Fatal("unready agent must not spawn")
	}
	if !strings.Contains(m.toastMsg, "not ready") {
		t.Errorf("toast = %q, want not-ready error", m.toastMsg)
	}
	if m.dispatchDlg.kind == dispatchNone {
		t.Error("dialog must stay open after refusal")
	}
}

func TestDispatchNoTargetDisables(t *testing.T) {
	m := newTestModel(t)
	m.dispatcher = &fakeDispatcher{previewErr: errors.New(`no dispatch target: not inside herdr or tmux and no known terminal detected — set "terminal_cmd" in dispatch.json at the store root`)}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "no dispatch target") {
		t.Errorf("overlay must show detection error:\n%s", view)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.dispatcher.(*fakeDispatcher).spawned) != 0 {
		t.Fatal("enter with no target must not spawn")
	}
}
```

(add `"errors"` to the test file's imports). Adjust `newTestModel` usage to however that helper actually seeds/focuses (read `app_test.go:30-50` first); if it does not seed a project, seed one exactly as other tests do and set `m.focused = paneProjects` + cursor. Before calling `renderOverlay` in any test, give the model a real size the way existing overlay tests do (deliver the same `tea.WindowSizeMsg` capabilities_test.go uses) — the box-width math assumes a nonzero `m.width`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestDispatch -v`
Expected: FAIL — `undefined: agentOption`, `m.dispatcher undefined`, etc.

- [ ] **Step 3: Implement the dialog sub-model**

Create `internal/tui/dispatch.go`:

```go
package tui

import (
	"os"
	"os/exec"
	"strings"

	"atm/internal/agent"
	"atm/internal/dispatch"

	tea "github.com/charmbracelet/bubbletea"
)

// Dispatcher is the TUI-facing dispatch port; *dispatch.Service implements
// it. nil disables dispatch with a clear error in the dialog.
type Dispatcher interface {
	Preview() (string, error)
	Spawn(dispatch.Spec) error
}

type dispatchKind int

const (
	dispatchNone dispatchKind = iota
	dispatchManager
	dispatchDeveloper
)

type agentOption struct {
	name  string
	ready bool
	hint  string
}

// agentOptions snapshots the catalog with readiness; swapped in tests via
// Model.agentOptionsFn.
func agentOptions() []agentOption {
	home, _ := os.UserHomeDir()
	var out []agentOption
	for _, e := range agent.Catalog() {
		r := agent.Status(e, home, exec.LookPath)
		out = append(out, agentOption{name: e.Name, ready: r.Ready(), hint: r.String()})
	}
	return out
}

// dispatchModel is the dispatch dialog overlay (pattern: capabilityModel).
type dispatchModel struct {
	m          *Model
	kind       dispatchKind
	project    string
	taskID     string
	taskTitle  string
	agents     []agentOption
	cursor     int
	preview    string
	previewErr string
}

func (d *dispatchModel) persona() string {
	if d.kind == dispatchDeveloper {
		return "developer"
	}
	return "manager"
}

func (d *dispatchModel) title() string {
	t := d.project + " · " + d.persona()
	if d.taskID != "" {
		t += " · " + d.taskID
	}
	return t
}

func (d *dispatchModel) open(kind dispatchKind, project, taskID, taskTitle string) {
	d.kind, d.project, d.taskID, d.taskTitle = kind, project, taskID, taskTitle
	d.agents = d.m.agentOptionsFn()
	d.cursor = 0
	for i, a := range d.agents { // preselect the first ready agent
		if a.ready {
			d.cursor = i
			break
		}
	}
	d.preview, d.previewErr = "", ""
	if d.m.dispatcher == nil {
		d.previewErr = "dispatch unavailable in this build"
		return
	}
	if p, err := d.m.dispatcher.Preview(); err != nil {
		d.previewErr = err.Error()
	} else {
		d.preview = p
	}
}

func (d *dispatchModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		d.kind = dispatchNone
	case "left", "h":
		if d.cursor > 0 {
			d.cursor--
		}
	case "right", "l":
		if d.cursor < len(d.agents)-1 {
			d.cursor++
		}
	case "enter":
		d.submit()
	}
	return nil
}

func (d *dispatchModel) submit() {
	if d.previewErr != "" {
		d.m.showToast("error: " + d.previewErr)
		return
	}
	if len(d.agents) == 0 {
		d.m.showToast("error: agent catalog is empty")
		return
	}
	a := d.agents[d.cursor]
	if !a.ready {
		d.m.showToast("error: agent " + a.name + " not ready: " + a.hint)
		return
	}
	argv := []string{"atm", "--persona", d.persona(), "--project", d.project, "--agent", a.name}
	if d.taskID != "" {
		argv = append(argv, "--task", d.taskID)
	}
	dir, err := os.Getwd()
	if err != nil {
		d.m.showToast("error: " + err.Error())
		return
	}
	if err := d.m.dispatcher.Spawn(dispatch.Spec{Title: d.title(), Argv: argv, Dir: dir}); err != nil {
		d.m.showToast("error: " + err.Error())
		return
	}
	d.m.showToast("dispatched " + d.persona() + " → " + d.preview)
	d.kind = dispatchNone
}

// renderOverlay draws the dialog. Box construction mirrors
// capabilityModel.renderOverlay (titledBoxHeight + styles.DialogBody) —
// reuse the same helpers and width conventions found there.
func (d *dispatchModel) renderOverlay() string {
	styles := d.m.styles
	var b strings.Builder
	if d.kind == dispatchDeveloper {
		b.WriteString("Task:   " + d.taskID + "\n")
		b.WriteString(styles.FieldHint.Render("        "+d.taskTitle) + "\n\n")
	}
	a := agentOption{name: "—"}
	if len(d.agents) > 0 {
		a = d.agents[d.cursor]
	}
	b.WriteString("Agent:  ‹ " + a.name + " ›\n")
	if a.ready {
		b.WriteString(styles.Success.Render("        ready") + "\n\n")
	} else {
		b.WriteString(styles.Error.Render("        x "+a.hint) + "\n\n")
	}
	if d.previewErr != "" {
		b.WriteString(styles.Error.Render("Target: x "+d.previewErr) + "\n")
	} else {
		b.WriteString("Target: " + d.preview + " “" + d.title() + "”\n")
	}
	b.WriteString("\n" + styles.KeyMenuDim.Render("[←/→]agent  [Enter]dispatch  [Esc]close"))

	bw := d.m.width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > d.m.width-4 {
		bw = d.m.width - 4
	}
	bh := strings.Count(b.String(), "\n") + 3
	return titledBoxHeight(styles.DialogBody, bw, "Dispatch "+d.persona()+" — "+d.project, b.String(), bh)
}
```

(`titledBoxHeight` and the `bw` computation mirror `capabilityModel.renderOverlay` in `capabilities.go:239-288`; the box helpers live in `styles.go:25-29`.) Truncate the `taskTitle` echo line to the box's inner width with `fitLine(s, w)` (`styles.go:106`) so long titles cannot widen the dialog: `styles.FieldHint.Render("        "+fitLine(d.taskTitle, bw-10))` — compute `bw` before the task lines are written (move the width computation to the top of the function).

- [ ] **Step 4: Wire the Model, keys, run signature, and composition root**

1. `internal/tui/app.go`:
   - `Model` fields: `dispatcher Dispatcher`, `agentOptionsFn func() []agentOption`, `dispatchDlg dispatchModel`.
   - `NewModelOpts`: add `Dispatcher Dispatcher`.
   - In `NewModel`: `m.dispatcher = opts.Dispatcher; m.agentOptionsFn = agentOptions; m.dispatchDlg.m = m` (mirror how `m.capability` gets its back-pointer).
   - Key routing in `handleKey`, directly after the `if m.capability.open { … }` block:

```go
	if m.dispatchDlg.kind != dispatchNone {
		return m.dispatchDlg.handleKey(k)
	}
```

   - Model-level key case (alongside the existing `case "C":` at `app.go:622`):

```go
	case "D":
		if m.focused == paneProjects {
			if row, ok := m.projects.selected(); ok {
				m.dispatchDlg.open(dispatchManager, row.code, "", "")
			}
			return nil
		}
		if m.focused == paneTasks {
			if r, ok := m.tasks.selectedRow(); ok {
				project := m.projectScope
				if r.task != nil && r.task.ProjectCode != "" {
					project = r.task.ProjectCode
				}
				if project == "" {
					m.showToast("error: no project scope for dispatch")
					return nil
				}
				m.dispatchDlg.open(dispatchDeveloper, project, r.id, r.title)
			}
			return nil
		}
```

   - In `View`, after the capability overlay placement (`app.go:833-835`):

```go
	if m.dispatchDlg.kind != dispatchNone {
		out = m.placeOverlay(out, m.dispatchDlg.renderOverlay())
	}
```

2. `internal/tui/tasks_list.go` — add a unified cursor accessor next to `rowAtCursor` (mirror the flat/grouped branching `openDetailAtCursor` at lines 163-199 already does):

```go
// selectedRow returns the task row under the cursor in either the flat or
// the grouped view.
func (t *tasksModel) selectedRow() (taskRow, bool) {
	// grouped view → delegate to rowAtCursor(); flat view → t.rows[t.cursor]
	// with bounds check. Copy the exact branching condition openDetailAtCursor
	// uses to decide between the two.
}
```

3. `internal/tui/keymap.go` — add to `keymapRows`:

```go
	{Key: "D", Projects: "dispatch manager", Tasks: "dispatch developer on task"},
```

(match the struct's field usage in neighboring rows; leave Boards/Detail empty).

4. `internal/tui/run.go` — extend the signature and pass through:

```go
func Run(svc core.Service, actor string, reg *capability.Registry, d Dispatcher) error {
	m, err := NewModel(NewModelOpts{Service: svc, Actor: actor, Registry: reg, Dispatcher: d})
	...
}
```

5. `cmd/atm/main.go` — in the `runTUI` closure:

```go
	runTUI := func(storePath, actor string) error {
		s, err := open(storePath)
		if err != nil {
			return err
		}
		d, err := dispatch.NewService(filepath.Join(s.StorePath(), "dispatch.json"))
		if err != nil {
			return err
		}
		return tui.Run(s, actor, reg, d)
	}
```

(add `"path/filepath"` and `"atm/internal/dispatch"` imports).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/tui/ ./tests/...`
Expected: PASS, including arch tests (tui imports dispatch and agent — neither is restricted).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/dispatch.go internal/tui/dispatch_test.go internal/tui/app.go internal/tui/tasks_list.go internal/tui/keymap.go internal/tui/run.go cmd/atm/main.go
git commit -m "feat(ATM-4b7e24): TUI dispatch dialogs — D dispatches manager/developer sessions"
```

---

### Task 7: TUI personas overlay (read-only)

**Files:**
- Create: `internal/tui/personas.go`
- Modify: `internal/tui/app.go` (field, `V` key case, routing, View), `internal/tui/keymap.go`
- Test: `internal/tui/personas_test.go`

**Interfaces:**
- Consumes: `m.store.ListPersonas() []*core.Persona` (existing `core.PersonaService`; `core.Persona{Name, Prompt, Description}`).
- Produces: nothing downstream — terminal feature.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/personas_test.go` (same harness as Task 6):

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPersonasOverlayListsAndViews(t *testing.T) {
	m := newTestModel(t)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("V")})
	if !m.personasOv.open {
		t.Fatal("V must open the personas overlay")
	}
	view := m.personasOv.renderOverlay()
	for _, want := range []string{"developer", "manager", "concierge", "admin"} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay missing built-in %q:\n%s", want, view)
		}
	}

	// Move to a persona and open its prompt.
	m.personasOv.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.personasOv.detail {
		t.Fatal("enter must open detail view")
	}
	detail := m.personasOv.renderOverlay()
	if !strings.Contains(detail, "Persona") {
		t.Errorf("detail must render the persona prompt:\n%s", detail)
	}

	// Esc: detail → list → closed.
	m.personasOv.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.personasOv.detail || !m.personasOv.open {
		t.Fatal("first esc must return to list")
	}
	m.personasOv.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.personasOv.open {
		t.Fatal("second esc must close the overlay")
	}
}

func TestPersonasOverlayIsReadOnly(t *testing.T) {
	m := newTestModel(t)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("V")})
	before := len(m.store.ListPersonas())
	for _, k := range []string{"e", "d", "a", "x", "p"} {
		m.personasOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
	if got := len(m.store.ListPersonas()); got != before {
		t.Fatalf("personas changed %d → %d; overlay must be read-only", before, got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestPersonasOverlay -v`
Expected: FAIL — `m.personasOv undefined`.

- [ ] **Step 3: Implement**

Create `internal/tui/personas.go`:

```go
package tui

import (
	"strings"

	"atm/internal/core"

	tea "github.com/charmbracelet/bubbletea"
)

// personasModel is the read-only personas overlay: list built-ins and
// customs, enter to view the effective prompt. No mutation paths.
type personasModel struct {
	m       *Model
	open    bool
	cursor  int
	entries []*core.Persona
	detail  bool
	lines   []string
	offset  int
}

func (p *personasModel) openOverlay() {
	p.entries = p.m.store.ListPersonas()
	p.open, p.detail, p.offset = true, false, 0
	if p.cursor >= len(p.entries) {
		p.cursor = 0
	}
}

func (p *personasModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "V":
		if p.detail {
			p.detail = false
			return nil
		}
		p.open = false
	case "j", "down":
		if p.detail {
			p.offset++
			return nil
		}
		if p.cursor < len(p.entries)-1 {
			p.cursor++
		}
	case "k", "up":
		if p.detail {
			if p.offset > 0 {
				p.offset--
			}
			return nil
		}
		if p.cursor > 0 {
			p.cursor--
		}
	case "g":
		p.offset, p.cursor = 0, 0
	case "enter":
		if !p.detail && len(p.entries) > 0 {
			p.lines = strings.Split(p.entries[p.cursor].Prompt, "\n")
			p.offset = 0
			p.detail = true
		}
	}
	return nil
}

// renderOverlay draws the persona list, or the scrolled prompt in detail
// mode. Box shape and cursor styling mirror capabilityModel.renderOverlay.
func (p *personasModel) renderOverlay() string {
	styles := p.m.styles
	bw := p.m.width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > p.m.width-4 {
		bw = p.m.width - 4
	}

	if p.detail {
		height := p.m.height - 8
		if height < 8 {
			height = 8
		}
		if p.offset > len(p.lines)-1 {
			p.offset = len(p.lines) - 1
		}
		if p.offset < 0 {
			p.offset = 0
		}
		end := p.offset + height - 3
		if end > len(p.lines) {
			end = len(p.lines)
		}
		var body strings.Builder
		for _, ln := range p.lines[p.offset:end] {
			body.WriteString(fitLine(ln, bw-4) + "\n")
		}
		body.WriteString("\n" + styles.KeyMenuDim.Render("[j/k]scroll  [Esc]back"))
		title := "Persona: " + p.entries[p.cursor].Name
		return titledBoxHeight(styles.DialogBody, bw, title, body.String(), height)
	}

	nameW := 10
	for _, e := range p.entries {
		if len(e.Name) > nameW {
			nameW = len(e.Name)
		}
	}
	var body strings.Builder
	for i, e := range p.entries {
		line := fmt.Sprintf("%-*s  %s", nameW, e.Name, e.Description)
		line = fitLine(line, bw-4)
		if i == p.cursor {
			line = styles.RowCursor.Render(line)
		} else {
			line = styles.Body.Render(line)
		}
		body.WriteString(line + "\n")
	}
	body.WriteString("\n" + styles.KeyMenuDim.Render("[↑/↓]move  [Enter]view prompt  [Esc]close"))
	return titledBoxHeight(styles.DialogBody, bw, "Personas", body.String(), len(p.entries)+5)
}
```

(add `"fmt"` to the imports; `titledBoxHeight`/`fitLine` are in `styles.go:25-29,106`).

Wire in `internal/tui/app.go`:
- `Model` field: `personasOv personasModel`; in `NewModel`: `m.personasOv.m = m`.
- Routing in `handleKey`, next to the other overlay routings (order: after `m.dispatchDlg`):

```go
	if m.personasOv.open {
		return m.personasOv.handleKey(k)
	}
```

- Model-level key case:

```go
	case "V":
		m.personasOv.openOverlay()
		return nil
```

- `View` placement after the dispatch overlay:

```go
	if m.personasOv.open {
		out = m.placeOverlay(out, m.personasOv.renderOverlay())
	}
```

- `keymap.go` `keymapRows`: add `{Key: "V", Projects: "view personas", Tasks: "view personas"}`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/personas.go internal/tui/personas_test.go internal/tui/app.go internal/tui/keymap.go
git commit -m "feat(ATM-4b7e24): read-only personas overlay on V"
```

---

### Task 8: Docs, spec sync, ledger

**Files:**
- Modify: `README.md`, `CHANGELOG.md`, `docs/superpowers/specs/2026-07-23-tui-agent-dispatch-design.md`

**Interfaces:** none — documentation only.

- [ ] **Step 1: Update the spec to match the implemented dialog mechanism**

In `docs/superpowers/specs/2026-07-23-tui-agent-dispatch-design.md`, replace the sentence stating the work "adds a select/cycle field type" to `form.go` with: the dialogs are dedicated overlay sub-models following the `capabilityModel` pattern (`internal/tui/dispatch.go`), and `form.go` is unchanged.

- [ ] **Step 2: README + CHANGELOG**

- `README.md`: in the section documenting the TUI keys / personas (the Advanced section that documents `atm --persona` launches), add: `D` dispatches a manager (projects pane) or developer-on-task (tasks pane) session into herdr/tmux/terminal, `V` opens the personas browser, `--task <id>` assigns a session a task (`ATM_TASK`), and `dispatch.json` (`terminal_cmd` template with `{cmd}`/`{dir}`/`{title}`) at the store root configures the terminal fallback. Match the surrounding prose style; keep it to one short subsection.
- `CHANGELOG.md`: add an entry under the unreleased/top section following the file's existing format: TUI agent dispatch (D), personas overlay (V), `--task`/`ATM_TASK` session assignment, `internal/dispatch` herdr/tmux/terminal targets.

- [ ] **Step 3: Full verification**

Run: `go build ./... && go test ./...`
Expected: everything PASS.

- [ ] **Step 4: Commit**

```bash
git add README.md CHANGELOG.md docs/superpowers/specs/2026-07-23-tui-agent-dispatch-design.md
git commit -m "docs(ATM-4b7e24): dispatch keys, --task, dispatch.json; spec sync to overlay dialogs"
```

- [ ] **Step 5: Ledger update**

Add a progress comment to ATM-4b7e24 (actor `developer@claude:<model>`): implementation complete per plan `docs/superpowers/plans/2026-07-23-tui-agent-dispatch.md`; list the landed commits and note any deviations (e.g. herdr flag adjustments from Task 5 Step 4).
