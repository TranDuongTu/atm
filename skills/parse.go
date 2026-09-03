package skills

import (
	"fmt"
	"regexp"
	"strings"
)

var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9_-]*[a-z0-9])?$`)

var stepLineRe = regexp.MustCompile(`^([ \t]*)(?:\d+\.|-)\s+(.+)$`)

var seedOriginRe = regexp.MustCompile(`^(user|shipped:[a-z0-9]([a-z0-9_-]*[a-z0-9])?)$`)

// frontmatter is the parsed `---` header: scalar keys and inline lists.
// Unknown scalar keys are tolerated so the store can add audit fields
// (created_at, ...) to custom persona files.
type frontmatter struct {
	scalars map[string]string
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
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			end = i
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		k, v, ok := splitKV(line)
		if !ok {
			return fm, "", fmt.Errorf("frontmatter line %d: want `key: value`", i+1)
		}
		if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
			inner := strings.TrimSpace(v[1 : len(v)-1])
			if inner != "" {
				for _, item := range strings.Split(inner, ",") {
					fm.lists[k] = append(fm.lists[k], strings.TrimSpace(item))
				}
			} else {
				fm.lists[k] = []string{}
			}
		} else {
			fm.scalars[k] = unquoteScalar(v)
		}
	}
	if end < 0 {
		return fm, "", fmt.Errorf("unterminated frontmatter: closing --- not found")
	}
	return fm, strings.Join(lines[end+1:], "\n"), nil
}

// unquoteScalar strips one layer of matching YAML quotes. A description
// containing a colon must be quoted in the frontmatter; the quotes are
// syntax, and every surface that renders the value (capability list, the
// [C] switcher, session Capabilities blocks) wants the value itself.
func unquoteScalar(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
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
		Name:            fm.scalars["name"],
		Description:     fm.scalars["description"],
		Launch:          fm.scalars["launch"],
		Kickoff:         fm.scalars["kickoff"],
		Expects:         fm.lists["expects"],
		Optional:        fm.lists["optional"],
		ProjectOptional: fm.scalars["project_optional"] == "true",
		Body:            strings.TrimSpace(body),
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
	case "prompt", "hook", "tui":
	default:
		return PersonaSpec{}, fmt.Errorf("persona %s: launch must be prompt, hook, or tui, got %q", stem, p.Launch)
	}
	if v := fm.scalars["project_optional"]; v != "" {
		if v != "true" && v != "false" {
			return PersonaSpec{}, fmt.Errorf("persona %s: project_optional must be true or false", stem)
		}
		p.ProjectOptional = v == "true"
	}
	if err := validateExpects(p.Expects); err != nil {
		return PersonaSpec{}, fmt.Errorf("persona %s: %w", stem, err)
	}
	if err := validateExpects(p.Optional); err != nil {
		return PersonaSpec{}, fmt.Errorf("persona %s: optional: %w", stem, err)
	}

	// Split body: pull out the personality section; everything else is the core prompt.
	pre, secs := splitSections(p.Body)
	core := []string{strings.TrimSpace(pre)}
	for _, s := range secs {
		if s.title == "Personality" {
			p.Personality = s.body
			continue
		}
		core = append(core, "## "+s.title+"\n\n"+s.body)
	}
	p.CorePrompt = strings.TrimSpace(strings.Join(core, "\n\n"))
	return p, nil
}

var validExpects = map[string]bool{
	"CODE":         true,
	"PROJECT_NAME": true,
	"ACTOR":        true,
	"TASK_ID":      true,
	"CAPABILITY":   true,
	"LANE":         true,
}

func validateExpects(expects []string) error {
	for _, e := range expects {
		if !validExpects[e] {
			return fmt.Errorf("unknown expects value %q (valid: CODE, PROJECT_NAME, ACTOR, TASK_ID, CAPABILITY, LANE)", e)
		}
	}
	return nil
}

type stepNode struct {
	text     string
	children []*stepNode
}

// stepIndent measures leading whitespace in columns; tabs count as 4.
func stepIndent(ws string) int {
	n := 0
	for _, r := range ws {
		if r == '\t' {
			n += 4
		} else {
			n++
		}
	}
	return n
}

func seedStepsOf(nodes []*stepNode) []SeedStep {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]SeedStep, len(nodes))
	for i, n := range nodes {
		out[i] = SeedStep{Text: n.text, Children: seedStepsOf(n.children)}
	}
	return out
}

// ParseSteps parses a markdown nested list ("-" or "N." markers; indentation
// is depth) into a step tree. Non-step lines are ignored, matching the v1
// parser's tolerance; a dedent attaches to the nearest shallower ancestor.
func ParseSteps(body string) ([]SeedStep, error) {
	root := &stepNode{}
	type frame struct {
		indent int
		node   *stepNode
	}
	stack := []frame{{-1, root}}
	for _, line := range strings.Split(body, "\n") {
		m := stepLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ind := stepIndent(m[1])
		for len(stack) > 1 && ind <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		n := &stepNode{text: strings.TrimSpace(m[2])}
		parent := stack[len(stack)-1].node
		parent.children = append(parent.children, n)
		stack = append(stack, frame{ind, n})
	}
	return seedStepsOf(root.children), nil
}

// ParseChecklistSeed parses one seed checklist file: frontmatter name/purpose
// (required; name must match the filename stem), optional suits (or the legacy
// persona scalar), requires_capabilities/requires_channels, origin, and a body
// of nested numbered or dashed step lines.
func ParseChecklistSeed(stem string, src []byte) (ChecklistSeed, error) {
	fm, body, err := parseFrontmatter(src)
	if err != nil {
		return ChecklistSeed{}, fmt.Errorf("checklist seed %s: %w", stem, err)
	}
	s := ChecklistSeed{
		Name:    fm.scalars["name"],
		Purpose: fm.scalars["purpose"],
		Suits:   fm.lists["suits"],
		Origin:  fm.scalars["origin"],
		Requires: SeedRequires{
			Capabilities: fm.lists["requires_capabilities"],
			Channels:     fm.lists["requires_channels"],
		},
	}
	if p := fm.scalars["persona"]; p != "" {
		if len(s.Suits) > 0 {
			return ChecklistSeed{}, fmt.Errorf("checklist seed %s: give suits or the legacy persona key, not both", stem)
		}
		s.Suits = []string{p}
	}
	if s.Purpose == "" || !nameRe.MatchString(s.Name) {
		return ChecklistSeed{}, fmt.Errorf("checklist seed %s: name and purpose are required", stem)
	}
	if s.Name != stem {
		return ChecklistSeed{}, fmt.Errorf("checklist seed %s: frontmatter name %q must match filename", stem, s.Name)
	}
	for _, suit := range s.Suits {
		if !nameRe.MatchString(suit) {
			return ChecklistSeed{}, fmt.Errorf("checklist seed %s: invalid suits entry %q", stem, suit)
		}
	}
	if s.Origin != "" && !seedOriginRe.MatchString(s.Origin) {
		return ChecklistSeed{}, fmt.Errorf("checklist seed %s: origin %q must be user, shipped:atm, or shipped:<capability>", stem, s.Origin)
	}
	if s.Steps, err = ParseSteps(body); err != nil {
		return ChecklistSeed{}, fmt.Errorf("checklist seed %s: %w", stem, err)
	}
	if len(s.Steps) == 0 {
		return ChecklistSeed{}, fmt.Errorf("checklist seed %s: needs at least one numbered or dashed step", stem)
	}
	return s, nil
}
