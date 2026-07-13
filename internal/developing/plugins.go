package developing

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Dot-prefixed plugin manifest directories are embedded explicitly so Go does
// not skip them while expanding a broad directory pattern.
//
//go:embed plugin_assets/opencode/atm-developing.js
//go:embed plugin_assets/opencode/skills/atm-developing/SKILL.md
//go:embed plugin_assets/claude/.claude-plugin/plugin.json
//go:embed plugin_assets/claude/hooks/hooks.json
//go:embed plugin_assets/claude/hooks/session-start
//go:embed plugin_assets/claude/skills/atm-developing/SKILL.md
//go:embed plugin_assets/codex/.codex-plugin/plugin.json
//go:embed plugin_assets/codex/hooks/hooks.json
//go:embed plugin_assets/codex/hooks/session-start
//go:embed plugin_assets/codex/skills/atm-developing/SKILL.md
var pluginFS embed.FS

type Asset struct {
	Path    string
	Mode    fs.FileMode
	Content []byte
}

func PluginAssets(agent string) ([]Asset, bool) {
	root := filepath.ToSlash(filepath.Join("plugin_assets", agent))
	if _, err := fs.Stat(pluginFS, root); err != nil {
		return nil, false
	}
	var assets []Asset
	err := fs.WalkDir(pluginFS, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := pluginFS.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if filepath.Base(path) == "session-start" {
			mode = 0o755
		}
		assets = append(assets, Asset{
			Path:    filepath.ToSlash(rel),
			Mode:    mode,
			Content: b,
		})
		return nil
	})
	if err != nil {
		return nil, false
	}
	return assets, true
}

type Status struct {
	Agent string `json:"agent"`
	State string `json:"state"`
	Path  string `json:"path"`
}

type InstallResult struct {
	Agent  string   `json:"agent"`
	Path   string   `json:"path"`
	Files  []string `json:"files"`
	DryRun bool     `json:"dry_run"`
}

const (
	codexMarketplaceName = "atm-local"
	codexPluginName      = "atm-developing"
)

func PluginInstallRoot(agent string, home string) (string, bool) {
	switch agent {
	case "opencode":
		return filepath.Join(home, ".config", "opencode", "plugins", "atm-developing.js"), true
	case "claude":
		return filepath.Join(home, ".claude", "skills", "atm-developing"), true
	case "codex":
		return filepath.Join(home, ".codex", "plugins", "atm-developing"), true
	default:
		return "", false
	}
}

func PluginStatus(agent string, home string) Status {
	root, ok := PluginInstallRoot(agent, home)
	if !ok {
		return Status{Agent: agent, State: "unknown"}
	}
	if _, err := os.Stat(root); err == nil {
		if agent == "claude" && !claudePluginComplete(home) {
			return Status{Agent: agent, State: "partial", Path: root}
		}
		if agent == "codex" && (!codexPluginEnabled(home) || !codexPluginCached(home)) {
			return Status{Agent: agent, State: "partial", Path: root}
		}
		return Status{Agent: agent, State: "installed", Path: root}
	}
	return Status{Agent: agent, State: "missing", Path: root}
}

type assetDest struct {
	asset Asset
	dest  string
}

func InstallPlugin(agent string, home string, dryRun bool) (InstallResult, error) {
	root, ok := PluginInstallRoot(agent, home)
	if !ok {
		return InstallResult{}, fmt.Errorf("unknown agent %q", agent)
	}
	assets, ok := PluginAssets(agent)
	if !ok {
		return InstallResult{}, fmt.Errorf("plugin assets for %q not found", agent)
	}
	res := InstallResult{Agent: agent, Path: root, DryRun: dryRun}
	dests := make([]assetDest, 0, len(assets))
	for _, a := range assets {
		dests = append(dests, assetDest{asset: a, dest: pluginAssetDestination(agent, home, root, a)})
	}
	if !dryRun {
		seen := map[string]bool{}
		for _, d := range dests {
			dir := filepath.Dir(d.dest)
			if seen[dir] {
				continue
			}
			seen[dir] = true
			if err := ValidateDirParents(dir); err != nil {
				return res, err
			}
		}
	}
	for _, d := range dests {
		res.Files = append(res.Files, d.dest)
		if dryRun {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(d.dest), 0o755); err != nil {
			return res, err
		}
		if err := os.WriteFile(d.dest, d.asset.Content, d.asset.Mode); err != nil {
			return res, err
		}
	}
	if agent == "codex" {
		extra, err := installCodexRegistration(home, root, dryRun)
		res.Files = append(res.Files, extra...)
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

func ValidateDirParents(path string) error {
	for {
		fi, err := os.Lstat(path)
		if err != nil {
			parent := filepath.Dir(path)
			if parent == path {
				return nil
			}
			path = parent
			continue
		}
		if fi.IsDir() {
			return nil
		}
		desc := "a regular file"
		if fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				desc = "a symlink"
			} else {
				desc = fmt.Sprintf("a symlink to %q", target)
			}
		}
		return fmt.Errorf("%q exists but is not a directory (it is %s); please remove it and try again", path, desc)
	}
}

func pluginAssetDestination(agent, home, root string, asset Asset) string {
	if agent == "opencode" {
		if asset.Path == "atm-developing.js" {
			return root
		}
		if strings.HasPrefix(asset.Path, "skills/") {
			return filepath.Join(home, ".agents", filepath.FromSlash(asset.Path))
		}
	}
	return filepath.Join(root, filepath.FromSlash(asset.Path))
}

type codexMarketplace struct {
	Name    string                `json:"name"`
	Plugins []codexMarketplaceRef `json:"plugins"`
}

type codexMarketplaceRef struct {
	Name     string            `json:"name"`
	Source   codexPluginSource `json:"source"`
	Policy   codexPluginPolicy `json:"policy"`
	Category string            `json:"category"`
}

type codexPluginSource struct {
	Source string `json:"source"`
	Path   string `json:"path"`
}

type codexPluginPolicy struct {
	Installation   string `json:"installation"`
	Authentication string `json:"authentication"`
}

func installCodexRegistration(home, pluginRoot string, dryRun bool) ([]string, error) {
	marketplacePath := filepath.Join(home, ".agents", "plugins", "marketplace.json")
	configPath := filepath.Join(home, ".codex", "config.toml")
	files := []string{marketplacePath, configPath}
	if dryRun {
		return files, nil
	}
	marketplaceName, err := writeCodexMarketplace(marketplacePath, home, pluginRoot)
	if err != nil {
		return files, err
	}
	if err := writeCodexConfig(configPath, home, marketplaceName); err != nil {
		return files, err
	}
	if err := runCodexPluginAdd(home, marketplaceName); err != nil {
		return files, err
	}
	return files, nil
}

func writeCodexMarketplace(path, marketplaceRoot, pluginRoot string) (string, error) {
	marketplace := codexMarketplace{Name: codexMarketplaceName}
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		if err := json.Unmarshal(b, &marketplace); err != nil {
			return "", fmt.Errorf("read codex marketplace %s: %w", path, err)
		}
		if marketplace.Name == "" {
			marketplace.Name = codexMarketplaceName
		}
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	entry := codexMarketplaceRef{
		Name: codexPluginName,
		Source: codexPluginSource{
			Source: "local",
			Path:   codexMarketplacePluginPath(marketplaceRoot, pluginRoot),
		},
		Policy: codexPluginPolicy{
			Installation:   "AVAILABLE",
			Authentication: "ON_INSTALL",
		},
		Category: "Developer Tools",
	}
	replaced := false
	for i := range marketplace.Plugins {
		if marketplace.Plugins[i].Name == codexPluginName {
			marketplace.Plugins[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		marketplace.Plugins = append(marketplace.Plugins, entry)
	}

	b, err := json.MarshalIndent(marketplace, "", "  ")
	if err != nil {
		return "", err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return marketplace.Name, os.WriteFile(path, b, 0o644)
}

func codexMarketplacePluginPath(marketplaceRoot, pluginRoot string) string {
	rel, err := filepath.Rel(marketplaceRoot, pluginRoot)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return filepath.ToSlash(pluginRoot)
	}
	return "./" + filepath.ToSlash(rel)
}

func writeCodexConfig(path, home, marketplaceName string) error {
	var content string
	if b, err := os.ReadFile(path); err == nil {
		content = string(b)
	} else if !os.IsNotExist(err) {
		return err
	}

	content = upsertTomlSection(content, "[marketplaces."+marketplaceName+"]", []string{
		`source_type = "local"`,
		`source = "` + tomlString(home) + `"`,
	})
	content = upsertTomlSection(content, `[plugins."`+codexPluginName+"@"+marketplaceName+`"]`, []string{
		"enabled = true",
	})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func codexPluginEnabled(home string) bool {
	b, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		return false
	}
	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		header := strings.TrimSpace(line)
		if !strings.HasPrefix(header, `[plugins."`+codexPluginName+"@") || !strings.HasSuffix(header, `"]`) {
			continue
		}
		for _, sectionLine := range tomlSectionLines(string(b), header) {
			if strings.TrimSpace(sectionLine) == "enabled = true" {
				return true
			}
		}
	}
	return false
}

func claudePluginComplete(home string) bool {
	root, ok := PluginInstallRoot("claude", home)
	if !ok {
		return false
	}
	for _, path := range []string{
		filepath.Join(root, ".claude-plugin", "plugin.json"),
		filepath.Join(root, "hooks", "hooks.json"),
		filepath.Join(root, "hooks", "session-start"),
		filepath.Join(root, "skills", "atm-developing", "SKILL.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

func codexPluginCached(home string) bool {
	matches, err := filepath.Glob(filepath.Join(home, ".codex", "plugins", "cache", "*", codexPluginName, "*", ".codex-plugin", "plugin.json"))
	return err == nil && len(matches) > 0
}

func runCodexPluginAdd(home, marketplaceName string) error {
	bin, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("codex not found on PATH; install Codex before installing the Codex ATM plugin: %w", err)
	}
	cmd := exec.Command(bin, "plugin", "add", codexPluginName+"@"+marketplaceName, "--json")
	cmd.Env = append(os.Environ(), "HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("codex plugin add %s@%s failed: %w: %s", codexPluginName, marketplaceName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func upsertTomlSection(content, header string, lines []string) string {
	replacement := append([]string{header}, lines...)
	replacementText := strings.Join(replacement, "\n")
	if strings.TrimSpace(content) == "" {
		return replacementText + "\n"
	}

	existing := strings.Split(content, "\n")
	for i, line := range existing {
		if strings.TrimSpace(line) != header {
			continue
		}
		end := i + 1
		for end < len(existing) && !strings.HasPrefix(strings.TrimSpace(existing[end]), "[") {
			end++
		}
		out := append([]string{}, existing[:i]...)
		out = append(out, replacement...)
		out = append(out, existing[end:]...)
		return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
	}
	return strings.TrimRight(content, "\n") + "\n\n" + replacementText + "\n"
}

func tomlSectionLines(content, header string) []string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != header {
			continue
		}
		end := i + 1
		for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), "[") {
			end++
		}
		return lines[i+1 : end]
	}
	return nil
}

func tomlString(s string) string {
	s = filepath.ToSlash(s)
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
