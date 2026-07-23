package dispatch

import (
	"fmt"
	"strings"
)

// herdrAvailable reports whether we run inside a herdr-managed pane (herdr
// injects HERDR_ENV=1 and HERDR_SOCKET_PATH) with the binary resolvable.
func herdrAvailable(env Env) bool {
	if env.Getenv("HERDR_ENV") != "1" && env.Getenv("HERDR_SOCKET_PATH") == "" {
		return false
	}
	_, err := env.LookPath("herdr")
	return err == nil
}

type herdrTarget struct{ env Env }

func (h herdrTarget) Name() string     { return "herdr" }
func (h herdrTarget) Describe() string { return "herdr · new pane" }

// Spawn creates a pane, sets its label, then runs the launcher in it. The
// split prints the new pane id; the last whitespace-separated token is taken
// so both bare-id and labeled outputs parse. Split direction defaults to
// "down" (a full-width pane suits an agent CLI session).
func (h herdrTarget) Spawn(s Spec) error {
	out, err := h.env.Run([]string{"herdr", "pane", "split", "--direction", "down", "--cwd", s.Dir})
	if err != nil {
		return fmt.Errorf("herdr pane split: %w", err)
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return fmt.Errorf("herdr pane split printed no pane id")
	}
	paneID := fields[len(fields)-1]
	if _, err := h.env.Run([]string{"herdr", "pane", "rename", paneID, s.Title}); err != nil {
		return fmt.Errorf("herdr pane rename: %w", err)
	}
	if _, err := h.env.Run([]string{"herdr", "pane", "run", paneID, ShellCommand(s.Argv)}); err != nil {
		return fmt.Errorf("herdr pane run: %w", err)
	}
	return nil
}
