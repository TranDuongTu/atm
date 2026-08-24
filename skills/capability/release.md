---
name: release
description: "Release registry capability: durable records of what shipped together, cut as versions and stamped onto their members."
brief: Release registry capability — records of what shipped together; read its guide before cutting, including, or shipping a release.
labels: [release:*]
boards: []
---
# release capability — agent guide

Durable records of what shipped together. A release is an ordinary task: its comment thread is the release log, its label is the version, and its payload holds the roster.

release is a REGISTRY capability, not a flow one. The distinction is mechanical — it does not implement the flow contract — and everything follows from that: **no lanes**, no inbox, no wiring, no sockets, no place in the capability switcher, and no manager triage loop. It is used through its verbs, through explicit manager dispatch, and through search ("what shipped in v1.2").

## Semantics

### One namespace

`release:*` is the only namespace this capability owns. Its values are minted on demand:

- One value per cut version. Versions are sanitized into the label grammar: dots and underscores become dashes, so `v1.2` becomes `release:v1-2`. Anything the grammar cannot carry is refused at `cut` rather than written and bounced.
- `release:done` — this task's change is shipped. `ship` stamps it on the container AND on every member.

### Containers and members

A CONTAINER is the release record `cut` creates. A MEMBER is a task `include` added to it. The edge is stored at both ends in `Meta["release"]`:

- on the container: `version` (the sanitized value) and `members` (the roster);
- on a member: `release_of`, the container's task ID. Its presence is what MAKES a task a member.

A shipped release keeps its roster. `exclude` refuses after `ship`, because editing a shipped record rewrites history rather than correcting it — cut the next version instead.

### Selection is judgment, not a check

**The verbs are mechanical.** `include` checks only that both tasks exist, share a project, that the container really is one, and that the member is not already in another release. It does not check that the work is certified — and that is deliberate: a rule enforced invisibly inside a verb is a rule nobody can read, argue with, or override.

The selection rule lives here instead, and the decider applies it:

> Prefer work that is `scrum-stage:done AND qa:done` — reviewed and verified — and **originals only**.

The originals-only half needs no filter of its own: qa never stamps its finish socket on a test scaffold, so no scaffold can satisfy that expression.

`report` supports the same judgment by showing each member's public labels verbatim. It does not interpret them; naming another capability's labels inside this one would tie release to capabilities it must not know about.

## Actions

- `atm capability release cut --project <CODE> --version <v>` — create the release record for a version (e.g. `--version v1.2` → `release:v1-2`).
- `atm capability release include --task <ID> --release <CONTAINER-ID>` — add a task: version label on the member, member on the roster, container on the member's payload.
- `atm capability release exclude --task <ID> --release <CONTAINER-ID>` — reverse it, while the release has not shipped.
- `atm capability release ship --release <CONTAINER-ID>` — stamp `release:done` on the container and every member, and write the log entry.
- `atm capability release report --project <CODE>` — every release, its roster, and each member's public labels (read-only).
- `atm capability release seed --project <CODE>` — ensure the vocabulary exists.

## Converge

A converged project reads like this:

- Every container's roster names tasks that exist.
- Every member's `release_of` points at a container whose roster lists it back.
- Every shipped release has every member stamped shipped; nothing is left behind.
- Open releases have current rosters — what is in them is what is meant to go out.
- No `report` finding is left standing: each is resolved or answered with a recorded decision.
