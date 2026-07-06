---
name: atm-developing
description: Use when ATM_ROLE=developing or when working in an ATM-linked development session.
---

When `ATM_ROLE=developing`, use ATM as the visible work ledger for the session.

- Use `$ATM_BIN` or `atm` for commands.
- Use `$ATM_PROJECT` as the project code.
- Before feature, design, spec, bug, chore, or meaningful investigation work, find or create the relevant task.
- Record intentions, progress, decisions, test results, commit references, blockers, and handoff notes as task comments.
- Follow repo instructions, existing skills, harness rules, tool permissions, and user directions first. ATM records the work; it does not replace the workflow.

## Tracking work via the manager

To track work, dispatch the `atm-manager` subagent. The prompt is an
optional `hint: <word>` line (`feature`, `bug`, `design`, `spec`,
`chore`, `investigation`, `decision`, `progress`, `blocker`, `handoff`,
`question`) followed by a freeform message describing what you just did,
are about to do, decided, blocked on, or noticed. Note the reply and
continue. Do not branch on it. If the manager is unavailable, note the
track intent in your own context and continue; ledger hygiene is
best-effort.
