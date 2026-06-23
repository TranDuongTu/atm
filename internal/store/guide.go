package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type GuideStatusResult struct {
	Coverage  GuideCoverage    `json:"coverage"`
	Freshness []GuideFreshness `json:"freshness"`
}

type GuideCoverage struct {
	EmptySections []string `json:"empty_sections"`
	TotalSections int      `json:"total_sections"`
	TotalRefs     int      `json:"total_refs"`
}

type GuideFreshness struct {
	Section   string `json:"section"`
	Kind      string `json:"kind"`
	Target    string `json:"target"`
	State     string `json:"state"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func (s *Store) GuideGet(code string) (*Guide, error) {
	p, err := s.GetProject(code)
	if err != nil {
		return nil, err
	}
	return p.Guide, nil
}

func (s *Store) GuideSectionAdd(code, name, actor string) error {
	if name == "" {
		return fmt.Errorf("%w: section name is required", ErrUsage)
	}
	return s.mutateProjectRaw(code, actor, func(p *Project) error {
		g := s.guideForEdit(p)
		if sectionExists(g.Sections, name) {
			return fmt.Errorf("%w: section %q already exists", ErrConflict, name)
		}
		g.Sections = append(g.Sections, GuideSection{Name: name, Refs: []GuideRef{}})
		s.touchGuide(p, g, actor, "section-added", map[string]any{"section": name})
		return nil
	})
}

func (s *Store) GuideSectionRename(code, name, newName, actor string) error {
	if newName == "" {
		return fmt.Errorf("%w: section name is required", ErrUsage)
	}
	return s.mutateProjectRaw(code, actor, func(p *Project) error {
		g := s.guideForEdit(p)
		idx := sectionIndex(g.Sections, name)
		if idx < 0 {
			return fmt.Errorf("%w: section %q", ErrNotFound, name)
		}
		if name != newName && sectionExists(g.Sections, newName) {
			return fmt.Errorf("%w: section %q already exists", ErrConflict, newName)
		}
		g.Sections[idx].Name = newName
		s.touchGuide(p, g, actor, "section-renamed", map[string]any{"from": name, "to": newName})
		return nil
	})
}

func (s *Store) GuideSectionRemove(code, name, actor string) error {
	return s.mutateProjectRaw(code, actor, func(p *Project) error {
		g := p.Guide
		if g == nil {
			return nil
		}
		idx := sectionIndex(g.Sections, name)
		if idx < 0 {
			return fmt.Errorf("%w: section %q", ErrNotFound, name)
		}
		g.Sections = append(g.Sections[:idx], g.Sections[idx+1:]...)
		s.touchGuide(p, g, actor, "section-removed", map[string]any{"section": name})
		return nil
	})
}

func (s *Store) GuideSectionMove(code, name, before, actor string) error {
	return s.mutateProjectRaw(code, actor, func(p *Project) error {
		g := p.Guide
		if g == nil {
			return fmt.Errorf("%w: section %q", ErrNotFound, name)
		}
		idx := sectionIndex(g.Sections, name)
		if idx < 0 {
			return fmt.Errorf("%w: section %q", ErrNotFound, name)
		}
		sec := g.Sections[idx]
		g.Sections = append(g.Sections[:idx], g.Sections[idx+1:]...)
		var insertAt int
		if before == "" {
			insertAt = len(g.Sections)
		} else {
			bi := sectionIndex(g.Sections, before)
			if bi < 0 {
				return fmt.Errorf("%w: section %q", ErrNotFound, before)
			}
			insertAt = bi
		}
		g.Sections = append(g.Sections[:insertAt], append([]GuideSection{sec}, g.Sections[insertAt:]...)...)
		s.touchGuide(p, g, actor, "section-moved", map[string]any{"section": name, "before": before})
		return nil
	})
}

func (s *Store) GuideRefAdd(code, section, kind, target, actor string) error {
	if kind != "task" && kind != "file" {
		return fmt.Errorf("%w: ref kind must be task or file", ErrUsage)
	}
	if target == "" {
		return fmt.Errorf("%w: ref target is required", ErrUsage)
	}
	if kind == "file" && !filepath.IsAbs(target) {
		return fmt.Errorf("%w: file ref target must be an absolute path", ErrUsage)
	}
	if kind == "task" {
		if _, _, ok := ParseTaskID(target); !ok {
			return fmt.Errorf("%w: invalid task id %q", ErrUsage, target)
		}
		tCode, _, _ := ParseTaskID(target)
		if tCode != code {
			return fmt.Errorf("%w: task %q not in project %q", ErrUsage, target, code)
		}
	}
	return s.mutateProjectRaw(code, actor, func(p *Project) error {
		g := s.guideForEdit(p)
		idx := sectionIndex(g.Sections, section)
		if idx < 0 {
			return fmt.Errorf("%w: section %q", ErrNotFound, section)
		}
		if kind == "task" {
			if _, err := s.GetTask(target); err != nil {
				return err
			}
		}
		for _, r := range g.Sections[idx].Refs {
			if r.Kind == kind && r.Target == target {
				return fmt.Errorf("%w: ref %s:%s already in section %q", ErrConflict, kind, target, section)
			}
		}
		g.Sections[idx].Refs = append(g.Sections[idx].Refs, GuideRef{Kind: kind, Target: target})
		s.touchGuide(p, g, actor, "ref-added", map[string]any{"section": section, "kind": kind, "target": target})
		return nil
	})
}

func (s *Store) GuideRefRemove(code, section, kind, target, actor string) error {
	return s.mutateProjectRaw(code, actor, func(p *Project) error {
		g := p.Guide
		if g == nil {
			return nil
		}
		idx := sectionIndex(g.Sections, section)
		if idx < 0 {
			return fmt.Errorf("%w: section %q", ErrNotFound, section)
		}
		refs := g.Sections[idx].Refs
		ri := -1
		for i, r := range refs {
			if r.Kind == kind && r.Target == target {
				ri = i
				break
			}
		}
		if ri < 0 {
			return fmt.Errorf("%w: ref %s:%s in section %q", ErrNotFound, kind, target, section)
		}
		g.Sections[idx].Refs = append(refs[:ri], refs[ri+1:]...)
		s.touchGuide(p, g, actor, "ref-removed", map[string]any{"section": section, "kind": kind, "target": target})
		return nil
	})
}

func (s *Store) GuideRefMove(code, section, kind, target, before, actor string) error {
	return s.mutateProjectRaw(code, actor, func(p *Project) error {
		g := p.Guide
		if g == nil {
			return fmt.Errorf("%w: section %q", ErrNotFound, section)
		}
		idx := sectionIndex(g.Sections, section)
		if idx < 0 {
			return fmt.Errorf("%w: section %q", ErrNotFound, section)
		}
		refs := g.Sections[idx].Refs
		ri := -1
		for i, r := range refs {
			if r.Kind == kind && r.Target == target {
				ri = i
				break
			}
		}
		if ri < 0 {
			return fmt.Errorf("%w: ref %s:%s in section %q", ErrNotFound, kind, target, section)
		}
		ref := refs[ri]
		refs = append(refs[:ri], refs[ri+1:]...)
		var insertAt int
		if before == "" {
			insertAt = len(refs)
		} else {
			bi := -1
			for i, r := range refs {
				if r.Target == before {
					bi = i
					break
				}
			}
			if bi < 0 {
				return fmt.Errorf("%w: ref target %q in section %q", ErrNotFound, before, section)
			}
			insertAt = bi
		}
		refs = append(refs[:insertAt], append([]GuideRef{ref}, refs[insertAt:]...)...)
		g.Sections[idx].Refs = refs
		s.touchGuide(p, g, actor, "ref-moved", map[string]any{"section": section, "kind": kind, "target": target, "before": before})
		return nil
	})
}

func (s *Store) GuideSetFreshness(code, threshold, actor string) error {
	return s.mutateProjectRaw(code, actor, func(p *Project) error {
		if threshold == "unset" {
			p.GuideFreshnessThreshold = ""
			s.touchGuide(p, p.Guide, actor, "freshness-unset", map[string]any{})
			return nil
		}
		d, err := time.ParseDuration(threshold)
		if err != nil {
			return fmt.Errorf("%w: invalid duration %q: %v", ErrUsage, threshold, err)
		}
		if d <= 0 {
			return fmt.Errorf("%w: duration must be positive", ErrUsage)
		}
		p.GuideFreshnessThreshold = threshold
		s.touchGuide(p, p.Guide, actor, "freshness-set", map[string]any{"threshold": threshold})
		return nil
	})
}

func (s *Store) GuideStatus(code string) (*GuideStatusResult, error) {
	p, err := s.GetProject(code)
	if err != nil {
		return nil, err
	}
	res := &GuideStatusResult{
		Coverage:  GuideCoverage{EmptySections: []string{}},
		Freshness: []GuideFreshness{},
	}
	if p.Guide == nil {
		return res, nil
	}
	var threshold time.Duration
	thresholdSet := false
	if p.GuideFreshnessThreshold != "" {
		if d, err := time.ParseDuration(p.GuideFreshnessThreshold); err == nil && d > 0 {
			threshold = d
			thresholdSet = true
		}
	}
	now := Now()
	res.Coverage.TotalSections = len(p.Guide.Sections)
	for _, sec := range p.Guide.Sections {
		res.Coverage.TotalRefs += len(sec.Refs)
		if len(sec.Refs) == 0 {
			res.Coverage.EmptySections = append(res.Coverage.EmptySections, sec.Name)
		}
		for _, r := range sec.Refs {
			res.Freshness = append(res.Freshness, s.freshnessForRef(code, sec.Name, r, thresholdSet, threshold, now))
		}
	}
	return res, nil
}

func (s *Store) freshnessForRef(code, section string, r GuideRef, thresholdSet bool, threshold time.Duration, now time.Time) GuideFreshness {
	f := GuideFreshness{Section: section, Kind: r.Kind, Target: r.Target}
	switch r.Kind {
	case "task":
		t, err := s.GetTask(r.Target)
		if err != nil || t == nil {
			f.State = "missing"
			return f
		}
		if !thresholdSet {
			f.State = "unknown"
			f.UpdatedAt = RFC3339UTC(t.UpdatedAt)
			return f
		}
		f.UpdatedAt = RFC3339UTC(t.UpdatedAt)
		if t.UpdatedAt.Before(now.Add(-threshold)) {
			f.State = "stale"
		} else {
			f.State = "fresh"
		}
	case "file":
		if _, err := os.Stat(r.Target); err == nil {
			f.State = "present"
		} else {
			f.State = "missing"
		}
	}
	return f
}

func (s *Store) guideForEdit(p *Project) *Guide {
	if p.Guide == nil {
		p.Guide = &Guide{Sections: []GuideSection{}, UpdatedAt: Now(), UpdatedBy: ""}
	}
	if p.Guide.Sections == nil {
		p.Guide.Sections = []GuideSection{}
	}
	return p.Guide
}

func (s *Store) touchGuide(p *Project, g *Guide, actor, action string, meta map[string]any) {
	now := Now()
	if g != nil {
		g.UpdatedAt = now
		g.UpdatedBy = actor
	}
	p.Guide = g
	p.UpdatedAt = now
	if p.History == nil {
		p.History = []HistoryEntry{}
	}
	p.NextHistoryN++
	entry := HistoryEntry{
		ID:     fmt.Sprintf("h%d", p.NextHistoryN),
		Action: "guide-updated",
		Actor:  actor,
		At:     now,
		Meta:   meta,
	}
	if meta == nil {
		entry.Meta = map[string]any{}
	}
	p.History = append(p.History, entry)
}

func sectionExists(sections []GuideSection, name string) bool {
	return sectionIndex(sections, name) >= 0
}

func sectionIndex(sections []GuideSection, name string) int {
	for i, s := range sections {
		if s.Name == name {
			return i
		}
	}
	return -1
}
