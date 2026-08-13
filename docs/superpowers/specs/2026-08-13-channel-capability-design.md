# Channel capability — design

Task: ATM-097849. Status: approved design, pre-plan.

## Concept

A Channel is how personas communicate with each other and with humans. Repositories are channels; Notion documents are the first new channel type; Slack is a later type, out of scope here. ATM records what a channel is and how to address it; agents do all channel I/O themselves through their own tools (MCP), and the concierge is the point of contact for setting channels up. ATM is a phone book, not a switchboard.

## Decisions (user-confirmed)

1. **Shape: facade over task substrate.** Channels are implemented by a new `channel` capability, not a fifth core entity. Each channel is a labelled task with a versioned `Task.Meta["channel"]` payload — invisible plumbing behind a dedicated `atm channel` CLI noun and a dedicated TUI overlay. Channels are never surfaced as tasks in their own UX. Accepted cost: records appear in an unfiltered `atm task list`. Rationale: channel lifecycle becomes ledger events (actor-stamped, synced, folded) with zero substrate change; purpose text is semantically searchable; the architecture doc's high bar for new store entities is respected.
2. **Three-tier config split.** Tier 1, the ledger record (synced): identity, type, purpose, address. Tier 2, local wiring in `config.json` (per-machine, never synced, no secrets): repo path, MCP server name, verification stamps. Tier 3, secrets: never stored by ATM anywhere — tokens live in the agent-side MCP server's own auth store. Addresses (Notion database/page id, repo remote URL) live in the ledger by default: an address is not a credential, and concierge rehydration on a new machine requires it; ledger readers are project-trusted.
3. **Repo cutover: full replacement.** Repo channels subsume `RepoConfig`. The dispatch dialog reads repo-type channels' tier-2 paths; `atm project repo add/list/remove` is retired with a short deprecation pointer; legacy `repos` entries in `config.json` are migrated via `atm channel migrate-repos` with concierge confirming URL/purpose — ledger records are authored, not fabricated silently.
4. **Status: local probes + aged stamps.** No network I/O from the TUI. Repo probes: path exists, is a git repo, dirty state, ahead/behind against the already-fetched tracking ref (no fetch). Notion and other non-witnessable types: wiring presence plus verification stamps recorded in tier 2 by concierge/agents when they actually touch the channel, displayed with age (fresh/stale coloring). Only agents can truly verify third-party auth; the overlay never claims more than ATM can know.
5. **Endpoints = CLI.** `atm channel show --name <handle> --output json` is the agent endpoint. No HTTP or MCP server in ATM, consistent with the existing machine API and the doctrine that ATM speaks no third-party API and holds no credentials.

## Data model

- Channel record = task labelled `<CODE>:channel:<type>`, `type ∈ {repo, notion}` initially. Task title = display name. Task description = purpose (the searchable narrative).
- `Task.Meta["channel"]` = versioned, unknown-field-preserving JSON payload (modeled on `internal/capability/workflowai/payload.go`): `{v, name, type, address}`. `name` is the unique handle within the project, enforced at the capability CLI layer (scan-before-create; paved road, not a fence). `address` is type-shaped: `{url}` for repo; `{workspace, database_id | parent_page}` for notion.
- Lifecycle events are the existing task actions (`task.created`, `task.capability-meta-set`, `comment.created`, `task.removed`) — no new eventsource actions, no fold or cache changes.
- Vocabulary (seeded idempotently): namespace label `<CODE>:channel:*`; board `<CODE>:channels` selecting the namespace. Workflow/workflow_ai boards filter on their own namespaces, so channel records never appear on them.
- Tier 2: `core.ProjectConfig` gains `Channels map[string]ChannelWiring` keyed by handle; `ChannelWiring = {path?, mcp_server?, stamps []VerificationStamp}`, `VerificationStamp = {at, by, note}`. The `Repos []RepoConfig` field is retired after migration.

## CLI surface

`atm channel` group, mounted only when the capability is enabled for the resolved project (standard registry mechanics):

- `add --name <handle> --type repo|notion --purpose <text> [--url <u>] [--workspace <w> --database <id> | --page <id>]` — authors the tier-1 record (task + label + payload).
- `edit` / `remove` — tier-1 updates; edits journal what changed.
- `wire --name <handle> [--path <p>] [--mcp-server <s>]` — tier-2 wiring for this machine.
- `stamp --name <handle> --note <text>` — appends a verification stamp (actor + timestamp) to tier-2 wiring.
- `list` / `show --name <handle>` — joined read: tier-1 record + this machine's wiring + probe results. `--output json` on both is the agent endpoint.
- `migrate-repos` — one-shot: for each legacy `RepoConfig`, create a repo channel (tier 1: name, URL; purpose left for concierge to confirm) and wiring (tier 2: path), then clear `repos`.
- `guide` / `seed` — standard capability verbs; guide embedded at `skills/capability/channel.md` with the frontmatter/vocabulary pinning test.

## Status model

Probe functions live in the capability package and return plain data; the TUI consumes them via the established seams (no store imports in TUI, no I/O in `View`). Probes run in the TUI's `refreshAll()` path and in `atm channel list/show`.

- Repo: wiring present → path exists → is a git repo → dirty? → ahead/behind vs tracking ref. All local; never fetches.
- Notion (and future non-witnessable types): wiring present (MCP server named) + latest stamp age. Freshness thresholds render green/yellow/red in the overlay.

## TUI overlay

New `internal/tui/channels.go` modeled on `personas.go`: read-only list (name, type, status glyph, stamp age) with a detail view (purpose, address, wiring, stamps, probe detail). Its gate is added to the three KEEP-IN-SYNC sites (`View`, `handleKey`, `workspaceIdle`) and the keymap/help overlay. One action: a key that dispatches a concierge session via the existing `Dispatcher` port, seeded with the selected channel's handle. No other writes from the TUI.

The dispatch dialog's repo cycle-picker switches from `ProjectRepos` to repo-type channels' tier-2 paths; behavior (up/down cycle, cwd fallback when empty) is unchanged.

## Concierge

`skills/persona/concierge.md` generalizes its repo step to channels: converse about where communication and knowledge live; author tier-1 records (`atm channel add`); wire this machine (`atm channel wire`); set up the host agent's MCP for the channel type (e.g. install the Notion MCP server, walk the human through OAuth — the token stays in the MCP server's store); verify and `atm channel stamp`. On a new machine: read the ledger's channel records, then re-wire, re-auth, re-stamp — this is the rehydration path. During migration windows, concierge confirms migrated repo channels' URL and purpose.

## Agent flow

1. `atm channel list --output json` / `atm channel show --name specs --output json` — resolve type, purpose, address, and this machine's wiring.
2. Do I/O directly against the channel through the agent's own tools (git; Notion MCP already authorized on this machine).
3. If wiring or auth is missing, the status is visibly not-connected; the human (or the agent) dispatches concierge to set it up.
4. After successfully touching a channel, agents may `atm channel stamp` to refresh verification.

The capability guide documents this flow for agents.

## Testing

- Payload version round-trip and unknown-field preservation, pinned like workflowai's payload tests.
- Probe unit tests against temp-dir git fixtures (missing path, non-repo, dirty, ahead/behind).
- CLI verbs against a temporary store (never the shared `~/.config/atm` cache, per project doctrine), including handle-uniqueness enforcement and enable-gating of the command group.
- `migrate-repos` from a `config.json` containing legacy repos: channels created, wiring set, `repos` cleared, idempotent on re-run.
- Vocabulary pinned by the existing capability spec test; arch import tests already enforce TUI/core boundaries.

## Out of scope (recorded, not blocked)

- Slack channel type (the type enum and type-shaped address are the extension points).
- workflow_ai spec/plan locators pointing at channels — locator `--kind` is an open string; a future `channel` kind needs no schema change here.
- TUI write operations beyond dispatching concierge.
- Deep probing of agent MCP config files; v1 probes wiring presence + stamps only.
