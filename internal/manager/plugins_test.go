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

func TestPluginAssetsCheckATMProject(t *testing.T) {
	for _, host := range []string{"opencode", "claude", "codex"} {
		assets, _ := PluginAssets(host)
		joined := string(joinManagerAssetContents(assets))
		if !strings.Contains(joined, "ATM_PROJECT") {
			t.Errorf("%s assets do not reference ATM_PROJECT", host)
		}
		// The gate must be ATM_PROJECT-presence, NOT ATM_ROLE=manager.
		// Subagent dispatch inherits ATM_ROLE=developing from the parent
		// session; an ATM_ROLE gate would make the subagent always refuse.
		if strings.Contains(joined, "ATM_ROLE` is not `manager") {
			t.Errorf("%s assets gate on ATM_ROLE=manager; subagent dispatch cannot satisfy that", host)
		}
	}
}

func TestPluginAssetsContainManagerRole(t *testing.T) {
	assets, _ := PluginAssets("opencode")
	joined := string(joinManagerAssetContents(assets))
	for _, want := range []string{
		"ATM ledger owner",
		"render-context",
		"follow it",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("OpenCode manager asset missing %q", want)
		}
	}
}

func TestManagerSubagentAssetResolvesRuntimeValuesFromEnv(t *testing.T) {
	// ATM-0047 regression guard. The subagent asset is copied verbatim to
	// ~/.claude/agents/atm-manager.md with NO per-dispatch render step, so any
	// <ATM_BIN>/<CODE>/<ACTOR> template placeholder survives literally in the
	// prompt. The model then substitutes a guessed default (e.g. a nonexistent
	// ~/.local/bin/atm), every atm call fails, and — with no failure rule — the
	// manager fabricates success. The subagent asset must resolve every runtime
	// value from env with real shell variables and never carry a placeholder.
	for _, host := range []string{"opencode", "claude", "codex"} {
		assets, _ := PluginAssets(host)
		body := string(joinManagerAssetContents(assets))
		for _, placeholder := range []string{"<ATM_BIN>", "<CODE>", "<ACTOR>", "<PROJECT_NAME>", "<RUN_ID>", "<TIMESTAMP>"} {
			if strings.Contains(body, placeholder) {
				t.Errorf("%s subagent asset carries unrendered placeholder %s; subagent mode never renders it, so the model will guess a value", host, placeholder)
			}
		}
		// Deterministic binary resolution from ATM_BIN, falling back to PATH.
		if !strings.Contains(body, "${ATM_BIN:-atm}") {
			t.Errorf("%s subagent asset does not resolve the binary via ${ATM_BIN:-atm}", host)
		}
		if strings.Contains(body, ".local/bin/atm") {
			t.Errorf("%s subagent asset names a hardcoded binary path", host)
		}
		if !strings.Contains(body, "render-context") {
			t.Errorf("%s subagent asset does not defer to `atm manager render-context`", host)
		}
	}
}

func TestClaudeManagerAssetToolsFrontmatterFormat(t *testing.T) {
	// ATM-0047 root cause. Claude Code requires the subagent `tools:` frontmatter
	// as a comma-separated string of capitalized canonical names
	// (e.g. `tools: Bash, Read, Glob, Grep`). The old asset used a lowercase YAML
	// block sequence (- bash / - read / ...), which Claude Code does not
	// recognize, so the subagent spawned without a working Bash tool: it ran zero
	// commands and fabricated ledger writes. Guard the correct format.
	assets, _ := PluginAssets("claude")
	body := string(joinManagerAssetContents(assets))
	if !strings.Contains(body, "tools: Bash, Read, Glob, Grep") {
		t.Errorf("claude asset missing the comma-separated capitalized tools frontmatter Claude Code requires")
	}
	if strings.Contains(body, "\n  - bash") || strings.Contains(body, "\n  - read") {
		t.Errorf("claude asset uses an unrecognized YAML-list tools frontmatter; Claude Code spawns it without tools")
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

func TestPluginStatusDetectsStaleDeployedFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Install both plugins so the manager status would otherwise be "installed".
	if _, err := developing.InstallPlugin("claude", home, false); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallPlugin("claude", home, false); err != nil {
		t.Fatal(err)
	}
	if got := PluginStatus("claude", home); got.State != "installed" {
		t.Fatalf("PluginStatus(claude) after fresh install = %q, want installed", got.State)
	}
	// Corrupt the deployed manager file so it no longer matches the embedded asset.
	root, _ := PluginInstallRoot("claude", home)
	if err := os.WriteFile(root, []byte("stale content from a previous version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := PluginStatus("claude", home); got.State != "stale" {
		t.Errorf("PluginStatus(claude) with stale content = %q, want stale", got.State)
	}
	// Reinstalling must clear the stale state.
	if _, err := InstallPlugin("claude", home, false); err != nil {
		t.Fatal(err)
	}
	if got := PluginStatus("claude", home); got.State != "installed" {
		t.Errorf("PluginStatus(claude) after reinstall = %q, want installed", got.State)
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
