# Stable Context Prompt — Design

**Date:** 2026-07-19
**Task:** ATM-4afb54 (couples with the RUN_ID dedup sibling)
**Status:** Proposed
**Spec author:** developer@ollama:glm-5.2-cloud

## 1. Problem

Every invocation of `atm dev` and `atm manage` regenerates a fresh
context prompt and writes it to a new per-run file:

- `$ATM_HOME/developing/<CODE>-<YYYYMMDDHHMMSS>-<6hex>.md`
- `$ATM_HOME/manager/<CODE>-<YYYYMMDDHHMMSS>-<6hex>.md`

163 such developing files and 34 manager files have accumulated on the
author's machine. The rendered prompt's volatility comes from three
changing dimensions:

- `<RUN_ID>` — fresh timestamp+random per launch (in the prompt title).
- `<TIMESTAMP>` — fresh per launch (threaded through, not displayed).
- `<ATM_BIN>` — machine-specific absolute path resolved via
  `os.Executable()` and stamped into every command example.

The user wants the prompt to be a function of `(project, persona, action,
capability)` only, so a repeat launch of the same tuple reuses the
existing file byte-for-byte and no regeneration happens.

The `<ATM_BIN>` removal is ticketed separately as ATM-4afb54. This spec
absorbs ATM-4afb54 because the same templates and tests are touched
either way, and removing `<ATM_BIN>` is one of the volatility dimensions
that has to go.

## 2. Goal & non-goals

**Goal.** Stop regenerating the developing/manager system prompt on every
launch. The rendered prompt is a function of `(project, persona, action,
capability)`. The file is reused byte-for-byte across launches of the
same tuple; the file's mtime does not change when content matches.

**Non-goals.**

- Caching the agent's own conversation state.
- Changing the launcher interface (`Launcher.BuildArgv` /
  `BuildArgvManage`).
- Touching `atm manage-context`'s no-project placeholder behavior beyond
  dropping `<ATM_BIN>`.
- Auto-cleaning the legacy top-level `$ATM_HOME/developing/` and
  `$ATM_HOME/manager/` dirs. The user removes them by hand once after
  upgrading. New code never reads or writes them.
- Changing the TUI. The TUI does not display context files.
- Changing the event log / DB schema. The cache dir is not part of the
  event log.

## 3. Template changes

Both `internal/developing/context_v1.md` and
`internal/manager/context_v1.md` lose the volatile placeholders.

### Developing template

Before:

```markdown
# ATM developing session <RUN_ID>

Project `<CODE>` (`<PROJECT_NAME>`) · actor `<ACTOR>` · atm `<ATM_BIN>`
```

After:

```markdown
# ATM developing session

Project `<CODE>` (`<PROJECT_NAME>`) · actor `<ACTOR>`
```

The `· atm \`<ATM_BIN>\`` segment is dropped from the header line. The
agent does not need an absolute path stamped into the prompt when `atm`
is on PATH (see §4 PATH guard).

All command examples in the template body use literal `atm`:
`atm conventions`, `atm capability list --project <CODE>`,
`atm search --project <CODE> "..."`, etc. The `<ATM_BIN>` placeholder is
gone from the template.

### Manager template

Same treatment. The header line drops `· atm \`<ATM_BIN>\``. The action
block (built in `internal/manager/context.go`) builds commands with the
literal `"atm"` instead of `data.ATMBin`. The `binOr` helper either
returns `"atm"` or is deleted.

### ContextData

`internal/developing.ContextData` drops: `ATMBin`, `RunID`, `Timestamp`.

Keeps: `Code`, `Name`, `Actor`, `Persona`, `PersonaPrompt`,
`PersonaDescription`.

`internal/manager.ContextData` drops: `ATMBin`, `RunID`, `Timestamp`.

Keeps: `Code`, `Name`, `Actor`, `Persona`, `PersonaPrompt`,
`PersonaDescription`, `Action`, `Capability`.

`RenderContext` in both packages drops the corresponding replacer pairs.

### `atm manage-context` (hidden)

`atm manage-context` (used by the manager subagent plugin) keeps
working. With no `--project`, the existing "leave placeholders" behavior
is preserved: `<CODE>`, `<ACTOR>` survive when empty. The literal `atm`
in command examples is fine in the generic template — there is no
binary path to substitute.

## 4. PATH guard

At launch (both `runDeveloping` and `runManager`), before rendering,
call `exec.LookPath("atm")`. If it fails:

```
atm is not on PATH; the developing/manager prompt assumes `atm` resolves on PATH.
Either add the directory containing the `atm` binary to PATH, or invoke atm from a shell where it resolves.
```

Exit non-zero (`ErrUsage`-wrapped). This replaces the previous
`os.Executable()` call in both launchers.

`ATM_BIN` env var is dropped from `developingEnvValues` and
`managerEnvValues`. If anything in the agent ecosystem needs the binary
path, the agent resolves `atm` itself via PATH. `atmBinPath()` in
`internal/cli/manager.go` is deleted (it was only used by
`manage-context`, which now uses the literal `"atm"`).

A new env var `ATM_TIMESTAMP` is added (RFC3339, fresh per launch). The
timestamp moves from the prompt body to env-only, matching how
`ATM_RUN_ID` is already env-only. `ATM_RUN_ID` stays — it remains the
session identifier in the event log and is still passed to the child.

## 5. Stable file path & write-if-diff

### Path layout

New path layout under the project dir, flat with role-prefixed keys:

```
$ATM_HOME/projects/<CODE>/cache/dev-<persona>.md
$ATM_HOME/projects/<CODE>/cache/manage-<persona>-<action>-<capability|all>.md
```

Examples:

- `dev-developer.md`
- `manage-manager-autopilot-all.md`
- `manage-manager-brief-boards.md`

Filename character rules: lowercase; non-alphanumeric in
persona/action/capability is replaced with `-`. The registry already
restricts these to lowercase + hyphens, so this is mostly defensive
normalization.

The cache dir is created lazily by `os.MkdirAll` on first launch. It is
not under the event-log-managed `projects/<CODE>/` subdirs (`config.json`,
`log.jsonl`, `vocabulary.json`, etc.); it sits beside them as a
separate, non-event-log-managed cache.

### Launch behavior

1. Resolve the target path from the tuple.
2. Render the prompt to a byte slice.
3. If the file exists and its bytes equal the rendered slice: do
   nothing (no write; mtime stays).
4. Otherwise: write the rendered slice to the path. Atomic via
   temp-file + rename within the same dir.

`ATM_CONTEXT_FILE` env var still points at the rendered file's path;
the agent still opens it the same way. `ATM_RUN_ID` is still generated
per launch and passed via env — the run ID lives on as a session
identifier in env / event log, not in the prompt body.

### Test observability

Step (3) — the no-op-when-matching path — is the test of "we did not
regenerate." A test that launches the same tuple twice and asserts the
file's mtime did not change on the second launch exercises this.

## 6. Cleanup of legacy top-level dirs

The new code path never touches `$ATM_HOME/developing/` or
`$ATM_HOME/manager/`. The user removes them by hand once after
upgrading:

```
rm -rf ~/.config/atm/developing/ ~/.config/atm/manager/
```

No migration command, no auto-cleanup, no spec churn for the legacy
dirs. They are dead weight once the new code path lands; nothing reads
them after the launch that wrote them.

`ATM_CONTEXT_FILE` env var historically pointed at
`$ATM_HOME/developing/<runID>.md`. After this change it points at
`projects/<CODE>/cache/dev-<persona>.md`. Existing `developing/*.md`
files are simply ignored.

## 7. Tests & goldens

### `internal/developing/context_test.go`

- Drop `<ATM_BIN>` from the placeholder list at line 17-20.
- Drop the `"atm \`/usr/local/bin/atm\`"` assertion at line 29.
- Drop `ATMBin` from the `ContextData` literals in
  `TestRenderContext_Persona`, `TestRenderContextPromptsJournaling`,
  `TestRenderContextModelStampInstruction`,
  `TestRenderContextIncludesPersonaDescription`.
- Add: assert the rendered body does NOT contain `<RUN_ID>` or
  `<TIMESTAMP>` (placeholders gone; test fails if anyone reintroduces
  them).
- Add: assert the body contains `atm conventions` (bare `atm`).

### `internal/manager/context_test.go`

Parallel changes: assert the action block uses `atm` literal, not
`<ATM_BIN>` or an absolute path. Drop `ATMBin`/`RunID`/`Timestamp` from
test `ContextData` literals.

### `internal/cli/developing_test.go`

- `normalizeDevelopingOutput` (line 238): drop the `ATM_BIN` regex
  normalization (line 246-247) since the bin is no longer in the env.
- Drop the runID regex (line 244-245) and the context path regex
  (line 240-243) since the path is now stable
  (`projects/<CODE>/cache/dev-<persona>.md`).
- Golden files under `testdata/` for `developer-codex-launch` and
  `developing-launcher-not-found` get regenerated.

### `internal/cli/developing_test.go::TestDevelopingEnvIncludesATMValues`

- Drop the `ATM_BIN=/bin/atm` assertion.
- Add `ATM_TIMESTAMP=` (new env var) and keep `ATM_RUN_ID=` (still
  present).

### `internal/cli/manager_test.go`

- Parallel env-value test changes for `managerEnvValues`.
- Any test that asserts the context file path contains
  `manager/<runID>.md` updates to `projects/<CODE>/cache/manage-...md`.

### New tests

1. **Write-if-diff (developing).** Launch the same
   `(project, persona)` tuple twice. Assert the context file's mtime did
   not change on the second launch (write-if-diff is a no-op when
   content matches).
2. **Write-if-diff (manager).** Same for the manager path with a fixed
   `(project, persona, action, capability)` tuple.
3. **PATH guard (dev).** Set `PATH` to a dir without `atm`; assert
   non-zero exit and the expected error string.
4. **PATH guard (manage).** Same for `manage`.
5. **No absolute path in body (ATM-4afb54).** Render the developing and
   manager contexts with a non-empty `ATMBin` value (which should now be
   impossible to pass, but defensively). Assert the rendered body
   contains only `atm ` (bare) and never an absolute path.

## 8. Conventions doc / spec impact

- The v2 spec at
  `docs/superpowers/specs/2026-07-02-tasks-management-v2-design.md`
  does not mention `$ATM_HOME/developing/` or `$ATM_HOME/manager/` for
  context files (grep confirmed no hits). No update needed there.
- The older launcher / manager subagent specs
  (`2026-07-05-atm-developing-agent-launcher-design.md`,
  `2026-07-06-atm-manager-subagent-design.md`) reference the legacy
  paths. They are historical; no retroactive edit.
- AGENTS.md "SUPERPOWERS" block is unaffected — it does not mention the
  context file path.
- No DB schema change; the cache dir is not part of the event log.
  `atm store rebuild` / `verify` do not touch it.

## 9. Rollout

Single PR. No feature flag, no compatibility window — the new path
layout is the only path. Users who upgrade clear the legacy dirs by
hand once.

## 10. Out of scope (parking lot)

- Migrating `atm manage-context` to a stable path too. Currently
  hidden, used by the manager subagent plugin; it always renders fresh
  per invocation. Could later be content-addressed, but the subagent
  plugin is a thin pointer and the cost is small. Defer.
- A `atm dev gc` / `atm manage gc` reaper command for old context
  files. Not needed once the file is stable per tuple.
- Surfacing the current context path in the TUI. Not asked for.