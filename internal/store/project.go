package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func (s *Store) CreateProject(code, name, actor string) (*Project, error) {
	if err := ValidateProjectCode(code); err != nil {
		return nil, err
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	var created *Project
	err := s.WithLock(code, func() error {
		if _, err := os.Stat(s.projectPath(code)); err == nil {
			return fmt.Errorf("%w: project %q already exists", ErrConflict, code)
		}
		now := Now()
		p := &Project{
			Code:      code,
			Name:      name,
			NextTaskN: 1,
			CreatedAt: now,
			CreatedBy: actor,
			UpdatedAt: now,
			UpdatedBy: actor,
		}
		if err := os.MkdirAll(s.tasksDir(code), 0o755); err != nil {
			return err
		}
		p.History = []HistoryEntry{{ID: "h1", Action: "created", Actor: actor, At: now, Meta: map[string]any{}}}
		p.NextHistoryN = 2
		if err := WriteJSON(s.projectPath(code), p); err != nil {
			return err
		}
		created = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Seed the default label set (idempotent; outside the project-create
	// lock — SeedLabels takes its own project lock). A fresh project has
	// all 17 default labels with descriptions the moment it exists.
	if seedErr := s.SeedLabels(code, actor); seedErr != nil {
		return nil, seedErr
	}
	return created, nil
}

func (s *Store) GetProject(code string) (*Project, error) {
	var p Project
	if err := ReadJSON(s.projectPath(code), &p); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: project %q", ErrNotFound, code)
		}
		return nil, err
	}
	return &p, nil
}

func (s *Store) ListProjects() []*Project {
	entries, err := os.ReadDir(s.projectsDir())
	if err != nil {
		return nil
	}
	var out []*Project
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		p, err := s.GetProject(e.Name()[:len(e.Name())-len(".json")])
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

func (s *Store) SetProjectName(code, name, actor string) error {
	return s.mutateProject(code, actor, func(p *Project) {
		p.Name = name
	})
}

func (s *Store) RemoveProject(code, actor string) error {
	if err := s.hasTasksGuard(code); err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		if err := s.hasTasksGuard(code); err != nil {
			return err
		}
		if _, err := os.Stat(s.projectPath(code)); os.IsNotExist(err) {
			return fmt.Errorf("%w: project %q", ErrNotFound, code)
		}
		_ = os.RemoveAll(s.tasksDir(code))
		return os.Remove(s.projectPath(code))
	})
}

func (s *Store) hasTasksGuard(code string) error {
	entries, err := os.ReadDir(s.tasksDir(code))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			return fmt.Errorf("%w: project %q has tasks — remove tasks first", ErrConflict, code)
		}
	}
	return nil
}

func (s *Store) mutateProject(code, actor string, fn func(p *Project)) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	return s.WithLock(code, func() error {
		p, err := s.GetProject(code)
		if err != nil {
			return err
		}
		fn(p)
		now := Now()
		p.UpdatedAt = now
		p.UpdatedBy = actor
		p.appendHistoryAt("name-changed", actor, now, map[string]any{})
		return WriteJSON(s.projectPath(code), p)
	})
}

func (p *Project) appendHistoryAt(action, actor string, at time.Time, meta map[string]any) {
	n := p.NextHistoryN
	if n == 0 {
		n = len(p.History) + 1
	}
	p.History = append(p.History, HistoryEntry{
		ID: fmt.Sprintf("h%d", n), Action: action, Actor: actor, At: at, Meta: meta,
	})
	p.NextHistoryN = n + 1
}