package qa

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
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

// ledgerDigest hashes the store's DURABLE ledger: the per-project event logs
// and the store manifest. Every mutation is an appended event, so a reader can
// be pinned as a reader by running this either side of it. The derived read
// cache is excluded — SQLite touches its sidecars on a pure read.
func ledgerDigest(t *testing.T, s *store.Store) string {
	t.Helper()
	h := sha256.New()
	root := s.StorePath()
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if filepath.Ext(rel) != ".jsonl" && rel != "store.json" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s:%d:", rel, len(b))
		h.Write(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walk store: %v", err)
	}
	return hex.EncodeToString(h.Sum(nil))
}
