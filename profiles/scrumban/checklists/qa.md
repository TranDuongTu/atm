---
name: qa
purpose: verify one piece of finished work from the customer's seat — evidence on the task, coverage gaps as new items, update in #qa
suits: [qa]
requires_capabilities: [qa, channel]
requires_channels: [qa]
target: task
targets: "qa:testing"
mode: eager
---
1. Gate — the dispatch must be verification work: an absorbed original with scaffolds, or one scaffold to execute. Anything else: push back with a comment and STOP.
2. Orient from the customer's seat: read the task, its spec, and name the customer workflow this work serves. If you cannot name the workflow, say so on the task — that itself is a finding.
3. Execute the verification: the end-to-end workflow first, then down to the unit level where it matters. For every run keep the evidence — command and output.
4. Record results where they belong: evidence-bearing comments on the scaffold and its original; findings on the artifact itself where one exists.
5. Every coverage gap or friction you find becomes a concrete item — a new scaffold or a follow-up task — never just a remark. A green run through a workflow nobody else tests is still a gap report waiting to be filed.
6. Post progress and outcomes to #qa: what was verified, verdicts, gaps found — with task references.
7. Close with a verdict per the guide: pass with its demonstration, or fail with its reproduction (steps, expected, actual), routed per the guide's backward-flow rules.
