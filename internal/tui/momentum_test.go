package tui

import (
	"reflect"
	"testing"

	"atm/internal/capability/qa"
	"atm/internal/capability/scrum"
)

func TestNewFlowSocketsScrumExcludesEvictFromClaim(t *testing.T) {
	s := newFlowSockets(scrum.New(), "ATM")
	if !reflect.DeepEqual(s.claimPrefixes, []string{"ATM:scrum:"}) {
		t.Fatalf("claimPrefixes = %#v, want [ATM:scrum:]", s.claimPrefixes)
	}
	if s.finish != "ATM:scrum-stage:done" {
		t.Fatalf("finish = %q", s.finish)
	}
	if s.evictPrefix != "ATM:scrum-out:" {
		t.Fatalf("evictPrefix = %q", s.evictPrefix)
	}
}

func TestNewFlowSocketsQAFinishOnClaimAxis(t *testing.T) {
	s := newFlowSockets(qa.New(), "ATM")
	if !reflect.DeepEqual(s.claimPrefixes, []string{"ATM:qa:"}) {
		t.Fatalf("claimPrefixes = %#v", s.claimPrefixes)
	}
	if s.finish != "ATM:qa:done" {
		t.Fatalf("finish = %q", s.finish)
	}
}

func TestFlowSocketsStateOf(t *testing.T) {
	s := newFlowSockets(scrum.New(), "ATM")
	cases := []struct {
		name   string
		labels []string
		want   flowState
	}{
		{"none", nil, flowNone},
		{"claimed", []string{"ATM:scrum:task"}, flowOpen},
		{"claimed+stage", []string{"ATM:scrum:task", "ATM:scrum-stage:planned"}, flowOpen},
		{"finished", []string{"ATM:scrum:task", "ATM:scrum-stage:done"}, flowFinished},
		{"evicted", []string{"ATM:scrum-out:duplicate"}, flowEvicted},
		{"evict wins over finish", []string{"ATM:scrum-stage:done", "ATM:scrum-out:not-worth-it"}, flowEvicted},
		{"stage without claim is not open", []string{"ATM:scrum-stage:planned"}, flowNone},
	}
	for _, c := range cases {
		m := map[string]bool{}
		for _, l := range c.labels {
			m[l] = true
		}
		if got := s.stateOf(m); got != c.want {
			t.Errorf("%s: stateOf = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFlowSocketsQAStateOfDoneOnClaimAxis(t *testing.T) {
	s := newFlowSockets(qa.New(), "ATM")
	if got := s.stateOf(map[string]bool{"ATM:qa:done": true}); got != flowFinished {
		t.Fatalf("qa:done alone = %v, want flowFinished", got)
	}
	if got := s.stateOf(map[string]bool{"ATM:qa:testing": true}); got != flowOpen {
		t.Fatalf("qa:testing = %v, want flowOpen", got)
	}
}

func TestFlowSocketsRelevant(t *testing.T) {
	s := newFlowSockets(scrum.New(), "ATM")
	for _, l := range []string{"ATM:scrum:task", "ATM:scrum-stage:done", "ATM:scrum-out:duplicate"} {
		if !s.relevant(l) {
			t.Errorf("relevant(%q) = false", l)
		}
	}
	for _, l := range []string{"ATM:qa:testing", "ATM:scrum-stage:planned"} {
		if s.relevant(l) {
			t.Errorf("relevant(%q) = true", l)
		}
	}
}
