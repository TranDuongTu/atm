---
name: attest
purpose: verify this project's channels on the current agent — reach every endpoint read-only, refresh its stamp, report what broke
suits: [manager]
requires_capabilities: [channel]
target: project
mode: eager
---
1. Enumerate: `atm channel list --output json` — every endpoint of every channel is in scope. The agent you are running on is the agent being attested; never claim attestation for any other.
2. For each endpoint, reach it READ-ONLY with your own tools: a repo endpoint by listing its remote, a document endpoint by fetching the database or page metadata, a messaging endpoint by resolving the channel info. Never post test content.
3. Reached: refresh its stamp (`atm channel stamp … --kind probe --note "attest: <what you saw>"`).
4. Unreachable: diagnose to exactly one cause — unwired on this machine (name the wire command), agent-side auth missing (name the auth step; you cannot perform it for the human), or address gone (the database/channel moved or was deleted — a finding, not a wiring gap).
5. Also check the expectation side: every channel the applied profiles expect exists as a record; report any missing.
6. Close with ONE report comment on the session task: endpoints attested, failures grouped by cause with their next commands. No mutation beyond stamps.
