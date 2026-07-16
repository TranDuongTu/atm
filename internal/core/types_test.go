package core

import "testing"

func TestLabelIsComputed(t *testing.T) {
	if !(Label{Name: "ATM:done", Expr: "a AND b"}).IsComputed() {
		t.Fatal("board (Expr set) must be computed")
	}
	if !(Label{Name: "ATM:status:*"}).IsComputed() {
		t.Fatal("namespace label must be computed")
	}
	if (Label{Name: "ATM:status:open"}).IsComputed() {
		t.Fatal("plain label must not be computed")
	}
}
