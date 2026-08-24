package qa

import "testing"

func TestEmptyPayloadRoundTrips(t *testing.T) {
	p, err := DecodePayload("")
	if err != nil {
		t.Fatal(err)
	}
	if p.PartOf() != "" || len(p.Scaffolds()) != 0 {
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

func TestUnknownFieldsSurviveRoundTrip(t *testing.T) {
	p, err := DecodePayload(`{"v":1,"future_field":"kept","part_of":"ATM-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	p.AddScaffold("ATM-s1")
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
	if q.PartOf() != "ATM-1" || len(q.Scaffolds()) != 1 {
		t.Fatalf("round trip lost fields: %s", out)
	}
}

func TestScaffoldListDeduplicatesAndPreservesOrder(t *testing.T) {
	p, _ := DecodePayload("")
	if !p.AddScaffold("ATM-a") {
		t.Fatal("first add must report added")
	}
	if p.AddScaffold("ATM-a") {
		t.Fatal("duplicate add must report not-added")
	}
	p.AddScaffold("ATM-b")
	if got := p.Scaffolds(); len(got) != 2 || got[0] != "ATM-a" || got[1] != "ATM-b" {
		t.Fatalf("scaffolds = %v", got)
	}
	if !p.RemoveScaffold("ATM-a") {
		t.Fatal("remove must report present")
	}
	if p.RemoveScaffold("ATM-a") {
		t.Fatal("second remove must report absent")
	}
}
