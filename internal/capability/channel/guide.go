package channel

import "atm/internal/capability"

// Summary is the one-line identity every enumeration surface shows.
func (Cap) Summary() string {
	return "Channel registry capability: where personas communicate — repositories, Notion, Slack — recorded as identity and endpoints, with machine-local wiring and no credentials anywhere."
}

// Definition is channel's structured self-description; `atm capability
// channel guide` renders it.
func (Cap) Definition() capability.Definition {
	return capability.Definition{
		Identity: "A channel is how personas communicate with each other and with humans. ATM is the PHONE BOOK, NOT THE SWITCHBOARD: it records what a channel is and how to address it, and agents do all channel I/O themselves through their own tools. A channel is a PLACE CONTENT FLOWS, not a single address, so it is reached through one or more ENDPOINTS. This capability has NO LANES: nothing moves through it toward a finish.",
		Axes: []capability.Axis{
			{Namespace: "channel", Meaning: "The bare label a channel record carries. The medium left the label when a channel gained several endpoints — a record's identity lives in its payload. Records written before that move carry `channel:<type>` and stay fully readable, decoding as a single endpoint; the first write relabels them."},
		},
		State: capability.StateDoc{
			Key:   "channel",
			Intro: "TIER 1, the ledger record (synced). Its title is the unique handle, its description the purpose. Addresses are not credentials.",
			Fields: []capability.StateField{
				{Name: "name", Meaning: "the unique handle within the project. Canonical in the payload, so it survives a title edit."},
				{Name: "endpoints", Meaning: "every way the channel is reached: each carries a medium (`repo`, `notion`, `slack`), an address shaped for it, and a role."},
				{Name: "role_hint", Meaning: "the role an endpoint created for this channel takes by default. A profile declares it on a channel EXPECTATION — a handle and a purpose with no address at all."},
			},
		},
		Invariants: []string{
			"HOME is where content lands: at most one per channel, so \"the home endpoint\" always means something to a writer. BROADCAST endpoints receive a one-line reference to whatever landed in the home.",
			"AUTO-MIRRORING is the contract, not a convention: put the content in the home, put a reference in every broadcast, and when reading, scan them all.",
			"A channel reaches each medium at most once: re-adding a medium CORRECTS its address rather than duplicating it.",
			"Removing the last endpoint leaves the handle. A purpose with no address is a legitimate expectation waiting to be addressed.",
			"TIER 2 is this machine's wiring, per endpoint, in config.json: never synced, never an event, never a secret. A stamp records that one agent reached one endpoint, with a `kind` — `use` when real work reached it, `probe` when a read-only check did.",
			"TIER 3 DOES NOT EXIST. ATM stores no tokens, passwords or credentials, anywhere: authorization lives with the agent's own tooling.",
			"There is no rename: the handle is the channel's identity, referenced by wiring keys and by agents that resolved it once. Re-authoring under a new handle is `remove` plus `add`.",
			"Channel records are tasks only as plumbing: manage them exclusively through `atm channel`, never through raw task verbs.",
		},
		Converge: []string{
			"Every channel the project's work expects exists as a record with a purpose that says what belongs in it.",
			"Every expectation has at least one endpoint with a real address.",
			"Every endpoint this machine needs is wired here, and its stamp is fresh enough to believe.",
			"No credential appears anywhere in the ledger or the config.",
		},
	}
}
