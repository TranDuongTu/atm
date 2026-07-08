# Manager as Knowledge-Base Owner: Onboarding Unification + Ubiquitous Language — Design Spec

**Status:** Draft (awaiting user review)
**Date:** 2026-07-08
**Depends on:** `2026-07-06-atm-manager-subagent-design.md` (manager subagent),
`2026-07-04-onboarding-v1-design.md` (onboarding, being superseded),
`2026-07-04-tui-project-summary-charts-design.md` (Charts, Chart 3 integration
point), `2026-07-07-personas-and-actor-activity-design.md` (personas, read by
the manager for actor attribution).
**Follow-up to:** ATM-0028.

## Driver

ATM currently has three subsystems that each own a slice of project knowledge,
and they have drifted apart:

1. **Onboarding** (`atm onboarding`, `internal/onboard/`) is a non-interactive
   agent run that reads a repo and writes context tasks. It has its own prompt,
   its own launcher package, and its own command tree — parallel to the manager,
   not part of it.
2. **The manager** (`atm manager`, `internal/manager/`) owns the ledger shape
   (tasks, labels, status) and formalizes developing-agent track calls. It has
   no relationship to onboarding and no knowledge of a project's vocabulary.
3. **The TUI "bubbles" chart** is a placeholder (`renderSampleBubbleCanvas`)
   that renders hardcoded sample labels (`events`/`agents`/`tasks`/`labels`).
   The charts spec explicitly reserved this slot for a future agent integration
   that computes the project's "ubiquitous language" (DDD's shared vocabulary),
   but the integration was never built.

This spec unifies all three under a single principle: **the manager is the
knowledge-base owner for every project.** "Knowledge base" means the ledger
shape, the ubiquitous language (domain terms mined from the project's free
text), and the context map (repo pointers from onboarding). Onboarding and
vocabulary computation stop being separate subsystems; they become manager
actions under one prompt. The TUI chart stops being a placeholder; it reads the
vocabulary the manager computes.

### What changes

- The manager prompt is reframed around the knowledge-base-owner principle and
  absorbs the onboarding responsibility + the vocabulary responsibility.
- `atm onboarding` is deleted; onboarding becomes a `--onboard` flag on the
  existing `atm manager <host>` commands.
- The TUI's third chart is renamed `bubbles` -> `Ubiquitous Language` and reads
  a new per-project `vocabulary.json` written by the manager.
- The manager's self-improvement gene is reframed to capture cross-project,
  reusable practices (especially how logic is reused through the label
  substrate), so the manager suggests best management practices, not just
  per-project notes.

## Goals

- One manager prompt that owns ledger, onboarding, vocabulary, and
  self-learning, with mode-driven pacing.
- One command tree (`atm manager`) for interactive sessions and non-interactive
  onboarding runs.
- One per-project vocabulary artifact (`vocabulary.json`) computed by the
  manager, read by the TUI.
- The TUI is a pure reader of the vocabulary; it never dispatches the manager.

## Non-Goals

- A TUI key to trigger vocabulary recompute (explicit-only via manager session
  or developing-agent track call with a `vocabulary` hint).
- A new `atm vocabulary` CLI command (vocabulary is written by the manager
  through the store helper, not by a dedicated CLI subcommand).
- Local Go-side frequency mining (extraction is pure LLM judgment in the
  manager; no deterministic tokenizer/stemmer is added to the store or CLI).
- An inquiry/search capability against the knowledge base (declared as the
  manager's remit in the prompt for forward compatibility, but not built in
  this spec).
- A persistent background manager process (the manager stays
  host-native-subagent or interactive-human-launched, as in v1).
- Rewriting raw actor strings in HISTORY/FACTS (audit provenance stays literal,
  unchanged from the personas spec).

## Decisions (locked during brainstorming)

| # | Decision |
|---|----------|
| D1 | **Vocabulary scope = domain terms from free text.** Bubbles are the recurring domain nouns/proper terms mined from task titles + descriptions + comments. Pure linguistic extraction by the manager (LLM), not structural substrate terms (labels/personas/task-types). |
| D2 | **Manager owns the computation.** The atm-manager subagent / interactive manager session pre-computes the vocabulary and persists it; the TUI only displays. |
| D3 | **Persistence = per-project `vocabulary.json` in the store.** At `$ATM_HOME/projects/<CODE>/vocabulary.json`. Read via a new store getter; written via a new store setter. No audit-log pollution, no Project-entity schema change. |
| D4 | **Refresh cadence = explicit recompute only.** The TUI reads last-known on every render. Recompute happens during onboarding, in an interactive manager session when the human asks, or on a developing-agent track call with a `vocabulary` hint. No TUI key, no auto-refresh, no background sweep. |
| D5 | **Missing/stale fallback = quiet empty state, no bubbles.** When `vocabulary.json` is absent or unreadable, the chart shows a one-line empty state (`no vocabulary yet — manager has not computed it`) and renders no bubbles. The old hardcoded sample bubbles are deleted. |
| D6 | **Recompute trigger from the TUI = none.** The TUI is a pure reader. Recompute is triggered from an interactive `atm manager` session or a developing-agent track call; the TUI re-reads on project selection and on generic refresh. |
| D7 | **Extraction method = pure LLM extraction in the manager.** No local Go-side frequency/tokenizer code. The manager reads tasks/comments via the `atm` CLI and uses its own language understanding to identify, dedupe, rank, and write the terms. |
| D8 | **Output shape = weighted term list.** `{ updated_at, actor, terms: [{term, weight}] }`, weight = normalized frequency (integer 1-10), top-N cap of 12 terms. |
| D9 | **Onboarding execution = `--onboard` flag on existing manager commands.** `atm manager <host> --project <CODE> --onboard` runs non-interactively (`--auto`) against cwd and activates the manager's onboarding responsibility via an `ATM_ONBOARD=1` env signal. Without the flag, the command is the unchanged interactive human session. |
| D10 | **Prompt shape = one manager prompt, env-conditional onboarding section.** The base manager prompt carries the onboarding+vocab responsibilities; the `ATM_ONBOARD` env var activates the onboarding section at runtime. One embedded prompt file, not two. |
| D11 | **Manager self-learning is cross-project.** The self-improvement gene captures reusable practices across projects (especially how logic is reused through the label substrate) and suggests best management practices. It is not tied to any single project's specifics; it stamps the observation's origin but frames the improvement as reusable. |

## Section 1: Manager prompt reframe

`internal/manager/context_v1.md` is rewritten around the knowledge-base-owner
principle. Structure:

### Role

The manager is the knowledge-base owner for project `<CODE>`. "Knowledge base"
means three things the manager owns and keeps coherent:

- **The ledger** — tasks, labels, status, titles, comments (existing
  responsibility).
- **The ubiquitous language** — the project's recurring domain terms, computed
  from task titles/descriptions/comments and persisted to `vocabulary.json`
  (new).
- **The context map** — repo pointers captured during onboarding (absorbed from
  the onboarding prompt).

The manager maintains all three and can answer inquiries against them. A
search/query capability lands in a later spec; this prompt declares it as the
manager's remit so the prompt is forward-compatible.

### Mode-driven pacing

- **Subagent mode (fast):** a developing agent dispatched the manager mid-work
  with a track request. Optimize for a fast, useful ledger write and a short
  confirmation. Do not over-deliberate; make a reasonable call, write it,
  return.
- **Interactive mode (thorough):** a human launched the manager via
  `atm manager <host> --project <CODE>` to consult or steer. Optimize for a
  thorough review — dig into the ledger, propose splits/merges, rewrite titles,
  surface staleness, sum up discussions, ask the human to clarify.
- **Onboarding mode (non-interactive):** the manager was launched with
  `--onboard` against a target repo in cwd. Read the repo, build the context
  map, compute the vocabulary, and return. Do not ask the human questions; make
  reasonable judgment calls and proceed.

In all modes the manager does not ask the developing agent back. In subagent
mode, ambiguous track requests get a best-guess write plus an optional
`needs clarification` note for the human. In interactive mode, the manager asks
the human directly. In onboarding mode, the manager proceeds non-interactively.

### Onboarding responsibility

When `ATM_ONBOARD=1` is set in the environment, the manager performs onboarding
for the project against the repo in its current working directory. The
responsibility (absorbed from `prompt_opencode_v1.md`):

1. **Orient as an ATM agent first.** Run `<ATM_BIN> conventions` and read it.
   It tells the manager how ATM projects are organized (the label substrate,
   the first-contact sequence a later agent will follow, the advisory seed
   namespaces). The manager is building exactly the context that `conventions`
   describes a fresh agent consuming.
2. **Research already-captured knowledge.** Run
   `<ATM_BIN> task list --project <CODE> --output json` and read existing tasks'
   titles, labels, and descriptions. Also run `<ATM_BIN> store log <CODE>` to
   read the project's audit log and observe recent activity before reconciling.
   This project may already have context from other repositories; the manager
   reconciles rather than duplicates.
3. **Explore the repo** breadth-first and budget-bounded. List top-level
   files/dirs; read README; read docs/ if present; sample representative source
   files. Do not read every file. Stop when the obvious surface is covered.
4. **Capture findings as ATM tasks**, label-agnostically (match findings to
   label descriptions from `<ATM_BIN> label list`):
   - Agent harness setup, structure, document/code pointers, findings, open
     questions (each as its own task), no duplication.
   - Cap work-task creation at ~20 per run; further findings go into a single
     aggregate task whose description lists them.
5. **Idempotency.** Before each `atm task create`, match against existing tasks
   AND what has been created this run, by title and topical overlap. Update
   rather than duplicate. If repos disagree about the same thing, prefer a task
   whose description names the disagreement (or a `context:question` task) over
   silently picking a side.
6. **Compute the vocabulary in the same pass.** From the task
   titles/descriptions/comments the manager just created (plus any pre-existing
   ones), extract the recurring domain nouns/proper terms using the manager's
   own language understanding. Dedupe, rank by frequency, normalize weights to
   1-10, cap at 12 terms, and write `vocabulary.json` via
   `<ATM_BIN>` (the store `WriteVocabulary` helper). See Section 3.
7. **Summary.** Print a one-paragraph natural-language summary of what was
   created/updated, reconciliations made, and the vocabulary written. This is
   the human's onboarding receipt.

No `<EXISTING_TASKS>` snapshot is embedded in the rendered prompt. The manager
reads the live `task list --output json` as its reconciliation baseline,
matching the existing track-pipeline pattern. (The old onboarding prompt's
embedded t0 snapshot is dropped; the live read is the baseline.)

### Vocabulary responsibility

The manager extracts recurring domain nouns/proper terms from task titles +
descriptions + comments using its own language understanding (pure LLM
extraction; no local Go-side frequency/tokenizer code is added).

- **Inputs:** task titles + descriptions + comments for the project (read via
  `<ATM_BIN> task list --project <CODE> --output json` and
  `<ATM_BIN> task comment list --task <ID> --output json`).
- **Output:** a weighted term list, written to `vocabulary.json` via the store
  `WriteVocabulary` helper. See Section 3 for the file shape.
- **Recompute is explicit:** during an onboarding pass, in an interactive
  session when the human asks ("recompute the vocabulary"), or on a
  developing-agent track call with a `vocabulary` hint. The manager never
  recomputes implicitly on every touch.

### Self-learning & cross-project practices (reframed gene)

After every manager session — regardless of mode — log one self-improvement
task before returning. The lens is now cross-project: capture common practices
that are reusable across projects, especially how logic is reused through the
label substrate (label conventions that worked, vocabulary-extraction patterns
that generalized, onboarding heuristics that applied across repos). The manager
suggests best management practices, not just per-project notes.

- The manager stamps the observation's origin (which project/session surfaced
  it) but frames the improvement as reusable.
- If a task already covers that improvement, add a comment noting the new
  evidence and skip creating a duplicate.
- Otherwise create a new task titled to name the improvement ("Manager:
  <change>"), with `type:chore` and the project's default open status, whose
  description captures: (a) the dynamic observed, (b) the proposed change to
  the manager prompt, a label convention, or a workflow, and (c) why it would
  make this or a future session smoother across projects.
- Keep it cheap — one task per session, distilled to a few lines.
- This gene is non-optional. A manager session that does not end with a
  self-improvement task logged (or a comment added to an existing one) is
  incomplete.

### Kept from the current prompt

Ledger hygiene, Interactive mode, Commands, and Code of conduct are kept from
the current `context_v1.md`, with the vocabulary write command added to the
command cheat sheet:

- `<ATM_BIN> vocabulary show --project <CODE>` (read; the TUI uses the store
  getter directly, but the manager may read it to inspect prior vocabulary).
- `<ATM_BIN> vocabulary write --project <CODE> --actor <ACTOR> --terms <json>`
  (write; the manager shells out to this so it stays a pure CLI actor). The
  full CLI surface is specified in Section 3.

## Section 2: Unify onboarding into the manager command tree

### Deleted

- `atm onboarding` command tree: the `onboarding` group command and its
  `opencode`/`ollama` subcommands in `internal/cli/onboarding.go`.
- `internal/onboard/` package: `embed.go`, `embed_test.go`, `launcher.go`,
  `launcher_test.go`, `prompt_opencode_v1.md`. The onboarding prompt content is
  absorbed into the manager prompt (Section 1); the non-interactive launcher
  argv shapes move into `internal/manager/launcher.go`.
- The `onboarding` directory under `$ATM_HOME` (rendered prompt files). Manager
  run-context files already live under `$ATM_HOME/manager/` and onboarding runs
  use the same path.

### Added: `--onboard` flag on existing manager agent commands

```
atm manager <host> --project <CODE> [--onboard] [--actor <id>] [--dry-run]
atm manager ollama --integration <name> --project <CODE> [--onboard] [--actor <id>] [--dry-run]
```

- Without `--onboard` (default): unchanged interactive human session. Launcher
  argv is the host's interactive entrypoint (`opencode`, `codex`, `claude`,
  `ollama launch <int> --`). Context rendered from
  `internal/manager/context_v1.md`.
- With `--onboard`: non-interactive run against cwd. The launcher argv switches
  to the non-interactive shape (absorbed from the old onboarding launcher:
  `opencode --auto --prompt <msg>` pointing at the rendered context file;
  ollama equivalent). The rendered manager context is the same prompt file,
  but the run sets `ATM_ONBOARD=1` in the child env so the prompt's onboarding
  responsibility is active.

### Launcher consolidation (`internal/manager/launcher.go`)

The `Launcher` interface gains a non-interactive argv builder. Each host
launcher carries both shapes:

```go
type Launcher interface {
    Name() string
    NotFoundHint() string
    BuildArgv() []string                       // interactive
    BuildArgvOnboard(contextPath string) []string // non-interactive --auto
}
```

- `staticLauncher` (opencode/codex/claude) gains a `BuildArgvOnboard` that
  produces the `--auto --prompt <msg>` shape pointing at the context file. The
  `onboardingMessagePrefix/Suffix` wrapper text ("Read the onboarding
  instructions in the file at ... and follow them exactly.") moves here and is
  generalized ("Read the manager instructions in the file at ... and follow
  them exactly.").
- `OllamaLauncher.BuildArgvOnboard` produces
  `ollama launch <int> -- --auto --prompt <msg>`.
- Codex/claude non-interactive argv: if the host has a clean non-interactive
  `--auto`-equivalent, use it; otherwise fall back to the interactive argv
  with a warning (matching the manager-subagent spec's Codex parity stance).
  This is a known open item resolved by probing the local host at
  implementation time, not a placeholder in the contract; opencode is the
  primary supported `--onboard` host and ollama passes through to it.

### CLI flow (`internal/cli/manager.go` `runManager`)

When `--onboard` is set:

1. Render the manager context file as today (one prompt, env-conditional
   onboarding section).
2. Select `BuildArgvOnboard(contextPath)` instead of `BuildArgv()`.
3. `managerEnvValues` adds `ATM_ONBOARD=1`.
4. Header/tail emit is shared (same `emitLaunchHeader`/`emitLaunchTail`,
   labeled as onboarding in the header line: `manager <host> --onboard
   <CODE>  run=...`).
5. `--dry-run` prints argv+env (including `ATM_ONBOARD=1`) and exits without
   launching, identical to the non-onboard dry-run path.

### `render-context` unchanged

`atm manager render-context` still prints the manager prompt; the onboarding
section is env-conditional (`ATM_ONBOARD`), so one render serves both modes. No
`--onboard` flag is needed on `render-context` (the section is inert in the
printed prompt unless `ATM_ONBOARD` is set at runtime).

### Removed from old onboarding

- `--prompt-version` selector (the manager prompt is one embedded file; no
  version selector in v1).
- The t0 `<EXISTING_TASKS>` snapshot table embedded in the prompt (the
  onboarding section tells the manager to run `task list --output json` live
  instead).
- The `--integration` pass-through on a separate command (it stays on
  `atm manager ollama`).

## Section 3: vocabulary.json

### File shape

```json
{
  "updated_at": "2026-07-08T12:00:00Z",
  "actor": "opencode-manager",
  "terms": [
    {"term": "labels", "weight": 9},
    {"term": "audit log", "weight": 7},
    {"term": "persona", "weight": 5}
  ]
}
```

- `weight` is a normalized frequency (integer 1-10, manager-chosen scale).
- Top-N cap: 12 terms (the manager drops the rest).
- File location: `$ATM_HOME/projects/<CODE>/vocabulary.json`.

### Store helpers (`internal/store/vocabulary.go`, new)

```go
type VocabularyTerm struct {
    Term   string `json:"term"`
    Weight int    `json:"weight"`
}

type Vocabulary struct {
    UpdatedAt time.Time        `json:"updated_at"`
    Actor     string           `json:"actor"`
    Terms     []VocabularyTerm `json:"terms"`
}

// GetVocabulary reads <store>/projects/<CODE>/vocabulary.json. Missing file ->
// (nil, nil) so the TUI treats it as the empty-state case. Malformed JSON ->
// error (tolerated by the TUI as empty-state).
func (s *Store) GetVocabulary(code string) (*Vocabulary, error)

// WriteVocabulary writes <store>/projects/<CODE>/vocabulary.json under the
// project's per-project lock, fsynced like other store writes. The manager
// goes through this helper, not raw file writes, so vocabulary.json stays under
// the store's locking/concurrency conventions.
func (s *Store) WriteVocabulary(code string, v *Vocabulary) error
```

- `GetVocabulary` is a pure read; no audit-log entry is created.
- `WriteVocabulary` is lock-guarded and fsynced; it overwrites any prior
  vocabulary.json. It does not append to the audit log (vocabulary is a
  derived artifact, not a ledger mutation; this mirrors how the cache.db is a
  derived artifact).

### CLI surface for vocabulary writes

The manager writes vocabulary.json through a thin CLI subcommand, staying a
pure CLI actor (mirroring how the manager uses `atm task create` etc.):

```
atm vocabulary show --project <CODE> [--output json]   # read
atm vocabulary write --project <CODE> --actor <ACTOR> --terms <json>   # write
```

- `--terms <json>` is a JSON array of `{"term": "<string>", "weight": <int>}`
  objects. The CLI parses it, builds the `Vocabulary` (stamping `updated_at`
  server-side so the manager cannot forge it), and calls `store.WriteVocabulary`.
- Output envelope (text + JSON) matches the existing CLI convention. `show`
  prints the current vocabulary or an empty-state message when the file is
  absent; `write` prints the path written and the term count.
- These subcommands are the only vocabulary CLI surface. No `mine`/`compute`
  subcommand exists (extraction is pure LLM judgment in the manager, D7).

## Section 4: Ubiquitous Language chart (TUI)

### Rename

The third Projects-pane chart box `bubbles` -> `Ubiquitous Language` across
`internal/tui/projects.go` (`renderBubbleChart` ->
`renderUbiquitousLanguageChart`, the degenerate-case
`dashboardLine(p.width, "bubbles")` -> `dashboardLine(p.width, "Ubiquitous
Language")`, `renderChartBox("bubbles", ...)` title). The charts spec
(`2026-07-04-tui-project-summary-charts-design.md`) is updated: Chart 3 is no
longer a placeholder.

### Render path

`projectSummaryData` already loads project, tasks, entries. Add a best-effort
`vocab, _ := p.m.store.GetVocabulary(project.Code)` to that helper (nil
tolerated).

`renderBubbleChart(maxLines int)` becomes
`renderUbiquitousLanguageChart(vocab *store.Vocabulary, maxLines int)`:

- `vocab == nil` or empty `terms` -> render the empty-state line inside the
  chart box: `no vocabulary yet — manager has not computed it`. No bubbles.
- Present -> render bubbles sized by weight (larger weight = bigger bubble)
  into the existing `canvas.New(width, height)` via `SetStringWithStyle`,
  placed deterministically (by weight desc, then term alpha). Bubble styling
  uses distinct Lip Gloss foreground colors cycled by index. Top-N limited by
  what fits the canvas (drop terms that overflow rather than crowding).
- Degenerate cases (`maxLines < 3`) render
  `dashboardLine(p.width, "Ubiquitous Language")`, same as today.

`renderSampleBubbleCanvas` and its hardcoded labels
(`events`/`agents`/`tasks`/`labels`) are deleted. The canvas container logic
(`canvas.New`, `SetStringWithStyle`) is reused for real bubbles.

### Empty state when no project selected

Unchanged: the whole summary region shows `select a project to see summaries`.

### Refresh

The TUI re-reads `vocabulary.json` on every `projectSummaryData` call (already
happens on project selection and `s` select). No new TUI key, no background
refresh, no manager dispatch from the TUI.

## Data flow

```
Onboarding + vocabulary compute (manager, non-interactive):
  atm manager <host> --project <CODE> --onboard
    -> runManager renders manager context (context_v1.md, ATM_ONBOARD=1 active)
    -> BuildArgvOnboard(contextPath) -> opencode --auto --prompt <msg>
    -> env: ATM_PROJECT, ATM_BIN, ATM_ACTOR, ATM_ONBOARD=1, ...
    -> manager reads repo in cwd, builds context tasks via atm CLI,
       computes vocabulary from task titles/descriptions/comments (pure LLM),
       writes vocabulary.json via atm CLI (store.WriteVocabulary)
    -> TUI re-reads vocabulary.json on next render

Vocabulary recompute (explicit):
  interactive: atm manager <host> --project <CODE> -> human asks "recompute vocabulary"
    -> manager reads tasks/comments, extracts terms, writes vocabulary.json
  OR subagent: developing agent dispatches atm-manager with hint: vocabulary
    -> manager reads tasks/comments, extracts terms, writes vocabulary.json
  -> TUI re-reads on next render (project select / refresh)

TUI render (pure reader):
  projectSummaryData(projectScope)
    -> Store.GetProject, ListTasks, ReadLog, GetVocabulary
    -> renderPersonaActivityChart / renderActivityStripeChart / renderUbiquitousLanguageChart
    -> vocab nil -> "no vocabulary yet" empty state
    -> vocab present -> weighted bubbles in canvas
```

## Error handling

- `atm manager <host> --project MISSING` -> `ErrNotFound` + the project-create
  hint (existing).
- `atm manager <host> --project <CODE> --onboard` with host binary missing ->
  exit 1 + install hint (existing `runChild` path).
- `--onboard --dry-run` -> renders context, prints argv+env (including
  `ATM_ONBOARD=1`), exits 0 without launching (existing dry-run path).
- `GetVocabulary` missing file -> `(nil, nil)`; TUI renders the empty state,
  no error.
- `GetVocabulary` malformed JSON -> TUI tolerates as empty state (best-effort);
  chart shows the empty-state line.
- `WriteVocabulary` failure (manager side) -> the manager surfaces the error in
  its session output and leaves any prior `vocabulary.json` intact (it does not
  delete on failure).
- Narrow/short terminals -> `renderUbiquitousLanguageChart` degrades via the
  existing `chartBoxHeights`/`maxLines` guards; no panic.

## Testing

- `internal/manager`: context render with `ATM_ONBOARD` active contains the
  onboarding responsibility + vocabulary responsibility; without it, the
  onboarding section is inert. Launcher `BuildArgvOnboard` produces the
  expected `--auto --prompt` argv for opencode/ollama.
- `internal/cli/manager.go`: `--onboard` flag sets `ATM_ONBOARD=1` in env and
  selects `BuildArgvOnboard`; `--onboard --dry-run` prints the non-interactive
  argv+env; without `--onboard` the interactive argv is unchanged. Regression:
  no `atm onboarding` command registered in the cobra tree.
- `internal/store/vocabulary_test.go`: `GetVocabulary` returns nil/nil for a
  missing file; returns a parsed vocab for a fixture; `WriteVocabulary`
  round-trips; malformed JSON is tolerated by `GetVocabulary` (returns nil/nil
  or an error the TUI treats as empty-state).
- `internal/tui/projects_test.go`: chart box title is `Ubiquitous Language`;
  nil vocab renders the empty-state line and no sample bubbles; present vocab
  renders weighted bubbles with deterministic placement;
  `renderSampleBubbleCanvas` and its hardcoded labels are gone.
- Regression: `internal/onboard/` package is deleted; the `onboarding` command
  is absent from the CLI.

## Rollout

Layered commits, each green (`make verify`):

1. `internal/store/vocabulary.go` + tests (read/write helpers).
2. `internal/manager/context_v1.md` reframe (knowledge-base owner, onboarding +
   vocab responsibilities, cross-project self-learning) + `launcher.go`
   `BuildArgvOnboard` + `context.go` render changes + tests.
3. `internal/cli/manager.go` `--onboard` flag + delete
   `internal/cli/onboarding.go` + delete `internal/onboard/` + remove
   `onboarding` command registration + tests.
4. `internal/tui/projects.go` rename + `renderUbiquitousLanguageChart` + delete
   `renderSampleBubbleCanvas` + tests.
5. Spec update: this doc + a note in
   `2026-07-04-tui-project-summary-charts-design.md` that Chart 3 is no longer a
   placeholder.

## Verification

`make verify` (runs `make build && make test`) is the gate. A manual smoke
(`atm manager opencode --project ATM --onboard --dry-run`;
`atm manager opencode --project ATM`; `atm tui`) confirms the surface.