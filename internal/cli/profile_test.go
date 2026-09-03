package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeProfileDir lays out a minimal valid profile directory and returns it.
func writeProfileDir(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("manifest.yaml", "name: demo\nversion: "+version+"\nformat: 1\ndescription: a demo profile\nrequires_capabilities: [scrum]\n")
	write("personas/coder.md", "---\nname: coder\ndescription: writes code\n---\n# Persona: coder\n\nYou write code.\n")
	write("checklists/work.md", "---\nname: work\npurpose: do the work\nsuits: [coder]\nrequires_capabilities: [scrum]\n---\n1. Do it.\n")
	return dir
}

func TestProfileBuildWritesADigestIdentifiedArtifact(t *testing.T) {
	st := newTestCLI(t)
	dir := writeProfileDir(t, "1.0.0")
	out := filepath.Join(t.TempDir(), "demo.atmprofile")

	text := runArgsOut(t, st, "profile", "build", "--dir", dir, "-o", out)
	mustContain(t, text, "built demo@1.0.0")
	mustContain(t, text, "digest sha256:")

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("artifact not written: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("artifact is empty")
	}
}

// A profile that does not validate never becomes a file: the failure belongs
// on the author's machine, not on whoever installs it.
func TestProfileBuildWritesNothingWhenInvalid(t *testing.T) {
	st := newTestCLI(t)
	dir := writeProfileDir(t, "1.0.0")
	if err := os.WriteFile(filepath.Join(dir, "checklists", "work.md"),
		[]byte("---\nname: work\npurpose: p\nsuits: [ghost]\n---\n1. step\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "demo.atmprofile")
	msg, code := runChecklistErrText(t, st, "profile", "build", "--dir", dir, "-o", out)
	if code == ExitSuccess {
		t.Fatal("build succeeded on an invalid profile")
	}
	if !strings.Contains(msg, "ghost") {
		t.Fatalf("error = %q, want the validation failure", msg)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("build wrote an artifact for a profile it rejected")
	}
}

func TestProfileInstallThenList(t *testing.T) {
	st := newTestCLI(t)
	artifact := filepath.Join(t.TempDir(), "demo.atmprofile")
	buildOut := runArgsOut(t, st, "profile", "build", "--dir", writeProfileDir(t, "1.0.0"), "-o", artifact)
	digest := digestFrom(t, buildOut)

	out := runArgsOut(t, st, "profile", "install", artifact, "--verify", digest)
	mustContain(t, out, "installed demo@1.0.0")
	// Installing must say plainly that it changed no project: the whole
	// point of the two-step lifecycle is that arrival is not adoption.
	mustContain(t, out, "Nothing changed in any project")

	out = runArgsOut(t, st, "profile", "list")
	mustContain(t, out, "demo@1.0.0")
	mustContain(t, out, "installed")
	// The profile the binary ships is available without anyone installing it.
	mustContain(t, out, "scrumban@")
	mustContain(t, out, "embedded")
}

func TestProfileInstallRefusesAWrongDigest(t *testing.T) {
	st := newTestCLI(t)
	artifact := filepath.Join(t.TempDir(), "demo.atmprofile")
	_ = runArgsOut(t, st, "profile", "build", "--dir", writeProfileDir(t, "1.0.0"), "-o", artifact)

	msg, code := runChecklistErrText(t, st, "profile", "install", artifact, "--verify", "sha256:"+strings.Repeat("0", 64))
	if code == ExitSuccess {
		t.Fatal("install accepted a mismatched digest")
	}
	if !strings.Contains(msg, "digest mismatch") {
		t.Fatalf("error = %q", msg)
	}
	out := runArgsOut(t, st, "profile", "list")
	if strings.Contains(out, "demo@") {
		t.Fatalf("a refused install left the profile listed:\n%s", out)
	}
}

// A newer install outranks the embedded copy without a binary upgrade; the
// listing shows both, newest first, so what a bare name resolves to is the
// first row.
func TestProfileListOrdersNewestFirstWithinAName(t *testing.T) {
	st := newTestCLI(t)
	for _, v := range []string{"1.0.0", "1.2.0"} {
		artifact := filepath.Join(t.TempDir(), "demo.atmprofile")
		_ = runArgsOut(t, st, "profile", "build", "--dir", writeProfileDir(t, v), "-o", artifact)
		_ = runArgsOut(t, st, "profile", "install", artifact)
	}
	out := runArgsOut(t, st, "profile", "list")
	first, second := strings.Index(out, "demo@1.2.0"), strings.Index(out, "demo@1.0.0")
	if first < 0 || second < 0 {
		t.Fatalf("both versions must be listed:\n%s", out)
	}
	if first > second {
		t.Fatalf("newest version must sort first:\n%s", out)
	}
}

func TestProfileListJSONIsTheAgentEndpoint(t *testing.T) {
	st := newTestCLI(t)
	st.output = outputJSON
	out := runArgsOut(t, st, "profile", "list")
	mustContain(t, out, `"profiles"`)
	mustContain(t, out, `"name": "scrumban"`)
	mustContain(t, out, `"embedded": true`)
}

func digestFrom(t *testing.T, buildOutput string) string {
	t.Helper()
	for _, line := range strings.Split(buildOutput, "\n") {
		if i := strings.Index(line, "sha256:"); i >= 0 {
			return strings.TrimSpace(line[i:])
		}
	}
	t.Fatalf("no digest in build output:\n%s", buildOutput)
	return ""
}
