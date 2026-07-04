package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
			LogSeq:    0,
		}
		// 1. Append project.created to log.
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionProjectCreated,
			Subject: Subject{Kind: "project", Code: code},
			Payload: mustMarshal(p),
		})
		if err != nil {
			return err
		}
		p.LogSeq = entry.Seq
		// 2. Seed default labels (appends label.upserted per default label).
		if err := s.seedLabelsLocked(code, actor, now); err != nil {
			return err
		}
		// 3. Write project cache.
		if err := os.MkdirAll(s.tasksDir(code), 0o755); err != nil {
			return err
		}
		if err := WriteJSON(s.projectPath(code), p); err != nil {
			return err
		}
		created = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func mustMarshal(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
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
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	return s.WithLock(code, func() error {
		p, err := s.GetProject(code)
		if err != nil {
			return err
		}
		p.Name = name
		now := Now()
		p.UpdatedAt = now
		p.UpdatedBy = actor
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionProjectNameChanged,
			Subject: Subject{Kind: "project", Code: code},
			Payload: mustMarshal(p),
		})
		if err != nil {
			return err
		}
		p.LogSeq = entry.Seq
		return WriteJSON(s.projectPath(code), p)
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
		p, err := s.GetProject(code)
		if err != nil {
			return err
		}
		now := Now()
		// 1. Append project.removed tombstone (payload = last state).
		_, _ = s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionProjectRemoved,
			Subject: Subject{Kind: "project", Code: code},
			Payload: mustMarshal(p),
		})
		// 2. Delete the project directory (including log.jsonl).
		_ = os.RemoveAll(s.projectDir(code))
		// 3. Delete the project cache file.
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
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionProjectNameChanged, // callers use SetProjectName directly; mutateProject retained for symmetry
			Subject: Subject{Kind: "project", Code: code},
			Payload: mustMarshal(p),
		})
		if err != nil {
			return err
		}
		p.LogSeq = entry.Seq
		return WriteJSON(s.projectPath(code), p)
	})
}
