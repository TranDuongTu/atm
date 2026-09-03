---
name: retrospect
purpose: the periodic quality pass — plan the codereview backlog, dispatch grouped reviews, turn findings and frictions into follow-ups
suits: [manager]
requires_capabilities: [codereview, channel]
requires_channels: [reviews, prs, planning]
target: project
mode: eager
---
1. Establish the window: your previous retrospect handoff comment is the CUTOFF.
2. Sweep the codereview flow per its definition: triage the inbox (finished tasks with their PR sets), group merged PRs since the CUTOFF into review units — by task, or by theme when PRs across tasks touch one area.
3. Dispatch staff sub-agents per review unit; collect verdicts; record begin/finish per the definition.
4. Route EVERY finding exactly once: a follow-up task, a reopen pair, or a comment recording acceptance.
5. Read the period's signals across #reviews, #qa, #prs, and the standups: recurring findings, frictions, drift between plans and as-built. Each systemic observation becomes a follow-up task or a written proposal — never just a remark.
6. Post the retrospect to #planning: what was reviewed, findings and follow-ups, systemic notes — direct input to the next planning pass.
7. Close with a handoff comment: units reviewed, decisions, the timestamp — next retrospect's CUTOFF.
