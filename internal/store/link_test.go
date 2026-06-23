package store

import "testing"

func TestLinkAddAndList(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t1", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t2", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t3", "", nil, "human:alice")

	if err := s.LinkAdd("ATM-0001", "blocks", "ATM-0002", "human:alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkAdd("ATM-0001", "implements", "ATM-0003", "human:alice"); err != nil {
		t.Fatal(err)
	}

	res, err := s.LinkList("ATM-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Out) != 2 {
		t.Fatalf("out edges = %d want 2", len(res.Out))
	}
	if len(res.In) != 0 {
		t.Fatalf("in edges = %d want 0", len(res.In))
	}

	res2, err := s.LinkList("ATM-0002")
	if err != nil {
		t.Fatal(err)
	}
	hasBlockedBy := false
	for _, e := range res2.In {
		if e.Link.Type == "blocked-by" && e.Link.Target == "ATM-0001" {
			hasBlockedBy = true
		}
	}
	if !hasBlockedBy {
		t.Fatalf("expected implied blocked-by in edge for ATM-0002, got %+v", res2.In)
	}
}

func TestLinkInvalidType(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t1", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t2", "", nil, "human:alice")
	if err := s.LinkAdd("ATM-0001", "blocked-by", "ATM-0002", "human:alice"); !IsUsage(err) {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestLinkSelfRejected(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t1", "", nil, "human:alice")
	if err := s.LinkAdd("ATM-0001", "related-to", "ATM-0001", "human:alice"); !IsUsage(err) {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestLinkRelatedToSymmetricDedup(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t1", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t2", "", nil, "human:alice")

	if err := s.LinkAdd("ATM-0001", "related-to", "ATM-0002", "human:alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkAdd("ATM-0002", "related-to", "ATM-0001", "human:alice"); err != nil {
		t.Fatal(err)
	}
	t2, _ := s.GetTask("ATM-0002")
	if len(t2.Links) != 0 {
		t.Fatalf("related-to should dedup symmetric, got %v", t2.Links)
	}

	res, _ := s.LinkList("ATM-0002")
	found := false
	for _, e := range res.In {
		if e.Link.Type == "related-to" && e.Link.Target == "ATM-0001" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected related-to in edge, got none")
	}
}

func TestLinkRemove(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t1", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t2", "", nil, "human:alice")

	_ = s.LinkAdd("ATM-0001", "blocks", "ATM-0002", "human:alice")
	if err := s.LinkRemove("ATM-0001", "blocks", "ATM-0002", "human:alice"); err != nil {
		t.Fatal(err)
	}
	t1, _ := s.GetTask("ATM-0001")
	if len(t1.Links) != 0 {
		t.Fatalf("links = %v want empty", t1.Links)
	}
}

func TestLinkStaleTargetPreserved(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t1", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t2", "", nil, "human:alice")

	_ = s.LinkAdd("ATM-0001", "documents", "ATM-0002", "human:alice")
	_ = s.SetStatus("ATM-0002", "cancelled", "human:alice")
	_ = s.SetStatus("ATM-0002", "open", "human:alice")
	t1, _ := s.GetTask("ATM-0001")
	if len(t1.Links) != 1 {
		t.Fatalf("stale link should be preserved, got %v", t1.Links)
	}
}
