// Package session launches a host agent as a persona: it renders the unified
// context prompt and builds the host argv. It replaces the former
// internal/developing and internal/manager packages.
package session

// Launcher builds the argv that starts a host agent. Both builders take the
// model because WHERE the model flag goes is the launcher's own knowledge:
// ollama needs it before its `--` separator, natives just take it as a flag.
// An empty model means "let the harness pick its own default" and yields an
// argv byte-identical to the pre-model shape.
type Launcher interface {
	Name() string
	NotFoundHint() string
	// BuildArgv launches the host bare (launch: hook personas — a session
	// plugin hook loads the context file from ATM_CONTEXT_FILE).
	BuildArgv(model string) []string
	// BuildArgvMessage launches the host with the given initial message
	// (launch: prompt personas). The message is caller-composed: the generic
	// PromptMessage(contextPath), or a persona's rendered kickoff template.
	BuildArgvMessage(msg, model string) []string
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

func (l staticLauncher) BuildArgv(model string) []string {
	return append([]string{l.name}, modelArgv(model)...)
}

func (l staticLauncher) BuildArgvMessage(msg, model string) []string {
	return append(l.BuildArgv(model), msgArgv(l.usePromptFlag, msg)...)
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

// BuildArgv places --model BEFORE the `--` separator, because it is ollama's
// own flag: ollama launch needs to know which model to serve. Verified on a
// real host — `ollama launch opencode --model qwen3:0.6b -- ...` launches with
// that model, while the same flag after the separator is rejected headless
// ("model selection requires an interactive terminal"). The value is the bare
// ollama model name, with no provider prefix.
func (l OllamaLauncher) BuildArgv(model string) []string {
	argv := append([]string{"ollama", "launch", l.Integration}, modelArgv(model)...)
	return append(argv, "--")
}

func (l OllamaLauncher) BuildArgvMessage(msg, model string) []string {
	return append(l.BuildArgv(model), msgArgv(l.Integration == "opencode", msg)...)
}

// modelArgv is shared because all three harnesses spell it --model.
func modelArgv(model string) []string {
	if model == "" {
		return nil
	}
	return []string{"--model", model}
}
