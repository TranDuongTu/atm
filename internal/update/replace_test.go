package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceBinary(t *testing.T) {
	target := filepath.Join(t.TempDir(), "atm")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceBinary(target, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("target bytes = %q", got)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestReplaceBinaryMissingDirectory(t *testing.T) {
	err := ReplaceBinary(filepath.Join(t.TempDir(), "missing", "atm"), []byte("new"))
	if err == nil {
		t.Fatal("expected error")
	}
}
