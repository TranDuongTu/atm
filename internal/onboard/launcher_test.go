package onboard

import (
	"reflect"
	"testing"
)

func TestOpencodeLauncherBuildArgv(t *testing.T) {
	l := OpencodeLauncher{}
	got := l.BuildArgv("/tmp/p.md", "ATM onboarding: FOO (FOO-x)")
	msg := onboardingMessagePrefix + "/tmp/p.md" + onboardingMessageSuffix
	want := []string{"opencode", "--auto", "--prompt", msg, "--title", "ATM onboarding: FOO (FOO-x)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildArgv = %v, want %v", got, want)
	}
}

func TestOpencodeLauncherNameAndHint(t *testing.T) {
	l := OpencodeLauncher{}
	if l.Name() != "opencode" {
		t.Errorf("Name = %q, want opencode", l.Name())
	}
	if l.NotFoundHint() != "https://opencode.ai" {
		t.Errorf("NotFoundHint = %q, want https://opencode.ai", l.NotFoundHint())
	}
}

func TestOllamaLauncherBuildArgv(t *testing.T) {
	l := OllamaLauncher{Integration: "opencode"}
	got := l.BuildArgv("/tmp/p.md", "ATM onboarding: FOO (FOO-x)")
	msg := onboardingMessagePrefix + "/tmp/p.md" + onboardingMessageSuffix
	want := []string{"ollama", "launch", "opencode", "--", "--auto", "--prompt", msg, "--title", "ATM onboarding: FOO (FOO-x)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildArgv = %v, want %v", got, want)
	}
}

func TestOllamaLauncherNameAndHint(t *testing.T) {
	l := OllamaLauncher{Integration: "codex"}
	if l.Name() != "ollama" {
		t.Errorf("Name = %q, want ollama", l.Name())
	}
	if l.NotFoundHint() != "https://ollama.com" {
		t.Errorf("NotFoundHint = %q, want https://ollama.com", l.NotFoundHint())
	}
}
