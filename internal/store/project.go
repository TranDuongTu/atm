package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func (s *Store) CreateProject(code, name, typeAxis string, labels []Label, repoPaths []string, actor string) (*Project, error) {
	if err := ValidateProjectCode(code); err != nil {
		return nil, err
	}
	if err := ValidateActorID(actor); err != nil {
		return nil, err
	}
	for _, l := range labels {
		if err := ValidateLabelName(l.Name); err != nil {
			return nil, err
		}
	}
	if typeAxis != "" && !namespaceHasLabel(labels, typeAxis) {
		return nil, fmt.Errorf("%w: type_axis namespace %q has no labels in set", ErrUsage, typeAxis)
	}

	var created *Project
	err := s.WithLock(code, func() error {
		return s.registerActorLazy(actor)
	})
	if err != nil {
		return nil, err
	}

	err = s.WithLock(code, func() error {
		if _, err := os.Stat(s.projectPath(code)); err == nil {
			return fmt.Errorf("%w: project %q already exists", ErrConflict, code)
		}
		now := Now()
		p := &Project{
			Code:      code,
			Name:      name,
			TypeAxis:  typeAxis,
			Labels:    dedupLabels(labels),
			NextTaskN: 1,
			RepoPaths: repoPaths,
			CreatedAt: now,
			CreatedBy: actor,
			UpdatedAt: now,
		}
		if err := os.MkdirAll(s.tasksDir(code), 0o755); err != nil {
			return err
		}
		return WriteJSON(s.projectPath(code), p)
	})
	if err != nil {
		return nil, err
	}
	created, err = s.GetProject(code)
	return created, err
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
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		p, err := s.GetProject(name[:len(name)-len(".json")])
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

func (s *Store) SetTypeAxis(code, namespace, actor string) error {
	if namespace != "" {
		var p *Project
		var err error
		err = s.WithLock(code, func() error {
			p, err = s.GetProject(code)
			return err
		})
		if err != nil {
			return err
		}
		if !namespaceHasLabel(p.Labels, namespace) {
			return fmt.Errorf("%w: type_axis namespace %q has no labels in set", ErrUsage, namespace)
		}
	}
	return s.mutateProject(code, actor, func(p *Project) {
		p.TypeAxis = namespace
	})
}

func (s *Store) RepoAdd(code, path, actor string) error {
	return s.mutateProject(code, actor, func(p *Project) {
		for _, r := range p.RepoPaths {
			if r == path {
				return
			}
		}
		p.RepoPaths = append(p.RepoPaths, path)
	})
}

func (s *Store) RepoRemove(code, path, actor string) error {
	return s.mutateProject(code, actor, func(p *Project) {
		out := p.RepoPaths[:0]
		for _, r := range p.RepoPaths {
			if r != path {
				out = append(out, r)
			}
		}
		p.RepoPaths = out
	})
}

func (s *Store) LabelAdd(code, name, description, actor string) error {
	if err := ValidateLabelName(name); err != nil {
		return err
	}
	return s.mutateProject(code, actor, func(p *Project) {
		for i, l := range p.Labels {
			if l.Name == name {
				if description != "" && l.Description != description {
					p.Labels[i].Description = description
				}
				return
			}
		}
		p.Labels = append(p.Labels, Label{Name: name, Description: description})
		sort.SliceStable(p.Labels, func(i, j int) bool { return p.Labels[i].Name < p.Labels[j].Name })
	})
}

type LabelRemoveResult struct {
	RetainedUsage int `json:"retained_usage"`
}

func (s *Store) LabelRemove(code, name, actor string) (*LabelRemoveResult, error) {
	var result *LabelRemoveResult
	err := s.mutateProjectRaw(code, actor, func(p *Project) error {
		idx := -1
		for i, l := range p.Labels {
			if l.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil
		}
		p.Labels = append(p.Labels[:idx], p.Labels[idx+1:]...)
		count, err := s.countTasksWithLabel(code, name)
		if err != nil {
			return err
		}
		result = &LabelRemoveResult{RetainedUsage: count}
		return nil
	})
	return result, err
}

func (s *Store) LabelList(code string) []Label {
	p, err := s.GetProject(code)
	if err != nil {
		return nil
	}
	return p.Labels
}

func (s *Store) mutateProject(code, actor string, fn func(p *Project)) error {
	return s.mutateProjectRaw(code, actor, func(p *Project) error {
		fn(p)
		return nil
	})
}

func (s *Store) mutateProjectRaw(code, actor string, fn func(p *Project) error) error {
	if err := ValidateActorID(actor); err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		if err := s.registerActorLazy(actor); err != nil {
			return err
		}
		p, err := s.GetProject(code)
		if err != nil {
			return err
		}
		if err := fn(p); err != nil {
			return err
		}
		p.UpdatedAt = Now()
		return WriteJSON(s.projectPath(code), p)
	})
}

func (s *Store) countTasksWithLabel(code, label string) (int, error) {
	entries, err := os.ReadDir(s.tasksDir(code))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		var t Task
		if err := ReadJSON(filepath.Join(s.tasksDir(code), e.Name()), &t); err != nil {
			continue
		}
		for _, l := range t.Labels {
			if l == label {
				count++
				break
			}
		}
	}
	return count, nil
}

func (s *Store) registerActorLazy(actor string) error {
	if _, err := os.Stat(s.actorsPath()); err != nil {
		_ = s.Init(s.Root)
	}
	return s.Register(actor, "")
}

func namespaceHasLabel(labels []Label, ns string) bool {
	for _, l := range labels {
		if len(l.Name) > len(ns) && l.Name[:len(ns)] == ns && l.Name[len(ns)] == ':' {
			return true
		}
	}
	return false
}

func dedupLabels(labels []Label) []Label {
	seen := map[string]bool{}
	out := make([]Label, 0, len(labels))
	for _, l := range labels {
		if seen[l.Name] {
			continue
		}
		seen[l.Name] = true
		out = append(out, l)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
