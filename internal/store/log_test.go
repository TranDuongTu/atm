package store

import (
	"fmt"
	"testing"
)

func TestIsIntegrity(t *testing.T) {
	if !IsIntegrity(fmt.Errorf("%w: x", ErrIntegrity)) {
		t.Fatal("IsIntegrity should match wrapped ErrIntegrity")
	}
}
