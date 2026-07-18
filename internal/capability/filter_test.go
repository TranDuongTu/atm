package capability

import (
	"reflect"
	"testing"

	"atm/internal/core"
)

func TestNames(t *testing.T) {
	r := NewRegistry(&fakeCap{name: "alpha"}, &fakeCap{name: "beta"})
	if got, want := r.Names(), []string{"alpha", "beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Names = %v, want %v", got, want)
	}
	var nilr *Registry
	if nilr.Names() != nil {
		t.Fatal("nil registry Names must be nil")
	}
}

func TestForFiltersByEnabledSet(t *testing.T) {
	r := NewRegistry(&fakeCap{name: "alpha"}, &fakeCap{name: "beta"})

	if got := r.For(nil); got != r {
		t.Error("For(nil project) must return the receiver (all enabled)")
	}
	legacy := &core.Project{Code: "L"} // Capabilities nil
	if got := r.For(legacy); got != r {
		t.Error("For(legacy project) must return the receiver (all enabled)")
	}
	narrowed := r.For(&core.Project{Code: "P", Capabilities: []string{"beta"}})
	if got, want := narrowed.Names(), []string{"beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("narrowed Names = %v, want %v", got, want)
	}
	none := r.For(&core.Project{Code: "P", Capabilities: []string{}})
	if got := none.Names(); len(got) != 0 {
		t.Fatalf("explicitly-none Names = %v, want empty", got)
	}
}
