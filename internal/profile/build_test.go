package profile

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
	"testing/fstest"
)

func TestBuildProducesAnIdentifiedArtifact(t *testing.T) {
	var buf bytes.Buffer
	art, err := Build(goodFiles(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if art.Name != "scrumban" || art.Version != "1.0.0" {
		t.Fatalf("artifact identity = %+v", art)
	}
	if !strings.HasPrefix(art.Digest, "sha256:") || len(art.Digest) != len("sha256:")+64 {
		t.Fatalf("digest = %q, want a sha256: hex digest", art.Digest)
	}
	if art.Size != int64(buf.Len()) || art.Size == 0 {
		t.Fatalf("size = %d, buffer = %d", art.Size, buf.Len())
	}
	if art.Filename() != "scrumban-1.0.0.atmprofile" {
		t.Fatalf("Filename() = %q", art.Filename())
	}
}

// Built means validated: a profile that Load rejects must never become an
// artifact, or the failure surfaces on someone else's machine at install.
func TestBuildValidatesFirst(t *testing.T) {
	var buf bytes.Buffer
	_, err := Build(withFile("checklists/planning.md", "---\nname: planning\npurpose: p\nsuits: [ghost]\n---\n1. step\n"), &buf)
	if err == nil {
		t.Fatal("Build packed an invalid profile")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v, want the validation failure", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("Build wrote %d bytes for a profile it rejected", buf.Len())
	}
}

// dev is what --dir mode applies as, never something you publish: an
// artifact carrying it could not be resolved by version.
func TestBuildRefusesDevVersion(t *testing.T) {
	var buf bytes.Buffer
	_, err := Build(withFile("manifest.yaml", "name: scrumban\nversion: dev\nformat: 1\nrequires_capabilities: [scrum, channel]\n"), &buf)
	if err == nil || !strings.Contains(err.Error(), DevVersion) {
		t.Fatalf("err = %v, want a refusal naming %q", err, DevVersion)
	}
}

// The digest identifies CONTENT. Building the same source twice must give
// the same bytes, or a digest tells a reader nothing.
func TestBuildIsReproducible(t *testing.T) {
	var a, b bytes.Buffer
	art1, err := Build(goodFiles(), &a)
	if err != nil {
		t.Fatal(err)
	}
	art2, err := Build(goodFiles(), &b)
	if err != nil {
		t.Fatal(err)
	}
	if art1.Digest != art2.Digest {
		t.Fatalf("digests differ across builds: %s vs %s", art1.Digest, art2.Digest)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("artifact bytes differ across builds of identical sources")
	}
}

func TestBuildDigestChangesWithContent(t *testing.T) {
	var a, b bytes.Buffer
	art1, _ := Build(goodFiles(), &a)
	art2, err := Build(withFile("channels/prs.md", "---\nname: prs\n---\nThe PR log.\n"), &b)
	if err != nil {
		t.Fatal(err)
	}
	if art1.Digest == art2.Digest {
		t.Fatal("adding a document did not change the digest")
	}
}

// An artifact round-trips: what Build packs, Open reads back as the same
// profile.
func TestBuildRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if _, err := Build(goodFiles(), &buf); err != nil {
		t.Fatal(err)
	}
	fsys, err := OpenArtifact(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Personas) != 2 || len(p.Checklists) != 2 || len(p.Channels) != 1 {
		t.Fatalf("round-tripped profile = %+v", p)
	}
}

// Files a profile does not own are not packed: an artifact is the format,
// not a snapshot of whatever sat in the author's directory.
func TestBuildPacksOnlyProfileContent(t *testing.T) {
	src := goodFiles()
	src["notes.txt"] = &fstest.MapFile{Data: []byte("scratch")}
	src[".git/config"] = &fstest.MapFile{Data: []byte("[core]")}
	var buf bytes.Buffer
	if _, err := Build(src, &buf); err != nil {
		t.Fatal(err)
	}
	names, err := artifactNames(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if n == "notes.txt" || strings.HasPrefix(n, ".git/") {
			t.Fatalf("artifact carries %q; contents = %v", n, names)
		}
	}
	if !contains(names, "manifest.yaml") || !contains(names, "checklists/planning.md") {
		t.Fatalf("artifact missing profile content: %v", names)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// artifactNames lists the paths an artifact carries, in stored order.
func artifactNames(r io.Reader) ([]string, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	var out []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, h.Name)
	}
}
