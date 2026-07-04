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

// OpencodeLauncher execs `opencode --auto --prompt "<msg>"`. The default
// `opencode` command (no `run` subcommand) opens the full native TUI the user
// is familiar with. `--prompt` sets the initial user message; `--auto` approves
// the onboarding agent's tool calls non-interactively. The message instructs
// the agent to read the onboarding prompt file and follow it.
//
// The prompt file is passed via the message text (an absolute path), not via
// `-f`, because the default `opencode` command does not accept `-f`. The agent
// reads the file at that path using its normal file tools. `--title` is also
// unavailable on the default command (it belongs to `opencode run`); the
// session title defaults to a truncation of the prompt.
type OpencodeLauncher struct{}

func (OpencodeLauncher) Name() string         { return "opencode" }
func (OpencodeLauncher) NotFoundHint() string { return "https://opencode.ai" }
func (OpencodeLauncher) BuildArgv(promptPath, title string) []string {
	msg := onboardingMessagePrefix + promptPath + onboardingMessageSuffix
	return []string{"opencode", "--auto", "--prompt", msg}
}

// OllamaLauncher execs `ollama launch <integration> -- --auto --prompt
// "<msg>"`. The `--` separator is ollama launch's documented passthrough; ATM
// does not validate the integration name. Everything after `--` is passed to
// the integration's default entrypoint (the full TUI for opencode).
type OllamaLauncher struct {
	Integration string
}

func (OllamaLauncher) Name() string         { return "ollama" }
func (OllamaLauncher) NotFoundHint() string { return "https://ollama.com" }
func (l OllamaLauncher) BuildArgv(promptPath, title string) []string {
	msg := onboardingMessagePrefix + promptPath + onboardingMessageSuffix
	return []string{"ollama", "launch", l.Integration, "--",
		"--auto", "--prompt", msg}
}

// onboardingMessagePrefix/Suffix wrap the prompt file path in a short
// instruction the agent reads as its first user message. The default
// `opencode` command has no `-f` file-attachment flag, so the message itself
// points at the file.
const (
	onboardingMessagePrefix = "Read the onboarding instructions in the file at "
	onboardingMessageSuffix = " and follow them exactly."
)
