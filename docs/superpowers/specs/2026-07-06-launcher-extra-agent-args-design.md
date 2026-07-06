# Launcher Extra Agent Args + Ollama Host — Design Spec

**Status:** Approved
**Date:** 2026-07-06
**Parent specs:** `2026-07-05-atm-developing-agent-launcher-design.md`,
`2026-07-04-onboarding-v1-design.md`,
`2026-07-06-atm-manager-subagent-design.md`

## Driver

ATM's launchers (`atm developing`, `atm manager`, `atm onboarding`) build a
fixed argv for the selected host agent (`opencode`, `codex`, `claude`, or
`ollama` for onboarding). The fixed argv is the agent's *normal interactive
entrypoint*: `["codex"]`, `["claude"]`, `["opencode"]`, or
`["ollama", "launch", "<integration>", "--", ...]`.

Users routinely run host agents with persistent per-agent flags that are not
part of the bare binary name: `codex --yolo`,
`claude --dangerously-skip-permission`, `opencode --auto`, etc. Today there is
no way to thread those through an `atm <role> <agent>` invocation, so users
must drop out of the ATM launcher path and run the host agent directly,
losing ATM's session-context injection and ledger plumbing.

ATM should pass through user-supplied flags to the host agent without
interpreting them. ATM's job is the work-ledger context, not the host agent's
approval mode or sandbox policy.

## Scope (v1)

- Add POSIX `--` passthrough to every agent subcommand under `developing`,
  `manager`, and `onboarding`. Everything after `--` is appended verbatim to
  the host agent's base argv.
- Add `ATM_<AGENT>_ARGS` environment variables as default args applied on
  every launch of that agent. Parsed with `strings.Fields` (whitespace split;
  no quoting support — documented limitation).
- Env args are applied first (base defaults); `--` args are appended after.
  No dedup; the host agent's flag parser resolves conflicts.
- Add `ollama` as a supported host agent under `developing` and `manager`
  (parity with onboarding). Ollama requires a `--integration <name>` flag,
  same as onboarding's ollama subcommand.
- Update README command reference and `atm conventions` day-to-day section
  to document the new mechanism.

## Out of Scope (v1)

- A persistent config-file registry for per-agent default args. Env + `--`
  is the v1 surface; shell aliases / env files cover the persistence case.
- Validating, sanitizing, or restricting which flags may be passed. Args
  after `--` and env args are appended verbatim; ATM trusts the user.
- Quote-aware shell-word splitting of `ATM_<AGENT>_ARGS`. Whitespace split
  only. Flag values that contain spaces are not supported via env; use `--`
  for those.
- Supporting host agents beyond `opencode`, `codex`, `claude`, `ollama`.
- Changing the onboarding prompt pipeline (the rendered prompt file remains
  the onboarding contract). Extra args append after the onboarding argv
  base, not into the prompt text.
- Deduping env + `--` args. If a user sets `ATM_CODEX_ARGS=--yolo` and also
  passes `-- --yolo`, the agent receives `--yolo --yolo`; the agent's flag
  parser handles it.

## Command Surface

### developing / manager

```
atm developing opencode --project <CODE> [--actor <id>] [--dry-run] [-- <agent args...>]
atm developing codex    --project <CODE> [--actor <id>] [--dry-run] [-- <agent args...>]
atm developing claude   --project <CODE> [--actor <id>] [--dry-run] [-- <agent args...>]
atm developing ollama   --project <CODE> --integration <name> [--actor <id>] [--dry-run] [-- <agent args...>]

atm manager opencode --project <CODE> [--actor <id>] [--dry-run] [-- <agent args...>]
atm manager codex    --project <CODE> [--actor <id>] [--dry-run] [-- <agent args...>]
atm manager claude   --project <CODE> [--actor <id>] [--dry-run] [-- <agent args...>]
atm manager ollama   --project <CODE> --integration <name> [--actor <id>] [--dry-run] [-- <agent args...>]
```

The `ollama` subcommands require `--integration <name>` (one of ollama's
supported integrations: `opencode`, `codex`, `claude`, etc.). ATM does not
validate the integration name; unknown values fail at `ollama launch`'s
door, mirroring onboarding's behavior.

### onboarding

```
atm onboarding opencode --project <CODE> [...onboarding flags] [-- <agent args...>]
atm onboarding ollama    --project <CODE> --integration <name> [...onboarding flags] [-- <agent args...>]
```

Onboarding's BaseArgv already ends with the constructed prompt message. Extra
args append after that base.

### Env defaults

```
ATM_OPENCODE_ARGS="<args>"
ATM_CODEX_ARGS="<args>"
ATM_CLAUDE_ARGS="<args>"
ATM_OLLAMA_ARGS="<args>"     (generic ollama host)
ATM_<INTEGRATION>_ARGS="<args>"  (e.g. ATM_CODEX_ARGS) — used by the ollama host when integration=codex, since codex is the actual exec target
```

For `ollama` hosts, env lookup precedence is:

1. `ATM_<UPPER(INTEGRATION)>_ARGS` (the integration is the actual exec target).
2. `ATM_OLLAMA_ARGS` fallback.

For `opencode`/`codex`/`claude` direct hosts (developing/manager), env lookup
is `ATM_<UPPER(AGENT)>_ARGS`.

Env args apply on every launch of that agent; `--` args append after env args.

## Mechanism

### Launcher packages stay pure-data

The launcher packages (`internal/developing`, `internal/manager`,
`internal/onboard`) keep their existing `Launcher` interface and
`BuildArgv()` / `BuildArgv(promptPath, title)` signatures unchanged. They
return the *base* argv for the agent's normal interactive entrypoint.

New `OllamaLauncher` types are added to `internal/developing` and
`internal/manager` (not just `internal/onboard`). For developing/manager,
ollama's base argv is the *interactive* form:

```
["ollama", "launch", "<integration>", "--"]
```

(no `--auto`, no `--prompt` — those are onboarding-specific.) Extra args
append after the `--`, on the integration side of ollama's passthrough,
which is the correct side per `ollama launch --help`.

The CLI's ollama subcommand constructs `OllamaLauncher{Integration: ...}`
directly in its `RunE`, mirroring how `internal/cli/onboarding.go` already
constructs `onboard.OllamaLauncher{Integration: ...}`. `LauncherFor` stays
returning ok=false for `"ollama"` in the developing and manager packages,
because the integration is not known at factory time and the direct
construction is the established onboarding pattern. The existing
`LauncherFor("ollama")` == false assertions in those packages' tests
stay unchanged.

### CLI layer owns arg assembly

`internal/cli/launcher_shared.go` gains one shared helper:

```go
// agentEnvArgs returns the env-derived extra args for a host agent.
// For ollama hosts, integration takes precedence over the generic ollama env.
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

// appendAgentArgs returns base + envArgs + extraArgs (no dedup).
func appendAgentArgs(base []string, envArgs, extraArgs []string) []string {
    out := make([]string, 0, len(base)+len(envArgs)+len(extraArgs))
    out = append(out, base...)
    out = append(out, envArgs...)
    out = append(out, extraArgs...)
    return out
}
```

Each agent subcommand:

1. Calls `l.BuildArgv()` (or `BuildArgv(promptPath, title)` for onboarding).
2. Calls `agentEnvArgs(agent, integration)` for env defaults.
3. Appends `opts.ExtraArgs` (collected from after `--`).
4. Passes the merged argv to `runChild`.

### Capturing `--` with Cobra

Each agent subcommand sets:

```go
cmd.Args = cobra.ArbitraryArgs
// In RunE, before calling runRole:
if rest := cmd.Flags().Args(); len(rest) > 0 {
    opts.ExtraArgs = rest
}
```

Cobra's `--` handling feeds post-dash positionals into `Flags().Args()`. For
v1 we do not need `ArgsLenAtDash()` because all ATM flags are named flags
(`--project`, `--actor`, `--dry-run`, `--integration`), not positionals;
there is no ambiguity about which args belong to ATM vs the agent.

Exception: the `--integration <name>` flag for ollama is a named flag, so it
sits before `--` correctly.

### Argument validation

- Unknown agent subcommand: Cobra usage error (unchanged).
- `ollama` subcommand without `--integration`: Cobra required-flag error
  (mirror onboarding's ollama subcommand).
- Unknown `--integration` value: not validated by ATM; `ollama launch` fails
  with its own error (unchanged behavior vs onboarding).
- Args after `--` are not validated or deduplicated.

## Output / Dry-Run

`--dry-run` prints the full argv array in JSON and the `launching: <argv>` line
in text mode. With extra args, the argv array simply includes them. No
envelope schema change.

The launch header's `argv` field reflects `base + env + extra`, so users see
exactly what will exec before the child starts.

Existing goldens (`developing-dry-run-codex.json`,
`manager-dry-run-codex.json`, onboarding dry-run goldens) stay byte-identical
because in the no-`--`/no-env case the merged argv equals the base argv.

## Testing

### Unit

- `appendAgentArgs` merge order: env-then-`--`, no dedup, empty both → base only.
- `agentEnvArgs` env-name mapping for direct hosts (opencode, codex, claude).
- `agentEnvArgs` precedence for ollama: `ATM_<INTEGRATION>_ARGS` wins over
  `ATM_OLLAMA_ARGS`; `ATM_OLLAMA_ARGS` used when integration env absent.
- `agentEnvArgs` empty env → nil.
- Launcher `BuildArgv()` for new ollama developing/manager launchers returns
  the interactive `ollama launch <integration> --` base.
- `LauncherFor("ollama")` stays ok=false for developing and manager (ollama
  is constructed directly by the CLI's ollama subcommand, mirroring
  onboarding's pattern, since integration is not known at factory time).
  Existing `launcher_test.go` assertions stay unchanged.

### Golden

- `developing-dry-run-codex-extra.json`: `atm developing codex --project FOO
  --dry-run -- --yolo --auto` → argv `["codex","--yolo","--auto"]`.
- `developing-dry-run-ollama.json`: `atm developing ollama --project FOO
  --integration codex --dry-run -- --yolo` → argv
  `["ollama","launch","codex","--","--yolo"]`.
- `manager-dry-run-claude-extra.json`: `atm manager claude --project FOO
  --dry-run -- --dangerously-skip-permission`.
- `manager-dry-run-ollama.json`: `atm manager ollama --project FOO
  --integration opencode --dry-run` → argv
  `["ollama","launch","opencode","--"]`.
- `onboarding-dry-run-opencode-extra.json`: `atm onboarding opencode
  --project FOO --dry-run -- --foo` → argv
  `["opencode","--auto","--prompt","<msg>","--foo"]`.
- `onboarding-dry-run-ollama-extra.json`: `atm onboarding ollama --project FOO
  --integration codex --dry-run -- --bar` → argv
  `["ollama","launch","codex","--","--auto","--prompt","<msg>","--bar"]`.
- Env-driven golden: `t.Setenv("ATM_CODEX_ARGS","--yolo")` + no `--` → argv
  has `--yolo` appended.
- Combined env + `--`: both present in argv in order (env first).

### Existing goldens

All existing developing/manager/onboarding dry-run goldens remain
byte-identical (regression guard). No existing launcher_test.go assertions
change; the `LauncherFor("ollama")` == false assertions stay, since ollama
is constructed directly by the CLI (mirroring onboarding).

### Repository gate

`make verify`.

## Docs

- README command reference for `developing`, `manager`, and `onboarding`
  updated to show the `-- [agent args...]` syntax and the new
  `atm developing ollama` / `atm manager ollama` subcommands with
  `--integration`.
- README documents `ATM_<AGENT>_ARGS` env vars and the ollama
  `ATM_<INTEGRATION>_ARGS` precedence.
- `atm conventions` day-to-day section gains a short bullet noting extra
  agent arg passthrough + env defaults.
- This spec is additive; parent specs are referenced, not rewritten.

## Internal Architecture

```
internal/developing/launcher.go
  + OllamaLauncher{Integration string} (interactive form: no --auto/--prompt)
    constructed directly by the CLI's ollama subcommand (mirrors onboarding);
    LauncherFor stays ok=false for "ollama"

internal/manager/launcher.go
  + OllamaLauncher{Integration string} (interactive form)
    constructed directly by the CLI's ollama subcommand;
    LauncherFor stays ok=false for "ollama"

internal/onboard/launcher.go
  unchanged (OllamaLauncher already exists; onboarding-specific --auto/--prompt form)

internal/cli/launcher_shared.go
  + agentEnvArgs(agent, integration string) []string
  + appendAgentArgs(base, envArgs, extraArgs []string) []string

internal/cli/developing.go
  + ollama subcommand with --integration flag
  + opts.ExtraArgs captured from cmd.Flags().Args() after "--"
  + runDeveloping uses appendAgentArgs(BuildArgv(), agentEnvArgs(...), opts.ExtraArgs)

internal/cli/manager.go
  + ollama subcommand with --integration flag
  + same ExtraArgs + appendAgentArgs integration

internal/cli/onboarding.go
  + opts.ExtraArgs captured after "--"
  + runOnboarding uses appendAgentArgs(BuildArgv(promptPath,title), agentEnvArgs(...), opts.ExtraArgs)
```

The external command shape stays `atm developing|manager|onboarding <agent>`;
no new top-level commands.

## Error Handling

- Unknown agent subcommand: Cobra usage error (unchanged).
- `ollama` subcommand without `--integration`: Cobra required-flag error.
- Unknown `--integration` value: not validated by ATM; `ollama launch` fails
  with its own error (unchanged vs onboarding).
- Missing project: `ErrNotFound` with the existing project-create hint
  (unchanged).
- Missing agent binary: existing `runChild` not-found error with the
  launcher's `NotFoundHint()` (unchanged). Ollama's hint is
  `https://ollama.com`.
- Empty env + no `--`: identical to today (zero behavior change, zero
  golden drift).
- User passes an ATM flag after `--` (e.g. `-- --project`): treated as a
  literal agent arg, not an ATM flag (POSIX `--` semantics). Documented.

## Backward Compatibility

This is purely additive:

- `atm developing|manager|onboarding <agent>` with no `--` and no
  `ATM_<AGENT>_ARGS` env behaves identically to today.
- Existing dry-run goldens remain byte-identical.
- The `Launcher` interface and `BuildArgv()` / `BuildArgv(promptPath, title)`
  signatures are unchanged; the assembly happens in the CLI layer.
- No existing test files are edited: `LauncherFor("ollama")` assertions
  (developing, manager) stay ok=false, since ollama is constructed directly
  by the CLI (mirroring onboarding's pattern).

## Open Questions

None for v1. Follow-up candidates (not in scope):

- A persistent per-agent config file if env proves insufficient for common
  defaults.
- Shell-word splitting for `ATM_<AGENT>_ARGS` if space-within-values becomes
  a real need.