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

func TestAppendAgentArgs_Order(t *testing.T) {
	base := []string{"codex"}
	env := []string{"--yolo"}
	extra := []string{"--auto"}
	got := appendAgentArgs(base, env, extra)
	want := []string{"codex", "--yolo", "--auto"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendAgentArgs order = %v, want %v", got, want)
	}
}

func TestAppendAgentArgs_NoDedup(t *testing.T) {
	base := []string{"codex"}
	env := []string{"--yolo"}
	extra := []string{"--yolo"}
	got := appendAgentArgs(base, env, extra)
	want := []string{"codex", "--yolo", "--yolo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendAgentArgs should not dedup = %v, want %v", got, want)
	}
}

func TestAppendAgentArgs_BothEmpty(t *testing.T) {
	base := []string{"codex"}
	got := appendAgentArgs(base, nil, nil)
	want := []string{"codex"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendAgentArgs both empty = %v, want %v", got, want)
	}
}

func TestAppendAgentArgs_DoesNotMutateBase(t *testing.T) {
	base := []string{"codex"}
	_ = appendAgentArgs(base, []string{"--x"}, []string{"--y"})
	if base[0] != "codex" || len(base) != 1 {
		t.Errorf("appendAgentArgs mutated base: %v", base)
	}
}

func TestContextCachePathPersona(t *testing.T) {
	got := contextCachePath("/STORE", "FOO", "developer", "", "", "")
	want := "/STORE/projects/FOO/cache/session-developer.md"
	if got != want {
		t.Fatalf("contextCachePath developer = %q, want %q", got, want)
	}
}

func TestContextCachePathManagerMode(t *testing.T) {
	got := contextCachePath("/STORE", "FOO", "manager", "autopilot", "", "")
	want := "/STORE/projects/FOO/cache/session-manager-autopilot.md"
	if got != want {
		t.Fatalf("contextCachePath manager-autopilot = %q, want %q", got, want)
	}
}

func TestContextCachePathManagerScopedCapability(t *testing.T) {
	got := contextCachePath("/STORE", "FOO", "manager", "brief", "contextmap", "")
	want := "/STORE/projects/FOO/cache/session-manager-brief-contextmap.md"
	if got != want {
		t.Fatalf("contextCachePath manager-scoped = %q, want %q", got, want)
	}
}

func TestContextCachePathNoProjectUsesStoreCache(t *testing.T) {
	got := contextCachePath("/STORE", "", "concierge", "", "", "")
	want := "/STORE/cache/session-concierge.md"
	if got != want {
		t.Fatalf("contextCachePath no-project = %q, want %q", got, want)
	}
}

func TestContextCachePathNormalizes(t *testing.T) {
	got := contextCachePath("/STORE", "FOO", "Dev-Staff", "", "", "")
	want := "/STORE/projects/FOO/cache/session-dev-staff.md"
	if got != want {
		t.Fatalf("contextCachePath normalize = %q, want %q", got, want)
	}
}

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

// TestCacheKeyWithTask verifies the task id joins the cache key so two
// concurrent sessions on different tasks never share a context file.
func TestCacheKeyWithTask(t *testing.T) {
	if got, want := cacheKey("developer", "", "", "ATM-4b7e24"), "session-developer-atm-4b7e24"; got != want {
		t.Fatalf("cacheKey = %q, want %q", got, want)
	}
	if got, want := cacheKey("developer", "", "", ""), "session-developer"; got != want {
		t.Fatalf("cacheKey no-task = %q, want %q", got, want)
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
