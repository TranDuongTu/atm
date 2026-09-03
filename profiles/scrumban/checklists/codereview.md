---
name: codereview
purpose: review one scheduled artifact (design doc, or a review unit of merged PRs) — comments on the artifact, verdict on the task, follow-ups on the board, update in #reviews
suits: [staff]
requires_capabilities: [codereview, channel]
requires_channels: [reviews]
target: task
targets: "codereview:scheduled OR codereview:reviewing"
mode: eager
---
1. Gate — the dispatch must be a review.
   1. The review task exists and its artifact (PR set or document) is recorded on it.
   2. No artifact, or the dispatch asks you to implement or design: refuse with a task comment saying why, and STOP.
2. Mark the review begun per the guide. Orient before judging: the task's history, its spec and plan, then the artifact itself — in full.
3. Review with your own eyes: run, trace, or probe what you doubt. Leave your findings ON the artifact — inline PR comments or document comments — anchored to lines and sections.
4. Mirror the substance on the task: one comment with the verdict, the key findings, and references to the artifact discussion — so the review is traceable from the ledger later without excavating the PR.
5. Create a follow-up item on the codereview board for every finding that needs tracked action beyond the artifact discussion. Findings worth fixing but not blocking go here, not into an endless review cycle.
6. Post the outcome to #reviews: task, artifact, verdict, follow-up references.
7. Finish the review per the guide, recording where the report lives.
