// Package dispatch spawns agent sessions on a separate terminal surface:
// a herdr pane, a tmux window, or a new terminal tab/window. It composes
// no session logic — the Argv it spawns is always the atm launcher.
package dispatch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Spec describes one session spawn.
type Spec struct {
	Title  string   // surface label: task id, or "<CODE> · <persona>"
	Argv   []string // the atm launcher invocation
	Dir    string   // working directory
	Target string   // force a specific target: "herdr", "tmux", "terminal"; "" = auto-detect
}

// Env abstracts process environment and execution so targets are testable
// without real processes.
type Env struct {
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Run      func(argv []string) (string, error)
}

// OSEnv is the production Env: real environment, PATH, and process runner.
// Run returns trimmed stdout; on failure stderr is folded into the error.
func OSEnv() Env {
	return Env{
		Getenv:   os.Getenv,
		LookPath: exec.LookPath,
		Run: func(argv []string) (string, error) {
			out, err := exec.Command(argv[0], argv[1:]...).Output()
			if err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) && len(ee.Stderr) > 0 {
					return "", fmt.Errorf("%s: %s", argv[0], strings.TrimSpace(string(ee.Stderr)))
				}
				return "", fmt.Errorf("%s: %w", argv[0], err)
			}
			return strings.TrimSpace(string(out)), nil
		},
	}
}

// Target is one dispatch surface.
type Target interface {
	Name() string     // "herdr" | "tmux" | "terminal"
	Describe() string // human preview, e.g. "tmux · new window"
	Spawn(Spec) error
}

// Detect returns the first available target in precedence order
// herdr → tmux → terminal. When force is non-empty it bypasses detection
// and constructs the named target directly. The config template affects only
// the terminal step — a tmux session still wins over a configured terminal_cmd.
func Detect(cfg Config, env Env, force string) (Target, error) {
	switch force {
	case "herdr":
		return herdrTarget{env: env}, nil
	case "tmux":
		return tmuxTarget{env: env}, nil
	case "terminal":
		if t, ok := terminalTarget(cfg, env); ok {
			return t, nil
		}
		return nil, errors.New(`no terminal target: no known terminal detected and no "terminal_cmd" configured — set "terminal_cmd" in dispatch.json or pick another target`)
	case "":
		if herdrAvailable(env) {
			return herdrTarget{env: env}, nil
		}
		if tmuxAvailable(env) {
			return tmuxTarget{env: env}, nil
		}
		if t, ok := terminalTarget(cfg, env); ok {
			return t, nil
		}
	default:
		return nil, fmt.Errorf("unknown dispatch target %q (valid: herdr, tmux, terminal, or empty for auto)", force)
	}
	return nil, errors.New(`no dispatch target: not inside herdr or tmux and no known terminal detected — set "terminal_cmd" in dispatch.json at the store root`)
}
