package profile

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	"atm/internal/core"
)

// Write renders a profile to its directory form — the inverse of Load.
// Keys are profile-relative paths (manifest.yaml, personas/<name>.md, …),
// so the result feeds ArtifactFS, WriteArtifact, or a directory writer
// unchanged.
//
// Only DOCUMENT fields are written: a profile document has no ledger
// identity, no origin, no endpoints — apply fills those on the way in, and
// export drops them on the way out. The form is canonical (one encoder per
// value shape), so Load(Write(p)) == p for any profile Load accepts, and
// Write(Load(Write(p))) == Write(p): a re-export of an unchanged project
// leaves `git diff` empty.
func Write(p *core.Profile) (map[string][]byte, error) {
	out := map[string][]byte{}
	var problems []error
	m, err := writeManifest(p.Manifest)
	if err != nil {
		problems = append(problems, err)
	}
	out[manifestFile] = m
	for _, x := range p.Personas {
		b, err := writePersona(x)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		out[path.Join(dirPersonas, x.Name+".md")] = b
	}
	for _, x := range p.Checklists {
		b, err := writeChecklist(x)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		out[path.Join(dirChecklists, x.Name+".md")] = b
	}
	for _, x := range p.Channels {
		b, err := writeChannel(x)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		out[path.Join(dirChannels, x.Name+".md")] = b
	}
	if err := errors.Join(problems...); err != nil {
		return nil, fmt.Errorf("profile %s: write: %w", p.Manifest.Ref(), err)
	}
	return out, nil
}

// StaleDocuments lists the documents in fsys (rooted at a profile
// directory) that files does not carry: what a directory export must
// remove so the directory mirrors the project. Files the format does not
// own — anything outside the three document directories, or not .md — are
// never listed. A missing directory has no stale documents.
func StaleDocuments(fsys fs.FS, files map[string][]byte) []string {
	var out []string
	for _, dir := range []string{dirPersonas, dirChecklists, dirChannels} {
		for _, stem := range markdownStems(fsys, dir) {
			name := path.Join(dir, stem+".md")
			if _, ok := files[name]; !ok {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

func writeManifest(m core.ProfileManifest) ([]byte, error) {
	var b bytes.Buffer
	var problems []error
	put := func(key, val string) {
		s, err := scalar(val)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: %s: %w", manifestFile, key, err))
		}
		fmt.Fprintf(&b, "%s:%s\n", key, s)
	}
	put("name", m.Name)
	put("version", m.Version)
	format := m.Format
	if format == 0 {
		format = Format
	}
	fmt.Fprintf(&b, "format: %d\n", format)
	put("description", m.Description)
	fmt.Fprintf(&b, "authors: %s\n", list(m.Authors))
	fmt.Fprintf(&b, "requires_capabilities: %s\n", list(m.RequiresCapabilities))
	return b.Bytes(), errors.Join(problems...)
}

func writePersona(x core.Persona) ([]byte, error) {
	fm := newFrontmatter("persona", x.Name)
	fm.scalar("name", x.Name)
	fm.scalar("description", x.Description)
	if x.Launch != "" {
		fm.scalar("launch", x.Launch)
	}
	if x.ProjectOptional {
		fm.raw("project_optional", "true")
	}
	return fm.document(x.Prompt)
}

func writeChecklist(x core.ChecklistRecord) ([]byte, error) {
	fm := newFrontmatter("checklist", x.Name)
	fm.scalar("name", x.Name)
	fm.scalar("purpose", x.Purpose)
	if len(x.Suits) > 0 {
		fm.raw("suits", list(x.Suits))
	}
	if len(x.Requires.Capabilities) > 0 {
		fm.raw("requires_capabilities", list(x.Requires.Capabilities))
	}
	if len(x.Requires.Channels) > 0 {
		fm.raw("requires_channels", list(x.Requires.Channels))
	}
	fm.raw("target", defaulted(x.Target, core.ChecklistTargetProject))
	if x.Targets != "" {
		fm.scalar("targets", x.Targets)
	}
	fm.raw("mode", defaulted(x.Mode, core.ChecklistModeEager))
	body, err := stepsBody(x.Steps, 0)
	if err != nil {
		fm.problems = append(fm.problems, fmt.Errorf("checklist %s: %w", x.Name, err))
	}
	return fm.document(body)
}

func writeChannel(x core.ChannelRecord) ([]byte, error) {
	fm := newFrontmatter("channel", x.Name)
	fm.scalar("name", x.Name)
	fm.raw("role_hint", defaulted(x.RoleHint, core.ChannelRoleHome))
	return fm.document(x.Purpose)
}

// frontmatter accumulates one document's header and its problems, so a
// document with two faults reports both.
type frontmatter struct {
	kind, name string
	lines      []string
	problems   []error
}

func newFrontmatter(kind, name string) *frontmatter {
	fm := &frontmatter{kind: kind, name: name}
	if !nameRe.MatchString(name) {
		fm.problems = append(fm.problems, fmt.Errorf("%s %q: invalid name (lowercase letters, digits, - and _)", kind, name))
	}
	return fm
}

func (fm *frontmatter) scalar(key, val string) {
	s, err := scalar(val)
	if err != nil {
		fm.problems = append(fm.problems, fmt.Errorf("%s %s: %s: %w", fm.kind, fm.name, key, err))
	}
	fm.lines = append(fm.lines, key+":"+s)
}

// raw writes a value the format never needs to quote: a list literal, a
// boolean, an enum the parser validates.
func (fm *frontmatter) raw(key, val string) { fm.lines = append(fm.lines, key+": "+val) }

func (fm *frontmatter) document(body string) ([]byte, error) {
	if err := errors.Join(fm.problems...); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("---\n")
	for _, l := range fm.lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	body = strings.TrimSpace(body)
	if body != "" {
		b.WriteString(body)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// scalar encodes one frontmatter value in the form the flat parser reads
// back to the same string: a leading space and the bare text when nothing
// in it would be misread; a quoted form when the bare text would parse as
// a list, open a block, or lose its own quotes; a `|` block when it spans
// lines. The parser trims each block line, so leading indentation inside
// a multi-line value is not representable — values here are prose, and
// trimming is what Load already does to everything it reads.
func scalar(v string) (string, error) {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		return "", nil
	case strings.Contains(v, "\n"):
		var b strings.Builder
		b.WriteString(" |")
		for _, line := range strings.Split(v, "\n") {
			b.WriteByte('\n')
			if t := strings.TrimSpace(line); t != "" {
				b.WriteString("  " + t)
			}
		}
		return b.String(), nil
	case needsQuotes(v):
		switch {
		case !strings.Contains(v, `"`):
			return ` "` + v + `"`, nil
		case !strings.Contains(v, `'`):
			return ` '` + v + `'`, nil
		default:
			return "", fmt.Errorf("value %q needs quoting but contains both quote kinds", v)
		}
	default:
		return " " + v, nil
	}
}

// needsQuotes reports whether the bare text would be misread by
// parseYAMLScalars: as a list, as a block opener, or as a quoted value.
func needsQuotes(v string) bool {
	if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
		return true
	}
	if v == ">" || v == "|" {
		return true
	}
	if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
		return true
	}
	return false
}

// list encodes an inline list. Items are names (nameRe) and never carry
// commas or brackets, so no item-level quoting exists.
func list(xs []string) string {
	if len(xs) == 0 {
		return "[]"
	}
	return "[" + strings.Join(xs, ", ") + "]"
}

// stepsBody renders the step tree as the nested numbered list parseSteps
// reads: `N. text` at the top level, three spaces of indentation per
// depth. This is deliberately NOT core.RenderChecklistSteps — that is the
// display form (1.1, 1.2.1), which the parser does not accept.
func stepsBody(steps []core.ChecklistStep, depth int) (string, error) {
	var b strings.Builder
	for i, s := range steps {
		text := strings.TrimSpace(s.Text)
		switch {
		case text == "":
			return "", fmt.Errorf("step %d at depth %d is empty", i+1, depth)
		case strings.Contains(text, "\n"):
			return "", fmt.Errorf("step %d at depth %d spans lines; a step is one line", i+1, depth)
		}
		fmt.Fprintf(&b, "%s%s. %s\n", strings.Repeat("   ", depth), strconv.Itoa(i+1), text)
		child, err := stepsBody(s.Children, depth+1)
		if err != nil {
			return "", err
		}
		b.WriteString(child)
	}
	return b.String(), nil
}
