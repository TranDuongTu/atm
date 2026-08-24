// Package release is the release registry capability: durable records of what
// shipped together, kept on the same task substrate as everything else.
//
// It is a REGISTRY capability, not a flow one — the distinction is mechanical:
// it does not implement capability.Flow. So it has no lanes, no wiring, no
// inbox to triage, and no place in the capability switcher. It is used through
// its verbs, the manager, and search.
package release

import "regexp"

// CapabilityName is both the capability identity and the metadata key this
// capability owns on a task.
const CapabilityName = "release"

// Namespace is the single label namespace this capability owns. Its values are
// created on demand by `include` — one per cut version — plus the fixed
// ValueShipped.
const Namespace = "release"

// ValueShipped marks a container and its members as shipped.
const ValueShipped = "done"

// versionValueRe is the label-VALUE grammar this capability must satisfy after
// sanitizing a version. The store validates full label names independently;
// the capability fence forbids importing that validator, so the rule is
// restated here and the store remains the final word.
var versionValueRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
