---
name: channel
description: Channels — repositories, Notion, Slack, and future surfaces personas communicate through; ledger-recorded identity and address, machine-local wiring, agent-side I/O.
brief: Before pasting large output or reaching an external surface, run `atm channel list` — channels are where big content and cross-tool handoffs go.
labels: [channel:*]
boards: [channels]
---
# channel capability — agent guide

A channel is how personas communicate with each other and with humans: a git repository, a Notion database, a Slack channel. ATM is the phone book, not the switchboard: it records what a channel is and how to address it, and agents do all channel I/O themselves through their own tools (git, MCP servers). Channels are set up — and re-established on a new machine — by a session scoped to this capability (`--capability channel`).

Channels offload large-content communication; they do not replace the ledger. Journal intent, decisions, and progress as task comments as you normally would; when the content itself is too large for a comment, put it in the channel whose purpose fits it and leave a short comment pointing at the channel handle. Read a channel's purpose before using it — `atm channel show --project <CODE> --name <handle>` — and never use a channel for work its purpose does not describe.

How each persona uses channels:

- **Developer** — journal as usual, and for large work content (a spec draft, a sizeable diff, a design doc, a long handoff write-up), place the content in the existing channel that fits — push to the repo channel, drop the doc in the Notion channel — instead of stuffing it into a task comment. Leave the task comment as a pointer: the channel handle plus what was placed there. If no channel fits, the default comment is fine; consider proposing a channel later.
- **Manager** — broadcast work across the communication channel for everyone's awareness when one exists: post the status update to the Slack channel, drop the summary in Notion. If you need clarification from a developer, send it to the channel and follow up until it resolves — record the pending question in the ledger so a later session checks it. The ledger stays the source of truth; the channel is the broadcast.
- **Other personas** — read each channel's purpose and pick the best fit for the work: a repo channel carries code-shaped handoffs, a Notion channel carries documents, a Slack channel carries announcements and questions. Use the channel that improves communication for that work; do not invent a channel where none fits.

Deciding where content goes:

1. A decision, a progress note, an intent — a short, durable ledger fact: task comment.
2. Large content that belongs somewhere durable — spec, doc, diff, long write-up: the channel whose purpose fits it, referenced from a short comment.
3. No channel fits: the default comment is acceptable.

**Scoped session.** A session dispatched with `--capability channel` (for example from the TUI channels overlay) exists to make this project's channels healthy and nothing else: run `atm channel list --project <CODE> --output json` and branch on what it shows. No records — interview the user about where work flows, discover repo candidates locally, propose each before acting (`add`, `wire`, verify, `stamp`). Records without wiring (fresh machine) — re-wire each record on this machine: match a local checkout's remote, clone the recorded URL, or authorize the agent-side Notion MCP server, then re-stamp. Partial or stale — read each status note and repair: re-verify and re-stamp fresh ones, re-wire moved paths, surface dirty or diverged repos rather than fixing them silently. Never expose flag shapes — speak in plain words and run the commands yourself. Never ask for tokens or passwords; authorization lives in the agent's own tooling. Hand off with a one-paragraph summary of what is now wired, stamped, and still outstanding.

## Semantics

A channel is a PLACE CONTENT FLOWS, not a single address, so it is reached through one or more ENDPOINTS. Each endpoint carries a medium, an address shaped for it, and a role. `home` is where content lands — at most one per channel, so "the home endpoint" always means something to a writer. `broadcast` endpoints receive a one-line reference to whatever landed in the home. Auto-mirroring is the contract, not a convention: put the content in the home, put a reference in every broadcast, and when reading, scan them all. Checklists never name a medium — they name `#handle` and leave the resolution to you (`atm channel show`), so the same procedure works whether that handle is a Notion database, a Slack channel, or both.

Three tiers. Tier 1, the ledger record (synced): a task labelled `channel` whose title is the unique handle, whose description is the purpose, and whose `channel` metadata payload carries the endpoints (repo: remote URL; notion: workspace + database/page; slack: workspace slug + channel id). Records written before a channel could have several endpoints carry a `channel:<type>` label instead and read as a single endpoint whose role follows its medium; the first write migrates the label. Addresses are not credentials. Tier 2, local wiring (`config.json`, this machine only, never synced): the repo clone path, the MCP server name agents here use, and verification stamps (actor + time + note) recorded when someone actually touches the channel. Tier 3 does not exist: ATM stores no tokens, passwords, or credentials, anywhere — authorization lives with the agent's own tooling (e.g. the Notion or Slack MCP server's OAuth store). A channel record carries no permissions or scopes either: a Slack app's bot scopes are granted at install time and its bot is invited to a channel in Slack's own UI, so two machines can share one record while holding different tokens, or none.

Channel records are tasks only as plumbing: manage them exclusively through `atm channel`, never through raw task verbs. The `channels` board exists so queries can see them; workflow boards filter on their own namespaces and never show channels.

## Actions

- `atm channel add --project <CODE> --name <handle> --type repo|notion|slack --purpose "..." [--url <u>] [--workspace <w>] [--database <id>] [--page <id>] [--channel-id <id>]` — author the ledger record. A slack channel takes `--workspace` (the domain slug, the `acme` of `acme.slack.com`) and `--channel-id`.
- `atm channel list --project <CODE>` / `atm channel show --project <CODE> --name <handle>` — the agent endpoint: with `--output json`, returns identity, purpose, address, this machine's wiring, and probe results. Resolve a channel once, then do I/O directly through your own tools.
- `atm channel endpoint add --project <CODE> --name <handle> --type repo|notion|slack [--role home|broadcast] [address flags]` — add a medium to a channel, or correct the address of one it already has. The role defaults from the medium: document-shaped media hold content, messaging media carry references. A second home is refused.
- `atm channel endpoint remove --project <CODE> --name <handle> --type <t>` — drop one medium. The handle survives with no endpoints: a purpose with no address is an expectation waiting to be addressed.
- `atm channel edit --project <CODE> --name <handle> [--purpose "..."] [address flags]` / `atm channel remove --project <CODE> --name <handle>` — tier-1 updates. An address edit names no medium, so it targets the channel's first endpoint; correct a specific one with `endpoint add`.
- `atm channel wire --project <CODE> --name <handle> [--path <dir>] [--mcp-server <name>]` — record how THIS machine reaches the channel.
- `atm channel stamp --project <CODE> --name <handle> --note "..."` — vouch that you actually reached the channel just now; stamps age into the status display.
- `atm channel migrate-repos --project <CODE>` — one-shot lift of legacy repo dispatch targets into repo channels.
- `atm capability channel seed --project <CODE>` — idempotently ensure vocabulary and board.

## Converge

Every place personas exchange work is recorded as a channel with an honest purpose; addresses live in the ledger so a new machine can rehydrate. On this machine, every channel an agent needs is wired (path or MCP server) and carries a reasonably fresh stamp; stale stamps mean dispatch a `--capability channel` session, which reads the ledger records, re-wires, walks the human through agent-side MCP auth, and re-stamps. After using a channel successfully, refresh its stamp. Never write credentials into any ATM surface — a channel needing auth is set up in the agent's own tooling, and ATM only records that it happened.
