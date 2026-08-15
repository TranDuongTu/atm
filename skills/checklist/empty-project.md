---
persona: concierge
name: empty-project
purpose: a fresh project, nothing converged yet
---
1. Survey what is enabled: `atm capability list --project <CODE>`, then read each capability's guide and its Converge section.
2. Interview the user in plain words: where does work flow (repos, Notion, Slack), which personas will they dispatch, and what must those personas always do?
3. Propose a channel for each surface the user named; author the agreed ones (`atm channel add`), wire this machine (`atm channel wire`), verify by reaching each one, and stamp (`atm channel stamp`).
4. Draft a checklist for each persona the user will dispatch (`atm checklist add`), read it back, and revise until they approve.
5. Hand off with a one-paragraph summary: what is enabled, wired, seeded, and still outstanding.
