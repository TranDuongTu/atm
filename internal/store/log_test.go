package store

import (
	"atm/internal/core"
	"fmt"
	"testing"
)

func TestIsIntegrity(t *testing.T) {
	if !core.IsIntegrity(fmt.Errorf("%w: x", core.ErrIntegrity)) {
		t.Fatal("core.IsIntegrity should match wrapped core.ErrIntegrity")
	}
}
