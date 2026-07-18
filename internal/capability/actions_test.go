package capability

import (
	"reflect"
	"testing"
)

type fakeActionCap struct {
	fakeCap
	actions []ActionSpec
}

func (c fakeActionCap) ManagerActions() []ActionSpec { return c.actions }

func TestManagerActionsAggregate(t *testing.T) {
	r := NewRegistry(
		&fakeActionCap{fakeCap: fakeCap{name: "alpha", cmdName: "al"}},
		&fakeActionCap{
			fakeCap: fakeCap{name: "beta", cmdName: "be"},
			actions: []ActionSpec{{Name: "sweep", Summary: "sweep the things"}},
		},
	)
	got := r.ManagerActions(&fakeEnv{})
	want := []ManagerAction{{Capability: "beta", Command: "be", Name: "sweep", Summary: "sweep the things"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ManagerActions = %+v, want %+v", got, want)
	}
	var nilr *Registry
	if nilr.ManagerActions(&fakeEnv{}) != nil {
		t.Fatal("nil registry must yield nil actions")
	}
}
