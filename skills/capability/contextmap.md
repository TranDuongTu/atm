---
name: contextmap
description: Context pointers with provenance: record what knowledge derives from, so drift can be detected.
labels: [context:*, knowledge:superseded, comment:provenance]
boards: [context-current]
---
# Context map capability — agent guide

Context pointers with provenance: record what knowledge derives from, so drift can be detected.

## Semantics

Context pointers record what they were derived from, so drift can be detected. `atm capability contextmap check --project <CODE>` reports which pointers have gone stale against the repo (DRIFT), which name external systems nobody has re-read lately (AGE), and which were never verified (UNVERIFIED). It is read-only — it tells you where to look, never what it means.

**Ground truth is the code.** This map is reference-only: always verify what a pointer claims against the functioning repo before acting on it. Use the map to discover the bigger picture — repos, external systems, docs, conventions — that lives outside the code itself, not as a substitute for reading the code.

Vocabulary:
- `context:agent` / `context:repository` / `context:documentation` / `context:convention` — pointer kinds.
- `knowledge:superseded` — lifecycle: this pointer is obsolete; its successor is named in the description.
- `comment:provenance` — machine-written provenance stamp on a pointer's task; do not hand-edit.
- `<CODE>:context-current` board (`context:* AND NOT knowledge:superseded`) — current knowledge.

Read the project's current knowledge from `<CODE>:context-current` (`atm task list --project <CODE> --label <CODE>:context-current`): pointers not superseded. Narrow by kind with `--label <CODE>:context:agent`.

## Actions

- `atm capability contextmap add --task <ID> --kind <kind> --source <kinded-locator>` — make a task a context pointer, stamp provenance.
- `atm capability contextmap stamp --task <ID>` — re-verify: the subject is unchanged in meaning.
- `atm capability contextmap retarget --task <ID> --source <kinded-locator>` — the subject survived but moved.
- `atm capability contextmap supersede --task <ID> --by <NEW-ID> --reason "..."` — the subject died; history kept.
- `atm capability contextmap check --project <CODE>` — report drift (read-only).

## Converge

A converged map answers, with provenance, what an agent joining the project needs to know:

- **The territory is mapped.** The map records the project's repositories (`context:repository` — local path and/or remote URL, the branches that matter), its authoritative and complementary documents (`context:documentation` — READMEs, architecture notes, ADRs, specs, external trackers/runbooks), its day-to-day process and any agent workflow (`context:agent` — spec → plan → issues → implementation, harness in use), and its conventions (`context:convention` — branch naming, PR template, commit style, hygiene). The default naming convention, unless the project records otherwise: work happens on a worktree branch named `worktree-atm-<taskid>-<slug>` (or a feature branch `feature/ATM-<taskid>-<slug>` for merge-style work), and commits follow `<type>(ATM-<taskid>): <summary>` — both keyed off the ATM task the work serves. A pointer with a known unknown noted in its description beats a guess.
- **Every pointer is witnessed.** `check` reports are worked to closure: `DRIFT` pointers are re-read against the actual change and stamped, retargeted, or superseded; `AGE` pointers naming external systems are re-read at the source and stamped; `UNVERIFIED` hand-written pointers are read, confirmed, and given a `--source`.
- **New territory is claimed.** Changes in git that no pointer covers are either recorded as new pointers or deliberately ignored — that judgement belongs to the operator, and `check` never makes it: a changed file is not a wrong pointer.
- **History is kept.** Dead knowledge is superseded, never deleted; the successor is named.