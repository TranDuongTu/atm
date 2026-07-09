# Agent Prompt Minimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Minimize and de-duplicate ATM's host-agent prompts — developing surfaces become thin pointers to a minimal rendered context; the manager collapses to a single principle-driven source reached via a stable thin subagent pointer + `atm manager render-context`.

**Architecture:** Two agent families. (1) The *developing* agent is guided by a minimal rendered `context_v1.md` (read-only CLIs + "delegate every write to the manager"); the installed assets (claude/codex `session-start` hook, opencode `atm-developing.js` bootstrap, `atm-developing/SKILL.md`) shrink to thin nudges that just load it. (2) The *manager* has one source of truth, `internal/manager/context_v1.md`, rewritten to principles + a four-action catalog; the installed `atm-manager.md` becomes a ~15-line pointer that runs `atm manager render-context` at dispatch, so manager logic updates with the binary and never needs re-install.

**Tech Stack:** Go 1.x, cobra CLI, `embed.FS` for plugin assets, table-driven Go tests, golden-file fixtures (`-update` flag).

## Global Constraints

- Prompt/markdown files are **unwrapped**: one physical line per paragraph/bullet (no hard wrapping at ~80 cols).
- The three per-agent variants of any installed asset (SKILL, hook/bootstrap, atm-manager pointer) must stay content-identical where the text is shared, so they cannot drift.
- Command syntax is **not** embedded in prompts; prompts point to `atm conventions` and `atm <command> --help`.
- `go test ./...` must pass before completion.
- Commits go on branch `atm-0071-prompt-minimization` (already created). Do not commit to `main`.
- ATM ledger task: **ATM-0071**. This is an ATM developing session — writes to the ledger are delegated to the `atm-manager` subagent; do not hand-edit ledger tasks in these steps.

## Deliberate coverage change (confirm on review)

Per the approved "principles only" manager decision, the **truth-discipline** rules (`did NOT happen`, `read it back`) are removed from the manager subagent asset and from `context_v1.md`. The concrete ATM-0047 mechanism (guessed binary path → silent failures) is still guarded: the thin pointer resolves `${ATM_BIN:-atm}`, `command -v`-checks it, and bails if unavailable. Task 4 removes the two truth-discipline test assertions accordingly. If reviewers want the read-back guardrail retained, add a one-line rule to the thin pointer instead of reverting the whole section.

---

### Task 1: Developing rendered context (`context_v1.md`)

**Files:**
- Modify: `internal/developing/context_v1.md` (full rewrite)
- Test: `internal/developing/context_test.go`

**Interfaces:**
- Consumes: `developing.RenderContext(ContextData)` (unchanged) which substitutes `<CODE>`, `<PROJECT_NAME>`, `<ATM_BIN>`, `<ACTOR>`, `<RUN_ID>`, `<PERSONA_BLOCK>` and (harmlessly, now-unused) `<TIMESTAMP>`.
- Produces: a rendered developing context containing the fragments asserted below; later tasks (hook/JS/skill) point to this file via `$ATM_CONTEXT_FILE`.

- [ ] **Step 1: Rewrite `internal/developing/context_v1.md`** to exactly:

```markdown
# ATM developing session <RUN_ID>

Project `<CODE>` (`<PROJECT_NAME>`) · actor `<ACTOR>` · atm `<ATM_BIN>`

ATM is the visible ledger for this work — keep it current as you go. Repo instructions, harness rules, permissions, and user directions come first.

<PERSONA_BLOCK>
Learn the contract and find what exists:

- `<ATM_BIN> conventions` — what ATM is, the label substrate, the first-contact sequence, and the code-of-conduct. Start here.
- `<ATM_BIN> search --project <CODE> "…"` — find existing tasks, decisions, and prior work before you start.
- `<ATM_BIN> task show --id <ID>` / `<ATM_BIN> task comment list --task <ID>` — read a task's running narrative before acting on it.
- `<ATM_BIN> label list --project <CODE>` — the project's live vocabulary.

Delegate every write to the manager:

- To record progress, create or update a task, add a comment, or set labels, dispatch the `atm-manager` subagent (`hint: <kind>` + a short message) — it curates and writes it for you.
- When unsure about a project standard — task conventions, which label to use, how work is normally organized — dispatch `atm-manager` and let it decide and update progress for you rather than guessing.
```

- [ ] **Step 2: Replace the three tests in `internal/developing/context_test.go`** with the versions below (the old ones assert removed content: the multi-line header, `atm task comment add`, `self-improvement gene`, `staff@claude:` actor guidance, and a `Retrieval`/`hint: question` section).

```go
func TestRenderContextSubstitutesAllPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{
		Code:      "FOO",
		Name:      "Foo Project",
		ATMBin:    "/usr/local/bin/atm",
		Actor:     "codex-dev",
		RunID:     "FOO-20260705120000-a1b2c3",
		Timestamp: "2026-07-05T12:00:00Z",
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
		"ATM developing session FOO-20260705120000-a1b2c3",
		"Project `FOO` (`Foo Project`)",
		"actor `codex-dev`",
		"atm `/usr/local/bin/atm`",
		"conventions",
		"search --project FOO",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q", want)
		}
	}
}

func TestRenderContext_Persona(t *testing.T) {
	out := RenderContext(ContextData{
		Code: "ATM", Name: "ATM", ATMBin: "/atm", Actor: "staff@claude",
		RunID: "R", Timestamp: "T", Persona: "staff", PersonaPrompt: "hold a high bar",
	})
	if !strings.Contains(out, "Persona: staff") || !strings.Contains(out, "hold a high bar") {
		t.Fatalf("persona block missing:\n%s", out)
	}

	out2 := RenderContext(ContextData{Code: "ATM", Name: "ATM", ATMBin: "/atm", Actor: "claude-dev", RunID: "R", Timestamp: "T"})
	if strings.Contains(out2, "## Persona") {
		t.Fatalf("no-persona render should omit persona block:\n%s", out2)
	}
}

func TestRenderContextDelegatesWritesToManager(t *testing.T) {
	got := RenderContext(ContextData{Code: "ATM", Name: "Agent Tasks Management", ATMBin: "/usr/local/bin/atm", Actor: "ollama-dev", RunID: "R1", Timestamp: "2026-07-08T00:00:00Z"})
	for _, frag := range []string{"atm search", "Delegate every write", "atm-manager", "dispatch"} {
		if !strings.Contains(got, frag) {
			t.Errorf("developing context missing %q", frag)
		}
	}
}
```

- [ ] **Step 3: Run the developing package tests, expect PASS**

Run: `go test ./internal/developing/ -run 'TestRenderContext' -v`
Expected: `TestRenderContextSubstitutesAllPlaceholders`, `TestRenderContext_Persona`, `TestRenderContextDelegatesWritesToManager` all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/developing/context_v1.md internal/developing/context_test.go
git commit -m "refactor: ATM-0071 minimal developing context (read + delegate)"
```

---

### Task 2: Developing thin nudges (hook, opencode bootstrap, SKILL)

**Files:**
- Modify: `internal/developing/plugin_assets/claude/hooks/session-start`
- Modify: `internal/developing/plugin_assets/codex/hooks/session-start`
- Modify: `internal/developing/plugin_assets/opencode/atm-developing.js` (only the `bootstrap()` return string)
- Modify: `internal/developing/plugin_assets/claude/skills/atm-developing/SKILL.md`
- Modify: `internal/developing/plugin_assets/codex/skills/atm-developing/SKILL.md`
- Modify: `internal/developing/plugin_assets/opencode/skills/atm-developing/SKILL.md`
- Test: `internal/developing/plugins_test.go`

**Interfaces:**
- Consumes: nothing new; assets are static, surfaced by `developing.PluginAssets(agent)`.
- Produces: thinned assets whose joined content contains `ATM_ROLE`, `ATM_PROJECT`, `visible ledger`, `ATM_CONTEXT_FILE` (claude), `Use the atm-developing skill`, `config.skills.paths`, `bootstrap injected` (opencode). The atm-manager dispatch contract now lives in the rendered context (Task 1), not these assets.

- [ ] **Step 1: Rewrite both `session-start` hooks** (`claude` and `codex` — identical content) to:

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

context="This is an ATM developing session for project ${ATM_PROJECT}. ATM is the visible ledger for this work. Use the atm-developing skill and your rendered context file (${ATM_CONTEXT_FILE:-}) for your instructions; keep the ledger current."
escaped="$(escape_json "$context")"

printf '{\n  "hookSpecificOutput": {\n    "hookEventName": "SessionStart",\n    "additionalContext": "%s"\n  }\n}\n' "$escaped"
```

- [ ] **Step 2: Thin the opencode `bootstrap()` return string** in `internal/developing/plugin_assets/opencode/atm-developing.js`. Replace the template literal returned by `bootstrap` (currently lines ~11-21) with:

```javascript
  return `<ATM_DEVELOPING_CONTEXT>
This is an ATM developing session for project ${project}.
ATM is the visible ledger for this work.
Use the atm-developing skill and your rendered context file (${contextFile}) for your instructions; keep the ledger current.
</ATM_DEVELOPING_CONTEXT>`
```

Leave the rest of the file (the `config`, `experimental.chat.messages.transform`, `shell.env` hooks, logging) unchanged.

- [ ] **Step 3: Rewrite all three `SKILL.md`** (`claude`, `codex`, `opencode` — identical) to:

```markdown
---
name: atm-developing
description: Use EAGERLY at the start of and throughout any ATM development session (ATM_ROLE=developing) — before creative, design, implementation, or investigation work, and whenever you make progress. Loads the session's ATM ledger instructions so the work stays visible.
---

# ATM Developing

You are in an ATM-tracked session; ATM is the visible ledger for this work.

Read `$ATM_CONTEXT_FILE` for the session's rendered instructions and follow them. If it is unset, run `atm conventions`.
```

- [ ] **Step 4: Update `internal/developing/plugins_test.go`.** Change the ledger-language wants in `TestPluginAssetsContainLedgerLanguage` and delete the two asset-level contract tests whose content moved into the rendered context.

Replace `TestPluginAssetsContainLedgerLanguage` body's `want` slice with:

```go
	for _, want := range []string{"visible ledger", "ATM_CONTEXT_FILE", "Use the atm-developing skill"} {
```

Delete `TestPluginAssetsContainManagerDispatchContract` (lines ~63-74) and `TestPluginAssetsForbidSelfImprovementGeneTasks` (lines ~76-87) entirely — the dispatch contract and the write-delegation boundary are now asserted on the rendered context by Task 1's `TestRenderContextDelegatesWritesToManager`. Leave `TestOpenCodePluginAssetContainsLedgerBeforeSkillsAndLogging` unchanged (its wants — `Use the atm-developing skill`, `config.skills.paths`, `bootstrap injected` — are all still present).

- [ ] **Step 5: Run the developing package tests, expect PASS**

Run: `go test ./internal/developing/ -v`
Expected: PASS. In particular `TestPluginAssetsContainLedgerLanguage`, `TestOpenCodePluginAssetContainsLedgerBeforeSkillsAndLogging`, `TestCodexHookCommandRunsWithClaudePluginRootFallback`, `TestCodexPluginManifestSuppressesAutoDiscoveredHooks` pass; the two deleted tests are gone.

- [ ] **Step 6: Commit**

```bash
git add internal/developing/plugin_assets internal/developing/plugins_test.go
git commit -m "refactor: ATM-0071 thin developing nudges (hook/opencode/skill point to context)"
```

---

### Task 3: Manager single source (`context_v1.md`) — principles + actions

**Files:**
- Modify: `internal/manager/context_v1.md` (full rewrite)
- Test: `internal/manager/context_test.go` (full rewrite)

**Interfaces:**
- Consumes: `manager.RenderContext(ContextData)` (unchanged: substitutes `<CODE>`, `<PROJECT_NAME>`, `<ATM_BIN>`, `<ACTOR>`; empty fields keep their placeholder).
- Produces: a rendered manager prompt containing `autonomous owner`, `Tracking request`, `Inquiry`, `Vocabulary`, `Onboarding`; consumed by Task 5's `render-context` and by interactive/onboarding launches.

- [ ] **Step 1: Rewrite `internal/manager/context_v1.md`** to exactly:

```markdown
# ATM manager — <CODE>

Project `<CODE>` (`<PROJECT_NAME>`) · actor `<ACTOR>` · atm `<ATM_BIN>`

## Who you are

- **You are the autonomous owner of everything `<CODE>`.** You keep track of all of it and present it — organized and easy to digest — for the AI agents and humans you serve, and for yourself: clients ask you to recall and curate what the project knows, so your own memory must stay legible.
- **You relentlessly keep the project organized.** Well-described labels, tasks kept current, conventions enforced, correct information easy to search and monitor. Assume your clients are disorganized: they log tasks and comments and invent labels ad hoc. That is fine — you read their intention, absorb the mess, and simplify it into a clean substrate.
- **You watch for your clients' confusion and learn from it.** Keep an eye on the errors and friction that surface during sessions and track them down. Manage your own self-improvement as its own tasks, kept separate from project work, and resolve them during your sessions. Your improvement window is the label substrate itself — you sharpen how its logic is expressed; you do not edit this prompt.

## What you do

- **Tracking request** — a developing agent hands you a `hint: <word>` + message mid-work; find the task it extends and curate it (comment, or create/split/label as the work demands).
- **Inquiry & curation** — recall and link knowledge on request, grounded in cited IDs; you digest your own journal too, connecting related tasks and keeping them searchable.
- **Vocabulary** — recompute the project's ubiquitous language (its recurring domain terms).
- **Onboarding** — when first introduced to a repo/project, learn it and organize it into a substrate a later agent can pick up.
```

- [ ] **Step 2: Replace the entire contents of `internal/manager/context_test.go`** (all seven tests assert removed content: `knowledge-base owner`, `ubiquitous language` header, `vocabulary.json`, `ATM_ONBOARD`, `Inquiry responsibility`, `ATM:comment:superseded`, the command cheat-sheet, etc.) with:

```go
package manager

import (
	"strings"
	"testing"
)

func TestRenderContextSubstitutesAllPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{
		Code:   "ATM",
		Name:   "Agent Tasks Management",
		ATMBin: "/usr/local/bin/atm",
		Actor:  "opencode-manager",
	})
	for _, placeholder := range []string{"<CODE>", "<PROJECT_NAME>", "<ATM_BIN>", "<ACTOR>"} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered context still contains %s", placeholder)
		}
	}
	for _, want := range []string{
		"ATM manager — ATM",
		"Project `ATM` (`Agent Tasks Management`)",
		"actor `opencode-manager`",
		"atm `/usr/local/bin/atm`",
		"autonomous owner",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q", want)
		}
	}
}

func TestRenderContextPrinciplesPresent(t *testing.T) {
	got := RenderContext(ContextData{Code: "ATM", Name: "ATM", ATMBin: "/bin/atm", Actor: "m"})
	for _, frag := range []string{
		"autonomous owner",
		"relentlessly keep the project organized",
		"self-improvement",
		"label substrate",
	} {
		if !strings.Contains(got, frag) {
			t.Errorf("principle missing %q", frag)
		}
	}
}

func TestRenderContextActionCatalogPresent(t *testing.T) {
	got := RenderContext(ContextData{Code: "ATM", Name: "ATM", ATMBin: "/bin/atm", Actor: "m"})
	for _, frag := range []string{"Tracking request", "Inquiry", "Vocabulary", "Onboarding"} {
		if !strings.Contains(got, frag) {
			t.Errorf("action catalog missing %q", frag)
		}
	}
}

func TestRenderContextGenericKeepsPlaceholders(t *testing.T) {
	// The generic body (no project) is produced by leaving placeholders in place
	// so `atm manager render-context` with no --project still renders a template.
	got := RenderContext(ContextData{})
	for _, placeholder := range []string{"<CODE>", "<ATM_BIN>", "<ACTOR>"} {
		if !strings.Contains(got, placeholder) {
			t.Errorf("generic render stripped %s; placeholders must survive for template use", placeholder)
		}
	}
}
```

- [ ] **Step 3: Run the manager context tests, expect PASS**

Run: `go test ./internal/manager/ -run 'TestRenderContext' -v`
Expected: `TestRenderContextSubstitutesAllPlaceholders`, `TestRenderContextPrinciplesPresent`, `TestRenderContextActionCatalogPresent`, `TestRenderContextGenericKeepsPlaceholders` PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/manager/context_v1.md internal/manager/context_test.go
git commit -m "refactor: ATM-0071 manager single source (principles + action catalog)"
```

---

### Task 4: Manager thin pointer (`atm-manager.md` ×3)

**Files:**
- Modify: `internal/manager/plugin_assets/claude/atm-manager.md` (full rewrite)
- Modify: `internal/manager/plugin_assets/codex/atm-manager.md` (full rewrite)
- Modify: `internal/manager/plugin_assets/opencode/atm-manager.md` (full rewrite)
- Test: `internal/manager/plugins_test.go`

**Interfaces:**
- Consumes: env `ATM_BIN`, `ATM_PROJECT`, `ATM_ACTOR` at dispatch; calls `atm manager render-context` (Task 5 guarantees it fills `<PROJECT_NAME>`).
- Produces: an installed asset whose body contains `ATM_PROJECT`, `${ATM_BIN:-atm}`, `render-context`, `tools: Bash, Read, Glob, Grep`, no `<PLACEHOLDER>` tokens, no hardcoded `.local/bin/atm`.

- [ ] **Step 1: Rewrite all three `atm-manager.md`** (identical content) to:

````markdown
---
name: atm-manager
description: ATM ledger owner. Invoke when the developing agent asks to track work, formalize progress, split/merge tasks, or organize the project ledger. Reads ATM_PROJECT/ATM_BIN/ATM_ACTOR from env; stays silent unless ATM_PROJECT is set.
tools: Bash, Read, Glob, Grep
---

<!-- Thin, stable pointer. All manager logic lives in the atm binary and is printed by `atm manager render-context`; this file only bootstraps and defers to it, so enhancing the manager never requires re-installing this plugin. -->

# ATM manager

Resolve your environment first — the only sources of truth are the env vars, never a guessed path or code:

```bash
ATM="${ATM_BIN:-atm}"
[ -n "$ATM_PROJECT" ] || { echo "atm-manager inactive"; exit 0; }   # not in an ATM session
command -v "$ATM" >/dev/null 2>&1 || { echo "atm binary UNAVAILABLE: $ATM"; exit 0; }
"$ATM" manager render-context --project "$ATM_PROJECT" --actor "$ATM_ACTOR"
```

The `render-context` output is your full, current instructions — read it and follow it for this session. Do not gate on `ATM_ROLE`; being loaded as the `atm-manager` agent is the role signal.
````

- [ ] **Step 2: Update `internal/manager/plugins_test.go`.** Two edits.

(a) Replace the `want` slice in `TestPluginAssetsContainManagerRole` (asserts removed content `needs clarification`, `semantic search`, `task set-title`) with pointer content:

```go
	for _, want := range []string{
		"ATM ledger owner",
		"render-context",
		"follow it",
	} {
```

(b) In `TestManagerSubagentAssetResolvesRuntimeValuesFromEnv`, delete the two truth-discipline assertions (the `did NOT happen` block, lines ~78-81, and the `read it back` block, lines ~82-85) — see the "Deliberate coverage change" note. Keep the placeholder scan, the `${ATM_BIN:-atm}` check, and the `.local/bin/atm` check. Append a positive check that the pointer defers to render-context:

```go
		if !strings.Contains(body, "render-context") {
			t.Errorf("%s subagent asset does not defer to `atm manager render-context`", host)
		}
```

Leave `TestClaudeManagerAssetToolsFrontmatterFormat`, `TestPluginAssetsCheckATMProject`, and `TestPluginInstallRoot` unchanged.

- [ ] **Step 3: Run the manager package tests, expect PASS**

Run: `go test ./internal/manager/ -v`
Expected: PASS, including `TestManagerSubagentAssetResolvesRuntimeValuesFromEnv`, `TestClaudeManagerAssetToolsFrontmatterFormat`, `TestPluginAssetsContainManagerRole`.

- [ ] **Step 4: Commit**

```bash
git add internal/manager/plugin_assets internal/manager/plugins_test.go
git commit -m "refactor: ATM-0071 manager subagent as thin render-context pointer"
```

---

### Task 5: `render-context` fills project name + fix stale comment

**Files:**
- Modify: `internal/cli/manager.go` (the `newManagerRenderContextCmd` RunE, ~lines 192-219)
- Modify: `internal/manager/context.go` (the stale doc comment, ~lines 20-24)
- Test: `internal/cli/manager_test.go`

**Interfaces:**
- Consumes: `st.openStore()` and `store.Store.GetProject(code) (*store.Project, error)` (field `Name string`); `atmBinPath() string`.
- Produces: `atm manager render-context --project <CODE> [--actor <A>]` output with `<PROJECT_NAME>` filled (from the store when the project exists, else falling back to the code) and no unrendered `<CODE>`/`<ATM_BIN>`/`<ACTOR>`.

- [ ] **Step 1: Write the failing test** — add to `internal/cli/manager_test.go`:

```go
func TestManagerRenderContextFillsProjectName(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo Project", "--actor", "ttran")
	h.reset()
	h.output = outputText
	_, _, code := h.run("manager", "render-context", "--project", "FOO", "--actor", "m")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := h.stdout.String()
	if !strings.Contains(got, "Foo Project") {
		t.Errorf("render-context did not fill <PROJECT_NAME> from the store:\n%s", got)
	}
	for _, ph := range []string{"<CODE>", "<PROJECT_NAME>", "<ATM_BIN>", "<ACTOR>"} {
		if strings.Contains(got, ph) {
			t.Errorf("render-context left placeholder %s when --project given", ph)
		}
	}
}
```

- [ ] **Step 2: Run it, expect FAIL**

Run: `go test ./internal/cli/ -run TestManagerRenderContextFillsProjectName -v`
Expected: FAIL — output still contains `<PROJECT_NAME>` (current code never fills the name).

- [ ] **Step 3: Update `newManagerRenderContextCmd` RunE** in `internal/cli/manager.go`. Replace the RunE body with:

```go
		RunE: func(cmd *cobra.Command, args []string) error {
			data := manager.ContextData{
				Code:  opts.Project,
				Actor: opts.Actor,
			}
			if opts.Project != "" {
				data.ATMBin = atmBinPath()
				data.Name = opts.Project // fallback when the project isn't in the store
				if s, err := st.openStore(); err == nil {
					if p, err := s.GetProject(opts.Project); err == nil {
						data.Name = p.Name
					}
				}
			}
			rendered := manager.RenderContext(data)
			// Text mode: print raw markdown. JSON mode: wrap in an envelope.
			return st.emit(st.stdout(), map[string]any{"context": rendered}, func() {
				fmt.Fprint(st.stdout(), rendered)
			})
		},
```

(The `RunID: "RENDER"` and store-less behavior are dropped: the principle-only template has no `<RUN_ID>`/`<TIMESTAMP>` placeholders, and name-fill needs the store.)

- [ ] **Step 4: Run the new test, expect PASS**

Run: `go test ./internal/cli/ -run TestManagerRenderContextFillsProjectName -v`
Expected: PASS.

- [ ] **Step 5: Fix the assertions in `TestManagerRenderContextTextHasPrompt`** (`internal/cli/manager_test.go`, ~line 132). Its `want` slice asserts removed content (`ATM manager session`, `knowledge-base owner`, `needs clarification`, `task set-title`). Replace the `want` slice with:

```go
	for _, want := range []string{"ATM manager", "autonomous owner", "Tracking request", "Onboarding"} {
```

`TestManagerRenderContextGenericKeepsPlaceholders` (no `--project`) stays valid and unchanged — the generic path passes empty `Code`/`ATMBin`/`Actor`, so `RenderContext` keeps those placeholders.

- [ ] **Step 6: Fix the stale comment** in `internal/manager/context.go` (~lines 20-24). Replace the doc comment above `RenderContext` with:

```go
// RenderContext substitutes the ContextData placeholders into the manager
// prompt template. Empty fields leave their placeholder in place so a generic,
// unrendered template can still be produced (e.g. `atm manager render-context`
// with no --project). The installed atm-manager subagent is a thin pointer that
// calls `atm manager render-context` at dispatch; it is NOT produced from this
// render.
```

- [ ] **Step 7: Run the cli manager tests, expect PASS**

Run: `go test ./internal/cli/ -run 'TestManagerRenderContext' -v`
Expected: `TestManagerRenderContextFillsProjectName`, `TestManagerRenderContextTextHasPrompt`, `TestManagerRenderContextGenericKeepsPlaceholders` PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/manager.go internal/cli/manager_test.go internal/manager/context.go
git commit -m "feat: ATM-0071 render-context fills project name; fix stale render comment"
```

---

### Task 6: Full suite + golden verification

**Files:**
- Possibly regenerate: `internal/cli/testdata/golden/developing-dry-run-*.json`, `internal/cli/testdata/golden/manager-onboard-dry-run-*.json` (only if they changed).

**Interfaces:**
- Consumes: all prior tasks.
- Produces: green `go test ./...`.

- [ ] **Step 1: Run the whole suite**

Run: `go test ./...`
Expected: PASS. Note: the launch dry-run goldens capture `agent/argv/env/context_path/project/role/run_id` — **not** prompt text — so prompt rewrites should not touch them. If any golden test fails on a genuine, intended shape change, inspect the diff first.

- [ ] **Step 2: If (and only if) a launch golden failed with an intended diff, regenerate and eyeball**

Run: `go test ./internal/cli/ -run 'Developing|ManagerOnboard' -update`
Then: `git diff internal/cli/testdata/golden/` — confirm every changed line is expected (paths/env only, no accidental content leak). If a diff is unexpected, revert and investigate rather than accepting it.

- [ ] **Step 3: Build and smoke-test render-context end to end**

Run:
```bash
go build -o /tmp/atm-atm0071 ./cmd/atm
/tmp/atm-atm0071 manager render-context --project ATM --actor smoke | head -20
```
Expected: prints the principle-driven manager prompt with `# ATM manager — ATM`, `Project \`ATM\` (\`...\`)`, the three principles, and the four-action catalog; no `<PLACEHOLDER>` tokens.

- [ ] **Step 4: Final commit (if goldens regenerated)**

```bash
git add internal/cli/testdata/golden/
git commit -m "test: ATM-0071 regenerate launch goldens"
```

---

## Self-Review

**Spec coverage:**
- Minimal/discovery-oriented prompts → Tasks 1, 3 (point to `conventions`/`--help`). ✓
- No re-install on logic change → Task 4 (thin pointer) + Task 5 (`render-context` fills name). ✓
- No drift → identical assets across variants (Tasks 2, 4); single manager source (Task 3). ✓
- Principle-driven manager → Task 3. ✓
- Developing three surfaces deduplicated (hook/context/SKILL) → Tasks 1, 2; the opencode JS bootstrap (a fourth inline copy the spec's "SessionStart hook" bucket implies) is also thinned in Task 2. ✓
- Supporting code: `render-context` polish → Task 5; stale `context.go` comment → Task 5. ✓
- Testing: golden files → Task 6; `PluginStatus` stale-check — no expectation change needed (those tests compare install↔embed bytes at runtime, not literal content), covered by `go test ./...` in Task 6. ✓

**Placeholder scan:** No "TBD"/"handle appropriately" steps; every file rewrite and test edit shows full content. ✓

**Type consistency:** `manager.ContextData` fields used (`Code`, `Name`, `ATMBin`, `Actor`) match `internal/manager/context.go`; `store.GetProject` returns `*store.Project` with `Name`; `atmBinPath()`, `st.openStore()`, `st.emit`, `ExitSuccess`, `outputText`, `newGoldenHarness` all exist in the `cli` package as used elsewhere in `manager_test.go`/`manager.go`. ✓

**Note on `<TIMESTAMP>`:** the spec mentioned stamping `<TIMESTAMP>`; the principle-only manager template dropped that placeholder entirely, so no timestamp fill is needed. Recorded here so the divergence from the spec is intentional and visible.
