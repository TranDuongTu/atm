package store

import "testing"

func TestShowWithContextConventionsAndGuide(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "type", []Label{{Name: "type:impl"}, {Name: "type:bug"}, {Name: "kind:convention"}}, nil, "human:alice")
	conv, _ := s.CreateTask("ATM", "PR conventions for bug fixes", "", []string{"kind:convention", "type:bug"}, "human:alice")
	bug, _ := s.CreateTask("ATM", "Fix claim race", "", []string{"type:bug"}, "human:alice")

	res, err := s.ShowWithContext(bug.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Task.ID != bug.ID {
		t.Fatalf("task = %q want %q", res.Task.ID, bug.ID)
	}
	if len(res.Context.Conventions) != 1 {
		t.Fatalf("conventions = %d want 1", len(res.Context.Conventions))
	}
	if res.Context.Conventions[0].ID != conv.ID {
		t.Fatalf("convention = %q want %q", res.Context.Conventions[0].ID, conv.ID)
	}
	found := false
	for _, l := range res.Context.Conventions[0].MatchedLabels {
		if l == "type:bug" {
			found = true
		}
	}
	if !found {
		t.Fatalf("matched labels = %v want type:bug", res.Context.Conventions[0].MatchedLabels)
	}
	if res.Context.Guide != nil {
		t.Fatalf("guide should be nil, got %+v", res.Context.Guide)
	}
}

func TestShowWithContextLinksBothDirections(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "type", []Label{{Name: "type:impl"}, {Name: "type:bug"}, {Name: "kind:convention"}}, nil, "human:alice")
	a, _ := s.CreateTask("ATM", "epic", "", []string{"type:impl"}, "human:alice")
	b, _ := s.CreateTask("ATM", "impl", "", []string{"type:impl"}, "human:alice")
	_ = s.LinkAdd(b.ID, "implements", a.ID, "human:alice")

	res, err := s.ShowWithContext(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Context.LinksIn) == 0 {
		t.Fatalf("links_in should contain the implements edge, got %v", res.Context.LinksIn)
	}
	resB, err := s.ShowWithContext(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(resB.Context.LinksOut) == 0 {
		t.Fatalf("links_out should contain the implements edge, got %v", resB.Context.LinksOut)
	}
}

func TestShowWithContextTimelineSorted(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{{Name: "type:impl"}}, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "first", "", []string{"type:impl"}, "human:alice")
	_, _ = s.TodoAdd("ATM-0001", "write tests", "human:alice")
	_, _ = s.DiscussionAdd("ATM-0001", "discuss", "human:alice")

	res, err := s.ShowWithContext("ATM-0001")
	if err != nil {
		t.Fatal(err)
	}
	tl := res.Context.Timeline
	if len(tl) < 3 {
		t.Fatalf("timeline len = %d want >=3", len(tl))
	}
	for i := 1; i < len(tl); i++ {
		if tl[i].At.Before(tl[i-1].At) {
			t.Fatalf("timeline not sorted at %d", i)
		}
	}
}

func TestShowWithContextTypeAxisMatchFirst(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "type", []Label{{Name: "type:bug"}, {Name: "type:impl"}, {Name: "area:cli"}, {Name: "kind:convention"}}, nil, "human:alice")
	typeConv, _ := s.CreateTask("ATM", "bug convention", "", []string{"kind:convention", "type:bug"}, "human:alice")
	areaConv, _ := s.CreateTask("ATM", "cli convention", "", []string{"kind:convention", "area:cli"}, "human:alice")
	bug, _ := s.CreateTask("ATM", "Fix bug", "", []string{"type:bug", "area:cli"}, "human:alice")

	res, err := s.ShowWithContext(bug.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Context.Conventions) != 2 {
		t.Fatalf("conventions = %d want 2", len(res.Context.Conventions))
	}
	if res.Context.Conventions[0].ID != typeConv.ID {
		t.Fatalf("first convention = %q want %q (type-axis match first)", res.Context.Conventions[0].ID, typeConv.ID)
	}
	if res.Context.Conventions[1].ID != areaConv.ID {
		t.Fatalf("second convention = %q want %q", res.Context.Conventions[1].ID, areaConv.ID)
	}
}
