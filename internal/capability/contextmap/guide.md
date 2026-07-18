# Context map capability — agent guide

Context pointers with provenance: record what knowledge derives from, so drift can be detected.

## What it means

Context pointers record what they were derived from, so drift can be detected. `atm capability contextmap check --project <CODE>` reports which pointers have gone stale against the repo (DRIFT), which name external systems nobody has re-read lately (AGE), and which were never verified (UNVERIFIED). It is read-only — it tells you where to look, never what it means.

## How to use it

- `atm capability contextmap add --task <ID> --kind <kind> --source <kinded-locator>` — make a task a context pointer, stamp provenance.
- `atm capability contextmap stamp --task <ID>` — re-verify: the subject is unchanged in meaning.
- `atm capability contextmap retarget --task <ID> --source <kinded-locator>` — the subject survived but moved.
- `atm capability contextmap supersede --task <ID> --by <NEW-ID> --reason "..."` — the subject died; history kept.
- `atm capability contextmap check --project <CODE>` — report drift (read-only).

Read the project's current knowledge from `<CODE>:context-current` (`atm task list --project <CODE> --label <CODE>:context-current`): pointers not superseded. Narrow by kind with `--label <CODE>:context:agent`.

## Vocabulary

- `context:agent` / `context:repository` / `context:documentation` / `context:question` — pointer kinds.
- `knowledge:superseded` — lifecycle: this pointer is obsolete; its successor is named in the description.
- `comment:provenance` — machine-written provenance stamp on a pointer's task; do not hand-edit.
- `<CODE>:context-current` board (`context:* AND NOT knowledge:superseded`) — current knowledge.

## Brief

Interview the human to map this project's knowledge. Ask:
- "What repos does this project depend on? Where are they?" → create `context:repository` tasks, `add` with `--source git:<path>` / `--source url:<url>`.
- "What docs should a new agent read first?" → create `context:documentation` tasks, `add` with the doc locator.
- "What are the agent-direction notes for this project (build/test/lint commands, gotchas)?" → create one `context:agent` task, write the notes in its description, `add` with the source.
- "Are there open questions about the project a human should clarify?" → create `context:question` tasks.

For each, the description is the payload; the provenance stamp records where it came from.

## Autopilot

Reconcile the context map against reality. Repeatable; meant to be run often.
1. **Verify.** Run `atm capability contextmap check --project <CODE>`. Work the report:
   - `DRIFT` — read the pointer's description against the actual change. If still true, `stamp`. If the subject moved, `retarget`. If it died, create the successor and `supersede`.
   - `AGE` — an external source (Jira, Notion) nothing can witness locally. Re-read it with your own tools, then `stamp`.
   - `UNVERIFIED` — a hand-written pointer. Read it, confirm, then `add` with a `--source`.
2. **Discover.** Work the `NEW` list: territory changed in git that no pointer claims. For each worth knowing, create a task and `add` it. Ignore what's not worth a pointer — that judgement is yours.
3. **Close.** Everything reported is now stamped, retargeted, superseded, or deliberately ignored.

`check` never marks anything stale: a changed file is not a wrong pointer. It tells you where to look; you decide what it means.