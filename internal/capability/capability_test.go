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
	vocab   []core.Label
	exposed []core.Label
}

func (f *fakeCap) Name() string { return f.name }

func (f *fakeCap) Summary() string { return f.summary }

func (f *fakeCap) Guide() string { return f.guide }

func (f *fakeCap) Vocabulary(code string) []core.Label { return f.vocab }

func (f *fakeCap) Exposed(code string) []core.Label { return f.exposed }

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

// listOnlyService is a core.LabelService whose only live method is LabelList.
type listOnlyService struct{ labels []core.Label }

func (s *listOnlyService) LabelList(project, namespace string) []core.Label      { return s.labels }
func (s *listOnlyService) LabelAdd(name, description, expr, actor string) error  { return nil }
func (s *listOnlyService) LabelSeed(name, description, expr, actor string) error { return nil }
func (s *listOnlyService) LabelShow(name string) (core.Label, error)             { return core.Label{}, nil }
func (s *listOnlyService) LabelRemove(name, actor string) (*core.LabelRemoveResult, error) {
	return nil, nil
}
func (s *listOnlyService) LabelUsageGrouped(projectCode string) (map[string]int, error) {
	return nil, nil
}

func TestRegistryExposedTagsOwnerInRegistrationOrder(t *testing.T) {
	var calls []string
	reg := NewRegistry(
		&fakeCap{name: "workflow", calls: &calls, exposed: []core.Label{
			{Name: "ATM:all-tasks", Expr: "*"}, {Name: "ATM:status:*"},
		}},
		&fakeCap{name: "contextmap", calls: &calls, exposed: []core.Label{
			{Name: "ATM:context-current", Expr: "context:*"},
		}},
	)
	got := reg.Exposed("ATM")
	want := []ExposedLabel{
		{Label: core.Label{Name: "ATM:all-tasks", Expr: "*"}, Owner: "workflow"},
		{Label: core.Label{Name: "ATM:status:*"}, Owner: "workflow"},
		{Label: core.Label{Name: "ATM:context-current", Expr: "context:*"}, Owner: "contextmap"},
	}
	if len(got) != len(want) {
		t.Fatalf("Exposed = %+v, want %+v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Exposed[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	var nilReg *Registry
	if nilReg.Exposed("ATM") != nil {
		t.Error("nil registry must return nil")
	}
}

// TestRegistryUnmanagedSubtractsOwnership is the core diff: owned = vocabulary
// FullNames + members under owned :* descriptors. Capability-internal labels
// (status:open) and ad-hoc members of an owned namespace (status:wip) are
// managed; loose tags, unowned namespaces, and leftover descriptors are not.
func TestRegistryUnmanagedSubtractsOwnership(t *testing.T) {
	var calls []string
	wf := &fakeCap{name: "workflow", calls: &calls, vocab: []core.Label{
		{Name: "ATM:status:*"}, {Name: "ATM:status:open"},
		{Name: "ATM:all-tasks", Expr: "*"},
	}}
	svc := &listOnlyService{labels: []core.Label{
		{Name: "ATM:all-tasks", Expr: "*"},     // owned board
		{Name: "ATM:status:*"},                 // owned descriptor
		{Name: "ATM:status:open"},              // owned member (exact)
		{Name: "ATM:status:wip"},               // ad-hoc member of owned ns -> managed
		{Name: "ATM:type:bug"},                 // unowned ns member -> unmanaged
		{Name: "ATM:type:*"},                   // unowned descriptor -> unmanaged
		{Name: "ATM:urgent"},                   // loose tag -> unmanaged
		{Name: "ATM:my-board", Expr: "urgent"}, // user board -> unmanaged
	}}
	reg := NewRegistry(wf)
	got, err := reg.Unmanaged(svc, "ATM")
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"ATM:type:bug", "ATM:type:*", "ATM:urgent", "ATM:my-board"}
	if len(got) != len(wantNames) {
		t.Fatalf("Unmanaged = %+v, want names %v", got, wantNames)
	}
	for i, l := range got {
		if l.Name != wantNames[i] {
			t.Errorf("Unmanaged[%d] = %s, want %s", i, l.Name, wantNames[i])
		}
	}
	// Repeated calls are stable (pure read).
	again, _ := reg.Unmanaged(svc, "ATM")
	if len(again) != len(got) {
		t.Errorf("second call = %d labels, want %d", len(again), len(got))
	}
}

func TestRegistryUnmanagedEmptyWhenAllOwned(t *testing.T) {
	var calls []string
	wf := &fakeCap{name: "workflow", calls: &calls, vocab: []core.Label{{Name: "ATM:status:*"}}}
	svc := &listOnlyService{labels: []core.Label{{Name: "ATM:status:*"}, {Name: "ATM:status:open"}}}
	got, err := NewRegistry(wf).Unmanaged(svc, "ATM")
	if err != nil || len(got) != 0 {
		t.Fatalf("Unmanaged = (%v, %v), want empty", got, err)
	}
	// Disabled capability (empty registry): everything is unmanaged.
	all, _ := NewRegistry().Unmanaged(svc, "ATM")
	if len(all) != 2 {
		t.Fatalf("empty registry Unmanaged = %v, want both labels", all)
	}
}

func TestOrderFullNames(t *testing.T) {
	effective := []string{"a", "b", "c", "d"}
	got := OrderFullNames(effective, []string{"c", "zzz-stale", "a"})
	want := []string{"c", "a", "b", "d"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("OrderFullNames = %v, want %v", got, want)
		}
	}
	if len(OrderFullNames(nil, []string{"x"})) != 0 {
		t.Error("empty effective must return empty")
	}
	if g := OrderFullNames(effective, nil); g[0] != "a" || g[3] != "d" {
		t.Errorf("no override must keep original order, got %v", g)
	}
}

func TestUmbrellaFullName(t *testing.T) {
	if UmbrellaFullName("ATM") != "ATM:unmanaged" {
		t.Fatalf("UmbrellaFullName = %q", UmbrellaFullName("ATM"))
	}
}

func TestRegistryAnnotateResolvesByName(t *testing.T) {
	var calls []string
	reg := NewRegistry(&fakeCap{name: "workflow", calls: &calls})
	if got := reg.Annotate("nope", core.Task{}); got != nil {
		t.Errorf("unknown name = %+v, want nil", got)
	}
	if got := reg.Annotate("unmanaged", core.Task{}); got != nil {
		t.Errorf("unmanaged pseudo-capability = %+v, want nil", got)
	}
	var nilReg *Registry
	if got := nilReg.Annotate("workflow", core.Task{}); got != nil {
		t.Errorf("nil registry = %+v, want nil", got)
	}
}

func TestLabelSetContains(t *testing.T) {
	s := NewLabelSet([]core.Label{
		{Name: "ATM:status:*"},
		{Name: "ATM:all-tasks", Expr: "*"},
		{Name: "ATM:needs-triage"},
	})
	for name, want := range map[string]bool{
		"ATM:all-tasks":     true,  // exact board
		"ATM:needs-triage":  true,  // exact tag
		"ATM:status:*":      true,  // exact descriptor
		"ATM:status:wip":    true,  // member of owned namespace
		"ATM:priority:high": false, // unowned namespace member
		"ATM:other-tag":     false, // unowned tag
	} {
		if got := s.Contains(name); got != want {
			t.Errorf("Contains(%q) = %v, want %v", name, got, want)
		}
	}
}
