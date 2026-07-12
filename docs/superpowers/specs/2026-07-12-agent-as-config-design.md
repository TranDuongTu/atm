# Agent as Config, Not Flags — Design Spec

**Status:** Approved for spec draft after ATM-0083 brainstorming.
**Task:** ATM-0083.
**Date:** 2026-07-12.

## Driver

Today the host agent (the harness ATM launches — `opencode`, `codex`, `claude`,
or an `ollama`-driven variant) is named on **every** launch as a subcommand:

- `atm claude --project ATM` / `atm codex --project ATM` (developer session)
- `atm manage claude --project ATM --planning` (manager session)
- `atm ollama --integration opencode --project ATM` (ollama-driven session)

The set of agents a user actually cares about is effectively decided once at
`atm init` (which plugins they install), but that decision is **not persisted**.
So the agent is re-declared on every command, and the ollama path additionally
requires a `--integration` flag each time.

This change makes the agent a **stored default** with a small `atm agents`
surface to inspect and switch it, so day-to-day launches stop naming the agent.

This is an intentional breaking cleanup, consistent with the recent CLI user
surface simplification (ATM-0084). There is no alias-first migration for the old
per-agent launcher subcommands.

## Goals

- Persist a selected agent so `atm dev --project <CODE>` and
  `atm manage --project <CODE> --<action>` launch with no agent named.
- Introduce an `atm agents` surface: `list`, `select`, `args`.
- Present agents as a fixed catalog of supported entries with **live readiness**,
  not a user-curated list — no `add`/`remove` bookkeeping.
- Fold the ollama-driven variants into that catalog as ordinary entries, so
  ollama stops being a special command with a required `--integration` flag.
- Support a lightweight per-launch override that does not mutate the stored
  selection, so concurrent sessions can each run a different agent.
- Keep `atm init` exactly as simple as it is today (native harnesses only).

## Non-goals

- No arbitrary/custom agent registration. The catalog is the known built-ins:
  `opencode`, `codex`, `claude`, and their `ollama:<integration>` variants.
- No new plugin type for ollama. Ollama has no plugin of its own; it reads the
  integration's plugin.
- No change to the store's task/label/comment data model.
- No change to actor stamping semantics (see "Actor and env" below).
- No change to the lower-level task/label/store/search commands.

## Concepts

### Agent catalog (code, not config)

The supported agents are fixed and defined in code. Each catalog entry is a small
launch profile:

| Field         | Meaning                                                        |
|---------------|----------------------------------------------------------------|
| `name`        | Selection key. Native: the launcher name. Ollama: `ollama:<integration>`. |
| `launcher`    | One of `opencode`, `codex`, `claude`, `ollama`.                |
| `integration` | Required iff `launcher == ollama`; the harness ollama drives.  |

The catalog is:

```
opencode          launcher=opencode
codex             launcher=codex
claude            launcher=claude
ollama:opencode   launcher=ollama  integration=opencode
ollama:codex      launcher=ollama  integration=codex
ollama:claude     launcher=ollama  integration=claude
```

### Readiness (computed live, never stored)

Each entry's status is derived at display/launch time from the environment:

- **binary check** — `exec.LookPath` of the launcher binary
  (`opencode`/`codex`/`claude`, or `ollama` for ollama entries).
- **plugin check** — `developing.PluginStatus(integration, home)` where the
  integration is the entry's harness (for a native entry, the integration is the
  launcher itself; for `ollama:opencode`, it is `opencode`). Ollama has no plugin
  of its own.

Status resolves to `ready`, or a human-readable gap: `needs binary`,
`needs plugin (atm init)`, or `needs binary + plugin`.

### Stored config (global, minimal)

A new **store-root** file `<ATM_HOME>/agents.json` (sibling of `projects/`, not
per-project). It holds only what cannot be derived:

```json
{
  "updated_at": "2026-07-12T09:00:00Z",
  "updated_by": "admin@cli:unset",
  "selected": "ollama:opencode",
  "args": {
    "ollama:opencode": ["--yolo"]
  }
}
```

- `selected` — the active entry `name`. Absent until `atm init` or
  `atm agents select` sets it.
- `args` — map of entry `name` → default passthrough args. Absent/empty when
  nothing is configured.

Reads return a zero value when the file is missing. Writes are atomic and stamp
`updated_at`/`updated_by` like `ProjectConfig`.

## Command surface

### `atm agents list`

Prints the full catalog with live status, the selected marker, and configured
args:

```
NAME             LAUNCH                      STATUS                   ARGS
opencode         opencode                    ready
codex            codex                       needs plugin (atm init)          *selected
claude           claude                      needs binary + plugin
ollama:opencode  ollama launch opencode --   ready                    --yolo
ollama:codex     ollama launch codex --      needs ollama binary
ollama:claude    ollama launch claude --     needs plugin
```

- `NAME` — the selection key.
- `LAUNCH` — the base argv the entry resolves to (native binary, or
  `ollama launch <integration> --`). This is where the ollama nature is visible.
- `STATUS` — computed readiness.
- `*selected` marker on the active entry.
- `ARGS` — configured default passthrough (blank when none).

JSON output emits the same fields as structured rows.

### `atm agents select <name>`

Sets `selected` in `agents.json`. Errors if `<name>` is not a catalog entry.
If the entry is not `ready`, it still succeeds but **warns** with the readiness
gap — the launch path already surfaces the install hint, and a user may be
selecting ahead of installing.

### `atm agents args <name> [-- <args…>]`

- With no trailing args: prints the entry's configured default passthrough.
- With `-- <args…>`: sets the entry's default passthrough (replaces prior value).
- Errors if `<name>` is not a catalog entry.

## Launch resolution

Both `atm dev` and `atm manage` resolve the agent in this order:

1. `--agent <name>` flag (per-launch override; does not mutate config).
2. `ATM_AGENT` env (per-shell override; does not mutate config).
3. `selected` from `agents.json`.
4. Otherwise: usage error pointing at `atm agents select` / `atm init`.

The resolved catalog entry produces the concrete `developing`/`manager`
`Launcher` — the existing `staticLauncher` for native entries, or
`OllamaLauncher{Integration}` for ollama entries.

Final argv assembly (unchanged mechanics, new source for defaults):

```
launcher base  +  entry default args (agents.json)  +  ATM_<AGENT>_ARGS env  +  trailing passthrough
```

`ATM_<AGENT>_ARGS` is kept for back-compat; the per-entry `args` is the new,
nicer home for defaults. Trailing args after `--` are appended verbatim for
one-offs (e.g. `atm dev --project ATM -- --yolo`), matching today's behavior.

## New launch commands (replacing per-agent subcommands)

- `atm dev --project <CODE> [--persona <P>] [--agent <name>] [-- <passthrough…>]`
- `atm manage --project <CODE> --planning|--grooming|--tracking|--asking|--glossary|--onboarding [--persona <P>] [--agent <name>] [-- <passthrough…>]`

Removed:

- `atm claude`, `atm codex`, `atm opencode`, `atm ollama`
- `atm manage claude|codex|opencode|ollama`

The `--integration` flag disappears — it now lives inside the `ollama:<integration>`
catalog entry.

## Actor and env

Unchanged from today. A launch stamps the actor `<persona>@<launcher>:unset` and
sets `ATM_AGENT` to the **launcher** name — so `ollama:opencode` stamps `@ollama`
exactly as the current `atm ollama` path does. The catalog entry `name` is a
selection/display key only; it never enters the actor string.

## `atm init` integration

`atm init` is unchanged in what it installs — plugins for the native harnesses
`opencode`/`codex`/`claude` only. Ollama is never installed by init (it has no
plugin). The one addition: after a successful install, init writes `selected`
in `agents.json`, defaulting to the first installed native harness, so a fresh
`atm init` leaves `atm dev --project <CODE>` immediately usable.

Readiness for every entry (including ollama variants) is computed live by
`atm agents list`; init writes no per-entry state beyond `selected`.

## Affected code

- **`internal/store`** — new `AgentsConfig` type + `GetAgentsConfig` /
  `SetSelectedAgent` / `SetAgentArgs`, backed by `<root>/agents.json`. Mirrors
  `config.go`'s atomic read/write and actor stamping. A root-level `configPath`
  helper (distinct from the per-project one).
- **`internal/developing` / `internal/manager`** — a shared agent **catalog**
  (name → launcher/integration) and a resolver that maps a catalog entry to the
  existing `Launcher` implementations. Reuse `LauncherFor` and `OllamaLauncher`.
- **`internal/developing/plugins.go`** — reuse `PluginStatus` for the readiness
  plugin check; no new plugin roots.
- **`internal/cli`**
  - New `agents.go`: `atm agents list|select|args`.
  - New `atm dev` command (replaces `newDeveloperAgentCmd` per-agent registration
    and `newDeveloperOllamaCmd`).
  - Rework `manage` to take the agent from resolution rather than an agent
    subcommand; keep the action flags and `manage context`.
  - Shared agent-resolution helper (`--agent` → `ATM_AGENT` → `selected`).
  - `root.go`: stop registering per-agent dev/manage subcommands; register
    `agents` and `dev`; init writes `selected`.
- **`internal/cli/conventions.go`** — update `day_to_day_development` and
  `extra agent args` text to the new command shape.
- **README / CHANGELOG** — document `atm agents`, `atm dev`, and the removal of
  the per-agent subcommands.

## Testing

- Store: `agents.json` round-trip, missing-file zero value, `selected` and
  `args` mutation, atomic write + actor stamp.
- Catalog/resolver: every catalog name maps to the correct `Launcher`; ollama
  entries carry integration; unknown name errors.
- Readiness: `ready` vs each gap variant, driven by fake PATH + plugin state;
  ollama entry readiness keys off the `ollama` binary + integration plugin.
- Resolution order: `--agent` beats `ATM_AGENT` beats `selected`; none set →
  usage error.
- argv assembly: base + entry args + `ATM_<AGENT>_ARGS` + trailing passthrough,
  for a native and an ollama entry.
- CLI: `agents list` output (text + JSON), `select` (valid, unknown, not-ready
  warning), `args` (get + set), `atm dev` / `atm manage` launch happy paths,
  and that the removed subcommands no longer resolve.
- Init: writes `selected` to first installed native harness; no ollama entry
  written; init unchanged when no agent installed.

## Open questions

None blocking. Deferred niceties (out of scope): `atm agents` interactive
picker, per-project agent overrides, and arbitrary custom-agent registration.
