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

// OpencodeLauncher execs `opencode run "<msg>" -f <prompt> --auto --title
// <title>`. opencode run requires a positional message; -f only attaches a
// file alongside the message, it does not replace one. The message instructs
// the agent to read the attached prompt file and follow it.
type OpencodeLauncher struct{}

func (OpencodeLauncher) Name() string         { return "opencode" }
func (OpencodeLauncher) NotFoundHint() string { return "https://opencode.ai" }
func (OpencodeLauncher) BuildArgv(promptPath, title string) []string {
	return []string{"opencode", "run", onboardingMessage, "-f", promptPath, "--auto", "--title", title}
}

// OllamaLauncher execs `ollama launch <integration> -- run "<msg>" -f <prompt>
// --auto --title <title>`. The `--` separator is ollama launch's documented
// passthrough; ATM does not validate the integration name.
type OllamaLauncher struct {
	Integration string
}

func (OllamaLauncher) Name() string         { return "ollama" }
func (OllamaLauncher) NotFoundHint() string { return "https://ollama.com" }
func (l OllamaLauncher) BuildArgv(promptPath, title string) []string {
	return []string{"ollama", "launch", l.Integration, "--",
		"run", onboardingMessage, "-f", promptPath, "--auto", "--title", title}
}

// onboardingMessage is the positional message handed to `opencode run` /
// `ollama launch <integration> -- run ...`. It points the agent at the
// attached prompt file (-f), which carries the full onboarding instructions.
const onboardingMessage = "Read the attached prompt file and follow its instructions exactly."
