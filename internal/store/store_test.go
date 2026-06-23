package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStorePath(t *testing.T) {
	t.Run("flag wins", func(t *testing.T) {
		t.Setenv("ATM_HOME", "/env/path")
		if got := ResolveStorePath("/flag/path"); got != "/flag/path" {
			t.Fatalf("got %q want /flag/path", got)
		}
	})
	t.Run("env used when flag empty", func(t *testing.T) {
		t.Setenv("ATM_HOME", "/env/path")
		if got := ResolveStorePath(""); got != "/env/path" {
			t.Fatalf("got %q want /env/path", got)
		}
	})
	t.Run("default when both empty", func(t *testing.T) {
		t.Setenv("ATM_HOME", "")
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".config", "atm")
		if got := ResolveStorePath(""); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})
}

func TestInitIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "projects")); err != nil {
		t.Fatalf("projects dir missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "actors.json")); err != nil {
		t.Fatalf("actors.json missing: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("second init should be idempotent: %v", err)
	}
}

func TestInitWithStorePath(t *testing.T) {
	root := t.TempDir()
	s := &Store{}
	target := filepath.Join(root, "custom")
	if err := s.Init(target); err != nil {
		t.Fatal(err)
	}
	if s.Root != target {
		t.Fatalf("Root = %q want %q", s.Root, target)
	}
	if _, err := os.Stat(filepath.Join(target, "projects")); err != nil {
		t.Fatalf("projects dir missing: %v", err)
	}
}

func TestStorePath(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	if got := s.StorePath(); got != dir {
		t.Fatalf("StorePath = %q want %q", got, dir)
	}
}

func TestOpenUsesEnvWhenFlagEmpty(t *testing.T) {
	t.Setenv("ATM_HOME", "/tmp/atm-env-test")
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Init("")
	defer os.RemoveAll("/tmp/atm-env-test")
	if s.Root != "/tmp/atm-env-test" {
		t.Fatalf("Root = %q want /tmp/atm-env-test", s.Root)
	}
}
