package profile

import (
	"fmt"
	"slices"

	"atm/internal/core"
)

// selfConsistency checks the rules that need the whole document set: no
// duplicate names within a kind, every suits entry naming a persona the
// profile ships, every required channel declared, and every required
// capability declared by the manifest. These are the invariants that make a
// profile a CLOSED WORLD — one an apply can satisfy without asking the
// project what it already has.
func selfConsistency(p *core.Profile) []error {
	var problems []error

	personas := map[string]bool{}
	for _, x := range p.Personas {
		if personas[x.Name] {
			problems = append(problems, fmt.Errorf("persona %s: declared twice", x.Name))
		}
		personas[x.Name] = true
	}
	channels := map[string]bool{}
	for _, x := range p.Channels {
		if channels[x.Name] {
			problems = append(problems, fmt.Errorf("channel %s: declared twice", x.Name))
		}
		channels[x.Name] = true
	}
	seenChecklist := map[string]bool{}
	declared := p.Manifest.RequiresCapabilities

	for _, c := range p.Checklists {
		if seenChecklist[c.Name] {
			problems = append(problems, fmt.Errorf("checklist %s: declared twice", c.Name))
		}
		seenChecklist[c.Name] = true

		for _, s := range c.Suits {
			if !personas[s] {
				problems = append(problems, fmt.Errorf("checklist %s: suits %q names no persona in this profile — the action would be undispatchable", c.Name, s))
			}
		}
		for _, h := range c.Requires.Channels {
			if !channels[h] {
				problems = append(problems, fmt.Errorf("checklist %s: requires_channels %q is not declared in channels/", c.Name, h))
			}
		}
		for _, cap := range c.Requires.Capabilities {
			if !slices.Contains(declared, cap) {
				problems = append(problems, fmt.Errorf("checklist %s: requires_capabilities %q is not in the manifest's requires_capabilities — apply enables the manifest's set, so this one would never be met", c.Name, cap))
			}
		}
	}
	return problems
}
