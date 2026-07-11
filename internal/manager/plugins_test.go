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
		"manage-context",
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
		if !strings.Contains(body, "manage-context") {
			t.Errorf("%s subagent asset does not defer to `atm manage-context`", host)
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

func TestManagerAssetBodiesIdenticalAcrossHosts(t *testing.T) {
	// The frontmatter is necessarily host-specific (claude/codex use `tools:`,
	// opencode uses native `mode:`/`permission:`), but the body — everything
	// after the closing `---` — is the single source of truth shared across
	// all three hosts. Guard against body drift while allowing frontmatter to
	// differ per host's native conventions.
	var bodies = map[string]string{}
	for _, host := range []string{"opencode", "claude", "codex"} {
		assets, _ := PluginAssets(host)
		body := string(joinManagerAssetContents(assets))
		bodies[host] = stripFrontmatter(t, body)
	}
	if bodies["claude"] != bodies["codex"] {
		t.Errorf("claude and codex atm-manager.md bodies differ")
	}
	if bodies["claude"] != bodies["opencode"] {
		t.Errorf("claude and opencode atm-manager.md bodies differ:\nclaude:\n%s\nopencode:\n%s", bodies["claude"], bodies["opencode"])
	}
}

func TestOpencodeManagerAssetFrontmatterFormat(t *testing.T) {
	// ATM-0071 review. opencode's subagent sandbox is expressed natively via
	// `mode: subagent` + `permission:` (not claude's `tools:` string), and this
	// asset must deny edit/write so the manager cannot silently mutate files
	// outside the ledger. Guard the sandbox so a future homogenization pass
	// can't drop it again.
	assets, _ := PluginAssets("opencode")
	body := string(joinManagerAssetContents(assets))
	for _, want := range []string{"mode: subagent", "edit: deny", "write: deny"} {
		if !strings.Contains(body, want) {
			t.Errorf("opencode asset missing %q", want)
		}
	}
	if strings.Contains(body, "\nname:") {
		t.Errorf("opencode asset should not carry a `name:` key; opencode derives the name from the filename")
	}
	if strings.Contains(body, "tools:") {
		t.Errorf("opencode asset should not carry claude's `tools:` frontmatter key")
	}
}

// stripFrontmatter removes the leading `---\n...\n---\n` frontmatter block
// (if any) and returns everything after it, so cross-host body comparisons
// ignore each host's native frontmatter dialect.
func stripFrontmatter(t *testing.T, s string) string {
	t.Helper()
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	rest := s[len("---\n"):]
	idx := strings.Index(rest, "\n---\n")
	if idx == -1 {
		t.Fatalf("frontmatter block has no closing ---")
	}
	return rest[idx+len("\n---\n"):]
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
