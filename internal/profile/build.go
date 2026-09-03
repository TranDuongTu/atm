package profile

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// ArtifactExt is the extension of a built profile.
const ArtifactExt = ".atmprofile"

// maxArtifactBytes caps what an artifact may unpack to. A profile is
// documents; anything approaching this is either a mistake or an attempt to
// fill someone's disk from a downloaded file.
const maxArtifactBytes = 32 << 20

// Artifact identifies a built profile: what it is, and the digest that says
// these exact bytes are it.
type Artifact struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
	Size    int64  `json:"size"`
}

// Ref is the artifact's name@version.
func (a Artifact) Ref() string { return a.Name + "@" + a.Version }

// Filename is the conventional artifact name to publish it under.
func (a Artifact) Filename() string { return a.Name + "-" + a.Version + ArtifactExt }

// Build validates the profile in fsys and writes a canonical artifact to w,
// returning its identity, size, and digest.
//
// BUILT MEANS VALIDATED: a profile that Load refuses never becomes an
// artifact, so a broken profile fails on its author's machine rather than on
// whoever installs it. Nothing is written until validation passes.
func Build(fsys fs.FS, w io.Writer) (Artifact, error) {
	p, err := Load(fsys)
	if err != nil {
		return Artifact{}, err
	}
	if p.Manifest.Version == DevVersion {
		return Artifact{}, fmt.Errorf("profile %s: version %q is what --dir mode applies as, never something to publish — set a semver version to build", p.Manifest.Name, DevVersion)
	}
	files, err := collectContent(fsys)
	if err != nil {
		return Artifact{}, err
	}
	// Digest the bytes we hand out, so it can be recomputed from the
	// published file with nothing but sha256sum.
	sum := sha256.New()
	counter := &countingWriter{w: io.MultiWriter(w, sum)}
	if err := writeArtifact(counter, files); err != nil {
		return Artifact{}, err
	}
	return Artifact{
		Name:    p.Manifest.Name,
		Version: p.Manifest.Version,
		Digest:  "sha256:" + hex.EncodeToString(sum.Sum(nil)),
		Size:    counter.n,
	}, nil
}

// collectContent reads exactly the files the format owns: the manifest and
// the markdown documents in the three document directories. An artifact is
// the FORMAT, not a snapshot of whatever sat in the author's directory — a
// README, an editor swapfile, or a .git directory is not profile content and
// must not travel or change the digest.
func collectContent(fsys fs.FS) (map[string][]byte, error) {
	out := map[string][]byte{}
	src, err := fs.ReadFile(fsys, manifestFile)
	if err != nil {
		return nil, fmt.Errorf("profile: %s is required at the profile root: %w", manifestFile, err)
	}
	out[manifestFile] = src
	for _, dir := range []string{dirPersonas, dirChecklists, dirChannels} {
		for _, stem := range markdownStems(fsys, dir) {
			name := path.Join(dir, stem+".md")
			b, err := fs.ReadFile(fsys, name)
			if err != nil {
				return nil, err
			}
			out[name] = b
		}
	}
	return out, nil
}

// writeArtifact packs files as a CANONICAL tar.gz: entries in sorted path
// order, a zero modification time, fixed mode and ownership, and a gzip
// header carrying neither name nor timestamp. Determinism is the point — a
// digest that changes when the content did not is not an identity, and
// reproducibility is what lets anyone verify a published artifact against
// its source.
func writeArtifact(w io.Writer, files map[string][]byte) error {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	zw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(zw)
	for _, n := range names {
		body := files[n]
		if err := tw.WriteHeader(&tar.Header{
			Name:   n,
			Mode:   0o644,
			Size:   int64(len(body)),
			Format: tar.FormatUSTAR,
		}); err != nil {
			return err
		}
		if _, err := tw.Write(body); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return zw.Close()
}

// OpenArtifact reads an artifact into an in-memory fs.FS ready for Load.
// Unpacking here rather than onto disk is what lets install validate a
// profile BEFORE it exists anywhere a reader could pick it up.
func OpenArtifact(r io.Reader) (fs.FS, error) {
	files, err := readArtifact(r)
	if err != nil {
		return nil, err
	}
	return artifactFS(files), nil
}

func readArtifact(r io.Reader) (map[string][]byte, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("profile artifact: not a gzip stream: %w", err)
	}
	defer zr.Close()
	tr := tar.NewReader(io.LimitReader(zr, maxArtifactBytes+1))
	out := map[string][]byte{}
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("profile artifact: %w", err)
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		// An artifact carries documents. A symlink, a device node, or a
		// path that climbs out of the destination is not content — it is a
		// way to write somewhere the installer never agreed to.
		if h.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("profile artifact: entry %q is not a regular file", h.Name)
		}
		if err := checkArtifactPath(h.Name); err != nil {
			return nil, err
		}
		total += h.Size
		if total > maxArtifactBytes {
			return nil, fmt.Errorf("profile artifact: unpacks to more than %d bytes", maxArtifactBytes)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("profile artifact: %q: %w", h.Name, err)
		}
		out[path.Clean(h.Name)] = b
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("profile artifact: empty")
	}
	return out, nil
}

func checkArtifactPath(name string) error {
	clean := path.Clean(name)
	switch {
	case path.IsAbs(name) || strings.HasPrefix(name, "/"):
		return fmt.Errorf("profile artifact: entry %q is an absolute path", name)
	case clean == ".." || strings.HasPrefix(clean, "../"):
		return fmt.Errorf("profile artifact: entry %q escapes the profile directory", name)
	case strings.ContainsRune(name, '\\'):
		return fmt.Errorf("profile artifact: entry %q contains a backslash", name)
	}
	return nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
