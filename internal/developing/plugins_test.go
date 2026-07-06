package developing

import (
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

func joinAssetContents(assets []Asset) []byte {
	var out []byte
	for _, a := range assets {
		out = append(out, a.Content...)
		out = append(out, '\n')
	}
	return out
}
