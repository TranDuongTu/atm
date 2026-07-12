# ATM — Agent Tasks Management

ATM is an append-only task ledger for people who work through coding agents.

## 30-Second Start

```sh
atm init
atm
atm codex --project ATM
atm manage codex --project ATM --planning
```

`atm init` initializes the store and guides you through installing ATM plugins
for one or more agents. Agent launchers create the project if it does not exist.

## User Actions

Open the TUI:

```sh
atm
```

Start a developer agent:

```sh
atm codex --project ATM
atm claude --project ATM
atm opencode --project ATM
atm ollama --project ATM --integration codex
```

Start a manager session:

```sh
atm manage codex --project ATM --planning
atm manage codex --project ATM --grooming
atm manage codex --project ATM --tracking
atm manage codex --project ATM --asking
atm manage codex --project ATM --glossary
atm manage codex --project ATM --onboarding
```

All agent launchers accept `--persona <name>` and pass host-agent arguments after `--`:

```sh
atm codex --project ATM --persona developer -- --yolo
atm manage claude --project ATM --planning --persona manager -- --dangerously-skip-permission
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
