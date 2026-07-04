package onboard

// Launcher builds the exec argv for a non-interactive agent run and provides
// the not-found install hint. Implementations are pure data; the CLI layer
// owns the actual exec.
type Launcher interface {
	// Name is the human-readable launcher name used in headers, tail
	// summaries, and actor defaults (e.g. "opencode", "ollama").
	Name() string
	// NotFoundHint is the install URL printed when the launcher binary is
	// not on PATH.
	NotFoundHint() string
	// BuildArgv returns the full exec argv for the launcher, given the
	// rendered prompt file path and the session title.
	BuildArgv(promptPath, title string) []string
}

// OpencodeLauncher execs `opencode run -f <prompt> --auto --title <title>`.
type OpencodeLauncher struct{}

func (OpencodeLauncher) Name() string         { return "opencode" }
func (OpencodeLauncher) NotFoundHint() string { return "https://opencode.ai" }
func (OpencodeLauncher) BuildArgv(promptPath, title string) []string {
	return []string{"opencode", "run", "-f", promptPath, "--auto", "--title", title}
}

// OllamaLauncher execs `ollama launch <integration> -- run -f <prompt> --auto
// --title <title>`. The `--` separator is ollama launch's documented
// passthrough; ATM does not validate the integration name.
type OllamaLauncher struct {
	Integration string
}

func (OllamaLauncher) Name() string         { return "ollama" }
func (OllamaLauncher) NotFoundHint() string { return "https://ollama.com" }
func (l OllamaLauncher) BuildArgv(promptPath, title string) []string {
	return []string{"ollama", "launch", l.Integration, "--",
		"run", "-f", promptPath, "--auto", "--title", title}
}
