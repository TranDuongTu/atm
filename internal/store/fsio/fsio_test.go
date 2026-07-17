package fsio

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWithLockNestsDifferentNames pins the one nesting rule the store relies
// on (project lock -> store-meta lock): DIFFERENT names nest, and the
// registry survives sequential re-acquisition of the same name.
func TestWithLockNestsDifferentNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects")
	ran := false
	err := WithLock(dir, "ABC", func() error {
		return WithLock(dir, "store-meta", func() error {
			ran = true
			return nil
		})
	})
	if err != nil || !ran {
		t.Fatalf("nested WithLock: err=%v ran=%v", err, ran)
	}
	if err := WithLock(dir, "ABC", func() error { return nil }); err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ABC.lock")); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}
}

func TestJSONRoundTripAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "x.json")
	if err := WriteJSON(path, map[string]any{"b": 2, "a": 1}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var got map[string]any
	if err := ReadJSON(path, &got); err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}
