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
	name   string
	board  string
	ensure error
	calls  *[]string
}

func (f *fakeCap) Name() string { return f.name }

func (f *fakeCap) EnsureVocabulary(svc core.LabelService, code, actor string) error {
	*f.calls = append(*f.calls, f.name+"/"+code+"/"+actor)
	return f.ensure
}

func (f *fakeCap) Command(env Env) *cobra.Command { return &cobra.Command{Use: f.name} }

func (f *fakeCap) DefaultBoard(code string) string { return f.board }

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
	if err := reg.EnsureVocabulary(nil, "ATM", "tester"); err != nil {
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
	if err := reg.EnsureVocabulary(nil, "ATM", "tester"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %v, want only workflow", calls)
	}
}

func TestDefaultBoardFirstNonEmptyWins(t *testing.T) {
	var calls []string
	reg := NewRegistry(
		&fakeCap{name: "contextmap", board: "", calls: &calls},
		&fakeCap{name: "workflow", board: "ATM:open-tasks", calls: &calls},
	)
	if got := reg.DefaultBoard("ATM"); got != "ATM:open-tasks" {
		t.Fatalf("DefaultBoard = %q, want ATM:open-tasks", got)
	}
}

func TestNilRegistryIsSafeAndEmpty(t *testing.T) {
	var reg *Registry
	if got := reg.Commands(nil); got != nil {
		t.Fatalf("Commands on nil = %v, want nil", got)
	}
	if err := reg.EnsureVocabulary(nil, "ATM", "tester"); err != nil {
		t.Fatalf("EnsureVocabulary on nil = %v, want nil", err)
	}
	if got := reg.DefaultBoard("ATM"); got != "" {
		t.Fatalf("DefaultBoard on nil = %q, want empty", got)
	}
}
