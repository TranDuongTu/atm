package eventsource

import (
	"reflect"
	"testing"
)

// foldCapabilityFixture builds:
//
//	e1 project.created         (code P, name P)
//	e2 project.capability-enabled  parents [e1], payload {"capability": "workflow"}
//	e3 project.capability-enabled  parents [e2], payload {"capability": "contextmap"}
//	e4 project.capability-disabled parents [e3], payload {"capability": "contextmap"}
//
// and folds it.
func foldCapabilityFixture(t *testing.T) *State {
	t.Helper()
	c := testClock(1000)
	e1 := testEvent(t, c, replicaA, nil, ActionProjectCreated,
		Subject{Kind: "project", Code: "P"}, map[string]any{"alias": "P", "name": "proj"})
	e2 := testEvent(t, c, replicaA, []string{e1.ID}, ActionProjectCapabilityEnabled,
		Subject{Kind: "project", ID: e1.ID, Code: "P"}, map[string]any{"capability": "workflow"})
	e3 := testEvent(t, c, replicaA, []string{e2.ID}, ActionProjectCapabilityEnabled,
		Subject{Kind: "project", ID: e1.ID, Code: "P"}, map[string]any{"capability": "contextmap"})
	e4 := testEvent(t, c, replicaA, []string{e3.ID}, ActionProjectCapabilityDisabled,
		Subject{Kind: "project", ID: e1.ID, Code: "P"}, map[string]any{"capability": "contextmap"})
	return fold(t, e1, e2, e3, e4)
}

// foldProjectOnlyFixture builds a set with only project.created — no
// capability event ever recorded.
func foldProjectOnlyFixture(t *testing.T) *State {
	t.Helper()
	c := testClock(1000)
	e1 := testEvent(t, c, replicaA, nil, ActionProjectCreated,
		Subject{Kind: "project", Code: "P"}, map[string]any{"alias": "P", "name": "proj"})
	return fold(t, e1)
}

// foldEnableThenDisableFixture builds a set that enables then disables the
// same capability — explicitly recorded as all-disabled (non-nil empty).
func foldEnableThenDisableFixture(t *testing.T) *State {
	t.Helper()
	c := testClock(1000)
	e1 := testEvent(t, c, replicaA, nil, ActionProjectCreated,
		Subject{Kind: "project", Code: "P"}, map[string]any{"alias": "P", "name": "proj"})
	e2 := testEvent(t, c, replicaA, []string{e1.ID}, ActionProjectCapabilityEnabled,
		Subject{Kind: "project", ID: e1.ID, Code: "P"}, map[string]any{"capability": "workflow"})
	e3 := testEvent(t, c, replicaA, []string{e2.ID}, ActionProjectCapabilityDisabled,
		Subject{Kind: "project", ID: e1.ID, Code: "P"}, map[string]any{"capability": "workflow"})
	return fold(t, e1, e2, e3)
}

// singleProject asserts exactly one non-tombstoned project is in st.Projects
// and returns it.
func singleProject(t *testing.T, st *State) *ProjectState {
	t.Helper()
	var found *ProjectState
	for _, p := range st.Projects {
		if p.Tombstoned {
			continue
		}
		if found != nil {
			t.Fatalf("expected exactly one non-tombstoned project, found more than one")
		}
		found = p
	}
	if found == nil {
		t.Fatalf("expected exactly one non-tombstoned project, found none")
	}
	return found
}

func TestCapabilityMembershipFolds(t *testing.T) {
	st := foldCapabilityFixture(t)
	p := singleProject(t, st)
	if got, want := p.Capabilities, []string{"workflow"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Capabilities = %v, want %v", got, want)
	}
}

func TestNoCapabilityEventsMeansNil(t *testing.T) {
	// fold a set with only project.created
	st := foldProjectOnlyFixture(t)
	p := singleProject(t, st)
	if p.Capabilities != nil {
		t.Fatalf("Capabilities = %v, want nil (legacy project: no capability event ever)", p.Capabilities)
	}
}

func TestAllDisabledIsEmptyNotNil(t *testing.T) {
	// enable workflow then disable workflow
	st := foldEnableThenDisableFixture(t)
	p := singleProject(t, st)
	if p.Capabilities == nil || len(p.Capabilities) != 0 {
		t.Fatalf("Capabilities = %v, want non-nil empty (explicitly disabled)", p.Capabilities)
	}
}
