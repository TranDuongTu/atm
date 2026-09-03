// Package profiles carries the profiles ATM ships, embedded into the binary.
// It lives at the repository top level, outside internal/, because a profile
// directory is PUBLIC FORMAT: scrumban is written exactly as a third-party
// profile would be, and the same directory is what `atm profile build` packs
// into a distributable artifact. Config visibly separated from code.
//
// Pure leaf: it embeds bytes and nothing else. The loader that reads them
// lives in internal/profile, and the embedded FS is served virtually as a
// pre-installed entry in the profile store — never materialized to disk, so
// two binaries carrying different embedded versions can never fight over the
// same files.
package profiles

import (
	"embed"
	"io/fs"
)

//go:embed scrumban
var files embed.FS

// Scrumban is the name of the profile ATM ships: the standard operating
// model of weekly planning, daily standup, periodic retrospect,
// design-gated incremental implementation, staff-held review, and
// customer-seat QA.
const Scrumban = "scrumban"

// FS returns the embedded profile directory rooted at its manifest, ready
// for profile.Load. An unknown name is reported rather than panicking: the
// set of embedded profiles is a build-time fact, but callers resolve names
// that come from a project's records.
func FS(name string) (fs.FS, bool) {
	sub, err := fs.Sub(files, name)
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "manifest.yaml"); err != nil {
		return nil, false
	}
	return sub, true
}

// Names lists the embedded profiles, sorted.
func Names() []string {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// Embedded is every profile the binary ships, keyed by name — the set the
// composition root hands to the profile store to serve as pre-installed.
func Embedded() map[string]fs.FS {
	out := map[string]fs.FS{}
	for _, name := range Names() {
		if sub, ok := FS(name); ok {
			out[name] = sub
		}
	}
	return out
}
