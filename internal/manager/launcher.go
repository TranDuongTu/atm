package manager

type Launcher interface {
	Name() string
	NotFoundHint() string
	BuildArgv() []string
	BuildArgvManage(contextPath string) []string
}

type staticLauncher struct {
	name          string
	hint          string
	argv          []string
	usePromptFlag bool
}

func (l staticLauncher) Name() string         { return l.name }
func (l staticLauncher) NotFoundHint() string { return l.hint }
func (l staticLauncher) BuildArgv() []string  { return append([]string(nil), l.argv...) }

func (l staticLauncher) msgArgv(msg string) []string {
	if l.usePromptFlag {
		return []string{"--prompt", msg}
	}
	return []string{msg}
}

func (l staticLauncher) BuildArgvManage(contextPath string) []string {
	msg := managerMessagePrefix + contextPath + managerMessageSuffix
	return append([]string{l.name}, l.msgArgv(msg)...)
}

func LauncherFor(name string) (Launcher, bool) {
	switch name {
	case "opencode":
		return staticLauncher{name: "opencode", hint: "https://opencode.ai", argv: []string{"opencode"}, usePromptFlag: true}, true
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

func agentMsgArgv(name string, msg string) []string {
	if name == "opencode" {
		return []string{"--prompt", msg}
	}
	return []string{msg}
}

func (l OllamaLauncher) BuildArgvManage(contextPath string) []string {
	msg := managerMessagePrefix + contextPath + managerMessageSuffix
	msgParts := agentMsgArgv(l.Integration, msg)
	return append([]string{"ollama", "launch", l.Integration, "--"}, msgParts...)
}

const (
	managerMessagePrefix = "Read the manager instructions in the file at "
	managerMessageSuffix = " and follow them exactly."
)
