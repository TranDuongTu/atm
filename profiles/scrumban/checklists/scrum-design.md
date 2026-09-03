---
name: scrum-design
purpose: take a planned scrum task to implementable — brainstorm with the user, publish spec and plan to #design, get approval
suits: [developer]
requires_capabilities: [scrum, channel]
requires_channels: [design]
target: task
targets: "scrum:* AND scrum-stage:planned"
mode: interactive
---
1. Gate — this task needs design, not code.
   1. The task is claimed by scrum, staged planned, and has no approved plan yet.
   2. Already implementable, or not ready for design at all: push back with a task comment saying why, and STOP.
2. Brainstorm with the user — ALWAYS. Use the repository's own framework for it (e.g. superpowers:brainstorming). Never draft a spec unilaterally; the user's intent is the raw material.
3. Write the spec, then the plan, per the repository's frameworks (e.g. superpowers:writing-plans). Structure the plan as ordered, independently mergeable increments — each leaves main green and shippable; coherence decides the cut, a reviewable PR (~1k diff) bounds it. Publish both as documents to #design — that channel is their home; the worktree is code-only.
4. Record the locators on the task (the scrum spec and plan verbs) and journal the key decisions and rejected alternatives as task comments.
5. Seek approval: present the plan in #design and to the user; iterate until it is explicitly approved. Approval covers content AND shape — the increments must be independently mergeable and reviewably sized. Silence is not approval.
6. On approval: stamp the task implementable — or, when the increments imply parallel work or a multi-session span, recommend decomposition (sub-tasks part_of this task, each referencing its plan section; the task retypes to story). Add a closing comment with the document references — the hand-off the next session picks up from.
