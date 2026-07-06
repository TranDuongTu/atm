package manager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/developing"
)

func TestPluginAssetsExistForSupportedHosts(t *testing.T) {
	for _, host := range []string{"opencode", "claude", "codex"} {
		assets, ok := PluginAssets(host)
		if !ok {
			t.Fatalf("PluginAssets(%q) ok=false", host)
		}
		if len(assets) == 0 {
			t.Fatalf("PluginAssets(%q) returned no files", host)
		}
	}
}

func TestPluginAssetsCheckATMRole(t *testing.T) {
	for _, host := range []string{"opencode", "claude", "codex"} {
		assets, _ := PluginAssets(host)
		joined := string(joinManagerAssetContents(assets))
		if !strings.Contains(joined, "ATM_ROLE") {
			t.Errorf("%s assets do not check ATM_ROLE", host)
		}
		if !strings.Contains(joined, "ATM_PROJECT") {
			t.Errorf("%s assets do not reference ATM_PROJECT", host)
		}
	}
}

func TestPluginAssetsContainManagerRole(t *testing.T) {
	assets, _ := PluginAssets("opencode")
	joined := string(joinManagerAssetContents(assets))
	for _, want := range []string{
		"ATM ledger owner",
		"needs clarification",
		"semantic search",
		"atm task set-title",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("OpenCode manager asset missing %q", want)
		}
	}
}

func TestPluginInstallRoot(t *testing.T) {
	home := "/home/user"
	cases := map[string]string{
		"opencode": filepath.Join(home, ".config", "opencode", "agents", "atm-manager.md"),
		"claude":   filepath.Join(home, ".claude", "agents", "atm-manager.md"),
		"codex":    filepath.Join(home, ".codex", "agents", "atm-manager.md"),
	}
	for host, want := range cases {
		got, ok := PluginInstallRoot(host, home)
		if !ok {
			t.Fatalf("PluginInstallRoot(%q) ok=false", host)
		}
		if got != want {
			t.Errorf("PluginInstallRoot(%q) = %q, want %q", host, got, want)
		}
	}
	if _, ok := PluginInstallRoot("ollama", home); ok {
		t.Fatal("PluginInstallRoot(ollama) returned ok=true")
	}
}

func TestPluginStatusMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, host := range []string{"opencode", "claude", "codex"} {
		if got := PluginStatus(host, home); got.State != "missing" {
			t.Errorf("PluginStatus(%q) = %q, want missing", host, got.State)
		}
	}
}

func TestPluginStatusInstalledRequiresDevelopingPlugin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Install only the manager asset, not the developing plugin.
	if _, err := InstallPlugin("opencode", home, false); err != nil {
		t.Fatal(err)
	}
	got := PluginStatus("opencode", home)
	if got.State != "partial" {
		t.Errorf("PluginStatus(opencode) without developing plugin = %q, want partial", got.State)
	}
	// Now fake the developing plugin presence by installing it.
	if _, err := developing.InstallPlugin("opencode", home, false); err != nil {
		t.Fatal(err)
	}
	got = PluginStatus("opencode", home)
	if got.State != "installed" {
		t.Errorf("PluginStatus(opencode) with developing plugin = %q, want installed", got.State)
	}
}

func TestInstallPluginWritesSubagentDefinition(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	res, err := InstallPlugin("opencode", home, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) == 0 {
		t.Fatal("InstallPlugin wrote no files")
	}
	for _, f := range res.Files {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("installed file %s missing: %v", f, err)
		}
	}
}

func TestInstallPluginDryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	res, err := InstallPlugin("claude", home, true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun {
		t.Fatal("DryRun=false on dry run")
	}
	for _, f := range res.Files {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("dry run wrote file %s", f)
		}
	}
}

func joinManagerAssetContents(assets []Asset) []byte {
	var out []byte
	for _, a := range assets {
		out = append(out, a.Content...)
		out = append(out, '\n')
	}
	return out
}
