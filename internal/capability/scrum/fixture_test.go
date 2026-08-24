package scrum

import (
	"path/filepath"
	"testing"

	"atm/internal/store"
)

const testActor = "admin@cli:unset"

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "atm"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Atm", testActor); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := EnsureVocabulary(s, "ATM", testActor); err != nil {
		t.Fatalf("ensure vocabulary: %v", err)
	}
	return s
}

func newRecorder(s *store.Store) *Recorder {
	return &Recorder{Store: s, Actor: testActor}
}

func labelsOf(t *testing.T, s *store.Store, id string) []string {
	t.Helper()
	tk, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask %s: %v", id, err)
	}
	return tk.Labels
}

func payloadOf(t *testing.T, s *store.Store, id string) *Payload {
	t.Helper()
	tk, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask %s: %v", id, err)
	}
	pl, err := DecodePayload(tk.Meta[CapabilityName])
	if err != nil {
		t.Fatalf("decode payload %s: %v", id, err)
	}
	return pl
}
