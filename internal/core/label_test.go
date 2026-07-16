package core

import (
	"reflect"
	"testing"
)

func TestIsWildcard(t *testing.T) {
	for _, tc := range []struct {
		label string
		want  bool
	}{
		{"ATM:status:*", true},
		{"ATM:*", true},
		{"ATM:status:open", false},
		{"ATM:urgent", false},
		{"", false},
		{"*", false},        // no ":" prefix — not a wildcard token
		{"ATM:status:", false},
	} {
		if got := IsWildcard(tc.label); got != tc.want {
			t.Errorf("IsWildcard(%q) = %v, want %v", tc.label, got, tc.want)
		}
	}
}

func TestLabelMatchesWildcard(t *testing.T) {
	for _, tc := range []struct {
		label, wildcard string
		want            bool
	}{
		{"ATM:status:open", "ATM:status:*", true},
		{"ATM:status:open", "ATM:*", true},
		{"ATM:type:bug", "ATM:status:*", false},
		{"OTHER:status:open", "ATM:status:*", false},
		// Prefix semantics: the namespace segment is not required to end at
		// a colon boundary. Documents today's behavior.
		{"ATM:statuses:open", "ATM:status:*", false},
		{"ATM:status:open:sub", "ATM:status:*", true},
	} {
		if got := LabelMatchesWildcard(tc.label, tc.wildcard); got != tc.want {
			t.Errorf("LabelMatchesWildcard(%q, %q) = %v, want %v", tc.label, tc.wildcard, got, tc.want)
		}
	}
}

func TestWildcardAndRestrictingTokensPartition(t *testing.T) {
	labels := []string{"ATM:status:*", "ATM:type:bug", "ATM:*", "ATM:urgent"}
	if got, want := WildcardTokens(labels), []string{"ATM:status:*", "ATM:*"}; !reflect.DeepEqual(got, want) {
		t.Errorf("WildcardTokens = %v, want %v", got, want)
	}
	if got, want := RestrictingTokens(labels), []string{"ATM:type:bug", "ATM:urgent"}; !reflect.DeepEqual(got, want) {
		t.Errorf("RestrictingTokens = %v, want %v", got, want)
	}
	if WildcardTokens(nil) != nil || RestrictingTokens(nil) != nil {
		t.Error("nil input must yield nil output")
	}
}

func TestFacetToken(t *testing.T) {
	if got := FacetToken("ATM", "status"); got != "ATM:status:*" {
		t.Errorf("FacetToken = %q, want ATM:status:*", got)
	}
}

func TestHasBareTag(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels []string
		want   bool
	}{
		{"namespaced only", []string{"ATM:status:open"}, false},
		{"bare", []string{"ATM:urgent"}, true},
		{"none", nil, false},
		{"mixed", []string{"ATM:status:open", "ATM:urgent"}, true},
		{"foreign scope", []string{"OTHER:urgent"}, false},
	} {
		if got := HasBareTag("ATM", tc.labels); got != tc.want {
			t.Errorf("%s: HasBareTag(ATM, %v) = %v, want %v", tc.name, tc.labels, got, tc.want)
		}
	}
}
