package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"atm/internal/core"
	"atm/skills"
)

func (s *Store) personasDir() string { return filepath.Join(s.Root, "personas") }
func (s *Store) personaMDPath(name string) string {
	return filepath.Join(s.personasDir(), name+".md")
}
func (s *Store) personaJSONPath(name string) string {
	return filepath.Join(s.personasDir(), name+".json")
}
func (s *Store) personalityPath(name string) string {
	return filepath.Join(s.personasDir(), name+".personality.md")
}

// builtinPersona converts a skills built-in to the core persona shape.
// Built-ins have no audit trail: they ship with the binary.
func builtinPersona(spec skills.PersonaSpec) *core.Persona {
	return &core.Persona{
		Name:        spec.Name,
		Prompt:      spec.Body,
		Description: spec.Description,
		CreatedBy:   "builtin",
		UpdatedBy:   "builtin",
	}
}

// composePersonaDoc renders a custom persona as a skills-format markdown
// document with audit fields in frontmatter (the parser tolerates them).
func composePersonaDoc(p *core.Persona) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", p.Name)
	fmt.Fprintf(&b, "description: %s\n", sanitizeFrontmatterValue(p.Description))
	fmt.Fprintf(&b, "created_at: %s\n", p.CreatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "created_by: %s\n", p.CreatedBy)
	fmt.Fprintf(&b, "updated_at: %s\n", p.UpdatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "updated_by: %s\n", p.UpdatedBy)
	b.WriteString("---\n")
	b.WriteString(p.Prompt)
	if !strings.HasSuffix(p.Prompt, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// sanitizeFrontmatterValue keeps frontmatter one-line.
func sanitizeFrontmatterValue(v string) string {
	v = strings.ReplaceAll(v, "\n", " ")
	return strings.TrimSpace(v)
}

// parsePersonaDoc reads a stored custom-persona markdown file back into the
// core shape. The skills parser validates format; audit fields come from the
// tolerated extra frontmatter keys, re-read here.
func parsePersonaDoc(name string, src []byte) (*core.Persona, error) {
	spec, err := skills.ParsePersona(name, src)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", core.ErrUsage, err)
	}
	p := &core.Persona{Name: spec.Name, Prompt: spec.Body, Description: spec.Description}
	// Best-effort audit re-read: scan frontmatter lines only (up to the
	// closing --- delimiter).
	lines := strings.Split(string(src), "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			break
		}
		k, v, ok := strings.Cut(lines[i], ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "created_by":
			p.CreatedBy = v
		case "updated_by":
			p.UpdatedBy = v
		case "created_at":
			if ts, err := time.Parse(time.RFC3339, v); err == nil {
				p.CreatedAt = ts
			}
		case "updated_at":
			if ts, err := time.Parse(time.RFC3339, v); err == nil {
				p.UpdatedAt = ts
			}
		}
	}
	return p, nil
}

func (s *Store) CreatePersona(name, prompt, description, actor string) (*core.Persona, error) {
	if err := core.ValidatePersonaName(name); err != nil {
		return nil, err
	}
	if _, ok := skills.Persona(name); ok {
		return nil, fmt.Errorf("%w: persona %q is built-in", core.ErrConflict, name)
	}
	if err := s.validateActor(actor); err != nil {
		return nil, err
	}
	var created *core.Persona
	err := s.WithLock("personas", func() error {
		if s.customExists(name) {
			return fmt.Errorf("%w: persona %q already exists", core.ErrConflict, name)
		}
		now := core.Now()
		p := &core.Persona{
			Name: name, Prompt: prompt, Description: description,
			CreatedAt: now, UpdatedAt: now, CreatedBy: actor, UpdatedBy: actor,
		}
		if p.Description == "" {
			p.Description = "Custom persona."
		}
		doc := composePersonaDoc(p)
		if _, err := parsePersonaDoc(name, []byte(doc)); err != nil {
			return err // format-enforced at the door
		}
		if err := os.MkdirAll(s.personasDir(), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(s.personaMDPath(name), []byte(doc), 0o644); err != nil {
			return err
		}
		created = p
		return nil
	})
	return created, err
}

func (s *Store) customExists(name string) bool {
	if _, err := os.Stat(s.personaMDPath(name)); err == nil {
		return true
	}
	if _, err := os.Stat(s.personaJSONPath(name)); err == nil {
		return true
	}
	return false
}

func (s *Store) GetPersona(name string) (*core.Persona, error) {
	if err := core.ValidatePersonaName(name); err != nil {
		return nil, err
	}
	if spec, ok := skills.Persona(name); ok {
		return builtinPersona(spec), nil
	}
	if b, err := os.ReadFile(s.personaMDPath(name)); err == nil {
		return parsePersonaDoc(name, b)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// Legacy JSON: migrate to markdown on first read.
	var legacy core.Persona
	if err := ReadJSON(s.personaJSONPath(name), &legacy); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: persona %q", core.ErrNotFound, name)
		}
		return nil, err
	}
	err := s.WithLock("personas", func() error {
		if err := os.WriteFile(s.personaMDPath(name), []byte(composePersonaDoc(&legacy)), 0o644); err != nil {
			return err
		}
		return os.Remove(s.personaJSONPath(name))
	})
	if err != nil {
		return nil, err
	}
	return &legacy, nil
}

func (s *Store) ListPersonas() []*core.Persona {
	var out []*core.Persona
	for _, spec := range skills.Personas() {
		out = append(out, builtinPersona(spec))
	}
	entries, err := os.ReadDir(s.personasDir())
	if err != nil {
		return out
	}
	seen := map[string]bool{}
	for _, p := range out {
		seen[p.Name] = true
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".md" && ext != ".json" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ext)
		if strings.HasSuffix(name, ".personality") || seen[name] {
			continue
		}
		if p, err := s.GetPersona(name); err == nil {
			seen[name] = true
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) EditPersona(name string, prompt, description *string, actor string) (*core.Persona, error) {
	if err := core.ValidatePersonaName(name); err != nil {
		return nil, err
	}
	if _, ok := skills.Persona(name); ok {
		return nil, fmt.Errorf("%w: persona %q is built-in; customize it via `atm persona personality`", core.ErrUsage, name)
	}
	if err := s.validateActor(actor); err != nil {
		return nil, err
	}
	var updated *core.Persona
	err := s.WithLock("personas", func() error {
		p, err := s.GetPersona(name)
		if err != nil {
			return err
		}
		if prompt != nil {
			p.Prompt = *prompt
		}
		if description != nil {
			p.Description = *description
		}
		p.UpdatedAt = core.Now()
		p.UpdatedBy = actor
		doc := composePersonaDoc(p)
		if _, err := parsePersonaDoc(name, []byte(doc)); err != nil {
			return err
		}
		if err := os.WriteFile(s.personaMDPath(name), []byte(doc), 0o644); err != nil {
			return err
		}
		updated = p
		return nil
	})
	return updated, err
}

func (s *Store) RemovePersona(name string) error {
	if err := core.ValidatePersonaName(name); err != nil {
		return err
	}
	if _, ok := skills.Persona(name); ok {
		return fmt.Errorf("%w: cannot remove built-in persona %q", core.ErrUsage, name)
	}
	return s.WithLock("personas", func() error {
		if _, err := s.GetPersona(name); err != nil {
			return err
		}
		_ = os.Remove(s.personaJSONPath(name))
		_ = os.Remove(s.personalityPath(name))
		return os.Remove(s.personaMDPath(name))
	})
}

// PersonaDoc returns the raw markdown document of a custom persona. Built-ins
// live in the binary; callers use the skills package for them.
func (s *Store) PersonaDoc(name string) (string, error) {
	if err := core.ValidatePersonaName(name); err != nil {
		return "", err
	}
	if _, ok := skills.Persona(name); ok {
		return "", fmt.Errorf("%w: persona %q is built-in", core.ErrUsage, name)
	}
	if _, err := s.GetPersona(name); err != nil { // triggers JSON migration
		return "", err
	}
	b, err := os.ReadFile(s.personaMDPath(name))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// personaExists reports whether name is a built-in or a stored custom.
func (s *Store) personaExists(name string) bool {
	if _, ok := skills.Persona(name); ok {
		return true
	}
	return s.customExists(name)
}

// GetPersonality returns the personality overlay text ("" when none is set).
func (s *Store) GetPersonality(name string) (string, error) {
	if err := core.ValidatePersonaName(name); err != nil {
		return "", err
	}
	if !s.personaExists(name) {
		return "", fmt.Errorf("%w: persona %q", core.ErrNotFound, name)
	}
	b, err := os.ReadFile(s.personalityPath(name))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func (s *Store) SetPersonality(name, text, actor string) error {
	if err := core.ValidatePersonaName(name); err != nil {
		return err
	}
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if !s.personaExists(name) {
		return fmt.Errorf("%w: persona %q", core.ErrNotFound, name)
	}
	return s.WithLock("personas", func() error {
		if err := os.MkdirAll(s.personasDir(), 0o755); err != nil {
			return err
		}
		return os.WriteFile(s.personalityPath(name), []byte(strings.TrimSpace(text)+"\n"), 0o644)
	})
}

func (s *Store) ClearPersonality(name string) error {
	if err := core.ValidatePersonaName(name); err != nil {
		return err
	}
	if !s.personaExists(name) {
		return fmt.Errorf("%w: persona %q", core.ErrNotFound, name)
	}
	return s.WithLock("personas", func() error {
		err := os.Remove(s.personalityPath(name))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	})
}
