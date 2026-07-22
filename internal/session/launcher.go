// Package session launches a host agent as a persona: it renders the unified
// context prompt and builds the host argv. It replaces the former
// internal/developing and internal/manager packages.
package session

type Launcher interface {
	Name() string
	NotFoundHint() string
	// BuildArgv launches the host bare (launch: hook personas — a session
	// plugin hook loads the context file from ATM_CONTEXT_FILE).
	BuildArgv() []string
	// BuildArgvPrompt launches the host with an initial message pointing at
	// the rendered context file (launch: prompt personas).
	BuildArgvPrompt(contextPath string) []string
}

const (
	promptMessagePrefix = "Read the session instructions in the file at "
	promptMessageSuffix = " and follow them exactly."
)

// PromptMessage is the initial message for launch:prompt personas.
func PromptMessage(contextPath string) string {
	return promptMessagePrefix + contextPath + promptMessageSuffix
}

type staticLauncher struct {
	name          string
	hint          string
	usePromptFlag bool
}

func (l staticLauncher) Name() string         { return l.name }
func (l staticLauncher) NotFoundHint() string { return l.hint }
func (l staticLauncher) BuildArgv() []string  { return []string{l.name} }

func (l staticLauncher) BuildArgvPrompt(contextPath string) []string {
	return append([]string{l.name}, msgArgv(l.usePromptFlag, PromptMessage(contextPath))...)
}

func msgArgv(usePromptFlag bool, msg string) []string {
	if usePromptFlag {
		return []string{"--prompt", msg}
	}
	return []string{msg}
}

func LauncherFor(name string) (Launcher, bool) {
	switch name {
	case "opencode":
		return staticLauncher{name: "opencode", hint: "https://opencode.ai", usePromptFlag: true}, true
	case "codex":
		return staticLauncher{name: "codex", hint: "https://developers.openai.com/codex"}, true
	case "claude":
		return staticLauncher{name: "claude", hint: "https://code.claude.com"}, true
	default:
		return nil, false
	}
}

// OllamaLauncher execs `ollama launch <integration> --`; extra args pass
// through after the separator. LauncherFor stays ok=false for "ollama"
// because the integration is not known at factory time.
type OllamaLauncher struct {
	Integration string
}

func (l OllamaLauncher) Name() string         { return "ollama" }
func (l OllamaLauncher) NotFoundHint() string { return "https://ollama.com" }
func (l OllamaLauncher) BuildArgv() []string {
	return []string{"ollama", "launch", l.Integration, "--"}
}

func (l OllamaLauncher) BuildArgvPrompt(contextPath string) []string {
	return append(l.BuildArgv(), msgArgv(l.Integration == "opencode", PromptMessage(contextPath))...)
}
