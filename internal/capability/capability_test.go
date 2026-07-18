package capability

import (
	"errors"
	"testing"

	"atm/internal/core"

	"github.com/spf13/cobra"
)

// fakeCap records EnsureVocabulary calls into a shared slice so tests can
// assert call order across a registry.
type fakeCap struct {
	name    string
	boards  []core.Label
	ensure  error
	calls   *[]string
	cmdName string
	summary string
	guide   string
}

func (f *fakeCap) Name() string { return f.name }

func (f *fakeCap) Summary() string { return f.summary }

func (f *fakeCap) Guide() string { return f.guide }

func (f *fakeCap) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error) {
	*f.calls = append(*f.calls, f.name+"/"+code+"/"+actor)
	return f.boards, f.ensure
}

func (f *fakeCap) Command(env Env) *cobra.Command {
	use := f.cmdName
	if use == "" {
		use = f.name
	}
	return &cobra.Command{Use: use}
}

func (f *fakeCap) ManagerActions() []ActionSpec { return nil }

func TestCommandsPreserveRegistrationOrder(t *testing.T) {
	var calls []string
	reg := NewRegistry(
		&fakeCap{name: "workflow", calls: &calls},
		&fakeCap{name: "contextmap", calls: &calls},
	)
	cmds := reg.Commands(nil)
	if len(cmds) != 2 || cmds[0].Use != "workflow" || cmds[1].Use != "contextmap" {
		t.Fatalf("Commands = %v, want [workflow contextmap]", cmds)
	}
}

func TestEnsureVocabularyLoopsAllInOrder(t *testing.T) {
	var calls []string
	reg := NewRegistry(
		&fakeCap{name: "workflow", calls: &calls},
		&fakeCap{name: "contextmap", calls: &calls},
	)
	if _, err := reg.EnsureVocabulary(nil, "ATM", "tester"); err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}
	if len(calls) != 2 || calls[0] != "workflow/ATM/tester" || calls[1] != "contextmap/ATM/tester" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestEnsureVocabularyStopsAtFirstError(t *testing.T) {
	var calls []string
	boom := errors.New("boom")
	reg := NewRegistry(
		&fakeCap{name: "workflow", ensure: boom, calls: &calls},
		&fakeCap{name: "contextmap", calls: &calls},
	)
	if _, err := reg.EnsureVocabulary(nil, "ATM", "tester"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %v, want only workflow", calls)
	}
}

// TestEnsureVocabularyAggregatesBoardsInRegistrationOrder asserts the
// registry unions each capability's returned boards in registration order.
func TestEnsureVocabularyAggregatesBoardsInRegistrationOrder(t *testing.T) {
	var calls []string
	reg := NewRegistry(
		&fakeCap{
			name:   "workflow",
			calls:  &calls,
			boards: []core.Label{{Name: "ATM:open-tasks", Expr: "status:open"}},
		},
		&fakeCap{
			name:   "contextmap",
			calls:  &calls,
			boards: []core.Label{{Name: "ATM:context-current", Expr: "context:*"}},
		},
	)
	boards, err := reg.EnsureVocabulary(nil, "ATM", "tester")
	if err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}
	want := []core.Label{
		{Name: "ATM:open-tasks", Expr: "status:open"},
		{Name: "ATM:context-current", Expr: "context:*"},
	}
	if len(boards) != len(want) {
		t.Fatalf("boards = %v, want %v", boards, want)
	}
	for i, b := range boards {
		if b != want[i] {
			t.Errorf("boards[%d] = %+v, want %+v", i, b, want[i])
		}
	}
}

func TestNilRegistryIsSafeAndEmpty(t *testing.T) {
	var reg *Registry
	if got := reg.Commands(nil); got != nil {
		t.Fatalf("Commands on nil = %v, want nil", got)
	}
	if boards, err := reg.EnsureVocabulary(nil, "ATM", "tester"); err != nil || boards != nil {
		t.Fatalf("EnsureVocabulary on nil = (%v, %v), want (nil, nil)", boards, err)
	}
}
