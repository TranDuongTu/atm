package session

import (
	"strings"
	"testing"
)

func TestLauncherFor(t *testing.T) {
	for _, name := range []string{"opencode", "codex", "claude"} {
		l, ok := LauncherFor(name)
		if !ok || l.Name() != name {
			t.Fatalf("LauncherFor(%s) = %v %v", name, l, ok)
		}
		if got := l.BuildArgv(); len(got) == 0 || got[0] != name {
			t.Fatalf("BuildArgv = %v", got)
		}
	}
	if _, ok := LauncherFor("nope"); ok {
		t.Fatal("unknown launcher must be !ok")
	}
}

func TestBuildArgvPrompt(t *testing.T) {
	l, _ := LauncherFor("claude")
	argv := l.BuildArgvPrompt("/tmp/ctx.md")
	if argv[0] != "claude" || !strings.Contains(strings.Join(argv, " "), "/tmp/ctx.md") {
		t.Fatalf("argv = %v", argv)
	}
	oc, _ := LauncherFor("opencode")
	ocArgv := oc.BuildArgvPrompt("/tmp/ctx.md")
	if ocArgv[1] != "--prompt" {
		t.Fatalf("opencode uses --prompt: %v", ocArgv)
	}
	ol := OllamaLauncher{Integration: "opencode"}
	olArgv := ol.BuildArgvPrompt("/tmp/ctx.md")
	if olArgv[0] != "ollama" || olArgv[1] != "launch" || !strings.Contains(strings.Join(olArgv, " "), "--prompt") {
		t.Fatalf("ollama argv = %v", olArgv)
	}
}
