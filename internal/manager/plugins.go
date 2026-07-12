package manager

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"atm/internal/developing"
)

//go:embed plugin_assets/opencode/atm-manager.md
//go:embed plugin_assets/claude/atm-manager.md
//go:embed plugin_assets/codex/atm-manager.md
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
		assets = append(assets, Asset{
			Path:    filepath.ToSlash(rel),
			Mode:    0o644,
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

func PluginInstallRoot(agent, home string) (string, bool) {
	switch agent {
	case "opencode":
		return filepath.Join(home, ".config", "opencode", "agents", "atm-manager.md"), true
	case "claude":
		return filepath.Join(home, ".claude", "agents", "atm-manager.md"), true
	case "codex":
		return filepath.Join(home, ".codex", "agents", "atm-manager.md"), true
	default:
		return "", false
	}
}

func PluginStatus(agent, home string) Status {
	root, ok := PluginInstallRoot(agent, home)
	if !ok {
		return Status{Agent: agent, State: "unknown"}
	}
	if _, err := os.Stat(root); err != nil {
		return Status{Agent: agent, State: "missing", Path: root}
	}
	// The manager subagent is only useful if the developing bootstrap is also
	// installed, since the developing agent learns the dispatch contract from
	// the developing plugin. Without it, report partial.
	devStatus := developing.PluginStatus(agent, home)
	if devStatus.State != "installed" {
		return Status{Agent: agent, State: "partial", Path: root}
	}
	// Detect stale deployed content: if the installed file no longer matches
	// the embedded asset (e.g. a previous version was installed and never
	// reinstalled after a prompt fix), report stale so the user knows to
	// reinstall. This is how ATM-0047 (claude manager subagent inactive) went
	// undetected: the file was present but still carried the old ATM_ROLE gate.
	assets, ok := PluginAssets(agent)
	if !ok || len(assets) == 0 {
		return Status{Agent: agent, State: "installed", Path: root}
	}
	deployed, err := os.ReadFile(root)
	if err != nil {
		return Status{Agent: agent, State: "partial", Path: root}
	}
	if !bytes.Equal(deployed, assets[0].Content) {
		return Status{Agent: agent, State: "stale", Path: root}
	}
	return Status{Agent: agent, State: "installed", Path: root}
}

func InstallPlugin(agent, home string, dryRun bool) (InstallResult, error) {
	root, ok := PluginInstallRoot(agent, home)
	if !ok {
		return InstallResult{}, fmt.Errorf("unknown agent %q", agent)
	}
	assets, ok := PluginAssets(agent)
	if !ok {
		return InstallResult{}, fmt.Errorf("plugin assets for %q not found", agent)
	}
	res := InstallResult{Agent: agent, Path: root, DryRun: dryRun}
	if !dryRun {
		if err := developing.ValidateDirParents(filepath.Dir(root)); err != nil {
			return res, err
		}
	}
	for _, a := range assets {
		dst := root // single-file install: the subagent definition is the only asset.
		res.Files = append(res.Files, dst)
		if dryRun {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return res, err
		}
		if err := os.WriteFile(dst, a.Content, a.Mode); err != nil {
			return res, err
		}
	}
	return res, nil
}
