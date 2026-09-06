package profile

import (
	"fmt"
	"strings"

	"atm/internal/core"
)

// RenderChecklistDocument is ParseChecklistDocument's inverse: the record as
// the document a profile ships and `atm checklist set` imports. TaskID and
// Origin are ledger identity and provenance, not document content, and are
// never written. target and mode are always written, defaults included, so
// an editor sees the dispatch axes it can change. Empty lists are omitted:
// the parser reads an absent key as nil, which is what records store.
func RenderChecklistDocument(rec core.ChecklistRecord) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", rec.Name)
	fmt.Fprintf(&b, "purpose: %s\n", yamlScalar(rec.Purpose))
	if len(rec.Suits) > 0 {
		fmt.Fprintf(&b, "suits: %s\n", yamlList(rec.Suits))
	}
	if len(rec.Requires.Capabilities) > 0 {
		fmt.Fprintf(&b, "requires_capabilities: %s\n", yamlList(rec.Requires.Capabilities))
	}
	if len(rec.Requires.Channels) > 0 {
		fmt.Fprintf(&b, "requires_channels: %s\n", yamlList(rec.Requires.Channels))
	}
	fmt.Fprintf(&b, "target: %s\n", defaulted(rec.Target, core.ChecklistTargetProject))
	if rec.Targets != "" {
		fmt.Fprintf(&b, "targets: %s\n", yamlScalar(rec.Targets))
	}
	fmt.Fprintf(&b, "mode: %s\n", defaulted(rec.Mode, core.ChecklistModeEager))
	b.WriteString("---\n")
	writeSteps(&b, rec.Steps, 0)
	return []byte(b.String())
}

// writeSteps writes the markdown nested list the parser reads: "N. text"
// with numbering restarting at each level, three spaces per depth — the
// shipped documents' own form. core.RenderChecklistSteps's "1.2.1" is a
// DISPLAY form: the parser's marker is `\d+\.` followed by whitespace, which
// "1.2 " is not, so rendering it here would silently drop every sub-step.
func writeSteps(b *strings.Builder, steps []core.ChecklistStep, depth int) {
	for i, s := range steps {
		fmt.Fprintf(b, "%s%d. %s\n", strings.Repeat("   ", depth), i+1, s.Text)
		writeSteps(b, s.Children, depth+1)
	}
}

// yamlScalar writes a value the way parseYAMLScalars reads it back. A bare
// value is fine unless the parser would take it for something else: a list
// (leading '['), a quoted string (leading quote), a comment ('#'), a block
// marker ('>' or '|'), or padding it would trim. A newline needs a literal
// block.
func yamlScalar(s string) string {
	switch {
	case s == "":
		return ""
	case strings.Contains(s, "\n"):
		return "|\n  " + strings.ReplaceAll(s, "\n", "\n  ")
	case s == ">", s == "|",
		strings.HasPrefix(s, "["), strings.HasPrefix(s, "\""), strings.HasPrefix(s, "'"), strings.HasPrefix(s, "#"),
		strings.TrimSpace(s) != s:
		return "\"" + s + "\""
	}
	return s
}

// yamlList writes an inline list; items are names (nameRe) and never carry
// commas or brackets.
func yamlList(xs []string) string {
	return "[" + strings.Join(xs, ", ") + "]"
}
