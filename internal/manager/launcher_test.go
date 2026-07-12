package manager

import (
	"reflect"
	"strings"
	"testing"
)

func TestLaunchersBuildNormalInteractiveArgv(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{name: "opencode", want: []string{"opencode"}},
		{name: "codex", want: []string{"codex"}},
		{name: "claude", want: []string{"claude"}},
	}
	for _, tt := range tests {
		l, ok := LauncherFor(tt.name)
		if !ok {
			t.Fatalf("LauncherFor(%q) not found", tt.name)
		}
		if got := l.BuildArgv(); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s BuildArgv = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestLauncherHints(t *testing.T) {
	tests := map[string]string{
		"opencode": "https://opencode.ai",
		"codex":    "https://developers.openai.com/codex",
		"claude":   "https://code.claude.com",
	}
	for name, wantHint := range tests {
		l, ok := LauncherFor(name)
		if !ok {
			t.Fatalf("LauncherFor(%q) not found", name)
		}
		if l.Name() != name {
			t.Errorf("Name = %q, want %q", l.Name(), name)
		}
		if l.NotFoundHint() != wantHint {
			t.Errorf("NotFoundHint = %q, want %q", l.NotFoundHint(), wantHint)
		}
	}
}

func TestLauncherForUnknown(t *testing.T) {
	if _, ok := LauncherFor("ollama"); ok {
		t.Fatal("LauncherFor(ollama) returned ok=true")
	}
}

func TestOllamaLauncherInteractiveArgv(t *testing.T) {
	l := OllamaLauncher{Integration: "codex"}
	if l.Name() != "ollama" {
		t.Errorf("Name = %q, want ollama", l.Name())
	}
	if l.NotFoundHint() != "https://ollama.com" {
		t.Errorf("NotFoundHint = %q, want https://ollama.com", l.NotFoundHint())
	}
	want := []string{"ollama", "launch", "codex", "--"}
	if got := l.BuildArgv(); !reflect.DeepEqual(got, want) {
		t.Errorf("OllamaLauncher BuildArgv = %v, want %v", got, want)
	}
}

func TestBuildArgvOnboardOpencode(t *testing.T) {
	l, ok := LauncherFor("opencode")
	if !ok {
		t.Fatal("LauncherFor(opencode) not found")
	}
	got := l.BuildArgvOnboard("/tmp/ctx.md")
	if len(got) < 4 || got[0] != "opencode" || got[1] != "--auto" || got[2] != "--prompt" {
		t.Fatalf("opencode BuildArgvOnboard = %v, want [--auto --prompt <msg>]", got)
	}
	if !strings.Contains(got[3], "/tmp/ctx.md") {
		t.Fatalf("opencode onboard prompt message should reference the context path; got %q", got[3])
	}
}

func TestBuildArgvOnboardOllama(t *testing.T) {
	l := OllamaLauncher{Integration: "opencode"}
	got := l.BuildArgvOnboard("/tmp/ctx.md")
	if len(got) < 6 || got[0] != "ollama" || got[1] != "launch" || got[2] != "opencode" || got[3] != "--" {
		t.Fatalf("ollama BuildArgvOnboard = %v, want [ollama launch opencode -- --auto --prompt <msg>]", got)
	}
}

func TestBuildArgvOnboardCodex(t *testing.T) {
	l, ok := LauncherFor("codex")
	if !ok {
		t.Fatal("LauncherFor(codex) not found")
	}
	got := l.BuildArgvOnboard("/tmp/ctx.md")
	if len(got) < 2 || got[0] != "codex" || !strings.Contains(got[1], "/tmp/ctx.md") {
		t.Fatalf("codex BuildArgvOnboard = %v, want [codex <msg>] with msg containing context path", got)
	}
	for _, s := range got {
		switch s {
		case "--auto", "--prompt":
			t.Fatalf("codex BuildArgvOnboard should not contain %s: %v", s, got)
		}
	}
}

func TestBuildArgvOnboardClaude(t *testing.T) {
	l, ok := LauncherFor("claude")
	if !ok {
		t.Fatal("LauncherFor(claude) not found")
	}
	got := l.BuildArgvOnboard("/tmp/ctx.md")
	if len(got) < 2 || got[0] != "claude" || !strings.Contains(got[1], "/tmp/ctx.md") {
		t.Fatalf("claude BuildArgvOnboard = %v, want [claude <msg>] with msg containing context path", got)
	}
	for _, s := range got {
		switch s {
		case "--auto", "--prompt":
			t.Fatalf("claude BuildArgvOnboard should not contain %s: %v", s, got)
		}
	}
}

func TestBuildArgvOnboardOllamaCodex(t *testing.T) {
	l := OllamaLauncher{Integration: "codex"}
	got := l.BuildArgvOnboard("/tmp/ctx.md")
	for _, s := range got {
		switch s {
		case "--auto", "--prompt":
			t.Fatalf("ollama:codex BuildArgvOnboard should not contain %s: %v", s, got)
		}
	}
	if !strings.Contains(got[len(got)-1], "/tmp/ctx.md") {
		t.Fatalf("ollama:codex BuildArgvOnboard last arg should contain context path: %v", got)
	}
}
