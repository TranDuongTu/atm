package eventsource

import "testing"

// The RFC 8785 example object (§3.2.3): numbers get ES6 shortest-round-trip
// serialization, strings get canonical escapes, keys sort by UTF-16 units.
func TestCanonicalizeRFC8785Vector(t *testing.T) {
	in := []byte(`{
  "numbers": [333333333.33333329, 1E30, 4.50, 2e-3, 0.000000000000000000000000001],
  "string": "\u20ac$\u000F\u000aA'\u0042\u0022\u005c\\\"\/",
  "literals": [null, true, false]
}`)
	want := `{"literals":[null,true,false],"numbers":[333333333.3333333,1e+30,4.5,0.002,1e-27],"string":"€$\u000f\nA'B\"\\\\\"/"}`
	got, err := Canonicalize(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("canonical form:\n got %s\nwant %s", got, want)
	}
}

func TestCanonicalizeSortsKeysAndStripsWhitespace(t *testing.T) {
	got, err := Canonicalize([]byte(`{"b": 2, "a": 1}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"a":1,"b":2}` {
		t.Errorf("got %s", got)
	}
}

func TestCanonicalizeIsIdempotent(t *testing.T) {
	once, err := Canonicalize([]byte(`{"z":[1,2],"a":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Canonicalize(once)
	if err != nil {
		t.Fatal(err)
	}
	if string(once) != string(twice) {
		t.Errorf("not idempotent: %s vs %s", once, twice)
	}
}

func TestCanonicalizeRejectsInvalidJSON(t *testing.T) {
	if _, err := Canonicalize([]byte(`{"unterminated`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
}
