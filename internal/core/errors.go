package core

import "errors"

// Domain error kinds. Adapters classify failures with the Is* predicates;
// the store wraps these sentinels so errors.Is matches across layers.
var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrIntegrity = errors.New("integrity")
	ErrUsage     = errors.New("usage")
)

func IsNotFound(err error) bool  { return errors.Is(err, ErrNotFound) }
func IsConflict(err error) bool  { return errors.Is(err, ErrConflict) }
func IsIntegrity(err error) bool { return errors.Is(err, ErrIntegrity) }
func IsUsage(err error) bool     { return errors.Is(err, ErrUsage) }
