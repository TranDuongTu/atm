package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"atm/internal/actor"
)

func (s *Store) aliasPath() string { return filepath.Join(s.Root, "actor-aliases.json") }

func (s *Store) LoadAliases() (actor.AliasMap, error) {
	m := actor.AliasMap{}
	err := ReadJSON(s.aliasPath(), &m)
	if err != nil {
		if os.IsNotExist(err) {
			return actor.AliasMap{}, nil
		}
		return nil, err
	}
	return m, nil
}

func (s *Store) SetAlias(raw string, e actor.AliasEntry) error {
	return s.WithLock("actor-aliases", func() error {
		m, err := s.LoadAliases()
		if err != nil {
			return err
		}
		m[raw] = e
		return WriteFileAtomic(s.aliasPath(), m)
	})
}

func (s *Store) RemoveAlias(raw string) error {
	return s.WithLock("actor-aliases", func() error {
		m, err := s.LoadAliases()
		if err != nil {
			return err
		}
		delete(m, raw)
		return WriteFileAtomic(s.aliasPath(), m)
	})
}

type MigrationResult struct {
	Seeded []string
	Added  map[string]actor.AliasEntry
}

// MigrateActors seeds the built-in personas and generates alias entries for
// distinct legacy actor strings found across all project logs. Idempotent:
// existing alias entries (including user overrides) are never overwritten.
// dryRun computes the result without writing personas or aliases.
func (s *Store) MigrateActors(dryRun bool) (*MigrationResult, error) {
	res := &MigrationResult{Added: map[string]actor.AliasEntry{}}

	// 1. Personas.
	if dryRun {
		for _, sp := range seedPersonaNamesMissing(s) {
			res.Seeded = append(res.Seeded, sp)
		}
	} else {
		added, err := s.SeedPersonas("migrate")
		if err != nil {
			return nil, err
		}
		res.Seeded = added
	}
	sort.Strings(res.Seeded)

	// 2. Distinct legacy actors across all project logs.
	existing, err := s.LoadAliases()
	if err != nil {
		return nil, err
	}
	codes, err := s.projectCodesOnDisk()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, code := range codes {
		entries, err := s.ReadLog(code)
		if err != nil && !IsIntegrity(err) {
			return nil, err
		}
		for _, e := range entries {
			raw := e.Actor
			if raw == "" || seen[raw] || strings.Contains(raw, "@") {
				continue
			}
			seen[raw] = true
			if _, ok := existing[raw]; ok {
				continue // never overwrite
			}
			if ent, ok := actor.LegacyAlias(raw); ok {
				res.Added[raw] = ent
			}
		}
	}

	// 3. Persist (unless dry-run).
	if !dryRun && len(res.Added) > 0 {
		err = s.WithLock("actor-aliases", func() error {
			m, err := s.LoadAliases()
			if err != nil {
				return err
			}
			for raw, ent := range res.Added {
				if _, ok := m[raw]; !ok {
					m[raw] = ent
				}
			}
			return WriteFileAtomic(s.aliasPath(), m)
		})
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

// seedPersonaNamesMissing returns built-in persona names not yet on disk
// (used to preview seeding under dry-run without creating files).
func seedPersonaNamesMissing(s *Store) []string {
	var missing []string
	for _, sp := range seedPersonas() {
		if _, err := s.GetPersona(sp); IsNotFound(err) {
			missing = append(missing, sp)
		}
	}
	return missing
}
