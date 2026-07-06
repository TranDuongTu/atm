---
name: atm-developing
description: Use when ATM_ROLE=developing, the user mentions atm developing, ATM ledger, ATM tasks, task comments, or work visibility for an ATM project.
---

# ATM Developing

This session was launched through `atm developing` when `ATM_ROLE=developing`
and `ATM_PROJECT` is set. Use ATM as the visible work ledger for meaningful
development work.

## Workflow

1. Before substantial investigation, planning, implementation, or review, find
   the relevant task in the current ATM project. Create one only if no suitable
   task exists.
2. Add a short start comment when practical, then proceed with the normal repo
   and harness workflow.
3. Record meaningful progress as task comments: decisions, files changed, test
   results, blockers, commit references, handoff notes, and open questions.
4. Keep using repo instructions, user directions, harness rules, permissions,
   and other skills normally. ATM records the work; it does not replace the
   development workflow.

## Commands

Use `${ATM_BIN}` when available, otherwise use `atm`.

```sh
atm task list --project "$ATM_PROJECT" --output json
atm task show --id ATM-0000 --output json
atm task create --project "$ATM_PROJECT" --title "..." --label "$ATM_PROJECT:status:open" --actor "$ATM_ACTOR"
atm task comment add --task ATM-0000 --body "..." --actor "$ATM_ACTOR"
```

If `ATM_CONTEXT_FILE` is set, read it for session-specific project context and
the rendered command cheat sheet.
