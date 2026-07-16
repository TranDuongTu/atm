// Package core is ATM's domain leaf: the label algebra every adapter shares.
//
// It imports nothing from this repository and nothing outside the standard
// library. That is a hard rule, not a preference — see
// docs/architecture/logical-components.md. In particular core does not know
// what a Task is; the grouping functions take a labelsOf accessor so the
// caller keeps its own type.
package core

import "strings"

// IsWildcard reports whether a label is a facet declaration — a token ending
// in ":*", e.g. "ATM:status:*" or "ATM:*". A wildcard declares a facet and
// does NOT restrict a query; see RestrictingTokens.
func IsWildcard(label string) bool { return strings.HasSuffix(label, ":*") }

// LabelMatchesWildcard reports whether label falls under wildcard, e.g.
// "ATM:status:open" matches both "ATM:status:*" and "ATM:*". The match is a
// plain prefix test against the wildcard minus its "*", so it does not require
// the prefix to end on a segment boundary.
func LabelMatchesWildcard(label, wildcard string) bool {
	return strings.HasPrefix(label, strings.TrimSuffix(wildcard, "*"))
}

// WildcardTokens returns the facet-declaring tokens of labels, in order.
func WildcardTokens(labels []string) []string {
	var out []string
	for _, l := range labels {
		if IsWildcard(l) {
			out = append(out, l)
		}
	}
	return out
}

// RestrictingTokens returns the query-restricting (non-wildcard) tokens of
// labels, in order. Together with WildcardTokens it partitions the input.
func RestrictingTokens(labels []string) []string {
	var out []string
	for _, l := range labels {
		if !IsWildcard(l) {
			out = append(out, l)
		}
	}
	return out
}

// FacetToken returns the wildcard label that facets a scope by a namespace:
// FacetToken("ATM", "status") == "ATM:status:*".
func FacetToken(scope, ns string) string { return scope + ":" + ns + ":*" }

// HasBareTag reports whether labels contains at least one unnamespaced (bare)
// label within scope — one whose suffix after "<scope>:" holds no further
// colon, e.g. "ATM:urgent".
func HasBareTag(scope string, labels []string) bool {
	for _, full := range labels {
		if !strings.HasPrefix(full, scope+":") {
			continue
		}
		if !strings.Contains(strings.TrimPrefix(full, scope+":"), ":") {
			return true
		}
	}
	return false
}

// IsNamespaceName reports whether name is a namespace label (e.g. "ATM:status:*"),
// whose membership is every label sharing its prefix.
func IsNamespaceName(name string) bool { return strings.HasSuffix(name, ":*") }
