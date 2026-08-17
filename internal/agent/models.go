package agent

import (
	"context"
	"errors"
	"strings"
)

// RunFunc runs a command and returns its stdout. Injected so listing is
// testable without touching a real binary.
type RunFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

// ErrNoLister means this launcher has no model-list verb, so the model must
// be typed by hand. It is not a failure.
var ErrNoLister = errors.New("launcher has no model list verb; enter the model name manually")

// ListModels asks whoever serves the model which models exist. The LAUNCHER
// decides: ollama serves the model to the harness, so ollama lists; a native
// harness lists its own. claude and codex ship no list verb at all.
func ListModels(ctx context.Context, s Selection, run RunFunc) ([]string, error) {
	switch {
	case s.Launcher == LauncherOllama:
		out, err := run(ctx, "ollama", "list")
		if err != nil {
			return nil, err
		}
		return firstColumnAfterHeader(out), nil
	case s.Agent == "opencode":
		out, err := run(ctx, "opencode", "models")
		if err != nil {
			return nil, err
		}
		return nonBlankLines(out), nil
	default:
		return nil, ErrNoLister
	}
}

func nonBlankLines(out []byte) []string {
	var models []string
	for _, line := range strings.Split(string(out), "\n") {
		if l := strings.TrimSpace(line); l != "" {
			models = append(models, l)
		}
	}
	return models
}

// firstColumnAfterHeader reads `ollama list`: a NAME-led header row followed
// by whitespace-aligned columns.
func firstColumnAfterHeader(out []byte) []string {
	var models []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "NAME" {
			continue
		}
		models = append(models, fields[0])
	}
	return models
}
