package channel

import "testing"

func TestVocabularyShape(t *testing.T) {
	v := Vocabulary("ATM")
	names := map[string]string{}
	for _, l := range v {
		names[l.Name] = l.Expr
	}
	if _, ok := names["ATM:channel:*"]; !ok {
		t.Fatal("missing namespace descriptor")
	}
	if _, ok := names["ATM:channel:repo"]; !ok {
		t.Fatal("missing repo type label")
	}
	if _, ok := names["ATM:channel:notion"]; !ok {
		t.Fatal("missing notion type label")
	}
	if expr := names["ATM:channels"]; expr != "channel:*" {
		t.Fatalf("channels board expr = %q", expr)
	}
}
