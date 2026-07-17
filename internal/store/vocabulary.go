package store

import (
	"atm/internal/core"
	"os"
	"path/filepath"
)

func (s *Store) vocabularyPath(code string) string {
	return filepath.Join(s.projectDir(code), "vocabulary.json")
}

// GetVocabulary reads <store>/projects/<CODE>/vocabulary.json. A missing file
// returns (nil, nil) so callers can treat it as the empty-state case. A
// malformed file returns the decode error.
func (s *Store) GetVocabulary(code string) (*Vocabulary, error) {
	var v Vocabulary
	if err := ReadJSON(s.vocabularyPath(code), &v); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}

// WriteVocabulary stamps UpdatedAt and writes vocabulary.json under the
// project's per-project lock. Actor is required.
func (s *Store) WriteVocabulary(code string, v *Vocabulary) error {
	if err := s.validateActor(v.Actor); err != nil {
		return err
	}
	v.UpdatedAt = core.Now()
	return s.WithLock(code, func() error {
		if err := os.MkdirAll(s.projectDir(code), 0o755); err != nil {
			return err
		}
		return WriteFileAtomic(s.vocabularyPath(code), v)
	})
}
