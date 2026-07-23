# ATM — Agent Tasks Manager

ATM is a fast, scalable, distributed task ledger — git-like in how it stores truth, Jira-like in how it tells the story — built as the main interface through which coding agents keep a software organization's knowledge base.

## 30-Second Start

**1. Install** the `atm` binary:

```sh
curl -fsSL https://raw.githubusercontent.com/TranDuongTu/atm/main/scripts/install.sh | bash
```

**2. Initialize** the store, install the agent plugins, and record your default agent and args:

```sh
atm init                                 # once: store, agent plugins, default agent + args
```

**3. Onboard with the concierge.** Run the concierge to set up your first project — it walks you through what ATM can do, learns how you work, and recommends the capabilities that fit:

```sh
atm --persona concierge                  # plain-language onboarding (no project needed)
```

The concierge creates your project, enables the right capabilities, and seeds their vocabulary.

**4. Daily work.** Open the dashboard to see everything, start dev sessions in repo directories, and run the manager to keep the ledger converged:

```sh
atm                                       # dashboard: tasks, projects, labels, activity

atm --persona developer --project ATM      # developer session (run inside the working repo)
atm --persona developer --project ATM --agent claude

atm --persona manager --project ATM        # converge all enabled capabilities across the backlog
```

## The Story

### What You Can Try Today

- Work across multiple projects at once, including projects that span several repositories.
- Switch coding agents freely to manage cost, context, and tokens — the ledger, not the agent, holds the state.
- Resume or hand off work between agents with minimal re-briefing.
- Move between machines: the store is an append-only ledger that is portable and shareable by copy.
- Skip ticket UIs built for human browsing — ask your agents, and they work from the ledger.
- Keep ideas flowing into the backlog anytime, anywhere, and let the manager groom and plan them later.

### The Grand Vision

Whether the future belongs to AI or to humans, software has to remain soft to stay useful — and it stays soft only while the intent behind it stays legible. Software has always been where an organization accumulates its lessons and scales them, and for decades human engineers kept that knowledge base alive with more than languages and IDEs: git preserved every decision as history, while Jira boards and a sprawl of docs carried the narrative of where the system goes next. A senior engineer often works those tools more than they write code.

Agentic coding has changed the interface between the developer and the software. You rarely write your own PRs or commits, you no longer read the tracker line by line, and you juggle more worktrees than you ever imagined — you manage intentions more than you manage code. Git honestly tells you the current truth and the whole history behind it, but not the road ahead. The road ahead lives in Jira, Notion, Quip, Google Docs — mutable surfaces where every edit overwrites the last, keeping the current plan but losing the growth, the decisions, and the awareness that produced it. Putting an MCP server in front of them gives agents access, not a fit.

As the world turns agentic, your main working interface becomes a single terminal where you talk to your own agents — and agents forget. Sessions end, context windows die, models get swapped for cost; the knowledge has to survive all of it. So ATM gives your intentions the storage discipline git gave your code: an append-only, plain-file, mergeable ledger that agents journal into as they work, recall from when they return, and hand off through when they change — and the one window through which you stay oriented while they do the writing.

ATM replaces none of those tools — it is the hub beneath them. The same append-only design that lets replicas of the ledger converge through a shared folder or a git remote lets adapters mirror it out to a Jira board, a Notion page, a doc your manager actually reads — and contribute knowledge back in to enrich them. Those surfaces stay what they are good at: views for humans. The ledger stays what they never were: the memory.

## Screenshots

![ATM dashboard showing projects, tasks, labels, activity, and vocabulary](docs/assets/screenshots/atm-dashboard.png)

Dashboard view with project-level activity, task lists, label vocabulary, and recent work density.

![ATM persona activity overview](docs/assets/screenshots/atm-persona-activity.png)

Persona activity overview for seeing how developer, manager, and admin work is distributed.

![ATM persona activity drilldown](docs/assets/screenshots/atm-persona-drilldown.png)

Persona drilldown with agent, model, and action breakdowns.

## Store

Each project stores its truth as an append-only event log — portable, mergeable, and rebuildable. Sync is a deterministic set union of event sets; any topology converges, and every failure's recovery is "run sync again."

```text
$ATM_HOME/
  store.json
  cache.db                 # derived SQLite projection — rebuildable from events
  projects/<CODE>/
    events.v2.jsonl        # source of truth: one event per line, append-only
    config.json            # per-project settings
```

### Remotes and Sync

Add a filesystem remote first — a directory on a shared drive or a sync folder:

```sh
mkdir -p ~/Sync/atm
atm store remote add origin ~/Sync/atm --project ATM
# Git remotes also work:
atm store remote add origin git@github.com:you/atm-ledger.git --project ATM
```

Sync in either direction; the default is bidirectional:

```sh
atm store sync                            # every project with a remote
atm store sync --project ATM --pull       # pull-only
atm store sync --project ATM --dry-run    # preview differences
```

Bootstrapping a project from its canonical remote is a one-liner: `atm store sync <url> --project <CODE>` fetches, creates it locally, and persists the URL as `origin`. A git remote needs at least one commit to exist first — seed it with `git commit --allow-empty`.

### Legacy v1 Import

v2 is the only current format. A pre-v2 `log.jsonl` can be imported:

```sh
atm store upgrade --project <CODE>
atm store verify
atm store prune-v1 --project <CODE>
```

Upgrade is side-by-side: the original `log.jsonl` is never touched during import, and `prune-v1` archives it by default (or `--delete`s it). There is no rollback.

## Build And Verify

```sh
make build
make test
make verify
```

## Advanced Features

These features are optional after the 30-second start. They are useful when you want tighter control over vocabulary, semantic search, or agent roles.

### Labels And Boards

**Labels** are the substrate everything builds on — free-form, namespaced names (`status:open`, `priority:high`, `context:agent`) with no fixed workflow fields. Each project grows its own vocabulary, visible with `atm label list --project <CODE>`. Labels come in three kinds:

- **Stored labels** — directly assigned to tasks (`ATM:type:bug`, `ATM:component:cli`).
- **Namespace labels** — emergent from task use; a wildcard like `ATM:status:*` matches any task carrying a `status:*` label, and the TUI groups by namespace for faceted browsing.
- **Boards** — computed labels whose membership is defined by a boolean expression over other labels, not asserted task-by-task.

A **board** is authored with `--expr`:

```sh
atm label add --project ATM \
  --name ATM:next-sprint \
  --description "open work slated for the next sprint" \
  --expr "status:open AND sprint:next"
```

A board name is a valid `--label` value, so listing its members reads like any other query:

```sh
atm task list --project ATM --label ATM:next-sprint
```

**Capabilities build on the label substrate.** Two built-in capabilities ship with ATM and mount their own boards, verbs, and vocabulary:

| Capability | Verbs | Namespaces | Seeded Boards |
|-----------|-------|------------|---------------|
| `workflow` | `start`, `open`, `block`, `complete`, `status` | `status:*`, `priority:*` | `backlog`, `open-tasks`, `in-progress-tasks`, `all-tasks` |
| `contextmap` | `add`, `stamp`, `retarget`, `supersede`, `check` | `context:*`, `knowledge:*` | `context-current` |

Enable capabilities per project and scope manager actions to one:

```sh
atm project capability add workflow --project ATM
atm --persona manager --project ATM --mode autopilot --capability workflow
```

Each capability ships a self-contained agent guide — read it to understand its semantics, actions, and converged state:

```sh
atm capability workflow guide
atm capability contextmap guide
atm capability list                     # summaries for every registered capability
```

The Boards pane in the TUI is the human's review surface for boards and namespaces, with a pinned-board strip, per-namespace drilldown, and live board-editor feedback.

### Semantic Search And Indexing

Semantic search needs an embedding endpoint and a vector index.

**1. Make the embedding model available.** ATM speaks any OpenAI-compatible `/v1/embeddings` endpoint. The common case is a local Ollama daemon — install Ollama, then pull an embed model before configuring ATM, or the indexer will fail with `model "..." not found`:

```sh
ollama pull nomic-embed-text        # default Nomic preset; 768-dim
# alternates, if nomic-embed-text is unavailable on your Ollama:
# ollama pull bge-m3                # 1024-dim
# ollama pull mxbai-embed-large     # 1024-dim
```

Hosted OpenAI-compatible providers (OpenAI, LocalAI, vLLM, etc.) need no pull step — just an API key and a reachable `/v1/embeddings` URL.

**2. Configure the embedding model.** Point ATM at the endpoint you just stood up:

```sh
atm project set-embedding --project ATM \
  --model nomic-embed-text \
  --endpoint http://localhost:11434/v1 \
  --dim 768 \
  --threshold 0.55
```

Match `--model` and `--dim` to the model you pulled. A 404 `model "..." not found` from the embed step means the named model is not present at the endpoint — pull it (Ollama) or fix the model name/provider.

**3. Build and inspect the index from the CLI.**

```sh
atm index reindex --project ATM      # one-shot index pass
atm index status --project ATM       # staleness per indexed model
atm index models --project ATM       # models with stored vectors
atm search --project ATM "query"     # semantic search with text fallback

atm index --project ATM              # continuous foreground indexing until Ctrl-C
```

**4. Or manage indexing from the TUI.** Run `atm`, then press `g 1` to open the indexer overlay: `e` edits embedding config (`p` fills the Nomic preset, `s` saves), `S` starts or stops the live indexer, `r` runs a one-shot reindex, `d` drops the selected model index.

### Personas And Agent Defaults

Personas shape the role prompt and actor identity used in `atm --persona <name> --project <CODE>`. ATM ships three built-in personas: `developer` (the default developer persona), `manager` (the default manager persona), and `admin` (human-driven CLI/TUI actions), plus `concierge` (plain-language onboarding, launchable without `--project`). Built-ins ship inside the binary from the top-level `skills/` folder and are no longer seeded into the store; inspect one with `atm persona show <name>` and customize it with `atm persona personality <name>`.

Create a custom persona when you want a recurring working style, and use it for one session with `--persona`:

```sh
atm persona create \
  --name reviewer \
  --description "reviews implementation quality before handoff" \
  --prompt-file ./prompts/reviewer.md

atm --persona developer --project ATM --persona reviewer
```

`atm init` records your default agent separately from personas. Use `atm agents` to inspect readiness, change the default host, or save default host-agent args; for one-off launches, override with `--agent` and pass host-agent args after `--`:

```sh
atm agents list
atm agents select claude
atm agents args claude -- --dangerously-skip-permission

atm --persona developer --project ATM --agent codex -- --yolo
```

### Dispatching Sessions From The TUI

The TUI can spawn manager and developer sessions into a separate terminal
surface (herdr pane → tmux window → new terminal tab, auto-detected in that
order). From the projects pane, `D` dispatches a **manager** session for the
selected project; from the tasks pane, `D` dispatches a **developer** session
bound to the selected task row. The only interactive field is the host agent
(cycle with `←/→`, dispatch with `Enter`); an unready agent is refused with its
missing-bin hint. `V` opens a read-only **personas** browser (list built-ins
and customs, `Enter` views a persona's effective prompt, `Esc` backs out).

A developer session can equally be handed a task from the shell with the new
`--task <id>` flag — it is validated against `--project`'s store, exported to
the host as `ATM_TASK=<id>`, and rendered into the session context as an
assigned-task block (so it works identically for `launch: hook` and
`launch: prompt` personas, and task-keyed context caches prevent concurrent
task sessions from sharing a context file):

```sh
atm --persona developer --project ATM --agent claude --task ATM-4b7e24
```

When neither herdr nor tmux is present, the terminal fallback opens a new tab
in a known emulator (kitty, wezterm, gnome-terminal, konsole, alacritty,
foot). Override it by hand-editing `dispatch.json` at the store root
(sibling of `agents.json`) with a `terminal_cmd` template run via `sh -c` and
`{cmd}` (shell-quoted argv) / `{dir}` / `{title}` placeholders:

```json
{ "terminal_cmd": "kitty @ launch --type=tab --cwd {dir} --tab-title {title} -- {cmd}" }
```

### Lower-Level API

The lower-level task, label, project, store, search, index, persona, and activity commands remain available for agents and scripts. Discover them with:

```sh
atm help
atm conventions
```
