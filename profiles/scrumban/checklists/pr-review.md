---
name: pr-review
purpose: pre-merge review of one open PR — the merge gate on an in-flight increment
suits: [staff]
requires_capabilities: [scrum, channel]
requires_channels: [reviews]
target: task
targets: "scrum-stage:implementing"
mode: eager
---
1. Gate: an open PR is recorded on the task (or named at dispatch). No PR: refuse with a comment.
2. Orient: the task's journal, the approved plan's section for this increment, the spec where the PR touches it.
3. Review with your own eyes — run, trace, probe what you doubt. Findings ON the PR, anchored to lines.
4. Verdict as the PR's review (approve / request changes) + one mirror comment on the task with the verdict and key findings.
5. Non-blocking findings worth tracked action become follow-up items; blocking ones are the developer's next session.
6. Brief entry in #reviews: task, PR, verdict.
