# ATM — Agent Tasks Manager

ATM is a fast, scalable, distributed task ledger — git-like in how it stores truth, Jira-like in how it tells the story — built as the main interface through which coding agents keep a software organization's knowledge base.

## 30-Second Start

**1. Install** the `atm` binary:

```sh
curl -fsSL https://raw.githubusercontent.com/TranDuongTu/atm/main/scripts/install.sh | bash
```

**2. Initialize, then map your repos.** Run the guided setup once — it initializes the store, installs the agent plugins, and records your default agent and args. Then create a project and let the manager map each working repo into it:

```sh
atm init                                # once: store, agent plugins, default agent + args
atm project create --code ATM --name "Agent Tasks Management"
atm manage --project ATM --mapping      # inside each working repo: manager-assisted mapping
```

Mapping reconciles the project's context map against the repo: the manager discovers the territory, records context pointers, and verifies drifted ones on later runs. Semantic indexing is optional — see [Advanced Features](#advanced-features).

**3. Daily work.** Open the dashboard to see everything, start dev sessions in repo directories, and run manager actions to keep the ledger groomed:

```sh
atm                            # dashboard: tasks, projects, labels, activity

atm dev --project ATM          # developer session (run inside the working repo)
atm dev --project ATM --agent claude

atm manage --project ATM                # curate the backlog (default action)
atm manage --project ATM --recall       # answer project questions from the ledger (read-only)
atm manage --project ATM --mapping      # reconcile the context map against the repo
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

Each project is a **distributed event source**: an append-only stream of events — task created, title changed, label added — that is the single source of truth. Event ids are content hashes, and every event carries a hybrid-logical-clock stamp and a replica id, so independent replicas of a project merge deterministically: a sync is an exact set union of the two event sets. Everything you query is a *derived* projection of that log — `cache.db` and the vector index rebuild from the events on demand, and deleting them never loses data.

```text
$ATM_HOME/
  store.json               # replica id + HLC clock, active format, per-project formats
  cache.db                 # derived SQLite projection — rebuildable, never the source of truth
  projects/<CODE>/
    events.v2.jsonl        # the source of truth: one event per line, append-only
    config.json            # per-project settings: embedding endpoint, sync remotes
    vectors/ vocabulary.json log.jsonl   # derived index, computed vocabulary, legacy v1 log (importable)
```

### Sync And Remotes

Remotes are named, per-project, and replica-local — a remote describes *this replica's* route to the world, exactly like `.git/config`. A remote is a passive medium: a directory (a NAS mount, a Syncthing or Dropbox folder, a USB stick) or a git URL; the transport is selected from the URL shape, and atm does no auth of its own — a directory remote works once it exists and is writable, a git remote works if `git` itself can already reach it with whatever's ambient (an SSH agent, a credential helper, `.netrc`).

```sh
mkdir -p ~/Sync/atm                                    # a directory remote must already exist
atm store remote add origin ~/Sync/atm --project ATM
atm store remote add origin git@github.com:you/atm-ledger.git --project ATM
atm store remote list --project ATM

atm store sync                            # every project with a remote, bidirectional
atm store sync --project ATM --pull       # restrict direction with --pull / --push
atm store sync <url> --project ATM        # ad-hoc remote — nothing persisted
atm store sync --project ATM --dry-run    # fetch, validate, report the differences; commit nothing
```

Because sync is set union — commutative, associative, idempotent — any topology converges, and every failure's recovery is "run sync again": an interrupted sync leaves both stores valid, and the next one completes the difference. Fetched events are validated before anything touches the local store (every hash recomputed, parents resolved, DAG acyclic, same project root on both sides), so a corrupt or mismatched remote aborts the whole sync with the local store untouched. The same holds for a lone push failure — a bidirectional sync that pulls fine but fails to push still commits the pull and reports the push error separately, so a failed push is never a special case: run `atm store sync` again once the remote is reachable.

There is no separate clone verb: `atm store sync <url> --project <CODE>` for a project you don't hold locally fetches it, creates it, and persists the URL as its `origin` — the second machine is one command.

A git remote needs at least one commit before its first sync — a freshly `git init --bare`d repo with nothing pushed to it has no `HEAD` for atm to clone against; seed it with one empty commit (`git commit --allow-empty`) first. Project logs live under a subpath within the remote — `.atm/<CODE>/events.v2.jsonl` by default, or a path of your choosing via a trailing `//<subpath>` on the URL (`git@host:repo.git//tickets`; use a `git::` prefix to force git recognition for a URL that wouldn't otherwise be recognized as one, e.g. `git::https://example.com/repo//tickets`). atm keeps a `.gitattributes` entry (`merge=union`) in that subpath up to date automatically, so the ledger file itself never conflicts even under a plain `git merge`.

Only a v2-active project can sync — `atm store sync` against a v1-active project (one still on the legacy `log.jsonl`, not yet imported) refuses with `project is v1-active and cannot sync; run "atm store upgrade" first`; run `atm store upgrade --project <CODE>` and sync again.

**Recovering from a folder-sync conflict.** A directory remote's event log is just a file that a folder-sync tool (Syncthing, Dropbox) also watches, and if two machines publish to it in the same window, the tool — not atm — may fork the shared file into a second copy (e.g. `events.v2.jsonl.sync-conflict-...`) rather than lose either side. Because sync is a lossless set union and every line re-parses to its own event id regardless of order, recovery only means restoring one file on the remote: concatenate the conflict copy's lines onto the end of the real `events.v2.jsonl` (`cat events.v2.jsonl.conflict >> events.v2.jsonl` inside the remote's `<CODE>/` directory, then delete the copy) and sync from any replica — the next `atm store sync` pulls the full union, repeated lines collapse back to one event each, and nothing is lost.

### Importing A Legacy v1 Log

v2 is the only storage format — every new project is born on it. A stray pre-v2 `log.jsonl` (from before v2 existed) can be imported:

```sh
atm store upgrade --project <CODE>   # or --all: build events.v2.jsonl from the legacy log
atm store verify
atm store prune-v1 --project <CODE>  # after verifying, retire the leftover log (--delete to remove)
```

Upgrade builds each project's `events.v2.jsonl` from its `log.jsonl`, verifies the two agree, and cuts over; the log is left untouched during import, so a failed import changes nothing. Existing ids are kept (`ATM-0001` stays `ATM-0001`); new tasks and comments get hash ids like `ATM-9f3c1a`. Once verified, `atm store prune-v1` archives the leftover log (or `--delete`s it). There is no rollback.

## Build And Verify

```sh
make build
make test
make verify
```

## Advanced Features

These features are optional after the 30-second start. They are useful when you want tighter control over vocabulary, semantic search, or agent roles.

### Labels And Boards

Labels are the substrate: free-form, namespaced names (`status:open`, `type:bug`, `sprint:next`) with no fixed workflow fields — each project grows its own vocabulary, and `atm label list --project <CODE>` shows the live one.

A **board** is a label whose membership is computed by an expression over other labels, not asserted task-by-task. Author one with `atm label add --expr`:

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

The Boards pane in the TUI is the human's review surface for boards and namespaces.

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

Personas shape the role prompt and actor identity used in `atm dev` and `atm manage`. ATM seeds three built-in personas: `developer` (default for `atm dev`), `manager` (default for `atm manage`), and `admin` (human-driven CLI/TUI actions).

Create a custom persona when you want a recurring working style, and use it for one session with `--persona`:

```sh
atm persona create \
  --name reviewer \
  --description "reviews implementation quality before handoff" \
  --prompt-file ./prompts/reviewer.md

atm dev --project ATM --persona reviewer
```

`atm init` records your default agent separately from personas. Use `atm agents` to inspect readiness, change the default host, or save default host-agent args; for one-off launches, override with `--agent` and pass host-agent args after `--`:

```sh
atm agents list
atm agents select claude
atm agents args claude -- --dangerously-skip-permission

atm dev --project ATM --agent codex -- --yolo
```

### Lower-Level API

The lower-level task, label, project, store, search, index, persona, and activity commands remain available for agents and scripts. Discover them with:

```sh
atm help
atm conventions
```
