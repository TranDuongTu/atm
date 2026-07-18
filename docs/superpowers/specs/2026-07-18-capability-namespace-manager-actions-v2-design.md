# Capability namespace + manager actions v2 — design

Date: 2026-07-18
Status: approved design (brainstormed 2026-07-18)
Supersedes (in part): the conventions/manager-prompt shape from `2026-07-18-capability-semantics-initiative-design.md` (phases 1-3, now merged). This initiative reshapes what that initiative built — the substrate stays, the capability contract and the manager action model change.

## Problem

The capability initiative shipped a working shape, but three concerns surfaced in review:

1. **`atm conventions` is still too heavy.** It carries substrate-wide advisory prose (label hygiene, how to read a task, how to narrate, how to search, a 9-step first-contact sequence, a first-time human sequence, notes) plus a registry-composed `## Capabilities` section. Capability-specific semantics already live in capability guides; the conventions document should be a *minimal substrate primer* that points at the capability namespace, not a restatement of substrate-wide advisory.

2. **Capability discovery is scattered.** A capability's command mounts at the cobra root (`atm workflow`, `atm context`), so an agent has no single namespace to enumerate capabilities. The registry-composed conventions section names them, but disabled capabilities are invisible (their `<cmd>` is unmounted). There is no programmatic `list`/`show` for capabilities.

3. **The manager action model is capability-coupled and ad hoc.** The manager prompt hardcodes `<CAPABILITY_ROLES>` from `ManagerActions()`, and `atm manage --action mapping` is a capability-contributed action tied to a launcher argv flavor (`BuildArgvOnboard` + `ATM_ONBOARD=1`) hardcoded in the manager for `action == "mapping"`. The actions (`curate`, `recall`, `mapping`) are vague ("autonomous owner" prose) and 1:1 with capabilities; the manager has no semantic-agnostic action vocabulary that applies to *every* capability.

4. **`DefaultBoard` is a capability-side UI policy.** `DefaultBoard(code) string` on the `Capability` interface makes a capability nominate the TUI's default board — a frontend concern living in the wrong layer. Capabilities should declare their boards; the TUI/CLI picks the default.

5. **`internal/seed/seed.go` is a parallel seeding path.** Substrate-wide labels (`comment:*`, `priority:*`, `status:*`, `context:*`) are seeded by `seed.go` on project create / `atm label seed`, and capability-specific labels are seeded by `EnsureVocabulary`. The two paths overlap (`status:*` is in both `seed.go` and `workflow`; `context:*` is in both `seed.go` and `contextmap`). Capabilities should own their vocabulary outright; the global default seed should go away.

## Goal

- Conventions becomes a minimal substrate primer that points at `atm capability` for capability discovery.
- All capability commands move under `atm capability <name> <verb>`; `atm capability list` enumerates registered (enabled + disabled) capabilities; `atm capability <name> -h` / `guide` discover a capability.
- The `Capability` interface drops `DefaultBoard` and `ManagerActions`; `EnsureVocabulary` returns the boards the capability owns (labels with `Expr`).
- The manager has three semantic-agnostic actions — `brief`, `autopilot`, `ask` — scoped by an optional `--capability <name>` (default: all enabled). Each capability's guide carries `## Brief` and `## Autopilot` sections; the manager reads the relevant section per capability.
- `seed.go` is removed; capabilities own all label seeding; un-managed labels are invented on demand.
- The launcher onboard flavor (`BuildArgvOnboard`, `ATM_ONBOARD`, the onboarding tmux label) is removed; one argv flavor for all three actions.

## Design

### 1. `atm conventions` — minimal substrate primer

The hand-written `conventionsCoreText` is replaced with:

```
# ATM Conventions (advisory)

## What ATM is
ATM (Agent Tasks Management) is a label-substrate task store. A project holds tasks; each task has free-form text (title, description) and a set of labels. No status field, no claims, no review queue, no state machine — status, type, priority, ownership, relationships are all labels, interpreted by the agent reading them. The store keeps the substrate legible; capabilities own the semantics.

## Substrate
Substrate commands live under these namespaces; run `-h` on each for verbs and flags:
- `atm task` — tasks (ID, title, description, labels).
- `atm task comment` — per-task append-mostly thread, classified by a label.
- `atm label` — labels (`<CODE>:<ns>:<value>` or `<CODE>:<tag>`); a label's description records its intention. Three kinds: stored (asserted), namespace (prefix, emergent), board (computed from an expression).
- `atm project`, `atm persona`, `atm activity`, `atm store`, `atm search` — project lifecycle, actor identity, audit log, semantic search.

## Capabilities
Semantics beyond the substrate live in capabilities. Each owns a slice of the label substrate, contributes verbs, and explains itself. A project enables a per-project subset; commands for disabled capabilities are not mounted.
- `atm capability list` — enumerate registered capabilities (enabled + disabled).
- `atm capability <name> -h` — the verb tree a capability mounts.
- `atm capability <name> guide` — the capability's full agent-facing semantics, vocabulary, and operating mode (Brief + Autopilot sections).

## Actor identity
Every mutation stamps `persona@agent:model` (e.g. `developer@claude:opus-4.8`). `atm persona -h`; built-ins `developer`, `manager`, `admin`. `atm dev -h`.

Conventions are advisory only.
```

**Removed vs. current**: "How to read a task and its labels", "How to narrate progress", "How to search", the detailed "Boards (computed labels)" prose, the "Agent first-contact sequence" (9 steps), the "Agent code-of-conduct" (7 label-hygiene rules), the "First-time human sequence", "Notes", and the registry-composed `## Capabilities enabled for this project` section (replaced by the `atm capability list` pointer).

**Substrate command help fattening** (implementation requirement): because conventions now points at `-h` for each substrate namespace, the cobra `Short`/`Long` on `atm task`, `atm task comment`, `atm label`, `atm project`, `atm persona`, `atm activity`, `atm store`, `atm search` must be genuinely informative. Today several are thin. The plan audits each and adds a `Long` that covers the namespace's purpose, key verbs, and any conventions the agent must know (e.g. `atm task -h` explains the ID formats; `atm label -h` explains the three kinds and the description-is-intention-record rule).

**JSON envelope**: `conventionsStructured` drops `seeded_labels`, `code_of_conduct`, `first_time_human_sequence`, `agent_first_contact_sequence`, `day_to_day_development`, `advisory`, and the composed `capabilities` list. It keeps `what_atm_is`, `substrate` (a list of namespace pointers), `capabilities` (a one-line pointer at `atm capability list`), `actor_identity`.

### 2. `atm capability` namespace

A new `atm capability` command (`internal/cli/capability.go`) becomes the single mount point for capability commands and the discovery surface.

**`atm capability list`** — enumerate registered capabilities. Output columns: `NAME`, `SUMMARY`, `ENABLED` (for the target project). `--all` ignores the project and lists all registered as enabled. JSON envelope:
```json
{"capabilities":[{"name":"workflow","summary":"...","enabled":true},{"name":"contextmap","summary":"...","enabled":true}]}
```
`--project` is resolved by the pre-parse mount (mountProjectCode), so the enabled/disabled status reflects the target project. The enumeration reads from the **full** registry (`st.fullRegistry`), not the mount-narrowed one, so disabled (registered-but-not-enabled) capabilities appear with `enabled:false` — the agent learns they exist even though their `atm capability <name>` subcommand is unmounted. `--all` ignores the project and lists every registered capability as enabled.

**`atm capability <name> ...`** — each enabled capability's cobra command tree, mounted under `atm capability <name>`. Today's `atm workflow start` becomes `atm capability workflow start`; today's `atm context add` becomes `atm capability context add`. The capability's `Command(env)` still builds its tree; the registry mounts it under `atm capability <name>` instead of at the root.

**`atm capability <name> guide`** — the deep guide. The registry continues to mount the uniform `guide` subcommand per capability (`internal/capability/capability.go` `newGuideCmd`); its location moves from `atm workflow guide` to `atm capability workflow guide`.

**Hard gate**: `atm capability <name>` is unmounted for disabled capabilities (cobra "unknown command"). `atm capability list` still lists them as disabled. The gate stays on the tooling surface only — the store still accepts anything.

**Migration / flag day**: `atm workflow start` → `atm capability workflow start` is a CLI surface break. ATM is pre-1.0; the capability initiative just merged. Any script/keybinding calling `atm workflow`/`atm context` directly must update. The plan notes this loudly; no compat shim (the old commands are not retained as aliases).

### 3. Capability interface

```go
type Capability interface {
    Name() string
    Summary() string
    Guide() string
    Command(env Env) *cobra.Command
    EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error)
}
```

**Removed**:
- `DefaultBoard(code) string` — the TUI/CLI picks the default (see §6).
- `ManagerActions() []ActionSpec` — replaced by the three semantic-agnostic manager actions (see §5); capability guides carry `## Brief` and `## Autopilot` sections instead.

**Changed**:
- `EnsureVocabulary` returns `([]core.Label, error)`. It seeds **all** the capability's labels (non-board + boards) into the store idempotently (`LabelSeed` upserts only when absent; a human's curated description is never overwritten) and returns the **board labels** (those with `Expr`) the capability owns. The capability is fully self-contained — one call leaves the project fully seeded for that capability, and the caller gets the boards back.

**Guarantee**: `EnsureVocabulary` is safe to run on any project at any time. It is idempotent and additive — it never deletes or overwrites. A new board added in a new capability version appears on the next run (project select in the TUI, `atm label seed`, `atm project capability add`, `atm project create`). No migration step.

**Registry** (`internal/capability/capability.go`):
- `EnsureVocabulary(svc, code, actor)` aggregates returned boards across enabled capabilities (registration order), returning the union.
- `Describe(env) []Description` stays (for `atm capability list`).
- `Commands(env)` stays — each capability's command tree is mounted under `atm capability <name>`.
- `For(project)` stays (mount-narrowing).
- `Names()` stays.
- **Removed**: `DefaultBoard`, `ManagerActions`.

### 4. `seed.go` removal + `atm label seed` reshape

**Remove `internal/seed/seed.go`** entirely. No global default seed. No substrate-wide label seeding.

**`atm label seed --project <CODE>`** reshapes to "re-apply each enabled capability's vocabulary":
- Calls `st.registry.EnsureVocabulary(s, project, actor)`; collects the returned boards.
- Emits the seeded label names + boards.
- The TUI's [S] key does the same.

**`atm project create`**:
- No `s.SeedLabels(project, actor)` (gone with `seed.go`).
- Calls `registry.EnsureVocabulary` for the chosen capabilities; consumes returned boards.

**`atm project capability add`**:
- Enables the capability; calls `EnsureVocabulary` for it; consumes returned boards.

**Labels not managed by any capability** (`type:bug`, `fixit`, custom namespaces an agent invents) are created on demand via `atm label add`. No seed. This is consistent with "capabilities own the semantics; the substrate is bare."

**Substrate-wide labels that previously lived in `seed.go`**:
- `comment:*` + `comment:progress/decision/open-question`: no capability owns them. Agents invent comment kinds on demand; `atm task comment add --label <CODE>:<kind>` works regardless. `comment:provenance` is owned by contextmap (its non-board vocabulary).
- `priority:*` + `priority:high`: no capability owns them. Agents invent priority on demand.

A fresh project has **no labels** until a capability is enabled and `EnsureVocabulary` runs, or until an agent creates one. The TUI Boards pane continues to surface undescribed-namespace warnings for agent-invented labels; that becomes the default state.

### 5. Manager action model — brief / autopilot / ask

Three semantic-agnostic actions, scoped by an optional `--capability <name>` (default: all enabled). `curate` and `recall` are absorbed; `ManagerActions()` is removed from the interface.

| Action | Behavior | Per-capability guide section |
|---|---|---|
| `brief` | Manager interviews the human to set up the project per each enabled capability's philosophy. For contextmap: "where is your knowledge? what does this repo contain? what docs matter?" → records context pointers from the human's answers. For workflow: "what status values do you use? what does 'done' mean?" → seeds/curates accordingly. | `## Brief` |
| `autopilot` | Manager autonomously ensures the project follows each enabled capability's guide. For contextmap: run `check`, verify drift, discover new territory, stamp/retarget/supersede (today's "mapping" procedure). For workflow: status hygiene, board hygiene. | `## Autopilot` |
| `ask` | Manager does nothing proactively; standby for the human to ask questions. Read-only. The manager reads each enabled capability's guide to be ready to answer. | (whole guide) |

**CLI** (`atm manage`):
- `atm manage --project <CODE> [--action brief|autopilot|ask] [--capability <name>]`
- `--action` defaults to `autopilot` (the most common manager mode — keep the project following the guides). `brief` is explicit setup; `ask` is explicit standby.
- `--capability <name>` scopes to one capability. Validated against the **full** registry first (unknown name → usage error listing registered capabilities), then against the enabled set (known but not enabled → "capability <name> is not enabled for project <CODE>; run `atm project capability add --project <CODE> --name <name>` first"). Empty = all enabled.
- **Removed flags**: `--curate`, `--recall`, `--mapping`, `--onboarding`. Pre-1.0 break; no deprecated aliases.
- `validateManagerAction` simplifies: action ∈ {brief, autopilot, ask}; capability ∈ enabled set or empty.

**Manager prompt** (`internal/manager/context_v1.md`):
- `<CAPABILITY_ROLES>` placeholder **removed** — no composed role list.
- `<ACTION_BLOCK>` restated per action:
  - `brief`: "Focus this session on **brief**. For each enabled capability (or just `<capability>` if scoped), run `atm capability <name> guide` and follow its `## Brief` section — interview the human to set up that capability's territory."
  - `autopilot`: "Focus this session on **autopilot**. For each enabled capability (or just `<capability>` if scoped), run `atm capability <name> guide` and follow its `## Autopilot` section — autonomously maintain that capability's territory."
  - `ask`: "Focus this session on **ask**. Standby for the human to ask questions. Read each enabled capability's guide (`atm capability <name> guide`) to be ready to answer; do not act proactively."
- The Roles section keeps the four principles (Ownership, Dive Deep, Simplify, Earn Trust) and a short pointer: "For each enabled capability, run `atm capability <name> guide` and follow its Brief / Autopilot section."
- No hardcoded "Curate/Recall/Mapping" role bullets.

**`internal/manager/context.go`**:
- `CapabilityAction` struct **removed** (no more composed role list).
- `ContextData` drops `CapabilityActions` and `ActionConsult`; keeps `Code`, `Name`, `ATMBin`, `Actor`, `RunID`, `Timestamp`, `Persona`, `PersonaPrompt`, `PersonaDescription`, `Action`. It gains `Capability string` (the `--capability` scope, empty = all enabled).
- `RenderContext` drops the `<CAPABILITY_ROLES>` replacer; `<ACTION_BLOCK>` is composed from `Action` + `Capability`.

**Launcher / env** (`internal/manager/launcher.go`, `internal/cli/manager.go`):
- `BuildArgvOnboard` removed from the `Launcher` interface and all implementations (opencode, codex, claude, ollama). One argv flavor (`BuildArgvManage`) for all three actions.
- `ATM_ONBOARD` env var removed. `ATM_MANAGER_ACTION` carries the action; `ATM_MANAGER_CAPABILITY` carries the scope (empty = all).
- `tmuxLabelOnboarding` removed; the onboarding tmux label is gone.
- `managerEnvValues` drops the `onboard` parameter.

### 6. Default-board policy

`DefaultBoard` is removed from the interface. The TUI/CLI picks the default by UI/CLI policy:

- **TUI `selectDefault`** (`internal/tui/labels.go`): `<CODE>:all-tasks` if present in the ring, else the first row. If the project disabled `workflow`, `all-tasks` doesn't exist, so it falls back to the first row of whatever boards the enabled capabilities seeded.
- **CLI `atm task list`** (no `--label`): same policy — `<CODE>:all-tasks` if it exists, else no default filter (list all tasks).

The capability declares its boards via `EnsureVocabulary`'s return; the store remains the source of truth for what boards exist at render time. The TUI reads the ring from the store as it does today.

### 7. Capability guides

Each capability guide gains `## Brief` and `## Autopilot` sections; the existing "Manager duty" section renames to `## Autopilot`. The full shape: `## What it means` / `## How to use it` (or `## Vocabulary`) / `## Brief` / `## Autopilot`. The `ask` action reads the whole guide.

#### `internal/capability/workflow/guide.md`

```
# Workflow capability — agent guide

Status transitions for tasks: the paved road for the `status:*` namespace.

## What it means

Four mutating verbs — `atm capability workflow start` (in-progress), `open`, `block` (blocked), `complete` (done) — plus a read-only `atm capability workflow status` reporter and `atm capability workflow seed` to ensure the boards. Each mutating verb swaps the task's `status:*` label (adds the target, removes any other), so exactly-one-status is an invariant the capability maintains. The store enforces nothing: raw `atm task label add/remove --label <CODE>:status:<value>` works; a human may hand-assign, rename, or delete any status label. This is a paved road, not a fence.

## Vocabulary

- `status:open` — not done.
- `status:in-progress` — someone is on it.
- `status:blocked` — stuck.
- `status:done` — stop.

Boards (declared by this capability, seeded by `atm capability workflow seed` / project create):
- `<CODE>:backlog` (`NOT status:*`) — untriaged jottings.
- `<CODE>:open-tasks` (`status:open`).
- `<CODE>:in-progress-tasks` (`status:in-progress`).
- `<CODE>:all-tasks` (`*`) — every task; the TUI's default-selected board.

## Brief

Interview the human to set up this project's status model. Ask:
- "Do you use these four status values, or do you want different ones (e.g. `status:review`, `status:wip`)?" — record the answer by creating any extra `status:<value>` labels with descriptions via `atm label add`.
- "What does 'done' mean for this project — merged, shipped, closed?" — write the answer into the `status:done` label's description.
- "Is there a board you want beyond backlog/open-tasks/in-progress-tasks/all-tasks?" — create it with `atm label add --expr`.

Leave the human's answers in the label descriptions; the boards read them.

## Autopilot

Keep status hygiene:
1. Run `atm task list --project <CODE> --label <CODE>:backlog` — triage untriaged tasks: assign a status with `atm capability workflow start|open|block` (or hand-assign the label).
2. Run `atm task list --project <CODE> --label <CODE>:in-progress-tasks` — confirm each is still in progress; `complete` what's done, `block` what's stuck.
3. Ensure boards exist: `atm capability workflow seed --project <CODE>` (idempotent).

Do not invent new status values on the human's behalf; ask first (Brief) or leave untriaged.
```

#### `internal/capability/contextmap/guide.md`

```
# Context map capability — agent guide

Context pointers with provenance: record what knowledge derives from, so drift can be detected.

## What it means

Context pointers record what they were derived from, so drift can be detected. `atm capability context check --project <CODE>` reports which pointers have gone stale against the repo (DRIFT), which name external systems nobody has re-read lately (AGE), and which were never verified (UNVERIFIED). It is read-only — it tells you where to look, never what it means.

## How to use it

- `atm capability context add --task <ID> --kind <kind> --source <kinded-locator>` — make a task a context pointer, stamp provenance.
- `atm capability context stamp --task <ID>` — re-verify: the subject is unchanged in meaning.
- `atm capability context retarget --task <ID> --source <kinded-locator>` — the subject survived but moved.
- `atm capability context supersede --task <ID> --by <NEW-ID> --reason "..."` — the subject died; history kept.
- `atm capability context check --project <CODE>` — report drift (read-only).

Read the project's current knowledge from `<CODE>:context-current` (`atm task list --project <CODE> --label <CODE>:context-current`): pointers not superseded. Narrow by kind with `--label <CODE>:context:agent`.

## Vocabulary

- `context:agent` / `context:repository` / `context:documentation` / `context:question` — pointer kinds.
- `knowledge:superseded` — lifecycle: this pointer is obsolete; its successor is named in the description.
- `comment:provenance` — machine-written provenance stamp on a pointer's task; do not hand-edit.
- `<CODE>:context-current` board (`context:* AND NOT knowledge:superseded`) — current knowledge.

## Brief

Interview the human to map this project's knowledge. Ask:
- "What repos does this project depend on? Where are they?" → create `context:repository` tasks, `add` with `--source git:<path>` / `--source url:<url>`.
- "What docs should a new agent read first?" → create `context:documentation` tasks, `add` with the doc locator.
- "What are the agent-direction notes for this project (build/test/lint commands, gotchas)?" → create one `context:agent` task, write the notes in its description, `add` with the source.
- "Are there open questions about the project a human should clarify?" → create `context:question` tasks.

For each, the description is the payload; the provenance stamp records where it came from.

## Autopilot

Reconcile the context map against reality. Repeatable; meant to be run often.
1. **Verify.** Run `atm capability context check --project <CODE>`. Work the report:
   - `DRIFT` — read the pointer's description against the actual change. If still true, `stamp`. If the subject moved, `retarget`. If it died, create the successor and `supersede`.
   - `AGE` — an external source (Jira, Notion) nothing can witness locally. Re-read it with your own tools, then `stamp`.
   - `UNVERIFIED` — a hand-written pointer. Read it, confirm, then `add` with a `--source`.
2. **Discover.** Work the `NEW` list: territory changed in git that no pointer claims. For each worth knowing, create a task and `add` it. Ignore what's not worth a pointer — that judgement is yours.
3. **Close.** Everything reported is now stamped, retargeted, superseded, or deliberately ignored.

`check` never marks anything stale: a changed file is not a wrong pointer. It tells you where to look; you decide what it means.
```

### 8. Substrate command help (implementation requirement)

Because conventions now defers to `-h`, the cobra `Short`/`Long` on each substrate namespace must be genuinely informative. The plan audits and fattens:

- `atm task` — purpose; the ID formats (`<CODE>-<hex>` for born-v2, `<CODE>-<NNNN>` for v1 imports); key verbs (list, show, create, comment, label).
- `atm task comment` — purpose; the kind-label classification; `--reply-to` threading.
- `atm label` — purpose; the three kinds (stored / namespace / board); the description-is-intention-record rule; `--expr` for boards.
- `atm project` — purpose; the minimal create (`--code`, `--name`); capability enablement.
- `atm persona` — purpose; the `persona@agent:model` format; built-ins.
- `atm activity` — purpose; `--group-by`.
- `atm store` — purpose; `log`, `upgrade`, `prune-v1`, `set-format`.
- `atm search` — purpose; semantic search over tasks + comments; text fallback.

## Ripple list (implementation plan must cover)

1. `internal/seed/seed.go` — deleted. All callers (`cli/label.go`, `cli/conventions.go` JSON `seeded_labels`, `store`'s `SeedLabels`) updated.
2. `internal/capability/capability.go` — interface reshape (drop `DefaultBoard`, `ManagerActions`; `EnsureVocabulary` returns `[]core.Label`); registry methods updated; `ManagerAction`/`ActionSpec` types removed.
3. `internal/capability/workflow/` — `EnsureVocabulary` returns its 4 boards; `DefaultBoard` removed; `ManagerActions` removed; guide gains `## Brief` + `## Autopilot`.
4. `internal/capability/contextmap/` — `EnsureVocabulary` returns `[context-current]`; `DefaultBoard`/`ManagerActions` removed; guide gains `## Brief` + `## Autopilot`.
5. `internal/cli/conventions.go` — rewrite `conventionsCoreText` to the minimal primer; drop `capabilitiesSection`; drop `seeded_labels` + advisory fields from JSON envelope.
6. `internal/cli/capability.go` (new) — `atm capability list` + the mount point that mounts each enabled capability's tree under `atm capability <name>`.
7. `internal/cli/root.go` — mount `atm capability`; per-capability commands no longer mount at root (they mount under `atm capability <name>`). `mountRegistry` unchanged in shape.
8. `internal/cli/manager.go` — drop `--curate`/`--recall`/`--mapping`/`--onboarding`; add `--action brief|autopilot|ask` (default `autopilot`) + `--capability <name>`; `validateManagerAction` simplifies; drop `BuildArgvOnboard`/`ATM_ONBOARD`/`tmuxLabelOnboarding`; `managerEnvValues` drops `onboard`, gains `capability`.
9. `internal/manager/context.go` + `context_v1.md` — drop `<CAPABILITY_ROLES>`; restate `<ACTION_BLOCK>` for the three actions; `ContextData` drops `CapabilityActions`/`ActionConsult`, gains `Capability`; `CapabilityAction` removed.
10. `internal/manager/launcher.go` — drop `BuildArgvOnboard` from interface + all implementations.
11. `internal/cli/project.go` — `atm project create` / `capability add` updated for the new `EnsureVocabulary` signature (consume returned boards).
12. `internal/tui/labels.go` — `selectDefault` picks `<CODE>:all-tasks` if present else first row (no `DefaultBoard`).
13. `internal/tui/app.go` / `projects.go` — `regFor(code).EnsureVocabulary` call sites updated for new signature.
14. Substrate command help — fatten `Short`/`Long` on `atm task`, `atm task comment`, `atm label`, `atm project`, `atm persona`, `atm activity`, `atm store`, `atm search`.
15. Tests + goldens — conventions goldens, capability tests, manager tests, launcher tests, TUI default-board tests all updated.
16. `atm init` — the "Next: atm manage --project <CODE> --onboarding" pointer updates to `--action brief`.

## Open questions (for the plan to resolve or flag)

- **CLI flag day**: `atm workflow start` → `atm capability workflow start` is a flag day. Acceptable pre-1.0; the plan notes it loudly. No compat shim.
- **`--capability` single vs multi**: design specifies single (`--capability <name>`); to target multiple, omit the flag (all enabled) or run the action multiple times.
- **`atm capability list` shape**: `NAME, SUMMARY, ENABLED` columns; JSON envelope `{"capabilities":[{"name","summary","enabled"}]}`.
- **Default-board policy**: `<CODE>:all-tasks` if present, else first row (TUI and CLI).

## Clarifications (2026-07-18 follow-up review, user-approved)

Resolved during plan-writing review; these amend the sections above where they conflict.

1. **Mount by `Name()`** — the spec wrote both `atm capability context add` (command name) and `atm capability <name> guide` / `--capability contextmap` (capability name). Resolution: one identifier everywhere — each capability's tree mounts under its `Name()`, so contextmap's verbs read `atm capability contextmap add/stamp/…`. The registry enforces this structurally (`Commands` sets the mounted command's `Use` to `Name()`), and contextmap's own `Use` string changes from `context` to `contextmap`. Every `atm capability context …` occurrence in §2/§7 reads as `atm capability contextmap …`. `Description` drops its `Command` field (name IS the command) and `Describe` no longer needs `Env`.
2. **`atm label seed` is REMOVED entirely** (supersedes §4's "reshape") — per-capability `EnsureVocabulary` is the only seeding path, running at: project create, `atm project capability add`, TUI project select, the TUI Boards [S] key (now a direct `EnsureVocabulary` re-run), and `atm capability workflow seed`. `EnsureVocabulary` returns **boards only** (`[]core.Label` with `Expr` set), per §3; §4's "emits the seeded label names" is void — callers emit the returned boards.
3. **Capabilities absorb the substrate labels they own** — workflow's `EnsureVocabulary` seeds the `status:*` namespace descriptor + the four status values (with `seed.go`'s descriptions) alongside its 4 boards; contextmap seeds the `context:*` descriptor and adopts `seed.go`'s richer per-kind descriptions. `comment:*` and `priority:*` are not seeded anywhere (invented on demand).
4. **Extra ripple found in review** (missing from the list below): `internal/store/project.go` `createProjectV2` writes `seed.Labels` as `label.upserted` events inside the project-birth changeset, and `core.Service`'s `LabelService` interface carries `SeedLabels` — both go away with `seed.go`. The CLI half of the §6 default-board policy is already today's behavior (`atm task list` with no `--label` lists everything and `all-tasks` is the `*` expression), so §6 is a TUI-only change. `conventions --project` becomes vestigial once the enumeration section dies and is removed. The TUI help's embedded conventions copy (`internal/tui/help.go` `conventionsTextTUI`) and the developing-session template (`internal/developing/context_v1.md`) restate removed prose and are updated. Label descriptions seeded by contextmap that say ``applied by `atm context supersede` `` update to the new command path.

## Doctrine notes

This initiative amends the capability doctrine established in `2026-07-18-capability-semantics-initiative-design.md`:

- A capability's `EnsureVocabulary` is its single self-setup seam; it returns the boards it owns. No parallel `seed.go`.
- A capability declares its boards; the UI picks the default. `DefaultBoard` is not a capability concern.
- The manager action model is semantic-agnostic (`brief`/`autopilot`/`ask`), scoped per-capability. Capability guides carry the procedure (`## Brief`, `## Autopilot`); the manager walks the guides. `ManagerActions()` is not a capability concern.
- Conventions is a minimal substrate primer; capability discovery is `atm capability list` + `atm capability <name> guide`. Conventions does not enumerate capabilities.