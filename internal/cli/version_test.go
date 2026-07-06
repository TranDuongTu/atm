package cli

import (
	"strings"
	"testing"

	"atm/internal/version"
)

// pinDevVersion resets the version package vars to the canonical dev defaults
// (the state a non-ldflags build sees) so the golden fixtures stay stable
// regardless of what release.sh last regenerated version.go to.
func pinDevVersion(t *testing.T) {
	t.Helper()
	version.Version = "dev"
	version.Commit = ""
	version.Date = ""
}

func TestVersionText(t *testing.T) {
	pinDevVersion(t)
	h := newGoldenHarness(t)
	h.output = outputText
	out, _, code := h.run("version")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	got := strings.TrimSpace(out)
	if !strings.HasPrefix(got, "atm ") {
		t.Fatalf("expected 'atm ' prefix: %q", got)
	}
	if !strings.Contains(got, "linux/") && !strings.Contains(got, "darwin/") {
		t.Fatalf("expected os/arch token: %q", got)
	}
	compareGolden(t, "version-text", out)
}

func TestVersionJSON(t *testing.T) {
	pinDevVersion(t)
	h := newGoldenHarness(t)
	out, _, code := h.run("version")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	for _, key := range []string{`"version"`, `"commit"`, `"date"`, `"os"`, `"arch"`} {
		if !strings.Contains(out, key) {
			t.Fatalf("expected key %s in JSON: %s", key, out)
		}
	}
	compareGolden(t, "version-json", out)
}
