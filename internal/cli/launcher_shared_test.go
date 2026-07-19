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

func TestContextCachePathDev(t *testing.T) {
	got := contextCachePath("/STORE", "FOO", "dev", "developer", "", "")
	want := "/STORE/projects/FOO/cache/dev-developer.md"
	if got != want {
		t.Fatalf("contextCachePath dev = %q, want %q", got, want)
	}
}

func TestContextCachePathManageAllCapabilities(t *testing.T) {
	got := contextCachePath("/STORE", "FOO", "manage", "manager", "autopilot", "")
	want := "/STORE/projects/FOO/cache/manage-manager-autopilot-all.md"
	if got != want {
		t.Fatalf("contextCachePath manage-all = %q, want %q", got, want)
	}
}

func TestContextCachePathManageScopedCapability(t *testing.T) {
	got := contextCachePath("/STORE", "FOO", "manage", "manager", "brief", "boards")
	want := "/STORE/projects/FOO/cache/manage-manager-brief-boards.md"
	if got != want {
		t.Fatalf("contextCachePath manage-scoped = %q, want %q", got, want)
	}
}

func TestContextCachePathNormalizes(t *testing.T) {
	got := contextCachePath("/STORE", "FOO", "dev", "Dev-Staff", "", "")
	want := "/STORE/projects/FOO/cache/dev-dev-staff.md"
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
