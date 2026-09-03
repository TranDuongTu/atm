// Package profile is ATM's operating-content format: a profile is a named,
// versioned bundle of personas, checklists, and channel expectations that a
// project applies to get an operating model (DispatchV2 unit 4, ATM-bce933).
//
// The three-layer model this package sits in the middle of: a CAPABILITY is
// code — lanes, axes, verbs, invariants compiled into the binary; a PROFILE
// is named config — how a team uses those words, portable across projects
// and machines; PROJECT RECORDS are the applied state, stamped with the
// origin they came from and free to diverge afterwards.
//
// This package is the FORMAT and nothing else: it reads a profile directory
// or artifact into core's types, validates it, and packs it back out. It
// does no file I/O of its own — an fs.FS goes in, values come out — so the
// data lives in internal/core and keeping installed copies on disk is
// internal/store's job, beside the persona side store it already owns.
//
// Validation that depends on what this build actually knows — which
// capabilities exist — takes the answer as an argument, so nothing here
// imports internal/capability.
package profile

import (
	"errors"
	"fmt"
	"slices"

	"atm/internal/core"
)

// Format is the profile format version this build reads. A profile
// declaring anything else is refused rather than half-understood.
const Format = 1

// ValidateCapabilities checks required capabilities against the set this
// build actually knows. It is separate from Load on purpose: only the
// composition root knows the registry, and keeping the check out of the
// loader keeps this package below internal/capability in the import graph.
// Apply calls it before writing anything (plan §3.2 step 2).
func ValidateCapabilities(required, known []string) error {
	var problems []error
	for _, c := range required {
		if !slices.Contains(known, c) {
			problems = append(problems, fmt.Errorf("requires capability %q, which this build does not provide (known: %v)", c, known))
		}
	}
	return errors.Join(problems...)
}

// ValidateProfileCapabilities checks one profile's manifest against the
// capabilities known to the caller.
func ValidateProfileCapabilities(p *core.Profile, known []string) error {
	if err := ValidateCapabilities(p.Manifest.RequiresCapabilities, known); err != nil {
		return fmt.Errorf("profile %s: %w", p.Manifest.Ref(), err)
	}
	return nil
}
