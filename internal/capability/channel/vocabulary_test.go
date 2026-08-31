package channel

import (
	"testing"

	"atm/internal/core"
)

func TestVocabularyShape(t *testing.T) {
	v := Vocabulary("ATM")
	names := map[string]string{}
	for _, l := range v {
		names[l.Name] = l.Expr
	}
	if _, ok := names["ATM:channel:*"]; !ok {
		t.Fatal("missing namespace descriptor")
	}
	// Every recognized type carries a label: a type the store accepts but
	// the vocabulary never describes is a channel record no reader can
	// interpret. Driven off core.ChannelTypes so opening the enum without
	// seeding the label fails here rather than in production.
	for _, typ := range core.ChannelTypes {
		if _, ok := names["ATM:channel:"+typ]; !ok {
			t.Fatalf("missing %s type label", typ)
		}
	}
	if expr := names["ATM:channels"]; expr != "channel:*" {
		t.Fatalf("channels board expr = %q", expr)
	}
}
