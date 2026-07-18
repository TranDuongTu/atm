# Context map capability — agent guide

Context pointers with provenance: record what knowledge derives from, so drift can be detected.

## What it means

Context pointers record what they were derived from, so drift can be detected. `atm context check --project <CODE>` reports which pointers have gone stale against the repo (DRIFT), which name external systems nobody has re-read lately (AGE), and which were never verified (UNVERIFIED). It is read-only — it tells you where to look, never what it means.

## How to use it

Record a pointer with `atm context add`; re-verify one with `atm context stamp`; repoint a subject that moved with `atm context retarget`; retire one whose subject died with `atm context supersede`. These verbs own the context vocabulary — do not hand-assign the labels or hand-edit a provenance comment.

Read the project's current knowledge from the `<CODE>:context-current` board (`atm task list --project <CODE> --label <CODE>:context-current`): agent directions, repository pointers, and documentation pointers that have not been superseded. Read this board rather than a raw namespace; membership is computed, so it is always the latest. Narrow by kind with an extra `--label <CODE>:context:agent`.

## Manager duty

Mapping — reconcile the project's context map against reality. Repeatable, and meant to be run often; the first run in a fresh repo is just the case where there is nothing yet to verify.

1. **Verify.** Run `atm context check --project <CODE>`. Work the report:
   - `DRIFT` — read the pointer's description against the actual change. If the description still tells the truth, `atm context stamp --task <ID>`. If the subject survived but moved, `atm context retarget --task <ID> --source <kinded-locator>`. If the subject died or was replaced, create the successor and `atm context supersede --task <ID> --by <NEW-ID> --reason "..."`.
   - `AGE` — an external source (Jira, Notion) that nothing can witness locally. Re-read it with your own tools, then `stamp`.
   - `UNVERIFIED` — a pointer someone wrote by hand. Read it, confirm it is true, then `atm context add --task <ID> --kind <kind> --source <kinded-locator>`.
2. **Discover.** Work the `NEW` list: territory that changed in git and that no pointer claims. For each thing worth knowing, create a task and `atm context add` it. Ignore what is not worth a pointer — that is a judgement, and it is yours.
3. **Close.** Everything reported is now stamped, retargeted, superseded, or deliberately ignored.

`check` never marks anything stale: a changed file is not a wrong pointer. It tells you where to look; you decide what it means.
