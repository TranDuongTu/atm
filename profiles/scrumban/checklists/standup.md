---
name: standup
purpose: the daily heartbeat — record what everyone is doing across all capabilities and post it to #standup
suits: [manager]
requires_capabilities: [channel]
requires_channels: [standup]
target: project
mode: eager
---
1. Establish the window: find the previous entry in #standup; its timestamp is the CUTOFF (none found: the last 24 hours).
2. Gather what happened since the CUTOFF, across every enabled capability:
   1. Task comments, stage changes, and new tasks — `atm search` and the boards.
   2. Work in flight: which tasks are actively being worked, by whom.
   3. New arrivals in each capability's inbox — count them only; deciding them is planning's job, not standup's.
3. Compose the standup, one line-set per actor or workstream: did / doing / blocked. Every blocker names its task and what would unblock it.
4. Post the entry to #standup.
5. A blocker that cannot wait for the next planning pass goes to a human NOW — one crisp question with task and comment IDs — not into the entry alone.
6. Add a brief comment on the session task referencing the posted entry.
