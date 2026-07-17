package store

import "atm/internal/store/fsio"

// WithLock delegates to the shared fsio primitive; the registry, nesting and
// cross-process semantics are unchanged (see fsio.WithLock).
func (s *Store) WithLock(code string, fn func() error) error {
	return fsio.WithLock(s.projectsDir(), code, fn)
}
