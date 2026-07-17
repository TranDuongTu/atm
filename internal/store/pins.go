package store

import (
	"atm/internal/core"
	"os"
	"path/filepath"
)

func (s *Store) pinsPath(code string) string {
	return filepath.Join(s.projectDir(code), "pins.json")
}

// GetPins reads <store>/projects/<CODE>/pins.json. A missing file returns
// (nil, nil) so callers can treat it as the empty-state case. A malformed
// file returns the decode error.
func (s *Store) GetPins(code string) (*Pins, error) {
	var p Pins
	if err := ReadJSON(s.pinsPath(code), &p); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// WritePins stamps UpdatedAt and writes pins.json under the project's
// per-project lock. Actor is required.
func (s *Store) WritePins(code string, p *Pins) error {
	if err := s.validateActor(p.Actor); err != nil {
		return err
	}
	p.UpdatedAt = core.Now()
	return s.WithLock(code, func() error {
		if err := os.MkdirAll(s.projectDir(code), 0o755); err != nil {
			return err
		}
		return WriteFileAtomic(s.pinsPath(code), p)
	})
}
