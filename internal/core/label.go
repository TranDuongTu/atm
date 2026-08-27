// Package core is ATM's domain leaf: the label algebra every adapter shares.
//
// It imports nothing from this repository and nothing outside the standard
// library. That is a hard rule, not a preference — see
// docs/architecture/logical-components.md. In particular core does not know
// what a Task is; the grouping functions take a labelsOf accessor so the
// caller keeps its own type.
package core

import "strings"

// IsWildcard reports whether a label is a facet token — a token ending in ":*"
// (e.g. "ATM:status:*" or "ATM:*"). A facet token declares a facet: it groups
// (with --facets / GroupTasksErr) but does NOT filter a query by itself. To
// filter by namespace membership use a namespace predicate in an expression
// (e.g. --expr "status:*"); see resolve.go's evalAtom. The bare "*" tautology
// atom is NOT a facet token (no ":*" suffix) — it is a filter token resolved
// in evalAtom to "match every task".
func IsWildcard(label string) bool { return strings.HasSuffix(label, ":*") }

// LabelMatchesWildcard reports whether label falls under a facet token, e.g.
// "ATM:status:open" matches both "ATM:status:*" and "ATM:*". The match is a
// plain prefix test against the facet token minus its "*", so it does not
// require the prefix to end on a segment boundary.
func LabelMatchesWildcard(label, wildcard string) bool {
	return strings.HasPrefix(label, strings.TrimSuffix(wildcard, "*"))
}

// WildcardTokens returns the facet tokens of labels, in order. A facet token
// declares a facet for grouping (GroupTasksErr); it does not filter.
func WildcardTokens(labels []string) []string {
	var out []string
	for _, l := range labels {
		if IsWildcard(l) {
			out = append(out, l)
		}
	}
	return out
}

// FilterTokens returns the filter tokens of labels, in order: the
// non-facet tokens that restrict a query (concrete labels and board names).
// Together with WildcardTokens it partitions the input. Renamed from
// RestrictingTokens; "filter" reads as a noun next to "facet" and avoids the
// "restricting wildcard" oxymoron that confused ATM-8289dc.
func FilterTokens(labels []string) []string {
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
