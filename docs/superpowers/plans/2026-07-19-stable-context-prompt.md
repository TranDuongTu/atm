# Stable Context Prompt Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop regenerating the developing/manager system prompt every launch — render once per `(project, persona, action, capability)` tuple into a stable file under `projects/<CODE>/cache/` and reuse byte-for-byte.

**Architecture:** Drop `<RUN_ID>`, `<TIMESTAMP>`, `<ATM_BIN>` from both context templates and `ContextData`; replace `os.Executable()` with an `exec.LookPath("atm")` guard at launch; write the rendered prompt to `projects/<CODE>/cache/<role>-<persona>[-<action>[-<capability>]].md` only when content differs from the existing file. Old top-level `$ATM_HOME/developing/` and `$ATM_HOME/manager/` dirs are abandoned (manual cleanup by user).

**Tech Stack:** Go 1.22+, cobra, embed, os/exec, internal/developing + internal/manager packages, internal/cli launchers, internal/store under `$ATM_HOME`.

## Global Constraints

- Verify gate: `make verify` must pass before any task is marked done.
- No emojis in code, tests, or commit messages.
- Follow existing style in neighboring files.
- Run tests with: `go test ./internal/...` after each task; the full gate is `make verify`.
- Each task ends with `git add` of the exact files it touched plus a `git commit -m "..."` per the message style shown.
- The hidden `atm manage-context` command stays working: with no `--project`, its existing "leave placeholders" behavior is preserved (only `<ATM_BIN>` becomes a literal `atm`).
- The cache dir `projects/<CODE>/cache/` is not event-log-managed; `atm store rebuild` / `verify` ignore it.
- TUI does not display context files; no TUI changes.
- Stable filename character rules: lowercase; non-alphanumeric → `-`. Persona/action/capability are already restricted to lowercase + hyphens by the registry, so normalization is defensive only.

---

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `internal/developing/context_v1.md` | Modify | Drop `<RUN_ID>`, `<TIMESTAMP>`, `<ATM_BIN>` placeholders and the `· atm ...` header segment. |
| `internal/developing/context.go` | Modify | Drop `ATMBin`, `RunID`, `Timestamp` from `ContextData`; drop matching replacer pairs. |
| `internal/developing/context_test.go` | Modify | Drop `ATMBin`/`RunID`/`Timestamp` from `ContextData` literals; drop `<ATM_BIN>` from placeholder list; drop absolute-path assertion; add bare-`atm` assertion and "no `<RUN_ID>`/`<TIMESTAMP>`" assertion. |
| `internal/manager/context_v1.md` | Modify | Drop `<ATM_BIN>` and the `· atm ...` header segment. |
| `internal/manager/context.go` | Modify | Drop `ATMBin`, `RunID`, `Timestamp` from `ContextData`; replace `binOr`/`ATMBin` references with literal `"atm"` in the action block; drop matching replacer pairs. |
| `internal/manager/context_test.go` | Modify | Parallel changes to the developing test; assert action block uses literal `atm`. |
| `internal/cli/developing.go` | Modify | Replace `os.Executable()` with `exec.LookPath("atm")` guard; drop `ATMBin` field in `ContextData`; compute stable `contextPath` under `projects/<CODE>/cache/dev-<persona>.md`; render → write-if-diff; drop `ATM_BIN` from `developingEnvValues`; add `ATM_TIMESTAMP`. |
| `internal/cli/manager.go` | Modify | Same as developing.go for the manager path; drop `atmBinPath()`; `manage-context` uses literal `"atm"` instead of `atmBinPath()`. |
| `internal/cli/launcher_shared.go` | Modify (small) | Add shared helper `writeContextIfDiff(path string, content []byte) error` and `contextCachePath(storePath, code, role, persona, action, capability string) string`. |
| `internal/cli/developing_test.go` | Modify | Drop `ATM_BIN`/runID/path regexes from `normalizeDevelopingOutput`; drop `ATM_BIN=/bin/atm` assertion; add `ATM_TIMESTAMP` assertion; regenerate goldens. |
| `internal/cli/manager_test.go` | Modify | Same normalization changes; update env-value test; drop `<ATM_BIN>` placeholder assertion in `TestManageContextGenericKeepsPlaceholders` and `TestManageContextFillsProjectName`; regenerate goldens. |
| `internal/cli/testdata/golden/developer-codex-launch.json` | Regenerate | New stable path, env without `ATM_BIN`, with `ATM_TIMESTAMP`. |
| `internal/cli/testdata/golden/manage-codex-autopilot-launch.json` | Regenerate | Same. |
| `internal/cli/testdata/golden/manage-codex-action-brief-launch.json` | Regenerate | Same. |
| `internal/cli/testdata/golden/developing-launcher-not-found.json` | (no change expected) | Already an error envelope; verify still matches. |
| `internal/cli/testdata/golden/developing-tail-summary.json` | (verify) | Tail signature unchanged; verify no `ATM_BIN` field present. |
| `internal/cli/developing_test.go` (new tests) | Add | Write-if-diff (developing) test; PATH-guard test. |
| `internal/cli/manager_test.go` (new tests) | Add | Write-if-diff (manager) test; PATH-guard test. |

---

## Task 1: Strip volatile placeholders from developing context template and renderer

**Files:**
- Modify: `internal/developing/context_v1.md`
- Modify: `internal/developing/context.go:12-43`
- Test: `internal/developing/context_test.go`

**Interfaces:**
- Produces: `developing.ContextData` without `ATMBin`, `RunID`, `Timestamp` fields. `developing.RenderContext(data ContextData) string` no longer substitutes these placeholders.

- [ ] **Step 1: Write the failing test**

Replace `internal/developing/context_test.go` with the version that drops the dropped fields and asserts the new invariants. The whole file:

```go
package developing

import (
	"strings"
	"testing"
)

func TestRenderContextSubstitutesAllPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{
		Code:      "FOO",
		Name:      "Foo Project",
		Actor:     "codex-dev",
	})
	for _, placeholder := range []string{
		"<CODE>", "<PROJECT_NAME>", "<ACTOR>",
	} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered context still contains %s", placeholder)
		}
	}
	for _, placeholder := range []string{
		"<RUN_ID>", "<TIMESTAMP>", "<ATM_BIN>",
	} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered context still contains volatile placeholder %s", placeholder)
		}
	}
	for _, want := range []string{
		"# ATM developing session",
		"Project `FOO` (`Foo Project`)",
		"actor `codex-dev`",
		"atm conventions",
		"capability list --project FOO",
		"search --project FOO",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q", want)
		}
	}
	if strings.Contains(got, "/usr/local/bin/atm") {
		t.Errorf("rendered context must not contain an absolute atm path")
	}
}

func TestRenderContext_Persona(t *testing.T) {
	out := RenderContext(ContextData{
		Code: "ATM", Name: "ATM", Actor: "staff@claude",
		Persona: "staff", PersonaPrompt: "hold a high bar",
	})
	if !strings.Contains(out, "Persona: staff") || !strings.Contains(out, "hold a high bar") {
		t.Fatalf("persona block missing:\n%s", out)
	}

	out2 := RenderContext(ContextData{Code: "ATM", Name: "ATM", Actor: "claude-dev"})
	if strings.Contains(out2, "## Persona") {
		t.Fatalf("no-persona render should omit persona block:\n%s", out2)
	}
}

func TestRenderContextPromptsJournaling(t *testing.T) {
	got := RenderContext(ContextData{Code: "ATM", Name: "Agent Tasks Management", Actor: "ollama-dev"})
	for _, frag := range []string{
		"atm search",
		"visible ledger",
		"Journal",
	} {
		if !strings.Contains(got, frag) {
			t.Errorf("developing context missing %q", frag)
		}
	}
}

func TestRenderContextIncludesPersonaDescription(t *testing.T) {
	out := RenderContext(ContextData{
		Persona:            "developer",
		PersonaPrompt:      "do good work",
		PersonaDescription: "Default working persona.",
		Actor:              "developer@claude:unset",
	})
	for _, want := range []string{"developer", "Default working persona.", "do good work"} {
		if !strings.Contains(out, want) {
			t.Errorf("context missing %q", want)
		}
	}
}

func TestRenderContextModelStampInstruction(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM", Name: "ATM", Actor: "developer@claude:unset"})
	if !strings.Contains(out, ":unset") {
		t.Errorf("context missing model-stamp instruction referencing :unset")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/developing/ -run TestRenderContext -v`
Expected: FAIL — `ContextData` still has `ATMBin` etc. (compile error: unknown fields `Actor` only is fine, but `ATMBin`/`RunID`/`Timestamp` not removed yet). The exact failure is a compile error: the test still passes only the three fields, but `ContextData` still requires nothing; the asserts will fail because `<ATM_BIN>` survives in the template.

- [ ] **Step 3: Update the template**

Replace `internal/developing/context_v1.md` with:

```markdown
# ATM developing session

Project `<CODE>` (`<PROJECT_NAME>`) · actor `<ACTOR>`

<PERSONA_BLOCK>

## Orientation

ATM is the visible ledger for this work. Use it to record ideas, discussions, decisions, and progress as you go, and to find prior work and handoffs from earlier sessions. Start with the CLI landscape, read the conventions, then discover which capabilities this project has enabled and read each one's guide.

```
atm -h                                # general CLI landscape
atm conventions                       # what ATM is, the label substrate, the actor convention
atm capability list --project <CODE>  # which capabilities this project has enabled
atm capability <name> guide           # how to use one capability (Brief + Autopilot + reference)
atm search --project <CODE> "..."     # find prior tasks, decisions, and handoffs before starting
```

Run `atm <cmd> --help` for exact flags.

## Working Principles

- Respect the repository's existing process. ATM complements it; do not let it intrude or override project-specific prompts and workflows.
- Do the work and tell people. Journal frequently — ideas, decisions, and progress recorded now save a future agent from re-deriving them.
```

(Note the file ends with a trailing newline after "re-deriving them." — preserve the original's final newline.)

- [ ] **Step 4: Update the renderer**

Replace `internal/developing/context.go` with:

```go
package developing

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed context_v1.md
var contextV1 string

type ContextData struct {
	Code          string
	Name          string
	Actor         string
	Persona       string
	PersonaPrompt string

	// PersonaDescription is the persona's human-readable description, rendered
	// into the persona block alongside the prompt so the agent sees both.
	PersonaDescription string
}

func RenderContext(data ContextData) string {
	personaBlock := ""
	if data.Persona != "" {
		personaBlock = fmt.Sprintf("## Persona: %s\n\n%s\n\n%s\n\nYou are operating as this persona. Hold to its principles throughout the session, alongside repo instructions and the working routine below.\n",
			data.Persona, data.PersonaDescription, data.PersonaPrompt)
	}
	replacer := strings.NewReplacer(
		"<CODE>", data.Code,
		"<PROJECT_NAME>", data.Name,
		"<ACTOR>", data.Actor,
		"<PERSONA_BLOCK>", personaBlock,
	)
	return replacer.Replace(contextV1)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/developing/ -run TestRenderContext -v`
Expected: PASS

- [ ] **Step 6: Run package tests**

Run: `go test ./internal/developing/ -v`
Expected: PASS (other tests in the package may break if they reference `ATMBin`/`RunID`/`Timestamp`; fix in Task 3 if needed, but they should not, since only `context_test.go` references them directly).

- [ ] **Step 7: Commit**

```bash
git add internal/developing/context_v1.md internal/developing/context.go internal/developing/context_test.go
git commit -m "refactor(ATM-4afb54): drop RUN_ID/TIMESTAMP/ATM_BIN from developing context template"
```

---

## Task 2: Strip volatile placeholders from manager context template and renderer

**Files:**
- Modify: `internal/manager/context_v1.md`
- Modify: `internal/manager/context.go:12-102`
- Test: `internal/manager/context_test.go`

**Interfaces:**
- Produces: `manager.ContextData` without `ATMBin`, `RunID`, `Timestamp`. `manager.RenderContext` builds the action block with literal `"atm"` and no longer references `binOr` or `ATMBin`.

- [ ] **Step 1: Read existing manager context test**

Run: `sed -n '1,200p' internal/manager/context_test.go` (read in editor)
Purpose: see which assertions reference `ATMBin`, `<ATM_BIN>`, `RunID`, `Timestamp` so the rewrite keeps coverage.

- [ ] **Step 2: Write the failing test**

Append (or replace if already present) the following assertions to `internal/manager/context_test.go`. If the file already has `TestRenderContextSubstitutesAllPlaceholders`, replace it. If it does not, add a new test:

```go
func TestRenderContextSubstitutesAllPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{
		Code:      "FOO",
		Name:      "Foo Project",
		Actor:     "manager@codex:unset",
		Action:    "autopilot",
	})
	for _, placeholder := range []string{
		"<CODE>", "<PROJECT_NAME>", "<ACTOR>", "<ACTION_BLOCK>",
	} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered context still contains %s", placeholder)
		}
	}
	for _, placeholder := range []string{
		"<RUN_ID>", "<TIMESTAMP>", "<ATM_BIN>",
	} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered context still contains volatile placeholder %s", placeholder)
		}
	}
	for _, want := range []string{
		"# ATM manager — FOO",
		"Project `FOO` (`Foo Project`)",
		"actor `manager@codex:unset`",
		"atm conventions",
		"atm capability list --project FOO",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q", want)
		}
	}
	if strings.Contains(got, "/usr/local/bin/atm") {
		t.Errorf("rendered context must not contain an absolute atm path")
	}
	// The action block builds commands with the literal "atm", not <ATM_BIN>.
	if !strings.Contains(got, "atm capability <name> guide") {
		t.Errorf("action block should reference literal `atm capability <name> guide`")
	}
}

func TestRenderContextGenericKeepsPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{})
	for _, placeholder := range []string{"<CODE>", "<ACTOR>"} {
		if !strings.Contains(got, placeholder) {
			t.Errorf("generic render stripped %s", placeholder)
		}
	}
	// <ATM_BIN> is gone from the template; the generic render must not contain it.
	if strings.Contains(got, "<ATM_BIN>") {
		t.Errorf("generic render must not contain <ATM_BIN>; literal `atm` is used")
	}
}
```

Drop any existing assertions in this file that reference `ATMBin`, `RunID`, or `Timestamp` in a `ContextData{}` literal — those literals must drop the dropped fields. Drop any assertion that the rendered body contains `<ATM_BIN>` or an absolute `atm` path.

Add `"strings"` and `"testing"` to the import block if missing.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/manager/ -run TestRenderContext -v`
Expected: FAIL — `ContextData` still has `ATMBin`/`RunID`/`Timestamp` and the template still contains `<ATM_BIN>`. Compile error or assert failure.

- [ ] **Step 4: Update the template**

Replace `internal/manager/context_v1.md` with:

```markdown
# ATM manager — <CODE>

Project `<CODE>` (`<PROJECT_NAME>`) · actor `<ACTOR>`

<PERSONA_BLOCK>
<ACTION_BLOCK>

Run `atm conventions` first — it defines the label substrate, the comment/label commands, and the actor-stamping convention; use `atm <cmd> --help` for exact flags. Stamp every ATM mutation with actor `<ACTOR>` — replace the `:unset` model segment with your actual model (e.g. `:opus-4.8`).

## Your Principles

- **Ownership**: You are the autonomous owner of everything `<CODE>`. You keep track of all of it and present it — organized and easy to digest — for the AI agents and humans you serve, and for yourself: clients ask you to recall and curate what the project knows, so your own memory must stay legible.
- **Dive Deep**: You stay connected to the details and work relentlessly to surface current information. You understand your project's past, present, and future. Stay informed in every conversation — the code itself and all documentation — to better assist humans and agents alike.
- **Simplify**: You relentlessly and frequently organize your project. You create order from chaos and turn complex things into simple narratives. You keep documentation easy to digest to aid external communication.
- **Earn Trust**: Keep an eye on the errors and friction that surface during sessions and track them down. Manage your own self-improvement as its own tasks, kept separate from project work, and resolve them during your sessions. Your improvement window is the label substrate itself — you sharpen how its logic is expressed; you do not edit this prompt.

## Your Roles

Capabilities own the operating procedures. Enumerate them with `atm capability list --project <CODE>`; for each enabled capability run `atm capability <name> guide` — its "Brief" section is the human-interview setup procedure, its "Autopilot" section the autonomous maintenance procedure, and the whole guide is your reference when the human asks questions. The current manager action (above) tells you which mode this session runs in. Whatever the mode: keep the ledger legible, ground every answer in cited task/comment IDs, and ask the human one-by-one when a task's intent is unclear.

## Rules of Thumb
- Understand the label logic to find tasks that may contain relevant information.
- Understand each capability's own organization rules and use them to self-organize the project.
```

(The header line dropped `· atm \`<ATM_BIN>\``. The body's `<ATM_BIN>` references became literal `atm`. The `Run \`<ATM_BIN> conventions\`` line became `Run \`atm conventions\``. The `atm <cmd> --help` dropped its `<ATM_BIN>` prefix.)

- [ ] **Step 5: Update the renderer**

Replace `internal/manager/context.go` with:

```go
package manager

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed context_v1.md
var contextV1 string

type ContextData struct {
	Code      string
	Name      string
	Actor     string

	// Persona, PersonaPrompt, PersonaDescription describe the persona the
	// manager is operating as. Rendered into a persona block by RenderContext
	// when Persona is non-empty.
	Persona            string
	PersonaPrompt      string
	PersonaDescription string
	Action             string
	// Capability scopes the action to one capability. Empty means "all enabled
	// capabilities"; the action block then reads "each enabled capability".
	Capability string
}

// RenderContext substitutes the ContextData placeholders into the manager
// prompt template. Empty fields leave their placeholder in place so a generic,
// unrendered template can still be produced (e.g. `atm manage-context`
// with no --project). The installed atm-manager subagent is a thin pointer that
// calls `atm manage-context` at dispatch; it is NOT produced from this
// render.
func RenderContext(data ContextData) string {
	personaBlock := ""
	if data.Persona != "" {
		personaBlock = fmt.Sprintf("## Persona: %s\n\n%s\n\n%s\n\nYou are operating as this persona. Hold to its principles throughout the session, alongside the responsibilities below.\n",
			data.Persona, data.PersonaDescription, data.PersonaPrompt)
	}
	actionBlock := ""
	if data.Action != "" {
		code := data.Code
		if code == "" {
			code = "<CODE>"
		}
		scope := fmt.Sprintf("each enabled capability (`atm capability list --project %s` enumerates them)", code)
		if data.Capability != "" {
			scope = fmt.Sprintf("the `%s` capability", data.Capability)
		}
		switch data.Action {
		case "brief":
			actionBlock = fmt.Sprintf("## Current manager action\n\nFocus this session on **brief**. For %s, run `atm capability <name> guide` and follow its \"Brief\" section — interview the human to set up that capability's territory.\n", scope)
		case "autopilot":
			actionBlock = fmt.Sprintf("## Current manager action\n\nFocus this session on **autopilot**. For %s, run `atm capability <name> guide` and follow its \"Autopilot\" section — autonomously keep that capability's territory following its guide.\n", scope)
		case "ask":
			actionBlock = fmt.Sprintf("## Current manager action\n\nFocus this session on **ask**. Standby for the human to ask questions; do not act proactively and do not mutate the ledger. Read the guide of %s (`atm capability <name> guide`) to be ready to answer.\n", scope)
		default:
			actionBlock = fmt.Sprintf("## Current manager action\n\nFocus this session on **%s**.\n", data.Action)
		}
	}
	// Build a replacer that substitutes non-empty values. Empty values are
	// replaced with the placeholder itself so it survives (a generic, unrendered
	// template can still be produced by `atm manage-context` with no --project).
	// <PERSONA_BLOCK> and <ACTION_BLOCK> are exceptions: when absent, the blocks
	// are genuinely omitted, so they substitute with "" (no placeholders
	// survive). The action block already embeds concrete code, so it never
	// carries a placeholder the replacer would skip.
	pairs := []string{
		"<CODE>", data.Code,
		"<PROJECT_NAME>", data.Name,
		"<ACTOR>", data.Actor,
		"<PERSONA_BLOCK>", personaBlock,
		"<ACTION_BLOCK>", actionBlock,
	}
	final := make([]string, 0, len(pairs))
	for i := 0; i < len(pairs); i += 2 {
		key, val := pairs[i], pairs[i+1]
		if val == "" && key != "<PERSONA_BLOCK>" && key != "<ACTION_BLOCK>" {
			final = append(final, key, key)
		} else {
			final = append(final, key, val)
		}
	}
	return strings.NewReplacer(final...).Replace(contextV1)
}
```

`binOr` is deleted (no callers after this change — `atm manage-context` no longer uses `atmBinPath()`; see Task 4).

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/manager/ -run TestRenderContext -v`
Expected: PASS

- [ ] **Step 7: Run package tests**

Run: `go test ./internal/manager/ -v`
Expected: PASS (other tests in the package may reference `ATMBin`/`RunID`/`Timestamp`; if compile errors, fix the literals to drop those fields).

- [ ] **Step 8: Commit**

```bash
git add internal/manager/context_v1.md internal/manager/context.go internal/manager/context_test.go
git commit -m "refactor(ATM-4afb54): drop RUN_ID/TIMESTAMP/ATM_BIN from manager context template"
```

---

## Task 3: Add shared helpers — contextCachePath and writeContextIfDiff

**Files:**
- Modify: `internal/cli/launcher_shared.go` (append helpers)
- Test: `internal/cli/launcher_shared_test.go` (add tests)

**Interfaces:**
- Produces:
  - `contextCachePath(storePath, code, role, persona, action, capability string) string` — returns `<storePath>/projects/<code>/cache/<key>.md`. `key` is built by `cacheKey(role, persona, action, capability)`. For role `"dev"`: `dev-<persona>`. For role `"manage"`: `manage-<persona>-<action>-<capability|all>`.
  - `writeContextIfDiff(path string, content []byte) error` — reads the existing file; if bytes match, returns nil (no write). Otherwise writes via temp-file + rename within the same dir. Creates parent dirs with `MkdirAll`.
  - `cacheKey(role, persona, action, capability string) string` — lowercase; non-alphanumeric runs collapse to a single `-`. Trim leading/trailing `-`.

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/launcher_shared_test.go`:

```go
func TestContextCachePathDev(t *testing.T) {
	got := contextCachePath("/STORE", "FOO", "dev", "developer", "", "")
	want := "/STORE/projects/FOO/cache/dev-developer.md"
	if got != want {
		t.Fatalf("contextCachePath dev = %q, want %q", got, want)
	}
}

func TestContextCachePathManageAllCapabilities(t *testing.T) {
	got := contextCachePath("/STORE", "FOO", "manage", "manager", "autopilot", "")
	want := "/STORE/projects/FOO/cache/manage-manager-autopilot-all.md"
	if got != want {
		t.Fatalf("contextCachePath manage-all = %q, want %q", got, want)
	}
}

func TestContextCachePathManageScopedCapability(t *testing.T) {
	got := contextCachePath("/STORE", "FOO", "manage", "manager", "brief", "boards")
	want := "/STORE/projects/FOO/cache/manage-manager-brief-boards.md"
	if got != want {
		t.Fatalf("contextCachePath manage-scoped = %q, want %q", got, want)
	}
}

func TestContextCachePathNormalizes(t *testing.T) {
	got := contextCachePath("/STORE", "FOO", "dev", "Dev-Staff", "", "")
	want := "/STORE/projects/FOO/cache/dev-dev-staff.md"
	if got != want {
		t.Fatalf("contextCachePath normalize = %q, want %q", got, want)
	}
}

func TestWriteContextIfDiffCreates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "dev-developer.md")
	content := []byte("# prompt\n")
	if err := writeContextIfDiff(path, content); err != nil {
		t.Fatalf("writeContextIfDiff: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("file content mismatch")
	}
}

func TestWriteContextIfDiffNoOpOnMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "dev-developer.md")
	content := []byte("# prompt\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	prevMtime := info.ModTime()

	// Sleep so a new write would change mtime if it happened.
	time.Sleep(10 * time.Millisecond)

	if err := writeContextIfDiff(path, content); err != nil {
		t.Fatalf("writeContextIfDiff: %v", err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.ModTime().Equal(prevMtime) {
		t.Fatalf("writeContextIfDiff should be a no-op when content matches; mtime changed")
	}
}

func TestWriteContextIfDiffOverwritesOnDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "dev-developer.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("# old\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := writeContextIfDiff(path, []byte("# new\n")); err != nil {
		t.Fatalf("writeContextIfDiff: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "# new\n" {
		t.Fatalf("file not overwritten; got %q", got)
	}
}
```

Add imports to the test file: `"os"`, `"path/filepath"`, `"time"` (skip those already present).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestContextCachePath|TestWriteContextIfDiff' -v`
Expected: FAIL — undefined: `contextCachePath`, `writeContextIfDiff`.

- [ ] **Step 3: Implement the helpers**

Append to `internal/cli/launcher_shared.go`:

```go
// contextCachePath returns the stable on-disk path for a rendered context
// prompt keyed on (project, role, persona, action, capability). Repeated
// launches of the same tuple reuse the same file.
//
// role is "dev" or "manage". For "dev", action and capability are ignored.
// For "manage", an empty capability becomes "all" in the filename.
func contextCachePath(storePath, code, role, persona, action, capability string) string {
	key := cacheKey(role, persona, action, capability)
	return filepath.Join(storePath, "projects", code, "cache", key+".md")
}

// cacheKey builds the filename stem for a context cache file. Non-alphanumeric
// characters collapse to a single "-"; the result is lowercased and trimmed
// of leading/trailing "-".
func cacheKey(role, persona, action, capability string) string {
	parts := []string{role, persona}
	if role == "manage" {
		parts = append(parts, action)
		if capability == "" {
			parts = append(parts, "all")
		} else {
			parts = append(parts, capability)
		}
	}
	for i, p := range parts {
		parts[i] = sanitizeCacheSegment(p)
	}
	return strings.Join(parts, "-")
}

// sanitizeCacheSegment lowercases and collapses non-alphanumeric runs to "-".
func sanitizeCacheSegment(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := true // suppress leading "-"
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else {
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}

// writeContextIfDiff writes content to path only when the existing file's
// bytes differ. When the existing file matches byte-for-byte, it is a no-op
// (mtime unchanged). Parent dirs are created with MkdirAll. The write is
// atomic via a temp file in the same dir followed by a rename.
func writeContextIfDiff(path string, content []byte) error {
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, content) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read existing context %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create context dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ctx-*.md")
	if err != nil {
		return fmt.Errorf("create temp context: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("write temp context: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp context: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp context: %w", err)
	}
	return nil
}
```

Add `"bytes"` to the import block of `launcher_shared.go` if not present.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestContextCachePath|TestWriteContextIfDiff' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/launcher_shared.go internal/cli/launcher_shared_test.go
git commit -m "feat(ATM-4afb54): add contextCachePath and writeContextIfDiff helpers"
```

---

## Task 4: Rewire `atm dev` — PATH guard, stable path, write-if-diff, env cleanup

**Files:**
- Modify: `internal/cli/developing.go:60-141`
- Test: `internal/cli/developing_test.go`

**Interfaces:**
- Consumes: `contextCachePath`, `writeContextIfDiff` from Task 3.
- Produces: `developingEnvValues(project, actor, runID, contextPath, agent, persona, timestamp string) map[string]string` — drops `atmBin` parameter and `ATM_BIN` env var; adds `ATM_TIMESTAMP`.

- [ ] **Step 1: Write the failing test for env values**

In `internal/cli/developing_test.go`, replace `TestDevelopingEnvIncludesATMValues`:

```go
func TestDevelopingEnvIncludesATMValues(t *testing.T) {
	got := assembleEnv(developingEnvValues("FOO", "developer@codex:unset", "FOO-RUNID", "/tmp/context.md", "codex", "developer", "2026-07-19T00:00:00Z"))
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"ATM_ROLE=developing",
		"ATM_PROJECT=FOO",
		"ATM_ACTOR=developer@codex:unset",
		"ATM_RUN_ID=FOO-RUNID",
		"ATM_TIMESTAMP=2026-07-19T00:00:00Z",
		"ATM_CONTEXT_FILE=/tmp/context.md",
		"ATM_AGENT=codex",
		"ATM_PERSONA=developer",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("developing env missing %q", want)
		}
	}
	if strings.Contains(joined, "ATM_BIN=") {
		t.Errorf("developing env must not set ATM_BIN; got:\n%s", joined)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestDevelopingEnvIncludesATMValues -v`
Expected: FAIL — compile error, signature mismatch.

- [ ] **Step 3: Add PATH-guard test**

Add to `internal/cli/developing_test.go`:

```go
func TestDevPATHGuard(t *testing.T) {
	// PATH must NOT resolve `atm`. Use a PATH that has only the harness's
	// own directories (none contain `atm`).
	t.Setenv("PATH", "/nonexistent")
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	h.reset()
	_, stderr, code := h.run("dev", "--agent", "codex", "--project", "FOO")
	if code == ExitSuccess {
		t.Fatalf("expected non-zero exit when atm is not on PATH")
	}
	if !strings.Contains(stderr, "atm is not on PATH") {
		t.Fatalf("expected 'atm is not on PATH' in stderr; got:\n%s", stderr)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestDevPATHGuard -v`
Expected: FAIL — `atm` resolves via the test process's own binary lookup (or no PATH guard is in place yet).

- [ ] **Step 5: Add write-if-diff test**

Add to `internal/cli/developing_test.go`:

```go
func TestDevWriteIfDiffNoOp(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	h.reset()

	// First launch writes the context file.
	if _, _, code := h.run("dev", "--agent", "codex", "--project", "FOO"); code != ExitSuccess {
		t.Fatalf("first dev exit=%d stderr=%s", code, h.stderr.String())
	}
	path := filepath.Join(h.store.StorePath(), "projects", "FOO", "cache", "dev-developer.md")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("context file not created at %s: %v", path, err)
	}
	prev := info.ModTime()

	// Sleep so a rewrite would change mtime.
	time.Sleep(15 * time.Millisecond)

	// Second launch of the same tuple should be a no-op on the file.
	if _, _, code := h.run("dev", "--agent", "codex", "--project", "FOO"); code != ExitSuccess {
		t.Fatalf("second dev exit=%d stderr=%s", code, h.stderr.String())
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("context file disappeared: %v", err)
	}
	if !info.ModTime().Equal(prev) {
		t.Fatalf("context file mtime changed on second launch; write-if-diff should be a no-op")
	}
}
```

Add imports `"os"`, `"path/filepath"`, `"time"` to the test file if not present.

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestDevWriteIfDiffNoOp -v`
Expected: FAIL — file path doesn't exist yet (old code writes to `developing/<runID>.md`).

- [ ] **Step 7: Implement the rewired `runDeveloping`**

Replace the body of `runDeveloping` and `developingEnvValues` in `internal/cli/developing.go`:

```go
func runDeveloping(st *cliState, l developing.Launcher, agent, integration string, opts developingOpts) error {
	s, err := st.openStore()
	if err != nil {
		return err
	}
	p, err := ensureProjectForLaunch(s, opts.Project)
	if err != nil {
		return err
	}

	effectivePersona := opts.Persona
	if effectivePersona == "" {
		effectivePersona = "developer"
	}
	pp, err := s.GetPersona(effectivePersona)
	if err != nil {
		return err // unregistered --persona fails fast
	}
	personaPrompt := pp.Prompt
	personaDescription := pp.Description
	actor := effectivePersona + "@" + l.Name() + ":unset"

	if _, err := exec.LookPath("atm"); err != nil {
		return fmt.Errorf("%w: atm is not on PATH; the developing/manager prompt assumes `atm` resolves on PATH. Either add the directory containing the `atm` binary to PATH, or invoke atm from a shell where it resolves.", ErrUsage)
	}

	now := time.Now().UTC()
	runID := newRunID(opts.Project)
	timestamp := core.RFC3339UTC(now)
	contextPath := contextCachePath(s.StorePath(), p.Code, "dev", effectivePersona, "", "")

	rendered := developing.RenderContext(developing.ContextData{
		Code:               p.Code,
		Name:               p.Name,
		Actor:              actor,
		Persona:            effectivePersona,
		PersonaPrompt:      personaPrompt,
		PersonaDescription: personaDescription,
	})
	if err := writeContextIfDiff(contextPath, []byte(rendered)); err != nil {
		return fmt.Errorf("write context file %s: %w", contextPath, err)
	}

	base := l.BuildArgv()
	envArgs := agentEnvArgs(agent, integration)
	argv := appendAgentArgs(append(base, opts.DefaultArgs...), envArgs, opts.ExtraArgs)
	envValues := developingEnvValues(opts.Project, actor, runID, contextPath, l.Name(), effectivePersona, timestamp)
	env := assembleEnv(envValues)
	if err := emitLaunchHeader(st, "developing", opts.Project, runID, contextPath, l.Name(), argv, envValues); err != nil {
		return err
	}

	exitCode, runErr := st.runChild(l.Name(), argv, env, l.NotFoundHint())
	if err := emitLaunchTail(st, "developing", opts.Project, runID, contextPath, l.Name(), exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}

func developingEnvValues(project, actor, runID, contextPath, agent, persona, timestamp string) map[string]string {
	m := map[string]string{
		"ATM_ROLE":         "developing",
		"ATM_PROJECT":      project,
		"ATM_ACTOR":        actor,
		"ATM_RUN_ID":       runID,
		"ATM_TIMESTAMP":    timestamp,
		"ATM_CONTEXT_FILE": contextPath,
		"ATM_AGENT":        agent,
	}
	if persona != "" {
		m["ATM_PERSONA"] = persona
	}
	return m
}
```

Update the import block in `internal/cli/developing.go`:
- Add `"os/exec"`.
- Remove `"os"` if no longer used (check: `os.WriteFile`/`os.MkdirAll` removed; if no `os.*` remains, drop the import).
- Remove `"path/filepath"` if no longer used (it was only for `filepath.Join`; now `contextCachePath` does the join. If nothing else in the file uses `filepath`, drop it).

- [ ] **Step 8: Run the new tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestDevPATHGuard|TestDevWriteIfDiffNoOp|TestDevelopingEnvIncludesATMValues' -v`
Expected: PASS

- [ ] **Step 9: Run the existing developing tests**

Run: `go test ./internal/cli/ -run 'TestDeveloper' -v`
Expected: Several goldens will FAIL because the env no longer has `ATM_BIN` and the path changed. Fixed in Task 6.

- [ ] **Step 10: Commit**

```bash
git add internal/cli/developing.go internal/cli/developing_test.go
git commit -m "feat(ATM-4afb54): atm dev uses PATH guard, stable cache path, write-if-diff"
```

---

## Task 5: Rewire `atm manage` — PATH guard, stable path, write-if-diff, env cleanup

**Files:**
- Modify: `internal/cli/manager.go:188-313` (covers `newManageContextCmd`, `runManager`, `managerEnvValues`, `atmBinPath`)
- Test: `internal/cli/manager_test.go`

**Interfaces:**
- Consumes: `contextCachePath`, `writeContextIfDiff` from Task 3.
- Produces: `managerEnvValues(project, actor, runID, contextPath, persona, action, capability, timestamp string) map[string]string` — drops `atmBin` parameter and `ATM_BIN` env var; adds `ATM_TIMESTAMP`.

- [ ] **Step 1: Write the failing test for env values**

In `internal/cli/manager_test.go`, replace `TestManagerEnvSetsActionAndCapability`:

```go
func TestManagerEnvSetsActionAndCapability(t *testing.T) {
	got := managerEnvValues("FOO", "manager@opencode:unset", "FOO-RUNID", "/tmp/ctx.md", "manager", "autopilot", "", "2026-07-19T00:00:00Z")
	joined := strings.Join(gotToSlice(got), "\n")
	for _, want := range []string{
		"ATM_PERSONA=manager",
		"ATM_MANAGER_ACTION=autopilot",
		"ATM_MANAGER_CAPABILITY=",
		"ATM_TIMESTAMP=2026-07-19T00:00:00Z",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("manager env missing %q; got:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "ATM_BIN=") {
		t.Errorf("manager env must not set ATM_BIN; got:\n%s", joined)
	}
}
```

- [ ] **Step 2: Write the PATH-guard test**

Add to `internal/cli/manager_test.go`:

```go
func TestManagePATHGuard(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	h.reset()
	_, stderr, code := h.run("manage", "--agent", "codex", "--project", "FOO")
	if code == ExitSuccess {
		t.Fatalf("expected non-zero exit when atm is not on PATH")
	}
	if !strings.Contains(stderr, "atm is not on PATH") {
		t.Fatalf("expected 'atm is not on PATH' in stderr; got:\n%s", stderr)
	}
}
```

- [ ] **Step 3: Write the write-if-diff test**

Add to `internal/cli/manager_test.go`:

```go
func TestManageWriteIfDiffNoOp(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	h.reset()

	if _, _, code := h.run("manage", "--agent", "codex", "--project", "FOO"); code != ExitSuccess {
		t.Fatalf("first manage exit=%d stderr=%s", code, h.stderr.String())
	}
	path := filepath.Join(h.store.StorePath(), "projects", "FOO", "cache", "manage-manager-autopilot-all.md")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("context file not created at %s: %v", path, err)
	}
	prev := info.ModTime()

	time.Sleep(15 * time.Millisecond)

	if _, _, code := h.run("manage", "--agent", "codex", "--project", "FOO"); code != ExitSuccess {
		t.Fatalf("second manage exit=%d stderr=%s", code, h.stderr.String())
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("context file disappeared: %v", err)
	}
	if !info.ModTime().Equal(prev) {
		t.Fatalf("context file mtime changed on second launch; write-if-diff should be a no-op")
	}
}
```

Add imports `"os"`, `"path/filepath"`, `"time"` to the test file if not present.

- [ ] **Step 4: Update `manage-context` test assertions**

In `internal/cli/manager_test.go`:

- `TestManageContextGenericKeepsPlaceholders` (line 269): drop `"<ATM_BIN>"` from the placeholder list. The generic render no longer contains `<ATM_BIN>` — the template uses the literal `atm`.
- `TestManageContextFillsProjectName` (line 284): drop `"<ATM_BIN>"` from the placeholder check list at line 297.

After:

```go
func TestManageContextGenericKeepsPlaceholders(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	_, _, code := h.run("manage-context")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := h.stdout.String()
	for _, placeholder := range []string{"<CODE>"} {
		if !strings.Contains(got, placeholder) {
			t.Errorf("generic manage-context stripped %s", placeholder)
		}
	}
	if strings.Contains(got, "<ATM_BIN>") {
		t.Errorf("generic manage-context must not contain <ATM_BIN>; literal `atm` is used")
	}
}
```

```go
func TestManageContextFillsProjectName(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo Project", "--actor", "admin@cli:unset")
	h.reset()
	h.output = outputText
	_, _, code := h.run("manage-context", "--project", "FOO", "--actor", "admin@cli:unset")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := h.stdout.String()
	if !strings.Contains(got, "Foo Project") {
		t.Errorf("manage-context did not fill <PROJECT_NAME> from the store:\n%s", got)
	}
	for _, ph := range []string{"<CODE>", "<PROJECT_NAME>", "<ACTOR>"} {
		if strings.Contains(got, ph) {
			t.Errorf("manage-context left placeholder %s when --project given", ph)
		}
	}
}
```

- [ ] **Step 5: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestManagePATHGuard|TestManageWriteIfDiffNoOp|TestManagerEnvSetsActionAndCapability|TestManageContextGenericKeepsPlaceholders|TestManageContextFillsProjectName' -v`
Expected: FAIL — signatures mismatch, path doesn't exist.

- [ ] **Step 6: Implement the rewired `runManager`, `managerEnvValues`, `newManageContextCmd`, and drop `atmBinPath`**

In `internal/cli/manager.go`:

Replace `newManageContextCmd` (line 188-221) with the version that uses the literal `"atm"`:

```go
func newManageContextCmd(st *cliState) *cobra.Command {
	var opts struct {
		Project string
		Actor   string
	}
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
				data.Name = opts.Project // fallback when the project isn't in the store
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
	return cmd
}
```

Replace `runManager` (line 223-291) with:

```go
func runManager(st *cliState, l manager.Launcher, agent, integration string, opts managerOpts) error {
	s, err := st.openStore()
	if err != nil {
		return err
	}
	p, err := ensureProjectForLaunch(s, opts.Project)
	if err != nil {
		return err
	}

	if err := validateManagerAction(opts.Action, opts.Capability, st.registry.Names(), st.fullRegistry.Names()); err != nil {
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

	if _, err := exec.LookPath("atm"); err != nil {
		return fmt.Errorf("%w: atm is not on PATH; the developing/manager prompt assumes `atm` resolves on PATH. Either add the directory containing the `atm` binary to PATH, or invoke atm from a shell where it resolves.", ErrUsage)
	}

	now := time.Now().UTC()
	runID := newRunID(opts.Project)
	timestamp := core.RFC3339UTC(now)
	contextPath := contextCachePath(s.StorePath(), p.Code, "manage", effectivePersona, opts.Action, opts.Capability)

	rendered := manager.RenderContext(manager.ContextData{
		Code:               p.Code,
		Name:               p.Name,
		Actor:              actor,
		Persona:            effectivePersona,
		PersonaPrompt:      mp.Prompt,
		PersonaDescription: mp.Description,
		Action:             opts.Action,
		Capability:         opts.Capability,
	})
	if err := writeContextIfDiff(contextPath, []byte(rendered)); err != nil {
		return fmt.Errorf("write context file %s: %w", contextPath, err)
	}

	base := l.BuildArgvManage(contextPath)
	envArgs := agentEnvArgs(agent, integration)
	argv := appendAgentArgs(append(base, opts.DefaultArgs...), envArgs, opts.ExtraArgs)
	envValues := managerEnvValues(opts.Project, actor, runID, contextPath, effectivePersona, opts.Action, opts.Capability, timestamp)
	env := assembleEnv(envValues)
	if err := emitLaunchHeader(st, "manager", opts.Project, runID, contextPath, l.Name(), argv, envValues); err != nil {
		return err
	}

	exitCode, runErr := st.runChild(l.Name(), argv, env, l.NotFoundHint())
	if err := emitLaunchTail(st, "manager", opts.Project, runID, contextPath, l.Name(), exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}
```

Replace `managerEnvValues` (line 293-305) with:

```go
func managerEnvValues(project, actor, runID, contextPath, persona, action, capability, timestamp string) map[string]string {
	return map[string]string{
		"ATM_ROLE":               "manager",
		"ATM_PROJECT":            project,
		"ATM_ACTOR":              actor,
		"ATM_RUN_ID":             runID,
		"ATM_TIMESTAMP":          timestamp,
		"ATM_CONTEXT_FILE":       contextPath,
		"ATM_PERSONA":            persona,
		"ATM_MANAGER_ACTION":     action,
		"ATM_MANAGER_CAPABILITY": capability,
	}
}
```

Delete the `atmBinPath` function (line 307-313).

Update the import block:
- Add `"os/exec"`.
- Remove `"os"` if no longer used (`os.WriteFile`/`os.MkdirAll`/`os.UserHomeDir` still used by `newManagerPluginStatusCmd`/`newManagerPluginInstallCmd` — keep `os`).
- Remove `"path/filepath"` if no longer used (`filepath.Join` was the only use in `runManager`; check the rest of the file. The plugin install functions use `filepath.Join` — keep the import).
- Remove `"fmt"` only if unused — keep it; it's used elsewhere in the file.

- [ ] **Step 7: Run new and updated tests**

Run: `go test ./internal/cli/ -run 'TestManagePATHGuard|TestManageWriteIfDiffNoOp|TestManagerEnvSetsActionAndCapability|TestManageContextGenericKeepsPlaceholders|TestManageContextFillsProjectName' -v`
Expected: PASS

- [ ] **Step 8: Run the full manager test suite**

Run: `go test ./internal/cli/ -run 'TestManage|TestManagerEnv' -v`
Expected: Goldens FAIL (path/env changes). Fixed in Task 6.

- [ ] **Step 9: Commit**

```bash
git add internal/cli/manager.go internal/cli/manager_test.go
git commit -m "feat(ATM-4afb54): atm manage uses PATH guard, stable cache path, write-if-diff"
```

---

## Task 6: Update test normalizers and regenerate goldens

**Files:**
- Modify: `internal/cli/developing_test.go:238-249` (`normalizeDevelopingOutput`)
- Modify: `internal/cli/manager_test.go:334-345` (`normalizeManagerOutput`)
- Regenerate: `internal/cli/testdata/golden/developer-codex-launch.json`
- Regenerate: `internal/cli/testdata/golden/manage-codex-autopilot-launch.json`
- Regenerate: `internal/cli/testdata/golden/manage-codex-action-brief-launch.json`
- Verify: `internal/cli/testdata/golden/developing-launcher-not-found.json`
- Verify: `internal/cli/testdata/golden/developing-tail-summary.json`
- Verify: `internal/cli/testdata/golden/developing-missing-project.json`

**Interfaces:** none new.

- [ ] **Step 1: Replace `normalizeDevelopingOutput`**

```go
func normalizeDevelopingOutput(s, storePath string) string {
	s = normalizeOutput(s)
	if storePath != "" {
		contextPathRe := regexp.MustCompile(strings.ReplaceAll(filepath.ToSlash(storePath), `/`, `\/`) + `/projects/FOO/cache/dev-developer\.md`)
		s = contextPathRe.ReplaceAllString(s, "/STORE/projects/FOO/cache/dev-developer.md")
	}
	runIDRe := regexp.MustCompile(`FOO-\d{14}-[0-9a-f]{6}`)
	s = runIDRe.ReplaceAllString(s, "FOO-RUNID")
	return s
}
```

The `atmBinRe` is dropped (no `ATM_BIN` in the env anymore).

- [ ] **Step 2: Replace `normalizeManagerOutput`**

```go
func normalizeManagerOutput(s, storePath string) string {
	s = normalizeOutput(s)
	if storePath != "" {
		contextPathRe := regexp.MustCompile(strings.ReplaceAll(filepath.ToSlash(storePath), `/`, `\/`) + `/projects/FOO/cache/manage-manager-autopilot-all\.md`)
		s = contextPathRe.ReplaceAllString(s, "/STORE/projects/FOO/cache/manage-manager-autopilot-all.md")
		contextPathRe = regexp.MustCompile(strings.ReplaceAll(filepath.ToSlash(storePath), `/`, `\/`) + `/projects/FOO/cache/manage-manager-brief-all\.md`)
		s = contextPathRe.ReplaceAllString(s, "/STORE/projects/FOO/cache/manage-manager-brief-all.md")
	}
	runIDRe := regexp.MustCompile(`FOO-\d{14}-[0-9a-f]{6}`)
	s = runIDRe.ReplaceAllString(s, "FOO-RUNID")
	return s
}
```

The `atmBinRe` is dropped.

- [ ] **Step 3: Run the affected tests to confirm they fail with golden diff**

Run: `go test ./internal/cli/ -run 'TestDeveloperCodexLaunchJSON|TestManageCodexAutopilotLaunchJSON|TestManageCodexActionBriefLaunch' -v`
Expected: FAIL with golden-file diff (path/env changed).

- [ ] **Step 4: Regenerate the goldens**

Run: `UPDATE_GOLDENS=1 go test ./internal/cli/ -run 'TestDeveloperCodexLaunchJSON|TestManageCodexAutopilotLaunchJSON|TestManageCodexActionBriefLaunch' -v`

(If the project uses a different env var or flag for golden regen, check the harness. Inspect `compareGolden` if the env var name differs.)

Expected: PASS; the three `.json` files under `internal/cli/testdata/golden/` are rewritten.

- [ ] **Step 5: Inspect the regenerated goldens**

Read each regenerated golden file. Expected shapes:

`developer-codex-launch.json` — two JSON objects (header + tail). Header's `context_path` is `.../projects/FOO/cache/dev-developer.md`. `env` has no `ATM_BIN`, has `ATM_TIMESTAMP` (any RFC3339 value, normalized by the regex to whatever the harness produces — verify the value matches `^\d{4}-\d{2}-\d{2}T...`), has `ATM_RUN_ID: "FOO-RUNID"` (post-normalization).

`manage-codex-autopilot-launch.json` — `context_path` is `.../projects/FOO/cache/manage-manager-autopilot-all.md`. `argv[1]` ends with the same path. `env` has `ATM_MANAGER_ACTION: "autopilot"`, no `ATM_BIN`, has `ATM_TIMESTAMP`.

`manage-codex-action-brief-launch.json` — `context_path` is `.../projects/FOO/cache/manage-manager-brief-all.md`. `env.ATM_MANAGER_ACTION: "brief"`.

If `ATM_TIMESTAMP` appears in the goldens with a real timestamp, add a normalization regex for it in `normalizeDevelopingOutput` and `normalizeManagerOutput`:

```go
// In normalizeDevelopingOutput and normalizeManagerOutput, after the runIDRe line:
timestampRe := regexp.MustCompile(`"ATM_TIMESTAMP": "[^"]+"`)
s = timestampRe.ReplaceAllString(s, `"ATM_TIMESTAMP": "TIMESTAMP"`)
```

Re-run regen if added.

- [ ] **Step 6: Verify the no-change goldens**

Run: `go test ./internal/cli/ -run 'TestDevelopingLauncherNotFound|TestDevelopingTailSummaryJSON' -v`
Expected: PASS. If FAIL, inspect — `developing-tail-summary.json` is produced by `emitLaunchTail` directly and should not reference `ATM_BIN`; `developing-launcher-not-found.json` is an error envelope.

If `developing-tail-summary.json` fails, the harness's `TestDevelopingTailSummaryJSON` passes a hardcoded path (`/STORE/developing/FOO-RUNID.md`) to `emitLaunchTail` and calls `normalizeDevelopingOutput`. The normalizer's new context-path regex won't match that hardcoded path; fix the test's input to `/STORE/projects/FOO/cache/dev-developer.md` and regen the golden.

- [ ] **Step 7: Run the full cli package test suite**

Run: `go test ./internal/cli/ -v 2>&1 | tail -50`
Expected: PASS (or surface any remaining test that referenced the old paths).

- [ ] **Step 8: Commit**

```bash
git add internal/cli/developing_test.go internal/cli/manager_test.go internal/cli/testdata/golden/
git commit -m "test(ATM-4afb54): update normalizers + regenerate goldens for stable cache path"
```

---

## Task 7: Final verification and cleanup

**Files:** none (verification only).

- [ ] **Step 1: Run `make verify`**

Run: `make verify`
Expected: PASS — all packages, scripts-test, arch test.

If any test fails:
- Search for residual `ATM_BIN` / `<ATM_BIN>` / `atmBinPath` / `os.Executable` references: `grep -rn 'ATM_BIN\|<ATM_BIN>\|atmBinPath\|os.Executable' internal/` — fix any leftovers.
- Search for residual `developing/` / `manager/` path references in test setup: `grep -rn 'filepath.Join.*"developing"\|filepath.Join.*"manager"' internal/cli/` — fix.

- [ ] **Step 2: Run the architecture test**

Already covered by `make verify` (`atm/tests/arch`). If it fails, read `tests/arch/` to see what architecture rule is violated and fix.

- [ ] **Step 3: Sanity smoke test (optional, if `atm` builds locally)**

Run: `make build && ./atm dev --project ATM --help`
Expected: prints the `dev` help (unchanged shape).

Run: `./atm manage --project ATM --help`
Expected: prints the `manage` help (unchanged shape).

- [ ] **Step 4: Manual cleanup note**

Remind the user (in the session output, not in code) to clean up the legacy dirs once after upgrading:

```
rm -rf ~/.config/atm/developing/ ~/.config/atm/manager/
```

- [ ] **Step 5: Final commit (if any leftover fixes)**

If Steps 1-2 surfaced fixes, commit them:

```bash
git add -A
git commit -m "fix(ATM-4afb54): residual cleanup from stable-context-prompt rollout"
```

---

## Self-Review

**Spec coverage.** Each spec section maps to tasks:

- §2 Template changes → Tasks 1 + 2.
- §3 PATH guard → Tasks 4 + 5 (both add `exec.LookPath("atm")`).
- §4 Stable file path & write-if-diff → Task 3 (helpers) + Tasks 4 + 5 (call sites).
- §5 Cleanup of legacy top-level dirs → Task 7 Step 4 (manual note). No code touches the legacy dirs.
- §6 Tests & goldens → Tasks 1-6 each include tests; Task 6 regenerates goldens.
- §7 Conventions doc / spec impact → no spec edits required (grep confirmed the v2 spec does not mention the legacy paths).
- §8 Rollout → Task 7 verifies; single-PR rollout, no feature flag.

**Placeholder scan.** No "TBD", "TODO", "implement later", or "add appropriate error handling" in the plan. Each step has actual code or actual commands.

**Type consistency.**
- `contextCachePath(storePath, code, role, persona, action, capability string) string` — signature identical in Task 3 (definition), Task 4 (dev call site: `contextCachePath(s.StorePath(), p.Code, "dev", effectivePersona, "", "")`), Task 5 (manage call site: `contextCachePath(s.StorePath(), p.Code, "manage", effectivePersona, opts.Action, opts.Capability)`).
- `writeContextIfDiff(path string, content []byte) error` — identical across definition and call sites.
- `developingEnvValues` — Task 4 signature: `(project, actor, runID, contextPath, agent, persona, timestamp string)`. Test call matches: `developingEnvValues("FOO", "developer@codex:unset", "FOO-RUNID", "/tmp/context.md", "codex", "developer", "2026-07-19T00:00:00Z")`.
- `managerEnvValues` — Task 5 signature: `(project, actor, runID, contextPath, persona, action, capability, timestamp string)`. Test call matches: `managerEnvValues("FOO", "manager@opencode:unset", "FOO-RUNID", "/tmp/ctx.md", "manager", "autopilot", "", "2026-07-19T00:00:00Z")`.
- `cacheKey` / `sanitizeCacheSegment` — internal to Task 3; no external callers.

All checks pass.