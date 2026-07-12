# ATM — Agent Tasks Management

ATM is an append-only task ledger for people who work through coding agents.

## 30-Second Start

```sh
atm init
atm
atm dev --project ATM
atm manage --project ATM --planning
```

`atm init` initializes the store, guides you through installing ATM plugins for
one or more agents, and selects the first as your default. `atm dev` / `atm
manage` then launch that selected agent — you do not name it each time. Agent
launchers create the project if it does not exist.

## User Actions

Open the TUI:

```sh
atm
```

Choose an agent (once):

```sh
atm agents list                 # supported agents + install/readiness
atm agents select opencode      # set the default for atm dev / atm manage
atm agents args codex -- --yolo # optional per-agent default passthrough
```

Supported agents are the built-ins `opencode`, `codex`, `claude`, and their
ollama-driven variants `ollama:opencode`, `ollama:codex`, `ollama:claude`.

Start a developer session (uses the selected agent):

```sh
atm dev --project ATM
atm dev --project ATM --agent claude   # override just this launch
```

Start a manager session:

```sh
atm manage --project ATM --planning
atm manage --project ATM --grooming
atm manage --project ATM --tracking
atm manage --project ATM --asking
atm manage --project ATM --glossary
atm manage --project ATM --onboarding
```

Both launchers accept `--persona <name>`, the `--agent <name>` override, and pass host-agent arguments after `--`:

```sh
atm dev --project ATM --persona developer -- --yolo
atm manage --project ATM --agent claude --planning --persona manager -- --dangerously-skip-permission
```

## Manager Actions

- `--planning` reviews open work and keeps statuses honest.
- `--grooming` prioritizes and shapes the backlog.
- `--tracking` curates progress, decisions, questions, and handoffs.
- `--asking` answers project questions from the ledger with cited task/comment IDs.
- `--glossary` maintains shared project language.
- `--onboarding` learns a repo/project and organizes it for later agents.

## Why I Built ATM

I work across multiple projects at once, and some projects span multiple repositories.

I use multiple coding agents and switch between them regularly to manage cost, context, and token usage.

I need to resume or hand off work across agents with minimal guidance.

I switch machines frequently, so I need a centralized, immutable, append-only ledger that can be shared.

I do not want a traditional Jira-style ticket system built around human browsing workflows. I want to ask my agents and have them work from the ledger.

## Store

ATM stores plain files under `ATM_HOME`, or `~/.config/atm` by default. A project is not the same thing as a repository; one project can cover multiple repos.

## Build And Verify

```sh
make build
make test
make verify
```

## Advanced/API Surface

The lower-level task, label, project, store, search, index, persona, and activity commands remain available for agents and scripts. Discover them with:

```sh
atm help
atm conventions
```
