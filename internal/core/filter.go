package core

import "strings"

// ParseFilter splits a filter string into its label tokens. Tokens ending
// ":*" are facets; the rest restrict. Empty or blank input yields nil.
func ParseFilter(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Fields(s)
}

// FilterHasToken reports whether token is one of filter's space-separated
// fields.
func FilterHasToken(filter, token string) bool {
	for _, f := range strings.Fields(filter) {
		if f == token {
			return true
		}
	}
	return false
}

// FilterAddToken appends token to filter, single-space separated, unless it is
// already present.
func FilterAddToken(filter, token string) string {
	if FilterHasToken(filter, token) {
		return filter
	}
	if strings.TrimSpace(filter) == "" {
		return token
	}
	return filter + " " + token
}

// FilterRemoveToken removes every occurrence of token from filter, rejoining
// the remainder with single spaces.
func FilterRemoveToken(filter, token string) string {
	var kept []string
	for _, f := range strings.Fields(filter) {
		if f != token {
			kept = append(kept, f)
		}
	}
	return strings.Join(kept, " ")
}
