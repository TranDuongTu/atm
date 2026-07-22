package skills

import (
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"
)

//go:embed persona/*.md capability/*.md
var files embed.FS

var (
	builtinPersonas     []PersonaSpec
	builtinCapabilities []CapabilitySpec
)

func init() {
	builtinPersonas = mustLoadPersonas()
	builtinCapabilities = mustLoadCapabilities()
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

func mustLoadCapabilities() []CapabilitySpec {
	var out []CapabilitySpec
	for _, name := range mustList("capability") {
		src, err := files.ReadFile(path.Join("capability", name))
		if err != nil {
			panic(fmt.Sprintf("skills: read %s: %v", name, err))
		}
		c, err := ParseCapability(strings.TrimSuffix(name, ".md"), src)
		if err != nil {
			panic(fmt.Sprintf("skills: %v", err))
		}
		out = append(out, c)
	}
	return out
}

func mustList(dir string) []string {
	entries, err := files.ReadDir(dir)
	if err != nil {
		// capability/ may be empty until Task 3; treat missing dir as empty.
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

// Capabilities returns the built-in capability specs (name-sorted order).
func Capabilities() []CapabilitySpec { return append([]CapabilitySpec(nil), builtinCapabilities...) }

// Capability returns the named built-in capability spec.
func Capability(name string) (CapabilitySpec, bool) {
	for _, c := range builtinCapabilities {
		if c.Name == name {
			return c, true
		}
	}
	return CapabilitySpec{}, false
}

// MustCapability is Capability for compile-time-known names (capability
// packages naming their own file); it panics on a missing file, which a unit
// test in the capability package catches.
func MustCapability(name string) CapabilitySpec {
	c, ok := Capability(name)
	if !ok {
		panic(fmt.Sprintf("skills: no capability file for %q", name))
	}
	return c
}
