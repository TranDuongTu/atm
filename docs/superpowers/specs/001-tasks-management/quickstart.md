# Quickstart: Tasks Management System

**Feature**: 001-tasks-management | **Date**: 2026-07-02 | **Revision**: v2.0.0

A walkthrough of the v2 CLI and TUI using a single shell session. Assumes
`atm` is on `$PATH` and `$ATM_HOME` is set to a scratch dir.

## Setup

```sh
export ATM_HOME=/tmp/atm-demo
rm -rf "$ATM_HOME"
```

## Scenario 1 - Create a project and curate labels

```sh
atm project create --code ATM --name "Agent Tasks Management" --actor human:alice
# expected: project ATM created

atm label add --name ATM:status:open --description "Open / not started" --actor human:alice
atm label add --name ATM:status:in-progress --description "Work in progress" --actor human:alice
atm label add --name ATM:status:review --description "Awaiting review" --actor human:alice
atm label add --name ATM:status:done --description "Completed" --actor human:alice
atm label add --name ATM:status:cancelled --description "Cancelled" --actor human:alice
atm label add --name ATM:type:epic --description "Large body of work" --actor human:alice
atm label add --name ATM:type:impl --description "Implementation task" --actor human:alice
atm label add --name ATM:type:bug --description "Bug fix" --actor human:alice
atm label add --name ATM:kind:convention --description "Convention doc" --actor human:alice
atm label add --name ATM:owner:alice --description "Owned by alice" --actor human:alice
atm label add --name ATM:owner:bob --description "Owned by bob" --actor human:alice
atm label add --name ATM:area:cli --description "CLI surface" --actor human:alice
atm label add --name ATM:area:tui --description "TUI surface" --actor human:alice
atm label add --name ATM:doc:architecture --description "Architecture notes" --actor human:alice

atm label list --project ATM
# expected: labels grouped by namespace (kind, owner, status, type, area, doc)
```

## Scenario 2 - Create tasks, label, and link

```sh
atm task create --project ATM --title "Add claim command" \
  --label ATM:type:impl --label ATM:area:cli --label ATM:owner:alice \
  --actor human:alice
# expected: ATM-0001, auto-labeled ATM:status:open

atm task create --project ATM --title "Fix locking bug" \
  --label ATM:type:bug --label ATM:area:cli --label ATM:owner:bob \
  --actor human:alice
# expected: ATM-0002, auto-labeled ATM:status:open

atm task link add --id ATM-0001 --type blocks --target ATM-0002 --actor human:alice
# ATM-0002 is now blocked by ATM-0001

atm task list --project ATM
# expected: ATM-0001 and ATM-0002, both status:open

atm task list --project ATM --group-by owner
# expected:
#   ATM:owner:alice (1)
#     ATM-0001 ...
#   ATM:owner:bob (1)
#     ATM-0002 ...
```

## Scenario 3 - Agent workflow: next, claim, show

```sh
atm next --project ATM --claim --actor agent:claude-1
# expected: ATM-0001 (ATM-0002 is blocked), claimed by agent:claude-1

atm next --project ATM
# expected: empty (ATM-0001 claimed, ATM-0002 blocked)

atm task show --id ATM-0001 --with-context
# expected: task + links_out + conventions (none yet) + timeline + guide (null)
```

## Scenario 4 - Convention docs and context discovery

```sh
atm task create --project ATM --title "Bug-fix conventions" \
  --label ATM:kind:convention --label ATM:type:bug \
  --actor human:alice
# expected: ATM-0003 (NOT returned by `next` because it has kind:convention)

atm task show --id ATM-0002 --with-context
# expected: context.conventions includes ATM-0003 (matched ATM:type:bug),
#           ranked by matched-label-count desc then ID asc
```

## Scenario 5 - Status as a label, free transitions

```sh
atm task set-status --id ATM-0001 --status in-progress --actor agent:claude-1
# ATM-0001 labels now include ATM:status:in-progress (replaced ATM:status:open)

atm task set-status --id ATM-0001 --status review --actor agent:claude-1
atm task set-status --id ATM-0001 --status open --actor agent:claude-1
# free transitions allowed: review -> open is fine (no state machine)
```

## Scenario 6 - Review flow

```sh
atm task set-status --id ATM-0002 --status review --actor agent:claude-1
atm review queue --project ATM
# expected: ATM-0002

atm review approve --id ATM-0002 --actor human:alice
# ATM-0002 status label -> ATM:status:done

atm review reject --id ATM-0001 --comment "Needs tests" --actor human:alice
# ATM-0001 status label -> ATM:status:open, discussion entry added
```

## Scenario 7 - Soft label removal

```sh
atm label remove --name ATM:area:cli --actor human:alice
# expected: retained_usage=2 (ATM-0001 and ATM-0002 still carry it)

atm task label add --id ATM-0001 --label ATM:area:cli --actor human:alice
# expected: rejected (label removed from registry)

atm task list --project ATM --label ATM:area:cli
# expected: ATM-0001 and ATM-0002 (legacy tasks still match)
```

## Scenario 8 - TUI

```sh
atm tui
```

- Tab 2 (Projects): select ATM, Enter. Labels pane shows labels grouped by
  namespace. `[L]` add label, `[l]` remove (toast shows `retained_usage`).
- Tab 1 (Tasks): select a task. Press `G` to group by axis; pick `owner` and
  tasks regroup under `ATM:owner:alice` / `ATM:owner:bob`. Pick `type` to
  regroup by type. Pick "none" to restore the flat list.
- Tab 3 (Dashboard): claimed tasks grouped by claimant, review queue, open
  followups.