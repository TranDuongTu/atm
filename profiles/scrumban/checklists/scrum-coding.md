---
name: scrum-coding
purpose: implement one implementable scrum task, increment by increment — one PR per increment, task done when the last one merges
suits: [developer]
requires_capabilities: [scrum, channel]
requires_channels: [prs, design]
target: task
targets: "(scrum:task OR scrum:bug) AND (scrum-stage:implementable OR scrum-stage:implementing)"
mode: eager
---
1. Gate — verify before building; push back instead of proceeding when any check fails.
   1. The task is claimed by scrum as implementation work (task or bug), staged implementable or implementing.
   2. An approved plan is recorded, structured as ordered mergeable increments (read it).
   3. No unresolved depends_on.
   4. The increments imply parallel work or a multi-session span: push back with a decomposition proposal — sub-tasks born part_of this task, each referencing its plan section; the task retypes to story. A push-back is a valid session outcome.
   5. Any other check fails: comment exactly what is missing, correct the stage per the guide if mislabeled, and STOP.
2. Locate your increment: from the task's journal and #prs, establish which increments are already merged and which PRs are in flight. This session works the first unfinished increment — finish an in-flight PR (respond to its review) before opening a new one. Stamp the task implementing if this is the first.
3. Set up an isolated worktree; the branch names the task and the increment. Never build on a shared checkout.
4. Implement the increment per the plan, inside the repository's own process and frameworks. Small commits naming the task; journal decisions and any deviation the moment they happen — and mirror deviations onto the plan document in #design, so the plan a later session reads is the plan as-built. A deviation that invalidates the plan's shape is not yours to absorb: push back for a scrum-design pass. If the increment outgrows a reviewable PR (~1k diff), stop and split it — a journaled plan deviation, not a bigger PR.
5. Run the repository's verification gate and read the full output. Green means read-and-confirmed, never assumed.
6. Open the increment's PR. Log it in #prs (task ID, increment, PR reference, verification evidence), record it on the task, and mark the increment landed on the plan document in #design (check its boxes, note the PR).
7. Drive it toward merged: address review findings now if the pr-review arrives within the session. If the wait outlasts the session, add a handoff comment (increment landed, PR ref, what's next) and end — the task stays implementing; the next scrum-coding dispatch resumes at step 2.
8. When the plan's LAST increment merges: stamp the task's stage per the guide and add the closing comment — outcome, evidence, the full PR list. Only then is the task's implementation done.
