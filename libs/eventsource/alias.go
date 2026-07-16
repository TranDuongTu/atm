package eventsource

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// MintTaskAlias mints the stored display alias for a new task (L1):
// "<CODE>-" + the first 6 lowercase hex chars of the creation event's
// digest, extended to the shortest length unambiguous among the aliases
// the minting replica currently holds (taken). The alias is stored on the
// creation event, immutable forever, and need not be globally unique —
// local extension only keeps local lookups convenient.
func MintTaskAlias(projectCode, eventID string, taken func(string) bool) string {
	return mintAlias(projectCode+"-", eventID, 6, taken)
}

// MintCommentAlias mints "<task-alias>-c" + ≥4 hex chars: a comment's
// prefix need only disambiguate within its task.
func MintCommentAlias(taskAlias, eventID string, taken func(string) bool) string {
	return mintAlias(taskAlias+"-c", eventID, 4, taken)
}

func mintAlias(prefix, eventID string, minLen int, taken func(string) bool) string {
	hex := strings.TrimPrefix(eventID, "sha256:")
	for n := minLen; n <= len(hex); n++ {
		alias := prefix + hex[:n]
		if !taken(alias) {
			return alias
		}
	}
	return prefix + hex
}

// Match is one resolution candidate.
type Match struct {
	Kind  string // "project", "task", or "comment"
	ID    string
	Alias string
}

// ErrNoMatch reports that an input resolved to nothing.
var ErrNoMatch = errors.New("eventsource: no entity matches")

// AmbiguousError reports an input matching more than one entity. Callers
// print the candidates and let the human disambiguate with an identity
// prefix — never silently pick one (L1-4).
type AmbiguousError struct {
	Input   string
	Matches []Match
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("eventsource: ambiguous %q — %d entities match", e.Input, len(e.Matches))
}

// Resolve maps a user-supplied string to an entity: exact alias match
// first, then unique identity prefix (with or without "sha256:"),
// git-style. Tombstoned entities resolve too — a restore must be able to
// name its target.
func (s *State) Resolve(input string) (Match, error) {
	all := make([]Match, 0, len(s.Projects)+len(s.Tasks)+len(s.Comments))
	for _, p := range s.Projects {
		all = append(all, Match{Kind: "project", ID: p.ID, Alias: p.Alias})
	}
	for _, t := range s.Tasks {
		all = append(all, Match{Kind: "task", ID: t.ID, Alias: t.Alias})
	}
	for _, c := range s.Comments {
		all = append(all, Match{Kind: "comment", ID: c.ID, Alias: c.Alias})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })

	var found []Match
	for _, m := range all {
		if m.Alias == input {
			found = append(found, m)
		}
	}
	if len(found) == 0 {
		hexInput := strings.TrimPrefix(input, "sha256:")
		if hexInput != "" {
			for _, m := range all {
				if strings.HasPrefix(strings.TrimPrefix(m.ID, "sha256:"), hexInput) {
					found = append(found, m)
				}
			}
		}
	}
	switch len(found) {
	case 0:
		return Match{}, fmt.Errorf("%w: %q", ErrNoMatch, input)
	case 1:
		return found[0], nil
	default:
		return Match{}, &AmbiguousError{Input: input, Matches: found}
	}
}
