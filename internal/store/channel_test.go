// internal/store/channel_test.go
package store

import (
	"errors"
	"testing"

	"atm/internal/core"
)

const chActor = "developer@test:unit"

func TestChannelCreateListEditRemove(t *testing.T) {
	s := newTestStore(t) // reuse this package's existing test-store helper
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	rec := core.ChannelRecord{Name: "specs", Type: core.ChannelTypeNotion, Purpose: "specs and plans live here", Address: core.ChannelAddress{Workspace: "acme", Database: "abc123"}}
	tk, err := s.CreateChannel("ATM", rec, chActor)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Title != "specs" {
		t.Fatalf("task title = %q, want handle", tk.Title)
	}
	got, err := s.ChannelRecords("ATM")
	if err != nil || len(got) != 1 {
		t.Fatalf("records: %v %v", got, err)
	}
	if got[0].Name != "specs" || got[0].Type != core.ChannelTypeNotion || got[0].Purpose != "specs and plans live here" {
		t.Fatalf("got %+v", got[0])
	}
	// duplicate handle rejected, across ALL types
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "specs", Type: core.ChannelTypeRepo}, chActor); !errors.Is(err, core.ErrUsage) {
		t.Fatalf("dup: %v", err)
	}
	// unknown type rejected
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "x", Type: "slack"}, chActor); !errors.Is(err, core.ErrUsage) {
		t.Fatalf("type: %v", err)
	}
	// edit purpose + address
	p := "moved purpose"
	if err := s.EditChannel("ATM", "specs", &p, &core.ChannelAddress{Workspace: "acme", Database: "zzz"}, chActor); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ChannelRecords("ATM")
	if got[0].Purpose != "moved purpose" || got[0].Address.Database != "zzz" {
		t.Fatalf("after edit: %+v", got[0])
	}
	// remove
	if err := s.RemoveChannel("ATM", "specs", chActor); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ChannelRecords("ATM"); len(got) != 0 {
		t.Fatalf("after remove: %v", got)
	}
	if err := s.RemoveChannel("ATM", "specs", chActor); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("remove missing: %v", err)
	}
}

func TestChannelEditPreservesUnknownPayloadFields(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "code", Type: core.ChannelTypeRepo, Address: core.ChannelAddress{URL: "git@x:y.git"}}, chActor)
	if err != nil {
		t.Fatal(err)
	}
	// simulate a newer binary's field
	m, _ := core.DecodeChannelPayload(mustTask(t, s, tk.ID).Meta[core.ChannelMetaKey])
	m["future_field"] = "kept"
	enc, _ := core.EncodeChannelPayload(m)
	if err := s.SetTaskCapabilityMeta(tk.ID, core.ChannelMetaKey, enc, chActor); err != nil {
		t.Fatal(err)
	}
	if err := s.EditChannel("ATM", "code", nil, &core.ChannelAddress{URL: "git@x:z.git"}, chActor); err != nil {
		t.Fatal(err)
	}
	m2, _ := core.DecodeChannelPayload(mustTask(t, s, tk.ID).Meta[core.ChannelMetaKey])
	if m2["future_field"] != "kept" {
		t.Fatal("edit dropped an unknown payload field")
	}
}

// One hand-corrupted record must not disable every OTHER channel's verbs.
func TestChannelCorruptRecordDoesNotPoisonNeighbours(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	bad, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "broken", Type: core.ChannelTypeRepo}, chActor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "good", Type: core.ChannelTypeNotion}, chActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskCapabilityMeta(bad.ID, core.ChannelMetaKey, "garbage", chActor); err != nil {
		t.Fatal(err)
	}
	// the healthy neighbour still resolves and edits
	p := "still works"
	if err := s.EditChannel("ATM", "good", &p, nil, chActor); err != nil {
		t.Fatalf("corrupt neighbour poisoned lookup: %v", err)
	}
	// the broken one is reachable by title and reports its own decode error
	if err := s.EditChannel("ATM", "broken", &p, nil, chActor); err == nil {
		t.Fatal("want a decode error for the corrupt record itself")
	}
	// and list degrades rather than failing
	recs, err := s.ChannelRecords("ATM")
	if err != nil || len(recs) != 1 || recs[0].Name != "good" {
		t.Fatalf("records: %+v %v", recs, err)
	}
}

func TestChannelWiringAndStamps(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "code", Type: core.ChannelTypeRepo, Address: core.ChannelAddress{URL: "git@x:y.git"}}, chActor); err != nil {
		t.Fatal(err)
	}
	// wiring an unknown channel fails: the ledger record must exist first
	if err := s.SetChannelWiring("ATM", "nope", t.TempDir(), "", chActor); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	// a path must exist
	if err := s.SetChannelWiring("ATM", "code", "/nonexistent/dir", "", chActor); !errors.Is(err, core.ErrUsage) {
		t.Fatalf("missing dir: %v", err)
	}
	dir := t.TempDir()
	if err := s.SetChannelWiring("ATM", "code", dir, "", chActor); err != nil {
		t.Fatal(err)
	}
	if err := s.AddChannelStamp("ATM", "code", "authorized as tu", chActor); err != nil {
		t.Fatal(err)
	}
	// merge: setting only mcp_server keeps path and stamps
	if err := s.SetChannelWiring("ATM", "code", "", "notion", chActor); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.GetProjectConfig("ATM")
	if err != nil || cfg == nil {
		t.Fatalf("config: %v %v", cfg, err)
	}
	w := cfg.Channels["code"]
	if w.Path != dir || w.MCPServer != "notion" || len(w.Stamps) != 1 || w.Stamps[0].By != chActor || w.Stamps[0].Note != "authorized as tu" {
		t.Fatalf("wiring: %+v", w)
	}
	// removing the channel drops its wiring
	if err := s.RemoveChannel("ATM", "code", chActor); err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.GetProjectConfig("ATM")
	if cfg != nil {
		if _, ok := cfg.Channels["code"]; ok {
			t.Fatal("wiring survived channel removal")
		}
	}
}

func mustTask(t *testing.T, s *Store, id string) *core.Task {
	t.Helper()
	tk, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}
