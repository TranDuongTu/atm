package core

import "testing"

func TestParseExprPrecedenceAndAtoms(t *testing.T) {
	// NOT binds tighter than AND; AND binds tighter than OR.
	n, err := ParseExpr("a OR b AND NOT c")
	if err != nil {
		t.Fatalf("ParseExpr: %v", err)
	}
	or, ok := n.(*ExprOr)
	if !ok {
		t.Fatalf("root = %T, want *ExprOr (OR is lowest precedence)", n)
	}
	if _, ok := or.R.(*ExprAnd); !ok {
		t.Fatalf("or.R = %T, want *ExprAnd", or.R)
	}
	got := Atoms(n)
	want := []string{"a", "b", "c"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("Atoms = %v, want %v", got, want)
	}
}

func TestParseExprParensOverridePrecedence(t *testing.T) {
	n, err := ParseExpr("(a OR b) AND c")
	if err != nil {
		t.Fatalf("ParseExpr: %v", err)
	}
	if _, ok := n.(*ExprAnd); !ok {
		t.Fatalf("root = %T, want *ExprAnd", n)
	}
}

func TestParseExprAtomForms(t *testing.T) {
	// stored label, namespace predicate, board reference
	for _, src := range []string{"status:open", "status:*", "next-sprint"} {
		n, err := ParseExpr(src)
		if err != nil {
			t.Fatalf("ParseExpr(%q): %v", src, err)
		}
		a, ok := n.(*ExprAtom)
		if !ok || a.Name != src {
			t.Fatalf("ParseExpr(%q) = %#v, want ExprAtom{%q}", src, n, src)
		}
	}
}

func TestParseExprRejectsMalformed(t *testing.T) {
	bad := []string{"", "  ", "AND a", "a AND", "(a", "a)", "a b", "NOT", "a OR OR b"}
	for _, src := range bad {
		if _, err := ParseExpr(src); err == nil {
			t.Errorf("ParseExpr(%q) = nil error, want error", src)
		}
	}
}

func TestParseExprStarAtom(t *testing.T) {
	// The bare '*' tautology atom: lexes as a single token and parses as an
	// ExprAtom. It is the membership predicate of the all-tasks board and a
	// reusable standalone filter token.
	n, err := ParseExpr("*")
	if err != nil {
		t.Fatalf("ParseExpr(%q): %v", "*", err)
	}
	a, ok := n.(*ExprAtom)
	if !ok || a.Name != "*" {
		t.Fatalf("ParseExpr(%q) = %#v, want ExprAtom{%q}", "*", n, "*")
	}
}

func TestParseExprStarComposes(t *testing.T) {
	// '*' is a normal atom: it composes with AND/OR/NOT like any other.
	n, err := ParseExpr("* AND NOT status:done")
	if err != nil {
		t.Fatalf("ParseExpr: %v", err)
	}
	if _, ok := n.(*ExprAnd); !ok {
		t.Fatalf("root = %T, want *ExprAnd", n)
	}
	got := Atoms(n)
	want := []string{"*", "status:done"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Atoms = %v, want %v", got, want)
	}
}
