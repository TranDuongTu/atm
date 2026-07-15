# ATM — Agent Tasks Manager

ATM is built for work shared between humans and coding agents across multiple projects and repositories. Humans jot down ideas as lightweight tasks; agents expand them, organize them with labels, link context, and keep the work discoverable over time.

ATM stays out of the normal development workflow: it hints agents to journal progress, decisions, and context without controlling how they code. The ledger is stored as plain files: detachable, syncable, shareable, database-free, and independent of any one coding agent.

## 30-Second Start

**1. Install** the `atm` binary:

```sh
curl -fsSL https://raw.githubusercontent.com/TranDuongTu/atm/main/scripts/install.sh | bash
```

**2. Onboard.** Run the guided setup once, then onboard each project -- one manager onboarding run per project:

```sh
atm init                               # guided setup: store, plugins, default agent, args
atm manage --project ATM --onboarding  # Run inside the working repo
```

**3. Daily work.** Open the dashboard to see everything, start dev sessions in repo directories, and run manager actions to keep the ledger groomed:

```sh
atm                            # dashboard: tasks, projects, labels, activity

atm dev --project ATM          # developer session (run inside the working repo)
atm dev --project ATM --agent claude

atm manage --project ATM                # curate the backlog (default action)
atm manage --project ATM --recall       # answer project questions from the ledger (read-only)

atm manage --project ATM --mapping      # reconcile the context map against the repo
```

## Why I Built ATM

I work across multiple projects at once, and some projects span multiple repositories.

I use multiple coding agents and switch between them regularly to manage cost, context, and token usage.

I need to resume or hand off work across agents with minimal guidance.

I switch machines frequently, so I need a centralized, immutable, append-only ledger that can be shared.

I do not want a traditional Jira-style ticket system built around human browsing workflows. I want to ask my agents and have them work from the ledger.

I like to be creative and keep ideas flowing into the backlog anytime, anywhere, and later have the manager groom and plan them for me.

## Screenshots

![ATM dashboard showing projects, tasks, labels, activity, and vocabulary](docs/assets/screenshots/atm-dashboard.png)

Dashboard view with project-level activity, task lists, label vocabulary, and recent work density.

![ATM persona activity overview](docs/assets/screenshots/atm-persona-activity.png)

Persona activity overview for seeing how developer, manager, and admin work is distributed.

![ATM persona activity drilldown](docs/assets/screenshots/atm-persona-drilldown.png)

Persona drilldown with agent, model, and action breakdowns.

## Store

ATM stores plain files under `ATM_HOME`, or `~/.config/atm` by default. A project is not the same thing as a repository; one project can cover multiple repos.

### Upgrade To EventSource v2

v2 makes each project's append-only `projects/<CODE>/events.v2.jsonl` the source of truth. Upgrade writes it next to the existing `log.jsonl`, verifies it against a replay of that log, and rebuilds `cache.db` before cutting the project over. The v1 log is preserved untouched, so a failed or aborted upgrade leaves the project exactly as it was.

```sh
atm store upgrade --all      # upgrade every project; new projects are then born on v2 too
atm store verify             # confirm every project reads back cleanly
```

Use `atm store upgrade --project <CODE>` to upgrade a single project without changing the store default. Upgrade clears the project's vector index, so the next `atm index` pass re-embeds.

After cutover a project keeps every id it already had — `ATM-0001` stays `ATM-0001` — while new tasks and comments get hash ids like `ATM-9f3c1a`, accepted everywhere the old ids are. `next_task_n` no longer applies (shown as `-`), and lists follow event-creation order.

## Build And Verify

```sh
make build
make test
make verify
```

## Advanced Features And API

These features are optional after the 30-second start. They are useful when you want tighter control over agents, semantic search, or scripting.

### Personas And Agent Defaults

Personas shape the role prompt and actor identity used in `atm dev` and `atm manage`. ATM seeds three built-in personas:

- `developer` — default for `atm dev`
- `manager` — default for `atm manage`
- `admin` — human-driven CLI/TUI actions

Create a custom persona when you want a recurring working style:

```sh
atm persona create \
  --name reviewer \
  --description "reviews implementation quality before handoff" \
  --prompt-file ./prompts/reviewer.md

atm persona list
atm persona show --name reviewer
```

Use a persona for one session with `--persona`:

```sh
atm dev --project ATM --persona reviewer
atm manage --project ATM --curate --persona manager
```

`atm init` records your default agent separately from personas. Use `atm agents` when you want to inspect readiness, change the default host, or save default host-agent args:

```sh
atm agents list
atm agents select claude
atm agents args claude -- --dangerously-skip-permission
```

For one-off launches, override the host with `--agent` and pass host-agent args after `--`:

```sh
atm dev --project ATM --agent codex -- --yolo
atm manage --project ATM --agent claude --curate -- --dangerously-skip-permission
```

### Semantic Search And Indexing

Semantic search needs an embedding endpoint and a vector index.

**1. Configure the embedding model.** Use any OpenAI-compatible `/v1/embeddings` endpoint:

```sh
atm project set-embedding --project ATM \
  --model nomic-embed-text \
  --endpoint http://localhost:11434/v1 \
  --dim 768 \
  --threshold 0.55
```

**2. Build and inspect the index from the CLI.**

```sh
atm index reindex --project ATM      # one-shot index pass
atm index status --project ATM       # staleness per indexed model
atm index models --project ATM       # models with stored vectors
atm search --project ATM "query"     # semantic search with text fallback
```

For continuous foreground indexing, run:

```sh
atm index --project ATM              # watches the project log until Ctrl-C
```

**3. Manage indexing from the TUI.** Run `atm`, then press `g 1` to open the indexer overlay.

Inside the overlay:

- `e` edits embedding config.
- `p` fills the Nomic preset while editing.
- `s` saves config while editing.
- `S` starts or stops the live indexer.
- `r` runs a one-shot reindex.
- `d` drops the selected model index.

### Boards (Computed Labels)

A board is a label whose membership is computed by an expression over other labels, not asserted task-by-task. Author one with `atm label add --expr`:

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

### Lower-Level API

The lower-level task, label, project, store, search, index, persona, and activity commands remain available for agents and scripts. Discover them with:

```sh
atm help
atm conventions
```
