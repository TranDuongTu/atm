package eventlog

import (
	"path/filepath"
	"testing"
)

func TestEventsourcePaths(t *testing.T) {
	root := t.TempDir()
	e := New(root, Options{})
	if got, want := e.EventsV2Path("ATM"), filepath.Join(root, "projects", "ATM", "events.v2.jsonl"); got != want {
		t.Fatalf("EventsV2Path = %q, want %q", got, want)
	}
	if got, want := e.eventsourceMetaPath("ATM"), filepath.Join(root, "projects", "ATM", "eventsource.json"); got != want {
		t.Fatalf("eventsourceMetaPath = %q, want %q", got, want)
	}
	if got, want := e.storeMetaPath(), filepath.Join(root, "store.json"); got != want {
		t.Fatalf("storeMetaPath = %q, want %q", got, want)
	}
}
