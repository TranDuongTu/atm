package store

import (
	"strings"
	"testing"

	"atm/internal/core"
)

func channelProject(t *testing.T) (*Store, string) {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("ATM", "ATM", testActor); err != nil {
		t.Fatal(err)
	}
	return s, "ATM"
}

func mustChannel(t *testing.T, s *Store, code, name string) *core.ChannelRecord {
	t.Helper()
	rec, err := s.channelByName(code, name)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// A channel created the old way — one type, one address — is one endpoint.
func TestCreateChannelFoldsASingleAddressIntoAnEndpoint(t *testing.T) {
	s, code := channelProject(t)
	rec := core.ChannelRecord{Name: "design", Purpose: "the design channel", Type: core.ChannelTypeNotion,
		Address: core.ChannelAddress{Workspace: "acme", Database: "db1"}}
	if _, err := s.CreateChannel(code, rec, testActor); err != nil {
		t.Fatal(err)
	}
	got := mustChannel(t, s, code, "design")
	if len(got.Endpoints) != 1 || got.Endpoints[0].Type != core.ChannelTypeNotion {
		t.Fatalf("endpoints = %#v", got.Endpoints)
	}
	if got.Endpoints[0].Address.Database != "db1" {
		t.Fatalf("address lost: %#v", got.Endpoints[0])
	}
	// The bare label is what a v2 record carries; the medium is payload now.
	tk, _ := s.GetTask(got.TaskID)
	if !hasLabel(tk.Labels, core.ChannelLabel(code)) {
		t.Fatalf("labels = %v, want the bare channel label", tk.Labels)
	}
	for _, l := range tk.Labels {
		if strings.HasPrefix(l, core.ChannelTypeLabelPrefix(code)) {
			t.Fatalf("a new record must not carry a per-medium label: %v", tk.Labels)
		}
	}
}

// The design channel is a Notion database that holds documents AND a Slack
// channel that gets told when one lands.
func TestAddChannelEndpointGivesAChannelASecondMedium(t *testing.T) {
	s, code := channelProject(t)
	if _, err := s.CreateChannel(code, core.ChannelRecord{Name: "design", Type: core.ChannelTypeNotion,
		Address: core.ChannelAddress{Workspace: "acme", Database: "db1"}}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.AddChannelEndpoint(code, "design", core.ChannelEndpoint{
		Type: core.ChannelTypeSlack, Address: core.ChannelAddress{Workspace: "acme", ChannelID: "C1"}}, testActor); err != nil {
		t.Fatal(err)
	}
	got := mustChannel(t, s, code, "design")
	if len(got.Endpoints) != 2 {
		t.Fatalf("endpoints = %#v", got.Endpoints)
	}
	home, ok := got.Home()
	if !ok || home.Type != core.ChannelTypeNotion {
		t.Fatalf("home = %#v, %v — the document medium holds the content", home, ok)
	}
	bs := got.Broadcasts()
	if len(bs) != 1 || bs[0].Type != core.ChannelTypeSlack {
		t.Fatalf("broadcasts = %#v — the messaging medium carries the reference", bs)
	}
}

// A second document-shaped medium cannot silently become a second home.
func TestAddChannelEndpointKeepsOneHome(t *testing.T) {
	s, code := channelProject(t)
	if _, err := s.CreateChannel(code, core.ChannelRecord{Name: "design", Type: core.ChannelTypeNotion}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.AddChannelEndpoint(code, "design", core.ChannelEndpoint{Type: core.ChannelTypeRepo,
		Address: core.ChannelAddress{URL: "https://example.invalid/x.git"}}, testActor); err != nil {
		t.Fatal(err)
	}
	got := mustChannel(t, s, code, "design")
	homes := 0
	for _, e := range got.Endpoints {
		if e.Role == core.ChannelRoleHome {
			homes++
		}
	}
	if homes != 1 {
		t.Fatalf("homes = %d in %#v, want exactly one", homes, got.Endpoints)
	}
	// An explicit second home is refused rather than silently demoted.
	err := s.AddChannelEndpoint(code, "design", core.ChannelEndpoint{Type: core.ChannelTypeRepo, Role: core.ChannelRoleHome}, testActor)
	if err == nil || !strings.Contains(err.Error(), "home") {
		t.Fatalf("err = %v, want a refusal naming the home role", err)
	}
}

// Re-adding a medium corrects its address instead of duplicating it.
func TestAddChannelEndpointReplacesTheSameMedium(t *testing.T) {
	s, code := channelProject(t)
	if _, err := s.CreateChannel(code, core.ChannelRecord{Name: "prs", Type: core.ChannelTypeSlack,
		Address: core.ChannelAddress{Workspace: "acme", ChannelID: "OLD"}}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.AddChannelEndpoint(code, "prs", core.ChannelEndpoint{Type: core.ChannelTypeSlack,
		Address: core.ChannelAddress{Workspace: "acme", ChannelID: "NEW"}}, testActor); err != nil {
		t.Fatal(err)
	}
	got := mustChannel(t, s, code, "prs")
	if len(got.Endpoints) != 1 || got.Endpoints[0].Address.ChannelID != "NEW" {
		t.Fatalf("endpoints = %#v", got.Endpoints)
	}
}

// Removing the last endpoint leaves the handle: a channel with no address
// is an expectation waiting to be addressed, which is what applying a
// profile creates.
func TestRemoveChannelEndpointKeepsTheHandle(t *testing.T) {
	s, code := channelProject(t)
	if _, err := s.CreateChannel(code, core.ChannelRecord{Name: "qa", Purpose: "verification", Type: core.ChannelTypeSlack}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveChannelEndpoint(code, "qa", core.ChannelTypeSlack, testActor); err != nil {
		t.Fatal(err)
	}
	got := mustChannel(t, s, code, "qa")
	if len(got.Endpoints) != 0 {
		t.Fatalf("endpoints = %#v, want none", got.Endpoints)
	}
	if got.Name != "qa" || got.Purpose != "verification" {
		t.Fatalf("the handle did not survive: %+v", got)
	}
	if err := s.RemoveChannelEndpoint(code, "qa", core.ChannelTypeSlack, testActor); !core.IsNotFound(err) {
		t.Fatalf("removing an absent endpoint = %v, want not-found", err)
	}
}

// A record written before endpoints existed reads as one endpoint, and the
// first WRITE migrates its label. Reads never write.
func TestChannelV1RecordReadsThenMigratesOnWrite(t *testing.T) {
	s, code := channelProject(t)
	legacyLabel := core.ChannelTypeLabel(code, core.ChannelTypeNotion)
	if err := s.LabelSeed(legacyLabel, "legacy", "", testActor); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask(code, "design", "the design channel", []string{legacyLabel}, testActor)
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"v":1,"name":"design","type":"notion","address":{"workspace":"acme","database":"db1"}}`
	if err := s.SetTaskCapabilityMeta(tk.ID, core.ChannelMetaKey, payload, testActor); err != nil {
		t.Fatal(err)
	}

	got := mustChannel(t, s, code, "design")
	if len(got.Endpoints) != 1 || got.Endpoints[0].Address.Database != "db1" {
		t.Fatalf("v1 record did not read as one endpoint: %#v", got.Endpoints)
	}
	after, _ := s.GetTask(tk.ID)
	if !hasLabel(after.Labels, legacyLabel) {
		t.Fatal("a READ relabelled the record; reads must never write")
	}

	if err := s.AddChannelEndpoint(code, "design", core.ChannelEndpoint{Type: core.ChannelTypeSlack,
		Address: core.ChannelAddress{Workspace: "acme", ChannelID: "C1"}}, testActor); err != nil {
		t.Fatal(err)
	}
	after, _ = s.GetTask(tk.ID)
	if !hasLabel(after.Labels, core.ChannelLabel(code)) {
		t.Fatalf("labels = %v, want the bare label after a write", after.Labels)
	}
	if hasLabel(after.Labels, legacyLabel) {
		t.Fatalf("labels = %v, want the per-medium label dropped", after.Labels)
	}
	final := mustChannel(t, s, code, "design")
	if len(final.Endpoints) != 2 {
		t.Fatalf("endpoints after migration = %#v", final.Endpoints)
	}
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}
