package store

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"atm/internal/core"
	"atm/internal/profile"
)

// profileFiles is a minimal but complete profile in the documented layout.
// The store's tests build their own artifacts from it so they exercise the
// real packing path rather than a hand-made tarball.
func profileFiles() fstest.MapFS {
	return fstest.MapFS{
		"manifest.yaml":          &fstest.MapFile{Data: []byte("name: scrumban\nversion: 1.0.0\nformat: 1\ndescription: a test profile\nrequires_capabilities: [scrum]\n")},
		"personas/manager.md":    &fstest.MapFile{Data: []byte("---\nname: manager\ndescription: Runs the flow.\n---\n# Persona: manager\n\nYou run things.\n")},
		"checklists/planning.md": &fstest.MapFile{Data: []byte("---\nname: planning\npurpose: the weekly pass\nsuits: [manager]\nrequires_capabilities: [scrum]\n---\n1. Orient.\n")},
	}
}

// withProfileFile returns the profile with one file replaced or added.
func withProfileFile(name, body string) fstest.MapFS {
	fsys := profileFiles()
	fsys[name] = &fstest.MapFile{Data: []byte(body)}
	return fsys
}

// artifactOf builds fsys and returns the bytes plus the digest.
func artifactOf(t *testing.T, fsys fstest.MapFS) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	art, err := profile.Build(fsys, &buf)
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), art.Digest
}

// newProfileTestStore opens a store over a temp dir with no embedded
// profiles unless the caller supplies them.
func newProfileTestStore(t *testing.T, embedded map[string]fs.FS) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	s.embeddedProfilesFn = func() map[string]fs.FS { return embedded }
	return s
}

// versioned returns the profile at a chosen version.
func versioned(v string) fstest.MapFS {
	return withProfileFile("manifest.yaml", "name: scrumban\nversion: "+v+"\nformat: 1\nrequires_capabilities: [scrum]\n")
}

func TestInstallExtractsUnderNameAndVersion(t *testing.T) {
	s := newProfileTestStore(t, nil)
	art, digest := artifactOf(t, profileFiles())
	e, err := s.installProfile(bytes.NewReader(art), "")
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "scrumban" || e.Version != "1.0.0" || e.Digest != digest {
		t.Fatalf("entry = %+v, want the artifact's identity and digest", e)
	}
	if _, err := os.Stat(filepath.Join(s.profileRoot(), "scrumban", "1.0.0", "manifest.yaml")); err != nil {
		t.Fatalf("profile not extracted: %v", err)
	}
	// Install bookkeeping stays OUT of the profile directory, or a rebuild
	// of an installed profile would pack its own metadata.
	entries, _ := os.ReadDir(filepath.Join(s.profileRoot(), "scrumban", "1.0.0"))
	for _, de := range entries {
		if strings.HasPrefix(de.Name(), ".") {
			t.Fatalf("install metadata leaked into the profile directory: %s", de.Name())
		}
	}
}

func TestInstallVerifiesDigest(t *testing.T) {
	s := newProfileTestStore(t, nil)
	art, digest := artifactOf(t, profileFiles())
	if _, err := s.installProfile(bytes.NewReader(art), "sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Fatal("Install accepted an artifact whose digest did not match")
	}
	if _, err := os.Stat(filepath.Join(s.profileRoot(), "scrumban")); !os.IsNotExist(err) {
		t.Fatal("a rejected artifact left files behind")
	}
	if _, err := s.installProfile(bytes.NewReader(art), digest); err != nil {
		t.Fatalf("Install rejected the artifact's own digest: %v", err)
	}
}

// An artifact is content, not a delivery mechanism for arbitrary writes.
func TestInstallRefusesUnsafePaths(t *testing.T) {
	for _, name := range []string{"../escape.md", "/etc/passwd"} {
		if _, err := s2Install(t, name); err == nil {
			t.Fatalf("Install accepted an entry named %q", name)
		}
	}
}

func TestInstallRejectsAnInvalidProfile(t *testing.T) {
	s := newProfileTestStore(t, nil)
	// A tarball that unpacks to something Load refuses must not install:
	// the store holds usable profiles, never bytes that merely arrived.
	var buf bytes.Buffer
	if err := profile.WriteArtifact(&buf, map[string][]byte{"manifest.yaml": []byte("name: x\nversion: 1.0.0\nformat: 1\n"), "checklists/broken.md": []byte("no frontmatter")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.installProfile(bytes.NewReader(buf.Bytes()), ""); err == nil {
		t.Fatal("Install accepted a profile that does not load")
	}
	if _, err := os.Stat(filepath.Join(s.profileRoot(), "x")); !os.IsNotExist(err) {
		t.Fatal("a rejected install left files behind")
	}
}

func TestListShowsInstalledAndEmbedded(t *testing.T) {
	embedded, _ := fs.Sub(fstest.MapFS(profileFiles()), ".")
	s := newProfileTestStore(t, map[string]fs.FS{"scrumban": embedded})
	art, _ := artifactOf(t, versioned("2.0.0"))
	if _, err := s.installProfile(bytes.NewReader(art), ""); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list = %+v, want the installed 2.0.0 and the embedded 1.0.0", list)
	}
	// Newest first, so `profile list` reads as what a bare name resolves to.
	if list[0].Version != "2.0.0" || list[0].Embedded {
		t.Fatalf("list[0] = %+v, want the installed 2.0.0 first", list[0])
	}
	if list[1].Version != "1.0.0" || !list[1].Embedded {
		t.Fatalf("list[1] = %+v, want the embedded 1.0.0", list[1])
	}
}

// A bare name resolves to the highest version anywhere — installed or
// embedded — so installing a newer scrumban takes effect without a binary
// upgrade, and a binary upgrade takes effect without an install.
func TestOpenResolvesHighestVersion(t *testing.T) {
	embedded, _ := fs.Sub(fstest.MapFS(profileFiles()), ".")
	s := newProfileTestStore(t, map[string]fs.FS{"scrumban": embedded})

	_, e, err := s.openProfile("scrumban", "")
	if err != nil {
		t.Fatal(err)
	}
	if e.Version != "1.0.0" || !e.Embedded {
		t.Fatalf("with nothing installed, resolved %+v, want the embedded copy", e)
	}

	art, _ := artifactOf(t, versioned("1.2.0"))
	if _, err := s.installProfile(bytes.NewReader(art), ""); err != nil {
		t.Fatal(err)
	}
	_, e, err = s.openProfile("scrumban", "")
	if err != nil {
		t.Fatal(err)
	}
	if e.Version != "1.2.0" || e.Embedded {
		t.Fatalf("resolved %+v, want the newer installed copy", e)
	}

	// An older install must not win just by being on disk.
	art, _ = artifactOf(t, versioned("0.9.0"))
	if _, err := s.installProfile(bytes.NewReader(art), ""); err != nil {
		t.Fatal(err)
	}
	_, e, _ = s.openProfile("scrumban", "")
	if e.Version != "1.2.0" {
		t.Fatalf("resolved %q after installing an older version", e.Version)
	}
}

func TestOpenPinsAnExactVersion(t *testing.T) {
	embedded, _ := fs.Sub(fstest.MapFS(profileFiles()), ".")
	s := newProfileTestStore(t, map[string]fs.FS{"scrumban": embedded})
	art, _ := artifactOf(t, versioned("2.0.0"))
	if _, err := s.installProfile(bytes.NewReader(art), ""); err != nil {
		t.Fatal(err)
	}
	fsys, e, err := s.openProfile("scrumban", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if e.Version != "1.0.0" || !e.Embedded {
		t.Fatalf("pinned open = %+v", e)
	}
	if p, err := profile.Load(fsys); err != nil || p.Manifest.Version != "1.0.0" {
		t.Fatalf("pinned FS loads as %v / %v", p, err)
	}
	if _, _, err := s.openProfile("scrumban", "9.9.9"); err == nil {
		t.Fatal("Open resolved a version nobody installed")
	}
	if _, _, err := s.openProfile("nosuch", ""); err == nil {
		t.Fatal("Open resolved an unknown profile name")
	}
}

// Installing over an existing version replaces it: re-install is how a
// corrupted or partially written profile is repaired.
func TestInstallIsIdempotentPerVersion(t *testing.T) {
	s := newProfileTestStore(t, nil)
	art, digest := artifactOf(t, profileFiles())
	if _, err := s.installProfile(bytes.NewReader(art), ""); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(s.profileRoot(), "scrumban", "1.0.0", "personas", "stale.md")
	if err := os.WriteFile(stray, []byte("---\nname: stale\ndescription: d\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := s.installProfile(bytes.NewReader(art), digest)
	if err != nil {
		t.Fatal(err)
	}
	if e.Version != "1.0.0" {
		t.Fatalf("entry = %+v", e)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatal("re-install left a file the artifact does not contain")
	}
}

func TestCompareProfileVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.2.0", "1.10.0", -1},
		{"2.0.0", "10.0.0", -1},
		{"1.0.0", "1.0.0-rc1", 1},
		{"1.0.0-rc1", "1.0.0-rc2", -1},
	} {
		if got := core.CompareProfileVersions(tc.a, tc.b); got != tc.want {
			t.Fatalf("core.CompareProfileVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		if got := core.CompareProfileVersions(tc.b, tc.a); got != -tc.want {
			t.Fatalf("compareVersions is not antisymmetric for %q / %q", tc.a, tc.b)
		}
	}
}

// s2Install packs a single-entry artifact under the given path and offers it
// to a fresh store, so path-safety can be tested without a fixture tarball.
func s2Install(t *testing.T, name string) (core.ProfileEntry, error) {
	t.Helper()
	var buf bytes.Buffer
	if err := profile.WriteArtifact(&buf, map[string][]byte{
		"manifest.yaml": []byte("name: x\nversion: 1.0.0\nformat: 1\n"),
		name:            []byte("payload"),
	}); err != nil {
		t.Fatal(err)
	}
	return newProfileTestStore(t, nil).installProfile(bytes.NewReader(buf.Bytes()), "")
}
