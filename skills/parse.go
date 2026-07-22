package skills

import (
	"fmt"
	"regexp"
	"strings"
)

var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9_-]*[a-z0-9])?$`)

// frontmatter is the parsed `---` header: scalar keys, one optional nested
// string map (modes), and inline lists. Unknown scalar keys are tolerated so
// the store can add audit fields (created_at, ...) to custom persona files.
type frontmatter struct {
	scalars map[string]string
	modes   []Mode // name+summary only; Instructions filled from body sections
	lists   map[string][]string
}

// parseFrontmatter splits src into frontmatter and body. The document must
// start with a `---` line; the header ends at the next `---` line.
func parseFrontmatter(src []byte) (frontmatter, string, error) {
	fm := frontmatter{scalars: map[string]string{}, lists: map[string][]string{}}
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fm, "", fmt.Errorf("missing frontmatter: file must start with ---")
	}
	end := -1
	inModes := false
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			end = i
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "  ") { // nested entry (only under modes:)
			if !inModes {
				return fm, "", fmt.Errorf("frontmatter line %d: unexpected indent", i+1)
			}
			k, v, ok := splitKV(strings.TrimSpace(line))
			if !ok {
				return fm, "", fmt.Errorf("frontmatter line %d: want `name: summary`", i+1)
			}
			fm.modes = append(fm.modes, Mode{Name: k, Summary: v})
			continue
		}
		inModes = false
		k, v, ok := splitKV(line)
		if !ok {
			return fm, "", fmt.Errorf("frontmatter line %d: want `key: value`", i+1)
		}
		switch {
		case k == "modes" && v == "":
			inModes = true
		case strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]"):
			inner := strings.TrimSpace(v[1 : len(v)-1])
			if inner != "" {
				for _, item := range strings.Split(inner, ",") {
					fm.lists[k] = append(fm.lists[k], strings.TrimSpace(item))
				}
			} else {
				fm.lists[k] = []string{}
			}
		default:
			fm.scalars[k] = v
		}
	}
	if end < 0 {
		return fm, "", fmt.Errorf("unterminated frontmatter: closing --- not found")
	}
	return fm, strings.Join(lines[end+1:], "\n"), nil
}

// splitKV splits "key: value" (value may be empty, and may contain colons).
func splitKV(line string) (k, v string, ok bool) {
	i := strings.Index(line, ":")
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

// sections splits a markdown body into a preamble and its `## `-level
// sections, preserving order. A section runs until the next `## ` heading.
type section struct{ title, body string }

func splitSections(body string) (preamble string, secs []section) {
	lines := strings.Split(body, "\n")
	var cur *section
	var pre []string
	flush := func() {
		if cur != nil {
			cur.body = strings.TrimSpace(cur.body)
			secs = append(secs, *cur)
		}
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			cur = &section{title: strings.TrimSpace(strings.TrimPrefix(line, "## "))}
			continue
		}
		if cur == nil {
			pre = append(pre, line)
		} else {
			cur.body += line + "\n"
		}
	}
	flush()
	return strings.Join(pre, "\n"), secs
}

// ParsePersona parses and validates one persona prompt file. stem is the
// filename without extension; it must equal the frontmatter name.
func ParsePersona(stem string, src []byte) (PersonaSpec, error) {
	fm, body, err := parseFrontmatter(src)
	if err != nil {
		return PersonaSpec{}, fmt.Errorf("persona %s: %w", stem, err)
	}
	p := PersonaSpec{
		Name:        fm.scalars["name"],
		Description: fm.scalars["description"],
		Launch:      fm.scalars["launch"],
		DefaultMode: fm.scalars["default_mode"],
		Modes:       fm.modes,
		Body:        strings.TrimSpace(body),
	}
	if !nameRe.MatchString(p.Name) {
		return PersonaSpec{}, fmt.Errorf("persona %s: invalid or missing name %q", stem, p.Name)
	}
	if p.Name != stem {
		return PersonaSpec{}, fmt.Errorf("persona %s: frontmatter name %q must match filename", stem, p.Name)
	}
	if p.Description == "" {
		return PersonaSpec{}, fmt.Errorf("persona %s: description is required", stem)
	}
	switch p.Launch {
	case "":
		p.Launch = "prompt"
	case "prompt", "hook":
	default:
		return PersonaSpec{}, fmt.Errorf("persona %s: launch must be prompt or hook, got %q", stem, p.Launch)
	}
	if v := fm.scalars["project_optional"]; v != "" {
		if v != "true" && v != "false" {
			return PersonaSpec{}, fmt.Errorf("persona %s: project_optional must be true or false", stem)
		}
		p.ProjectOptional = v == "true"
	}

	// Reconcile frontmatter modes with `## Mode: <name>` sections, and pull
	// out the personality section; everything else is the core prompt.
	pre, secs := splitSections(p.Body)
	core := []string{strings.TrimSpace(pre)}
	seen := map[string]bool{}
	for _, s := range secs {
		if name, ok := strings.CutPrefix(s.title, "Mode: "); ok {
			name = strings.TrimSpace(name)
			found := false
			for i := range p.Modes {
				if p.Modes[i].Name == name {
					p.Modes[i].Instructions = s.body
					found = true
					break
				}
			}
			if !found {
				return PersonaSpec{}, fmt.Errorf("persona %s: section %q has no frontmatter modes entry", stem, s.title)
			}
			seen[name] = true
			continue
		}
		if s.title == "Personality" {
			p.Personality = s.body
			continue
		}
		core = append(core, "## "+s.title+"\n\n"+s.body)
	}
	for _, m := range p.Modes {
		if !seen[m.Name] {
			return PersonaSpec{}, fmt.Errorf("persona %s: mode %q has no `## Mode: %s` section", stem, m.Name, m.Name)
		}
	}
	if p.DefaultMode != "" {
		if _, ok := p.Mode(p.DefaultMode); !ok {
			return PersonaSpec{}, fmt.Errorf("persona %s: default_mode %q is not a declared mode", stem, p.DefaultMode)
		}
	}
	p.CorePrompt = strings.TrimSpace(strings.Join(core, "\n\n"))
	return p, nil
}

// ParseCapability parses and validates one capability prompt file.
func ParseCapability(stem string, src []byte) (CapabilitySpec, error) {
	fm, body, err := parseFrontmatter(src)
	if err != nil {
		return CapabilitySpec{}, fmt.Errorf("capability %s: %w", stem, err)
	}
	c := CapabilitySpec{
		Name:        fm.scalars["name"],
		Description: fm.scalars["description"],
		Labels:      fm.lists["labels"],
		Boards:      fm.lists["boards"],
		Body:        strings.TrimSpace(body),
	}
	if !nameRe.MatchString(c.Name) {
		return CapabilitySpec{}, fmt.Errorf("capability %s: invalid or missing name %q", stem, c.Name)
	}
	if c.Name != stem {
		return CapabilitySpec{}, fmt.Errorf("capability %s: frontmatter name %q must match filename", stem, c.Name)
	}
	if c.Description == "" {
		return CapabilitySpec{}, fmt.Errorf("capability %s: description is required", stem)
	}
	if len(c.Labels) == 0 {
		return CapabilitySpec{}, fmt.Errorf("capability %s: labels is required", stem)
	}
	if len(c.Boards) == 0 {
		return CapabilitySpec{}, fmt.Errorf("capability %s: boards is required", stem)
	}
	_, secs := splitSections(c.Body)
	have := map[string]bool{}
	for _, s := range secs {
		have[s.title] = true
	}
	for _, required := range []string{"Semantics", "Actions", "Converge"} {
		if !have[required] {
			return CapabilitySpec{}, fmt.Errorf("capability %s: missing required section `## %s`", stem, required)
		}
	}
	return c, nil
}
