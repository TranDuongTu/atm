package setup

import (
	"context"
	"regexp"
	"strings"
)

var versionRe = regexp.MustCompile(`\d+\.\d+(?:\.\d+)*`)

// ProbeVersion returns the installed version, or "" when it cannot be read.
// It deliberately says nothing about whether a NEWER version exists: nothing
// available here can know that without running the update.
func ProbeVersion(ctx context.Context, binary string, run RunFunc) string {
	out, err := run(ctx, binary, "--version")
	if err != nil {
		return ""
	}
	first, _, _ := strings.Cut(string(out), "\n")
	return versionRe.FindString(first)
}

// UpdateArgv is the harness's own update verb. ollama has none — it is
// installed out of band — so it returns false and the UI offers no action.
func UpdateArgv(agent string) ([]string, bool) {
	switch agent {
	case "claude", "codex":
		return []string{"update"}, true
	case "opencode":
		return []string{"upgrade"}, true
	default:
		return nil, false
	}
}
