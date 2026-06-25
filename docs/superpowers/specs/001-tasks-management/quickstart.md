# Quickstart: Tasks Management System

**Feature**: 001-tasks-management | **Date**: 2026-06-23 | **Spec revision**: v1.1.0

Runnable validation scenarios that prove the feature works end-to-end. These mirror the spec's acceptance scenarios and the CLI contract in `contracts/cli.md`. Prerequisite: the `atm` binary is built and on `$PATH`.

All scenarios use an explicit `--store` pointing at a temp directory so they are self-contained and reproducible. In normal use the store defaults to `~/.config/atm` (see `contracts/cli.md` store resolution).

## Prerequisites

Build via the Makefile; artifacts go to the gitignored `bin/` directory.

```sh
# from repo root
make build                 # -> bin/atm
export PATH="$(pwd)/bin:$PATH"
export ATM_HOME=/tmp/atm-home       # used by scenarios below
rm -rf "$ATM_HOME" && mkdir -p "$ATM_HOME"
```

Other useful targets: `make test`, `make verify` (build + test, the AGENTS.md verify step), `make lint`, `make clean`.

All management capabilities below are available both via the CLI **and** the TUI (`atm tui`). The TUI mirrors every command; see `tui-mockups.md` for screens/keymaps and `contracts/tui.md` for the parity matrix. TUI validation scenarios are at the end of this file.

## Scenario 1 - Agent queries next task and claims it (US1)

```sh
atm init --store "$ATM_HOME" --actor human:alice

atm project create --code ATM --name "Agent Tasks Management" \
  --label type:epic --label type:user-story --label type:impl --label type:bug \
  --label area:cli --label area:tui --label kind:convention \
  --type-axis type --actor human:alice --store "$ATM_HOME"

# seed a convention doc that applies to bugs
atm task create --project ATM --title "PR conventions for bug fixes" \
  --label kind:convention --label type:bug --actor human:alice --store "$ATM_HOME"
# -> ATM-0001

atm task create --project ATM --title "Fix claim race" --label type:bug --label area:cli \
  --actor human:alice --store "$ATM_HOME"
# -> ATM-0002

atm task create --project ATM --title "Blocked subtask" --label type:impl --actor human:alice --store "$ATM_HOME"
# -> ATM-0003
atm task link add --id ATM-0002 --type blocks --target ATM-0003 --actor human:alice --store "$ATM_HOME"
# ATM-0003 is now blocked by ATM-0002

# agent asks for the next claimable task (should skip the blocked ATM-0003)
atm task next --project ATM --output json --store "$ATM_HOME"
# expected: ATM-0002 (bug, claimable, not blocked)

# agent claims it atomically
atm task next --project ATM --claim --actor agent:claude-1 --output json --store "$ATM_HOME"
# expected: ATM-0002 with claim.actor = agent:claude-1

# context retrieval surfaces the matching convention doc ATM-0001
atm task show --id ATM-0002 --with-context --output json --store "$ATM_HOME"
# expected: context.conventions contains ATM-0001 (matched type:bug)
```

**Expected outcome**: `next` returns the unblocked, claimable bug; `--claim` marks it claimed; `show --with-context` lists the matching convention doc. Verify by inspecting the JSON.

## Scenario 2 - Human manages projects and labels (US2)

```sh
atm project create --code DEMO --name "Demo" --label type:impl --label area:cli --actor human:alice --store "$ATM_HOME"
atm project label add --code DEMO --label type:bug --description "Bug fix" --actor human:alice --store "$ATM_HOME"
atm project set-type-axis --code DEMO --namespace type --actor human:alice --store "$ATM_HOME"

atm project label list --code DEMO --output json --store "$ATM_HOME"
# expected: labels include type:impl, area:cli, type:bug; type_axis == "type"

# soft removal: tasks keep the label, new assignments reject it
atm task create --project DEMO --title "Old task" --label area:cli --actor human:alice --store "$ATM_HOME"
atm project label remove --code DEMO --label area:cli --actor human:alice --store "$ATM_HOME"
# next create with area:cli should fail
atm task create --project DEMO --title "New task" --label area:cli --actor human:alice --store "$ATM_HOME"
# expected: exit code 2 (usage error) / error message about removed label
```

**Expected outcome**: labels list reflects add/remove; soft removal lets the existing task keep `area:cli` while the new task creation is rejected.

## Scenario 3 - Links and hierarchy (US3)

```sh
atm task create --project ATM --title "Epic: agent workflow" --label type:epic --actor human:alice --store "$ATM_HOME"
# -> ATM-0004 (epic)
atm task create --project ATM --title "Impl: claim command" --label type:impl --actor human:alice --store "$ATM_HOME"
# -> ATM-0005
atm task link add --id ATM-0005 --type implements --target ATM-0004 --actor human:alice --store "$ATM_HOME"

atm task link list --id ATM-0004 --output json --store "$ATM_HOME"
# expected: links_in contains ATM-0005 with type implements, direction in
atm task link list --id ATM-0005 --output json --store "$ATM_HOME"
# expected: links_out contains ATM-0004 with type implements, direction out
```

**Expected outcome**: the `implements` link is traversable both ways; querying the epic returns its implementation tasks.

## Scenario 4 - Todos, followups, discussions (US4)

```sh
atm task todo add --id ATM-0002 --text "Write tests for claim" --actor agent:claude-1 --store "$ATM_HOME"
atm task followup add --id ATM-0002 --text "Decide storage format" --assignee human:alice --actor human:alice --store "$ATM_HOME"
atm task discussion add --id ATM-0002 --text "Use file-level locking." --actor human:alice --store "$ATM_HOME"

atm task timeline --id ATM-0002 --output json --store "$ATM_HOME"
# expected: entries sorted by timestamp; kinds include history, todo, followup, discussion

atm task followup resolve --id ATM-0002 --followup f1 --actor human:alice --store "$ATM_HOME"
atm task timeline --id ATM-0002 --output json --store "$ATM_HOME"
# expected: followup f1.status == resolved, with resolved_at and resolved_by
```

**Expected outcome**: the timeline merges all entry kinds chronologically; resolving a followup updates its status and records who/when.

## Scenario 5 - Human coordinator review (US5)

```sh
atm task claim --id ATM-0005 --actor agent:claude-1 --store "$ATM_HOME"
atm review request --id ATM-0005 --actor agent:claude-1 --store "$ATM_HOME"   # status -> review
atm review queue --output json --store "$ATM_HOME"
# expected: groups[].claimant == agent:claude-1, tasks include ATM-0005

atm review approve --id ATM-0005 --comment "Looks good" --actor human:alice --store "$ATM_HOME"
atm task show --id ATM-0005 --output json --store "$ATM_HOME"
# expected: status == done; history has an "approved" entry by human:alice
```

**Expected outcome**: the review queue groups tasks by claimant; approving moves the task to `done` and records the approver.

## Scenario 6 - Project guide: always-read harness and dashboard (FR-016/017/018) *(new in v1.1.0)*

```sh
# ATM-0001 is already a convention doc (kind:convention, type:bug) from Scenario 1.
# Add it to the guide under a "conventions" section, plus a testing convention task.
atm task create --project ATM --title "Testing conventions" --label kind:convention --label area:cli \
  --actor human:alice --store "$ATM_HOME"
# -> ATM-0006

atm project guide section add --code ATM --name conventions --actor human:alice --store "$ATM_HOME"
atm project guide section add --code ATM --name testing    --actor human:alice --store "$ATM_HOME"
atm project guide ref add --code ATM --section conventions --kind task --target ATM-0001 --actor human:alice --store "$ATM_HOME"
atm project guide ref add --code ATM --section testing    --kind task --target ATM-0006 --actor human:alice --store "$ATM_HOME"
atm project guide set-freshness --code ATM --threshold 720h --actor human:alice --store "$ATM_HOME"

atm project guide show --code ATM --output json --store "$ATM_HOME"
# expected: sections == [conventions(1 ref), testing(1 ref)]; updated_at/updated_by set

# The guide is returned in next/show context (FR-017)
atm task next --project ATM --output json --store "$ATM_HOME"
# expected: response includes "guide" alongside "task"

# Coverage + freshness on the coordinator dashboard (FR-018)
atm review dashboard --project ATM --output json --store "$ATM_HOME"
# expected: guide_status.coverage.total_sections == 2; freshness lists ATM-0001 and ATM-0006 with state fresh|stale|unknown
```

**Expected outcome**: the guide is editable via CLI, returned in `next`/`show --with-context`, and the dashboard reports coverage and freshness per the configured threshold.

## Determinism check (SC-002a)

```sh
atm task list --project ATM --output json --store "$ATM_HOME" > /tmp/a.json
atm task list --project ATM --output json --store "$ATM_HOME" > /tmp/b.json
diff /tmp/a.json /tmp/b.json   # expected: no diff
```

**Expected outcome**: byte-identical output across runs for the same store and arguments.

## Detachability check (SC-004, FR-001)

```sh
cp -r "$ATM_HOME" /tmp/atm-home-copy
atm task list --project ATM --output json --store /tmp/atm-home-copy > /tmp/c.json
atm task list --project ATM --output json --store "$ATM_HOME"      > /tmp/d.json
diff /tmp/c.json /tmp/d.json   # expected: no diff
```

**Expected outcome**: copying the store wholesale to another location reproduces the same state and the same output.

## TUI scenarios (parity with the CLI)

These are manual validation scenarios for `atm tui` confirming the TUI mirrors every CLI command group per `contracts/tui.md`. Each scenario assumes the corresponding CLI scenario above has already seeded the store at `$ATM_HOME`, then drives the same operations through the TUI and verifies the on-screen data matches the CLI JSON. Screens and keymaps are in `tui-mockups.md`.

### TUI Scenario 1 - Dashboard review + guide status (mirrors Scenarios 5 + 6)

```sh
atm tui --store "$ATM_HOME" --actor human:alice
# 1 -> Dashboard tab (default landing)
#   REVIEW QUEUE shows ATM-0005 (claimant agent:claude-1, status review)
#   press [a] on ATM-0005 -> approve form -> comment "Looks good" -> Enter
#   OPEN FOLLOWUPS shows ATM-0002 f1 -> press [R] to resolve
#   GUIDE STATUS shows sections conventions/testing with [OK]/[STALE] markers
#   press [r] to refresh; queue now empty, followup resolved
```
**Expected outcome**: the Dashboard renders the same review queue, open followups, and guide status as `atm review dashboard`; approve/resolve mutate the store (verify with `atm task show --id ATM-0005 --output json` showing status `done`).

### TUI Scenario 2 - Projects: labels, type-axis, guide editing (mirrors Scenarios 2 + 6)

```sh
atm tui --store "$ATM_HOME" --actor human:alice
# 2 -> Projects tab
#   select DEMO -> Enter -> project detail
#   [L] add label -> name "type:spike", description "Spike investigation" -> Enter
#   [l] remove label "area:cli" -> confirm; toast shows retained_usage: 1
#   guide pane: [S] section add "testing" -> [g] ref add section=testing kind=task target=ATM-0006
#   [F] set freshness -> "720h" -> Enter
#   [D] jump to Dashboard scoped to DEMO -> GUIDE STATUS shows testing/ATM-0006 [OK]
```
**Expected outcome**: label add/remove with soft-removal `retained_usage` and full guide editing (sections/refs/freshness) all work from the TUI; verify with `atm project guide show --code DEMO --output json` and `atm project label list --code DEMO --output json`.

### TUI Scenario 3 - Tasks: create, claim, context, timeline (mirrors Scenarios 1 + 3 + 4)

```sh
atm tui --store "$ATM_HOME" --actor human:alice
# 3 -> Tasks tab
#   [a] -> new task form: project=ATM, title="TUI-driven task", labels=type:impl,area:tui -> Enter
#     (assigned ATM-0007; verify id shown)
#   [n] -> previews next claimable task; [c] claims it
#   select ATM-0007 -> Enter -> task detail
#     PROJECT GUIDE section renders the always-read harness (FR-017)
#     MATCHING CONVENTIONS lists label-matched convention docs
#     [t] add todo "Wire TUI form"; Space toggles it
#     [o] add followup "Review TUI parity", assignee human:alice
#     [d] add discussion "TUI mirrors CLI"
#     TIMELINE merges history + todo + followup + discussion chronologically
#     [s] set-status -> only in-progress enabled (open->review is disabled/invalid)
```
**Expected outcome**: task creation, next/claim, context (guide + conventions + timeline), and entries all work from the TUI; the task detail renders the same payload as `atm task show --with-context --output json`, and the status popup enforces the transition matrix (invalid transitions disabled).

### TUI/CLI parity check (FR-002)

```sh
# Same store + filters -> same order in TUI list and CLI JSON
atm task list --project ATM --label type:impl --output json --store "$ATM_HOME" > /tmp/cli-list.json
atm tui --store "$ATM_HOME" --actor human:alice
# 3 -> Tasks -> filter: project=ATM, label=type:impl -> observe the row order
# expected: the on-screen order matches /tmp/cli-list.json's task order byte-for-byte
```
**Expected outcome**: the TUI task list and the CLI `task list --output json` produce the same order for the same store and filters (FR-002 thin-client parity; snapshot-testable from one fixture).
