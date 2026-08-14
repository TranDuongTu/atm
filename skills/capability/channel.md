---
name: channel
description: Channels — repositories, Notion, and future surfaces personas communicate through; ledger-recorded identity and address, machine-local wiring, agent-side I/O.
labels: [channel:*]
boards: [channels]
---
# channel capability — agent guide

A channel is how personas communicate with each other and with humans: a git repository, a Notion database, later Slack. ATM is the phone book, not the switchboard: it records what a channel is and how to address it, and agents do all channel I/O themselves through their own tools (git, MCP servers). The concierge is the point of contact for setting channels up and re-establishing them on new machines.

## Semantics

Three tiers. Tier 1, the ledger record (synced): a task labelled `channel:<type>` whose title is the unique handle, whose description is the purpose, and whose `channel` metadata payload carries the type-shaped address (repo: remote URL; notion: workspace + database/page). Addresses are not credentials. Tier 2, local wiring (`config.json`, this machine only, never synced): the repo clone path, the MCP server name agents here use, and verification stamps (actor + time + note) recorded when someone actually touches the channel. Tier 3 does not exist: ATM stores no tokens, passwords, or credentials, anywhere — authorization lives with the agent's own tooling (e.g. the Notion MCP server's OAuth store).

Channel records are tasks only as plumbing: manage them exclusively through `atm channel`, never through raw task verbs. The `channels` board exists so queries can see them; workflow boards filter on their own namespaces and never show channels.

## Actions

- `atm channel add --project <CODE> --name <handle> --type repo|notion --purpose "..." [--url <u>] [--workspace <w>] [--database <id>] [--page <id>]` — author the ledger record.
- `atm channel list --project <CODE>` / `atm channel show --project <CODE> --name <handle>` — the agent endpoint: with `--output json`, returns identity, purpose, address, this machine's wiring, and probe results. Resolve a channel once, then do I/O directly through your own tools.
- `atm channel edit --project <CODE> --name <handle> [--purpose "..."] [address flags]` / `atm channel remove --project <CODE> --name <handle>` — tier-1 updates.
- `atm channel wire --project <CODE> --name <handle> [--path <dir>] [--mcp-server <name>]` — record how THIS machine reaches the channel.
- `atm channel stamp --project <CODE> --name <handle> --note "..."` — vouch that you actually reached the channel just now; stamps age into the status display.
- `atm channel migrate-repos --project <CODE>` — one-shot lift of legacy repo dispatch targets into repo channels.
- `atm capability channel seed --project <CODE>` — idempotently ensure vocabulary and board.

## Converge

Every place personas exchange work is recorded as a channel with an honest purpose; addresses live in the ledger so a new machine can rehydrate. On this machine, every channel an agent needs is wired (path or MCP server) and carries a reasonably fresh stamp; stale stamps mean dispatch a concierge session, which reads the ledger records, re-wires, walks the human through agent-side MCP auth, and re-stamps. After using a channel successfully, refresh its stamp. Never write credentials into any ATM surface — a channel needing auth is set up in the agent's own tooling, and ATM only records that it happened.

## Scoped session

A session dispatched with `--capability channel` (for example from the TUI channels overlay) exists to make this project's channels healthy and nothing else. Run `atm channel list --project <CODE> --output json` first and branch on what it shows:

- **No records.** Interview the user in their own words about where work flows — repositories, Notion workspaces, other surfaces — one question at a time. In parallel, discover candidates yourself: scan the working directory, its parent, and sibling checkouts/worktrees for git repositories and note each one's remote URL. Propose every candidate (remote URL as the address, folder as the wiring) and confirm before acting: `atm channel add`, then `atm channel wire`, verify you can actually reach it, then `atm channel stamp`.
- **Records without wiring (fresh machine).** The ledger already knows this project's channels; only this machine's wiring is missing. For each unwired record, search local checkouts for a repository whose remote matches the record's address and propose `atm channel wire --path` for it; offer to clone the recorded URL when nothing matches. For a notion record, help the user install and authorize the agent-side MCP server, then `atm channel wire --mcp-server`. Re-stamp each channel after verifying it.
- **Partial or stale.** For every `◐`/`○` entry with wiring, read its status note and repair it: re-verify and re-stamp fresh ones, re-wire moved paths, surface dirty or diverged repos to the user rather than fixing them silently.

Never expose flag shapes to the user — speak in plain words and run the commands yourself. Never ask for tokens or passwords; authorization lives in the agent's own tooling. Hand off with a one-paragraph summary of what is now wired, stamped, and still outstanding.
