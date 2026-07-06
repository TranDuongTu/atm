package developing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "atm-developing", "skills", "atm-developing", "SKILL.md")); err != nil {
		t.Fatalf("bundled Claude skill missing: %v", err)
	}
}

func TestInstallOpenCodePluginWritesPluginAndSkill(t *testing.T) {
	home := t.TempDir()
	res, err := InstallPlugin("opencode", home, false)
	if err != nil {
		t.Fatalf("InstallPlugin opencode: %v", err)
	}

	pluginPath := filepath.Join(home, ".config", "opencode", "plugins", "atm-developing.js")
	skillPath := filepath.Join(home, ".agents", "skills", "atm-developing", "SKILL.md")
	for _, path := range []string{pluginPath, skillPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected installed file %s: %v", path, err)
		}
	}

	joined := strings.Join(res.Files, "\n")
	for _, want := range []string{pluginPath, skillPath} {
		if !strings.Contains(joined, want) {
			t.Fatalf("install result missing %s in files: %v", want, res.Files)
		}
	}
}

func TestClaudePluginStatusIsPartialWhenSkillMissing(t *testing.T) {
	home := t.TempDir()
	pluginRoot := filepath.Join(home, ".claude", "skills", "atm-developing")
	if err := os.MkdirAll(filepath.Join(pluginRoot, ".claude-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir plugin manifest dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, ".claude-plugin", "plugin.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}

	status := PluginStatus("claude", home)
	if status.State != "partial" {
		t.Fatalf("status = %q, want partial", status.State)
	}
}

func TestInstallCodexPluginRegistersMarketplaceAndEnablesPlugin(t *testing.T) {
	home := t.TempDir()
	fakeCodex := installFakeCodex(t, home)
	if _, err := InstallPlugin("codex", home, false); err != nil {
		t.Fatalf("InstallPlugin codex: %v", err)
	}

	pluginRoot := filepath.Join(home, ".codex", "plugins", "atm-developing")
	if _, err := os.Stat(filepath.Join(pluginRoot, ".codex-plugin", "plugin.json")); err != nil {
		t.Fatalf("codex plugin manifest missing: %v", err)
	}

	marketplacePath := filepath.Join(home, ".agents", "plugins", "marketplace.json")
	marketplaceBytes, err := os.ReadFile(marketplacePath)
	if err != nil {
		t.Fatalf("marketplace missing: %v", err)
	}
	var marketplace struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source struct {
				Source string `json:"source"`
				Path   string `json:"path"`
			} `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(marketplaceBytes, &marketplace); err != nil {
		t.Fatalf("marketplace invalid JSON: %v", err)
	}
	if marketplace.Name != "atm-local" {
		t.Fatalf("marketplace name = %q, want atm-local", marketplace.Name)
	}
	if len(marketplace.Plugins) != 1 {
		t.Fatalf("marketplace plugins = %d, want 1", len(marketplace.Plugins))
	}
	if marketplace.Plugins[0].Name != "atm-developing" {
		t.Fatalf("plugin name = %q, want atm-developing", marketplace.Plugins[0].Name)
	}
	wantSourcePath := "./.codex/plugins/atm-developing"
	if marketplace.Plugins[0].Source.Source != "local" || marketplace.Plugins[0].Source.Path != wantSourcePath {
		t.Fatalf("plugin source = %#v, want local path %s", marketplace.Plugins[0].Source, wantSourcePath)
	}

	configBytes, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("codex config missing: %v", err)
	}
	config := string(configBytes)
	for _, want := range []string{
		"[marketplaces.atm-local]",
		`source_type = "local"`,
		`source = "` + filepath.ToSlash(home) + `"`,
		`[plugins."atm-developing@atm-local"]`,
		"enabled = true",
	} {
		if !strings.Contains(filepath.ToSlash(config), want) {
			t.Fatalf("codex config missing %q:\n%s", want, config)
		}
	}

	status := PluginStatus("codex", home)
	if status.State != "installed" {
		t.Fatalf("status = %q, want installed", status.State)
	}

	argsBytes, err := os.ReadFile(filepath.Join(home, "codex-args.txt"))
	if err != nil {
		t.Fatalf("fake codex was not called: %v", err)
	}
	if got, want := strings.TrimSpace(string(argsBytes)), "plugin add atm-developing@atm-local --json"; got != want {
		t.Fatalf("codex args = %q, want %q", got, want)
	}
	if fakeCodex == "" {
		t.Fatal("fake codex path is empty")
	}
}

func TestCodexPluginStatusIsPartialWhenAssetsExistWithoutRegistration(t *testing.T) {
	home := t.TempDir()
	pluginRoot := filepath.Join(home, ".codex", "plugins", "atm-developing")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatalf("mkdir plugin root: %v", err)
	}

	status := PluginStatus("codex", home)
	if status.State != "partial" {
		t.Fatalf("status = %q, want partial", status.State)
	}
}

func installFakeCodex(t *testing.T, home string) string {
	t.Helper()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	bin := filepath.Join(binDir, "codex")
	script := `#!/bin/sh
printf '%s\n' "$*" > "$HOME/codex-args.txt"
mkdir -p "$HOME/.codex/plugins/cache/atm-local/atm-developing/0.1.0/.codex-plugin"
printf '{"name":"atm-developing"}\n' > "$HOME/.codex/plugins/cache/atm-local/atm-developing/0.1.0/.codex-plugin/plugin.json"
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bin
}
