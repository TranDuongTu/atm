---
name: planning
purpose: the weekly planning pass — sweep the flow boards, decide every open item, and publish what each persona should pick up next
suits: [manager]
requires_capabilities: [scrum, channel]
requires_channels: [planning, standup]
target: project
mode: eager
---
1. Orient before deciding anything.
   1. Scan #planning over the last week: the previous plan, the latest retrospect, and any discussion threads that followed. Open threads are input, not history — carry them into today's decisions.
   2. Scan #standup since the previous plan: blockers, drift, and finished work are planning signals.
   3. Find your previous planning handoff comment on the session task. Its timestamp is the CUTOFF; no handoff found means treat everything as fresh.
2. Sweep each enabled flow capability EXCEPT codereview (scrum, qa — skip any not enabled; the codereview flow belongs to retrospect). Take verbs and semantics from `atm capability <name> guide`; this checklist never spells them.
   1. TRIAGE — list the capability's inbox and decide EVERY row exactly once: absorb it per the guide, evict it with a reason, or defer it with a task comment saying why. An empty inbox is converged — never invent work for it. A plan too large for one implementation unit is decomposed here: sub-tasks born part_of the parent, each referencing its plan section, the parent retyped to story.
   2. ADVANCE — run the capability's report and resolve every finding: a verb call, a follow-up task, or a comment recording acceptance. A parent whose live children are all done: verify the children, then stage it done. An unresolved finding means the sweep is not finished.
   3. ROUTE — handle each eviction or verdict newer than the CUTOFF per the guide's backward-flow rules. Entries at or before the CUTOFF are settled.
3. Derive the ready-next list, one section per persona:
   1. developer — scrum tasks that are implementable (approved plan, no unresolved depends_on), tasks mid-implementation with their next increment, and planned tasks that need design work.
   2. staff — open PRs awaiting a pr-review gate (from #prs), and review units retrospect scheduled.
   3. qa — verification work ready to execute.
4. Publish the plan to #planning: the ready-next list per persona, what changed since last week, and open risks or questions. Discussion of the plan continues there.
5. Close with ONE handoff comment on the session task: capabilities swept, decisions made (counts and task IDs), deferrals and why, the #planning reference, and the timestamp — next week's CUTOFF.
