# Context map capability — agent guide

Context pointers with provenance: record what knowledge derives from, so drift can be detected.

## What it means

Context pointers record what they were derived from, so drift can be detected. `atm capability contextmap check --project <CODE>` reports which pointers have gone stale against the repo (DRIFT), which name external systems nobody has re-read lately (AGE), and which were never verified (UNVERIFIED). It is read-only — it tells you where to look, never what it means.

**Ground truth is the code.** This map is reference-only: always verify what a pointer claims against the functioning repo before acting on it. Use the map to discover the bigger picture — repos, external systems, docs, conventions — that lives outside the code itself, not as a substitute for reading the code.

## How to use it

- `atm capability contextmap add --task <ID> --kind <kind> --source <kinded-locator>` — make a task a context pointer, stamp provenance.
- `atm capability contextmap stamp --task <ID>` — re-verify: the subject is unchanged in meaning.
- `atm capability contextmap retarget --task <ID> --source <kinded-locator>` — the subject survived but moved.
- `atm capability contextmap supersede --task <ID> --by <NEW-ID> --reason "..."` — the subject died; history kept.
- `atm capability contextmap check --project <CODE>` — report drift (read-only).

Read the project's current knowledge from `<CODE>:context-current` (`atm task list --project <CODE> --label <CODE>:context-current`): pointers not superseded. Narrow by kind with `--label <CODE>:context:agent`.

## Vocabulary

- `context:agent` / `context:repository` / `context:documentation` / `context:convention` — pointer kinds.
- `knowledge:superseded` — lifecycle: this pointer is obsolete; its successor is named in the description.
- `comment:provenance` — machine-written provenance stamp on a pointer's task; do not hand-edit.
- `<CODE>:context-current` board (`context:* AND NOT knowledge:superseded`) — current knowledge.

## Brief

Interview the human to map this project's knowledge. The goal is to record what an agent needs to know — and where each piece came from — so future drift is detectable. Ask one topic at a time, in this order; let each answer finish before moving on.

1. **Repos.** "Which repositories does this project involve — its own, and the ones it depends on or builds against? Where do they live (local path and remote URL), and which branches/tags matter right now?" Also brief the human on the repos the map already manages, so they can confirm or correct. → `context:repository` tasks; `add` with `--source git:<path>` and/or `--source url:<url>`.
2. **Docs.** First self-analyze the repos from step 1 to discover candidate documents (READMEs, architecture notes, ADRs, specs, AGENTS.md / CLAUDE.md equivalents). Present that list to the human and ask which are authoritative vs. complementary. Then ask whether any external documents (issue trackers, design docs, runbooks, upstream APIs) also matter. → `context:documentation` tasks; `add` with the doc locator. Take notes on what each doc covers.
3. **Process.** "What does day-to-day development here look like — spec → plan → issues → implementation? Do you use any AI harness or agent workflow, and if so which?" → one `context:agent` task whose description carries the agent-direction and process notes; `add` with the source.
4. **Conventions.** "Any other conventions to record — branch naming, PR template, commit style, build/test/lint hygiene?" Also surface conventions you inferred from reading the code in step 2, and ask the human to confirm or reject each. Take notes. → `context:convention` tasks; `add` with the source (e.g. `--source file:<path>` or `--source git:<path>`).

For each, the description is the payload; the provenance stamp records where it came from. When the human is unsure, capture what they do know as a `context:documentation` or `context:convention` pointer with the gap noted in the description rather than guessing — pointers with unknowns are still useful.

## Autopilot

Reconcile the context map you set up earlier against reality. Repeatable; meant to be run often. The point is to detect staleness in what you recorded and correct it.

1. **Verify.** Run `atm capability contextmap check --project <CODE>`. Work the report:
   - `DRIFT` — read the pointer's description against the actual change. If still true, `stamp`. If the subject moved, `retarget`. If it died, create the successor and `supersede`.
   - `AGE` — an external source (Jira, Notion) nothing can witness locally. Re-read it with your own tools, then `stamp`.
   - `UNVERIFIED` — a hand-written pointer. Read it, confirm, then `add` with a `--source`.
2. **Discover.** Work the `NEW` list: territory changed in git that no pointer claims. For each worth knowing, create a task and `add` it. Ignore what's not worth a pointer — that judgement is yours.
3. **Close.** Everything reported is now stamped, retargeted, superseded, or deliberately ignored.

`check` never marks anything stale: a changed file is not a wrong pointer. It tells you where to look; you decide what it means.
