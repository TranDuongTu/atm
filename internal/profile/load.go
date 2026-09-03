package profile

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"atm/internal/core"
)

// Document directory names inside a profile.
const (
	dirPersonas   = "personas"
	dirChecklists = "checklists"
	dirChannels   = "channels"
	manifestFile  = "manifest.yaml"
)

var (
	nameRe     = regexp.MustCompile(`^[a-z0-9]([a-z0-9_-]*[a-z0-9])?$`)
	stepLineRe = regexp.MustCompile(`^([ \t]*)(?:\d+\.|-)\s+(.+)$`)
)

// Load reads and validates a profile from fsys, which must be rooted at the
// profile directory (manifest.yaml at its top level). Every failure the
// documents can answer alone is collected and reported together, so an
// author fixes a whole profile in one round trip rather than one error at a
// time.
//
// Checks that depend on this build's knowledge — whether a required
// capability exists — are NOT done here; see ValidateCapabilities.
func Load(fsys fs.FS) (*core.Profile, error) {
	manifest, err := LoadManifest(fsys)
	if err != nil {
		// Without an identity nothing else can be judged, so this one stops
		// the load rather than joining the collected errors.
		return nil, err
	}
	p := &core.Profile{Manifest: manifest}
	var problems []error

	// Each document is parsed to a VALUE plus its problems, and a document
	// whose name is sound joins the profile even when something else about
	// it is wrong. That is what lets one load report a bad mode AND a suits
	// entry naming nobody, instead of hiding the second behind the first.
	for _, stem := range markdownStems(fsys, dirPersonas) {
		src, err := fs.ReadFile(fsys, path.Join(dirPersonas, stem+".md"))
		if err != nil {
			problems = append(problems, err)
			continue
		}
		x, errs := parsePersona(stem, src)
		problems = append(problems, errs...)
		if x.Name == stem {
			p.Personas = append(p.Personas, x)
		}
	}
	for _, stem := range markdownStems(fsys, dirChecklists) {
		src, err := fs.ReadFile(fsys, path.Join(dirChecklists, stem+".md"))
		if err != nil {
			problems = append(problems, err)
			continue
		}
		x, errs := parseChecklist(stem, src)
		problems = append(problems, errs...)
		if x.Name == stem {
			p.Checklists = append(p.Checklists, x)
		}
	}
	for _, stem := range markdownStems(fsys, dirChannels) {
		src, err := fs.ReadFile(fsys, path.Join(dirChannels, stem+".md"))
		if err != nil {
			problems = append(problems, err)
			continue
		}
		x, errs := parseChannel(stem, src)
		problems = append(problems, errs...)
		if x.Name == stem {
			p.Channels = append(p.Channels, x)
		}
	}

	problems = append(problems, selfConsistency(p)...)
	if err := errors.Join(problems...); err != nil {
		return nil, fmt.Errorf("profile %s: %w", manifest.Ref(), err)
	}
	return p, nil
}

// markdownStems lists the .md filenames in dir without their extension,
// sorted. A missing directory is legal — an extension profile may ship only
// one kind of document — and non-markdown files are ignored so a profile
// directory may carry a README or an editor artefact.
func markdownStems(fsys fs.FS, dir string) []string {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		out = append(out, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(out)
	return out
}

// LoadManifest reads just a profile's identity — enough to list it without
// parsing every document.
func LoadManifest(fsys fs.FS) (core.ProfileManifest, error) {
	src, err := fs.ReadFile(fsys, manifestFile)
	if err != nil {
		return core.ProfileManifest{}, fmt.Errorf("profile: %s is required at the profile root: %w", manifestFile, err)
	}
	doc, err := parseYAMLScalars(string(src))
	if err != nil {
		return core.ProfileManifest{}, fmt.Errorf("profile: %s: %w", manifestFile, err)
	}
	m := core.ProfileManifest{
		Name:                 doc.scalars["name"],
		Version:              doc.scalars["version"],
		Description:          doc.scalars["description"],
		Authors:              doc.lists["authors"],
		RequiresCapabilities: doc.lists["requires_capabilities"],
	}
	var problems []error
	if !core.ValidProfileName(m.Name) {
		problems = append(problems, fmt.Errorf("%s: invalid or missing name %q (lowercase letters, digits, - and _)", manifestFile, m.Name))
	}
	if !core.ValidProfileVersion(m.Version) {
		problems = append(problems, fmt.Errorf("%s: version %q must be semver (1.2.3) or %q", manifestFile, m.Version, core.DevVersion))
	}
	raw, ok := doc.scalars["format"]
	if !ok {
		problems = append(problems, fmt.Errorf("%s: format is required", manifestFile))
	} else {
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		switch {
		case err != nil:
			problems = append(problems, fmt.Errorf("%s: format %q must be an integer", manifestFile, raw))
		case n != Format:
			problems = append(problems, fmt.Errorf("%s: format %d is not readable by this build (format %d)", manifestFile, n, Format))
		default:
			m.Format = n
		}
	}
	for _, c := range m.RequiresCapabilities {
		if !core.ValidProfileName(c) {
			problems = append(problems, fmt.Errorf("%s: invalid requires_capabilities entry %q", manifestFile, c))
		}
	}
	if err := errors.Join(problems...); err != nil {
		return core.ProfileManifest{}, fmt.Errorf("profile: %w", err)
	}
	return m, nil
}

// ParsePersonaDocument parses one persona document — the same parser the
// profile loader uses, exported so `atm persona set` and `profile apply`
// import identical text through identical rules. A persona a user writes by
// hand and one a profile ships are the same thing.
// An empty stem means "trust the document": there is no filename to agree
// with when a persona arrives on stdin.
func ParsePersonaDocument(stem string, src []byte) (core.Persona, error) {
	if stem == "" {
		fm, _, err := parseFrontmatter(src)
		if err != nil {
			return core.Persona{}, fmt.Errorf("persona: %w", err)
		}
		stem = fm.scalars["name"]
	}
	p, problems := parsePersona(stem, src)
	return p, errors.Join(problems...)
}

func parsePersona(stem string, src []byte) (core.Persona, []error) {
	fm, body, err := parseFrontmatter(src)
	if err != nil {
		return core.Persona{}, []error{fmt.Errorf("persona %s: %w", stem, err)}
	}
	// Prompt is the whole body. The personality section skills/ used to
	// split out of a persona file is pruned by this unit: identity is one
	// document, not a base plus an overlay.
	p := core.Persona{
		Name:        fm.scalars["name"],
		Description: fm.scalars["description"],
		Prompt:      strings.TrimSpace(body),
	}
	problems := nameProblems("persona", stem, p.Name)
	if p.Description == "" {
		problems = append(problems, fmt.Errorf("persona %s: description is required", stem))
	}
	if p.Prompt == "" {
		problems = append(problems, fmt.Errorf("persona %s: body is required — a persona with no prompt says nothing", stem))
	}
	return p, problems
}

func parseChecklist(stem string, src []byte) (core.ChecklistRecord, []error) {
	fm, body, err := parseFrontmatter(src)
	if err != nil {
		return core.ChecklistRecord{}, []error{fmt.Errorf("checklist %s: %w", stem, err)}
	}
	// TaskID and Origin stay zero: a document has no ledger identity, and
	// its provenance is stamped from the manifest at apply time.
	c := core.ChecklistRecord{
		Name:    fm.scalars["name"],
		Purpose: fm.scalars["purpose"],
		Suits:   fm.lists["suits"],
		Requires: core.ChecklistRequires{
			Capabilities: fm.lists["requires_capabilities"],
			Channels:     fm.lists["requires_channels"],
		},
		Target:  fm.scalars["target"],
		Targets: fm.scalars["targets"],
		Mode:    fm.scalars["mode"],
		Steps:   parseSteps(body),
	}
	if c.Target == "" {
		c.Target = core.ChecklistTargetProject
	}
	if c.Mode == "" {
		c.Mode = core.ChecklistModeEager
	}
	problems := nameProblems("checklist", stem, c.Name)
	if c.Purpose == "" {
		problems = append(problems, fmt.Errorf("checklist %s: purpose is required — the dialog lists actions by it", stem))
	}
	if len(c.Steps) == 0 {
		problems = append(problems, fmt.Errorf("checklist %s: needs at least one numbered or dashed step", stem))
	}
	for _, s := range c.Suits {
		if !nameRe.MatchString(s) {
			problems = append(problems, fmt.Errorf("checklist %s: invalid suits entry %q", stem, s))
		}
	}
	if !core.ValidChecklistTarget(c.Target) {
		problems = append(problems, fmt.Errorf("checklist %s: target %q must be %s or %s", stem, c.Target, core.ChecklistTargetProject, core.ChecklistTargetTask))
	}
	if c.Targets != "" && c.Target != core.ChecklistTargetTask {
		problems = append(problems, fmt.Errorf("checklist %s: targets narrows the dispatchable tasks and needs target: %s", stem, core.ChecklistTargetTask))
	}
	if !core.ValidChecklistMode(c.Mode) {
		problems = append(problems, fmt.Errorf("checklist %s: mode %q must be %s, %s, or %s", stem, c.Mode, core.ChecklistModeEager, core.ChecklistModeInteractive, core.ChecklistModeResident))
	}
	return c, problems
}

func parseChannel(stem string, src []byte) (core.ChannelRecord, []error) {
	fm, body, err := parseFrontmatter(src)
	if err != nil {
		return core.ChannelRecord{}, []error{fmt.Errorf("channel %s: %w", stem, err)}
	}
	// Type and Address stay zero. A profile declares a channel EXPECTATION —
	// the handle and what belongs in it; which medium carries it, and at
	// which address, are per-project, per-machine facts set with
	// `atm channel endpoint add` after apply.
	c := core.ChannelRecord{
		Name:     fm.scalars["name"],
		RoleHint: fm.scalars["role_hint"],
		Purpose:  strings.TrimSpace(body),
	}
	if c.RoleHint == "" {
		c.RoleHint = core.ChannelRoleHome
	}
	problems := nameProblems("channel", stem, c.Name)
	if c.Purpose == "" {
		problems = append(problems, fmt.Errorf("channel %s: purpose body is required — it is what tells an agent what belongs here", stem))
	}
	if !core.ValidChannelRole(c.RoleHint) {
		problems = append(problems, fmt.Errorf("channel %s: role_hint %q must be %s or %s", stem, c.RoleHint, core.ChannelRoleHome, core.ChannelRoleBroadcast))
	}
	// Addresses are per-project, per-machine facts. A profile that carried
	// one would be unportable the moment it left its author's workspace.
	for _, k := range []string{"address", "url", "workspace", "database", "page", "channel_id", "type"} {
		if _, ok := fm.scalars[k]; ok {
			problems = append(problems, fmt.Errorf("channel %s: %q is not profile content — an address or type is a per-project fact set with `atm channel endpoint add`", stem, k))
		}
	}
	return c, problems
}

// nameProblems checks a document's frontmatter name against its filename.
// The filename is the identity every other document refers to, so a mismatch
// also drops the document from the cross-document checks — hence a slice
// return rather than a bool.
func nameProblems(kind, stem, name string) []error {
	if !nameRe.MatchString(name) {
		return []error{fmt.Errorf("%s %s: invalid or missing name %q", kind, stem, name)}
	}
	if name != stem {
		return []error{fmt.Errorf("%s %s: frontmatter name %q must match the filename", kind, stem, name)}
	}
	return nil
}

// yamlDoc is the parsed subset of YAML a manifest or a frontmatter header
// uses: scalars, inline lists, and folded block scalars.
type yamlDoc struct {
	scalars map[string]string
	lists   map[string][]string
}

// parseFrontmatter splits src into its `---` header and the body after it.
func parseFrontmatter(src []byte) (yamlDoc, string, error) {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return yamlDoc{}, "", fmt.Errorf("missing frontmatter: file must start with ---")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return yamlDoc{}, "", fmt.Errorf("unterminated frontmatter: closing --- not found")
	}
	doc, err := parseYAMLScalars(strings.Join(lines[1:end], "\n"))
	if err != nil {
		return yamlDoc{}, "", err
	}
	return doc, strings.Join(lines[end+1:], "\n"), nil
}

// parseYAMLScalars reads the flat YAML subset profiles use: `key: value`,
// `key: [a, b]`, and `key: >` followed by an indented block folded into one
// line. Nesting, anchors, and block sequences are deliberately unsupported —
// a profile document that needs them is saying something the format does
// not mean, and a hand-rolled parser that silently half-reads real YAML
// would be worse than one that refuses it.
func parseYAMLScalars(text string) (yamlDoc, error) {
	doc := yamlDoc{scalars: map[string]string{}, lists: map[string][]string{}}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if line != strings.TrimLeft(line, " \t") {
			return yamlDoc{}, fmt.Errorf("line %d: unexpected indentation — the profile format is flat `key: value`", i+1)
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(k) == "" {
			return yamlDoc{}, fmt.Errorf("line %d: want `key: value`, got %q", i+1, line)
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		switch {
		case val == ">" || val == "|":
			block, next := foldedBlock(lines, i+1, val == ">")
			doc.scalars[key] = block
			i = next - 1
		case strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]"):
			inner := strings.TrimSpace(val[1 : len(val)-1])
			list := []string{}
			if inner != "" {
				for _, item := range strings.Split(inner, ",") {
					list = append(list, unquote(strings.TrimSpace(item)))
				}
			}
			doc.lists[key] = list
		default:
			doc.scalars[key] = unquote(val)
		}
	}
	return doc, nil
}

// foldedBlock consumes the indented lines starting at from. `>` folds them
// into one paragraph (blank lines become paragraph breaks); `|` keeps the
// line breaks. It returns the value and the index of the first line after
// the block.
func foldedBlock(lines []string, from int, fold bool) (string, int) {
	var block []string
	i := from
	for ; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			block = append(block, "")
			continue
		}
		if line == strings.TrimLeft(line, " \t") {
			break
		}
		block = append(block, strings.TrimSpace(line))
	}
	for len(block) > 0 && block[len(block)-1] == "" {
		block = block[:len(block)-1]
	}
	if !fold {
		return strings.Join(block, "\n"), i
	}
	var paragraphs []string
	var cur []string
	for _, l := range block {
		if l == "" {
			if len(cur) > 0 {
				paragraphs = append(paragraphs, strings.Join(cur, " "))
				cur = nil
			}
			continue
		}
		cur = append(cur, l)
	}
	if len(cur) > 0 {
		paragraphs = append(paragraphs, strings.Join(cur, " "))
	}
	return strings.Join(paragraphs, "\n\n"), i
}

// unquote strips one layer of matching YAML quotes. A value containing a
// colon — a targets expression, a purpose — must be quoted; the quotes are
// syntax, and every surface wants the value itself.
func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// parseSteps reads a markdown nested list ("-" or "N." markers, indentation
// as depth) into a step tree. Non-step lines are ignored; a dedent attaches
// to the nearest shallower ancestor.
func parseSteps(body string) []core.ChecklistStep {
	type node struct {
		text     string
		children []*node
	}
	root := &node{}
	type frame struct {
		indent int
		n      *node
	}
	stack := []frame{{-1, root}}
	for _, line := range strings.Split(body, "\n") {
		m := stepLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ind := indentWidth(m[1])
		for len(stack) > 1 && ind <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		n := &node{text: strings.TrimSpace(m[2])}
		parent := stack[len(stack)-1].n
		parent.children = append(parent.children, n)
		stack = append(stack, frame{ind, n})
	}
	var conv func([]*node) []core.ChecklistStep
	conv = func(in []*node) []core.ChecklistStep {
		if len(in) == 0 {
			return nil
		}
		out := make([]core.ChecklistStep, len(in))
		for i, n := range in {
			out[i] = core.ChecklistStep{Text: n.text, Children: conv(n.children)}
		}
		return out
	}
	return conv(root.children)
}

// indentWidth measures leading whitespace in columns; a tab counts as four.
func indentWidth(ws string) int {
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
