package store

import (
	"atm/internal/core"
	"testing"
)

func TestValidateLabelNameAcceptsNamespaceAndBoardNames(t *testing.T) {
	ok := []string{"ATM:stale", "ATM:next-sprint", "ATM:status:open", "ATM:status:*"}
	for _, n := range ok {
		if err := ValidateLabelName(n); err != nil {
			t.Errorf("ValidateLabelName(%q) = %v, want nil", n, err)
		}
	}
	bad := []string{"ATM:status:open:*", "ATM:*", "ATM:Status:open", "atm:status:open", "ATM"}
	for _, n := range bad {
		if err := ValidateLabelName(n); err == nil {
			t.Errorf("ValidateLabelName(%q) = nil, want error", n)
		}
	}
}

func TestIsNamespaceName(t *testing.T) {
	if !core.IsNamespaceName("ATM:status:*") {
		t.Error("ATM:status:* should be a namespace name")
	}
	if core.IsNamespaceName("ATM:status:open") || core.IsNamespaceName("ATM:next-sprint") {
		t.Error("non-:* names are not namespace names")
	}
}
