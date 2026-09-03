package profile

import (
	"errors"
	"fmt"
	"slices"
)

// selfConsistency checks the rules that need the whole document set: no
// duplicate names within a kind, every suits entry naming a persona the
// profile ships, every required channel declared, and every required
// capability declared by the manifest. These are the invariants that make a
// profile a CLOSED WORLD — one an apply can satisfy without asking the
// project what it already has.
func (p *Profile) selfConsistency() []error {
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

// ValidateCapabilities checks the manifest's required capabilities against
// the set this build actually knows. It is separate from Load on purpose:
// only the composition root knows the registry, and keeping the check out of
// the loader keeps this package below internal/capability in the import
// graph. Apply calls it before writing anything (plan §3.2 step 2).
func ValidateCapabilities(required, known []string) error {
	var problems []error
	for _, c := range required {
		if !slices.Contains(known, c) {
			problems = append(problems, fmt.Errorf("requires capability %q, which this build does not provide (known: %v)", c, known))
		}
	}
	return errors.Join(problems...)
}

// ValidateCapabilities checks this profile's manifest against the
// capabilities known to the caller.
func (p *Profile) ValidateCapabilities(known []string) error {
	if err := ValidateCapabilities(p.Manifest.RequiresCapabilities, known); err != nil {
		return fmt.Errorf("profile %s: %w", p.Manifest.Ref(), err)
	}
	return nil
}
