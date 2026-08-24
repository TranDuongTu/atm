package codereview

import "testing"

func TestEmptyPayloadEncodesToNothing(t *testing.T) {
	p, err := DecodePayload("")
	if err != nil {
		t.Fatal(err)
	}
	if p.PR() != "" || p.Report() != "" {
		t.Fatalf("empty payload is not empty: %+v", p)
	}
	out, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("empty payload encodes to %q, want \"\" (so the Meta key is deleted)", out)
	}
}

func TestMalformedPayloadIsAnError(t *testing.T) {
	if _, err := DecodePayload("{["); err == nil {
		t.Fatal("malformed payload must error")
	}
}

// An older binary must never destroy a newer binary's state.
func TestUnknownFieldsSurviveRoundTrip(t *testing.T) {
	p, err := DecodePayload(`{"v":1,"future_field":"kept","pr":"#142"}`)
	if err != nil {
		t.Fatal(err)
	}
	p.SetReport("docs/reviews/142.md")
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
	if q.PR() != "#142" || q.Report() != "docs/reviews/142.md" {
		t.Fatalf("round trip lost fields: %s", out)
	}
}

// release clears both locators, and clearing everything this version owns must
// empty the payload — that is what deletes the Meta key.
func TestClearingEveryOwnedFieldEmptiesThePayload(t *testing.T) {
	p, _ := DecodePayload(`{"v":1,"pr":"#1","report":"r"}`)
	p.ClearPR()
	p.ClearReport()
	out, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("encode = %q, want \"\"", out)
	}
}
