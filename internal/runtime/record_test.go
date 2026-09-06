package runtime

import (
	"path/filepath"
	"regexp"
	"testing"
)

func TestNewRunIDShape(t *testing.T) {
	re := regexp.MustCompile(`^ATM-\d{14}-[0-9a-f]{6}$`)
	a, b := NewRunID("ATM"), NewRunID("ATM")
	if !re.MatchString(a) {
		t.Fatalf("run id %q does not match <CODE>-<YYYYMMDDHHMMSS>-<6 hex>", a)
	}
	if a == b {
		t.Fatalf("two run ids minted back to back must differ: %q", a)
	}
}

func TestContextPathPerRun(t *testing.T) {
	got := ContextPath("/STORE", "ATM", "ATM-20260905075522-a1b2c3")
	want := filepath.Join("/STORE", "projects", "ATM", "cache", "sessions", "ATM-20260905075522-a1b2c3.md")
	if got != want {
		t.Fatalf("ContextPath = %q, want %q", got, want)
	}
	if got := ContextPath("/STORE", "", "atm-20260905075522-a1b2c3"); got != filepath.Join("/STORE", "cache", "sessions", "atm-20260905075522-a1b2c3.md") {
		t.Fatalf("no-project ContextPath = %q", got)
	}
	if got := ContextDir("/STORE", "ATM"); got != filepath.Join("/STORE", "projects", "ATM", "cache", "sessions") {
		t.Fatalf("ContextDir = %q", got)
	}
}

func TestStatesAndLiveness(t *testing.T) {
	for _, s := range []State{StateWorking, StateIdle, StateBlocked, StateWatching, StateEnded} {
		if !ValidState(string(s)) {
			t.Errorf("ValidState(%q) = false", s)
		}
	}
	if ValidState("sleeping") {
		t.Error("ValidState(sleeping) must be false")
	}
}
