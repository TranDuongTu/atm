package developing

import (
	"embed"
	"io/fs"
	"path/filepath"
)

// Dot-prefixed plugin manifest directories are embedded explicitly so Go does
// not skip them while expanding a broad directory pattern.
//
//go:embed plugin_assets/opencode/atm-developing.js
//go:embed plugin_assets/claude/.claude-plugin/plugin.json
//go:embed plugin_assets/claude/hooks/hooks.json
//go:embed plugin_assets/claude/hooks/session-start
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
