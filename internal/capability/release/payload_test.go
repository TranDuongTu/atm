package release

import "testing"

func TestEmptyPayloadEncodesToNothing(t *testing.T) {
	p, err := DecodePayload("")
	if err != nil {
		t.Fatal(err)
	}
	if p.Version() != "" || p.ReleaseOf() != "" || len(p.Members()) != 0 {
		t.Fatalf("empty payload is not empty: %+v", p)
	}
	out, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("empty payload encodes to %q, want \"\"", out)
	}
}

func TestMalformedPayloadIsAnError(t *testing.T) {
	if _, err := DecodePayload("{["); err == nil {
		t.Fatal("malformed payload must error")
	}
}

// An older binary must never destroy a newer binary's state.
func TestUnknownFieldsSurviveRoundTrip(t *testing.T) {
	p, err := DecodePayload(`{"v":1,"future_field":"kept","version":"v1-2"}`)
	if err != nil {
		t.Fatal(err)
	}
	p.AddMember("ATM-a")
	out, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	q, err := DecodePayload(out)
	if err != nil {
		t.Fatal(err)
	}
	if str(q.raw["future_field"]) != "kept" {
		t.Fatalf("future_field lost: %s", out)
	}
	if q.Version() != "v1-2" || len(q.Members()) != 1 {
		t.Fatalf("round trip lost fields: %s", out)
	}
}

func TestMemberRosterDeduplicatesAndPreservesOrder(t *testing.T) {
	p, _ := DecodePayload("")
	if !p.AddMember("ATM-a") {
		t.Fatal("first add must report added")
	}
	if p.AddMember("ATM-a") {
		t.Fatal("duplicate add must report not-added")
	}
	p.AddMember("ATM-b")
	if got := p.Members(); len(got) != 2 || got[0] != "ATM-a" || got[1] != "ATM-b" {
		t.Fatalf("members = %v", got)
	}
	if !p.RemoveMember("ATM-a") {
		t.Fatal("remove must report present")
	}
	if p.RemoveMember("ATM-a") {
		t.Fatal("second remove must report absent")
	}
	// An emptied roster is deleted, never stored as [].
	p.RemoveMember("ATM-b")
	out, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("encode = %q, want \"\"", out)
	}
}
