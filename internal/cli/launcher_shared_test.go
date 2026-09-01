package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAgentEnvArgs_DirectHosts(t *testing.T) {
	t.Setenv("ATM_OPENCODE_ARGS", "--auto --foo bar")
	got := agentEnvArgs("opencode", "")
	want := []string{"--auto", "--foo", "bar"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("agentEnvArgs(opencode) = %v, want %v", got, want)
	}
}

func TestAgentEnvArgs_EmptyEnv(t *testing.T) {
	t.Setenv("ATM_CODEX_ARGS", "")
	if got := agentEnvArgs("codex", ""); got != nil {
		t.Errorf("agentEnvArgs(codex) with empty env = %v, want nil", got)
	}
}

func TestAgentEnvArgs_OllamaIntegrationPrecedence(t *testing.T) {
	t.Setenv("ATM_CODEX_ARGS", "--yolo")
	t.Setenv("ATM_OLLAMA_ARGS", "--generic")
	got := agentEnvArgs("ollama", "codex")
	want := []string{"--yolo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ollama integration precedence = %v, want %v", got, want)
	}
}

func TestAgentEnvArgs_OllamaNoIntegration(t *testing.T) {
	t.Setenv("ATM_OLLAMA_ARGS", "--generic")
	got := agentEnvArgs("ollama", "")
	want := []string{"--generic"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ollama no integration = %v, want %v", got, want)
	}
}

// The former appendAgentArgs/contextCachePath/cacheKey unit pins moved to
// internal/compose with the code (compose_test.go).

func TestWriteContextIfDiffCreates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "dev-developer.md")
	content := []byte("# prompt\n")
	if err := writeContextIfDiff(path, content); err != nil {
		t.Fatalf("writeContextIfDiff: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("file content mismatch")
	}
}

func TestWriteContextIfDiffNoOpOnMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "dev-developer.md")
	content := []byte("# prompt\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	prevMtime := info.ModTime()

	// Sleep so a new write would change mtime if it happened.
	time.Sleep(10 * time.Millisecond)

	if err := writeContextIfDiff(path, content); err != nil {
		t.Fatalf("writeContextIfDiff: %v", err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.ModTime().Equal(prevMtime) {
		t.Fatalf("writeContextIfDiff should be a no-op when content matches; mtime changed")
	}
}

func TestWriteContextIfDiffOverwritesOnDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "dev-developer.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("# old\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := writeContextIfDiff(path, []byte("# new\n")); err != nil {
		t.Fatalf("writeContextIfDiff: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "# new\n" {
		t.Fatalf("file not overwritten; got %q", got)
	}
}
