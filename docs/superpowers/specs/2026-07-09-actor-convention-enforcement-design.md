# Actor Convention Enforcement — Design Spec

**Status:** Draft (awaiting user review)
**Task:** ATM-0072 (Decide TUI default actor stamp + alias)
**Depends on:** Personas & Actor Activity (2026-07-07), audit log redesign
(2026-07-04), developing agent launcher (2026-07-05), atm-manager subagent
(2026-07-06).
**Forward dependency:** ATM-0083 ("Agent as configs not flags") will later
supply real `:model` values from config; this spec uses a prompt-driven /
placeholder model until then.

## Driver

Today an *actor* is an opaque free-form string. It is stamped verbatim into the
log, and the only write-time rule is "non-empty". Meaning (which persona/agent/
model it represents) is reconstructed at read time, and only partially:

- `actor.Resolve` parses the `persona@agent:model` convention when present, but
  legacy strings (`default`, `ollama-dev`, `atm-manager`, …) only resolve to a
  persona if a one-time migration (`atm actor migrate`) previously wrote an
  entry into `actor-aliases.json`. Nothing consults `actor.LegacyAlias` at read
  time.
- The bare TUI launch path stamps the literal string `"default"`, which is
  meaningful only post-migration. CLI `developing`/`manager` sessions stamp
  `<agent>-dev` / `<agent>-manager`, also non-conforming.

The result is three overlapping mechanisms (convention parse, an alias file, a
migration command) and a write path that will happily record identities the
system cannot explain.

**This design makes the canonical form `persona@agent:model` the *only* thing
written to the log, enforces a registered persona at the store, moves full
identity resolution to the client, and replaces the entire alias/migration
subsystem with pure read-time inference.**

### Relationship to the 2026-07-07 Personas design (deliberate reversal)

The earlier Personas spec deliberately kept the store dumb: *"The store never
validates that an actor references a known persona."* **This design reverses
that decision.** Persona registration becomes a write-time invariant enforced by
the store. The rest of that spec (personas as global config, activity as
read-side aggregation) still holds; only the "store stays dumb about actors"
principle is superseded here.

## Goals

1. Every newly written log/entity stamp is `persona@agent:model`, all three
   segments present and non-empty.
2. The `persona` segment must be a **registered** persona, or the mutation
   errors. No silent fallback.
3. Persona is inferred by *context* at the client: `atm developing`→`developer`,
   `atm manager`→`manager`, human CLI/TUI→a new built-in `admin`.
4. `developer` / `manager` / `admin` are protected built-ins that cannot be
   removed.
5. Old, non-conforming actors already in the logs are translated to a persona
   **at read time** — no migration, no log rewrite.
6. Injected `developing`/`manager` prompts incorporate the persona's description
   (and prompt), and instruct the host agent to stamp its real `agent:model`.

## Non-goals

- No rewriting of existing log entries or entity `CreatedBy`/`UpdatedBy` fields.
- No real model detection in the ATM binary — that is ATM-0083's job. This spec
  stamps `:unset` (human sessions) or defers to the prompt (agent sessions).
- No constraint on the `agent`/`model` segment values beyond "non-empty".

## Division of labor

The central architectural idea: **the client owns identity resolution; the store
owns validation.**

| Concern | Owner |
|---|---|
| Build the full `persona@agent:model` (including the real model) | **Client.** For `developing`/`manager`, the host agent, driven by the injected prompt. For the ATM binary's own human CLI/TUI sessions, the binary. |
| Actor shape valid + persona registered? | **Store** `validateActor` — authoritative gate hit by every caller (CLI, TUI, tests, future agents). |
| Friendly early error for a bad `--actor` | CLI (optional convenience); the store re-checks regardless. |

## Design

### 1. The `admin` built-in + protected personas

Add a third entry to `seed.Personas`:

- **`admin`** — "Human operator persona: a person driving ATM directly via the
  CLI or TUI, not an autonomous agent." Description + prompt describe the
  human-operator role.

`developer`, `manager`, `admin` are **protected**: `Store.RemovePersona` returns
`ErrUsage` ("cannot remove built-in persona %q") for any of them. Users may still
`EditPersona` their prompt/description, and `CreatePersona` new personas freely.

`Init` seeds all three (via `SeedPersonas`) before any validated write occurs.

### 2. Store-layer enforcement: `validateActor`

New unexported helper on `Store`:

```
func (s *Store) validateActor(raw string) error
```

Rules:

1. Parse `persona@agent:model`: split on the first `@`, then split the remainder
   on the first `:`. **All three** of `persona`, `agent`, `model` must be
   non-empty, else `ErrUsage: actor must be persona@agent:model (got %q)`.
2. `persona` must resolve to an existing persona file (`GetPersona`), else
   `ErrUsage: unknown persona %q; create it first with 'atm persona create'`.
3. `agent` / `model` are accepted as free-form non-empty strings.

Every log-producing mutation replaces its current `if actor == ""` guard with
`if err := s.validateActor(actor); err != nil { return … }`. This covers:
task create / meta-change / label add-remove, comment create / edit / remove /
label, project mutations, and persona create / edit (a human editing a persona
stamps `admin@cli:unset`, which is valid once `admin` exists). `RemovePersona`
takes no actor today and stays that way; it is guarded by the built-in check in
§1, not by `validateActor`.

**Bootstrap exemption.** Seeding the three built-ins is the one write that cannot
satisfy rule 2 (the persona is being created). `Init` / `SeedPersonas` write the
built-in persona files through a path that **skips `validateActor`**, stamping a
reserved system actor `admin@atm:seed`. After init, `admin`/`developer`/`manager`
exist, so all subsequent writes — including user-driven persona CRUD — validate
normally. (Concretely: `SeedPersonas` writes the built-ins with a
validation-skipping internal helper; the public `CreatePersona` used for
user-created personas goes through `validateActor`.)

### 3. Read-time inference replaces the alias subsystem

`actor.Resolve` becomes a **pure function with no alias argument**:

```
func Resolve(raw string) Identity
```

1. Contains `@` → parse the convention (`persona@agent:model`; empty persona →
   `(none)`).
2. Otherwise, legacy inference (the current `LegacyAlias` rules, folded inline):
   - `default` → persona `developer`
   - `<agent>` and `<agent>-dev` → `developer` + that agent
   - `atm-manager`, `<agent>-manager`, `<agent>-onboard` → `manager` + that agent
   - any other non-conforming string → `developer`
     (preserves current activity aggregation; a future tweak could route the
     truly-unknown bucket to `(none)`).

`activity.Build` drops its `aliases` parameter and calls `Resolve(e.Actor)`.

**Deletions:**

- `internal/store/alias.go` (`LoadAliases`, `SetAlias`, `RemoveAlias`,
  `MigrateActors`, `MigrationResult`) and `internal/store/alias_test.go`.
- `internal/cli/actor.go` in full — the `atm actor` command group (`migrate`,
  `alias set/list/remove`) is removed (it would otherwise be empty).
- `actor.LegacyAlias`, `actor.AliasMap`, `actor.AliasEntry`.
- All `LoadAliases`/aliases plumbing through `activity`, TUI actors pane, and
  `atm activity`.

Existing `actor-aliases.json` files on disk become inert (never read, never
written); they are left in place, not deleted.

### 4. Client-side actor construction

The ATM binary constructs a **floor default** and expands shorthand; the host
agent may override with its real model.

**Human, non-prompt sessions (the binary is the client):**

| Context | No `--actor`/`ATM_ACTOR` | Bare `--actor foo` | Full `--actor foo@x:y` |
|---|---|---|---|
| plain CLI | `admin@cli:unset` | `foo@cli:unset` | as-is |
| TUI | `admin@tui:unset` | `foo@tui:unset` | as-is |

- `resolveActor` (root.go) default becomes `admin@cli:unset`; the `"anonymous"`
  fallback is removed.
- TUI `NewModel` fallback becomes `admin@tui:unset` (replaces `"default"`).
- A bare `--actor foo` is treated as a **persona name**, expanded to
  `foo@<surface>:unset`. If `foo` is not a registered persona the store rejects
  the first mutation; the CLI may pre-check to fail fast with the same message.

**Prompt-driven sessions (the host agent is the client):**

| Context | Launcher floor default | Prompt drives → |
|---|---|---|
| `atm developing claude` | `developer@claude:unset` | `developer@claude:<real-model>` |
| `atm developing … --persona P` | `P@claude:unset` (P must be registered) | `P@claude:<real-model>` |
| `atm manager claude` | `manager@claude:unset` | `manager@claude:<real-model>` |

- `defaultDevelopingActor` / `defaultManagerActor` produce
  `<persona>@<agent>:unset` (persona defaulting to `developer` / `manager`,
  agent = the launcher agent name). This is the floor so bare `atm` commands in
  the session validate.
- The injected prompt (see §5) instructs the agent to stamp its **real** model,
  so real sessions record `…:<model>` rather than `:unset`.
- `--persona P` with an unregistered `P` errors at launch (CLI pre-check) and at
  the store.

Note: `atm developing`/`manager` agents are `claude`, `codex`, `opencode`,
`ollama` — the agent segment is the real host agent name, which makes
`atm activity --group-by agent` meaningful for agent sessions.

### 5. Prompts carry the persona description + model instruction

`developing`/`manager` sessions now **always** have a persona (default
`developer` / `manager`), so the persona context block always renders.

- `developing.ContextData` (and `manager.ContextData`) gain a
  `PersonaDescription` field. The rendered persona block includes the persona
  **name + description + prompt** (today it renders only the prompt, and only
  when `--persona` was explicitly supplied).
- The developing/manager launcher always looks up the effective persona
  (`developer`/`manager`/`--persona`) and passes name+description+prompt into
  the context.
- The context template adds a short instruction directing the agent to stamp its
  ATM actor as `<persona>@<agent>:<model>` using its own model — this is the
  mechanism by which the client resolves the real model (superseded by ATM-0083
  config later).

### 6. Documentation

`internal/cli/conventions.go` actor-identity text is rewritten: remove the
`atm actor migrate` / `atm actor alias` references; state that the actor is
always `persona@agent:model`, that the persona must be registered, and that
legacy strings are inferred at read time.

## Data flow

**Write (agent developing session):**
```
atm developing claude --persona developer
  → launcher: floor ATM_ACTOR = developer@claude:unset  (persona validated present)
  → prompt injected: persona block + "stamp your model"
  → agent runs: atm task comment … --actor developer@claude:opus-4.8
      → store.validateActor: shape ok, persona "developer" registered ✓
      → log entry Actor = "developer@claude:opus-4.8"
```

**Write (human TUI):**
```
atm tui                      (no --actor / ATM_ACTOR)
  → NewModel: actor = admin@tui:unset
  → mutation → validateActor: persona "admin" registered ✓
  → log entry Actor = "admin@tui:unset"
```

**Read (activity over mixed old + new logs):**
```
entries: ["default", "ollama-dev", "developer@claude:opus-4.8", "admin@tui:unset"]
  → Resolve("default")                 → {developer}
  → Resolve("ollama-dev")              → {developer, ollama}
  → Resolve("developer@claude:opus-4.8") → {developer, claude, opus-4.8}
  → Resolve("admin@tui:unset")         → {admin, tui, unset}
  → Aggregate by persona: developer×2, admin×1
```

## Error handling

- Empty or malformed actor (missing `@` or `:`, empty segment) → `ErrUsage`,
  surfaced by the store and mirrored by the CLI pre-check.
- Unregistered persona → `ErrUsage` with a "create it first" hint.
- `RemovePersona` on a built-in → `ErrUsage`.
- Read-time inference never errors: an unparseable/unknown legacy string maps to
  `developer` (or `(none)` for the empty-persona convention case).

## Testing

- **actor package:** `Resolve` table tests for convention parse, each legacy
  rule, and the unknown fallback; confirm `LegacyAlias`/`AliasMap` removal
  compiles nothing.
- **store:** `validateActor` unit tests (good triple, each malformed shape,
  unregistered persona, registered persona); `RemovePersona` built-in guard;
  `Init` seeds `admin` and a subsequent `admin@cli:unset` mutation succeeds;
  bootstrap path writes built-ins without a pre-existing persona.
- **cli:** default actors — plain CLI `admin@cli:unset`, TUI `admin@tui:unset`,
  `developing` `developer@claude:unset`, `manager` `manager@claude:unset`; bare
  `--actor foo` expansion; unregistered `--actor`/`--persona` error.
- **activity / TUI actors pane:** aggregation over mixed legacy + convention
  logs without an alias file.
- **Blast radius:** every existing test that passes a bare actor
  (`"tester"`, `"default"`, `ollama-dev`, …) directly to a store mutation must
  switch to a conforming, registered actor (e.g. `admin@cli:unset`) or seed the
  needed persona. This is the bulk of the mechanical work.

## Migration / rollout

- No data migration. Existing logs are read-inferred; existing
  `actor-aliases.json` files are ignored.
- The `atm actor` command group is removed — a behavior change for any tooling
  that shells out to `atm actor migrate`/`alias`. Documented in the release
  notes.

## Open questions (resolved defaults, vetoable)

- **Unknown legacy string → `developer`** (chosen, preserves current activity
  numbers). Alternative: `(none)`.
- **`atm actor` group removed entirely** (chosen). Alternative: keep the group
  as a stub.
