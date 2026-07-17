package store

import (
	"atm/internal/core"
	"testing"
)

func TestParseTaskIDAcceptsBothAliasGenerations(t *testing.T) {
	if code, n, ok := core.ParseTaskID("ATM-0001"); !ok || code != "ATM" || n != 1 {
		t.Fatalf("v1 id: code=%q n=%d ok=%t", code, n, ok)
	}
	if code, n, ok := core.ParseTaskID("ATM-7f3a2b"); !ok || code != "ATM" || n != 0 {
		t.Fatalf("v2 alias: code=%q n=%d ok=%t (MintTaskAlias output shape)", code, n, ok)
	}
	if code, _, ok := core.ParseTaskID("ATM-7f3a2b0d"); !ok || code != "ATM" {
		t.Fatalf("locally-extended v2 alias must parse: code=%q ok=%t", code, ok)
	}
	if _, _, ok := core.ParseTaskID("ATM-7F3A2B"); ok {
		t.Fatal("uppercase hex is not a minted alias shape and must stay invalid")
	}
	if _, _, ok := core.ParseTaskID("ATM-abc"); ok {
		t.Fatal("hex shorter than the minted minimum (6) must stay invalid")
	}
}

func TestParseCommentIDAcceptsBothAliasGenerations(t *testing.T) {
	if code, taskN, commentN, ok := core.ParseCommentID("ATM-0001-c0002"); !ok || code != "ATM" || taskN != 1 || commentN != 2 {
		t.Fatalf("v1 id: code=%q taskN=%d commentN=%d ok=%t", code, taskN, commentN, ok)
	}
	if code, taskN, commentN, ok := core.ParseCommentID("ATM-7f3a2b-c9e1d"); !ok || code != "ATM" || taskN != 0 || commentN != 0 {
		t.Fatalf("v2 alias: code=%q taskN=%d commentN=%d ok=%t (MintCommentAlias output shape)", code, taskN, commentN, ok)
	}
	if _, _, _, ok := core.ParseCommentID("ATM-7f3a2b"); ok {
		t.Fatal("a task alias is not a comment alias")
	}
}

func TestCommentTaskAliasBothGenerations(t *testing.T) {
	if a, ok := commentTaskAlias("ATM-0001-c0002"); !ok || a != "ATM-0001" {
		t.Fatalf("v1: %q ok=%t", a, ok)
	}
	if a, ok := commentTaskAlias("ATM-7f3a2b-c9e1d"); !ok || a != "ATM-7f3a2b" {
		t.Fatalf("v2: %q ok=%t", a, ok)
	}
}

func TestCreateCommentReplyChecksTaskAliasPrefix(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateComment("ATM-0001", "b", nil, "ATM-0002-c0001", "admin@cli:unset"); !core.IsUsage(err) {
		t.Fatalf("cross-task numeric reply = %v, want core.ErrUsage", err)
	}
	if _, err := s.CreateComment("ATM-7f3a2b", "b", nil, "ATM-ffffff-c0f0f", "admin@cli:unset"); !core.IsUsage(err) {
		t.Fatalf("cross-task hash reply = %v, want core.ErrUsage", err)
	}
}
