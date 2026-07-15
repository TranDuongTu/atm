package store

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// copyTree recursively copies src to dst, preserving the directory
// structure. It is used to simulate a naive directory copy (cp -r, rsync,
// tar extract-to-new-path) of a store root for TestCopiedStoreRemintsReplicaBeforeWrite.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func TestCopiedStoreRemintsReplicaBeforeWrite(t *testing.T) {
	original := testStore(t)
	_, _ = original.CreateProject("ATM", "x", "admin@cli:unset")
	first, err := original.ensureReplicaForWriteLocked()
	if err != nil {
		t.Fatal(err)
	}
	copyDir := filepath.Join(t.TempDir(), "copy")
	if err := copyTree(original.StorePath(), copyDir); err != nil {
		t.Fatal(err)
	}
	copied, err := Open(copyDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := copied.ensureReplicaForWriteLocked()
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatalf("copied store kept replica id %s", first)
	}
}

// TestUncopiedStoreKeepsReplicaAcrossWrites is the false-positive guard: a
// store that is never copied must keep the SAME replica id across repeated
// calls to ensureReplicaForWriteLocked (i.e. across repeated event
// authoring), even though every call re-checks the copy marker.
func TestUncopiedStoreKeepsReplicaAcrossWrites(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	first, err := s.ensureReplicaForWriteLocked()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		next, err := s.ensureReplicaForWriteLocked()
		if err != nil {
			t.Fatal(err)
		}
		if next != first {
			t.Fatalf("replica id changed on an uncopied store: %s -> %s", first, next)
		}
	}
}

// TestCopiedStoreDoesNotLoseExistingEvents guards the corruption invariant:
// re-minting the replica id for future writes must never touch events
// already appended by the original instance. The copy's events.v2.jsonl
// must be byte-identical to the original's at copy time, and a subsequent
// write on the copy must extend (not rewrite) that file.
func TestCopiedStoreDoesNotLoseExistingEvents(t *testing.T) {
	original := testStore(t)
	_, _ = original.CreateProject("ATM", "x", "admin@cli:unset")
	if _, _, err := original.appendV2TaskCreatedLocked("ATM", "first task", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	beforeCopy, err := os.ReadFile(original.eventsV2Path("ATM"))
	if err != nil {
		t.Fatal(err)
	}

	copyDir := filepath.Join(t.TempDir(), "copy")
	if err := copyTree(original.StorePath(), copyDir); err != nil {
		t.Fatal(err)
	}
	copied, err := Open(copyDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := copied.appendV2TaskCreatedLocked("ATM", "second task", "", nil, testActor); err != nil {
		t.Fatal(err)
	}

	afterAppend, err := os.ReadFile(copied.eventsV2Path("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	if len(afterAppend) < len(beforeCopy) || string(afterAppend[:len(beforeCopy)]) != string(beforeCopy) {
		t.Fatalf("copy's event log does not extend the original's byte-for-byte: original=%q, after=%q", beforeCopy, afterAppend)
	}

	// The original instance's own future writes must be unaffected by the
	// copy's re-mint: it keeps authoring under its own (unchanged) replica.
	origAfter, err := original.ensureReplicaForWriteLocked()
	if err != nil {
		t.Fatal(err)
	}
	copiedReplica, err := copied.ensureReplicaForWriteLocked()
	if err != nil {
		t.Fatal(err)
	}
	if origAfter == copiedReplica {
		t.Fatalf("original and copy converged on the same replica id %s after copy+append", origAfter)
	}
}
