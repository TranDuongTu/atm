package store

import "testing"

func TestRequestReviewTransition(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t", "", nil, "human:alice")
	if err := s.SetStatus("ATM-0001", "in-progress", "human:alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestReview("ATM-0001", "human:alice"); err != nil {
		t.Fatal(err)
	}
	tt, err := s.GetTask("ATM-0001")
	if err != nil {
		t.Fatal(err)
	}
	if tt.Status != "review" {
		t.Fatalf("status = %q want review", tt.Status)
	}
	foundReq := false
	for _, h := range tt.History {
		if h.Action == "review-requested" {
			foundReq = true
		}
	}
	if !foundReq {
		t.Fatal("missing review-requested history entry")
	}
}

func TestApproveReviewTransitionAndComment(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t", "", nil, "human:alice")
	if err := s.SetStatus("ATM-0001", "in-progress", "human:alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestReview("ATM-0001", "human:alice"); err != nil {
		t.Fatal(err)
	}
	tt, err := s.ApproveReview("ATM-0001", "human:alice", "looks good")
	if err != nil {
		t.Fatal(err)
	}
	if tt.Status != "done" {
		t.Fatalf("status = %q want done", tt.Status)
	}
	if len(tt.Discussions) != 1 {
		t.Fatalf("discussions = %d want 1", len(tt.Discussions))
	}
	if tt.Discussions[0].Text != "looks good" {
		t.Fatalf("discussion text = %q", tt.Discussions[0].Text)
	}
	if tt.Discussions[0].Author != "human:alice" {
		t.Fatalf("discussion author = %q", tt.Discussions[0].Author)
	}
	foundApproved := false
	for _, h := range tt.History {
		if h.Action == "approved" && h.Actor == "human:alice" {
			foundApproved = true
		}
	}
	if !foundApproved {
		t.Fatal("missing approved history entry by human:alice")
	}
}

func TestRejectReviewTransitionAndComment(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t", "", nil, "human:alice")
	if err := s.SetStatus("ATM-0001", "in-progress", "human:alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestReview("ATM-0001", "human:alice"); err != nil {
		t.Fatal(err)
	}
	tt, err := s.RejectReview("ATM-0001", "human:alice", "please fix")
	if err != nil {
		t.Fatal(err)
	}
	if tt.Status != "in-progress" {
		t.Fatalf("status = %q want in-progress", tt.Status)
	}
	if len(tt.Discussions) != 1 {
		t.Fatalf("discussions = %d want 1", len(tt.Discussions))
	}
	if tt.Discussions[0].Text != "please fix" {
		t.Fatalf("discussion text = %q", tt.Discussions[0].Text)
	}
	foundRejected := false
	for _, h := range tt.History {
		if h.Action == "rejected" && h.Actor == "human:alice" {
			foundRejected = true
		}
	}
	if !foundRejected {
		t.Fatal("missing rejected history entry by human:alice")
	}
}

func TestReviewQueueGroupedByClaimant(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t1", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t2", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t3", "", nil, "human:alice")
	if _, err := s.Claim("ATM-0001", "agent:claude-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim("ATM-0002", "agent:claude-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim("ATM-0003", "agent:claude-1"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ATM-0001", "ATM-0002", "ATM-0003"} {
		if err := s.SetStatus(id, "in-progress", "human:alice"); err != nil {
			t.Fatal(err)
		}
		if err := s.RequestReview(id, "human:alice"); err != nil {
			t.Fatal(err)
		}
	}
	res, err := s.ReviewQueue("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("groups = %d want 2", len(res.Groups))
	}
	if res.Groups[0].Claimant != "agent:claude-1" {
		t.Fatalf("group 0 claimant = %q want agent:claude-1", res.Groups[0].Claimant)
	}
	if len(res.Groups[0].Tasks) != 2 {
		t.Fatalf("group 0 tasks = %d want 2", len(res.Groups[0].Tasks))
	}
	if res.Groups[0].Tasks[0].ID != "ATM-0001" {
		t.Fatalf("group 0 task 0 id = %q want ATM-0001", res.Groups[0].Tasks[0].ID)
	}
	if res.Groups[1].Claimant != "agent:claude-2" {
		t.Fatalf("group 1 claimant = %q want agent:claude-2", res.Groups[1].Claimant)
	}
	if len(res.Groups[1].Tasks) != 1 {
		t.Fatalf("group 1 tasks = %d want 1", len(res.Groups[1].Tasks))
	}
}

func TestReviewQueueEmptyProject(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	res, err := s.ReviewQueue("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 0 {
		t.Fatalf("groups = %d want 0", len(res.Groups))
	}
}

func TestOpenFollowups(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t1", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t2", "", nil, "human:alice")
	if _, err := s.FollowupAdd("ATM-0001", "open fu", "human:alice", "human:alice", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FollowupAdd("ATM-0002", "resolved fu", "human:alice", "human:alice", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FollowupResolve("ATM-0002", "f1", "human:alice"); err != nil {
		t.Fatal(err)
	}
	out, err := s.OpenFollowups("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("open followups = %d want 1", len(out))
	}
	if out[0].ID != "ATM-0001" || out[0].Followup != "f1" {
		t.Fatalf("got %+v", out[0])
	}
}

func TestDashboardComposition(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t", "", nil, "human:alice")
	if _, err := s.Claim("ATM-0001", "agent:claude-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus("ATM-0001", "in-progress", "human:alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestReview("ATM-0001", "human:alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FollowupAdd("ATM-0001", "fu", "human:alice", "human:alice", nil); err != nil {
		t.Fatal(err)
	}
	d, err := s.Dashboard("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if d.Project != "ATM" {
		t.Fatalf("project = %q want ATM", d.Project)
	}
	if len(d.ReviewQueue.Groups) != 1 {
		t.Fatalf("review_queue groups = %d want 1", len(d.ReviewQueue.Groups))
	}
	if len(d.OpenFollowups) != 1 {
		t.Fatalf("open_followups = %d want 1", len(d.OpenFollowups))
	}
	if d.GuideStatus == nil {
		t.Fatal("guide_status is nil")
	}
}
