// internal/core/channel_test.go
package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestChannelPayloadRoundTrip(t *testing.T) {
	rec := ChannelRecord{Name: "specs", Type: ChannelTypeNotion, Address: ChannelAddress{Workspace: "acme", Database: "abc123"}}
	s, err := EncodeChannelPayload(ChannelPayloadFrom(rec))
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "ATM-x1", Description: "specs live here", Labels: []string{"ATM:channel:notion"}, Meta: map[string]string{ChannelMetaKey: s}}
	got, err := ChannelFromTask("ATM", task)
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != "ATM-x1" || got.Name != "specs" || got.Type != ChannelTypeNotion || got.Purpose != "specs live here" || got.Address.Database != "abc123" {
		t.Fatalf("got %+v", got)
	}
}

// A slack channel is addressed by the workspace slug plus the channel id.
// The slug, not the T… team id, because it is what builds a permalink:
// https://<workspace>.slack.com/archives/<channel_id>/p<ts>.
func TestChannelPayloadRoundTripSlack(t *testing.T) {
	rec := ChannelRecord{Name: "pr-reviews", Type: ChannelTypeSlack, Address: ChannelAddress{Workspace: "acme", ChannelID: "C0123ABC"}}
	s, err := EncodeChannelPayload(ChannelPayloadFrom(rec))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, `"channel_id":"C0123ABC"`) {
		t.Fatalf("payload key is a contract, got %s", s)
	}
	task := Task{ID: "ATM-x2", Description: "agents post every PR here", Labels: []string{"ATM:channel:slack"}, Meta: map[string]string{ChannelMetaKey: s}}
	got, err := ChannelFromTask("ATM", task)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != ChannelTypeSlack || got.Address.Workspace != "acme" || got.Address.ChannelID != "C0123ABC" {
		t.Fatalf("got %+v", got)
	}
}

// The agent endpoint's JSON keys are a contract: pin them so a field rename
// or a dropped tag cannot silently change what `--output json` emits.
func TestChannelViewJSONKeys(t *testing.T) {
	b, err := json.Marshal(ChannelView{ChannelRecord: ChannelRecord{TaskID: "ATM-x1", Name: "specs", Type: ChannelTypeNotion, Purpose: "p", Address: ChannelAddress{Database: "abc123"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"task_id":"ATM-x1"`, `"name":"specs"`, `"type":"notion"`, `"purpose":"p"`, `"address":{"database":"abc123"}`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("view JSON %s missing %s", b, want)
		}
	}
}

func TestChannelPayloadPreservesUnknownFields(t *testing.T) {
	m, err := DecodeChannelPayload(`{"v":1,"name":"x","future_field":"kept"}`)
	if err != nil {
		t.Fatal(err)
	}
	m["name"] = "y"
	s, err := EncodeChannelPayload(m)
	if err != nil {
		t.Fatal(err)
	}
	m2, _ := DecodeChannelPayload(s)
	if m2["future_field"] != "kept" || m2["name"] != "y" {
		t.Fatalf("unknown field lost: %s", s)
	}
}

func TestChannelPayloadErrors(t *testing.T) {
	if _, err := DecodeChannelPayload("not json"); err == nil {
		t.Fatal("want error on malformed payload")
	}
	if m, err := DecodeChannelPayload(""); err != nil || len(m) != 0 {
		t.Fatal("empty payload must decode to empty map")
	}
	// A task without a channel:<type> label is not a channel: nil, nil.
	if rec, err := ChannelFromTask("ATM", Task{ID: "t", Labels: []string{"ATM:status:open"}}); rec != nil || err != nil {
		t.Fatalf("non-channel task: got %v, %v", rec, err)
	}
	// A channel label with an unreadable payload is an error (verbs must not overwrite state they cannot read).
	if _, err := ChannelFromTask("ATM", Task{ID: "t", Labels: []string{"ATM:channel:repo"}, Meta: map[string]string{ChannelMetaKey: "garbage"}}); err == nil {
		t.Fatal("want error for unreadable payload")
	}
}

// TestChannelStatus pins the one status rule both adapters read: the glyph
// vocabulary, the notes, and the 14/45-day stamp thresholds. It moved here
// from internal/tui when the rule did — a status the CLI and the TUI compute
// separately is a status they will eventually disagree about.
func TestChannelStatus(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	stamp := func(daysAgo int) []VerificationStamp {
		return []VerificationStamp{{At: now.AddDate(0, 0, -daysAgo).Format(time.RFC3339), By: "developer@test:unit"}}
	}
	cases := []struct {
		name  string
		view  ChannelView
		glyph string
		note  string
	}{
		{
			name:  "fresh stamp",
			view:  ChannelView{Wiring: &ChannelWiring{MCPServer: "notion", Stamps: stamp(2)}},
			glyph: "●",
			note:  "verified 2d ago",
		},
		{
			name:  "aging stamp",
			view:  ChannelView{Wiring: &ChannelWiring{MCPServer: "notion", Stamps: stamp(20)}},
			glyph: "◐",
			note:  "verified 20d ago",
		},
		{
			name:  "stale stamp",
			view:  ChannelView{Wiring: &ChannelWiring{MCPServer: "notion", Stamps: stamp(60)}},
			glyph: "○",
			note:  "stale · verified 60d ago",
		},
		{
			name:  "never verified",
			view:  ChannelView{Wiring: &ChannelWiring{MCPServer: "notion"}},
			glyph: "◐",
			note:  "wired, never verified",
		},
		{
			name:  "unwired",
			view:  ChannelView{},
			glyph: "○",
			note:  "unwired",
		},
		{
			name:  "probe clean",
			view:  ChannelView{Wiring: &ChannelWiring{Path: "/x"}, Probe: &ChannelProbe{PathExists: true, IsGitRepo: true}},
			glyph: "●",
			note:  "clean",
		},
		{
			name:  "probe dirty",
			view:  ChannelView{Wiring: &ChannelWiring{Path: "/x"}, Probe: &ChannelProbe{PathExists: true, IsGitRepo: true, Dirty: true}},
			glyph: "◐",
			note:  "dirty",
		},
		{
			name:  "probe diverged",
			view:  ChannelView{Wiring: &ChannelWiring{Path: "/x"}, Probe: &ChannelProbe{PathExists: true, IsGitRepo: true, HasUpstream: true, Ahead: 2, Behind: 1}},
			glyph: "◐",
			note:  "clean · 2 ahead 1 behind",
		},
		{
			name:  "probe path missing",
			view:  ChannelView{Wiring: &ChannelWiring{Path: "/x"}, Probe: &ChannelProbe{}},
			glyph: "○",
			note:  "path missing",
		},
		{
			name:  "probe not a git repo",
			view:  ChannelView{Wiring: &ChannelWiring{Path: "/x"}, Probe: &ChannelProbe{PathExists: true}},
			glyph: "◐",
			note:  "not a git repo",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			glyph, note := ChannelStatus(tc.view, now)
			if glyph != tc.glyph || note != tc.note {
				t.Errorf("ChannelStatus = %q/%q, want %q/%q", glyph, note, tc.glyph, tc.note)
			}
		})
	}
}
