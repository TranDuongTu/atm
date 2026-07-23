package dispatch

import "strings"

// templateTarget runs the user-configured terminal_cmd via `sh -c` after
// placeholder substitution.
type templateTarget struct {
	env  Env
	tmpl string
}

func (t templateTarget) Name() string     { return "terminal" }
func (t templateTarget) Describe() string { return "terminal · configured command" }

func (t templateTarget) Spawn(s Spec) error {
	line := strings.NewReplacer(
		"{cmd}", ShellCommand(s.Argv),
		"{dir}", shellQuote(s.Dir),
		"{title}", shellQuote(s.Title),
	).Replace(t.tmpl)
	_, err := t.env.Run([]string{"sh", "-c", line})
	return err
}

// shellQuote single-quotes v for safe interpolation into a sh -c template,
// escaping embedded single quotes with the '\” idiom.
func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// emulator is one spawn-table row: an env fingerprint plus the argv that
// opens a new tab (window where the emulator has no tabs).
type emulator struct {
	name        string
	bin         string
	fingerprint func(Env) bool
	argv        func(Spec) []string
}

var emulators = []emulator{
	{
		name: "kitty", bin: "kitty",
		fingerprint: func(e Env) bool { return e.Getenv("KITTY_LISTEN_ON") != "" },
		argv: func(s Spec) []string {
			return append([]string{"kitty", "@", "launch", "--type=tab", "--tab-title", s.Title, "--cwd", s.Dir, "--"}, s.Argv...)
		},
	},
	{
		name: "wezterm", bin: "wezterm",
		fingerprint: func(e Env) bool { return e.Getenv("WEZTERM_UNIX_SOCKET") != "" },
		argv: func(s Spec) []string {
			return append([]string{"wezterm", "cli", "spawn", "--cwd", s.Dir, "--"}, s.Argv...)
		},
	},
	{
		name: "gnome-terminal", bin: "gnome-terminal",
		fingerprint: func(e Env) bool { return e.Getenv("GNOME_TERMINAL_SCREEN") != "" },
		argv: func(s Spec) []string {
			return append([]string{"gnome-terminal", "--tab", "--title", s.Title, "--working-directory", s.Dir, "--"}, s.Argv...)
		},
	},
	{
		name: "konsole", bin: "konsole",
		fingerprint: func(e Env) bool { return e.Getenv("KONSOLE_VERSION") != "" },
		argv: func(s Spec) []string {
			return append([]string{"konsole", "--new-tab", "--workdir", s.Dir, "-e"}, s.Argv...)
		},
	},
	{
		name: "alacritty", bin: "alacritty",
		fingerprint: func(e Env) bool { return e.Getenv("ALACRITTY_SOCKET") != "" },
		argv: func(s Spec) []string {
			return append([]string{"alacritty", "msg", "create-window", "--working-directory", s.Dir, "-e"}, s.Argv...)
		},
	},
	{
		name: "foot", bin: "foot",
		fingerprint: func(e Env) bool { return strings.HasPrefix(e.Getenv("TERM"), "foot") },
		argv: func(s Spec) []string {
			return append([]string{"foot", "--title", s.Title, "--working-directory", s.Dir}, s.Argv...)
		},
	},
}

type emulatorTarget struct {
	env Env
	em  emulator
}

func (t emulatorTarget) Name() string     { return "terminal" }
func (t emulatorTarget) Describe() string { return "terminal · " + t.em.name }
func (t emulatorTarget) Spawn(s Spec) error {
	_, err := t.env.Run(t.em.argv(s))
	return err
}

// terminalTarget resolves the terminal step of detection: a configured
// template always wins; otherwise the first emulator whose fingerprint
// matches and whose binary resolves.
func terminalTarget(cfg Config, env Env) (Target, bool) {
	if cfg.TerminalCmd != "" {
		return templateTarget{env: env, tmpl: cfg.TerminalCmd}, true
	}
	for _, em := range emulators {
		if !em.fingerprint(env) {
			continue
		}
		if _, err := env.LookPath(em.bin); err != nil {
			continue
		}
		return emulatorTarget{env: env, em: em}, true
	}
	return nil, false
}
