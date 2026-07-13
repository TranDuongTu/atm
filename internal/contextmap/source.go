// Package contextmap implements the atm context capability: it records where
// each context:* pointer came from, and reports which pointers have drifted
// from reality. It owns its slice of the label substrate -- the context kinds,
// the knowledge lifecycle namespace, the provenance comment kind, and the
// context-current board -- and ensures that vocabulary exists before using it.
//
// See docs/architecture/label-substrate-and-capabilities.md for the pattern,
// and docs/superpowers/specs/2026-07-13-context-map-refresh-design.md.
package contextmap

import (
	"fmt"
	"strings"
)

// Kind classifies a source by how -- and whether -- it can be witnessed.
type Kind string

const (
	KindGit      Kind = "git"      // path in the repo; witnessed by git object id
	KindFile     Kind = "file"     // path outside the repo; witnessed by content hash
	KindURL      Kind = "url"      // fetched over HTTP; witnessed by body hash
	KindExternal Kind = "external" // Jira, Notion, ...; NOT witnessable, aged only
)

// Source is a kinded locator: the thing a context pointer was derived from.
type Source struct {
	Kind    Kind
	Locator string
}

// Provable reports whether this kind of source can be witnessed locally. When
// false, drift is undetectable and check reports age instead. ATM speaks no
// third-party API, so external sources are never provable.
func (s Source) Provable() bool { return s.Kind != KindExternal }

func (s Source) String() string { return string(s.Kind) + ":" + s.Locator }

// ParseSource parses a kinded locator such as "git:internal/store".
func ParseSource(s string) (Source, error) {
	kindStr, locator, ok := strings.Cut(s, ":")
	if !ok || locator == "" {
		return Source{}, fmt.Errorf("source %q: want <kind>:<locator>, e.g. git:internal/store", s)
	}
	kind := Kind(kindStr)
	switch kind {
	case KindGit, KindFile, KindURL, KindExternal:
	default:
		return Source{}, fmt.Errorf("source %q: unknown kind %q (want git, file, url, or external)", s, kindStr)
	}
	return Source{Kind: kind, Locator: locator}, nil
}