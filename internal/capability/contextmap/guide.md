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

Interview the human to map this project's knowledge. The goal is to record what an agent needs to know — and where each piece came from — so future drift is detectable. Ask, one topic at a time:

- **Repos.** "Which repositories does this project involve — its own, and the ones it depends on or builds against? Where do they live, and which branches/tags matter right now?" → `context:repository` tasks, `add` with `--source git:<path>` / `--source url:<url>`.
- **External sources.** "Are there external systems this project references — issue trackers, design docs, runbooks, dashboards, upstream APIs?" → `context:documentation` (or `context:repository` when it's another repo) tasks, `add` with the appropriate `--source`.
- **Docs to read first.** "What docs should a new agent read first to orient? Architecture, ADRs, specs, READMEs, the AGENTS.md / CLAUDE.md equivalent?" → `context:documentation` tasks, `add` with the doc locator.
- **Process, conventions, skills.** "What process does this repo run — spec → plan → issues → implementation? What conventions (build/test/lint, commit style, branch model) and which agent skills or prompts are in force?" → one `context:agent` task whose description carries the agent-direction notes, `add` with the source.
- **Open questions.** "Are there open questions about the project a human should clarify?" → `context:question` tasks.

For each, the description is the payload; the provenance stamp records where it came from. When the human is unsure, capture what they do know as a `context:question` rather than guessing — pointers with unknowns are still useful.

## Autopilot

Reconcile the context map against reality. Repeatable; meant to be run often.
1. **Verify.** Run `atm capability contextmap check --project <CODE>`. Work the report:
   - `DRIFT` — read the pointer's description against the actual change. If still true, `stamp`. If the subject moved, `retarget`. If it died, create the successor and `supersede`.
   - `AGE` — an external source (Jira, Notion) nothing can witness locally. Re-read it with your own tools, then `stamp`.
   - `UNVERIFIED` — a hand-written pointer. Read it, confirm, then `add` with a `--source`.
2. **Discover.** Work the `NEW` list: territory changed in git that no pointer claims. For each worth knowing, create a task and `add` it. Ignore what's not worth a pointer — that judgement is yours.
3. **Close.** Everything reported is now stamped, retargeted, superseded, or deliberately ignored.

`check` never marks anything stale: a changed file is not a wrong pointer. It tells you where to look; you decide what it means.
