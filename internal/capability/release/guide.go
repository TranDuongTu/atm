package release

import "atm/internal/capability"

// Summary is the one-line identity every enumeration surface shows.
func (Cap) Summary() string {
	return "Release registry capability: durable records of what shipped together — a version label, a roster, and a comment thread as the release log."
}

// Definition is release's structured self-description; `atm capability
// release guide` renders it.
func (Cap) Definition() capability.Definition {
	return capability.Definition{
		Identity: "Durable records of what shipped together. A release is an ordinary task: its comment thread is the release log, its label is the version, and its payload holds the roster. It has NO LANES — no inbox, no wiring, no sockets, no place in the capability switcher, and no triage loop. It is reached through its verbs, through explicit dispatch, and through search: \"what shipped in v1.2\".",
		Axes: []capability.Axis{
			{Namespace: Namespace, Meaning: "The only namespace this capability owns. Its values are minted on demand: one per cut version, plus the shipped marker. Versions are sanitized into the label grammar — dots and underscores become dashes, so `v1.2` becomes `release:v1-2`. Anything the grammar cannot carry is refused at `cut` rather than written and bounced.",
				Values: []capability.AxisValue{
					{Value: "<version>", Meaning: "the sanitized version this task belongs to. Carried by the container and by every member."},
					{Value: ValueShipped, Meaning: "this task's change is shipped. `ship` stamps it on the container AND on every member, so a member's own board shows it without anyone reading the container."},
				}},
		},
		State: capability.StateDoc{
			Key:   CapabilityName,
			Intro: "The container/member topology, stored at both ends. A CONTAINER is the release record `cut` creates; a MEMBER is a task `include` added to it.",
			Fields: []capability.StateField{
				{Name: "version", Meaning: "on the container: the sanitized version value."},
				{Name: "members", Meaning: "on the container: the roster of task IDs in this release."},
				{Name: "release_of", Meaning: "on a member: the container's task ID. Its presence is what MAKES a task a member."},
			},
		},
		Invariants: []string{
			"A version that cannot be carried by the label grammar is refused at `cut`, not silently rewritten.",
			"`exclude` is refused after `ship`: editing a shipped record rewrites history rather than correcting it — cut the next version instead.",
			"A shipped release keeps its roster.",
		},
		Converge: []string{
			"Every shipped change belongs to exactly one release container.",
			"Every container's roster and its members' `release_of` agree.",
			"A shipped release's log says what went out and what it means for a reader.",
		},
	}
}
