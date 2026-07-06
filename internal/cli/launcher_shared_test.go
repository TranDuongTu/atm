package cli

import (
	"reflect"
	"testing"
)

func TestAgentEnvArgs_DirectHosts(t *testing.T) {
	t.Setenv("ATM_OPENCODE_ARGS", "--auto --foo bar")
	got := agentEnvArgs("opencode", "")
	want := []string{"--auto", "--foo", "bar"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("agentEnvArgs(opencode) = %v, want %v", got, want)
	}
}

func TestAgentEnvArgs_EmptyEnv(t *testing.T) {
	t.Setenv("ATM_CODEX_ARGS", "")
	if got := agentEnvArgs("codex", ""); got != nil {
		t.Errorf("agentEnvArgs(codex) with empty env = %v, want nil", got)
	}
}

func TestAgentEnvArgs_OllamaIntegrationPrecedence(t *testing.T) {
	t.Setenv("ATM_CODEX_ARGS", "--yolo")
	t.Setenv("ATM_OLLAMA_ARGS", "--generic")
	got := agentEnvArgs("ollama", "codex")
	want := []string{"--yolo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ollama integration precedence = %v, want %v", got, want)
	}
}

func TestAgentEnvArgs_OllamaNoIntegration(t *testing.T) {
	t.Setenv("ATM_OLLAMA_ARGS", "--generic")
	got := agentEnvArgs("ollama", "")
	want := []string{"--generic"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ollama no integration = %v, want %v", got, want)
	}
}

func TestAppendAgentArgs_Order(t *testing.T) {
	base := []string{"codex"}
	env := []string{"--yolo"}
	extra := []string{"--auto"}
	got := appendAgentArgs(base, env, extra)
	want := []string{"codex", "--yolo", "--auto"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendAgentArgs order = %v, want %v", got, want)
	}
}

func TestAppendAgentArgs_NoDedup(t *testing.T) {
	base := []string{"codex"}
	env := []string{"--yolo"}
	extra := []string{"--yolo"}
	got := appendAgentArgs(base, env, extra)
	want := []string{"codex", "--yolo", "--yolo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendAgentArgs should not dedup = %v, want %v", got, want)
	}
}

func TestAppendAgentArgs_BothEmpty(t *testing.T) {
	base := []string{"codex"}
	got := appendAgentArgs(base, nil, nil)
	want := []string{"codex"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendAgentArgs both empty = %v, want %v", got, want)
	}
}

func TestAppendAgentArgs_DoesNotMutateBase(t *testing.T) {
	base := []string{"codex"}
	_ = appendAgentArgs(base, []string{"--x"}, []string{"--y"})
	if base[0] != "codex" || len(base) != 1 {
		t.Errorf("appendAgentArgs mutated base: %v", base)
	}
}
