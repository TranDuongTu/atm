package profile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// installMetaExt names the sidecar recording how a version was installed.
// It sits BESIDE the version directory, never inside it: install bookkeeping
// is not profile content, and a rebuild of an installed directory must not
// pack the store's own records.
const installMetaExt = ".install.json"

// Entry is one profile available on this machine.
type Entry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	// Digest of the artifact this version was installed from; empty for an
	// embedded profile, which has no artifact.
	Digest string `json:"digest,omitempty"`
	// Embedded marks a profile served from the binary rather than from disk.
	Embedded bool `json:"embedded"`
	// Path is the version directory on disk; empty when embedded.
	Path        string `json:"path,omitempty"`
	InstalledAt string `json:"installed_at,omitempty"`
}

// Ref is the entry's name@version.
func (e Entry) Ref() string { return e.Name + "@" + e.Version }

// Store is the machine-local set of profiles: those installed under root,
// plus those embedded in the binary.
//
// Embedded profiles are served VIRTUALLY — read straight from the embedded
// filesystem, never materialized to disk. Writing them out would make two
// binaries carrying different embedded versions fight over the same files,
// and would leave a stale copy behind after an upgrade.
type Store struct {
	root     string
	embedded map[string]fs.FS
}

// NewStore builds a store over an install root and a set of embedded
// profiles keyed by name. Neither has to exist.
func NewStore(root string, embedded map[string]fs.FS) *Store {
	return &Store{root: root, embedded: embedded}
}

// Root is the directory installed profiles live under.
func (s *Store) Root() string { return s.root }

// List returns every available profile, newest first within a name and
// name-ordered across them — so the first row for a name is what a bare
// name resolves to.
func (s *Store) List() ([]Entry, error) {
	var out []Entry
	names, err := s.installedNames()
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		versions, err := s.installedVersions(name)
		if err != nil {
			return nil, err
		}
		out = append(out, versions...)
	}
	for name, fsys := range s.embedded {
		e, err := s.embeddedEntry(name, fsys)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return compareVersions(out[i].Version, out[j].Version) > 0
	})
	return out, nil
}

// Open resolves a profile to a filesystem ready for Load. An empty version
// takes the highest available across installed AND embedded copies, so a
// newer installed profile takes effect without a binary upgrade and a binary
// upgrade takes effect without an install.
func (s *Store) Open(name, version string) (fs.FS, Entry, error) {
	candidates, err := s.candidates(name)
	if err != nil {
		return nil, Entry{}, err
	}
	if len(candidates) == 0 {
		return nil, Entry{}, fmt.Errorf("profile %q is not installed (see: atm profile list)", name)
	}
	if version != "" {
		for _, e := range candidates {
			if e.Version == version {
				return s.fsFor(e)
			}
		}
		return nil, Entry{}, fmt.Errorf("profile %s@%s is not installed; available: %s", name, version, strings.Join(versionsOf(candidates), ", "))
	}
	best := candidates[0]
	for _, e := range candidates[1:] {
		if compareVersions(e.Version, best.Version) > 0 {
			best = e
		}
	}
	return s.fsFor(best)
}

func (s *Store) fsFor(e Entry) (fs.FS, Entry, error) {
	if e.Embedded {
		return s.embedded[e.Name], e, nil
	}
	return os.DirFS(e.Path), e, nil
}

func (s *Store) candidates(name string) ([]Entry, error) {
	out, err := s.installedVersions(name)
	if err != nil {
		return nil, err
	}
	if fsys, ok := s.embedded[name]; ok {
		e, err := s.embeddedEntry(name, fsys)
		if err != nil {
			return nil, err
		}
		// An installed copy of the same version wins: it is the one a
		// reader can inspect on disk.
		if !containsVersion(out, e.Version) {
			out = append(out, e)
		}
	}
	return out, nil
}

// Install unpacks an artifact into the store. wantDigest, when given, must
// match the artifact's own digest.
//
// Order matters: verify, then unpack IN MEMORY, then validate, and only then
// touch the disk. The store holds profiles that are known to load — never
// bytes that merely arrived — and a rejected install leaves nothing behind.
func (s *Store) Install(r io.Reader, wantDigest string) (Entry, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxArtifactBytes+1))
	if err != nil {
		return Entry{}, err
	}
	if int64(len(raw)) > maxArtifactBytes {
		return Entry{}, fmt.Errorf("profile artifact: larger than %d bytes", maxArtifactBytes)
	}
	digest := digestOf(raw)
	if wantDigest != "" && !strings.EqualFold(wantDigest, digest) {
		return Entry{}, fmt.Errorf("profile artifact: digest mismatch — expected %s, got %s", wantDigest, digest)
	}
	files, err := readArtifact(bytes.NewReader(raw))
	if err != nil {
		return Entry{}, err
	}
	p, err := Load(artifactFS(files))
	if err != nil {
		return Entry{}, fmt.Errorf("refusing to install: %w", err)
	}

	dir := filepath.Join(s.root, p.Manifest.Name, p.Manifest.Version)
	// Stage beside the destination and swap, so an interrupted install
	// cannot leave a half-written profile where a reader would find it.
	staging, err := s.staging(p.Manifest.Name)
	if err != nil {
		return Entry{}, err
	}
	defer os.RemoveAll(staging)
	for name, body := range files {
		dst := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return Entry{}, err
		}
		if err := os.WriteFile(dst, body, 0o644); err != nil {
			return Entry{}, err
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return Entry{}, err
	}
	if err := os.Rename(staging, dir); err != nil {
		return Entry{}, err
	}
	e := Entry{
		Name:        p.Manifest.Name,
		Version:     p.Manifest.Version,
		Description: p.Manifest.Description,
		Digest:      digest,
		Path:        dir,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.writeInstallMeta(e); err != nil {
		return Entry{}, err
	}
	return e, nil
}

func (s *Store) staging(name string) (string, error) {
	parent := filepath.Join(s.root, name)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(parent, ".staging-")
}

func (s *Store) metaPath(name, version string) string {
	return filepath.Join(s.root, name, version+installMetaExt)
}

func (s *Store) writeInstallMeta(e Entry) error {
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.metaPath(e.Name, e.Version), append(b, '\n'), 0o644)
}

func (s *Store) installedNames() ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *Store) installedVersions(name string) ([]Entry, error) {
	dir := filepath.Join(s.root, name)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, de := range entries {
		if !de.IsDir() || strings.HasPrefix(de.Name(), ".") {
			continue
		}
		e := Entry{Name: name, Version: de.Name(), Path: filepath.Join(dir, de.Name())}
		if b, err := os.ReadFile(s.metaPath(name, de.Name())); err == nil {
			var stored Entry
			if json.Unmarshal(b, &stored) == nil {
				e.Digest, e.InstalledAt, e.Description = stored.Digest, stored.InstalledAt, stored.Description
			}
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) embeddedEntry(name string, fsys fs.FS) (Entry, error) {
	m, err := loadManifest(fsys)
	if err != nil {
		return Entry{}, fmt.Errorf("embedded profile %s: %w", name, err)
	}
	return Entry{Name: m.Name, Version: m.Version, Description: m.Description, Embedded: true}, nil
}

// digestOf is the artifact identity: sha256 over the exact published bytes,
// so a reader can recompute it with nothing but sha256sum.
func digestOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func containsVersion(es []Entry, v string) bool {
	for _, e := range es {
		if e.Version == v {
			return true
		}
	}
	return false
}

func versionsOf(es []Entry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Version)
	}
	sort.Slice(out, func(i, j int) bool { return compareVersions(out[i], out[j]) > 0 })
	return out
}

var versionPartRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?`)

// compareVersions orders two semver versions: -1, 0, or 1. A prerelease
// sorts BELOW its release (1.0.0-rc1 < 1.0.0), per semver. Anything
// unparseable sorts below everything parseable, so a hand-made directory
// name can never shadow a real version.
func compareVersions(a, b string) int {
	am, bm := versionPartRe.FindStringSubmatch(a), versionPartRe.FindStringSubmatch(b)
	switch {
	case am == nil && bm == nil:
		return strings.Compare(a, b)
	case am == nil:
		return -1
	case bm == nil:
		return 1
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(am[i])
		y, _ := strconv.Atoi(bm[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case am[4] == bm[4]:
		return 0
	case am[4] == "":
		return 1
	case bm[4] == "":
		return -1
	}
	return strings.Compare(am[4], bm[4])
}
