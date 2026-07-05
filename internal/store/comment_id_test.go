package store

import "testing"

func TestParseCommentID(t *testing.T) {
	cases := []struct {
		in       string
		ok       bool
		code     string
		taskN    int
		commentN int
	}{
		{"ATM-0001-c0001", true, "ATM", 1, 1},
		{"ATM-9999-c9999", true, "ATM", 9999, 9999},
		{"ATM-0001-c0001", true, "ATM", 1, 1},
		{"ATM-10000-c0001", true, "ATM", 10000, 1},
		{"ATM-c0001", false, "", 0, 0},
		{"ATM-0001", false, "", 0, 0},
		{"c0001", false, "", 0, 0},
		{"", false, "", 0, 0},
		{"atm-0001-c0001", false, "", 0, 0},
		{"ATM-0001-C0001", false, "", 0, 0},
		{"XYZ-0001-c0001", true, "XYZ", 1, 1},
	}
	for _, tc := range cases {
		code, taskN, commentN, ok := ParseCommentID(tc.in)
		if ok != tc.ok || (ok && (code != tc.code || taskN != tc.taskN || commentN != tc.commentN)) {
			t.Errorf("ParseCommentID(%q) = (%q, %d, %d, %v), want (%q, %d, %d, %v)",
				tc.in, code, taskN, commentN, ok, tc.code, tc.taskN, tc.commentN, tc.ok)
		}
	}
}

func TestRenderCommentID(t *testing.T) {
	if got := RenderCommentID("ATM-0001", 1); got != "ATM-0001-c0001" {
		t.Fatalf("RenderCommentID(1) = %q want ATM-0001-c0001", got)
	}
	if got := RenderCommentID("ATM-0001", 9999); got != "ATM-0001-c9999" {
		t.Fatalf("RenderCommentID(9999) = %q want ATM-0001-c9999", got)
	}
	if got := RenderCommentID("ATM-0001", 10000); got != "ATM-0001-c10000" {
		t.Fatalf("RenderCommentID(10000) = %q want ATM-0001-c10000", got)
	}
}
