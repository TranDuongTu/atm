package cli

import (
	"bytes"
	"strings"
	"testing"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// TestCLIStateImplementsEnv pins the Env contract: the compile-time
// assertion in env.go is the real gate; this test exercises the two
// delegations with observable behavior.
func TestCLIStateImplementsEnv(t *testing.T) {
	var _ capability.Env = (*cliState)(nil)

	out := &bytes.Buffer{}
	st := &cliState{flags: globalFlags{output: outputJSON}, out: out}
	if err := st.Emit(map[string]any{"k": "v"}, func() { t.Fatal("textFn must not run in JSON mode") }); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !strings.Contains(out.String(), `"k": "v"`) && !strings.Contains(out.String(), `"k":"v"`) {
		t.Fatalf("Emit JSON = %q", out.String())
	}

	st2 := &cliState{}
	if _, err := st2.RequireMutatingActor(); err == nil {
		t.Fatal("RequireMutatingActor with no actor must error")
	}
	st2.flags.actor = "dev"
	actor, err := st2.RequireMutatingActor()
	if err != nil {
		t.Fatalf("RequireMutatingActor: %v", err)
	}
	if actor != "dev@cli:unset" {
		t.Fatalf("actor = %q, want dev@cli:unset", actor)
	}
}

// TestRegistryCommandsMount pins that a registry handed to Execute's state
// mounts its command trees on the root command.
func TestRegistryCommandsMount(t *testing.T) {
	st := &cliState{registry: capability.NewRegistry(&fakeMountCap{name: "fakecap"})}
	root := newRootCmdWithState(st)
	for _, c := range root.Commands() {
		if c.Use == "fakecap" {
			return
		}
	}
	t.Fatal("registry command not mounted on root")
}

type fakeMountCap struct {
	name string
}

func (f *fakeMountCap) Name() string { return f.name }

func (f *fakeMountCap) Summary() string { return "fake capability for mount test" }

func (f *fakeMountCap) Guide() string { return "fake capability guide" }

func (f *fakeMountCap) EnsureVocabulary(svc core.LabelService, code, actor string) error {
	return nil
}

func (f *fakeMountCap) Command(env capability.Env) *cobra.Command {
	return &cobra.Command{Use: f.name}
}

func (f *fakeMountCap) DefaultBoard(code string) string { return "" }
