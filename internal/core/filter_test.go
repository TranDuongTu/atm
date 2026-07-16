package core

import (
	"reflect"
	"testing"
)

func TestParseFilter(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"ATM:status:*", []string{"ATM:status:*"}},
		{"ATM:status:* ATM:type:*", []string{"ATM:status:*", "ATM:type:*"}},
		{"  ATM:status:*   ATM:type:*  ", []string{"ATM:status:*", "ATM:type:*"}},
	} {
		if got := ParseFilter(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseFilter(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestFilterTokenOps(t *testing.T) {
	if !FilterHasToken("ATM:status:* ATM:type:*", "ATM:type:*") {
		t.Error("present token must be found")
	}
	if FilterHasToken("ATM:status:*", "ATM:type:*") {
		t.Error("absent token must not be found")
	}
	if got := FilterAddToken("ATM:status:*", "ATM:type:*"); got != "ATM:status:* ATM:type:*" {
		t.Errorf("FilterAddToken = %q", got)
	}
	if got := FilterAddToken("ATM:status:*", "ATM:status:*"); got != "ATM:status:*" {
		t.Errorf("FilterAddToken must dedupe, got %q", got)
	}
	if got := FilterAddToken("", "ATM:status:*"); got != "ATM:status:*" {
		t.Errorf("FilterAddToken on empty = %q", got)
	}
	if got := FilterRemoveToken("ATM:status:* ATM:type:*", "ATM:status:*"); got != "ATM:type:*" {
		t.Errorf("FilterRemoveToken = %q", got)
	}
	if got := FilterRemoveToken("ATM:status:*", "ATM:status:*"); got != "" {
		t.Errorf("FilterRemoveToken to empty = %q", got)
	}
}
