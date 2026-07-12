package manager

type Launcher interface {
	Name() string
	NotFoundHint() string
	BuildArgv() []string
	BuildArgvOnboard(contextPath string) []string
	BuildArgvManage(contextPath string) []string
}

type staticLauncher struct {
	name         string
	hint         string
	argv         []string
	supportsAuto bool
}

func (l staticLauncher) Name() string         { return l.name }
func (l staticLauncher) NotFoundHint() string { return l.hint }
func (l staticLauncher) BuildArgv() []string  { return append([]string(nil), l.argv...) }

func (l staticLauncher) BuildArgvOnboard(contextPath string) []string {
	msg := managerMessagePrefix + contextPath + managerMessageSuffix
	if l.supportsAuto {
		return []string{l.name, "--auto", "--prompt", msg}
	}
	return []string{l.name, "--prompt", msg}
}

func (l staticLauncher) BuildArgvManage(contextPath string) []string {
	msg := managerMessagePrefix + contextPath + managerMessageSuffix
	return []string{l.name, "--prompt", msg}
}

func LauncherFor(name string) (Launcher, bool) {
	switch name {
	case "opencode":
		return staticLauncher{name: "opencode", hint: "https://opencode.ai", argv: []string{"opencode"}, supportsAuto: true}, true
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
	if agentSupportsAutoFlag(l.Integration) {
		return []string{"ollama", "launch", l.Integration, "--",
			"--auto", "--prompt", msg}
	}
	return []string{"ollama", "launch", l.Integration, "--",
		"--prompt", msg}
}

func (l OllamaLauncher) BuildArgvManage(contextPath string) []string {
	msg := managerMessagePrefix + contextPath + managerMessageSuffix
	return []string{"ollama", "launch", l.Integration, "--",
		"--prompt", msg}
}

func agentSupportsAutoFlag(name string) bool {
	switch name {
	case "opencode":
		return true
	default:
		return false
	}
}

const (
	managerMessagePrefix = "Read the manager instructions in the file at "
	managerMessageSuffix = " and follow them exactly."
)
