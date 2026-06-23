package store

import (
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestProjectCreate(t *testing.T) {
	s := newTestStore(t)
	p, err := s.CreateProject("ATM", "Agent Tasks", "type", []Label{
		{Name: "type:impl"}, {Name: "area:cli"},
	}, []string{"/repo"}, "human:alice")
	if err != nil {
		t.Fatal(err)
	}
	if p.Code != "ATM" {
		t.Fatalf("code = %q", p.Code)
	}
	if p.NextTaskN != 1 {
		t.Fatalf("next_task_n = %d want 1", p.NextTaskN)
	}
	if len(p.Labels) != 2 {
		t.Fatalf("labels = %d want 2", len(p.Labels))
	}
	if p.CreatedBy != "human:alice" {
		t.Fatalf("created_by = %q", p.CreatedBy)
	}
	if p.TypeAxis != "type" {
		t.Fatalf("type_axis = %q", p.TypeAxis)
	}
}

func TestProjectCreateDuplicateConflict(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "first", "", nil, nil, "human:alice")
	_, err := s.CreateProject("ATM", "second", "", nil, nil, "human:alice")
	if !IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestProjectCreateInvalidCode(t *testing.T) {
	s := newTestStore(t)
	bad := []string{"", "lower", "A", "1ABC", "TOOLONG-CODE-HERE"}
	for _, code := range bad {
		_, err := s.CreateProject(code, "x", "", nil, nil, "human:alice")
		if err == nil {
			t.Errorf("expected error for code %q", code)
		}
	}
}

func TestProjectCreateInvalidActor(t *testing.T) {
	s := newTestStore(t)
	_, err := s.CreateProject("ATM", "x", "", nil, nil, "robot:marvin")
	if err == nil {
		t.Fatal("expected error for invalid actor")
	}
}

func TestSetTypeAxisRequiresLabel(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{{Name: "type:impl"}}, nil, "human:alice")
	err := s.SetTypeAxis("ATM", "kind", "human:alice")
	if !IsUsage(err) {
		t.Fatalf("expected usage error, got %v", err)
	}
	if err := s.SetTypeAxis("ATM", "type", "human:alice"); err != nil {
		t.Fatalf("set type axis: %v", err)
	}
	p, _ := s.GetProject("ATM")
	if p.TypeAxis != "type" {
		t.Fatalf("type_axis = %q want type", p.TypeAxis)
	}
}

func TestCreateTypeAxisRequiresLabelAtCreate(t *testing.T) {
	s := newTestStore(t)
	_, err := s.CreateProject("ATM", "x", "kind", []Label{{Name: "type:impl"}}, nil, "human:alice")
	if !IsUsage(err) {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestLabelRemoveSoftRetention(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{{Name: "type:impl"}, {Name: "area:cli"}}, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "task1", "", []string{"area:cli"}, "human:alice")
	_, _ = s.CreateTask("ATM", "task2", "", []string{"area:cli"}, "human:alice")

	res, err := s.LabelRemove("ATM", "area:cli", "human:alice")
	if err != nil {
		t.Fatal(err)
	}
	if res.RetainedUsage != 2 {
		t.Fatalf("retained_usage = %d want 2", res.RetainedUsage)
	}
	p, _ := s.GetProject("ATM")
	for _, l := range p.Labels {
		if l.Name == "area:cli" {
			t.Fatal("label not removed from project set")
		}
	}

	_, err = s.CreateTask("ATM", "task3", "", []string{"area:cli"}, "human:alice")
	if !IsUsage(err) {
		t.Fatalf("expected usage error assigning removed label, got %v", err)
	}
}

func TestLabelAddAndList(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	if err := s.LabelAdd("ATM", "type:epic", "Large body", "human:alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.LabelAdd("ATM", "type:bug", "", "human:alice"); err != nil {
		t.Fatal(err)
	}
	labels := s.LabelList("ATM")
	if len(labels) != 2 {
		t.Fatalf("got %d labels want 2", len(labels))
	}
	if err := s.LabelAdd("ATM", "type:bug", "Bug fix", "human:alice"); err != nil {
		t.Fatal(err)
	}
	labels = s.LabelList("ATM")
	for _, l := range labels {
		if l.Name == "type:bug" && l.Description != "Bug fix" {
			t.Fatalf("description not updated: %q", l.Description)
		}
	}
}

func TestProjectSetName(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "old", "", nil, nil, "human:alice")
	if err := s.SetProjectName("ATM", "new name", "human:alice"); err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProject("ATM")
	if p.Name != "new name" {
		t.Fatalf("name = %q", p.Name)
	}
}

func TestProjectRepoAddRemove(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	if err := s.RepoAdd("ATM", "/repo1", "human:alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.RepoAdd("ATM", "/repo2", "human:alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.RepoAdd("ATM", "/repo1", "human:alice"); err != nil {
		t.Fatal("duplicate add should be no-op")
	}
	p, _ := s.GetProject("ATM")
	if len(p.RepoPaths) != 2 {
		t.Fatalf("got %d repos want 2", len(p.RepoPaths))
	}
	if err := s.RepoRemove("ATM", "/repo1", "human:alice"); err != nil {
		t.Fatal(err)
	}
	p, _ = s.GetProject("ATM")
	if len(p.RepoPaths) != 1 || p.RepoPaths[0] != "/repo2" {
		t.Fatalf("repos = %v", p.RepoPaths)
	}
}

func TestListProjects(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ZZZ", "z", "", nil, nil, "human:alice")
	_, _ = s.CreateProject("AAA", "a", "", nil, nil, "human:alice")
	_, _ = s.CreateProject("MMM", "m", "", nil, nil, "human:alice")
	projects := s.ListProjects()
	if len(projects) != 3 {
		t.Fatalf("got %d want 3", len(projects))
	}
	if projects[0].Code != "AAA" || projects[1].Code != "MMM" || projects[2].Code != "ZZZ" {
		t.Fatalf("order = %s %s %s", projects[0].Code, projects[1].Code, projects[2].Code)
	}
}
