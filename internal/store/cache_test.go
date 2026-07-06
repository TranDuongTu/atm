package store

import "testing"

func TestCacheProjectUpsertGetRoundTrip(t *testing.T) {
	s := newTestStore(t)
	db, err := s.cacheDB()
	if err != nil {
		t.Fatal(err)
	}
	now := Now()
	p := &Project{Code: "ATM", Name: "x", NextTaskN: 3, LogSeq: 5, CreatedAt: now, CreatedBy: "c", UpdatedAt: now, UpdatedBy: "c"}
	if err := cacheUpsertProject(db, p); err != nil {
		t.Fatal(err)
	}
	got, ok, err := cacheGetProject(db, "ATM")
	if err != nil || !ok {
		t.Fatalf("cacheGetProject: ok=%v err=%v", ok, err)
	}
	if got.NextTaskN != 3 || got.LogSeq != 5 {
		t.Fatalf("got = %+v", got)
	}
}

func TestCacheProjectGetMissing(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	_, ok, err := cacheGetProject(db, "NOPE")
	if err != nil || ok {
		t.Fatalf("expected ok=false err=nil, got ok=%v err=%v", ok, err)
	}
}

func TestCacheProjectUpsertOverwrites(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	p := &Project{Code: "ATM", Name: "x", NextTaskN: 1, LogSeq: 1, CreatedAt: now, CreatedBy: "c", UpdatedAt: now, UpdatedBy: "c"}
	_ = cacheUpsertProject(db, p)
	p.NextTaskN = 9
	p.LogSeq = 9
	_ = cacheUpsertProject(db, p)
	got, _, _ := cacheGetProject(db, "ATM")
	if got.NextTaskN != 9 || got.LogSeq != 9 {
		t.Fatalf("upsert did not overwrite: %+v", got)
	}
}

func TestCacheDeleteProject(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	_ = cacheUpsertProject(db, &Project{Code: "ATM", Name: "x", CreatedAt: now, UpdatedAt: now})
	if err := cacheDeleteProject(db, "ATM"); err != nil {
		t.Fatal(err)
	}
	_, ok, _ := cacheGetProject(db, "ATM")
	if ok {
		t.Fatal("project row still present after delete")
	}
}

func TestCacheListProjectCodesSorted(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	_ = cacheUpsertProject(db, &Project{Code: "ZZZ", CreatedAt: now, UpdatedAt: now})
	_ = cacheUpsertProject(db, &Project{Code: "AAA", CreatedAt: now, UpdatedAt: now})
	codes, err := cacheListProjectCodes(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 2 || codes[0] != "AAA" || codes[1] != "ZZZ" {
		t.Fatalf("codes = %v", codes)
	}
}

func TestCacheTaskUpsertGetRoundTripWithLabels(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	tk := &Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t", Labels: []string{"ATM:type:bug", "ATM:status:open"},
		LogSeq: 3, CreatedAt: now, CreatedBy: "c", UpdatedAt: now, UpdatedBy: "c"}
	if err := cacheUpsertTask(db, tk); err != nil {
		t.Fatal(err)
	}
	got, ok, err := cacheGetTask(db, "ATM-0001")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(got.Labels) != 2 || got.Labels[0] != "ATM:status:open" || got.Labels[1] != "ATM:type:bug" {
		t.Fatalf("labels = %v", got.Labels)
	}
}

func TestCacheTaskUpsertReplacesLabels(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	tk := &Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t", Labels: []string{"ATM:type:bug"}, CreatedAt: now, UpdatedAt: now}
	_ = cacheUpsertTask(db, tk)
	tk.Labels = []string{"ATM:status:open"}
	_ = cacheUpsertTask(db, tk)
	got, _, _ := cacheGetTask(db, "ATM-0001")
	if len(got.Labels) != 1 || got.Labels[0] != "ATM:status:open" {
		t.Fatalf("labels not replaced: %v", got.Labels)
	}
}

func TestCacheDeleteTaskRemovesLabels(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	tk := &Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t", Labels: []string{"ATM:type:bug"}, CreatedAt: now, UpdatedAt: now}
	_ = cacheUpsertTask(db, tk)
	if err := cacheDeleteTask(db, "ATM-0001"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cacheGetTask(db, "ATM-0001"); ok {
		t.Fatal("task row still present")
	}
	labels, _ := cacheTaskLabels(db, "ATM-0001")
	if len(labels) != 0 {
		t.Fatalf("task_labels rows not cleaned up: %v", labels)
	}
}

func TestCacheListTaskIDsScopedByProjectAndSorted(t *testing.T) {
	s := newTestStore(t)
	db, _ := s.cacheDB()
	now := Now()
	_ = cacheUpsertTask(db, &Task{ID: "ATM-0002", ProjectCode: "ATM", Title: "b", CreatedAt: now, UpdatedAt: now})
	_ = cacheUpsertTask(db, &Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "a", CreatedAt: now, UpdatedAt: now})
	_ = cacheUpsertTask(db, &Task{ID: "OTH-0001", ProjectCode: "OTH", Title: "c", CreatedAt: now, UpdatedAt: now})
	ids, err := cacheListTaskIDs(db, "ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "ATM-0001" || ids[1] != "ATM-0002" {
		t.Fatalf("ids = %v", ids)
	}
}
