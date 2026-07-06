package developing

import (
	"reflect"
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

func TestOllamaLauncherBuildArgvDoesNotMutate(t *testing.T) {
	l := OllamaLauncher{Integration: "opencode"}
	_ = l.BuildArgv()
	if l.Integration != "opencode" {
		t.Errorf("BuildArgv mutated Integration: %q", l.Integration)
	}
}
