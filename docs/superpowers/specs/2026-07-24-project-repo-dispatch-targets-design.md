# Project repo dispatch targets: config.json repos + concierge recording + TUI picker

- **Task:** ATM-0871aa
- **Date:** 2026-07-24
- **Status:** Approved design, pre-implementation

## Problem

Developer dispatch from the TUI (`internal/tui/dispatch.go`) always spawns the
agent session in the TUI's current working directory (`os.Getwd()`), regardless
of which repo the project's work actually lives in. A project is not 1:1 with a
repo — one project may span several repos — so "the cwd" is frequently the
wrong place. The concierge already briefs the user on their repos during
onboarding (`skills/persona/concierge.md` Step 2) but records that knowledge only
as `context:repository` pointer tasks (the knowledge layer); nothing durable
captures the *local dispatch target* (where to spawn), so the TUI has nothing to
offer.

Two gaps follow:

1. The concierge learns where each repo lives on this machine and its remote
   link, but has no sanctioned place to record that machine-local dispatch
   state. It is lost when the session ends.
2. The developer dispatch dialog has no way to choose which local repo to spawn
   into; it always uses cwd.

## Goals

- Let the concierge record, during onboarding, each repo's local path and
  remote link as a dispatch target for the project.
- Let the user manage those dispatch targets later via a CLI verb.
- Let the developer dispatch dialog select among the project's recorded repos
  before spawning, falling back to cwd when none are recorded.
- Keep repos machine-local: they are config, not substrate state, so a fresh
  machine carrying a synced event log has no repos until a concierge session
  records them there.

## Decisions of record

- **Repos are config, not substrate.** A new `repos` list in `config.json`
  (name + path + url) joins `embedding`, `remotes`, and `boards` as a
  display/config field: no event-log entry, no history entry, not carried by
  the event source, not synced. A repo is not a core domain object. This means
  a fresh machine with a synced event log has *no* repos until a concierge
  session records them locally — which is the intended behavior: repos are
  machine-local dispatch targets, re-established per machine by the concierge.
- **A repo record is separate from a `context:repository` pointer.** The
  pointer is knowledge (what the repo *is*, its branches, its role) and is
  synced substrate-adjacent state; the repo config is the machine-local
  dispatch target (where to spawn). The concierge records both, but they are
  not collapsed into one record.
- **CLI-managed, TUI-consumed.** A new `atm project repo` command group
  (`add`/`list`/`remove`) manages the list, mirroring `atm store remote`. The
  TUI dispatch dialog is read-only over the list. This matches how every other
  `config.json` field already works (`remote`, `embedding`, `boards` all have
  verbs).
- **Developer dialog only.** Only the developer (task-bound) dispatch dialog
  gets the repo cycle-picker. Manager, concierge, and admin dispatches stay on
  cwd — they are project-level or project-optional and do not target a specific
  repo's working tree.
- **Path validation at add time.** `atm project repo add` resolves `~` and
  relative paths to absolute and requires the directory to exist. Dispatch
  does not re-check existence; a dir that has since vanished surfaces the error
  at spawn time through the target (tmux/terminal), same as a missing cwd
  would today.
- **Two cycle-picker axes in the dialog.** The existing `←/→` cycles the agent;
  a new `↑/↓` cycles the repo. Single-key, unambiguous, no nested menus.

## Design

### 1. Data model & store layer

New config struct in `internal/core/config.go`:

```go
type RepoConfig struct {
    Name string `json:"name"`          // short handle, unique within the project
    Path string `json:"path"`          // absolute local path (existence-validated on add)
    URL  string `json:"url,omitempty"` // remote link the concierge logged; optional
}

type ProjectConfig struct {
    // ...existing fields unchanged...
    Repos []RepoConfig `json:"repos,omitempty"`
}
```

`Repos` is config, not substrate state — same status as `Remotes`,
`Embedding`, and `Boards`: no event-log entry, not carried by the event source,
not synced. The `GetProjectConfig` "is this effectively empty?" check
(`internal/store/config.go`) is extended to include `len(c.Repos) == 0` so an
all-default config still reads as absent.

New store methods in `internal/store/config.go`, mirroring
`SetProjectRemote`/`RemoveProjectRemote`/`ProjectRemotes`:

- `SetProjectRepo(code, name, path, url, actor string) error` — upsert by name.
  Resolves `~` and relative paths to absolute via `filepath.Abs` (no
  `os.Getwd` dependency — operates on the passed path). Requires the resolved
  dir to exist (`os.Stat`); rejects empty name or path with `core.ErrUsage`.
  Read-modify-write under the project lock; refreshes `updated_at`/`updated_by`.
  `url` is optional (empty allowed).
- `RemoveProjectRepo(code, name, actor string) error` — `core.ErrNotFound` if
  the name is absent.
- `ProjectRepos(code string) ([]RepoConfig, error)` — returns the list (nil/empty
  if none or no config).

No event emission, no history entries — identical to the remote methods. The
`actor` stamp is required (validated) for auditability in `updated_by`, even
though there is no event.

### 2. CLI surface

New command group `atm project repo` in `internal/cli/project.go`, mirroring
`atm store remote` (add/list/remove). Registered alongside the existing project
subcommands.

```
atm project repo add    <name> <path> [--url <url>] --project <CODE> [--actor <a>]
atm project repo list                       --project <CODE>
atm project repo remove <name>              --project <CODE> [--actor <a>]
```

- **`add`** — positional `name` + `path` (`cobra.ExactArgs(2)`); `--url`
  optional; `--project` required; `--actor` via `resolveActor(true)`. Calls
  `store.SetProjectRepo`, which resolves the path to absolute and requires
  existence. Upsert by name: re-adding the same name updates path/url. Text
  output: `added repo <name> -> <path> (project <CODE>)`; JSON:
  `{project, name, path, url}`.
- **`list`** — `--project` required (repos are machine-local, so there is no
  "all projects" mode, unlike remotes). Text: `name\tpath[\turl]` per line;
  JSON: array of `{name, path, url}`.
- **`remove`** — positional `name` (`cobra.ExactArgs(1)`); `--project` required;
  `--actor` resolved. Calls `store.RemoveProjectRepo`. Text:
  `removed repo <name> (project <CODE>)`; JSON: `{project, name}`.

Errors follow the existing convention: `core.ErrUsage` for missing required
flags, `core.ErrNotFound` for removing an unknown name (maps to the same exit
codes the `remote` commands use).

The `atm project` long help gets a one-line addition noting the `repo` subgroup
records local dispatch targets.

### 3. Concierge persona change

The concierge persona doc (`skills/persona/concierge.md`) gains a small,
plain-language addition — no jargon, no "config.json" exposed to the user.

**Step 2 — Converse** (existing repo bullet, lightly extended): for each repo
the user names, also ask where it lives on this machine (the local folder) and
its remote link if it has one.

**Step 4 — Triage** (new action after project creation, alongside recording
reference tasks): for each repo the user named in Step 2, record it as a
dispatch target: `atm project repo add --project <CODE> --name <short-name>
--path <local-folder> [--url <remote-link>]`. Confirm in plain words before
writing — "I'll note that your `atm` work lives in
`~/projects/scyllas/atm`" — never expose the flag shape. This is machine-local
setup: when the user sets up ATM on a new machine, run a concierge session there
to re-record the local paths.

The concierge still records `context:repository` pointer tasks (knowledge
layer). The new `atm project repo add` calls record the dispatch target. These
are separate concerns and stay distinct in the doc.

### 4. TUI developer dispatch dialog

The developer dispatch dialog (`internal/tui/dispatch.go`) gains a second
cycle-picker line. Manager/concierge/admin dialogs are unchanged.

**Data flow:** when `dispatchModel.open` runs for a developer dispatch, it reads
the project's repos via `m.store.ProjectRepos(project)` and stores them on the
model. The dialog preselects the first repo; if the list is empty, it falls
back to cwd (current behavior, no visible change beyond one display line).

**New model fields:**

```go
type dispatchModel struct {
    // ...existing...
    repos      []store.RepoConfig // project's repos; empty -> cwd fallback
    repoCursor int                // selected repo index
}
```

**Key handling** — two cycle-picker axes:

- `←/→` (`h/l`): cycle agent (unchanged)
- `↑/↓` (`k/j`): cycle repo (new; no-op when the list is empty)
- `enter`: dispatch; `esc`: close (unchanged)

**Render** — a new `Repo:` line inserted between the `Task:` block and the
`Agent:` line:

```
╭─ Dispatch developer — ATM ── ATM-4b7e24 ───────────╮
│                                                     │
│  Task:     ATM-4b7e24                               │
│            TUI agent dispatch: …                    │
│                                                     │
│  Repo:     ‹ ~/projects/scyllas/atm ›               │
│            (cwd)                  ← shown when empty │
│                                                     │
│  Agent:    ‹ opencode ›                             │
│            ready                                    │
│                                                     │
│  Target:   herdr · pane "ATM · developer · ATM-…"   │
│                                                     │
╰─────────────────────────────────────────────────────╯
  ←/→ agent  ↑/↓ repo  [Enter] dispatch  [Esc] close
```

When `repos` is empty, the `Repo:` line shows `‹ (cwd) ›` and `↑/↓` is a
no-op — the dialog renders and behaves exactly as today, plus one display
line.

**Dispatch (`submit`):** `Spec.Dir` is set to the selected repo's resolved path
when `len(repos) > 0`, otherwise `os.Getwd()` (current behavior). No existence
re-check at spawn — the path was validated at `add` time; if the dir has since
vanished, the spawn target surfaces the error in its own way, same as a missing
cwd would today.

**Keymap** (`internal/tui/keymap.go`): the dispatch help line gains `↑/↓ repo`;
the existing `D`/`V` bindings are unchanged.

## Testing

- **`internal/store/config.go`** (mirror the existing `remote` tests in
  `config_test.go`): `SetProjectRepo` upserts by name (add new, update existing
  path/url); `RemoveProjectRepo` returns `core.ErrNotFound` for an unknown
  name; empty name or path → `core.ErrUsage`; path resolution (`~`-prefixed and
  relative → absolute; non-existent → error); actor required; multiple repos
  coexist in insertion order; read-modify-write preserves existing
  `Embedding`/`Remotes`/`Boards` (no clobber); `GetProjectConfig` empty-check
  treats a lone `Repos` entry as non-empty.
- **`internal/cli/project.go`** (mirror `store_sync` test patterns):
  `project repo add` writes and prints (text + JSON shapes); `project repo
  list` output shapes (text + JSON); `project repo remove` success and
  `ErrNotFound` exit code; missing `--project` → usage error; missing
  positional → cobra arg error.
- **`internal/tui/dispatch.go`** (extend `dispatch_test.go`): developer dialog
  with repos present — `Repo:` line renders, `↑/↓` cycles, `submit` spawns with
  `Spec.Dir` = selected repo path; developer dialog with no repos — `Repo:`
  shows `(cwd)`, `↑/↓` no-op, `submit` spawns with `Spec.Dir` = cwd (existing
  behavior preserved); manager dialog — no `Repo:` line, cwd dispatch
  unchanged (regression guard); argv still omits/sets `--project` per
  `projectRequired()` — repo selection changes only `Spec.Dir`, never the argv.
- **`skills/skills_test.go`**: the existing concierge smoke test stays green;
  no new assertion required (the persona doc change is prose, not behavior the
  test asserts).

`make verify` is the gate.

## Implementation stages (one branch)

1. `RepoConfig` struct + `ProjectConfig.Repos` field + store methods
   (`SetProjectRepo`/`RemoveProjectRepo`/`ProjectRepos`) + `GetProjectConfig`
   empty-check update; store tests.
2. `atm project repo` command group (add/list/remove) + project help text; CLI
   tests.
3. Concierge persona doc updates (Step 2 + Step 4).
4. TUI developer dispatch dialog: repo cycle-picker, render, `Spec.Dir`
   selection, keymap help; TUI tests.
5. Docs (README, CHANGELOG) + ledger updates.