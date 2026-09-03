package store

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
	"sort"
	"strings"
	"time"

	"atm/internal/core"
	"atm/internal/profile"
	"atm/profiles"
)

// The profile side store: <store>/profiles/<name>/<version>/, a plain-file
// side store beside personas/ (docs/architecture/logical-components.md names
// those as this package's responsibility). Installing a profile changes no
// project — it only makes one available to apply.

// installMetaExt names the sidecar recording how a version was installed.
// It sits BESIDE the version directory, never inside it: install bookkeeping
// is not profile content, and a rebuild of an installed directory must not
// pack the store's own records.
const installMetaExt = ".install.json"

// profileRoot is where installed profiles live under the store.
//
// Embedded profiles are NOT written here: they are served virtually, read
// straight from the binary. Materializing them would make two binaries
// carrying different embedded versions fight over the same files, and would
// leave a stale copy behind after an upgrade.
func (s *Store) profileRoot() string { return filepath.Join(s.Root, "profiles") }

// embeddedProfiles is the set this binary ships, keyed by name — the same
// treatment built-in personas get (see builtinPersona in persona.go).
// Tests override the hook to work against a fixture profile instead of the
// real scrumban.
func (s *Store) embeddedProfiles() map[string]fs.FS {
	if s.embeddedProfilesFn != nil {
		return s.embeddedProfilesFn()
	}
	return profiles.Embedded()
}

// List returns every available profile, newest first within a name and
// name-ordered across them — so the first row for a name is what a bare
// name resolves to.
func (s *Store) ListProfiles() ([]core.ProfileEntry, error) {
	var out []core.ProfileEntry
	names, err := s.installedProfileNames()
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		versions, err := s.installedProfileVersions(name)
		if err != nil {
			return nil, err
		}
		out = append(out, versions...)
	}
	for name, fsys := range s.embeddedProfiles() {
		e, err := s.embeddedProfileEntry(name, fsys)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return core.CompareProfileVersions(out[i].Version, out[j].Version) > 0
	})
	return out, nil
}

// Open resolves a profile to a filesystem ready for Load. An empty version
// takes the highest available across installed AND embedded copies, so a
// newer installed profile takes effect without a binary upgrade and a binary
// upgrade takes effect without an install.
func (s *Store) openProfile(name, version string) (fs.FS, core.ProfileEntry, error) {
	candidates, err := s.profileCandidates(name)
	if err != nil {
		return nil, core.ProfileEntry{}, err
	}
	if len(candidates) == 0 {
		return nil, core.ProfileEntry{}, fmt.Errorf("profile %q is not installed (see: atm profile list)", name)
	}
	if version != "" {
		for _, e := range candidates {
			if e.Version == version {
				return s.fsFor(e)
			}
		}
		return nil, core.ProfileEntry{}, fmt.Errorf("profile %s@%s is not installed; available: %s", name, version, strings.Join(profileVersionsOf(candidates), ", "))
	}
	best := candidates[0]
	for _, e := range candidates[1:] {
		if core.CompareProfileVersions(e.Version, best.Version) > 0 {
			best = e
		}
	}
	return s.fsFor(best)
}

func (s *Store) fsFor(e core.ProfileEntry) (fs.FS, core.ProfileEntry, error) {
	if e.Embedded {
		return s.embeddedProfiles()[e.Name], e, nil
	}
	return os.DirFS(e.Path), e, nil
}

func (s *Store) profileCandidates(name string) ([]core.ProfileEntry, error) {
	out, err := s.installedProfileVersions(name)
	if err != nil {
		return nil, err
	}
	if fsys, ok := s.embeddedProfiles()[name]; ok {
		e, err := s.embeddedProfileEntry(name, fsys)
		if err != nil {
			return nil, err
		}
		// An installed copy of the same version wins: it is the one a
		// reader can inspect on disk.
		if !containsProfileVersion(out, e.Version) {
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
func (s *Store) installProfile(r io.Reader, wantDigest string) (core.ProfileEntry, error) {
	raw, err := io.ReadAll(io.LimitReader(r, profile.MaxArtifactBytes+1))
	if err != nil {
		return core.ProfileEntry{}, err
	}
	if int64(len(raw)) > profile.MaxArtifactBytes {
		return core.ProfileEntry{}, fmt.Errorf("profile artifact: larger than %d bytes", profile.MaxArtifactBytes)
	}
	digest := digestOf(raw)
	if wantDigest != "" && !strings.EqualFold(wantDigest, digest) {
		return core.ProfileEntry{}, fmt.Errorf("profile artifact: digest mismatch — expected %s, got %s", wantDigest, digest)
	}
	files, err := profile.ReadArtifact(bytes.NewReader(raw))
	if err != nil {
		return core.ProfileEntry{}, err
	}
	p, err := profile.Load(profile.ArtifactFS(files))
	if err != nil {
		return core.ProfileEntry{}, fmt.Errorf("refusing to install: %w", err)
	}

	dir := filepath.Join(s.profileRoot(), p.Manifest.Name, p.Manifest.Version)
	// Stage beside the destination and swap, so an interrupted install
	// cannot leave a half-written profile where a reader would find it.
	staging, err := s.profileStaging(p.Manifest.Name)
	if err != nil {
		return core.ProfileEntry{}, err
	}
	defer os.RemoveAll(staging)
	for name, body := range files {
		dst := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return core.ProfileEntry{}, err
		}
		if err := os.WriteFile(dst, body, 0o644); err != nil {
			return core.ProfileEntry{}, err
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return core.ProfileEntry{}, err
	}
	if err := os.Rename(staging, dir); err != nil {
		return core.ProfileEntry{}, err
	}
	e := core.ProfileEntry{
		Name:        p.Manifest.Name,
		Version:     p.Manifest.Version,
		Description: p.Manifest.Description,
		Digest:      digest,
		Path:        dir,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.writeProfileInstallMeta(e); err != nil {
		return core.ProfileEntry{}, err
	}
	return e, nil
}

func (s *Store) profileStaging(name string) (string, error) {
	parent := filepath.Join(s.profileRoot(), name)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(parent, ".staging-")
}

func (s *Store) profileMetaPath(name, version string) string {
	return filepath.Join(s.profileRoot(), name, version+installMetaExt)
}

func (s *Store) writeProfileInstallMeta(e core.ProfileEntry) error {
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.profileMetaPath(e.Name, e.Version), append(b, '\n'), 0o644)
}

func (s *Store) installedProfileNames() ([]string, error) {
	entries, err := os.ReadDir(s.profileRoot())
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

func (s *Store) installedProfileVersions(name string) ([]core.ProfileEntry, error) {
	dir := filepath.Join(s.profileRoot(), name)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []core.ProfileEntry
	for _, de := range entries {
		if !de.IsDir() || strings.HasPrefix(de.Name(), ".") {
			continue
		}
		e := core.ProfileEntry{Name: name, Version: de.Name(), Path: filepath.Join(dir, de.Name())}
		if b, err := os.ReadFile(s.profileMetaPath(name, de.Name())); err == nil {
			var stored core.ProfileEntry
			if json.Unmarshal(b, &stored) == nil {
				e.Digest, e.InstalledAt, e.Description = stored.Digest, stored.InstalledAt, stored.Description
			}
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) embeddedProfileEntry(name string, fsys fs.FS) (core.ProfileEntry, error) {
	m, err := profile.LoadManifest(fsys)
	if err != nil {
		return core.ProfileEntry{}, fmt.Errorf("embedded profile %s: %w", name, err)
	}
	return core.ProfileEntry{Name: m.Name, Version: m.Version, Description: m.Description, Embedded: true}, nil
}

func containsProfileVersion(es []core.ProfileEntry, v string) bool {
	for _, e := range es {
		if e.Version == v {
			return true
		}
	}
	return false
}

func profileVersionsOf(es []core.ProfileEntry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Version)
	}
	sort.Slice(out, func(i, j int) bool { return core.CompareProfileVersions(out[i], out[j]) > 0 })
	return out
}

// digestOf is the artifact identity: sha256 over the exact published bytes,
// so a reader can recompute it with nothing but sha256sum.
func digestOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// GetProfile loads one profile, resolving the version like openProfile.
// The caller gets DATA, never a filesystem handle: reading the format is
// internal/profile's job and keeping the bytes is this package's, so
// nothing above the store ever holds a file cursor into the profile store.
func (s *Store) GetProfile(name, version string) (*core.Profile, core.ProfileEntry, error) {
	fsys, e, err := s.openProfile(name, version)
	if err != nil {
		return nil, core.ProfileEntry{}, err
	}
	p, err := profile.Load(fsys)
	if err != nil {
		return nil, core.ProfileEntry{}, fmt.Errorf("profile %s: %w", e.Ref(), err)
	}
	return p, e, nil
}

// InstallProfile installs a built artifact from a local file.
func (s *Store) InstallProfile(artifactPath, wantDigest string) (core.ProfileEntry, error) {
	f, err := os.Open(artifactPath)
	if err != nil {
		return core.ProfileEntry{}, err
	}
	defer f.Close()
	return s.installProfile(f, wantDigest)
}
