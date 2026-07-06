# ATM Manager Subagent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `atm manager` command tree parallel to `atm developing` that launches an autonomous ATM-ledger-owner subagent/interactive session, plus installable `atm-manager` subagent definitions for OpenCode, Codex, and Claude.

**Architecture:** Mirror `internal/developing/` as a new `internal/manager/` package (context renderer, launcher, plugin assets, plugin install/status). Add `internal/cli/manager.go` parallel to `internal/cli/developing.go`. Extract shared launcher helpers (run id, child process exec, env assembly, header/tail emission) from `internal/cli/developing.go` into `internal/cli/launcher_shared.go` so both trees use one code path without cross-package coupling. The manager prompt is embedded as `internal/manager/context_v1.md` (the spec's Appendix) and rendered by `atm manager render-context`. Per-host adapters are thin subagent-definition markdown files installed into each host's user-level agents directory.

**Tech Stack:** Go 1.22+, `embed`, `github.com/spf13/cobra`, bash for Claude/Codex hook scripts, JS for the OpenCode plugin (already installed by `atm developing plugin install`).

## Global Constraints

- Go 1.22+.
- `make verify` is the repository gate (or `make build && make test`).
- No emojis in code or commits.
- Follow existing style in neighboring files; do not unilaterally restructure.
- ATM env contract: `ATM_ROLE`, `ATM_PROJECT`, `ATM_BIN`, `ATM_ACTOR`, `ATM_RUN_ID`, `ATM_CONTEXT_FILE`.
- Manager actor default is `<host>-manager` (not `<host>-dev`).
- Status is a label axis (`<CODE>:status:<state>`), not a dedicated field.
- Plugin install writes only to user-level agent config; never repo-local.

---

## File Structure

New files:

- `internal/manager/context.go` — embeds `context_v1.md`, `RenderContext(data)`.
- `internal/manager/context_v1.md` — the manager system prompt template (from spec Appendix).
- `internal/manager/launcher.go` — `Launcher` interface + `LauncherFor(host)`.
- `internal/manager/launcher_test.go` — argv/hint tests, mirroring `developing/launcher_test.go`.
- `internal/manager/context_test.go` — placeholder substitution tests.
- `internal/manager/plugins.go` — `Asset`, `PluginAssets`, `Status`, `InstallResult`, `PluginInstallRoot`, `PluginStatus`, `InstallPlugin`.
- `internal/manager/plugins_test.go` — asset presence, env-conditional, status/install tests.
- `internal/manager/plugin_assets/opencode/atm-manager.md` — OpenCode subagent definition.
- `internal/manager/plugin_assets/claude/atm-manager.md` — Claude agent definition.
- `internal/manager/plugin_assets/codex/atm-manager.md` — Codex agent definition.
- `internal/cli/manager.go` — `atm manager` command tree (host launchers, `render-context`, `plugin status`, `plugin install`).
- `internal/cli/launcher_shared.go` — extracted `newRunID`, `shortUUID`, `runChild`, `assembleEnv`, `emitLaunchHeader`, `emitLaunchTail` shared by developing and manager.
- `internal/cli/manager_test.go` — golden tests parallel to `developing_test.go`.
- `internal/cli/testdata/golden/manager-*.json` — golden fixtures.

Modified files:

- `internal/cli/root.go` — register `newManagerCmd(st)`.
- `internal/cli/developing.go` — switch to shared helpers from `launcher_shared.go` (no behavior change).
- `internal/developing/plugin_assets/opencode/atm-developing.js` — add the `atm-manager` dispatch paragraph to the bootstrap block (one string change).
- `internal/developing/plugin_assets/claude/hooks/session-start` — add the dispatch paragraph to the `context` string.
- `internal/developing/plugin_assets/codex/hooks/session-start` — same.
- `internal/developing/plugin_assets/claude/skills/atm-developing/SKILL.md` — add a "Tracking work via the manager" section.
- `internal/developing/plugin_assets/codex/skills/atm-developing/SKILL.md` — same.
- `internal/developing/plugin_assets/opencode/skills/atm-developing/SKILL.md` — same.

---

## Task 1: Extract shared launcher helpers

**Goal:** Pull `newRunID`, `shortUUID`, child-process exec, env assembly, header/tail emission out of `internal/cli/developing.go` into `internal/cli/launcher_shared.go` so the manager tree reuses them without import cycles. Behavior of `atm developing` must not change; the existing developing golden tests are the regression gate.

**Files:**
- Create: `internal/cli/launcher_shared.go`
- Modify: `internal/cli/developing.go`
- Test: `internal/cli/developing_test.go` (no new tests; existing goldens are the gate)

**Interfaces:**
- Produces:
  - `func newRunID(code string) string` (moved from `onboarding.go`-local use; keep `onboarding.go`'s own copy or have onboarding call the shared one — see step 2)
  - `func runChild(name string, argv []string, env []string) (int, error)`
  - `func assembleEnv(extras map[string]string) []string`
  - `func emitLaunchHeader(st *cliState, role, project, runID, contextPath, agent string, argv []string, env map[string]string) error`
  - `func emitLaunchTail(st *cliState, role, project, runID, contextPath, agent string, agentExit int) error`

- [ ] **Step 1: Write the shared helpers file**

Create `internal/cli/launcher_shared.go`:

```go
package cli

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// newRunID builds a run id of the form <CODE>-<YYYYMMDDHHMMSS>-<6-hex>.
// Shared by the developing and manager launchers.
func newRunID(code string) string {
	return fmt.Sprintf("%s-%s-%s",
		code,
		time.Now().UTC().Format("20060102150405"),
		shortUUID(),
	)
}

// shortUUID returns a 6-char hex suffix for collision safety in run IDs.
func shortUUID() string {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "000000"
	}
	return fmt.Sprintf("%x", b[:])
}

// runChild executes the host agent with inherited stdio and the given env.
// Returns the exit code and error (if any). Shared by developing and manager.
func runChild(name string, argv []string, env []string) (int, error) {
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return 0, fmt.Errorf("%s not found on PATH; install: see host docs", name)
	}
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = env
	err = cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), err
		}
		return 1, err
	}
	return 0, nil
}

// assembleEnv returns os.Environ() plus the extras, extras winning on conflict.
func assembleEnv(extras map[string]string) []string {
	env := os.Environ()
	for k, v := range extras {
		env = append(env, k+"="+v)
	}
	return env
}

// emitLaunchHeader writes the pre-launch summary in JSON or text form.
// role is "developing" or "manager".
func emitLaunchHeader(st *cliState, role, project, runID, contextPath, agent string, argv []string, env map[string]string) error {
	return st.emit(st.stdout(), map[string]any{
		"role":         role,
		"run_id":       runID,
		"project":      project,
		"agent":        agent,
		"context_path": contextPath,
		"argv":         argv,
		"env":          env,
	}, func() {
		fmt.Fprintf(st.stdout(), "%s %s  run=%s  agent=%s\n", role, project, runID, agent)
		fmt.Fprintf(st.stdout(), "  context:  %s\n", contextPath)
		fmt.Fprintf(st.stdout(), "  launching: %s\n", strings.Join(argv, " "))
	})
}

// emitLaunchTail writes the post-launch summary in JSON or text form.
func emitLaunchTail(st *cliState, role, project, runID, contextPath, agent string, agentExit int) error {
	return st.emit(st.stdout(), map[string]any{
		"role":         role,
		"run_id":       runID,
		"project":      project,
		"agent":        agent,
		"context_path": contextPath,
		"agent_exit":   agentExit,
	}, func() {
		fmt.Fprintf(st.stdout(), "%s %s  run=%s\n", role, project, runID)
		fmt.Fprintf(st.stdout(), "  context: %s\n", contextPath)
		fmt.Fprintf(st.stdout(), "%s exited %d\n", agent, agentExit)
	})
}

// hostNotFoundHint returns the install hint for a known host, or "" if unknown.
func hostNotFoundHint(agent string) string {
	switch agent {
	case "opencode":
		return "https://opencode.ai"
	case "codex":
		return "https://developers.openai.com/codex"
	case "claude":
		return "https://code.claude.com"
	default:
		return ""
	}
}

var _ = cobra.Command{} // keep cobra import meaningful if unused after extraction
```

- [ ] **Step 2: Remove the now-duplicate helpers from developing.go and onboarding.go**

In `internal/cli/developing.go`:
- Delete `runDevelopingChild`, `developingEnv`, `emitDevelopingHeader`, `emitDevelopingTail` (replaced by `runChild`, `assembleEnv`, `emitLaunchHeader`, `emitLaunchTail`).
- Update `runDeveloping` to call `runChild(l.Name(), argv, env)`, `assembleEnv(developingEnvValues(...))`, `emitLaunchHeader(st, "developing", ...)`, `emitLaunchTail(st, "developing", ...)`.
- Keep `developingEnvValues` (returns the map).

In `internal/cli/onboarding.go`:
- Delete the local `newRunID` and `shortUUID` (they are now in `launcher_shared.go`). If onboarding does not currently call `newRunID`, leave it alone — verify with `grep newRunID internal/cli/onboarding.go`. Only remove if unused locally; otherwise switch its call site to the shared `newRunID`.

Concrete `runDeveloping` body after change:

```go
func runDeveloping(st *cliState, l developing.Launcher, opts developingOpts) error {
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

	argv := l.BuildArgv()
	envValues := developingEnvValues(opts.Project, atmBin, opts.Actor, runID, contextPath)
	env := assembleEnv(envValues)
	if err := emitLaunchHeader(st, "developing", opts.Project, runID, contextPath, l.Name(), argv, envValues); err != nil {
		return err
	}
	if opts.DryRun {
		return nil
	}

	exitCode, runErr := runChild(l.Name(), argv, env)
	if err := emitLaunchTail(st, "developing", opts.Project, runID, contextPath, l.Name(), exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}
```

Also update `runChild` to use the host hint: change the not-found error in `launcher_shared.go`'s `runChild` to accept a hint, or keep `runChild` generic and let callers wrap. Simpler: have `runDeveloping` pass `l.NotFoundHint()` separately. Revise `runChild` signature to `runChild(name string, argv []string, env []string, notFoundHint string) (int, error)` and use `notFoundHint` in the error message:

```go
func runChild(name string, argv []string, env []string, notFoundHint string) (int, error) {
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		hint := notFoundHint
		if hint == "" {
			hint = "install: see host docs"
		}
		return 0, fmt.Errorf("%s not found on PATH; install: %s", name, hint)
	}
	// ... rest unchanged
}
```

Update the `runDeveloping` call to `runChild(l.Name(), argv, env, l.NotFoundHint())`.

- [ ] **Step 3: Build and run the developing golden tests to confirm no regression**

Run: `go build ./... && go test ./internal/cli -run 'TestDeveloping' -count=1`
Expected: PASS. If `developing-dry-run-codex` golden fails, the header/tail JSON shape changed — update the golden with `go test ./internal/cli -run 'TestDevelopingCodexDryRunJSON' -update` only after confirming the new shape is correct (it should be identical; the only addition is a `"role": "developing"` field in the header, which the golden must capture).

- [ ] **Step 4: Regenerate affected developing goldens and verify**

If the header now includes `"role": "developing"`, regenerate:
Run: `go test ./internal/cli -run 'TestDeveloping' -update`
Then: `go test ./internal/cli -run 'TestDeveloping' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/launcher_shared.go internal/cli/developing.go internal/cli/onboarding.go internal/cli/testdata/golden/developing-*.json
git commit -m "Extract shared launcher helpers for developing and manager"
```

---

## Task 2: Manager context renderer + prompt template

**Goal:** Create `internal/manager/context_v1.md` (the spec Appendix prompt) and `internal/manager/context.go` with `RenderContext`. The prompt has the same placeholders as developing's (`<CODE>`, `<PROJECT_NAME>`, `<ATM_BIN>`, `<ACTOR>`, `<RUN_ID>`, `<TIMESTAMP>`).

**Files:**
- Create: `internal/manager/context_v1.md`
- Create: `internal/manager/context.go`
- Create: `internal/manager/context_test.go`

**Interfaces:**
- Produces:
  - `package manager`
  - `type ContextData struct { Code, Name, ATMBin, Actor, RunID, Timestamp string }`
  - `func RenderContext(data ContextData) string`

- [ ] **Step 1: Write the prompt template**

Create `internal/manager/context_v1.md` with the exact content from the spec's [Appendix: Manager Prompt](../specs/2026-07-06-atm-manager-subagent-design.md#appendix-manager-prompt), keeping all `<CODE>`, `<PROJECT_NAME>`, `<ATM_BIN>`, `<ACTOR>`, `<RUN_ID>`, `<TIMESTAMP>` placeholders intact. The first three lines must be:

```
# ATM manager session <RUN_ID>

Project: `<CODE>` (`<PROJECT_NAME>`)
Started: `<TIMESTAMP>`
ATM binary: `<ATM_BIN>`
Actor: `<ACTOR>`
```

Followed by the `## Role`, `## Track pipeline (subagent mode)`, `## Ledger hygiene`, `## Interactive mode (human → manager)`, `## Commands`, and `## Code of conduct` sections exactly as in the spec appendix.

- [ ] **Step 2: Write the failing test**

Create `internal/manager/context_test.go`:

```go
package manager

import (
	"strings"
	"testing"
)

func TestRenderContextSubstitutesAllPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{
		Code:      "ATM",
		Name:      "Agent Tasks Management",
		ATMBin:    "/usr/local/bin/atm",
		Actor:     "opencode-manager",
		RunID:     "ATM-20260706120000-a1b2c3",
		Timestamp: "2026-07-06T12:00:00Z",
	})
	for _, placeholder := range []string{
		"<CODE>", "<PROJECT_NAME>", "<ATM_BIN>", "<ACTOR>",
		"<RUN_ID>", "<TIMESTAMP>",
	} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered context still contains %s", placeholder)
		}
	}
	for _, want := range []string{
		"ATM manager session ATM-20260706120000-a1b2c3",
		"Project: `ATM` (`Agent Tasks Management`)",
		"ATM binary: `/usr/local/bin/atm`",
		"Actor: `opencode-manager`",
		"atm task comment add --task <ID>",
		"atm task set-title --id <ID>",
		"needs clarification",
		"semantic search",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q", want)
		}
	}
}

func TestRenderContextGenericKeepsPlaceholders(t *testing.T) {
	// The env-driven generic body is produced by leaving placeholders in place
	// so the subagent resolves them from env at dispatch time. RenderContext
	// with empty fields must NOT strip placeholders.
	got := RenderContext(ContextData{})
	for _, placeholder := range []string{"<CODE>", "<ATM_BIN>", "<ACTOR>"} {
		if !strings.Contains(got, placeholder) {
			t.Errorf("generic render stripped %s; placeholders must survive for env-driven use", placeholder)
		}
	}
}
```

Note: `RenderContext` uses `strings.NewReplacer` exactly like `developing.RenderContext`. With empty fields, the replacer substitutes `""` — which would strip placeholders. To support the generic env-driven body, `RenderContext` must leave a placeholder alone when the corresponding field is empty. Implement that in step 3.

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/manager -run TestRenderContext -count=1`
Expected: FAIL (package doesn't exist yet / `RenderContext` undefined).

- [ ] **Step 4: Write the implementation**

Create `internal/manager/context.go`:

```go
package manager

import (
	_ "embed"
	"strings"
)

//go:embed context_v1.md
var contextV1 string

type ContextData struct {
	Code      string
	Name      string
	ATMBin    string
	Actor     string
	RunID     string
	Timestamp string
}

// RenderContext substitutes the ContextData placeholders into the manager
// prompt template. Empty fields leave their placeholder in place so the
// env-driven generic body (used by atm-manager subagent definitions) can be
// produced by calling RenderContext with zero values.
func RenderContext(data ContextData) string {
	pairs := []string{
		"<CODE>", data.Code,
		"<PROJECT_NAME>", data.Name,
		"<ATM_BIN>", data.ATMBin,
		"<ACTOR>", data.Actor,
		"<RUN_ID>", data.RunID,
		"<TIMESTAMP>", data.Timestamp,
	}
	// Build a replacer that only substitutes non-empty values; empty values
	// are replaced with the placeholder itself so it survives.
	final := make([]string, 0, len(pairs))
	for i := 0; i < len(pairs); i += 2 {
		key, val := pairs[i], pairs[i+1]
		if val == "" {
			final = append(final, key, key)
		} else {
			final = append(final, key, val)
		}
	}
	return strings.NewReplacer(final...).Replace(contextV1)
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/manager -run TestRenderContext -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/manager/context_v1.md internal/manager/context.go internal/manager/context_test.go
git commit -m "Add manager context renderer and prompt template"
```

---

## Task 3: Manager launcher

**Goal:** `internal/manager/launcher.go` with `LauncherFor(host)` returning the host's interactive argv, mirroring `developing.LauncherFor`.

**Files:**
- Create: `internal/manager/launcher.go`
- Create: `internal/manager/launcher_test.go`

**Interfaces:**
- Produces:
  - `type Launcher interface { Name() string; NotFoundHint() string; BuildArgv() []string }`
  - `func LauncherFor(name string) (Launcher, bool)`

- [ ] **Step 1: Write the failing test**

Create `internal/manager/launcher_test.go` (copy `developing/launcher_test.go` verbatim — same argv/hints since the hosts are the same):

```go
package manager

import (
	"reflect"
	"testing"
)

func TestLaunchersBuildNormalInteractiveArgv(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{name: "opencode", want: []string{"opencode"}},
		{name: "codex", want: []string{"codex"}},
		{name: "claude", want: []string{"claude"}},
	}
	for _, tt := range tests {
		l, ok := LauncherFor(tt.name)
		if !ok {
			t.Fatalf("LauncherFor(%q) not found", tt.name)
		}
		if got := l.BuildArgv(); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s BuildArgv = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestLauncherHints(t *testing.T) {
	tests := map[string]string{
		"opencode": "https://opencode.ai",
		"codex":    "https://developers.openai.com/codex",
		"claude":   "https://code.claude.com",
	}
	for name, wantHint := range tests {
		l, ok := LauncherFor(name)
		if !ok {
			t.Fatalf("LauncherFor(%q) not found", name)
		}
		if l.Name() != name {
			t.Errorf("Name = %q, want %q", l.Name(), name)
		}
		if l.NotFoundHint() != wantHint {
			t.Errorf("NotFoundHint = %q, want %q", l.NotFoundHint(), wantHint)
		}
	}
}

func TestLauncherForUnknown(t *testing.T) {
	if _, ok := LauncherFor("ollama"); ok {
		t.Fatal("LauncherFor(ollama) returned ok=true")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/manager -run TestLauncher -count=1`
Expected: FAIL (`LauncherFor` undefined).

- [ ] **Step 3: Write the implementation**

Create `internal/manager/launcher.go`:

```go
package manager

type Launcher interface {
	Name() string
	NotFoundHint() string
	BuildArgv() []string
}

type staticLauncher struct {
	name string
	hint string
	argv []string
}

func (l staticLauncher) Name() string         { return l.name }
func (l staticLauncher) NotFoundHint() string { return l.hint }
func (l staticLauncher) BuildArgv() []string  { return append([]string(nil), l.argv...) }

func LauncherFor(name string) (Launcher, bool) {
	switch name {
	case "opencode":
		return staticLauncher{name: "opencode", hint: "https://opencode.ai", argv: []string{"opencode"}}, true
	case "codex":
		return staticLauncher{name: "codex", hint: "https://developers.openai.com/codex", argv: []string{"codex"}}, true
	case "claude":
		return staticLauncher{name: "claude", hint: "https://code.claude.com", argv: []string{"claude"}}, true
	default:
		return nil, false
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/manager -run TestLauncher -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manager/launcher.go internal/manager/launcher_test.go
git commit -m "Add manager launcher for opencode/codex/claude"
```

---

## Task 4: Manager plugin assets (subagent definitions)

**Goal:** Create the per-host `atm-manager` subagent definition templates under `internal/manager/plugin_assets/`. Each is an env-driven markdown file whose body is the manager prompt rendered with placeholders left in place (so the subagent reads `ATM_PROJECT`/`ATM_BIN`/`ATM_ACTOR` from env at dispatch time).

**Files:**
- Create: `internal/manager/plugin_assets/opencode/atm-manager.md`
- Create: `internal/manager/plugin_assets/claude/atm-manager.md`
- Create: `internal/manager/plugin_assets/codex/atm-manager.md`

**Interfaces:**
- Produces: files consumed by `manager.PluginAssets(host)` in Task 5.

- [ ] **Step 1: Write the OpenCode subagent definition**

Create `internal/manager/plugin_assets/opencode/atm-manager.md`. The frontmatter uses OpenCode's agent schema (`description`, `mode`, `permission`). The body is the manager prompt with a one-line env preamble and placeholders left intact:

```markdown
---
description: ATM ledger owner. Invoke when the developing agent asks to track work, formalize progress, split/merge tasks, or organize the project ledger. Reads ATM_PROJECT/ATM_BIN/ATM_ACTOR from env; stays silent unless ATM_ROLE=manager.
mode: subagent
permission:
  bash: allow
  edit: deny
  write: deny
---

<!-- Rendered by `atm manager render-context`. Placeholders resolve from env at dispatch time. -->

# ATM manager session <RUN_ID>

Project: `<CODE>` (`<PROJECT_NAME>`)
Started: `<TIMESTAMP>`
ATM binary: `<ATM_BIN>`
Actor: `<ACTOR>`

If `ATM_ROLE` is not `manager`, respond with "atm-manager inactive" and stop.

## Role

You are the ATM ledger owner for project `<CODE>` (`<PROJECT_NAME>`).
You own the ledger's shape: consistent labels, clear titles, accurate
status, prioritized work, and comments that capture intent and progress
rather than chat. You are a full ATM CLI actor via `<ATM_BIN>` — you
create tasks, add comments, adjust labels, transition status, rewrite
titles, split tasks into subtasks, and merge related tasks. Stamp every
mutating write with actor `<ACTOR>`.

You run in one of two modes, and the mode sets your pacing:

- **Subagent mode (fast)**: a developing agent dispatched you mid-work
  with a track request. Optimize for a fast, useful ledger write and a
  short confirmation. Do not over-deliberate. Make a reasonable call,
  write it, return. The developing agent is waiting briefly and then
  continuing; it does not depend on your reply.
- **Interactive mode (thorough)**: a human launched you via
  `atm manager <host> --project <CODE>` to consult or steer you about
  project organization. Optimize for a thorough review. Dig into the
  ledger, propose splits/merges, rewrite titles for clarity, surface
  staleness and priority, sum up long discussions into structured
  comments, and ask the human to clarify when something is genuinely
  ambiguous.

In both modes you do not ask the developing agent back. In subagent
mode, if a track request is ambiguous, make the most reasonable
interpretation, write that, and optionally leave a short
"needs clarification" note on the task for the human to resolve in an
interactive session. In interactive mode, ask the human directly.

## Track pipeline (subagent mode)

A track request arrives as your prompt: an optional advisory hint line
of the form `hint: <word>` followed by a freeform message. The hint is
a short string from this open set: `feature`, `bug`, `design`, `spec`,
`chore`, `investigation`, `decision`, `progress`, `blocker`, `handoff`,
`question`. Unknown or missing hints are fine; fall back to interpreting
the freeform message alone.

On receiving a track request, work quickly:

1. Read `ATM_PROJECT`, `ATM_BIN`, `ATM_ACTOR` from your environment. If
   `ATM_ROLE` is not `manager`, stay silent.
2. Skim the current ledger for the project: open tasks, recent comments,
   labels. Find the task this track call most likely extends.
3. Decide the formal action and write it:
   - Append a progress/comment to an existing open task (the common
     case).
   - Create a new task if the track call clearly starts a new unit of
     work.
   - Adjust labels (add priority, transition status) when the hint or
     message signals it (`blocker`, `decision`, etc.).
   - Split a task into subtasks only when the track call clearly spans
     unrelated work that the original task conflated.
4. Simplify titles and descriptions when you touch a task. Rewrite the
   title so a future agent's semantic search will find it: name the
   concept, not the transient activity. Keep titles short.
5. Sum up discussion into the comment. If the track message is a long
   chat-like dump, distill it into one or two lines of structured
   progress note, not a verbatim copy.
6. If the request is genuinely ambiguous in a way that changes which
   task or action is correct, make your best guess, write it, and add a
   one-line `needs clarification` comment on the task. Do not block.
7. Return a concise confirmation: which task(s) you touched, what action
   you took, and the task ID(s). Do not summarize the track message
   back.

## Ledger hygiene

- Use the project's label conventions consistently. Run
  `<ATM_BIN> label list --project <CODE> --output json` if you are
  unsure which labels exist.
- Status is a label axis (`<CODE>:status:<state>`), not a field. Do not
  invent status values; reuse the project's existing status labels or
  add a new one only when the work genuinely introduces a new state.
- Keep titles concise, accurate, and searchable. A good title names the
  concept ("Refactor label resolver to handle hierarchical prefixes")
  not the moment ("working on labels"). Update titles when work drifts
  from the original framing.
- Comments record intent, progress, decisions, files changed, test
  results, blockers, commit SHAs, and handoff notes. Distill chat-like
  input into structured notes. A track call that says "still working on
  X, hit a snag with Y" becomes `Progress: working on X. Blocker: Y
  needs resolution.`, not a paragraph.
- Surface priority when you see it. If a track call describes a blocker
  or a regression, add the appropriate priority label and name the task
  in your confirmation.

## Interactive mode (human → manager)

When launched as `atm manager <host>`, take your time and review the
project thoroughly. Typical asks:

- "What's stale?" — query open tasks with old `updated_at`, surface
  them, propose cleanup (close, merge, or re-prioritize).
- "What should I work on next?" — rank open tasks by priority/status
  and recent activity.
- "Reconcile labels on ATM-0024." — adjust labels per the human's
  steering.
- "Summarize the last session's ledger activity." — read recent
  comments and produce a digest.
- "Split ATM-0017 into subtasks." — break a conflated task into focused
  subtasks with clear titles.
- "Merge ATM-0018 and ATM-0019." — combine related tasks, preserving
  comment history.

Answer in dialogue. Propose writes and execute them when the human
agrees. Ask the human to clarify when something is genuinely ambiguous
— that is what interactive mode is for. Do not write code or modify
repo files; you only touch the ATM ledger.

## Commands

- `<ATM_BIN> conventions`
- `<ATM_BIN> label list --project <CODE> --output json`
- `<ATM_BIN> task list --project <CODE> --output json`
- `<ATM_BIN> task show --id <ID> --output json`
- `<ATM_BIN> task create --project <CODE> --title "<title>" --label <CODE>:status:open --actor <ACTOR>`
- `<ATM_BIN> task comment add --task <ID> --body "<progress note>" --actor <ACTOR>`
- `<ATM_BIN> task comment list --task <ID> --output json`
- `<ATM_BIN> task label add --task <ID> --label <LABEL> --actor <ACTOR>`
- `<ATM_BIN> task label remove --task <ID> --label <LABEL> --actor <ACTOR>`
- `<ATM_BIN> task set-title --id <ID> --title "<title>" --actor <ACTOR>`
- `<ATM_BIN> task set-description --id <ID> --description "<desc>" --actor <ACTOR>`
- `<ATM_BIN> task remove --id <ID> --actor <ACTOR>`

## Code of conduct

Follow repo instructions, existing skills, harness rules, tool
permissions, and user directions first. ATM is the ledger you own; it
is not a workflow that overrides the host's normal rules. If
instructions conflict, preserve the normal agent/repo instruction
hierarchy and use ATM where compatible.
```

- [ ] **Step 2: Write the Claude subagent definition**

Create `internal/manager/plugin_assets/claude/atm-manager.md`. Claude agent frontmatter uses `name`, `description`, `tools`. The body is identical to the OpenCode body (the env preamble + manager prompt). Use this frontmatter:

```markdown
---
name: atm-manager
description: ATM ledger owner. Invoke when the developing agent asks to track work, formalize progress, split/merge tasks, or organize the project ledger. Reads ATM_PROJECT/ATM_BIN/ATM_ACTOR from env; stays silent unless ATM_ROLE=manager.
tools:
  - bash
  - read
  - glob
  - grep
---

<!-- Rendered by `atm manager render-context`. Placeholders resolve from env at dispatch time. -->
```

Followed by the same body as the OpenCode file (from the `# ATM manager session <RUN_ID>` line through the `## Code of conduct` section).

- [ ] **Step 3: Write the Codex subagent definition**

Create `internal/manager/plugin_assets/codex/atm-manager.md`. Codex agent frontmatter is minimal (`name`, `description`). Use:

```markdown
---
name: atm-manager
description: ATM ledger owner. Invoke when the developing agent asks to track work, formalize progress, split/merge tasks, or organize the project ledger. Reads ATM_PROJECT/ATM_BIN/ATM_ACTOR from env; stays silent unless ATM_ROLE=manager.
---

<!-- Rendered by `atm manager render-context`. Placeholders resolve from env at dispatch time. -->
```

Followed by the same body as the OpenCode file.

- [ ] **Step 4: Commit**

```bash
git add internal/manager/plugin_assets/
git commit -m "Add atm-manager subagent definitions for opencode/claude/codex"
```

---

## Task 5: Manager plugin assets API and install/status

**Goal:** `internal/manager/plugins.go` mirroring `developing/plugins.go` but for the manager subagent definitions. The install root is the host's user-level agents directory (not the developing plugin directory). `PluginStatus` reports `partial` when the developing plugin is absent (the developing agent would not know to dispatch `atm-manager`).

**Files:**
- Create: `internal/manager/plugins.go`
- Create: `internal/manager/plugins_test.go`

**Interfaces:**
- Produces:
  - `type Asset struct { Path string; Mode fs.FileMode; Content []byte }`
  - `func PluginAssets(agent string) ([]Asset, bool)`
  - `type Status struct { Agent, State, Path string }`
  - `type InstallResult struct { Agent string; Path string; Files []string; DryRun bool }`
  - `func PluginInstallRoot(agent, home string) (string, bool)`
  - `func PluginStatus(agent, home string) Status`
  - `func InstallPlugin(agent, home string, dryRun bool) (InstallResult, error)`
- Consumes: `developing.PluginStatus(agent, home)` to detect whether the developing bootstrap is installed (for the `partial` state).

- [ ] **Step 1: Write the failing test**

Create `internal/manager/plugins_test.go`:

```go
package manager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/developing"
)

func TestPluginAssetsExistForSupportedHosts(t *testing.T) {
	for _, host := range []string{"opencode", "claude", "codex"} {
		assets, ok := PluginAssets(host)
		if !ok {
			t.Fatalf("PluginAssets(%q) ok=false", host)
		}
		if len(assets) == 0 {
			t.Fatalf("PluginAssets(%q) returned no files", host)
		}
	}
}

func TestPluginAssetsCheckATMRole(t *testing.T) {
	for _, host := range []string{"opencode", "claude", "codex"} {
		assets, _ := PluginAssets(host)
		joined := string(joinManagerAssetContents(assets))
		if !strings.Contains(joined, "ATM_ROLE") {
			t.Errorf("%s assets do not check ATM_ROLE", host)
		}
		if !strings.Contains(joined, "ATM_PROJECT") {
			t.Errorf("%s assets do not reference ATM_PROJECT", host)
		}
	}
}

func TestPluginAssetsContainManagerRole(t *testing.T) {
	assets, _ := PluginAssets("opencode")
	joined := string(joinManagerAssetContents(assets))
	for _, want := range []string{
		"ATM ledger owner",
		"needs clarification",
		"semantic search",
		"atm task set-title",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("OpenCode manager asset missing %q", want)
		}
	}
}

func TestPluginInstallRoot(t *testing.T) {
	home := "/home/user"
	cases := map[string]string{
		"opencode": filepath.Join(home, ".config", "opencode", "agents", "atm-manager.md"),
		"claude":   filepath.Join(home, ".claude", "agents", "atm-manager.md"),
		"codex":    filepath.Join(home, ".codex", "agents", "atm-manager.md"),
	}
	for host, want := range cases {
		got, ok := PluginInstallRoot(host, home)
		if !ok {
			t.Fatalf("PluginInstallRoot(%q) ok=false", host)
		}
		if got != want {
			t.Errorf("PluginInstallRoot(%q) = %q, want %q", host, got, want)
		}
	}
	if _, ok := PluginInstallRoot("ollama", home); ok {
		t.Fatal("PluginInstallRoot(ollama) returned ok=true")
	}
}

func TestPluginStatusMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, host := range []string{"opencode", "claude", "codex"} {
		if got := PluginStatus(host, home); got.State != "missing" {
			t.Errorf("PluginStatus(%q) = %q, want missing", host, got.State)
		}
	}
}

func TestPluginStatusInstalledRequiresDevelopingPlugin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Install only the manager asset, not the developing plugin.
	if _, err := InstallPlugin("opencode", home, false); err != nil {
		t.Fatal(err)
	}
	got := PluginStatus("opencode", home)
	if got.State != "partial" {
		t.Errorf("PluginStatus(opencode) without developing plugin = %q, want partial", got.State)
	}
	// Now fake the developing plugin presence by installing it.
	if _, err := developing.InstallPlugin("opencode", home, false); err != nil {
		t.Fatal(err)
	}
	got = PluginStatus("opencode", home)
	if got.State != "installed" {
		t.Errorf("PluginStatus(opencode) with developing plugin = %q, want installed", got.State)
	}
}

func TestInstallPluginWritesSubagentDefinition(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	res, err := InstallPlugin("opencode", home, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) == 0 {
		t.Fatal("InstallPlugin wrote no files")
	}
	for _, f := range res.Files {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("installed file %s missing: %v", f, err)
		}
	}
}

func TestInstallPluginDryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	res, err := InstallPlugin("claude", home, true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun {
		t.Fatal("DryRun=false on dry run")
	}
	for _, f := range res.Files {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("dry run wrote file %s", f)
		}
	}
}

func joinManagerAssetContents(assets []Asset) []byte {
	var out []byte
	for _, a := range assets {
		out = append(out, a.Content...)
		out = append(out, '\n')
	}
	return out
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/manager -run TestPlugin -count=1`
Expected: FAIL (`PluginAssets`/`PluginStatus`/`InstallPlugin` undefined).

- [ ] **Step 3: Write the implementation**

Create `internal/manager/plugins.go`:

```go
package manager

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"atm/internal/developing"
)

//go:embed plugin_assets/opencode/atm-manager.md
//go:embed plugin_assets/claude/atm-manager.md
//go:embed plugin_assets/codex/atm-manager.md
var pluginFS embed.FS

type Asset struct {
	Path    string
	Mode    fs.FileMode
	Content []byte
}

func PluginAssets(agent string) ([]Asset, bool) {
	root := filepath.ToSlash(filepath.Join("plugin_assets", agent))
	if _, err := fs.Stat(pluginFS, root); err != nil {
		return nil, false
	}
	var assets []Asset
	err := fs.WalkDir(pluginFS, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := pluginFS.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		assets = append(assets, Asset{
			Path:    filepath.ToSlash(rel),
			Mode:    0o644,
			Content: b,
		})
		return nil
	})
	if err != nil {
		return nil, false
	}
	return assets, true
}

type Status struct {
	Agent string `json:"agent"`
	State string `json:"state"`
	Path  string `json:"path"`
}

type InstallResult struct {
	Agent  string   `json:"agent"`
	Path   string   `json:"path"`
	Files  []string `json:"files"`
	DryRun bool     `json:"dry_run"`
}

func PluginInstallRoot(agent, home string) (string, bool) {
	switch agent {
	case "opencode":
		return filepath.Join(home, ".config", "opencode", "agents", "atm-manager.md"), true
	case "claude":
		return filepath.Join(home, ".claude", "agents", "atm-manager.md"), true
	case "codex":
		return filepath.Join(home, ".codex", "agents", "atm-manager.md"), true
	default:
		return "", false
	}
}

func PluginStatus(agent, home string) Status {
	root, ok := PluginInstallRoot(agent, home)
	if !ok {
		return Status{Agent: agent, State: "unknown"}
	}
	if _, err := os.Stat(root); err != nil {
		return Status{Agent: agent, State: "missing", Path: root}
	}
	// The manager subagent is only useful if the developing bootstrap is also
	// installed, since the developing agent learns the dispatch contract from
	// the developing plugin. Without it, report partial.
	devStatus := developing.PluginStatus(agent, home)
	if devStatus.State != "installed" {
		return Status{Agent: agent, State: "partial", Path: root}
	}
	return Status{Agent: agent, State: "installed", Path: root}
}

func InstallPlugin(agent, home string, dryRun bool) (InstallResult, error) {
	root, ok := PluginInstallRoot(agent, home)
	if !ok {
		return InstallResult{}, fmt.Errorf("unknown agent %q", agent)
	}
	assets, ok := PluginAssets(agent)
	if !ok {
		return InstallResult{}, fmt.Errorf("plugin assets for %q not found", agent)
	}
	res := InstallResult{Agent: agent, Path: root, DryRun: dryRun}
	for _, a := range assets {
		dst := root // single-file install: the subagent definition is the only asset.
		res.Files = append(res.Files, dst)
		if dryRun {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return res, err
		}
		if err := os.WriteFile(dst, a.Content, a.Mode); err != nil {
			return res, err
		}
	}
	return res, nil
}
```

Note: the manager has a single asset per host (the `atm-manager.md` file), so `dst` is always `root`. No `pluginAssetDestination` helper is needed (unlike developing's, which splits assets across plugin + skills dirs).

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/manager -run TestPlugin -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manager/plugins.go internal/manager/plugins_test.go
git commit -m "Add manager plugin assets API, install, and status"
```

---

## Task 6: Manager CLI command tree

**Goal:** `internal/cli/manager.go` with `atm manager opencode|codex|claude`, `atm manager render-context`, `atm manager plugin status|install`. Register in `root.go`.

**Files:**
- Create: `internal/cli/manager.go`
- Modify: `internal/cli/root.go` (add `root.AddCommand(newManagerCmd(st))`)
- Create: `internal/cli/manager_test.go`
- Create: `internal/cli/testdata/golden/manager-*.json` (via `-update`)

**Interfaces:**
- Consumes:
  - `manager.RenderContext(manager.ContextData{...})`
  - `manager.LauncherFor(host)`
  - `manager.PluginStatus(agent, home)`, `manager.InstallPlugin(agent, home, dryRun)`
  - Shared helpers from `launcher_shared.go`: `newRunID`, `runChild`, `assembleEnv`, `emitLaunchHeader`, `emitLaunchTail`
- Produces: the `atm manager` command tree registered on the root.

- [ ] **Step 1: Write the manager CLI implementation**

Create `internal/cli/manager.go`:

```go
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"atm/internal/manager"
	"atm/internal/store"

	"github.com/spf13/cobra"
)

type managerOpts struct {
	Project string
	Actor   string
	DryRun  bool
}

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
	return cmd
}

func newManagerPluginCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage ATM manager subagent definitions",
	}
	cmd.AddCommand(newManagerPluginStatusCmd(st))
	cmd.AddCommand(newManagerPluginInstallCmd(st))
	return cmd
}

func newManagerPluginStatusCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "status [all|opencode|codex|claude]",
		Short: "Show ATM manager plugin install status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "all"
			if len(args) > 0 {
				target = args[0]
			}
			agents, err := managerPluginAgents(target)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			plugins := make([]manager.Status, 0, len(agents))
			for _, agent := range agents {
				plugins = append(plugins, manager.PluginStatus(agent, home))
			}
			return st.emit(st.stdout(), map[string]any{"plugins": plugins}, func() {
				for _, plugin := range plugins {
					fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", plugin.Agent, plugin.State, plugin.Path)
				}
			})
		},
	}
}

func newManagerPluginInstallCmd(st *cliState) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "install [all|opencode|codex|claude]",
		Short: "Install ATM manager subagent definitions",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "all"
			if len(args) > 0 {
				target = args[0]
			}
			agents, err := managerPluginAgents(target)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			installed := make([]manager.InstallResult, 0, len(agents))
			for _, agent := range agents {
				res, err := manager.InstallPlugin(agent, home, dryRun)
				if err != nil {
					return err
				}
				installed = append(installed, res)
			}
			return st.emit(st.stdout(), map[string]any{"installed": installed}, func() {
				for _, res := range installed {
					mode := "installed"
					if res.DryRun {
						mode = "would install"
					}
					fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", res.Agent, mode, res.Path)
				}
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print files that would be written without modifying user config")
	return cmd
}

func managerPluginAgents(target string) ([]string, error) {
	all := []string{"opencode", "codex", "claude"}
	if target == "" || target == "all" {
		return all, nil
	}
	for _, agent := range all {
		if target == agent {
			return []string{agent}, nil
		}
	}
	return nil, fmt.Errorf("%w: unknown manager plugin agent %q", ErrUsage, target)
}

func newManagerAgentCmd(st *cliState, agent string) *cobra.Command {
	var opts managerOpts
	cmd := &cobra.Command{
		Use:   agent,
		Short: "Launch " + agent + " with ATM manager context",
		RunE: func(cmd *cobra.Command, args []string) error {
			l, ok := manager.LauncherFor(agent)
			if !ok {
				return fmt.Errorf("%w: unknown manager agent %q", ErrUsage, agent)
			}
			opts.Actor = defaultManagerActor(l.Name(), st, opts.Actor)
			return runManager(st, l, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project the manager owns")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id stamped into ATM commands (default <agent>-manager)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "render context + print argv/env; do not launch")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func defaultManagerActor(agent string, st *cliState, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if st.flags.actor != "" {
		return st.flags.actor
	}
	return agent + "-manager"
}

func newManagerRenderContextCmd(st *cliState) *cobra.Command {
	var opts struct {
		Project string
		Actor   string
	}
	cmd := &cobra.Command{
		Use:   "render-context",
		Short: "Print the ATM manager system prompt to stdout",
		RunE: func(cmd *cobra.Command, args []string) error {
			data := manager.ContextData{
				Code:   opts.Project,
				Actor:  opts.Actor,
				ATMBin: atmBinPath(),
				RunID:  "RENDER",
			}
			rendered := manager.RenderContext(data)
			// Text mode: print raw markdown. JSON mode: wrap in an envelope.
			return st.emit(st.stdout(), map[string]any{"context": rendered}, func() {
				fmt.Fprint(st.stdout(), rendered)
			})
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project code (optional; when absent, placeholders are left for env-driven use)")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id (optional)")
	return cmd
}

func runManager(st *cliState, l manager.Launcher, opts managerOpts) error {
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
	contextPath := filepath.Join(s.StorePath(), "manager", runID+".md")
	if err := os.MkdirAll(filepath.Dir(contextPath), 0o755); err != nil {
		return fmt.Errorf("create manager dir: %w", err)
	}

	rendered := manager.RenderContext(manager.ContextData{
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

	argv := l.BuildArgv()
	envValues := managerEnvValues(opts.Project, atmBin, opts.Actor, runID, contextPath)
	env := assembleEnv(envValues)
	if err := emitLaunchHeader(st, "manager", opts.Project, runID, contextPath, l.Name(), argv, envValues); err != nil {
		return err
	}
	if opts.DryRun {
		return nil
	}

	exitCode, runErr := runChild(l.Name(), argv, env, l.NotFoundHint())
	if err := emitLaunchTail(st, "manager", opts.Project, runID, contextPath, l.Name(), exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}

func managerEnvValues(project, atmBin, actor, runID, contextPath string) map[string]string {
	return map[string]string{
		"ATM_ROLE":         "manager",
		"ATM_PROJECT":      project,
		"ATM_BIN":          atmBin,
		"ATM_ACTOR":        actor,
		"ATM_RUN_ID":       runID,
		"ATM_CONTEXT_FILE": contextPath,
	}
}

func atmBinPath() string {
	bin, err := os.Executable()
	if err != nil {
		return "atm"
	}
	return bin
}
```

Note the `strings` import is unused after removing `var _ = strings.TrimSpace` — drop it from the import block in step 1. The `managerEnvValues` and `atmBinPath` helpers are used by `runManager` and `newManagerRenderContextCmd` respectively.

- [ ] **Step 2: Register the manager command on the root**

In `internal/cli/root.go`, add after the `newDevelopingCmd(st)` line:

```go
	root.AddCommand(newManagerCmd(st))
```

- [ ] **Step 3: Write the failing tests**

Create `internal/cli/manager_test.go`:

```go
package cli

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestManagerCodexDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("manager", "codex", "--project", "FOO", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manager-dry-run-codex", got)
}

func TestManagerMissingProject(t *testing.T) {
	h := newGoldenHarness(t)
	_, stderrStr, code := h.run("manager", "codex", "--project", "NOPE", "--dry-run")
	if code != ExitNotFound {
		t.Fatalf("exit = %d, want %d", code, ExitNotFound)
	}
	got := normalizeManagerOutput(stderrStr, h.store.StorePath())
	compareGolden(t, "manager-missing-project", got)
}

func TestManagerPluginStatusJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	_, _, code := h.run("manager", "plugin", "status", "codex")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeHome(h.stdout.String(), home)
	compareGolden(t, "manager-plugin-status", got)
}

func TestManagerPluginInstallDryRunJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	_, _, code := h.run("manager", "plugin", "install", "opencode", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeHome(h.stdout.String(), home)
	compareGolden(t, "manager-plugin-install-dry-run", got)
}

func TestManagerEnvIncludesATMValues(t *testing.T) {
	got := managerEnvValues("FOO", "/bin/atm", "codex-manager", "FOO-RUNID", "/tmp/context.md")
	joined := strings.Join(gotToSlice(got), "\n")
	for _, want := range []string{
		"ATM_ROLE=manager",
		"ATM_PROJECT=FOO",
		"ATM_BIN=/bin/atm",
		"ATM_ACTOR=codex-manager",
		"ATM_RUN_ID=FOO-RUNID",
		"ATM_CONTEXT_FILE=/tmp/context.md",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("manager env missing %q", want)
		}
	}
}

func TestManagerRenderContextTextHasPrompt(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	_, _, code := h.run("manager", "render-context", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := h.stdout.String()
	for _, want := range []string{"ATM manager session", "ledger owner", "needs clarification", "atm task set-title"} {
		if !strings.Contains(got, want) {
			t.Errorf("render-context output missing %q", want)
		}
	}
}

func TestManagerRenderContextGenericKeepsPlaceholders(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	_, _, code := h.run("manager", "render-context")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := h.stdout.String()
	for _, placeholder := range []string{"<CODE>", "<ATM_BIN>", "<ACTOR>"} {
		if !strings.Contains(got, placeholder) {
			t.Errorf("generic render-context stripped %s", placeholder)
		}
	}
}

func gotToSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func normalizeManagerOutput(s, storePath string) string {
	s = normalizeOutput(s)
	if storePath != "" {
		contextPathRe := regexp.MustCompile(strings.ReplaceAll(filepath.ToSlash(storePath), `/`, `\/`) + `/manager/FOO-\d{14}-[0-9a-f]{6}\.md`)
		s = contextPathRe.ReplaceAllString(s, "/STORE/manager/FOO-RUNID.md")
	}
	runIDRe := regexp.MustCompile(`FOO-\d{14}-[0-9a-f]{6}`)
	s = runIDRe.ReplaceAllString(s, "FOO-RUNID")
	atmBinRe := regexp.MustCompile(`"ATM_BIN": "[^"]+"`)
	s = atmBinRe.ReplaceAllString(s, `"ATM_BIN": "/ATM_BIN"`)
	return s
}
```

- [ ] **Step 4: Build and confirm tests fail**

Run: `go build ./...`
Expected: build fails on the stray `if !opts.Project == "" {}` block — remove it, then build succeeds.

Run: `go test ./internal/cli -run TestManager -count=1`
Expected: FAIL (missing goldens).

- [ ] **Step 5: Generate goldens and verify**

Run: `go test ./internal/cli -run TestManager -update`
Then: `go test ./internal/cli -run TestManager -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/manager.go internal/cli/manager_test.go internal/cli/root.go internal/cli/testdata/golden/manager-*.json
git commit -m "Add atm manager CLI command tree"
```

---

## Task 7: Add the atm-manager dispatch contract to the developing bootstrap

**Goal:** The developing agent's bootstrap context (OpenCode JS plugin, Claude/Codex SessionStart hooks, and the atm-developing SKILL.md files) gains the dispatch contract telling it to dispatch `atm-manager` to track work.

**Files:**
- Modify: `internal/developing/plugin_assets/opencode/atm-developing.js`
- Modify: `internal/developing/plugin_assets/claude/hooks/session-start`
- Modify: `internal/developing/plugin_assets/codex/hooks/session-start`
- Modify: `internal/developing/plugin_assets/opencode/skills/atm-developing/SKILL.md`
- Modify: `internal/developing/plugin_assets/claude/skills/atm-developing/SKILL.md`
- Modify: `internal/developing/plugin_assets/codex/skills/atm-developing/SKILL.md`
- Modify: `internal/developing/plugins_test.go` (add an assertion for the dispatch contract)

**Interfaces:**
- Consumes: the existing developing asset files.

- [ ] **Step 1: Write the failing test assertion**

Add to `internal/developing/plugins_test.go` a new test:

```go
func TestPluginAssetsContainManagerDispatchContract(t *testing.T) {
	for _, agent := range []string{"opencode", "claude", "codex"} {
		assets, _ := PluginAssets(agent)
		joined := string(joinAssetContents(assets))
		if !strings.Contains(joined, "atm-manager") {
			t.Errorf("%s assets do not mention the atm-manager subagent", agent)
		}
		if !strings.Contains(joined, "dispatch") {
			t.Errorf("%s assets do not mention dispatching the manager", agent)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/developing -run TestPluginAssetsContainManagerDispatchContract -count=1`
Expected: FAIL.

- [ ] **Step 3: Add the dispatch contract to the OpenCode JS plugin**

In `internal/developing/plugin_assets/opencode/atm-developing.js`, append a sentence to the `bootstrap` return template (after the "More context:" line, before `</ATM_DEVELOPING_CONTEXT>`):

```
To track work, dispatch the atm-manager subagent. The prompt is an optional `hint: <word>` line (feature, bug, design, spec, chore, investigation, decision, progress, blocker, handoff, question) followed by a freeform message. Note the reply and continue. Do not branch on it.
```

- [ ] **Step 4: Add the same sentence to the Claude and Codex session-start hooks**

In `internal/developing/plugin_assets/claude/hooks/session-start` and `internal/developing/plugin_assets/codex/hooks/session-start`, append the same sentence to the `context` bash string (before the closing quote).

- [ ] **Step 5: Add a "Tracking work via the manager" section to each SKILL.md**

In each of `internal/developing/plugin_assets/{opencode,claude,codex}/skills/atm-developing/SKILL.md`, add before the `## Commands` section:

```markdown
## Tracking work via the manager

To track work, dispatch the `atm-manager` subagent. The prompt is an
optional `hint: <word>` line (`feature`, `bug`, `design`, `spec`,
`chore`, `investigation`, `decision`, `progress`, `blocker`, `handoff`,
`question`) followed by a freeform message describing what you just did,
are about to do, decided, blocked on, or noticed. Note the reply and
continue. Do not branch on it. If the manager is unavailable, note the
track intent in your own context and continue; ledger hygiene is
best-effort.
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/developing -run TestPlugin -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/developing/plugin_assets/ internal/developing/plugins_test.go
git commit -m "Add atm-manager dispatch contract to developing bootstrap"
```

---

## Task 8: Verification and install local smoke

**Goal:** Run the full repository gate, then install the manager plugin locally and confirm `atm manager plugin status` reports `installed` (after the developing plugin is also installed).

**Files:** none (verification only)

- [ ] **Step 1: Run make verify**

Run: `make verify`
Expected: PASS. If the pre-existing `internal/cli` version-golden mismatch (v0.1.1 vs dev testdata) fails, that is a known unrelated failure (ATM-0025 c0009) — confirm it is the only failure and proceed; otherwise fix any new failure introduced by this plan.

- [ ] **Step 2: Build and install the atm binary**

Run: `make build && sudo cp atm /usr/local/bin/atm` (or `go build -o atm ./cmd/atm && sudo cp atm /usr/local/bin/atm`)
Expected: build succeeds.

- [ ] **Step 3: Install the manager and developing plugins locally**

Run: `/usr/local/bin/atm developing plugin install all && /usr/local/bin/atm manager plugin install all`
Expected: each host reports installed/would-install.

- [ ] **Step 4: Confirm manager plugin status**

Run: `/usr/local/bin/atm manager plugin status all`
Expected: `installed` for each host (since developing plugin is now present).

- [ ] **Step 5: Dry-run the interactive manager launcher**

Run: `/usr/local/bin/atm manager opencode --project ATM --dry-run`
Expected: JSON envelope with `role: manager`, `agent: opencode`, a `manager/`-prefixed context path, and the ATM env vars including `ATM_ROLE=manager`.

- [ ] **Step 6: Confirm the OpenCode subagent definition is visible**

Run: `ls ~/.config/opencode/agents/atm-manager.md`
Expected: file exists.

- [ ] **Step 7: Record verification on ATM-0024**

```bash
/usr/local/bin/atm task comment add --task ATM-0024 --body "Implementation complete. make verify passes (modulo the pre-existing version-golden mismatch from ATM-0025). Manager plugin installed locally; atm manager plugin status all reports installed. Dry-run confirmed ATM_ROLE=manager env and manager/ context path." --actor opencode-dev
```

- [ ] **Step 8: Commit any remaining changes**

```bash
git status
# if clean, nothing to commit; the work is already committed per-task.
```

---

## Self-Review

**1. Spec coverage:**
- Driver / Scope (v1): `atm manager` tree (Task 6), two runtime modes (Tasks 2+6), `render-context` (Task 6), installable subagent definitions (Tasks 4+5), ATM env vars incl. `ATM_ROLE=manager` (Task 6), autonomy (Task 2 prompt) — covered.
- Out of Scope: no background daemon, no wire protocol, no auto-project-inference (`--project` required in Task 6), no repo-local config writes (Task 5 installs only to user dirs), no MCP server — respected.
- Command Surface: all five commands present in Task 6.
- Track dispatch contract: Task 7 adds the dispatch contract to the developing bootstrap; the spec's Task-tool example shape is encoded in the SKILL.md additions.
- Manager Prompt: Task 2 embeds the full prompt (spec Appendix).
- Host Adapters: Tasks 4+5 install env-driven markdown subagent definitions to each host's user-level agents dir.
- Plugin Install UX: `installed`/`partial`/`missing` implemented in Task 5 (`partial` = developing plugin absent).
- Internal Architecture: `internal/manager/` mirrors `internal/developing/`; shared helpers extracted in Task 1.
- Error Handling: missing project → `ErrNotFound` + create hint (Task 6); unknown host → Cobra usage (Task 6); missing binary → host hint (Task 1 `runChild`); context write failure → fs error (Task 6); child non-zero → tail + non-zero (Task 6); manager plugin missing → warning (deferred to runtime, but `plugin status` surfaces it); dispatch failure → developing continues (encoded in the SKILL.md text, Task 7).
- Testing: unit/golden tests in Tasks 2, 3, 5, 6; manual smoke in Task 8.
- Open Questions: Codex subagent registration parity is flagged in the spec; the plan installs a markdown agent file and relies on `atm manager plugin status codex` reporting — if Codex cannot register it, the status will surface it at install time. No code change blocks on resolving the open question.

**2. Placeholder scan:** The plan has complete code in every step. The one intentional placeholder (`<CODE>` etc. in the prompt template) is the runtime substitution contract, not a plan placeholder. No "TBD"/"TODO"/"implement later".

**3. Type consistency:** `manager.ContextData`, `manager.Launcher`, `manager.Asset`, `manager.Status`, `manager.InstallResult`, `manager.PluginAssets`, `manager.PluginInstallRoot`, `manager.PluginStatus`, `manager.InstallPlugin` — all defined in Task 2/3/5 and consumed in Task 6 with matching signatures. Shared helpers `newRunID`, `runChild(name, argv, env, hint)`, `assembleEnv(map)`, `emitLaunchHeader(st, role, project, runID, contextPath, agent, argv, env)`, `emitLaunchTail(st, role, project, runID, contextPath, agent, exit)` — defined in Task 1 and called identically in Task 6. `managerEnvValues` returns `map[string]string` matching `assembleEnv`'s parameter.

One inconsistency found and fixed inline: the stray `if !opts.Project == "" {}` block in Task 6 step 1 is explicitly called out for removal in step 4.

Plan complete.
