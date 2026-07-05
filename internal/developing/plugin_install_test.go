package developing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginInstallRoot(t *testing.T) {
	home := "/home/tester"
	tests := map[string]string{
		"opencode": filepath.Join(home, ".config", "opencode", "plugins", "atm-developing.js"),
		"claude":   filepath.Join(home, ".claude", "skills", "atm-developing"),
		"codex":    filepath.Join(home, ".codex", "plugins", "atm-developing"),
	}
	for agent, want := range tests {
		got, ok := PluginInstallRoot(agent, home)
		if !ok {
			t.Fatalf("PluginInstallRoot(%q) ok=false", agent)
		}
		if got != want {
			t.Errorf("PluginInstallRoot(%q) = %q, want %q", agent, got, want)
		}
	}
}

func TestInstallPluginDryRunDoesNotWrite(t *testing.T) {
	home := t.TempDir()
	res, err := InstallPlugin("claude", home, true)
	if err != nil {
		t.Fatalf("InstallPlugin dry-run: %v", err)
	}
	if len(res.Files) == 0 {
		t.Fatal("dry-run result has no files")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "atm-developing")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote plugin dir: %v", err)
	}
}

func TestInstallPluginWritesAssetsAndStatusInstalled(t *testing.T) {
	home := t.TempDir()
	if _, err := InstallPlugin("claude", home, false); err != nil {
		t.Fatalf("InstallPlugin: %v", err)
	}
	status := PluginStatus("claude", home)
	if status.State != "installed" {
		t.Fatalf("status = %q, want installed", status.State)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "atm-developing", ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("plugin manifest missing: %v", err)
	}
}
