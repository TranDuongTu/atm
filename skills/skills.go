package skills

import (
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"
)

//go:embed persona/*.md
var files embed.FS

var builtinPersonas []PersonaSpec

func init() {
	builtinPersonas = mustLoadPersonas()
}

func mustLoadPersonas() []PersonaSpec {
	var out []PersonaSpec
	for _, name := range mustList("persona") {
		src, err := files.ReadFile(path.Join("persona", name))
		if err != nil {
			panic(fmt.Sprintf("skills: read %s: %v", name, err))
		}
		p, err := ParsePersona(strings.TrimSuffix(name, ".md"), src)
		if err != nil {
			panic(fmt.Sprintf("skills: %v", err))
		}
		out = append(out, p)
	}
	return out
}

func mustList(dir string) []string {
	entries, err := files.ReadDir(dir)
	if err != nil {
		// A pruned directory reads as empty, never a load failure.
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// Personas returns the built-in persona specs (stable, name-sorted order).
func Personas() []PersonaSpec { return append([]PersonaSpec(nil), builtinPersonas...) }

// Persona returns the named built-in persona.
func Persona(name string) (PersonaSpec, bool) {
	for _, p := range builtinPersonas {
		if p.Name == name {
			return p, true
		}
	}
	return PersonaSpec{}, false
}
