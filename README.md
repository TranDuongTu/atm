# ATM — Agent Tasks Manager

ATM is built for work shared between humans and coding agents across multiple projects and repositories. Humans jot down ideas as lightweight tasks; agents expand them, organize them with labels, link context, and keep the work discoverable over time.

ATM stays out of the normal development workflow: it hints agents to journal progress, decisions, and context without controlling how they code. The ledger is stored as plain files: detachable, syncable, shareable, database-free, and independent of any one coding agent.

## 30-Second Start

**1. Install** the `atm` binary:

```sh
curl -fsSL https://raw.githubusercontent.com/TranDuongTu/atm/main/scripts/install.sh | bash
```

**2. Onboard.** Initialize the store and onboard each project — one onboarding run per project:

```sh
atm init                       # guided setup: store, agent plugins, default agent
atm agents list                # see what's installed and ready
atm manage --project ATM --onboarding
```

**3. Daily work.** Open the dashboard to see everything, start dev sessions in repo directories, and run manager actions to keep the ledger groomed:

```sh
atm tui                        # dashboard: tasks, projects, labels, activity

atm dev --project ATM          # developer session (run inside the working repo)
atm dev --project ATM --agent claude

atm manage --project ATM --planning     # review open work, keep statuses honest
atm manage --project ATM --grooming     # prioritize and shape the backlog
atm manage --project ATM --tracking     # curate progress, decisions, handoffs
atm manage --project ATM --asking       # answer project questions from the ledger
atm manage --project ATM --glossary     # maintain shared project language
```

Both `atm dev` and `atm manage` accept `--persona <name>`, the `--agent <name>` override, and pass host-agent arguments after `--`:

```sh
atm dev --project ATM --persona developer -- --yolo
atm manage --project ATM --agent claude --planning --persona manager -- --dangerously-skip-permission
```

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
