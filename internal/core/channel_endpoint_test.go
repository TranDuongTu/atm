package core

import (
	"reflect"
	"strings"
	"testing"
)

func endpointTask(payload string, labels ...string) Task {
	return Task{ID: "ATM-1", Title: "design", Description: "the design channel",
		Labels: labels, Meta: map[string]string{ChannelMetaKey: payload}}
}

func TestChannelEndpointsRoundTrip(t *testing.T) {
	rec := ChannelRecord{
		Name:    "design",
		Purpose: "the design channel",
		Endpoints: []ChannelEndpoint{
			{Type: ChannelTypeNotion, Role: ChannelRoleHome, Address: ChannelAddress{Workspace: "acme", Database: "db1"}},
			{Type: ChannelTypeSlack, Role: ChannelRoleBroadcast, Address: ChannelAddress{Workspace: "acme", ChannelID: "C123"}},
		},
	}
	enc, err := EncodeChannelPayload(ChannelPayloadFrom(rec))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ChannelFromTask("ATM", endpointTask(enc, ChannelLabel("ATM")))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Endpoints, rec.Endpoints) {
		t.Fatalf("endpoints = %#v, want %#v", got.Endpoints, rec.Endpoints)
	}
}

// A record written before endpoints existed carries its type in the LABEL
// and a single address in the payload. It must read as one endpoint, with
// the role inferred from the type, or every existing project loses its
// channels on upgrade.
func TestChannelReadCompatSingleAddress(t *testing.T) {
	legacy := `{"v":1,"name":"design","type":"notion","address":{"workspace":"acme","database":"db1"}}`
	got, err := ChannelFromTask("ATM", endpointTask(legacy, "ATM:channel:notion"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Endpoints) != 1 {
		t.Fatalf("endpoints = %#v, want exactly one", got.Endpoints)
	}
	e := got.Endpoints[0]
	if e.Type != ChannelTypeNotion || e.Address.Database != "db1" {
		t.Fatalf("endpoint = %#v", e)
	}
	// A document-shaped medium is where content lands.
	if e.Role != ChannelRoleHome {
		t.Fatalf("role = %q, want %q inferred from the type", e.Role, ChannelRoleHome)
	}
	// The pre-endpoint accessors keep answering, so existing readers work.
	if got.Type != ChannelTypeNotion {
		t.Fatalf("Type = %q, want the legacy label's type", got.Type)
	}
}

// Slack is a messaging medium: a reference lands there, not the content.
func TestChannelReadCompatInfersBroadcastForMessaging(t *testing.T) {
	legacy := `{"v":1,"name":"prs","type":"slack","address":{"workspace":"acme","channel_id":"C1"}}`
	got, err := ChannelFromTask("ATM", endpointTask(legacy, "ATM:channel:slack"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Endpoints[0].Role != ChannelRoleBroadcast {
		t.Fatalf("role = %q, want %q for a messaging endpoint", got.Endpoints[0].Role, ChannelRoleBroadcast)
	}
}

// Content lands in ONE place. Two homes make "the home endpoint" ambiguous
// for every writer, so the invariant is enforced where records are built.
func TestValidateChannelEndpointsRejectsTwoHomes(t *testing.T) {
	err := ValidateChannelEndpoints([]ChannelEndpoint{
		{Type: ChannelTypeNotion, Role: ChannelRoleHome},
		{Type: ChannelTypeRepo, Role: ChannelRoleHome},
	})
	if err == nil || !strings.Contains(err.Error(), "home") {
		t.Fatalf("err = %v, want a refusal naming the home role", err)
	}
}

func TestValidateChannelEndpointsRejectsDuplicateTypes(t *testing.T) {
	err := ValidateChannelEndpoints([]ChannelEndpoint{
		{Type: ChannelTypeSlack, Role: ChannelRoleHome},
		{Type: ChannelTypeSlack, Role: ChannelRoleBroadcast},
	})
	if err == nil || !strings.Contains(err.Error(), ChannelTypeSlack) {
		t.Fatalf("err = %v, want a refusal naming the duplicated type", err)
	}
}

func TestValidateChannelEndpointsRejectsUnknownTypeAndRole(t *testing.T) {
	if err := ValidateChannelEndpoints([]ChannelEndpoint{{Type: "carrier-pigeon", Role: ChannelRoleHome}}); err == nil {
		t.Fatal("accepted an unknown endpoint type")
	}
	if err := ValidateChannelEndpoints([]ChannelEndpoint{{Type: ChannelTypeRepo, Role: "shouty"}}); err == nil {
		t.Fatal("accepted an unknown endpoint role")
	}
}

// No endpoints at all is legal: `profile apply` creates channel records from
// a profile's expectations, which carry a handle and a purpose and no
// address — the endpoints are per-project setup that follows.
func TestValidateChannelEndpointsAllowsNone(t *testing.T) {
	if err := ValidateChannelEndpoints(nil); err != nil {
		t.Fatalf("an expectation with no endpoints yet must be legal: %v", err)
	}
}

// Auto-mirroring needs one answer to "where does content land" and one list
// of "who gets told".
func TestChannelHomeAndBroadcasts(t *testing.T) {
	rec := ChannelRecord{Endpoints: []ChannelEndpoint{
		{Type: ChannelTypeSlack, Role: ChannelRoleBroadcast},
		{Type: ChannelTypeNotion, Role: ChannelRoleHome},
		{Type: ChannelTypeRepo, Role: ChannelRoleBroadcast},
	}}
	home, ok := rec.Home()
	if !ok || home.Type != ChannelTypeNotion {
		t.Fatalf("Home() = %#v, %v", home, ok)
	}
	var types []string
	for _, e := range rec.Broadcasts() {
		types = append(types, e.Type)
	}
	if !reflect.DeepEqual(types, []string{ChannelTypeSlack, ChannelTypeRepo}) {
		t.Fatalf("Broadcasts() = %v, want declaration order", types)
	}
	if _, ok := (&ChannelRecord{}).Home(); ok {
		t.Fatal("a channel with no endpoints reported a home")
	}
}

func TestChannelEndpointLookupByType(t *testing.T) {
	rec := ChannelRecord{Endpoints: []ChannelEndpoint{{Type: ChannelTypeNotion, Role: ChannelRoleHome}}}
	if _, ok := rec.Endpoint(ChannelTypeNotion); !ok {
		t.Fatal("Endpoint(notion) not found")
	}
	if _, ok := rec.Endpoint(ChannelTypeSlack); ok {
		t.Fatal("Endpoint resolved a type the channel does not have")
	}
}
