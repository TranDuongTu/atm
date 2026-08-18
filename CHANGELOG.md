## Unreleased

### feat
- ATM-cdb166: redesigned the Projects-pane activity charts as one combined box with a bounded persona-card carousel, braille pulse, and `1w`/`1m`/`3m`/`6m`/`1y` time ranges. Each visible persona card keeps its icon on screen and summarizes the selected period's activity total plus top three models. `Ctrl+Left/Right` changes the persona view, `Ctrl+Up/Down` changes the range, and Ctrl navigation gives the chart and selected persona card a transient focus treatment that clears after the interaction. The active range now appears as a full-English bottom legend, such as `Range: One week`. Per-persona dispatch and the old bar stripe are removed.
- ATM-708552: a **setup & readiness wizard**, on `W` from any pane. It is a full-screen view (it replaces the workspace and keeps the status line, rather than layering over it) with one AGENTS section always present — a row per harness with a readiness glyph (`●` ready, `◐` fixable right here, `○` the fix is outside ATM), its installed version, plugin state, launchers, model, and channel coverage — plus CHANNELS and PERSONAS sections that appear only when a project is in scope (with none selected the wizard is honestly global, so those sections are absent rather than empty). It renders in two tiers: everything reachable without a subprocess is on screen immediately, while the cells that cost a `--version` or an `mcp list` per harness render `…` until their probe lands, so a pending answer never reads as a known-empty one; `r` re-probes, and a superseded probe is dropped whole rather than reverting newer facts. Each section carries its own fix ladder — AGENTS: `i` install ATM's plugin, `d` make default, `m` set model, `a` add this project's MCP servers through the harness's own `mcp add`, `l` authorize, `u` run the harness's own update verb; CHANNELS: `w` wire, `s` stamp; PERSONAS: `e` enable the `checklist` capability, `s` author the missing shipped starters; and `c` for a scoped concierge session from anywhere. Every fix ATM performs itself works with **no agent ready at all** (on a fresh store nothing is ready, and the action that ends that bootstrap must not require what it produces); only the concierge waits for a ready agent, and the three that need a terminal of their own (`l`, `u`, `c`) go through the same dispatcher `D` uses — with the literal command shown to paste when there is no dispatch target. A store with **no projects lands on the wizard** at launch (it hands off to project creation once an agent is ready; it never creates a project itself), and the status line carries `⚠ setup [W]` while the agent you would actually dispatch with is not ready (the selected default if there is one, otherwise "no agent is ready at all") — from launch, without the wizard ever having been opened, and absent entirely once there is nothing to say. `Enter` drills into the focused section for what the table has no column for: an agent's MCP servers and each one's health, a channel's per-agent coverage, a persona's missing and customised starters.
- ATM-708552: new `atm setup status [--project <CODE>] [--output json]` — the same picture headless, probing every tier synchronously, read-only (it never writes to the store). Two honesty rules bind both surfaces. **Unknown is not missing**: every fact is tri-state, and a probe that could not answer (harness absent, verb failed, call timed out, output unparseable) reports `unknown`, never `absent` — including the writes, so `a` refuses to "repair" an MCP configuration ATM never managed to read. The rule cuts both ways, and each direction is enforced: a harness that answers "No MCP servers configured" (which `claude` and `opencode` say in prose, not in a parseable row) has ANSWERED, so it reads `absent` and `a` proceeds — the wizard's own bootstrap rung would otherwise dead-end on precisely the fresh machine it exists for; and a server a harness lists without reporting health (`codex mcp list --json` carries configuration only) reads `unknown`, not `absent`, because claiming it is missing would send the user to repair a setup that is fine. **No "update available" claims**: ATM reports the version a harness states and offers that harness's own update verb (`claude update`, `codex update`, `opencode upgrade`), and never compares it against a latest release, because nothing available to it knows one without running the update.
- ATM-3eab28: the host-agent selection is now **agent × launcher × model** instead of a flat six-entry catalog. `ollama` is a launcher, not an agent: the three harnesses (`claude`, `codex`, `opencode`) are one axis and who starts them — the harness itself or `ollama` serving it a local model — is the other, so whether the `ollama` binary is installed is one fact rather than three. `atm agents list` renders that shape in text mode (a row per agent, a column per launcher, `*` on the selected cell); JSON mode still emits one object per selection key, so existing scripts are unaffected — `model` and `launcher` are the only new fields. New: `atm agents select <name> --model M` (with `--model ""` clearing it) and `atm agents models <name>`, which lists what the launcher can serve via `ollama list` / `opencode models` and says plainly that `claude` and `codex` have no list verb. The model rides into the launch argv (for ollama, as its own flag before the `--` separator, which is what ollama accepts headless) and into the session actor, so `developer@ollama:unset` becomes `developer@ollama:qwen3:8b`; an unset model still renders `:unset`, which is the honest answer when ATM does not know the harness's own default. The TUI dispatch dialog shows the configured model on the agent row. **No migration is needed**: `agents.json` already stored the (agent, launcher) pair in `selected`, so this only adds a `models` map keyed exactly like the existing `args` map, and a store with no models configured launches byte-identically to before.
- ATM-77af5e: the `\` spotlight is rebuilt as a horizontal launcher — a left action list and a right full-height preview, superseding the single flat list from ATM-c1bc82. The root shows four groups (Project ▤, Task ☰, Board ▦, Reference §) plus the inline global views (Dispatch, Channels, Personas, Capabilities, Cycle theme). `↑/↓` move the cursor, `Enter` drills into a group, `Esc` peels back one level at a time, `Tab` swaps focus to the preview pane (arrows or `j`/`k` scroll it a line, `pgup`/`pgdn` a screenful — both page keys also work without switching focus), any printable key types into the search query, and `\` closes the spotlight from any level. A non-empty query flattens the visible registry into ranked `Group · Label` matches — at the root, inside a static group, or among a task's own actions — and a query with no hits shows a `no matches` hint.
- ATM-77af5e: the Task group is contextual and carries three distinct hint states. With no project in scope: `select a project first`. Scoped to a project with an empty query: `type to find a task…`. Scoped with a query that hits nothing: `no tasks match`. Otherwise, typing searches that project's tasks case-insensitively against both ID and title, ranks ID matches above title matches, and caps the result list at 5. Every task ID in a project shares that project's code prefix (e.g. `ATM-xxxxxx`), so a short query that happens to appear in the prefix — a single letter, or a fragment like `at`/`tm`/`atm` — matches every task in the project by ID and can fill the 5-result cap before any title match is even considered; this is the direct, intended consequence of the ID-substring matching rule, not a bug. Hovering a result previews the task, including its history (see the task-detail `H` removal below); `Enter` opens six actions scoped to that task, reachable from any pane, cursor position, or board filter, guarded against acting on a task deleted since the list was built. `Esc` from the task actions returns to the results with the query intact.
- ATM-77af5e: the spotlight's memory across a dialog round trip (e.g. opening a form from a drilled-in row, then `Esc`-ing back) now restores the full drill position — level, group, task, query, and cursor — rather than just a root row index.
- ATM-77af5e: **Removed** — the project-detail `H` history toggle; project-detail capability toggling (`c` to cursor-cycle, `space` to enable/disable — the detail view keeps a read-only capabilities listing, and management lives in the `C` capabilities overlay only); the task-detail `H` history overlay (task history now renders in the spotlight's Task-group preview instead); and two keymap-reference rows, `Cycle sort` and `Re-ensure capability vocabulary`, on the tasks list (both keys, `s` and `S`, still work — only their advertised rows were dropped). The sort order is now shown inline on the tasks pane header (`SORT: updated-desc [s]`), and seeding is advertised from the Board group instead (`[S] seed vocabulary`).

### feat
- ATM-3714db: the help overlay is retired in favor of a single declarative entry table that both a menu overlay and the keymap reference render from, so advertised keys can never drift from real bindings; entries replay the real key (same behavior path as a direct keypress), and the status bar no longer shows per-action key hints — a single pointer remains instead (see ATM-c1bc82 for the trigger and surface that pointer opens today). The `C` conventions binding is removed — conventions is a menu reference view only, and `C` is the capabilities switcher.
- ATM-c1bc82: the `[?]` contextual menu is replaced by a `\` spotlight — one global, key-first list of every action across every pane, rather than a menu scoped to the current context. Hovering a row previews it: a one-line summary by default, and for a registered subset of dialog entries (dispatch, channels, personas, capabilities, and the project/task create forms) a live render of the actual overlay/form content; entries with no registered renderer fall back to their summary. Reference entries (keymap, CLI↔TUI parity, conventions) preview their full text. `→` is the only activation key — it replays the scope's prelude and then the entry's key through the real dispatch path, so a spotlight activation and a direct keypress are indistinguishable; `Enter` is inert in the list. `Esc` on a dialog opened from the spotlight returns to the spotlight with the cursor where it was; a successful submit, dispatch, or capability switch lands on the workspace instead. `?` is unbound, and the status bar now advertises `[\]spotlight`.
- Session contexts render a ## Capabilities block: every enabled capability's one-line brief, sourced from its own guide frontmatter (ATM-daa760).
- New checklist capability: named per-persona standing operating procedures behind atm checklist, with seeded concierge starter checklists (ATM-daa760).
- ATM-3714db: concierge dispatch from the channels overlay is scoped — the dispatch argv carries `--capability channel` and the dialog renders a `Scope: channel capability` line.
- ATM-3714db: `--capability` renders a `## Session scope` section in the session context (orientation stays capability-local), and the capability segment joins the session-context cache key, so scoped and unscoped sessions never share a context file.
- ATM-3714db: the dispatch argv now carries `--project` for any non-admin persona when a project is known — project-optional personas still accept it, and a scoped session needs a project-bound context. Only `admin` dispatches without a project.

### feat
- Channels: repositories and Notion databases as first-class, ledger-recorded
  communication surfaces (`atm channel`, channels TUI overlay, `E`); `atm
  project repo` verbs retired (`atm channel migrate-repos` lifts existing
  entries).
- ATM-29f8b0: the TUI dispatch dialog is now universal. `D` opens it from any
  pane (even an empty workspace); the persona is a selectable field (`p`) over
  every store persona instead of being fixed by the opening context. Context
  only preselects the persona, project, and task. A persona that requires a
  project shows an inline warning and refuses to dispatch when no project is in
  scope. Concierge (and other project-optional personas) are now reachable from
  the TUI on a fresh project with no activity.
- ATM-0119: `atm update` self-updates the running binary from GitHub releases,
  verifies `SHA256SUMS`, and atomically replaces `os.Executable()`; use
  `--version <tag>` to pin a release.
- **Breaking:** `atm dev` and `atm manage` are removed. Launch sessions with
  `atm --persona developer|manager|concierge --project <CODE>`; manager
  brief/autopilot are now `--mode brief|autopilot|ask` (default autopilot).
- Built-in personas (developer, manager, admin, new concierge) now ship inside
  the binary from the top-level `skills/` folder and are no longer seeded into
  the store; leftover seeded files are ignored. Customize a built-in with
  `atm persona personality <name> --set/--file/--clear`.
- Custom personas persist as markdown (`personas/<name>.md`); legacy JSON
  personas migrate automatically on first read.
- Capability guides are restructured: `## Semantics` / `## Actions` /
  `## Converge` replace the manager-specific `## Brief` / `## Autopilot`.
- New `concierge` persona: plain-language onboarding; launchable without
  `--project`.
- Env: `ATM_MODE`/`ATM_CAPABILITY` replace `ATM_MANAGER_ACTION`/
  `ATM_MANAGER_CAPABILITY`. `ATM_ROLE` still reads `developing` for developer
  sessions (installed session-start hooks keep working); reinstall plugins
  (`atm init`) to pick up the new context-file-gated hooks.

### feat
- ATM-0871aa: project repo dispatch targets. New `atm project repo add/list/remove` records machine-local repo dispatch targets (name + path + url) in `config.json` — config, not substrate (no event-log entry, not synced), so a fresh machine re-records them via a concierge session. The developer dispatch dialog gains a `Repo:` cycle-picker (`↑/↓`) over the project's repos; `Spec.Dir` becomes the selected repo's path, falling back to cwd when none are recorded. Manager/concierge/admin dispatches are unchanged. The concierge persona records repos during onboarding (Step 2 asks for the local folder + remote link; Step 4 writes them via the CLI verb in plain language).
- ATM-4eae82: TUI per-project background art. The spare vertical space in the
  Projects pane (between the project list, now a fixed 5-row page, and the
  events feed) and the Tasks pane (between the task table and the boards ring)
  now fills with a dim, subtly-animated ASCII motif that gives each project a
  stable visual identity. Art rescales with the terminal, collapses back to
  blank padding when space is tight, and animates only while the plain
  workspace is visible. (Default-on auto-assignment and the `atm project theme`
  CLI described in the original shipped release were later replaced — see
  ATM-cac464.)
- ATM-cac464: TUI art is now off by default and switchable per project. Each
  project shows a pair of two of six motion themes (galaxy, lorenz, matrix,
  tunnel, skyline, constellation); `A` (Shift+A) toggles art on/off for the
  scoped project, persisting to `art_on` and flashing the status line. Each
  off->on re-rolls a fresh random pair and pins it to a new `art_pair` config
  field so it survives a TUI restart; turning art off clears the pin. (A
  project that has never toggled art falls back to a deterministic,
  code-derived pair.) When art is on, pair[0] renders in the Projects pane
  and pair[1] in the Tasks pane gap, and only when a project is selected
  (fixes art appearing on TUI startup). The `atm project theme` CLI is
  removed; art switching is TUI-only.
- ATM-4b7e24: TUI agent dispatch. `D` from the projects pane dispatches a manager session and `D` from the tasks pane dispatches a developer session bound to the selected task, each spawned into an auto-detected terminal surface (herdr → tmux → terminal tab) via the new `internal/dispatch` package. The agent is the only interactive field (cycle with `←/→`); an unready agent is refused with its missing-bin hint; the target preview and any detection failure render in the dialog. Fire-and-forget — no session registry.
- ATM-4b7e24: `V` opens a read-only personas overlay in the TUI (list built-ins and customs, `Enter` views a persona's effective prompt, `Esc` backs out to the list then closes). No create/edit/personality customization from the TUI.
- ATM-4b7e24: `--task <id>` session assignment. New optional `atm --persona <p> --project <CODE> --agent <a> --task <id>` flag — validated against the project's store (missing task or cross-project task fails before launch), exported to the host as `ATM_TASK=<id>`, and rendered into the session context as an assigned-task block. Task-keyed context caches prevent concurrent task sessions from sharing a context file.
- ATM-4b7e24: `internal/dispatch` targets — `Spec`/`Env`/`Target`, shell quoting, `Config`/`LoadConfig`, `tmuxTarget`, `terminalTarget` (config `terminal_cmd` template via `sh -c` with `{cmd}`/`{dir}`/`{title}` placeholders + an emulator spawn table for kitty/wezterm/gnome-terminal/konsole/alacritty/foot), `herdrTarget` (split → rename → run), `Detect` precedence, and a `Service` facade. `dispatch.json` at the store root configures the terminal fallback.
- ATM-2e64a5: task metadata column. New `task.capability-meta-set` event folds into per-capability `meta!<name>` scalar slots on `eventsource.TaskState.Meta`; `core.Task.Meta` and `Store.SetTaskCapabilityMeta` (mirrored on `TaskWriter`, `TaskService`, `changeSet`) carry it through the write path; cache schema bumped to v4. `atm task show` lists metadata presence (capability + bytes, never content).
- ATM-2e64a5: capability-annotated contextual column in the tasks pane. `Annotate(task core.Task) *Cell` on the `Capability` interface (with `Registry.Annotate`) supplies a tone-styled cell following `capabilityModel.current` — workflow renders status cells, contextmap renders kind cells on context tasks; `[C]` to unmanaged hides the column.
- ATM-793b19: Recent Events feed in the TUI Projects pane — a git-log-style digest of the selected project's event stream (commit-graph gutter, event ids when space permits, per-action wording), rendered as a bordered box aligned with the summary chart boxes below it. Scrolls modelessly with `Shift`+arrows — up/down by line, left/right by page — with no focus mode to enter.
- ATM-0083: agent as config, not flags. The host agent is now a stored default — choose it once with `atm agents select <name>` (`atm agents list` shows readiness; `atm agents args <name> -- <flags>` sets per-agent defaults), then launch with `atm dev --project <CODE>` and `atm manage --project <CODE> --<action>`. Override a single launch with `--agent <name>` or `ATM_AGENT`.
- ATM-0115: boards (computed labels). A label may carry an expression over other labels; its membership is computed, not asserted. New `--expr` flag on `atm label add` and `atm task list` (`--label <CODE>:<board>` resolves a board's expression). Label-name grammar widened so `:*` namespace wildcards are legal labels; `atm label seed` back-fills namespace descriptors into existing projects.

### perf
- ATM-4c476c: TUI lag fixes. The v2 cache-freshness probe (`ChangeCount`) is
  memoized against the event file's stat identity, so the steady-state probe
  is one `os.Stat` instead of reading the whole `events.v2.jsonl`; the tasks
  pane resolves its annotate registry once per refresh instead of once per
  row; and the summary pane + events feed render from refresh-time snapshots,
  making `View()` pure formatting (zero store reads per frame). On a live-
  scale store (2.4k events, 188 tasks): refreshAll 106ms→48ms with 635MB→19MB
  allocated, View 18ms→2.9ms per frame.
- ATM-4c476c: the indexer can no longer block the UI. Embedding requests are
  context-aware with a 60s backstop timeout (`EmbedFunc` now takes a
  `context.Context`), and stopping the watcher (project switch, `q` quit,
  reset) cancels without waiting — each run gets its own message channel, so
  an abandoned watcher can't pollute the next run's log. The background
  watcher's drain tick relaxes from 120ms to 500ms when the indexer overlay
  is closed, cutting steady-state re-renders ~4x.

### changed
- ATM-0115: the Labels pane is now the Boards pane — a flat list of computed labels (boards + namespaces) with a live-validated expression editor.
- Removed per-agent launch subcommands (`atm claude|codex|opencode|ollama` and `atm manage <agent>`). Ollama variants are now catalog entries (`ollama:<integration>`), so the `--integration` flag is gone. `atm init` additionally records the first installed agent as the default.


## v1.2.11 - 2026-07-11

### docs
- docs: plan CLI user surface simplification
- docs: specify CLI user surface simplification
- ATM-0072: implementation plan for actor convention enforcement
- ATM-0072: spec for actor convention enforcement


## v1.2.10 - 2026-07-11

### docs
- ATM-0072: implementation plan for actor convention enforcement
- ATM-0072: spec for actor convention enforcement


## v1.2.9 - 2026-07-09


## v1.2.8 - 2026-07-09

### docs
- Merge ATM-0071: agent prompt minimization — thin developing nudges + principle-driven manager render-context pointer
- docs: reconcile D15 — no-config does not auto-open overlay (dock hint surfaces it)
- spec+plan: dock always visible (D14) + auto-start on project select (D15) + error auto-opens overlay (D16)
- spec+plan: dock keybind hint (D12) + bottom-anchored log pane (D13)
- docs: ATM-0071 agent prompt minimization implementation plan
- docs: ATM-0071 agent prompt minimization design spec
- plan: TUI indexer integration — 9-task TDD rollout (ATM-0071)
- spec: TUI indexer integration — plugin dock + indexer overlay (ATM-0071)


## v1.2.7 - 2026-07-09

### docs
- Merge ATM-0071: agent prompt minimization — thin developing nudges + principle-driven manager render-context pointer
- docs: reconcile D15 — no-config does not auto-open overlay (dock hint surfaces it)
- spec+plan: dock always visible (D14) + auto-start on project select (D15) + error auto-opens overlay (D16)
- spec+plan: dock keybind hint (D12) + bottom-anchored log pane (D13)
- docs: ATM-0071 agent prompt minimization implementation plan
- docs: ATM-0071 agent prompt minimization design spec
- plan: TUI indexer integration — 9-task TDD rollout (ATM-0071)
- spec: TUI indexer integration — plugin dock + indexer overlay (ATM-0071)
- Refine ATM-0057 design: verified embedding backend, project-declared model, run-once watcher, eval deferred
- Implementation plan: ATM memory substrate — retrieval + indexing + recall measurement (ATM-0057)
- Design spec: ATM as memory substrate — retrieval surface + indexing model + manager cognition (ATM-0057)


## v1.2.6 - 2026-07-09

### docs
- Refine ATM-0057 design: verified embedding backend, project-declared model, run-once watcher, eval deferred
- Implementation plan: ATM memory substrate — retrieval + indexing + recall measurement (ATM-0057)
- Design spec: ATM as memory substrate — retrieval surface + indexing model + manager cognition (ATM-0057)
- Fix ATM-0047 + ATM-0061: manager plugin staleness detection + developing gene boundary


## v1.2.5 - 2026-07-08

### docs
- Fix ATM-0047 + ATM-0061: manager plugin staleness detection + developing gene boundary
- Reconcile TUI chart with spec: sort by weight, empty-state text, layout mockup (ATM-0028)
- Update docs/smoke for manager onboarding + Ubiquitous Language (ATM-0028)
- Implementation plan: manager knowledge-base owner + onboarding unification + Ubiquitous Language (ATM-0028)
- Design spec: manager as knowledge-base owner (onboarding unification + Ubiquitous Language) (ATM-0028)


## v1.2.4 - 2026-07-08

### docs
- Reconcile TUI chart with spec: sort by weight, empty-state text, layout mockup (ATM-0028)
- Update docs/smoke for manager onboarding + Ubiquitous Language (ATM-0028)
- Implementation plan: manager knowledge-base owner + onboarding unification + Ubiquitous Language (ATM-0028)
- Design spec: manager as knowledge-base owner (onboarding unification + Ubiquitous Language) (ATM-0028)
- Implementation plan: persona activity in Projects pane + P overlay + p form (ATM-0054)
- Design spec: persona activity in Projects pane + P overlay + p add-persona (ATM-0054)
- Implementation plan: personas & actor activity (ATM-0052)
- Design spec: personas & actor activity (ATM-0052)


## v1.2.3 - 2026-07-08

### docs
- Implementation plan: persona activity in Projects pane + P overlay + p form (ATM-0054)
- Design spec: persona activity in Projects pane + P overlay + p add-persona (ATM-0054)
- Implementation plan: personas & actor activity (ATM-0052)
- Design spec: personas & actor activity (ATM-0052)
- Toggle exact-label filter on row Enter; move label detail to i key (ATM-0041)
- Add implementation plan for ATM-0041 labels namespace filtering
- Add design spec for ATM-0041 labels namespace filtering


## Unreleased
<!-- manual entry; release.sh prepends a dated section at cut — fold these bullets into the next release section and delete this block -->

### tui
- Replace Projects pane "activity by actor" chart with persona-grouped "activity by persona" (alias-resolved); fix bar-width alignment (ATM-0054)
- Remove the `[4] Actors` maximized pane; `numPanes` back to 3 (ATM-0054)
- Add `P` key in Projects pane to expand the persona activity chart into an overlay with per-persona agents/models/actions drilldown (ATM-0054)
- Add `p` key in Projects pane to open a New persona form (name + description; prompt set via CLI) (ATM-0054)

### docs
- Document personas, actor convention (`persona@agent:model`), `atm actor migrate`/`alias`, `atm developing --persona`, and `atm activity` (ATM-0052)
- Update conventions/README to reference the `P` overlay + `p` add-persona instead of the removed `[4] Actors` pane (ATM-0054)


## v1.2.2 - 2026-07-07

### docs
- Toggle exact-label filter on row Enter; move label detail to i key (ATM-0041)
- Add implementation plan for ATM-0041 labels namespace filtering
- Add design spec for ATM-0041 labels namespace filtering


## v1.2.1 - 2026-07-07


## v1.2.0 - 2026-07-07


## v1.1.4 - 2026-07-06

### docs
- Add implementation plan: cache.db consolidation (ATM-0027 phase 1)
- Add design spec: consolidate caches into SQLite + cross-machine sync
- Add implementation plan: launcher extra agent args + ollama host
- Refine launcher-args spec: construct OllamaLauncher directly (mirror onboarding)
- Add design spec: launcher extra agent args + ollama host for developing/manager


## v1.1.3 - 2026-07-06

### docs
- Add implementation plan: cache.db consolidation (ATM-0027 phase 1)
- Add design spec: consolidate caches into SQLite + cross-machine sync
- Add implementation plan: launcher extra agent args + ollama host
- Refine launcher-args spec: construct OllamaLauncher directly (mirror onboarding)
- Add design spec: launcher extra agent args + ollama host for developing/manager
- fix manager logics


## v1.1.2 - 2026-07-06

### docs
- fix manager logics


## v1.1.1 - 2026-07-06

### docs
- Add ATM manager subagent implementation plan (ATM-0024)
- Revise manager spec: add prompt appendix, dispatch contract, fast/thorough pacing
- Add ATM manager subagent design spec (ATM-0024)


## v1.1.0 - 2026-07-06

### docs
- Add ATM manager subagent implementation plan (ATM-0024)
- Revise manager spec: add prompt appendix, dispatch contract, fast/thorough pacing
- Add ATM manager subagent design spec (ATM-0024)


## v1.0.2 - 2026-07-06


## v0.1.1 - 2026-07-06

### docs
- docs: semver build & release pipeline implementation plan
- docs: semver build & release pipeline design spec
- remove existing tasks interpolation in developing prompt
- docs: TUI visual polish implementation plan
- docs: TUI visual polish design spec
- docs: plan atm developing launcher
- docs: design atm developing launcher
- docs: task comments v1 implementation plan
- docs: task comments v1 design spec
- feat(tui): add project summary activity charts
- docs: tighten plan task-brief cross-refs and seq-gap assertion
- docs: audit log redesign implementation plan
- docs: audit log redesign spec (event-sourced per-project WAL)
- docs: plan project summary charts
- docs: design project summary charts
- docs: onboarding v1 spec and implementation plan
- update Help and Convention layout
- docs: add three-pane tui implementation plan
- docs: specify three-pane tui workspace
- fix layout and themes
- docs: add TUI theme refresh plan
- docs: add TUI theme refresh design
- chore: gofmt canonicalization (trailing newlines + alignment) + add label-management plan doc
- spec: label management refinement — dedicated Labels tab, multi-label task create, default seeding, agent code-of-conduct
- update plan
- v2: amend spec facets JSON shape + fix label form titles + keymap b row
- add spec
- spec: add Onboarding & conventions (Section 7) + atm conventions cmd + tui auto-init
- update spec
- spec: rewrite tasks-management v2 as pure label-substrate
- remove legacy plan
- spec: rewrite tasks-management v2 as single superpowers design file
- spec: rewrite 001-tasks-management to v2.0.0
- feat: introduce tui workspace focus
- Add lazygit-style TUI rewrite design
- replace speckit with superpowers


## v0.1.0 - 2026-07-06

### docs
- docs: semver build & release pipeline implementation plan
- docs: semver build & release pipeline design spec
- remove existing tasks interpolation in developing prompt
- docs: TUI visual polish implementation plan
- docs: TUI visual polish design spec
- docs: plan atm developing launcher
- docs: design atm developing launcher
- docs: task comments v1 implementation plan
- docs: task comments v1 design spec
- feat(tui): add project summary activity charts
- docs: tighten plan task-brief cross-refs and seq-gap assertion
- docs: audit log redesign implementation plan
- docs: audit log redesign spec (event-sourced per-project WAL)
- docs: plan project summary charts
- docs: design project summary charts
- docs: onboarding v1 spec and implementation plan
- update Help and Convention layout
- docs: add three-pane tui implementation plan
- docs: specify three-pane tui workspace
- fix layout and themes
- docs: add TUI theme refresh plan
- docs: add TUI theme refresh design
- chore: gofmt canonicalization (trailing newlines + alignment) + add label-management plan doc
- spec: label management refinement — dedicated Labels tab, multi-label task create, default seeding, agent code-of-conduct
- update plan
- v2: amend spec facets JSON shape + fix label form titles + keymap b row
- add spec
- spec: add Onboarding & conventions (Section 7) + atm conventions cmd + tui auto-init
- update spec
- spec: rewrite tasks-management v2 as pure label-substrate
- remove legacy plan
- spec: rewrite tasks-management v2 as single superpowers design file
- spec: rewrite 001-tasks-management to v2.0.0
- feat: introduce tui workspace focus
- Add lazygit-style TUI rewrite design
- replace speckit with superpowers


# Changelog

All notable changes to atm are documented here. The first release section
will be prepended by `scripts/release.sh` phase 3.
