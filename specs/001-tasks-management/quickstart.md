# Quickstart: Tasks Management System

**Feature**: 001-tasks-management | **Date**: 2026-06-23

Runnable validation scenarios that prove the feature works end-to-end. These mirror the spec's acceptance scenarios and the CLI contract in `contracts/cli.md`. Prerequisite: the `atm` binary is built and on `$PATH`.

## Prerequisites

```sh
# from repo root
go build -o /tmp/atm ./cmd/atm      # once implemented
export PATH="/tmp:$PATH"
```

## Scenario 1 - Agent queries next task and claims it (US1)

```sh
atm init --store /tmp/atm-store --actor human:alice

atm project create --code ATM --name "Agent Tasks Management" \
  --label type:epic --label type:user-story --label type:impl --label type:bug \
  --label area:cli --label area:tui --label kind:convention \
  --type-axis type --actor human:alice --store /tmp/atm-store

# seed a convention doc that applies to bugs
atm task create --project ATM --title "PR conventions for bug fixes" \
  --label kind:convention --label type:bug --actor human:alice --store /tmp/atm-store
# -> ATM-0001

atm task create --project ATM --title "Fix claim race" --label type:bug --label area:cli \
  --actor human:alice --store /tmp/atm-store
# -> ATM-0002

atm task create --project ATM --title "Blocked subtask" --label type:impl --actor human:alice --store /tmp/atm-store
# -> ATM-0003
atm task link add --id ATM-0002 --type blocks --target ATM-0003 --actor human:alice --store /tmp/atm-store
# ATM-0003 is now blocked by ATM-0002

# agent asks for the next claimable task (should skip the blocked ATM-0003)
atm task next --project ATM --output json --store /tmp/atm-store
# expected: ATM-0002 (bug, claimable, not blocked)

# agent claims it atomically
atm task next --project ATM --claim --actor agent:claude-1 --output json --store /tmp/atm-store
# expected: ATM-0002 with claim.actor = agent:claude-1

# context retrieval surfaces the matching convention doc ATM-0001
atm task show --id ATM-0002 --with-context --output json --store /tmp/atm-store
# expected: context.conventions contains ATM-0001 (matched type:bug)
```

**Expected outcome**: `next` returns the unblocked, claimable bug; `--claim` marks it claimed; `show --with-context` lists the matching convention doc. Verify by inspecting the JSON.

## Scenario 2 - Human manages projects and labels (US2)

```sh
atm project create --code DEMO --name "Demo" --label type:impl --label area:cli --actor human:alice --store /tmp/atm-store
atm project label add --code DEMO --label type:bug --description "Bug fix" --actor human:alice --store /tmp/atm-store
atm project set-type-axis --code DEMO --namespace type --actor human:alice --store /tmp/atm-store

atm project label list --code DEMO --output json --store /tmp/atm-store
# expected: labels include type:impl, area:cli, type:bug; type_axis == "type"

# soft removal: tasks keep the label, new assignments reject it
atm task create --project DEMO --title "Old task" --label area:cli --actor human:alice --store /tmp/atm-store
atm project label remove --code DEMO --label area:cli --actor human:alice --store /tmp/atm-store
# next create with area:cli should fail
atm task create --project DEMO --title "New task" --label area:cli --actor human:alice --store /tmp/atm-store
# expected: exit code 2 (usage error) / error message about removed label
```

**Expected outcome**: labels list reflects add/remove; soft removal lets the existing task keep `area:cli` while the new task creation is rejected.

## Scenario 3 - Links and hierarchy (US3)

```sh
atm task create --project ATM --title "Epic: agent workflow" --label type:epic --actor human:alice --store /tmp/atm-store
# -> ATM-0004 (epic)
atm task create --project ATM --title "Impl: claim command" --label type:impl --actor human:alice --store /tmp/atm-store
# -> ATM-0005
atm task link add --id ATM-0005 --type implements --target ATM-0004 --actor human:alice --store /tmp/atm-store

atm task link list --id ATM-0004 --output json --store /tmp/atm-store
# expected: links_in contains ATM-0005 with type implements, direction in
atm task link list --id ATM-0005 --output json --store /tmp/atm-store
# expected: links_out contains ATM-0004 with type implements, direction out
```

**Expected outcome**: the `implements` link is traversable both ways; querying the epic returns its implementation tasks.

## Scenario 4 - Todos, followups, discussions (US4)

```sh
atm task todo add --id ATM-0002 --text "Write tests for claim" --actor agent:claude-1 --store /tmp/atm-store
atm task followup add --id ATM-0002 --text "Decide storage format" --assignee human:alice --actor human:alice --store /tmp/atm-store
atm task discussion add --id ATM-0002 --text "Use file-level locking." --actor human:alice --store /tmp/atm-store

atm task timeline --id ATM-0002 --output json --store /tmp/atm-store
# expected: entries sorted by timestamp; kinds include history, todo, followup, discussion

atm task followup resolve --id ATM-0002 --followup f1 --actor human:alice --store /tmp/atm-store
atm task timeline --id ATM-0002 --output json --store /tmp/atm-store
# expected: followup f1.status == resolved, with resolved_at and resolved_by
```

**Expected outcome**: the timeline merges all entry kinds chronologically; resolving a followup updates its status and records who/when.

## Scenario 5 - Human coordinator review (US5)

```sh
atm task claim --id ATM-0005 --actor agent:claude-1 --store /tmp/atm-store
atm review request --id ATM-0005 --actor agent:claude-1 --store /tmp/atm-store   # status -> review
atm review queue --output json --store /tmp/atm-store
# expected: groups[].claimant == agent:claude-1, tasks include ATM-0005

atm review approve --id ATM-0005 --comment "Looks good" --actor human:alice --store /tmp/atm-store
atm task show --id ATM-0005 --output json --store /tmp/atm-store
# expected: status == done; history has an "approved" entry by human:alice
```

**Expected outcome**: the review queue groups tasks by claimant; approving moves the task to `done` and records the approver.

## Determinism check (SC-002a)

```sh
atm task list --project ATM --output json --store /tmp/atm-store > /tmp/a.json
atm task list --project ATM --output json --store /tmp/atm-store > /tmp/b.json
diff /tmp/a.json /tmp/b.json   # expected: no diff
```

**Expected outcome**: byte-identical output across runs for the same store and arguments.