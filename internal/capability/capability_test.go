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
	def     Definition
	vocab   []core.Label
}

func (f *fakeCap) Name() string { return f.name }

func (f *fakeCap) Summary() string { return f.summary }

func (f *fakeCap) Definition() Definition { return f.def }

func (f *fakeCap) Vocabulary(code string) []core.Label { return f.vocab }

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

func (f *fakeCap) Annotate(core.Task) *Cell { return nil }

func TestCommandsPreserveRegistrationOrder(t *testing.T) {
	var calls []string
	reg := NewRegistry(
		&fakeCap{name: "alpha", calls: &calls},
		&fakeCap{name: "beta", calls: &calls},
	)
	cmds := reg.Commands(nil)
	if len(cmds) != 2 || cmds[0].Use != "alpha" || cmds[1].Use != "beta" {
		t.Fatalf("Commands = %v, want [alpha beta]", cmds)
	}
}

// fakeSeedService records LabelSeedBatch calls. Only the batch method is
// ever invoked by the registry; the embedded nil LabelService panics on
// anything else, which is exactly the pin we want.
type fakeSeedService struct {
	core.LabelService
	batches [][]core.Label
	actors  []string
	err     error
}

func (f *fakeSeedService) LabelSeedBatch(labels []core.Label, actor string) error {
	f.batches = append(f.batches, append([]core.Label(nil), labels...))
	f.actors = append(f.actors, actor)
	return f.err
}

// TestEnsureVocabularyBatchesAcrossCapabilities: the registry concatenates
// every capability's Vocabulary in registration order into ONE batch call
// (one event-log fold per select — ATM-40faff).
func TestEnsureVocabularyBatchesAcrossCapabilities(t *testing.T) {
	var calls []string
	svc := &fakeSeedService{}
	reg := NewRegistry(
		&fakeCap{name: "alpha", calls: &calls, vocab: []core.Label{
			{Name: "ATM:status:open", Description: "open"},
			{Name: "ATM:open-tasks", Description: "board", Expr: "status:open"},
		}},
		&fakeCap{name: "beta", calls: &calls, vocab: []core.Label{
			{Name: "ATM:context-current", Description: "board", Expr: "context:*"},
		}},
	)
	boards, err := reg.EnsureVocabulary(svc, "ATM", "tester")
	if err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}
	if len(svc.batches) != 1 {
		t.Fatalf("batches = %d, want exactly 1", len(svc.batches))
	}
	if len(svc.batches[0]) != 3 || svc.batches[0][0].Name != "ATM:status:open" || svc.batches[0][2].Name != "ATM:context-current" {
		t.Errorf("batch = %+v, want the 3 labels in registration+vocabulary order", svc.batches[0])
	}
	if len(svc.actors) != 1 || svc.actors[0] != "tester" {
		t.Errorf("actors = %v, want [tester]", svc.actors)
	}
	if len(calls) != 0 {
		t.Errorf("registry called per-capability EnsureVocabulary %v; it must batch via Vocabulary instead", calls)
	}
	want := []core.Label{
		{Name: "ATM:open-tasks", Description: "board", Expr: "status:open"},
		{Name: "ATM:context-current", Description: "board", Expr: "context:*"},
	}
	if len(boards) != len(want) {
		t.Fatalf("boards = %+v, want %+v", boards, want)
	}
	for i, b := range boards {
		if b != want[i] {
			t.Errorf("boards[%d] = %+v, want %+v", i, b, want[i])
		}
	}
}

// TestEnsureVocabularyStopsAtFirstError: a batch error surfaces and no
// boards are returned.
func TestEnsureVocabularyStopsAtFirstError(t *testing.T) {
	boom := errors.New("boom")
	svc := &fakeSeedService{err: boom}
	var calls []string
	reg := NewRegistry(&fakeCap{name: "alpha", calls: &calls, vocab: []core.Label{{Name: "ATM:x", Description: "d"}}})
	if boards, err := reg.EnsureVocabulary(svc, "ATM", "tester"); !errors.Is(err, boom) || boards != nil {
		t.Fatalf("EnsureVocabulary = (%v, %v), want (nil, boom)", boards, err)
	}
}

// TestEnsureVocabularyEmptyRegistryTouchesNothing: no capabilities → no
// service call at all (also covers the nil-svc callers in older tests).
func TestEnsureVocabularyEmptyRegistryTouchesNothing(t *testing.T) {
	svc := &fakeSeedService{}
	boards, err := NewRegistry().EnsureVocabulary(svc, "ATM", "tester")
	if err != nil || boards != nil {
		t.Fatalf("EnsureVocabulary = (%v, %v), want (nil, nil)", boards, err)
	}
	if len(svc.batches) != 0 {
		t.Errorf("empty registry issued %d batch calls, want 0", len(svc.batches))
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

// listOnlyService is a core.LabelService whose only live method is LabelList.
type listOnlyService struct{ labels []core.Label }

func (s *listOnlyService) LabelList(project, namespace string) []core.Label       { return s.labels }
func (s *listOnlyService) LabelAdd(name, description, expr, actor string) error   { return nil }
func (s *listOnlyService) LabelSeed(name, description, expr, actor string) error  { return nil }
func (s *listOnlyService) LabelSeedBatch(labels []core.Label, actor string) error { return nil }
func (s *listOnlyService) LabelShow(name string) (core.Label, error)              { return core.Label{}, nil }
func (s *listOnlyService) LabelRemove(name, actor string) (*core.LabelRemoveResult, error) {
	return nil, nil
}
func (s *listOnlyService) LabelUsageGrouped(projectCode string) (map[string]int, error) {
	return nil, nil
}

func TestRegistryAnnotateResolvesByName(t *testing.T) {
	var calls []string
	reg := NewRegistry(&fakeCap{name: "alpha", calls: &calls})
	if got := reg.Annotate("nope", core.Task{}); got != nil {
		t.Errorf("unknown name = %+v, want nil", got)
	}
	if got := reg.Annotate("unmanaged", core.Task{}); got != nil {
		t.Errorf("unmanaged pseudo-capability = %+v, want nil", got)
	}
	var nilReg *Registry
	if got := nilReg.Annotate("alpha", core.Task{}); got != nil {
		t.Errorf("nil registry = %+v, want nil", got)
	}
}

type fakeParenterCap struct {
	fakeCap
	parent string
}

func (f *fakeParenterCap) ParentOf(core.Task) string { return f.parent }

func TestRegistryParentOfResolvesByName(t *testing.T) {
	var calls []string
	reg := NewRegistry(&fakeParenterCap{fakeCap: fakeCap{name: "withparent", calls: &calls}, parent: "ATM-000042"},
		&fakeCap{name: "plain", calls: &calls})
	if got := reg.ParentOf("withparent", core.Task{}); got != "ATM-000042" {
		t.Fatalf("ParentOf(withparent) = %q, want ATM-000042", got)
	}
	if got := reg.ParentOf("plain", core.Task{}); got != "" {
		t.Fatalf("non-Parenter capability answered %q, want empty", got)
	}
	if got := reg.ParentOf("nope", core.Task{}); got != "" {
		t.Fatalf("unknown capability answered %q, want empty", got)
	}
	var nilReg *Registry
	if got := nilReg.ParentOf("withparent", core.Task{}); got != "" {
		t.Fatalf("nil registry answered %q, want empty", got)
	}
}
