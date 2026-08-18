package workflowrpi

import "testing"

func TestPayloadRoundTripPreservesUnknownFields(t *testing.T) {
	pl, err := DecodePayload(`{"v":99,"future":"keep","depends_on":["ATM-a"],"product_of":"ATM-p"}`)
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if pl.ProductOf() != "ATM-p" {
		t.Fatalf("ProductOf = %q", pl.ProductOf())
	}
	if !pl.AddDependsOn("ATM-b") {
		t.Fatal("AddDependsOn did not add ATM-b")
	}
	enc, err := pl.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	again, err := DecodePayload(enc)
	if err != nil {
		t.Fatalf("Decode encoded: %v", err)
	}
	if again.raw["future"] != "keep" {
		t.Errorf("unknown field lost: %v", again.raw)
	}
	if got := again.DependsOn(); len(got) != 2 || got[0] != "ATM-a" || got[1] != "ATM-b" {
		t.Errorf("DependsOn = %v", got)
	}
}

func TestPayloadEncodeEmptyClearsKey(t *testing.T) {
	pl, err := DecodePayload("")
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	enc, err := pl.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if enc != "" {
		t.Fatalf("empty payload encodes to %q, want empty string", enc)
	}
}

func TestDecodePayloadRejectsMalformedJSON(t *testing.T) {
	if _, err := DecodePayload("not json"); err == nil {
		t.Fatal("malformed payload must be an error")
	}
}
