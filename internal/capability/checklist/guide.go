package checklist

import "atm/internal/capability"

// Summary is the one-line identity every enumeration surface shows.
func (Cap) Summary() string {
	return "Checklist registry capability: name-keyed standing operating procedures with recursive steps, the project's own process for a specific job."
}

// Definition is checklist's structured self-description; `atm capability
// checklist guide` renders it.
func (Cap) Definition() capability.Definition {
	return capability.Definition{
		Identity: "A checklist is a free-standing, name-keyed standing operating procedure: this project's own process for a specific job — what happens, in what order, against which surfaces. It is a BRIEFING, NOT A TRACKER: it has no done or pending state, and compliance shows in the ledger, in the work journalled while following the steps. Checklists commonly name channels by handle; resolve a handle and do the I/O with your own tools — a checklist names a surface, it does not reach it for you. This capability has NO LANES: nothing moves through it toward a finish.",
		Axes: []capability.Axis{
			{Namespace: "checklist", Meaning: "The bare label a checklist record carries. The record's identity lives in its payload, not in the label — which is why `suits`, a many-valued fact, is payload rather than a label axis. Records written before that move carry `checklist:<persona>` instead and stay fully readable; the first write relabels them."},
		},
		State: capability.StateDoc{
			Key:   "checklist",
			Intro: "The record itself. Its title is the name, its description a fixed pointer, and its payload everything that makes it a checklist. Name is unique per project.",
			Fields: []capability.StateField{
				{Name: "name", Meaning: "the unique key within the project."},
				{Name: "purpose", Meaning: "what job this checklist is for — the line every selection surface shows."},
				{Name: "steps", Meaning: "a recursive tree; every node is `{text, children}`, rendered everywhere with nested numbering (1., 1.1, 1.2.1)."},
				{Name: "suits", Meaning: "the personas this checklist is a default fit for. A HINT, not ownership: any persona can run any checklist."},
				{Name: "requires", Meaning: "the capabilities and channel handles the checklist needs. Unmet requirements WARN, never block."},
				{Name: "target", Meaning: "what a dispatch of this action operates on: the project as a whole, or one task."},
				{Name: "targets", Meaning: "a label expression narrowing the tasks a task-target action may be dispatched on; empty offers every task."},
				{Name: "mode", Meaning: "the action's natural autonomy: eager starts executing, interactive waits for the human."},
				{Name: "origin", Meaning: "reset provenance: `user`, or the `<profile>@<version>` it was applied from."},
			},
		},
		Invariants: []string{
			"The document IS the record: `set` replaces it wholesale, and there is no per-field edit anywhere.",
			"Name and origin are identity and provenance — a `set` takes them from the existing record, never from the caller, so a local rewording cannot erase where a record came from.",
			"Legacy records relabel on the first write. Reads never rewrite anything.",
			"Two legacy records could share a name under different personas; by-name verbs then fail naming both task IDs, and `--task <ID>` disambiguates.",
		},
		Converge: []string{
			"Every dispatchable action the project means to run exists as a record, with a purpose findable at a glance.",
			"Steps are concrete and imperative, and every channel handle they name resolves.",
			"No checklist's `suits` names a persona the project does not have.",
			"Credentials, secrets and session-specific trivia appear in no step: those live in the work, never in a procedure.",
		},
	}
}
