package developing

type Launcher interface {
	Name() string
	NotFoundHint() string
	BuildArgv() []string
}

type staticLauncher struct {
	name string
	hint string
	argv []string
}

func (l staticLauncher) Name() string         { return l.name }
func (l staticLauncher) NotFoundHint() string { return l.hint }
func (l staticLauncher) BuildArgv() []string  { return append([]string(nil), l.argv...) }

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

// OllamaLauncher execs `ollama launch <integration> --` for an interactive
// developing session. The `--` separator is ollama launch's documented
// passthrough; extra agent args append after it (on the integration side).
// ATM does not validate the integration name; unknown values fail at
// `ollama launch`'s door. Constructed directly by the CLI's ollama subcommand.
// LauncherFor stays ok=false for "ollama" because the integration is not
// known at factory time.
type OllamaLauncher struct {
	Integration string
}

func (l OllamaLauncher) Name() string         { return "ollama" }
func (l OllamaLauncher) NotFoundHint() string { return "https://ollama.com" }
func (l OllamaLauncher) BuildArgv() []string {
	return []string{"ollama", "launch", l.Integration, "--"}
}
