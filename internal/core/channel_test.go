// internal/core/channel_test.go
package core

import (
	"encoding/json"
	"strings"
	"testing"
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
