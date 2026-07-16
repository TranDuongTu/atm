package core

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorPredicatesMatchWrapped(t *testing.T) {
	cases := []struct {
		err  error
		pred func(error) bool
	}{
		{fmt.Errorf("task %q: %w", "ATM-1", ErrNotFound), IsNotFound},
		{fmt.Errorf("stale: %w", ErrConflict), IsConflict},
		{fmt.Errorf("log: %w", ErrIntegrity), IsIntegrity},
		{fmt.Errorf("bad flag: %w", ErrUsage), IsUsage},
	}
	for i, c := range cases {
		if !c.pred(c.err) {
			t.Fatalf("case %d: predicate did not match wrapped sentinel", i)
		}
		if c.pred(errors.New("other")) {
			t.Fatalf("case %d: predicate matched unrelated error", i)
		}
	}
}
