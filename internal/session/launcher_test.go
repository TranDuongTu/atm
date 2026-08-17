package session

import (
	"reflect"
	"strings"
	"testing"
)

func TestLauncherFor(t *testing.T) {
	for _, name := range []string{"opencode", "codex", "claude"} {
		l, ok := LauncherFor(name)
		if !ok || l.Name() != name {
			t.Fatalf("LauncherFor(%s) = %v %v", name, l, ok)
		}
		if got := l.BuildArgv(""); len(got) == 0 || got[0] != name {
			t.Fatalf("BuildArgv = %v", got)
		}
	}
	if _, ok := LauncherFor("nope"); ok {
		t.Fatal("unknown launcher must be !ok")
	}
}

func TestBuildArgvPrompt(t *testing.T) {
	l, _ := LauncherFor("claude")
	argv := l.BuildArgvPrompt("/tmp/ctx.md", "")
	if argv[0] != "claude" || !strings.Contains(strings.Join(argv, " "), "/tmp/ctx.md") {
		t.Fatalf("argv = %v", argv)
	}
	oc, _ := LauncherFor("opencode")
	ocArgv := oc.BuildArgvPrompt("/tmp/ctx.md", "")
	if ocArgv[1] != "--prompt" {
		t.Fatalf("opencode uses --prompt: %v", ocArgv)
	}
	ol := OllamaLauncher{Integration: "opencode"}
	olArgv := ol.BuildArgvPrompt("/tmp/ctx.md", "")
	if olArgv[0] != "ollama" || olArgv[1] != "launch" || !strings.Contains(strings.Join(olArgv, " "), "--prompt") {
		t.Fatalf("ollama argv = %v", olArgv)
	}
}

// An empty model must leave every argv byte-identical to the pre-model shape:
// an unconfigured store launches exactly as it always has.
func TestBuildArgvEmptyModelIsUnchanged(t *testing.T) {
	cx, _ := LauncherFor("codex")
	if got := cx.BuildArgv(""); !reflect.DeepEqual(got, []string{"codex"}) {
		t.Fatalf("codex BuildArgv = %v", got)
	}
	ol := OllamaLauncher{Integration: "codex"}
	if got := ol.BuildArgv(""); !reflect.DeepEqual(got, []string{"ollama", "launch", "codex", "--"}) {
		t.Fatalf("ollama BuildArgv = %v", got)
	}
}

func TestBuildArgvNativeCarriesTheModel(t *testing.T) {
	for _, name := range []string{"claude", "codex", "opencode"} {
		l, ok := LauncherFor(name)
		if !ok {
			t.Fatalf("no launcher for %q", name)
		}
		if got := l.BuildArgv("some-model"); !reflect.DeepEqual(got, []string{name, "--model", "some-model"}) {
			t.Fatalf("%s BuildArgv = %v", name, got)
		}
	}
	oc, _ := LauncherFor("opencode")
	want := []string{"opencode", "--model", "some-model", "--prompt", PromptMessage("/tmp/ctx.md")}
	if got := oc.BuildArgvPrompt("/tmp/ctx.md", "some-model"); !reflect.DeepEqual(got, want) {
		t.Fatalf("opencode BuildArgvPrompt = %v, want %v", got, want)
	}
}

// ollama launch needs the model as ITS OWN flag, before the `--` separator:
// verified on a real host, `ollama launch opencode -- --model X` is rejected
// headless ("model selection requires an interactive terminal").
func TestBuildArgvOllamaPutsTheModelBeforeTheSeparator(t *testing.T) {
	l := OllamaLauncher{Integration: "opencode"}
	want := []string{"ollama", "launch", "opencode", "--model", "qwen3:8b", "--"}
	if got := l.BuildArgv("qwen3:8b"); !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgv = %v, want %v", got, want)
	}
	wantPrompt := append(append([]string(nil), want...), "--prompt", PromptMessage("/tmp/ctx.md"))
	if got := l.BuildArgvPrompt("/tmp/ctx.md", "qwen3:8b"); !reflect.DeepEqual(got, wantPrompt) {
		t.Fatalf("BuildArgvPrompt = %v, want %v", got, wantPrompt)
	}
}
