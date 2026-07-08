package developing

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginAssetsExistForSupportedAgents(t *testing.T) {
	for _, agent := range []string{"opencode", "claude", "codex"} {
		assets, ok := PluginAssets(agent)
		if !ok {
			t.Fatalf("PluginAssets(%q) ok=false", agent)
		}
		if len(assets) == 0 {
			t.Fatalf("PluginAssets(%q) returned no files", agent)
		}
	}
}

func TestPluginAssetsStaySilentWithoutATMRole(t *testing.T) {
	for _, agent := range []string{"opencode", "claude", "codex"} {
		assets, _ := PluginAssets(agent)
		joined := string(joinAssetContents(assets))
		if !strings.Contains(joined, "ATM_ROLE") {
			t.Errorf("%s assets do not check ATM_ROLE", agent)
		}
		if !strings.Contains(joined, "ATM_PROJECT") {
			t.Errorf("%s assets do not check ATM_PROJECT", agent)
		}
	}
}

func TestPluginAssetsContainLedgerLanguage(t *testing.T) {
	assets, _ := PluginAssets("claude")
	joined := string(joinAssetContents(assets))
	for _, want := range []string{"visible work ledger", "task comments", "ATM_CONTEXT_FILE", "Use the atm-developing skill"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Claude assets missing %q", want)
		}
	}
	if strings.Contains(joined, "brainstorming/planning skills") {
		t.Error("Claude assets should not depend on Superpowers-specific skill names")
	}
}

func TestOpenCodePluginAssetContainsLedgerBeforeSkillsAndLogging(t *testing.T) {
	assets, _ := PluginAssets("opencode")
	joined := string(joinAssetContents(assets))
	for _, want := range []string{"Use the atm-developing skill", "config.skills.paths", "bootstrap injected"} {
		if !strings.Contains(joined, want) {
			t.Errorf("OpenCode asset missing %q", want)
		}
	}
	if strings.Contains(joined, "brainstorming/planning skills") {
		t.Error("OpenCode asset should not depend on Superpowers-specific skill names")
	}
}

func TestPluginAssetsContainManagerDispatchContract(t *testing.T) {
	for _, agent := range []string{"opencode", "claude", "codex"} {
		assets, _ := PluginAssets(agent)
		joined := string(joinAssetContents(assets))
		if !strings.Contains(joined, "atm-manager") {
			t.Errorf("%s assets do not mention the atm-manager subagent", agent)
		}
		if !strings.Contains(joined, "dispatch") {
			t.Errorf("%s assets do not mention dispatching the manager", agent)
		}
	}
}

func TestPluginAssetsForbidSelfImprovementGeneTasks(t *testing.T) {
	for _, agent := range []string{"opencode", "claude", "codex"} {
		assets, _ := PluginAssets(agent)
		joined := string(joinAssetContents(assets))
		if !strings.Contains(joined, "self-improvement gene") {
			t.Errorf("%s developing assets do not mention the self-improvement gene boundary", agent)
		}
		if !strings.Contains(joined, "Manager:") {
			t.Errorf("%s developing assets do not forbid Manager: gene tasks by name", agent)
		}
	}
}

func TestCodexPluginManifestSuppressesAutoDiscoveredHooks(t *testing.T) {
	assets, _ := PluginAssets("codex")
	var manifest struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	for _, asset := range assets {
		if asset.Path != ".codex-plugin/plugin.json" {
			continue
		}
		if err := json.Unmarshal(asset.Content, &manifest); err != nil {
			t.Fatalf("parse codex plugin manifest: %v", err)
		}
		if manifest.Hooks == nil {
			t.Fatal("codex plugin manifest must declare hooks: {} to suppress hooks/hooks.json auto-discovery")
		}
		if len(manifest.Hooks) != 0 {
			t.Fatalf("codex plugin manifest hooks = %#v, want empty object", manifest.Hooks)
		}
		return
	}
	t.Fatal("codex plugin manifest not found")
}

func TestCodexHookCommandRunsWithClaudePluginRootFallback(t *testing.T) {
	assets, _ := PluginAssets("codex")
	command := codexHookCommand(t, assets)

	pluginRoot := t.TempDir()
	hookDir := filepath.Join(pluginRoot, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("mkdir hook dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "session-start"), []byte("#!/bin/sh\nprintf 'hook ran\\n'\n"), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(),
		"CODEX_PLUGIN_ROOT=",
		"CLAUDE_PLUGIN_ROOT="+pluginRoot,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("codex hook command failed with CLAUDE_PLUGIN_ROOT fallback: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if got := strings.TrimSpace(string(out)); got != "hook ran" {
		t.Fatalf("hook output = %q, want hook ran", got)
	}
}

func codexHookCommand(t *testing.T, assets []Asset) string {
	t.Helper()
	for _, asset := range assets {
		if asset.Path != "hooks/hooks.json" {
			continue
		}
		var config struct {
			Hooks map[string][]struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if err := json.Unmarshal(asset.Content, &config); err != nil {
			t.Fatalf("parse codex hooks.json: %v", err)
		}
		for _, group := range config.Hooks["SessionStart"] {
			for _, hook := range group.Hooks {
				if hook.Command != "" {
					return hook.Command
				}
			}
		}
	}
	t.Fatal("codex SessionStart hook command not found")
	return ""
}

func joinAssetContents(assets []Asset) []byte {
	var out []byte
	for _, a := range assets {
		out = append(out, a.Content...)
		out = append(out, '\n')
	}
	return out
}
