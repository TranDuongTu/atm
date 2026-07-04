# Onboarding v1 — Design Spec

**Status:** Approved (drafted via brainstorming)
**Date:** 2026-07-04
**Depends on:** `2026-07-02-tasks-management-v2-design.md` (Sections 4, 7)

## Driver

v2's Section 7 documents the *advisory* onboarding contract — seed labels, index
tasks, the first-contact sequence — but leaves the *act of onboarding* as a
manual human sequence (`atm tui` → add project → seed tasks by hand). For any
non-trivial repo, that is tedious and inconsistent: the human has to read the
repo, decide what's index-worthy, and translate it into tasks by hand. The
result is that ATM's cold-start value depends on a step most users will skip.

This spec defines the *automated onboarding path*: ATM launches an external
agent non-interactively, hands it a versioned system prompt ATM controls, and
the agent does the research + translation into ATM's task/label substrate by
calling the `atm` CLI itself. The user runs `atm onboarding ...` in the repo
they want onboarded; the agent explores that repo under its own permission
model and writes its findings into a pre-existing ATM project as tasks. The
user repeats this per repo/workspace to build a multi-repo context map for a
project. Later, any working agent landing in the project queries ATM and finds
the relevant context.

The design holds to v2's philosophy: ATM has no intrinsic workflow knowledge,
onboarding does not introduce a second API surface, and the agent is just
another CLI actor mutating the same store via the same `atm` commands.

## Scope (v1)

- Two `atm onboarding` subcommands: `opencode` and `ollama`. Each launches a
  non-interactive agent run against the user's current working directory.
- OpenCode is the supported agent surface (its `run -f <prompt> --auto` shape).
  `ollama` is the thin launcher (`ollama launch <integration> -- ...`) that
  passes the same opencode argv through unchanged with an ollama-backed model.
- Onboarding writes tasks + labels into a **pre-existing** ATM project. It does
  not create the project; the human does that first.
- The agent runs in the cwd, under the user-agent's own permission model. ATM
  does not pass a repo path or constrain the agent's file access.
- Onboarding is **idempotent**: the rendered prompt embeds the existing task
  snapshot, and the agent is instructed to match-by-title before creating.
- The system prompt is **embedded in the binary**, versioned, ATM-owned.

## Out of scope (v1)

- Direct `codex` / `claude` launcher subcommands (only `opencode` + `ollama`).
- External-system onboarding (Jira, Quip, Notion, GitHub issues).
- An MCP/tool-server API surface (the agent shells out to `atm` only).
- A `--prompt-file` user override (prompt is embedded-only; `--prompt-version`
  is the only escape hatch).
- TUI entrypoint for onboarding (CLI only in v1).
- An `atm onboarding log` audit trail (Approach B from brainstorming, dropped).
- Onboarding-run garbage collection (rendered prompts kept indefinitely).
- Shell-quoted launcher commands or env expansion in `--agent`-style flags (no
  such flag — the two subcommands are explicit and use simple argv).
- Validating `ollama`'s `--integration` value against a whitelist (passed
  through; unknown values fail at `ollama launch`'s door).

## Command surface

```
atm onboarding opencode --project <CODE> [--actor <id>] [--prompt-version <v>] [--dry-run]
atm onboarding ollama   --project <CODE> --integration <name> [--actor <id>] [--prompt-version <v>] [--dry-run]
```

Both subcommands share `--project`, `--actor`, `--prompt-version`, `--dry-run`,
and the global flags (`--store`, `--output`, `--quiet`).

| Flag | Subcommand | Required | Default | Purpose |
|------|------------|----------|---------|---------|
| `--project <CODE>` | both | yes | — | ATM project to onboard into. Must pre-exist. |
| `--actor <id>` | both | no | `<launcher>-onboard` (e.g. `opencode-onboard`, `ollama-onboard`) | Stamped into history entries the agent creates. |
| `--prompt-version <v>` | both | no | `onboard.Latest` (e.g. `v1`) | Select embedded prompt version. Unknown → exit 2. |
| `--dry-run` | both | no | false | Render + write prompt file + print the exact launcher command, then exit 0 without launching. |
| `--integration <name>` | `ollama` | yes | — | One of ollama's supported integrations (`opencode`, `codex`, `claude`, ...). Passed through to `ollama launch`. Not validated by ATM. |

### Exec argv per subcommand

- `opencode` → `opencode run -f <prompt-file> --auto --title "ATM onboarding: <CODE> (<run-id>)"`.
- `ollama --integration opencode` → `ollama launch opencode -- run -f <prompt-file> --auto --title "ATM onboarding: <CODE> (<run-id>)"`.
- The `--` separator is `ollama launch`'s documented passthrough; everything
  after it goes to the launched integration unchanged. So `ollama launch codex
  -- run -f <prompt> --auto --title ...` works against codex too, even though
  codex won't accept opencode's `run -f` shape — that failure surfaces at the
  codex door, not in ATM. This is intentional: ATM only knows the opencode
  argv shape; non-opencode integrations are the user's experiment.

### Shared launcher abstraction

A small `launcher` interface in `internal/onboard` keeps the two subcommands
clean and is the seam for future direct launcher subcommands:

```go
type launcher interface {
    name() string                                    // "opencode" | "ollama"
    notFoundHint() string                            // install hint for the error
    buildArgv(promptPath, title string) []string     // the exec argv
}
```

Two implementations: `opencodeLauncher{}`, `ollamaLauncher{integration string}`.
The CLI subcommands construct the right one and pass it to a shared
`runOnboarding(l launcher, opts Options) error`.

## Execution flow

`atm onboarding <subcommand> --project FOO`:

1. **Validate.** Resolve store, load project `FOO`. If missing → stderr
   `project FOO not found; create it first:` + the exact `atm project create`
   command, exit 3.
2. **Snapshot.** Read the project's current tasks (`ListTasks(QueryFilters{
   Project: FOO})`) and current labels (`LabelList("FOO", "")`). The task list
   is the dedupe baseline.
3. **Resolve ATM binary.** `os.Executable()` — absolute path to the running
   `atm`, substituted into the prompt so the agent calls the same binary.
4. **Render prompt.** `onboard.Render(version, data)` where `data` is an
   `onboard.Data` struct carrying `Code`, `Name`, `ATMBin`, `Actor`, `RunID`,
   `Timestamp`, `ExistingTasks` (rendered as a markdown table: ID, title,
   labels, description-snippet). Unknown `--prompt-version` → exit 2.
5. **Write prompt file.** `$ATM_HOME/onboarding/<run-id>.md`. Create
   `$ATM_HOME/onboarding/` if missing. `<run-id>` =
   `<CODE>-<YYYYMMDDHHMMSS>-<short-uuid>` (sortable, collision-safe).
6. **Print header** (text or JSON per `--output`): project, run-id, prompt
   path, prompt version, agent launcher name.
7. **Dry-run stop.** If `--dry-run`, print the exact launcher argv and exit 0.
8. **Launch as child.** ATM stays the parent (`cmd.Run()`); it does **not**
   `exec`-replace. The child inherits stdin/stdout/stderr so the user watches
   the agent's TUI/output live. ATM blocks until the child exits.
9. **Tail summary.** After exit, ATM re-reads `ListTasks(QueryFilters{Project:
   FOO})`, prints a deterministic bookend: project, run-id, prompt path,
   before/after task counts, agent exit code.
10. **Exit code.** Reflect the child's exit: 0 → ATM exits 0 with the summary;
    non-zero → ATM exits 1 with the error. The agent itself also prints a
    natural-language summary (prompt step 9) inside the opencode TUI; ATM's
    tail summary is the machine-readable bookend from the parent's view.

### Why ATM stays the parent (not `exec`-replace)

`exec` would put opencode in the foreground but eliminate ATM's post-exit
bookend. Staying the parent via `cmd.Run()` gives ATM deterministic control
over the bookends (header before, summary after) without observing the middle.
The user still sees the agent's output live via the inherited stdio.

### Idempotency

Idempotency is a *prompt instruction* backed by *rendered state*. The rendered
prompt embeds the t0 task snapshot; the prompt instructs the agent to match
each candidate against that table AND against what it has already created this
run, by title and topical overlap, before calling `atm task create`. If a match
exists, the agent updates via `atm task set-description` and `atm task label
add` rather than duplicating. Re-running onboarding extends and reconciles; it
does not duplicate.

## Prompt logic vs. label logic

The design separates two layers that must not be fused:

- **Label logic** lives in `internal/seed.Labels`. Each label's `Description`
  is its contract. A fresh agent reads `atm label list --project <CODE>` and
  learns what each label is for. This is the source of meaning.
- **Prompt logic** lives in `internal/onboard/prompt_opencode_vN.md`. It states
  the *goal* and *method* of onboarding, without enumerating labels. It points
  the agent at the label registry and trusts the agent to self-select.

This mirrors v2's core philosophy ("the system has no intrinsic knowledge of
any namespace") — the onboarding prompt likewise has no intrinsic knowledge of
any label. Hardcoding `context:repository` in the prompt would be exactly the
kind of namespace special-casing v2 rejected.

### Label-description refinement (part of this work)

The current seed descriptions are terse ("the labeled task contains
documentation about the project"). For the agent to self-select correctly from
descriptions alone, they are refined to state, per label: what kind of finding
it classifies, and what the task's description should point at. Final wording
is fixed during implementation, but the shape is:

> `context:documentation` — "the task's description points at a specific
> document (file path or URL) and summarizes what it covers, so a later agent
> can decide whether to read it."

Names and the seeded set's structure are unchanged; only descriptions change.

### New seed label: `context:question`

A new seed label is added for the "ask learning question" workflow:

> `context:question` — "the task's description poses an open question or
> ambiguity about the project that a human or later agent should clarify; not
> a defect, not a work item — a gap in understanding."

The agent uses this rather than burying questions inside other tasks'
descriptions. This brings the seed set from 17 to 18 labels;
`seed_test.go`'s count assertion bumps accordingly (the existing
`TestLabelsCountIs17` becomes `TestLabelsCountIs18`).

### The prompt (`prompt_opencode_v1.md`)

Sections:

1. **Orient as an ATM agent first.** "Before exploring the repo, run
   `<ATM_BIN> conventions` and read it. It tells you how ATM projects are
   organized (the label substrate, the first-contact sequence a later agent
   will follow, the advisory seed namespaces). You are building exactly the
   context that `conventions` describes a fresh agent consuming. Understanding
   it makes your output useful to the next agent."

2. **Research already-captured knowledge.** "Run `<ATM_BIN> task list --project
   <CODE> --output json` and read the existing tasks' titles, labels, and
   descriptions. This project may already have context from other repositories
   (frontend, backend, infra — they may disagree). This is your reconciliation
   baseline: what's already known, what's already pointer-mapped, where the gaps
   are. Note any disagreements between existing tasks; you'll either update
   them or flag them as `context:question` rather than silently picking a side."

   (For audit-trail reproducibility, the rendered prompt also embeds a
   `<EXISTING_TASKS>` snapshot taken at t0. The live `atm task list` is the
   agent's research tool; the snapshot is ATM's record of the starting state.)

3. **Role & goal.** "You are an onboarding agent for ATM project `<CODE>`
   (`<PROJECT_NAME>`). Your goal is to build a navigable context map: a later
   agent landing in this project should be able to query ATM, find pointers to
   all relevant context, and narrow to specific resources by reading task
   descriptions. You explore the repository in your current working directory
   and translate what you find into ATM tasks. You operate non-interactively;
   do not ask the human questions — make reasonable judgment calls and proceed."

4. **Tools.** "Your only persistent side-effect is the `atm` CLI at
   `<ATM_BIN>`. All mutations go through it. You may freely read files in the
   cwd. Do not edit repo files, do not `git commit`, do not run long-running
   services. Stamp every mutating command with `--actor <ACTOR>`."

5. **Vocabulary.** "Run `<ATM_BIN> label list --project <CODE> --output json`
   to learn the available labels and their descriptions. Each label's
   description is its contract — match findings to labels by reading those
   descriptions. Use the seeded labels; the label substrate is open but
   onboarding's contract in v1 is to populate the seeded namespaces, not invent
   new ones. If a finding genuinely fits no seeded label, describe it in the
   task description and leave it unlabeled rather than inventing a namespace."

6. **Idempotency & multi-repo reconciliation.** "Before each `atm task create`:
   match against the existing tasks you researched in step 2 AND what you've
   created this run, by title and topical overlap; if a match exists, update
   via `atm task set-description` and add any missing labels via
   `atm task label add` rather than duplicating; if repos disagree about the
   same thing, prefer a task whose description names the disagreement (or a
   `context:question` task) over silently picking a side. Re-running onboarding
   extends and reconciles; it does not duplicate."

7. **What to capture** (goal-stated, label-agnostic):
   - **Agent harness setup** — how an agent should work here: build/test/lint
     commands, conventions, gotchas. A later agent reads this to know how to
     operate.
   - **Structure** — where things live: top-level layout, source/docs/tests/
     configs. A later agent reads this to navigate without re-exploring.
   - **Document & code pointers** — notable docs and code locations, each with
     a path and a one-line statement of what it covers. A later agent searches
     by label, finds the pointer, reads the description to decide whether to
     open it.
   - **Findings** — TODO/FIXME clusters, stale docs, broken examples, obvious
     gaps. A later agent picks these up as work.
   - **Open questions** — anything you couldn't resolve from the code: an
     ambiguity, a "why is it this way", a contradiction between repos. Capture
     each as its own task so a human or later agent can clarify. Don't bury
     questions inside other tasks' descriptions.
   - **Don't duplicate** — if an existing task (from another repo's onboarding)
     already covers a finding, update it; don't restate. The context map should
     converge, not pile up.

8. **Working method.** "Breadth-first, budget-bounded. List top-level
   files/dirs; read README; read docs/ if present; sample representative source
   files. Do not read every file. Stop when you've covered the obvious surface
   and produced the context map. Cap work-task creation at ~20 per run; further
   findings go into a single aggregate task whose description lists them."

9. **Summary.** "Before finishing, run `<ATM_BIN> task list --project <CODE>
   --output json` and print a one-paragraph natural-language summary of what
   you created/updated, any reconciliations you made against existing tasks,
   and any judgments worth flagging. This is the human's onboarding receipt."

### What the prompt does NOT contain

- No enumeration of label names. The agent learns labels from `atm label list`.
- No per-label playbook ("create one `context:repository` task per repo").
  The agent decides, from the label descriptions, which label(s) fit a finding.
- No hardcoded count of index tasks. The agent produces whatever the repo's
  surface warrants, bounded by the cap on work tasks.

## Data model & file layout

No new store entities. Onboarding reuses the existing Task + Label + Project
substrate exclusively. The only new on-disk artifacts are the rendered prompt
files and the embedded prompt assets.

### On-disk

```
$ATM_HOME/
  onboarding/                     # NEW dir; created lazily by `atm onboarding`
    <run-id>.md                   # rendered prompt for run N
  projects/
    <CODE>.json                   # unchanged
    <CODE>/tasks/...              # unchanged
  labels.json                     # unchanged
```

- `<run-id>` = `<CODE>-<YYYYMMDDHHMMSS>-<short-uuid>`, e.g.
  `FOO-20260704121530-a1b2c3`. Sortable, collision-safe, debug-friendly.
- Rendered prompts are kept indefinitely (no GC in v1). They're small, and
  retaining them is the audit trail for "what did the agent see when it ran."

### Embedded assets

```
internal/onboard/
  prompt_opencode_v1.md           # the v1 prompt (raw markdown, placeholders)
  embed.go                        # //go:embed directives + Latest const + Render() + Data
  launcher.go                     # launcher interface + opencode/ollama impls
```

`internal/onboard/` is library-only (no cobra, no stdout, no store). The CLI
layer `internal/cli/onboarding.go` owns `runOnboarding(l launcher, opts)`,
which orchestrates store + render + exec; it calls into `internal/onboard`
for rendering and argv construction.

- `//go:embed prompt_opencode_v*.md` embeds all shipped versions.
- `Latest = "v1"` const (a string matching the filename suffix after
  `prompt_opencode_` and before `.md`); bumped when a new version is added.
- `Render(version string, data Data) (string, error)` — `strings.NewReplacer`
  substitution; returns the rendered markdown. Unknown version → error.
- Placeholders in the prompt: `<CODE>`, `<PROJECT_NAME>`, `<ATM_BIN>`,
  `<ACTOR>`, `<RUN_ID>`, `<TIMESTAMP>`, `<EXISTING_TASKS>`. All substituted;
  no template engine (just `strings.NewReplacer`).

### Seed label change

`internal/seed/seed.go`:
- Add `{"context:question", "<refined description>"}`.
- Refine existing context-label descriptions to be pointer-oriented per the
  label-logic section above.
- `seed_test.go` count assertion bumps from 17 → 18.

No other store API changes. No new store methods. Onboarding uses the public
CLI surface (`atm task create`, `atm task set-description`, `atm task label
add`, `atm task list`, `atm label list`, `atm conventions`) — by design, so
the agent is just another CLI actor and the design adds no second API surface.

## CLI command & error handling

### Command registration

New `onboarding` command group in `internal/cli/`:

- `onboarding.go` — group cmd + shared `runOnboarding(l launcher, opts)`.
- `opencode` and `ollama` registered as subcommands via
  `onboardingCmd.AddCommand(opencodeCmd, ollamaCmd)`.

Follows the existing cobra pattern (`root.go`, `project.go`, `task.go`).
Registered as a subcommand of root: `rootCmd.AddCommand(onboardingCmd)`.

### Error cases

| Case | Exit | Message shape |
|------|------|---------------|
| Project not found | 3 | `project FOO not found; create it first:` + the exact `atm project create --code FOO --name "..."` command |
| Unknown `--prompt-version` | 2 | `unknown prompt version "vN"; available: v1` |
| Launcher not on `$PATH` | 1 | `<launcher>: not found on PATH; install: <hint>` (hint = `https://opencode.ai` for opencode, `https://ollama.com` for ollama) |
| Launcher exits non-zero | 1 | tail summary printed first, then `<launcher> exited: <err>` |
| Prompt-file write fails | 1 | `failed to write prompt file: <path>: <err>` |
| Store open fails | 1 | existing store error envelope |

All errors to stderr in the existing `{"error":{"code","message"}}` envelope
when `--output json`; text otherwise. Same envelope and exit-code convention
as the rest of the CLI.

### Tail summary shape

Text mode:
```
onboarding FOO  run=FOO-20260704121530-a1b2c3
  prompt:  /home/.../atm/onboarding/FOO-20260704121530-a1b2c3.md
  before:  3 tasks
  after:   12 tasks
  created: 9   (net)
opencode exited 0
```

JSON mode (`--output json`):
```json
{"run_id":"FOO-...","project":"FOO","prompt_path":"...","prompt_version":"v1","before":3,"after":12,"agent_exit":0}
```

### Header shape (printed before launch)

Text mode:
```
onboarding FOO  run=FOO-20260704121530-a1b2c3  agent=opencode  prompt-version=v1
  prompt:  /home/.../atm/onboarding/FOO-20260704121530-a1b2c3.md
  launching: opencode run -f <prompt> --auto --title "ATM onboarding: FOO (FOO-...)"
```

JSON mode:
```json
{"run_id":"FOO-...","project":"FOO","agent":"opencode","prompt_version":"v1","prompt_path":"...","argv":["opencode","run","-f","...","--auto","--title","ATM onboarding: FOO (FOO-...)"]}
```

### Dry-run output

Identical to the header, plus the launcher argv, then exit 0 without launching.

## Testing, verification & rollout

### Unit tests

- `internal/onboard/embed_test.go` — `Render` substitutes all placeholders;
  unknown version returns an error; the rendered output contains expected
  fragments for a fixed `Data` input (golden file per version).
- `internal/onboard/launcher_test.go` — `opencodeLauncher.buildArgv` and
  `ollamaLauncher.buildArgv` produce the expected argv for given inputs;
  `notFoundHint` returns the expected install hint.
- `internal/cli/onboarding_test.go` (golden pattern via `testdata/`):
  - Project-missing → exit 3 + the suggested `atm project create` command.
  - Unknown `--prompt-version` → exit 2.
  - `--dry-run` writes the prompt file, prints the launcher argv, exits 0
    without launching (assert no `opencode`/`ollama` process was spawned;
    use a stub binary path that doesn't exist and assert the not-found error
    is *not* reached in dry-run).
  - Header + tail summary shapes match golden files for text and JSON modes.
  - Launcher-not-on-PATH → exit 1 + the install hint (use a launcher name
    that resolves to a non-existent binary).
- `internal/seed/seed_test.go` — label count is 18 (bumped from 17); the new
  `context:question` label is present with a non-empty description.

### What is NOT unit-tested

`opencode run` / `ollama launch` themselves are not unit-tested — they are
external processes with side effects on a real install. Launching is
exercised by a `scripts/onboard-smoke.sh` integration script (manual run),
not by `go test`. The script runs `atm onboarding opencode --dry-run` against
a fixture project and asserts the prompt file + printed argv look right; a
full live run is a manual step the user performs.

### Verification gate

`make verify` (runs `make build && make test`) remains the gate per
AGENTS.md. No new make targets.

### Rollout

Single feature branch, layered commits:
1. `internal/seed` — add `context:question`, refine descriptions, bump test
   count. `make verify` green.
2. `internal/onboard/embed.go` + `prompt_opencode_v1.md` + tests. `make
   verify` green.
3. `internal/onboard/launcher.go` + tests. `make verify` green.
4. `internal/cli/onboarding.go` + `opencode`/`ollama` subcommands + tests +
   golden files. `make verify` green.
5. `scripts/onboard-smoke.sh` + README section. `make verify` green.

Each commit is independently green; the tree builds at every step.

## Future work (explicitly deferred)

- Direct `codex` / `claude` launcher subcommands (when those agents have a
  clean non-interactive `run -f`-equivalent surface ATM can target).
- External-system onboarding (Jira, Quip, Notion, GitHub issues) — likely a
  new `atm onboarding jira` style subcommand with its own prompt version.
- Prompt GC (`atm onboarding clean --before <date>`) once retention matters.
- Per-project prompt overrides (deferred by the embedded-only decision).
- An `atm onboarding log` audit trail (Approach B from brainstorming, dropped
  as marginal benefit over the rendered-prompt-file + history-log audit trail
  ATM already keeps).
- Validating `ollama --integration` against ollama's supported list (deferred
  by the pass-through decision).