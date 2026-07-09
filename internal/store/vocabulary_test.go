package store

import (
	"os"
	"strings"
	"testing"
)

func TestGetVocabularyMissingFileReturnsNilNil(t *testing.T) {
	s := openTempStore(t)
	got, err := s.GetVocabulary("FOO")
	if err != nil || got != nil {
		t.Fatalf("GetVocabulary(missing) = (%v, %v), want (nil, nil)", got, err)
	}
}

func TestWriteVocabularyRoundTrips(t *testing.T) {
	s := openTempStore(t)
	if _, err := s.CreateProject("FOO", "Foo", testActor); err != nil {
		t.Fatal(err)
	}
	in := &Vocabulary{
		Actor: testActor,
		Terms: []VocabularyTerm{
			{Term: "labels", Weight: 9},
			{Term: "audit log", Weight: 7},
		},
	}
	if err := s.WriteVocabulary("FOO", in); err != nil {
		t.Fatalf("WriteVocabulary: %v", err)
	}
	got, err := s.GetVocabulary("FOO")
	if err != nil || got == nil {
		t.Fatalf("GetVocabulary after write = (%v, %v), want non-nil", got, err)
	}
	if got.Actor != in.Actor {
		t.Errorf("Actor = %q, want %q", got.Actor, in.Actor)
	}
	if len(got.Terms) != 2 || got.Terms[0].Term != "labels" || got.Terms[0].Weight != 9 {
		t.Errorf("Terms = %#v, want [{labels 9} {audit log 7}]", got.Terms)
	}
	if got.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt should be stamped by WriteVocabulary")
	}
}

func TestWriteVocabularyRequiresActor(t *testing.T) {
	s := openTempStore(t)
	if _, err := s.CreateProject("FOO", "Foo", testActor); err != nil {
		t.Fatal(err)
	}
	err := s.WriteVocabulary("FOO", &Vocabulary{Actor: "", Terms: []VocabularyTerm{{Term: "x", Weight: 1}}})
	if err == nil || !strings.Contains(err.Error(), "actor") {
		t.Fatalf("WriteVocabulary(empty actor) err = %v, want actor-required error", err)
	}
}

func TestGetVocabularyMalformedJSONReturnsError(t *testing.T) {
	s := openTempStore(t)
	if _, err := s.CreateProject("FOO", "Foo", testActor); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.projectDir("FOO"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.vocabularyPath("FOO"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetVocabulary("FOO")
	if err == nil {
		t.Fatalf("GetVocabulary(malformed) = (%v, nil), want error", got)
	}
}

// openTempStore builds an initialized store in a temp dir.
func openTempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	return s
}
