package store

import (
	"sync"
	"testing"
)

func TestNextReturnsOldestUnblocked(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "type", []Label{{Name: "type:impl"}, {Name: "type:bug"}, {Name: "kind:convention"}}, nil, "human:alice")
	conv, _ := s.CreateTask("ATM", "convention", "", []string{"kind:convention", "type:bug"}, "human:alice")
	t2, _ := s.CreateTask("ATM", "Fix claim race", "", []string{"type:bug"}, "human:alice")
	t3, _ := s.CreateTask("ATM", "Blocked subtask", "", []string{"type:impl"}, "human:alice")
	_ = s.LinkAdd(t2.ID, "blocks", t3.ID, "human:alice")

	got, guide, err := s.Next("ATM", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected a task, got nil")
	}
	if got.ID != t2.ID {
		t.Fatalf("next = %q want %q (convention %q should be skipped, blocked %q skipped)", got.ID, t2.ID, conv.ID, t3.ID)
	}
	if guide != nil {
		t.Fatalf("expected nil guide, got %+v", guide)
	}
}

func TestNextEmptyReturnsNil(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{{Name: "type:impl"}}, nil, "human:alice")
	got, _, err := s.Next("ATM", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got.ID)
	}
}

func TestNextClaimSetsClaimAndHistory(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{{Name: "type:impl"}}, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "first", "", []string{"type:impl"}, "human:alice")
	got, _, err := s.Next("ATM", true, "agent:claude-1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Claim == nil {
		t.Fatalf("expected claimed task, got %+v", got)
	}
	if got.Claim.Actor != "agent:claude-1" {
		t.Fatalf("claim actor = %q", got.Claim.Actor)
	}
	last := got.History[len(got.History)-1]
	if last.Action != "claimed" {
		t.Fatalf("last history action = %q want claimed", last.Action)
	}
}

func TestNextClaimSkipsAlreadyClaimed(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{{Name: "type:impl"}}, nil, "human:alice")
	a, _ := s.CreateTask("ATM", "first", "", []string{"type:impl"}, "human:alice")
	b, _ := s.CreateTask("ATM", "second", "", []string{"type:impl"}, "human:alice")
	got1, _, err := s.Next("ATM", true, "agent:claude-1")
	if err != nil {
		t.Fatal(err)
	}
	if got1.ID != a.ID {
		t.Fatalf("first next = %q want %q", got1.ID, a.ID)
	}
	got2, _, err := s.Next("ATM", true, "agent:claude-2")
	if err != nil {
		t.Fatal(err)
	}
	if got2 == nil || got2.ID != b.ID {
		t.Fatalf("second next = %+v want %q", got2, b.ID)
	}
}

func TestNextRace(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{{Name: "type:impl"}}, nil, "human:alice")
	for i := 0; i < 4; i++ {
		_, _ = s.CreateTask("ATM", "task", "", []string{"type:impl"}, "human:alice")
	}

	var wg sync.WaitGroup
	results := make([]string, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			got, _, err := s.Next("ATM", true, "agent:bot")
			if err != nil {
				results[idx] = "err:" + err.Error()
				return
			}
			if got == nil {
				results[idx] = "nil"
				return
			}
			results[idx] = got.ID
		}(i)
	}
	wg.Wait()

	seen := map[string]int{}
	for _, r := range results {
		seen[r]++
	}
	for id, count := range seen {
		if id != "nil" && count > 1 {
			t.Fatalf("task %q claimed by more than one goroutine: %v", id, results)
		}
	}
	claimable := 0
	for _, r := range results {
		if r != "nil" && len(r) > 4 {
			claimable++
		}
	}
	if claimable == 0 {
		t.Fatalf("expected at least one claim, got %v", results)
	}
}

func TestClaimConflict(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{{Name: "type:impl"}}, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "first", "", []string{"type:impl"}, "human:alice")
	if _, err := s.Claim("ATM-0001", "agent:claude-1"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Claim("ATM-0001", "agent:claude-2")
	if !IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestUnclaimClearsClaim(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{{Name: "type:impl"}}, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "first", "", []string{"type:impl"}, "human:alice")
	_, _ = s.Claim("ATM-0001", "agent:claude-1")
	t1, err := s.Unclaim("ATM-0001", "agent:claude-1")
	if err != nil {
		t.Fatal(err)
	}
	if t1.Claim != nil {
		t.Fatalf("claim should be nil, got %+v", t1.Claim)
	}
	last := t1.History[len(t1.History)-1]
	if last.Action != "unclaimed" {
		t.Fatalf("last action = %q want unclaimed", last.Action)
	}
}
