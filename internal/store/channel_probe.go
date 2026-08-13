// internal/store/channel_probe.go
package store

import (
	"os"
	"os/exec"
	"strconv"
	"strings"

	"atm/internal/core"
)

// probeRepoPath runs the cheap, strictly-local repo checks: existence, git
// repo, dirty tree, and ahead/behind against the ALREADY-FETCHED upstream
// tracking ref. It never fetches, never touches the network, and degrades a
// failed check to its zero value — a probe is a status light, not a gate.
func probeRepoPath(path string) *core.ChannelProbe {
	p := &core.ChannelProbe{}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return p
	}
	p.PathExists = true
	git := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", path}, args...)...).Output()
		return strings.TrimSpace(string(out)), err
	}
	if out, err := git("rev-parse", "--is-inside-work-tree"); err != nil || out != "true" {
		return p
	}
	p.IsGitRepo = true
	if out, err := git("status", "--porcelain"); err == nil && out != "" {
		p.Dirty = true
	}
	if out, err := git("rev-list", "--left-right", "--count", "@{upstream}...HEAD"); err == nil {
		if parts := strings.Fields(out); len(parts) == 2 {
			p.HasUpstream = true
			p.Behind, _ = strconv.Atoi(parts[0])
			p.Ahead, _ = strconv.Atoi(parts[1])
		}
	}
	return p
}
