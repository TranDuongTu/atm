# ATM Developing Agent Launcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `atm developing opencode|codex|claude --project <CODE>` plus opt-in plugin install/status support so day-to-day agent sessions use ATM as the visible work ledger without disrupting normal agent workflows.

**Architecture:** Add a new `internal/developing` package for role-specific launchers, context rendering, and plugin assets. Add a thin `internal/cli/developing.go` command layer that mirrors onboarding's parent-process launcher flow while keeping onboarding conceptually separate. The command sets ATM environment variables and relies on installed per-agent bootstrap plugins to inject concise context only when `ATM_ROLE=developing`.

**Tech Stack:** Go 1.22+, Cobra CLI, embedded markdown/assets via `go:embed`, existing JSON/text output helpers, existing file-based ATM store.

## Global Constraints

- Project is required: `atm developing <agent> --project <CODE>`.
- No project inference in v1.
- `atm developing` must launch the selected agent's normal interactive entrypoint.
- `atm developing` must not silently install plugins or modify agent configuration.
- Plugin installation is explicit and user-scoped.
- Do not write repo-local agent config.
- Do not replace or suppress existing agent system prompts, repo instructions, skills, plugins, MCP servers, approval modes, or sandbox settings.
- Plugin bootstrap activates only when `ATM_ROLE=developing` and `ATM_PROJECT` are present.
- The launcher must not send a visible first user message.
- The command supports only `opencode`, `codex`, and `claude` in v1.
- Repository verification gate is `make verify`.

---

## File Structure

- Create `internal/developing/launcher.go`: agent launcher interface and concrete `OpencodeLauncher`, `CodexLauncher`, `ClaudeLauncher`.
- Create `internal/developing/context.go`: context-template rendering and data struct.
- Create `internal/developing/context_v1.md`: embedded developing context markdown.
- Create `internal/developing/plugins.go`: embedded plugin asset definitions, install/status helpers, user config path resolution.
- Create `internal/developing/plugin_assets/...`: OpenCode, Claude, and Codex bootstrap assets copied by install commands.
- Create `internal/developing/*_test.go`: launcher, context, plugin asset/status tests.
- Create `internal/cli/developing.go`: Cobra command wiring, dry-run/header/tail emission, child process execution with ATM env vars.
- Modify `internal/cli/root.go`: add `newDevelopingCmd(st)`.
- Create `internal/cli/developing_test.go`: golden tests and error-path tests.
- Create golden files in `internal/cli/testdata/golden/`: developing dry-run/status/missing-project fixtures.
- Modify docs only if command help or conventions need a one-line pointer after implementation.

---

### Task 1: Developing Context Renderer and Agent Launchers

**Files:**
- Create: `internal/developing/launcher.go`
- Create: `internal/developing/context.go`
- Create: `internal/developing/context_v1.md`
- Create: `internal/developing/launcher_test.go`
- Create: `internal/developing/context_test.go`

**Interfaces:**
- Produces: `type Launcher interface { Name() string; NotFoundHint() string; BuildArgv() []string }`
- Produces: `func LauncherFor(name string) (Launcher, bool)`
- Produces: `type ContextData struct { Code, Name, ATMBin, Actor, RunID, Timestamp, ExistingTasks string }`
- Produces: `func RenderContext(data ContextData) string`
- Consumes: no new code from other tasks.

- [ ] **Step 1: Write launcher tests**

Add `internal/developing/launcher_test.go`:

```go
package developing

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

- [ ] **Step 2: Run launcher tests and verify they fail**

Run: `go test ./internal/developing -run TestLauncher -count=1`

Expected: FAIL with package or symbol errors because `internal/developing` and `LauncherFor` do not exist.

- [ ] **Step 3: Implement launchers**

Create `internal/developing/launcher.go`:

```go
package developing

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

- [ ] **Step 4: Write context render tests**

Add `internal/developing/context_test.go`:

```go
package developing

import (
	"strings"
	"testing"
)

func TestRenderContextSubstitutesAllPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{
		Code:          "FOO",
		Name:          "Foo Project",
		ATMBin:        "/usr/local/bin/atm",
		Actor:         "codex-dev",
		RunID:         "FOO-20260705120000-a1b2c3",
		Timestamp:     "2026-07-05T12:00:00Z",
		ExistingTasks: "| FOO-0001 | Existing work | FOO:status:open |",
	})
	for _, placeholder := range []string{
		"<CODE>", "<PROJECT_NAME>", "<ATM_BIN>", "<ACTOR>",
		"<RUN_ID>", "<TIMESTAMP>", "<EXISTING_TASKS>",
	} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered context still contains %s", placeholder)
		}
	}
	for _, want := range []string{
		"ATM developing session FOO-20260705120000-a1b2c3",
		"Project: `FOO` (`Foo Project`)",
		"ATM binary: `/usr/local/bin/atm`",
		"Actor: `codex-dev`",
		"atm task comment add --task <ID>",
		"| FOO-0001 | Existing work | FOO:status:open |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q", want)
		}
	}
}

func TestRenderContextDefaultsExistingTasks(t *testing.T) {
	got := RenderContext(ContextData{Code: "FOO", Name: "Foo"})
	if !strings.Contains(got, "(none)") {
		t.Fatalf("rendered context missing default existing task marker")
	}
}
```

- [ ] **Step 5: Run context tests and verify they fail**

Run: `go test ./internal/developing -run TestRenderContext -count=1`

Expected: FAIL with undefined `RenderContext` and `ContextData`.

- [ ] **Step 6: Implement context renderer and template**

Create `internal/developing/context.go`:

```go
package developing

import (
	_ "embed"
	"strings"
)

//go:embed context_v1.md
var contextV1 string

type ContextData struct {
	Code          string
	Name          string
	ATMBin        string
	Actor         string
	RunID         string
	Timestamp     string
	ExistingTasks string
}

func RenderContext(data ContextData) string {
	if data.ExistingTasks == "" {
		data.ExistingTasks = "(none)"
	}
	replacer := strings.NewReplacer(
		"<CODE>", data.Code,
		"<PROJECT_NAME>", data.Name,
		"<ATM_BIN>", data.ATMBin,
		"<ACTOR>", data.Actor,
		"<RUN_ID>", data.RunID,
		"<TIMESTAMP>", data.Timestamp,
		"<EXISTING_TASKS>", data.ExistingTasks,
	)
	return replacer.Replace(contextV1)
}
```

Create `internal/developing/context_v1.md`:

```md
# ATM developing session <RUN_ID>

Project: `<CODE>` (`<PROJECT_NAME>`)
Started: `<TIMESTAMP>`
ATM binary: `<ATM_BIN>`
Actor: `<ACTOR>`

## Role

This is an ATM developing session. Use ATM project `<CODE>` as the visible work ledger during normal software development. Follow repo instructions, existing skills, harness rules, tool permissions, and direct user requests first; ATM records the work, it does not replace the workflow.

## Working routine

1. Before feature, design, spec, bug, chore, or meaningful investigation work, find the relevant task or create one.
2. Record intent and progress as task comments.
3. Add comments for decisions, files changed, test results, blockers, review findings, commit SHAs, and handoff notes.
4. Prefer comments on the relevant task over private-only chat summaries.
5. If instructions conflict, preserve the normal agent/repo instruction hierarchy and use ATM where compatible.

## Commands

- `<ATM_BIN> conventions`
- `<ATM_BIN> label list --project <CODE> --output json`
- `<ATM_BIN> task list --project <CODE> --output json`
- `<ATM_BIN> task show --id <ID> --output json`
- `<ATM_BIN> task create --project <CODE> --title "<title>" --label <CODE>:status:open --actor <ACTOR>`
- `<ATM_BIN> task comment add --task <ID> --body "<progress note>" --actor <ACTOR>`
- `<ATM_BIN> task comment list --task <ID> --output json`

## Existing tasks at session start

<EXISTING_TASKS>
```

- [ ] **Step 7: Run package tests**

Run: `go test ./internal/developing -count=1`

Expected: PASS.

- [ ] **Step 8: Commit Task 1**

```bash
git add internal/developing
git commit -m "feat: add developing context renderer"
```

---

### Task 2: CLI Launch and Dry-Run Surface

**Files:**
- Create: `internal/cli/developing.go`
- Modify: `internal/cli/root.go`
- Create: `internal/cli/developing_test.go`
- Create: `internal/cli/testdata/golden/developing-dry-run-codex.json`
- Create: `internal/cli/testdata/golden/developing-missing-project.json`
- Create: `internal/cli/testdata/golden/developing-tail-summary.json`

**Interfaces:**
- Consumes: `developing.LauncherFor`, `developing.RenderContext`, `developing.ContextData`.
- Produces: Cobra command `newDevelopingCmd(st *cliState) *cobra.Command`.
- Produces: `emitDevelopingHeader`, `emitDevelopingTail`, `normalizeDevelopingOutput` test helper.

- [ ] **Step 1: Write CLI dry-run and missing-project tests**

Create `internal/cli/developing_test.go`:

```go
package cli

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDevelopingCodexDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("developing", "codex", "--project", "FOO", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeDevelopingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "developing-dry-run-codex", got)
}

func TestDevelopingMissingProject(t *testing.T) {
	h := newGoldenHarness(t)
	_, stderrStr, code := h.run("developing", "codex", "--project", "NOPE", "--dry-run")
	if code != ExitNotFound {
		t.Fatalf("exit = %d, want %d", code, ExitNotFound)
	}
	got := normalizeDevelopingOutput(stderrStr, h.store.StorePath())
	compareGolden(t, "developing-missing-project", got)
}

func TestDevelopingTailSummaryJSON(t *testing.T) {
	st := &cliState{flags: globalFlags{output: outputJSON}}
	buf := &bytes.Buffer{}
	st.out = buf
	if err := emitDevelopingTail(st, "codex", "FOO", "FOO-RUNID", "/STORE/developing/FOO-RUNID.md", 0); err != nil {
		t.Fatalf("emitDevelopingTail: %v", err)
	}
	got := normalizeDevelopingOutput(buf.String(), "")
	compareGolden(t, "developing-tail-summary", got)
}

func normalizeDevelopingOutput(s, storePath string) string {
	s = normalizeOutput(s)
	if storePath != "" {
		contextPathRe := regexp.MustCompile(strings.ReplaceAll(filepath.ToSlash(storePath), `/`, `\/`) + `/developing/FOO-\d{14}-[0-9a-f]{6}\.md`)
		s = contextPathRe.ReplaceAllString(s, "/STORE/developing/FOO-RUNID.md")
	}
	runIDRe := regexp.MustCompile(`FOO-\d{14}-[0-9a-f]{6}`)
	s = runIDRe.ReplaceAllString(s, "FOO-RUNID")
	return s
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run: `go test ./internal/cli -run TestDeveloping -count=1`

Expected: FAIL because `developing` command and emit helpers do not exist.

- [ ] **Step 3: Implement CLI command**

Create `internal/cli/developing.go` with this structure:

```go
package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"atm/internal/developing"
	"atm/internal/store"

	"github.com/spf13/cobra"
)

type developingOpts struct {
	Project string
	Actor   string
	DryRun  bool
}

func newDevelopingCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{Use: "developing", Short: "Launch an agent with ATM developing context"}
	for _, name := range []string{"opencode", "codex", "claude"} {
		cmd.AddCommand(newDevelopingAgentCmd(st, name))
	}
	return cmd
}

func newDevelopingAgentCmd(st *cliState, agent string) *cobra.Command {
	var opts developingOpts
	cmd := &cobra.Command{
		Use:   agent,
		Short: "Launch " + agent + " with ATM developing context",
		RunE: func(cmd *cobra.Command, args []string) error {
			l, ok := developing.LauncherFor(agent)
			if !ok {
				return fmt.Errorf("%w: unknown developing agent %q", ErrUsage, agent)
			}
			opts.Actor = defaultDevelopingActor(l.Name(), st, opts.Actor)
			return runDeveloping(st, l, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project to use as the work ledger")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id stamped into ATM commands (default <agent>-dev)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "render context + print argv/env; do not launch")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func defaultDevelopingActor(agent string, st *cliState, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if st.flags.actor != "" {
		return st.flags.actor
	}
	return agent + "-dev"
}
```

Add `runDeveloping`, `runDevelopingChild`, and emit helpers using onboarding's shape. Use `os.Environ()` plus appended ATM env vars:

```go
func developingEnv(project, atmBin, actor, runID, contextPath string) []string {
	return append(os.Environ(),
		"ATM_ROLE=developing",
		"ATM_PROJECT="+project,
		"ATM_BIN="+atmBin,
		"ATM_ACTOR="+actor,
		"ATM_RUN_ID="+runID,
		"ATM_CONTEXT_FILE="+contextPath,
	)
}
```

Use `s.GetProject(opts.Project)` for validation and map missing project to:

```go
return fmt.Errorf("%w: project %s not found; create it first:\n  atm project create --code %s --name \"...\"",
	ErrNotFound, opts.Project, opts.Project)
```

Use `renderExistingTasksTable` from `internal/cli/onboarding.go` for the task snapshot.

- [ ] **Step 4: Wire root command**

Modify `internal/cli/root.go`:

```go
root.AddCommand(newOnboardingCmd(st))
root.AddCommand(newDevelopingCmd(st))
root.AddCommand(newTUICmd(st))
```

- [ ] **Step 5: Generate golden fixtures**

Run: `go test ./internal/cli -run TestDeveloping -count=1 -update`

Expected: FAIL or PASS after writing new golden files. If it fails, inspect only developing failures and fix output shape.

Expected JSON fields for dry-run:

```json
{
  "agent": "codex",
  "argv": ["codex"],
  "context_path": "/STORE/developing/FOO-RUNID.md",
  "env": {
    "ATM_ACTOR": "codex-dev",
    "ATM_BIN": "/PATH/atm",
    "ATM_CONTEXT_FILE": "/STORE/developing/FOO-RUNID.md",
    "ATM_PROJECT": "FOO",
    "ATM_ROLE": "developing",
    "ATM_RUN_ID": "FOO-RUNID"
  },
  "project": "FOO",
  "run_id": "FOO-RUNID"
}
```

If `os.Executable()` introduces an unstable absolute path, normalize it in `normalizeDevelopingOutput` with a regex replacing `"ATM_BIN":"..."` or JSON pretty equivalent with `"/ATM_BIN"`.

- [ ] **Step 6: Run CLI tests**

Run: `go test ./internal/cli -run TestDeveloping -count=1`

Expected: PASS.

- [ ] **Step 7: Commit Task 2**

```bash
git add internal/cli/root.go internal/cli/developing.go internal/cli/developing_test.go internal/cli/testdata/golden/developing-*.json
git commit -m "feat: add developing launcher command"
```

---

### Task 3: Bootstrap Plugin Assets

**Files:**
- Create: `internal/developing/plugin_assets/opencode/atm-developing.js`
- Create: `internal/developing/plugin_assets/claude/.claude-plugin/plugin.json`
- Create: `internal/developing/plugin_assets/claude/hooks/hooks.json`
- Create: `internal/developing/plugin_assets/claude/hooks/session-start`
- Create: `internal/developing/plugin_assets/codex/.codex-plugin/plugin.json`
- Create: `internal/developing/plugin_assets/codex/hooks/hooks.json`
- Create: `internal/developing/plugin_assets/codex/hooks/session-start`
- Create: `internal/developing/plugin_assets/codex/skills/atm-developing/SKILL.md`
- Create: `internal/developing/plugins.go`
- Create: `internal/developing/plugins_test.go`

**Interfaces:**
- Produces: `type AgentPlugin string`
- Produces: `func PluginAssets(agent string) ([]Asset, bool)`
- Produces: `type Asset struct { Path string; Mode fs.FileMode; Content []byte }`
- Consumes: environment variables set by Task 2.

- [ ] **Step 1: Write plugin asset tests**

Add `internal/developing/plugins_test.go`:

```go
package developing

import (
	"strings"
	"testing"
)

func TestPluginAssetsExistForSupportedAgents(t *testing.T) {
	for _, agent := range []string{"opencode", "claude", "codex"} {
		assets, ok := PluginAssets(agent)
		if !ok {
			t.Fatalf("PluginAssets(%q) ok=false", agent)
		}
		if len(assets) == 0 {
			t.Fatalf("PluginAssets(%q) returned no files", agent)
		}
	}
}

func TestPluginAssetsStaySilentWithoutATMRole(t *testing.T) {
	for _, agent := range []string{"opencode", "claude", "codex"} {
		assets, _ := PluginAssets(agent)
		joined := string(joinAssetContents(assets))
		if !strings.Contains(joined, "ATM_ROLE") {
			t.Errorf("%s assets do not check ATM_ROLE", agent)
		}
		if !strings.Contains(joined, "ATM_PROJECT") {
			t.Errorf("%s assets do not check ATM_PROJECT", agent)
		}
	}
}

func TestPluginAssetsContainLedgerLanguage(t *testing.T) {
	assets, _ := PluginAssets("claude")
	joined := string(joinAssetContents(assets))
	for _, want := range []string{"visible work ledger", "task comments", "ATM_CONTEXT_FILE"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Claude assets missing %q", want)
		}
	}
}

func joinAssetContents(assets []Asset) []byte {
	var out []byte
	for _, a := range assets {
		out = append(out, a.Content...)
		out = append(out, '\n')
	}
	return out
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run: `go test ./internal/developing -run TestPluginAssets -count=1`

Expected: FAIL with undefined `PluginAssets` and `Asset`.

- [ ] **Step 3: Add OpenCode plugin asset**

Create `internal/developing/plugin_assets/opencode/atm-developing.js`:

```js
const bootstrap = () => {
  const role = process.env.ATM_ROLE
  const project = process.env.ATM_PROJECT
  if (role !== "developing" || !project) return null

  const atm = process.env.ATM_BIN || "atm"
  const contextFile = process.env.ATM_CONTEXT_FILE || ""
  return `<ATM_DEVELOPING_CONTEXT>
This is an ATM developing session for project ${project}.
Use ATM as the visible work ledger for feature, design, spec, bug, chore, and investigation work.
Use ${atm} for ATM commands.
Find or create a relevant task before substantial work, then record intentions, progress, decisions, test results, commit references, blockers, and handoff notes as task comments.
Follow repo instructions, existing skills, harness rules, tool permissions, and user directions first; ATM records the work, it does not replace the workflow.
More context: ${contextFile}
</ATM_DEVELOPING_CONTEXT>`
}

export const ATMDevelopingPlugin = async () => {
  return {
    "experimental.chat.messages.transform": async (_input, output) => {
      const context = bootstrap()
      if (!context || !output.messages.length) return
      const firstUser = output.messages.find((m) => m.info.role === "user")
      if (!firstUser || !firstUser.parts.length) return
      if (firstUser.parts.some((p) => p.type === "text" && p.text.includes("ATM_DEVELOPING_CONTEXT"))) return
      const ref = firstUser.parts[0]
      firstUser.parts.unshift({ ...ref, type: "text", text: context })
    },
    "shell.env": async (_input, output) => {
      if (process.env.ATM_ROLE !== "developing") return
      for (const key of ["ATM_ROLE", "ATM_PROJECT", "ATM_BIN", "ATM_CONTEXT_FILE", "ATM_ACTOR", "ATM_RUN_ID"]) {
        if (process.env[key]) output.env[key] = process.env[key]
      }
    },
  }
}
```

- [ ] **Step 4: Add Claude plugin assets**

Create `internal/developing/plugin_assets/claude/.claude-plugin/plugin.json`:

```json
{
  "name": "atm-developing",
  "description": "Session bootstrap for ATM developing sessions",
  "version": "0.1.0",
  "author": {
    "name": "ATM"
  }
}
```

Create `internal/developing/plugin_assets/claude/hooks/hooks.json`:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume|clear|compact",
        "hooks": [
          {
            "type": "command",
            "command": "${CLAUDE_PLUGIN_ROOT}/hooks/session-start",
            "args": [],
            "timeout": 10
          }
        ]
      }
    ]
  }
}
```

Create `internal/developing/plugin_assets/claude/hooks/session-start`:

```bash
#!/usr/bin/env bash
set -euo pipefail

if [ "${ATM_ROLE:-}" != "developing" ] || [ -z "${ATM_PROJECT:-}" ]; then
  printf '{}\n'
  exit 0
fi

escape_json() {
  local s="$1"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  s="${s//$'\n'/\\n}"
  s="${s//$'\r'/\\r}"
  s="${s//$'\t'/\\t}"
  printf '%s' "$s"
}

context="This is an ATM developing session for project ${ATM_PROJECT}. Use ATM as the visible work ledger. Use ${ATM_BIN:-atm} for ATM commands. Find or create a relevant task before substantial feature, design, spec, bug, chore, or investigation work. Record intentions, progress, decisions, test results, commit references, blockers, and handoff notes as task comments. Follow repo instructions, existing skills, harness rules, tool permissions, and user directions first; ATM records the work, it does not replace the workflow. More context: ${ATM_CONTEXT_FILE:-}"
escaped="$(escape_json "$context")"

printf '{\n  "hookSpecificOutput": {\n    "hookEventName": "SessionStart",\n    "additionalContext": "%s"\n  }\n}\n' "$escaped"
```

- [ ] **Step 5: Add Codex plugin assets**

Create `internal/developing/plugin_assets/codex/.codex-plugin/plugin.json`:

```json
{
  "name": "atm-developing",
  "version": "0.1.0",
  "description": "Session bootstrap and skill for ATM developing sessions",
  "author": {
    "name": "ATM"
  },
  "skills": "./skills/",
  "interface": {
    "displayName": "ATM Developing",
    "shortDescription": "Use ATM tasks and comments as a development ledger",
    "longDescription": "Adds a small ATM developing reminder when Codex is launched by atm developing.",
    "developerName": "ATM",
    "category": "Developer Tools",
    "capabilities": ["Interactive", "Read", "Write"],
    "defaultPrompt": ["Use ATM as the work ledger for this development session."]
  }
}
```

Create `internal/developing/plugin_assets/codex/hooks/hooks.json`:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume|clear|compact",
        "hooks": [
          {
            "type": "command",
            "command": "${CODEX_PLUGIN_ROOT}/hooks/session-start",
            "timeout": 10,
            "statusMessage": "Loading ATM developing context"
          }
        ]
      }
    ]
  }
}
```

Create `internal/developing/plugin_assets/codex/hooks/session-start`:

```bash
#!/usr/bin/env bash
set -euo pipefail

if [ "${ATM_ROLE:-}" != "developing" ] || [ -z "${ATM_PROJECT:-}" ]; then
  printf '{}\n'
  exit 0
fi

escape_json() {
  local s="$1"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  s="${s//$'\n'/\\n}"
  s="${s//$'\r'/\\r}"
  s="${s//$'\t'/\\t}"
  printf '%s' "$s"
}

context="This is an ATM developing session for project ${ATM_PROJECT}. Use ATM as the visible work ledger. Use ${ATM_BIN:-atm} for ATM commands. Find or create a relevant task before substantial feature, design, spec, bug, chore, or investigation work. Record intentions, progress, decisions, test results, commit references, blockers, and handoff notes as task comments. Follow repo instructions, existing skills, harness rules, tool permissions, and user directions first; ATM records the work, it does not replace the workflow. More context: ${ATM_CONTEXT_FILE:-}"
escaped="$(escape_json "$context")"

printf '{\n  "additionalContext": "%s"\n}\n' "$escaped"
```

Create `internal/developing/plugin_assets/codex/skills/atm-developing/SKILL.md`:

```md
---
name: atm-developing
description: Use when ATM_ROLE=developing or when working in an ATM-linked development session.
---

When `ATM_ROLE=developing`, use ATM as the visible work ledger for the session.

- Use `$ATM_BIN` or `atm` for commands.
- Use `$ATM_PROJECT` as the project code.
- Before feature, design, spec, bug, chore, or meaningful investigation work, find or create the relevant task.
- Record intentions, progress, decisions, test results, commit references, blockers, and handoff notes as task comments.
- Follow repo instructions, existing skills, harness rules, tool permissions, and user directions first. ATM records the work; it does not replace the workflow.
```

- [ ] **Step 6: Implement embedded asset loader**

Create `internal/developing/plugins.go`:

```go
package developing

import (
	"embed"
	"io/fs"
	"path/filepath"
)

// Dot-prefixed plugin manifest directories are embedded explicitly so Go does
// not skip them while expanding a broad directory pattern.
//go:embed plugin_assets/opencode/atm-developing.js
//go:embed plugin_assets/claude/.claude-plugin/plugin.json
//go:embed plugin_assets/claude/hooks/hooks.json
//go:embed plugin_assets/claude/hooks/session-start
//go:embed plugin_assets/codex/.codex-plugin/plugin.json
//go:embed plugin_assets/codex/hooks/hooks.json
//go:embed plugin_assets/codex/hooks/session-start
//go:embed plugin_assets/codex/skills/atm-developing/SKILL.md
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
		mode := fs.FileMode(0o644)
		if filepath.Base(path) == "session-start" {
			mode = 0o755
		}
		assets = append(assets, Asset{
			Path:    filepath.ToSlash(rel),
			Mode:    mode,
			Content: b,
		})
		return nil
	})
	if err != nil {
		return nil, false
	}
	return assets, true
}
```

- [ ] **Step 7: Run asset tests**

Run: `go test ./internal/developing -run TestPluginAssets -count=1`

Expected: PASS.

- [ ] **Step 8: Commit Task 3**

```bash
git add internal/developing
git commit -m "feat: add developing plugin assets"
```

---

### Task 4: Plugin Install and Status Commands

**Files:**
- Modify: `internal/developing/plugins.go`
- Create: `internal/developing/plugin_install_test.go`
- Modify: `internal/cli/developing.go`
- Modify: `internal/cli/developing_test.go`
- Create: `internal/cli/testdata/golden/developing-plugin-status.json`
- Create: `internal/cli/testdata/golden/developing-plugin-install-dry-run.json`

**Interfaces:**
- Produces: `func PluginInstallRoot(agent string, home string) (string, bool)`
- Produces: `func PluginStatus(agent string, home string) Status`
- Produces: `func InstallPlugin(agent string, home string, dryRun bool) (InstallResult, error)`
- Produces: CLI commands `atm developing plugin status` and `atm developing plugin install`.

- [ ] **Step 1: Write install/status unit tests**

Create `internal/developing/plugin_install_test.go`:

```go
package developing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginInstallRoot(t *testing.T) {
	home := "/home/tester"
	tests := map[string]string{
		"opencode": filepath.Join(home, ".config", "opencode", "plugins", "atm-developing.js"),
		"claude":   filepath.Join(home, ".claude", "skills", "atm-developing"),
		"codex":    filepath.Join(home, ".codex", "plugins", "atm-developing"),
	}
	for agent, want := range tests {
		got, ok := PluginInstallRoot(agent, home)
		if !ok {
			t.Fatalf("PluginInstallRoot(%q) ok=false", agent)
		}
		if got != want {
			t.Errorf("PluginInstallRoot(%q) = %q, want %q", agent, got, want)
		}
	}
}

func TestInstallPluginDryRunDoesNotWrite(t *testing.T) {
	home := t.TempDir()
	res, err := InstallPlugin("claude", home, true)
	if err != nil {
		t.Fatalf("InstallPlugin dry-run: %v", err)
	}
	if len(res.Files) == 0 {
		t.Fatal("dry-run result has no files")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "atm-developing")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote plugin dir: %v", err)
	}
}

func TestInstallPluginWritesAssetsAndStatusInstalled(t *testing.T) {
	home := t.TempDir()
	if _, err := InstallPlugin("claude", home, false); err != nil {
		t.Fatalf("InstallPlugin: %v", err)
	}
	status := PluginStatus("claude", home)
	if status.State != "installed" {
		t.Fatalf("status = %q, want installed", status.State)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "atm-developing", ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("plugin manifest missing: %v", err)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run: `go test ./internal/developing -run 'TestPluginInstall|TestInstallPlugin' -count=1`

Expected: FAIL with undefined install/status symbols.

- [ ] **Step 3: Implement install/status helpers**

Add to `internal/developing/plugins.go`:

```go
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

func PluginInstallRoot(agent string, home string) (string, bool) {
	switch agent {
	case "opencode":
		return filepath.Join(home, ".config", "opencode", "plugins", "atm-developing.js"), true
	case "claude":
		return filepath.Join(home, ".claude", "skills", "atm-developing"), true
	case "codex":
		return filepath.Join(home, ".codex", "plugins", "atm-developing"), true
	default:
		return "", false
	}
}

func PluginStatus(agent string, home string) Status {
	root, ok := PluginInstallRoot(agent, home)
	if !ok {
		return Status{Agent: agent, State: "unknown"}
	}
	if _, err := os.Stat(root); err == nil {
		return Status{Agent: agent, State: "installed", Path: root}
	}
	return Status{Agent: agent, State: "missing", Path: root}
}
```

Implement `InstallPlugin`:

```go
func InstallPlugin(agent string, home string, dryRun bool) (InstallResult, error) {
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
		dst := root
		if agent != "opencode" {
			dst = filepath.Join(root, filepath.FromSlash(a.Path))
		}
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

Add imports: `fmt`, `os`.

- [ ] **Step 4: Run install/status unit tests**

Run: `go test ./internal/developing -run 'TestPluginInstall|TestInstallPlugin|TestPluginStatus' -count=1`

Expected: PASS.

- [ ] **Step 5: Add CLI plugin subcommands and tests**

Extend `internal/cli/developing.go`:

```go
func newDevelopingPluginCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{Use: "plugin", Short: "Manage ATM developing agent plugins"}
	cmd.AddCommand(newDevelopingPluginStatusCmd(st))
	cmd.AddCommand(newDevelopingPluginInstallCmd(st))
	return cmd
}
```

In `newDevelopingCmd`, add:

```go
cmd.AddCommand(newDevelopingPluginCmd(st))
```

Status command behavior:

```go
atm developing plugin status all
atm developing plugin status codex
```

If no argument is given, default to `all`. Use `os.UserHomeDir()` for home. Emit JSON:

```json
{"plugins":[{"agent":"codex","state":"missing","path":"/HOME/.codex/plugins/atm-developing"}]}
```

Install command behavior:

```go
atm developing plugin install all --dry-run
atm developing plugin install claude
```

Emit JSON:

```json
{"installed":[{"agent":"claude","path":"/HOME/.claude/skills/atm-developing","files":["/HOME/.claude/skills/atm-developing/.claude-plugin/plugin.json"],"dry_run":true}]}
```

Add tests to `internal/cli/developing_test.go` using `t.Setenv("HOME", h.t.TempDir())` before command execution:

```go
func TestDevelopingPluginStatusJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	_, _, code := h.run("developing", "plugin", "status", "codex")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeHome(h.stdout.String(), home)
	compareGolden(t, "developing-plugin-status", got)
}
```

Add `normalizeHome` helper:

```go
func normalizeHome(s, home string) string {
	return strings.ReplaceAll(normalizeOutput(s), filepath.ToSlash(home), "/HOME")
}
```

- [ ] **Step 6: Generate plugin command goldens**

Run: `go test ./internal/cli -run TestDevelopingPlugin -count=1 -update`

Expected: PASS after golden files are created.

- [ ] **Step 7: Run developing CLI tests**

Run: `go test ./internal/cli -run TestDeveloping -count=1`

Expected: PASS.

- [ ] **Step 8: Commit Task 4**

```bash
git add internal/developing internal/cli/developing.go internal/cli/developing_test.go internal/cli/testdata/golden/developing-plugin-*.json
git commit -m "feat: add developing plugin install commands"
```

---

### Task 5: Child Process Environment and Missing Binary Errors

**Files:**
- Modify: `internal/cli/developing.go`
- Modify: `internal/cli/developing_test.go`
- Create: `internal/cli/testdata/golden/developing-launcher-not-found.json`

**Interfaces:**
- Consumes: Task 2 `runDevelopingChild` and `developingEnv`.
- Produces: tested error behavior when selected agent binary is absent.

- [ ] **Step 1: Add tests for environment construction**

Add to `internal/cli/developing_test.go`:

```go
func TestDevelopingEnvIncludesATMValues(t *testing.T) {
	got := developingEnv("FOO", "/bin/atm", "codex-dev", "FOO-RUNID", "/tmp/context.md")
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"ATM_ROLE=developing",
		"ATM_PROJECT=FOO",
		"ATM_BIN=/bin/atm",
		"ATM_ACTOR=codex-dev",
		"ATM_RUN_ID=FOO-RUNID",
		"ATM_CONTEXT_FILE=/tmp/context.md",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("developing env missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Add missing binary test**

Add:

```go
func TestDevelopingLauncherNotFound(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, stderrStr, code := h.run("developing", "codex", "--project", "FOO")
	if code != ExitGeneric {
		t.Fatalf("exit = %d, want %d", code, ExitGeneric)
	}
	got := normalizeDevelopingOutput(stderrStr, h.store.StorePath())
	compareGolden(t, "developing-launcher-not-found", got)
}
```

- [ ] **Step 3: Run tests and verify failure if behavior is missing**

Run: `go test ./internal/cli -run 'TestDevelopingEnv|TestDevelopingLauncherNotFound' -count=1`

Expected: PASS if Task 2 already implemented these paths, otherwise FAIL showing missing helper or wrong error text.

- [ ] **Step 4: Fix child process behavior**

Ensure `runDevelopingChild` matches this shape:

```go
func runDevelopingChild(l developing.Launcher, argv []string, env []string) (int, error) {
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return 0, fmt.Errorf("%s not found on PATH; install: %s", l.Name(), l.NotFoundHint())
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
```

Generate the new golden:

Run: `go test ./internal/cli -run TestDevelopingLauncherNotFound -count=1 -update`

Expected: PASS and creates `developing-launcher-not-found.json`.

- [ ] **Step 5: Run focused CLI tests**

Run: `go test ./internal/cli -run TestDeveloping -count=1`

Expected: PASS.

- [ ] **Step 6: Commit Task 5**

```bash
git add internal/cli/developing.go internal/cli/developing_test.go internal/cli/testdata/golden/developing-launcher-not-found.json
git commit -m "test: cover developing launcher execution errors"
```

---

### Task 6: Help Text, Full Verification, and Final Review

**Files:**
- Modify: `README.md` only if command list/help examples need an entry.
- Modify: `internal/cli/conventions.go` only if a short pointer to `atm developing` belongs in agent first-contact text.
- Test: all changed packages.

**Interfaces:**
- Consumes: all previous tasks.
- Produces: repository-ready implementation.

- [ ] **Step 1: Run full test suite before docs polish**

Run: `make test`

Expected: PASS.

- [ ] **Step 2: Inspect CLI help output**

Run:

```bash
go run ./cmd/atm --help
go run ./cmd/atm developing --help
go run ./cmd/atm developing plugin --help
```

Expected:

- Root help lists `developing`.
- `developing` help lists `opencode`, `codex`, `claude`, and `plugin`.
- `plugin` help lists `status` and `install`.

- [ ] **Step 3: Add documentation only if help lacks discoverability**

If README already lists major commands, add this line under CLI usage:

```md
- `atm developing <opencode|codex|claude> --project <CODE>` launches a normal interactive agent session with ATM developing context.
```

If `internal/cli/conventions.go` has an "Agent first-contact sequence" block, add one short paragraph after that sequence:

```go
For day-to-day development, start the agent through `atm developing <agent> --project <CODE>` after installing the ATM developing plugin. The command preserves the agent's normal workflow and adds ATM ledger context for the session.
```

- [ ] **Step 4: Run formatting**

Run:

```bash
gofmt -w internal/developing/*.go internal/cli/developing.go internal/cli/developing_test.go internal/cli/root.go
```

Expected: command exits 0.

- [ ] **Step 5: Run repository verification**

Run: `make verify`

Expected: PASS.

- [ ] **Step 6: Inspect git diff**

Run:

```bash
git status --short
git diff --stat
git diff --check
```

Expected:

- Only intended files changed.
- `git diff --check` exits 0.

- [ ] **Step 7: Final commit**

```bash
git add README.md internal/cli/conventions.go internal/cli/root.go internal/cli/developing.go internal/cli/developing_test.go internal/cli/testdata/golden internal/developing
git commit -m "docs: document developing launcher"
```

If README and conventions did not need edits, commit only verification-related generated or formatting changes that remain. If no files remain, skip this commit and record that Task 6 was verification-only.

---

## Implementation Notes

- Keep `internal/onboard` unchanged except for shared helper extraction only if duplication becomes painful during implementation.
- Do not rename onboarding functions in the same change. Shared helpers can be introduced in a separate small commit if needed.
- Prefer JSON output maps with stable keys matching onboarding style.
- Normalize run-specific paths in tests instead of hardcoding temporary directories.
- Plugin hook scripts must print only JSON on stdout.
- OpenCode plugin must guard duplicate context insertion.
- Claude and Codex hook scripts must print `{}` when ATM env vars are absent.
- The Codex adapter is allowed to require hook trust. If local testing shows additional context is not consumed, keep the skill asset and emit a warning in `plugin status` as `unknown` instead of claiming `installed`.
