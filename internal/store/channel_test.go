// internal/store/channel_test.go
package store

import (
	"errors"
	"os"
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
	// slack is a recognized type, addressed by workspace + channel id
	sl, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "pr-reviews", Type: core.ChannelTypeSlack,
		Address: core.ChannelAddress{Workspace: "acme", ChannelID: "C0123ABC"}}, chActor)
	if err != nil {
		t.Fatalf("slack: %v", err)
	}
	if err := s.RemoveChannel("ATM", "pr-reviews", chActor); err != nil {
		t.Fatalf("remove slack %s: %v", sl.ID, err)
	}
	// unknown type still rejected — the enum stays closed
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "x", Type: "carrier-pigeon"}, chActor); !errors.Is(err, core.ErrUsage) {
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

// A corrupt record must be removable through its own noun: removal needs no
// payload, only the task ID, which the title fallback already yields. Without
// this the record is unremovable — the capability guide forbids repairing
// channel records with raw task verbs.
func TestChannelCorruptRecordCanBeRemoved(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	bad, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "broken", Type: core.ChannelTypeRepo}, chActor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskCapabilityMeta(bad.ID, core.ChannelMetaKey, "garbage", chActor); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveChannel("ATM", "broken", chActor); err != nil {
		t.Fatalf("remove a corrupt record by handle: %v", err)
	}
	tasks, err := s.ListTasksErr(core.QueryFilters{Project: "ATM", Expr: "channel:*"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("corrupt channel task survived removal: %+v", tasks)
	}
	// and an unknown handle still reports absence, not success
	if err := s.RemoveChannel("ATM", "nope", chActor); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("unknown handle: %v", err)
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

func TestProjectChannelsJoinsWiringAndProbe(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "code", Type: core.ChannelTypeRepo}, chActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "specs", Type: core.ChannelTypeNotion}, chActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetChannelWiring("ATM", "code", dir, "", chActor); err != nil {
		t.Fatal(err)
	}
	views, err := s.ProjectChannels("ATM")
	if err != nil || len(views) != 2 {
		t.Fatalf("views: %v %v", views, err)
	}
	// sorted by name: code, specs
	if views[0].Name != "code" || views[0].Wiring == nil || views[0].Probe == nil || !views[0].Probe.PathExists {
		t.Fatalf("repo view: %+v", views[0])
	}
	if views[1].Name != "specs" || views[1].Wiring != nil || views[1].Probe != nil {
		t.Fatalf("notion view without wiring: %+v", views[1])
	}
	v, err := s.GetChannelByName("ATM", "code")
	if err != nil || v.Probe == nil {
		t.Fatalf("by name: %v %v", v, err)
	}
}

func TestMigrateReposToChannels(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetProjectRepo("ATM", "atm", dir, "git@github.com:TranDuongTu/atm.git", chActor); err != nil {
		t.Fatal(err)
	}
	n, unwired, skipped, err := s.MigrateReposToChannels("ATM", chActor)
	if err != nil || n != 1 || len(unwired) != 0 || len(skipped) != 0 {
		t.Fatalf("migrated %d, unwired %v, skipped %v, %v", n, unwired, skipped, err)
	}
	v, err := s.GetChannelByName("ATM", "atm")
	if err != nil || v.Type != core.ChannelTypeRepo || v.Address.URL != "git@github.com:TranDuongTu/atm.git" || v.Wiring == nil || v.Wiring.Path != dir {
		t.Fatalf("migrated channel: %+v err %v", v, err)
	}
	if repos, _ := s.ProjectRepos("ATM"); len(repos) != 0 {
		t.Fatalf("legacy repos not cleared: %v", repos)
	}
	// idempotent: second run migrates nothing and does not error on the existing handle
	if n, _, _, err := s.MigrateReposToChannels("ATM", chActor); err != nil || n != 0 {
		t.Fatalf("re-run: %d %v", n, err)
	}
}

// A legacy repo whose name collides with an EXISTING channel of a DIFFERENT
// type must not be silently dropped: its Path/URL live nowhere else, so
// clearing it out from under the collision would be unrecoverable. It stays
// in the legacy list and is reported in `skipped`; the other channel is
// untouched.
func TestMigrateReposToChannelsSkipsTypeCollision(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "docs", Type: core.ChannelTypeNotion, Purpose: "specs"}, chActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetProjectRepo("ATM", "docs", dir, "git@x:docs.git", chActor); err != nil {
		t.Fatal(err)
	}
	n, unwired, skipped, err := s.MigrateReposToChannels("ATM", chActor)
	if err != nil || n != 0 || len(unwired) != 0 || len(skipped) != 1 || skipped[0] != "docs" {
		t.Fatalf("migrated %d, unwired %v, skipped %v, %v", n, unwired, skipped, err)
	}
	repos, err := s.ProjectRepos("ATM")
	if err != nil || len(repos) != 1 || repos[0].Name != "docs" || repos[0].Path != dir || repos[0].URL != "git@x:docs.git" {
		t.Fatalf("legacy repo must survive the collision: %v %v", repos, err)
	}
	v, err := s.GetChannelByName("ATM", "docs")
	if err != nil || v.Type != core.ChannelTypeNotion || v.Purpose != "specs" {
		t.Fatalf("notion channel must be untouched: %+v %v", v, err)
	}
}

// A legacy repo whose folder moved away must still reach the ledger: the
// record migrates, the wiring is reported missing, and the run completes.
func TestMigrateReposToChannelsSurvivesMissingPath(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	gone := t.TempDir()
	if err := s.SetProjectRepo("ATM", "moved", gone, "git@x:moved.git", chActor); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	n, unwired, skipped, err := s.MigrateReposToChannels("ATM", chActor)
	if err != nil || n != 1 || len(unwired) != 1 || unwired[0] != "moved" || len(skipped) != 0 {
		t.Fatalf("migrated %d, unwired %v, skipped %v, %v", n, unwired, skipped, err)
	}
	v, err := s.GetChannelByName("ATM", "moved")
	if err != nil || v.Address.URL != "git@x:moved.git" || v.Wiring != nil {
		t.Fatalf("record must exist unwired: %+v %v", v, err)
	}
	if repos, _ := s.ProjectRepos("ATM"); len(repos) != 0 {
		t.Fatalf("legacy repos not cleared: %v", repos)
	}
}

func TestRepoChannelTargetsSkipsProbes(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "code", Type: core.ChannelTypeRepo, Address: core.ChannelAddress{URL: "git@x:y.git"}}, chActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "specs", Type: core.ChannelTypeNotion}, chActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetChannelWiring("ATM", "code", dir, "", chActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.RepoChannelTargets("ATM")
	if err != nil || len(got) != 1 || got[0].Name != "code" || got[0].Path != dir || got[0].URL != "git@x:y.git" {
		t.Fatalf("targets: %+v %v", got, err)
	}
}
