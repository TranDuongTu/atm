# ATM — Agent Tasks Management

ATM is an append-only task ledger for people who work through coding agents.

## 30-Second Start

**1. Install** the `atm` binary:

```sh
curl -fsSL https://raw.githubusercontent.com/TranDuongTu/atm/main/scripts/install.sh | bash
```

**2. Onboard.** `atm init` initializes the store, guides you through installing ATM plugins for one or more agents, and selects the first as your default agent:

```sh
atm init
```

**3. Pick your agent** (once — reuse or change any time). The supported agents are the built-ins `opencode`, `codex`, `claude`, and their ollama-driven variants `ollama:opencode`, `ollama:codex`, `ollama:claude`:

```sh
atm agents list                 # supported agents + install/readiness
atm agents select opencode      # set the default for atm dev / atm manage
atm agents args codex -- --yolo # optional per-agent default passthrough
```

**4. Work.** Open the dashboard, or launch a session with your selected agent — you never name the agent each time, and launchers create the project if it does not exist:

```sh
atm                                       # dashboard TUI
atm dev --project ATM                     # developer session
atm dev --project ATM --agent claude      # override the agent for one launch
atm manage --project ATM --planning       # manager session (see actions below)
```

Both `atm dev` and `atm manage` accept `--persona <name>`, the `--agent <name>` override, and pass host-agent arguments after `--`:

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

I like to be creative and keep ideas flowing into the backlog anytime, anywhere, and later have the manager groom and plan them for me.

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
