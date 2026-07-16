package core

import (
	"testing"
	"time"
)

func TestRFC3339UTC(t *testing.T) {
	in := time.Date(2026, 7, 16, 10, 30, 0, 0, time.FixedZone("X", 7*3600))
	if got, want := RFC3339UTC(in), "2026-07-16T03:30:00Z"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestParseTaskID(t *testing.T) {
	if code, n, ok := ParseTaskID("ATM-0001"); !ok || code != "ATM" || n != 1 {
		t.Fatalf("v1 id: got %q %d %v", code, n, ok)
	}
	if code, n, ok := ParseTaskID("ATM-7f3a2b"); !ok || code != "ATM" || n != 0 {
		t.Fatalf("v2 alias: got %q %d %v", code, n, ok)
	}
	if _, _, ok := ParseTaskID("ATM-7F3A2B"); ok {
		t.Fatal("uppercase hex must not parse")
	}
}

func TestParseCommentID(t *testing.T) {
	code, taskN, commentN, ok := ParseCommentID("ATM-0001-c0002")
	if !ok || code != "ATM" || taskN != 1 || commentN != 2 {
		t.Fatalf("v1 id: got %q %d %d %v", code, taskN, commentN, ok)
	}
	code, taskN, commentN, ok = ParseCommentID("ATM-7f3a2b-c9e1d")
	if !ok || code != "ATM" || taskN != 0 || commentN != 0 {
		t.Fatalf("v2 alias: got %q %d %d %v", code, taskN, commentN, ok)
	}
	if _, _, _, ok := ParseCommentID("nope"); ok {
		t.Fatal("junk id parsed")
	}
}

func TestValidatePersonaName(t *testing.T) {
	if err := ValidatePersonaName("dev-agent2"); err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
	for _, bad := range []string{"", "-x", "x-", "UPPER", "a b"} {
		if err := ValidatePersonaName(bad); err == nil {
			t.Fatalf("invalid name %q accepted", bad)
		}
	}
}

func TestValidatePersonaNameWrapsErrUsage(t *testing.T) {
	err := ValidatePersonaName("Bad Name")
	if !IsUsage(err) {
		t.Fatal("must wrap ErrUsage so the CLI maps it to exit 2")
	}
	want := `usage: invalid persona name "Bad Name" (want ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$)`
	if err.Error() != want {
		t.Fatalf("message drift:\n got %q\nwant %q", err.Error(), want)
	}
}

func TestIsNamespaceName(t *testing.T) {
	if !IsNamespaceName("ATM:status:*") || IsNamespaceName("ATM:status:open") {
		t.Fatal("IsNamespaceName wrong")
	}
}
