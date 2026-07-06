package version

import (
	"os/exec"
	"strings"
	"testing"
)

func TestFormatTextFull(t *testing.T) {
	got := FormatText(map[string]string{
		"version": "v0.1.0",
		"commit":  "abc1234",
		"date":    "2026-07-06T13:45:03Z",
		"os":      "linux",
		"arch":    "amd64",
	})
	want := "atm v0.1.0 (commit: abc1234, built: 2026-07-06T13:45:03Z, linux/amd64)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFormatTextEmptyCommitDate(t *testing.T) {
	got := FormatText(map[string]string{
		"version": "dev",
		"commit":  "",
		"date":    "",
		"os":      "linux",
		"arch":    "amd64",
	})
	want := "atm dev (linux/amd64)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFormatTextCommitOnly(t *testing.T) {
	got := FormatText(map[string]string{
		"version": "v0.1.0",
		"commit":  "abc1234",
		"date":    "",
		"os":      "darwin",
		"arch":    "arm64",
	})
	want := "atm v0.1.0 (commit: abc1234, darwin/arm64)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEmitJSONKeyOrder(t *testing.T) {
	got := EmitJSON(map[string]any{
		"version": "v0.1.0",
		"commit":  "abc1234",
		"date":    "2026-07-06T13:45:03Z",
		"os":      "linux",
		"arch":    "amd64",
	})
	want := `{
  "arch": "amd64",
  "commit": "abc1234",
  "date": "2026-07-06T13:45:03Z",
  "os": "linux",
  "version": "v0.1.0"
}`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEmitJSONDeterministicContent(t *testing.T) {
	info := map[string]any{
		"version": "v0.1.0",
		"commit":  "abc1234",
		"date":    "2026-07-06T13:45:03Z",
		"os":      "linux",
		"arch":    "amd64",
	}
	a := EmitJSON(info)
	b := EmitJSON(info)
	if a != b {
		t.Fatalf("non-deterministic:\n%s\n%s", a, b)
	}
}

func TestLdflagsOverride(t *testing.T) {
	t.Skip("requires Task 2 wiring of newVersionCmd (internal/cli/root.go:107) to consume internal/version; un-skip in Task 2")
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	tmp := t.TempDir() + "/atm_version_probe"
	ldflags := "-X 'atm/internal/version.Version=test-v' " +
		"-X 'atm/internal/version.Commit=deadbeef' " +
		"-X 'atm/internal/version.Date=2026-01-02T03:04:05Z'"
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", tmp,
		"./cmd/atm")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}
	defer func() { _ = exec.Command("rm", "-f", tmp).Run() }()

	out, err := exec.Command(tmp, "version").Output()
	if err != nil {
		t.Fatalf("run probe: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if !strings.Contains(got, "test-v") {
		t.Fatalf("ldflags override not baked in: %q", got)
	}
	if !strings.Contains(got, "deadbeef") {
		t.Fatalf("commit not baked in: %q", got)
	}
}
