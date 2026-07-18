package capability

import (
	"bytes"
	"io"
	"testing"

	"atm/internal/core"

	"github.com/spf13/cobra"
)

type fakeEnv struct{ out bytes.Buffer }

func (f *fakeEnv) OpenService() (core.Service, error)               { return nil, nil }
func (f *fakeEnv) Stdout() io.Writer                                { return &f.out }
func (f *fakeEnv) Stderr() io.Writer                                { return io.Discard }
func (f *fakeEnv) Emit(v any, textFn func()) error                  { textFn(); return nil }
func (f *fakeEnv) RequireMutatingActor() (string, error)            { return "t@t:t", nil }
func (f *fakeEnv) ResolveActor(bool) (string, error)                { return "t@t:t", nil }
func (f *fakeEnv) BindActorFlag(*cobra.Command)                     {}
func (f *fakeEnv) BindTaskIDFlags(*cobra.Command, *string, *string) {}
func (f *fakeEnv) ResolveTaskID(id, _ string) (string, error)       { return id, nil }
func (f *fakeEnv) TaskJSON(t *core.Task) any                        { return t }

func TestDescribeEnumeratesInRegistrationOrder(t *testing.T) {
	r := NewRegistry(
		&fakeCap{name: "alpha", cmdName: "al", summary: "does alpha"},
		&fakeCap{name: "beta", cmdName: "be", summary: "does beta"},
	)
	got := r.Describe()
	want := []Description{
		{Name: "alpha", Summary: "does alpha"},
		{Name: "beta", Summary: "does beta"},
	}
	if len(got) != len(want) {
		t.Fatalf("Describe len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Describe[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDescribeNilRegistry(t *testing.T) {
	var r *Registry
	if got := r.Describe(); got != nil {
		t.Fatalf("nil registry Describe = %v, want nil", got)
	}
}

func TestCommandsMountGuideSubcommand(t *testing.T) {
	env := &fakeEnv{}
	r := NewRegistry(&fakeCap{name: "alpha", cmdName: "al", summary: "does alpha", guide: "GUIDE BODY\n"})
	cmds := r.Commands(env)
	if len(cmds) != 1 {
		t.Fatalf("Commands len = %d, want 1", len(cmds))
	}
	var guide *cobra.Command
	for _, sub := range cmds[0].Commands() {
		if sub.Name() == "guide" {
			guide = sub
		}
	}
	if guide == nil {
		t.Fatal("no guide subcommand mounted")
	}
	if err := guide.RunE(guide, nil); err != nil {
		t.Fatalf("guide RunE: %v", err)
	}
	if env.out.String() != "GUIDE BODY\n" {
		t.Errorf("guide output = %q, want the Guide() text", env.out.String())
	}
}
