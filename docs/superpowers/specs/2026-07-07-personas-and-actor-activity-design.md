# Personas & Actor Activity — Design Spec

**Status:** Draft (awaiting user review)
**Depends on:** developing agent launcher (2026-07-05), TUI three-pane workspace
(2026-07-04), audit log redesign (2026-07-04).

## Driver

Today an *actor* is an opaque free-form string (`--actor` / `ATM_ACTOR`) stamped
onto every mutation as `CreatedBy` / `UpdatedBy` and onto every log entry's
`Actor` field. It carries no meaning the system understands, and there is no way
to attach reusable guidance to a working identity.

We want two capabilities:

1. **Personas** — reusable, named bundles of guidance ("a staff engineer who
   holds a high bar in review"). When an agent developing session is launched
   with a persona, the session's prompt is enriched with that persona's text, so
   the agent works to those principles.
2. **Actor activity** — visibility into *who* is doing the work: which personas
   are active, what they do, and which agents/models they run under.

### Relationship to the v2 label-substrate philosophy

v2 deliberately removed the old *managed Actor entity* and its intrinsic
workflow knowledge. This design does **not** revive that. The distinction:

- The **actor** stays exactly what v2 made it: a free-form string on log and
  entity records. **No schema change. No new required entity on the write
  path.** An agent with no persona keeps working exactly as today.
- A **Persona** is not a workflow concept the store enforces. It is *global
  configuration* — reusable prompt text plus a name — consulted only at
  **launch time** to (a) enrich the developing prompt and (b) suggest a naming
  **convention** for the free-form actor string. The store never validates that
  an actor references a known persona.
- **Actor activity** is pure *read-side* aggregation over the existing log. It
  invents no new stored state; it parses the actor strings already being
  written.

So personas ride the same rails as the rest of v2: the store stays dumb, and the
meaning lives in a convention (the actor-string format) plus prompt guidance —
not in enforced structure.

## Concepts & terminology

| Term | Meaning |
|------|---------|
| **Persona** | A named, reusable identity + guidance: `{ name, prompt, description? }`. Global (shared across all projects). Holds no agent or model. |
| **Actor** | The runtime identity stamped on an action. Conceptually **Persona + agent + model**. Encoded as a single free-form string (see convention below). |
| **Agent** | The developing harness that was launched: `claude`, `codex`, `opencode`, `ollama`. Known to ATM at launch. |
| **Model** | The concrete model the agent is actually running under (e.g. `opus-4.8`, `gpt-5`). Known only to the **running agent**, not to ATM. |

### Actor-string convention

```
<persona>@<agent>:<model>
```

Example: `staff-engineer@claude:opus-4.8`.

- Purely a **convention**, not an enforced format. Free-form actor strings
  (including today's `claude-dev`, `default`, arbitrary names) remain valid
  everywhere. Resolution is best-effort and total: a legacy string is mapped by
  the alias table (§1b) if present, otherwise convention-parsed, otherwise
  attributed to `persona="(none)"`.
- ATM composes the `<persona>@<agent>` prefix at launch (both pieces are known).
- The `:<model>` suffix is **self-reported by the running agent**, because only
  the agent knows its model. The developing context instructs the agent to stamp
  its ATM commands with `--actor <persona>@<agent>:<its-model>`.

**Resolution order** (best-effort, used only by the read-side aggregation). Each
raw actor string is resolved to a `(persona, agent, model)` triple by:

1. **Alias table** (§1b) — if the raw string has an entry in
   `<store>/actor-aliases.json`, use its mapping. This covers legacy actors
   (`opencode-dev`, `default`, …) and any user-defined override.
2. **Convention parse** — otherwise parse the `<persona>@<agent>:<model>` form:

   ```
   persona = substring before first '@'  (or whole string if no '@')
   rest    = substring after  first '@'
   agent   = substring of rest before first ':'  (or whole rest)
   model   = substring of rest after  first ':'  ("" if absent)
   ```

3. Empty `persona` (e.g. a string starting with `@`, or an unaliased legacy
   string with no `@`) yields `persona="(none)"`; unknown agent/model stay empty.

Resolution never errors: every string yields a triple.

## Components

### 1. Persona store entity (new, global)

New type in `internal/store`:

```go
type Persona struct {
    Name        string    `json:"name"`         // slug: ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$
    Prompt      string    `json:"prompt"`       // the persona guidance text (may be multi-line)
    Description string    `json:"description"`   // optional one-line summary
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
    CreatedBy   string    `json:"created_by"`
    UpdatedBy   string    `json:"updated_by"`
}
```

- **Storage:** one JSON file per persona at `<store>/personas/<name>.json`,
  alongside the existing `projects/` directory. Global, not under any project.
- **Name constraint:** lowercase slug so it is safe in the `<persona>@...` actor
  string and on the filesystem. Reject `@` and `:` explicitly (they are the
  actor-string delimiters).
- **CRUD methods** on `*Store`, mirroring existing label/project patterns:
  `CreatePersona`, `GetPersona`, `ListPersonas`, `SetPersonaPrompt`,
  `SetPersonaDescription`, `RemovePersona`. Writes are lock-guarded and fsynced
  like other store writes.
- **Audit:** persona mutations are **not** added to any project's `log.jsonl`
  (personas are global, logs are per-project). This keeps the audit log's
  subject model unchanged. (A future global audit stream is out of scope.)

### 1a. Built-in (seeded) personas

Two personas ship built-in and are seeded into `<store>/personas/` on demand:

| Name | Purpose |
|------|---------|
| `developer` | The default working persona: implements features/fixes/chores in a developing session. |
| `manager` | Curates the ledger and oversees work (the manager/onboarding side). |

- **Seed data** lives in the data-only `internal/seed` package (mirroring the
  existing label `Labels` slice — no store import), e.g. a `Personas` slice of
  `{Name, Prompt, Description}`. The store applies it via `SeedPersonas`.
- Seeded prompts are short, sensible starting text the user can edit or ignore.
  Seeding is **idempotent** and does not clobber a persona the user has since
  edited (create-if-absent; never overwrite an existing file).
- These two names are also the targets the legacy alias migration maps onto
  (§1b), so historical activity attributes to real, present personas.

### 1b. Actor aliases & legacy migration

Legacy actor strings predate the convention and are **not** rewritten (the log is
append-only; the true persona/model of past work is unknown). Instead a **global
alias table** maps a raw actor string to a `(persona, agent, model)` triple,
consulted first during read-side resolution (see Actor-string convention above).

- **Storage:** a single global file `<store>/actor-aliases.json`:

  ```json
  {
    "opencode-dev":     { "persona": "developer", "agent": "opencode" },
    "opencode-manager": { "persona": "manager",   "agent": "opencode" },
    "ollama-onboard":   { "persona": "manager",   "agent": "ollama" },
    "codex":            { "persona": "developer", "agent": "codex" },
    "default":          { "persona": "developer" }
  }
  ```

  Project logs are untouched; this is global config (like `personas/`), not an
  appended log event — so it adds **no new log action** and needs **no `Replay`
  change**.

- **Migration command:** `atm actor migrate [--dry-run]`.
  1. Seeds the two built-in personas (idempotent, §1a).
  2. Scans every project's `log.jsonl` for distinct actor strings, and for each
     one that lacks an entry and is not already convention-formatted (no `@`),
     writes an alias using the pattern rules below.
  3. Idempotent: never overwrites an existing alias entry (so user edits and
     prior runs are preserved); `--dry-run` prints the diff without writing.

- **Pattern rules** (used only to generate initial alias entries):

  | Raw actor | persona | agent |
  |-----------|---------|-------|
  | `<agent>-manager`, `<agent>-onboard` | `manager` | `<agent>` |
  | `atm-manager` | `manager` | (unknown) |
  | `<agent>-dev`, bare `<agent>` | `developer` | `<agent>` |
  | `default` | `developer` | (unknown) |
  | anything else without `@` | `developer` | (unknown) |

  `<agent>` ∈ {`claude`, `codex`, `opencode`, `ollama`}.

- **User control:** `atm actor alias set <raw> [--persona ...] [--agent ...]
  [--model ...]`, `atm actor alias list [--output json]`, and
  `atm actor alias remove <raw>` let a user correct or add mappings (e.g. "my old
  `codex` work was really the `staff-engineer` persona"). Aliases always win over
  convention parsing, so a user can override even a `@`-formatted string.

### 2. Persona CLI (`atm persona`)

New top-level command group, registered in `newRootCmdWithState`:

```
atm persona create --name staff-engineer --prompt "..."        # or --prompt-file <path>
                   [--description "holds a high bar in review"]
atm persona list   [--output json]
atm persona show   --name staff-engineer [--output json]
atm persona edit   --name staff-engineer [--prompt ... | --prompt-file ...] [--description ...]
atm persona remove --name staff-engineer
```

- `--prompt-file` (mutually exclusive with `--prompt`) because persona text is
  usually multi-line prose; reads the file contents as the prompt.
- `edit` is upsert-friendly for the fields provided; omitted fields are left
  unchanged. It errors if the persona does not exist (create is explicit).
- Text and JSON output follow the existing deterministic-output conventions
  (`internal/cli/output.go`); list is sorted by name.
- Persona commands do **not** require `--actor` for reads. Mutations stamp
  `CreatedBy`/`UpdatedBy` from the resolved actor (reusing `resolveActor`), for
  provenance only.

### 3. Launch integration (`atm developing ... --persona`)

Extend `internal/cli/developing.go` and the developing context template.

- New flag `--persona <name>` on each `atm developing <agent>` subcommand and on
  the `ollama` subcommand.
- When set, ATM:
  1. Resolves the persona from the store (error if unknown).
  2. Composes the **actor base** `<persona>@<agent>` and uses it as the default
     actor (replacing today's `<agent>-dev` default). An explicit `--actor`
     still overrides.
  3. Injects the persona into the rendered developing context (see §4).
  4. Exports launch env vars:
     - `ATM_PERSONA=<persona>` (new)
     - `ATM_AGENT=<agent>` (new)
     - `ATM_ACTOR=<persona>@<agent>` (existing var, new default value)
     - `ATM_ROLE=developing` (**unchanged** — session mode, not the persona)
- When `--persona` is absent, behavior is exactly as today (`ATM_PERSONA`/
  `ATM_AGENT` unset; actor defaults to `<agent>-dev`).

`ATM_ROLE` deliberately keeps its current meaning (session mode:
`developing`/`manager`). The persona rides `ATM_PERSONA` to avoid collision.

### 4. Developing context enrichment

The developing context template (`internal/developing/context_v1.md`, rendered by
`RenderContext`) gains:

- A new `<PERSONA>` placeholder and a **Persona** section, rendered only when a
  persona is active. It embeds the persona's `prompt` verbatim under a heading
  like:

  ```
  ## Persona: <name>

  <prompt>

  You are operating as this persona. Hold to its principles throughout the
  session, alongside repo instructions and the working routine below.
  ```

- The **actor-string convention** is documented in the context so the agent
  stamps correctly, including self-reporting its model:

  ```
  Stamp your ATM commands with --actor <ACTOR>:<your-model>, e.g.
  --actor staff-engineer@claude:opus-4.8, filling in the model you are
  actually running as. If unsure of the model, use --actor <ACTOR>.
  ```

- When no persona is active, the Persona section is omitted and the actor
  guidance falls back to the plain `<ACTOR>` value (today's behavior).

`ContextData` gains `Persona string` (the name) and `PersonaPrompt string`
fields. Whether this is a new `context_v2.md` or an edit to `context_v1.md` is an
implementation choice for the plan; the rendered output is what this spec fixes.

### 5. Actor activity — API (`atm activity`)

New read-only command that aggregates the per-project log by actor.

```
atm activity --project ATM [--group-by persona|agent|model|actor] [--output json]
```

- Reads `projects/<CODE>/log.jsonl` via `ReadLog`, resolves each entry's `Actor`
  to a `(persona, agent, model)` triple (alias table first, then convention
  parse — see Actor-string convention), and counts activity.
- Default `--group-by persona`. Output rows carry the group key and a count; the
  JSON form additionally exposes the breakdown needed by the TUI, e.g.:

  ```json
  [
    { "persona": "staff-engineer", "count": 42,
      "agents": { "claude": 30, "codex": 12 },
      "models": { "opus-4.8": 30, "gpt-5": 12 },
      "actions": { "task.created": 5, "comment.created": 20, ... } }
  ]
  ```

- Deterministic ordering: by count desc, then key asc.
- This is the **queryable substrate** the TUI pane renders — API-first: the pane
  is a thin client over this aggregation logic (shared Go function in
  `internal/store` or a small `internal/activity` package, not duplicated).

### 6. Actor activity — TUI (`[4] Actors` pane)

Add a fourth workspace pane to the TUI.

- `internal/tui/app.go`: extend the `workspacePane` enum with `paneActors`, bump
  `numPanes` to 4, add a `[4] Actors` tab and its render slot in the layout. The
  existing three-pane layout math (`SetSize`, `renderPane`) is generalized to
  accommodate the extra pane; the exact split is a layout-design detail for the
  plan (the Labels-pane precedent of a list view + chart view is the model).
- New `internal/tui/actors.go` holding an `actorsModel` with:
  - **List view:** personas ranked by activity in the current project scope,
    rendered as a horizontal bar chart reusing `meterBar` (as the Labels pane's
    `renderChart` does) — persona name, meter, percent, count.
  - **Detail view (Enter on a persona):** that persona's **favorite
    agents/models** breakdown and its **activity by action**, again as
    meter-bar rows. `Esc` returns to the list.
- Data source: the shared aggregation from §5, scoped to the pane's active
  `projectScope` (consistent with the other panes). A cross-project roll-up is
  explicitly **out of scope** for this iteration (YAGNI).
- Legacy actors resolve through the alias table (§1b), so after `atm actor
  migrate` they attribute to the `developer`/`manager` built-in personas with
  their agent. Any unaliased, non-convention string falls back to
  `persona="(none)"` — never dropped.

## Data flow

```
Migration (one-time, opt-in):
  atm actor migrate
    -> seed built-in personas: developer, manager
    -> scan all project logs for distinct legacy actors
    -> write <store>/actor-aliases.json (pattern rules; idempotent)

Launch:
  atm developing claude --persona staff-engineer
    -> resolve Persona{staff-engineer}
    -> render context with Persona section + actor convention
    -> env ATM_PERSONA, ATM_AGENT, ATM_ACTOR=staff-engineer@claude
    -> exec claude

Runtime (inside the agent):
  agent stamps: atm task comment add ... --actor staff-engineer@claude:opus-4.8
    -> log.jsonl entry { actor: "staff-engineer@claude:opus-4.8", action, ... }

Read-side:
  atm activity --project ATM
  TUI [4] Actors pane
    -> ReadLog(ATM) -> resolve actors (alias table -> convention parse)
    -> aggregate by persona/agent/model/action
    -> render rows / bar charts
```

## Error handling

- `atm persona create` with an existing name → `ErrUsage` ("persona already
  exists"). `edit`/`show`/`remove` on a missing name → not-found error.
- Invalid persona name (contains `@`, `:`, uppercase, or fails the slug regex) →
  `ErrUsage` with the constraint.
- `--persona` on launch referencing an unknown persona → `ErrUsage` before the
  agent is exec'd (fail fast, don't launch a mis-stamped session).
- `--prompt` and `--prompt-file` both set → `ErrUsage`. `--prompt-file` pointing
  at a missing/unreadable file → wrapped IO error.
- Actor resolution never errors: any string yields a `(persona, agent, model)`
  triple (alias table → convention parse), with unknown pieces left empty.
- `atm activity` on a project with an empty/absent log → empty result, not an
  error.
- `atm actor migrate` is idempotent and safe to re-run: it never overwrites an
  existing alias entry or an already-edited built-in persona; `--dry-run` writes
  nothing. A malformed/hand-edited `actor-aliases.json` → wrapped parse error.
- `atm actor alias set` with an unknown `--persona` is allowed (aliases may point
  at a persona name that is not yet defined; read-side treats the name as a label
  only), keeping the alias table decoupled from persona lifecycle.

## Testing

- **Store:** persona CRUD round-trips; name validation (rejects `@`, `:`,
  uppercase, empty); prompt/description upsert; list determinism; remove.
- **Seed:** `SeedPersonas` creates `developer`/`manager` when absent; is
  idempotent; never overwrites a user-edited built-in.
- **Actor resolution:** table test covering alias-table hits (`opencode-dev` →
  developer@opencode, `ollama-onboard` → manager@ollama, `default` → developer)
  and, for unaliased strings, convention parse of `persona@agent:model`,
  `persona@agent`, `persona`, leading-`@`, empty string, extra `:` in model;
  alias precedence over a `@`-formatted string.
- **Migration:** `atm actor migrate` over a synthetic multi-project store
  produces the expected alias file from the pattern rules; is idempotent
  (second run is a no-op); `--dry-run` writes nothing; preserves pre-existing
  and user-edited entries.
- **Aggregation:** given a synthetic log, `atm activity` groups correctly by
  persona/agent/model/action with deterministic ordering; post-migration legacy
  actors attribute to built-in personas; unaliased strings bucket under `(none)`.
- **CLI:** `atm persona` text + JSON output snapshots; determinism test extended;
  `--prompt`/`--prompt-file` mutual exclusion; unknown-persona launch error.
- **Launch:** with `--persona`, env values and default actor
  (`persona@agent`) are set; context render includes the Persona section and the
  convention line; without `--persona`, output is byte-identical to today.
- **TUI:** `actorsModel` renders list + detail from a fixture aggregation; tab
  switching reaches `[4] Actors`; layout snapshot with four panes; empty-log
  pane renders a muted "no activity" state.

## Out of scope (YAGNI)

- Cross-project / global activity roll-up in the TUI.
- A global audit stream for persona mutations.
- ATM forcing or validating the agent's actual model (self-reported only).
- Per-project persona overrides (personas are global).
- Binding a favorite agent/model *into* the persona entity (favorites are an
  observed, charted property, not stored on the persona).
- **Rewriting historical log entries.** Migration records mappings in the alias
  table; it never mutates `log.jsonl`.
- Auto-running migration on upgrade. `atm actor migrate` is explicit/opt-in.

## Rollout / compatibility

Fully backward compatible. **No log rewriting** — existing entries keep their
free-form actor strings; the read-side resolves them via the alias table.

- Sessions launched without `--persona` are unchanged (actor default stays
  `<agent>-dev`), and such strings resolve to the `developer` persona once
  aliased.
- The `personas/` directory is created on first `atm persona create` or
  `atm actor migrate` (which seeds `developer`/`manager`); `actor-aliases.json`
  on first `atm actor migrate` / `atm actor alias set`.
- Migration is **opt-in**: until the user runs `atm actor migrate`, legacy
  actors simply resolve to `(none)` + best-effort agent via convention parse —
  nothing breaks, the pane is just less pretty. Running it backfills attribution
  to the two built-in personas.
