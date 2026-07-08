package manager

type Launcher interface {
	Name() string
	NotFoundHint() string
	BuildArgv() []string
	BuildArgvOnboard(contextPath string) []string
}

type staticLauncher struct {
	name string
	hint string
	argv []string
}

func (l staticLauncher) Name() string         { return l.name }
func (l staticLauncher) NotFoundHint() string { return l.hint }
func (l staticLauncher) BuildArgv() []string  { return append([]string(nil), l.argv...) }

func (l staticLauncher) BuildArgvOnboard(contextPath string) []string {
	msg := managerMessagePrefix + contextPath + managerMessageSuffix
	return []string{l.name, "--auto", "--prompt", msg}
}

func LauncherFor(name string) (Launcher, bool) {
	switch name {
	case "opencode":
		return staticLauncher{name: "opencode", hint: "https://opencode.ai", argv: []string{"opencode"}}, true
	case "codex":
		return staticLauncher{name: "codex", hint: "https://developers.openai.com/codex", argv: []string{"codex"}}, true
	case "claude":
		return staticLauncher{name: "claude", hint: "https://code.claude.com", argv: []string{"claude"}}, true
	default:
		return nil, false
	}
}

type OllamaLauncher struct {
	Integration string
}

func (l OllamaLauncher) Name() string         { return "ollama" }
func (l OllamaLauncher) NotFoundHint() string { return "https://ollama.com" }
func (l OllamaLauncher) BuildArgv() []string {
	return []string{"ollama", "launch", l.Integration, "--"}
}

func (l OllamaLauncher) BuildArgvOnboard(contextPath string) []string {
	msg := managerMessagePrefix + contextPath + managerMessageSuffix
	return []string{"ollama", "launch", l.Integration, "--",
		"--auto", "--prompt", msg}
}

const (
	managerMessagePrefix = "Read the manager instructions in the file at "
	managerMessageSuffix = " and follow them exactly."
)
