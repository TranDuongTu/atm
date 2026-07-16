package eventsync

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestDirFetchAbsent(t *testing.T) {
	root := t.TempDir()
	target := NewDirTarget(root)

	snap, err := target.Fetch(context.Background(), "PRJ")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !snap.Absent {
		t.Errorf("Absent = false, want true")
	}
}

func TestDirFetchReadsEvents(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	task := mustTask(t, clock, replicaA, []string{root.ID}, "task")

	dir := t.TempDir()
	path := filepath.Join(dir, "PRJ", "events.v2.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	buf.Write(root.Raw)
	buf.WriteByte('\n')
	buf.Write(task.Raw)
	buf.WriteByte('\n')
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	target := NewDirTarget(dir)
	snap, err := target.Fetch(context.Background(), "PRJ")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Absent {
		t.Fatalf("Absent = true, want false")
	}
	if len(snap.Events) != 2 {
		t.Fatalf("Events = %v, want 2", rawEventIDs(snap.Events))
	}
	if snap.Events[0].ID != root.ID || snap.Events[1].ID != task.ID {
		t.Errorf("Events = %v, want [%s, %s]", rawEventIDs(snap.Events), root.ID, task.ID)
	}
	wantDigest := SetDigest([]string{root.ID, task.ID})
	if snap.Digest != wantDigest {
		t.Errorf("Digest = %q, want %q", snap.Digest, wantDigest)
	}
}

func TestDirFetchSkipsTornTailLine(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	task := mustTask(t, clock, replicaA, []string{root.ID}, "task")

	dir := t.TempDir()
	path := filepath.Join(dir, "PRJ", "events.v2.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	buf.Write(root.Raw)
	buf.WriteByte('\n')
	buf.Write(task.Raw) // torn: no trailing newline, as if the write was interrupted
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	target := NewDirTarget(dir)
	snap, err := target.Fetch(context.Background(), "PRJ")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(snap.Events) != 1 || snap.Events[0].ID != root.ID {
		t.Errorf("Events = %v, want only [%s] (torn tail skipped)", rawEventIDs(snap.Events), root.ID)
	}
}

func TestDirPublishAppendsOnly(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "first")
	e2 := mustTask(t, clock, replicaA, []string{root.ID}, "second")

	dir := t.TempDir()
	target := NewDirTarget(dir)
	ctx := context.Background()

	if err := target.Publish(ctx, "PRJ", rawOf(root, e1), nil); err != nil {
		t.Fatalf("Publish (1): %v", err)
	}
	path := filepath.Join(dir, "PRJ", "events.v2.jsonl")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := target.Publish(ctx, "PRJ", rawOf(e2), nil); err != nil {
		t.Fatalf("Publish (2): %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.HasPrefix(after, before) {
		t.Fatalf("old bytes are not a prefix of new bytes:\nbefore=%q\nafter=%q", before, after)
	}
}

func TestDirPublishCreatesProjectDir(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")

	dir := t.TempDir()
	target := NewDirTarget(dir)

	if err := target.Publish(context.Background(), "PRJ", rawOf(root), nil); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	path := filepath.Join(dir, "PRJ", "events.v2.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := append(append([]byte(nil), root.Raw...), '\n')
	if !bytes.Equal(data, want) {
		t.Errorf("file contents = %q, want %q", data, want)
	}
}

func TestDirConcurrentPublishInterleavesWholeLines(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")

	dir := t.TempDir()
	target := NewDirTarget(dir)
	ctx := context.Background()
	if err := target.Publish(ctx, "PRJ", rawOf(root), nil); err != nil {
		t.Fatalf("seed Publish: %v", err)
	}

	const n = 50
	batchA := make([]RawEvent, n)
	batchB := make([]RawEvent, n)
	wantIDs := map[string]bool{root.ID: true}
	for i := 0; i < n; i++ {
		ea := mustTask(t, clock, replicaA, []string{root.ID}, fmt.Sprintf("a%d", i))
		eb := mustTask(t, clock, replicaB, []string{root.ID}, fmt.Sprintf("b%d", i))
		batchA[i] = RawEvent{ID: ea.ID, Raw: ea.Raw}
		batchB[i] = RawEvent{ID: eb.ID, Raw: eb.Raw}
		wantIDs[ea.ID] = true
		wantIDs[eb.ID] = true
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); target.Publish(ctx, "PRJ", batchA, nil) }()
	go func() { defer wg.Done(); target.Publish(ctx, "PRJ", batchB, nil) }()
	wg.Wait()

	snap, err := target.Fetch(ctx, "PRJ")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(snap.Events) != len(wantIDs) {
		t.Fatalf("Events count = %d, want %d (whole lines must survive interleaving, not torn ones)", len(snap.Events), len(wantIDs))
	}
	got := make(map[string]bool, len(snap.Events))
	for _, ev := range snap.Events {
		got[ev.ID] = true
	}
	for id := range wantIDs {
		if !got[id] {
			t.Errorf("missing event %s from the union", id)
		}
	}
}

func TestDirDuplicateLinesHarmless(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	task := mustTask(t, clock, replicaA, []string{root.ID}, "task")

	dir := t.TempDir()
	target := NewDirTarget(dir)
	ctx := context.Background()

	events := rawOf(root, task)
	if err := target.Publish(ctx, "PRJ", events, nil); err != nil {
		t.Fatalf("Publish (1): %v", err)
	}
	if err := target.Publish(ctx, "PRJ", events, nil); err != nil {
		t.Fatalf("Publish (2): %v", err)
	}

	snap, err := target.Fetch(ctx, "PRJ")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(snap.Events) != 2 {
		t.Errorf("Events = %v, want 2 (deduped by id)", rawEventIDs(snap.Events))
	}
}
