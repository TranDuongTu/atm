package agent

import (
	"errors"
	"testing"
)

var errNotFound = errors.New("not found")

func fakeLookPath(present map[string]bool) func(string) (string, error) {
	return func(bin string) (string, error) {
		if present[bin] {
			return "/usr/bin/" + bin, nil
		}
		return "", errNotFound
	}
}

func TestReadinessStates(t *testing.T) {
	// An empty home means no plugin is installed, so the plugin axis is
	// deterministic; we drive the binary axis via the injected lookPath.
	home := t.TempDir()
	e, _ := Lookup("opencode")

	r := Status(e, home, fakeLookPath(map[string]bool{"opencode": true}))
	if !r.MissingPlugin {
		t.Fatal("expected MissingPlugin with empty home")
	}
	if r.MissingBin {
		t.Fatal("binary present; MissingBin should be false")
	}
	if r.String() != "needs plugin (atm init)" {
		t.Fatalf("String = %q", r.String())
	}

	r = Status(e, home, fakeLookPath(map[string]bool{}))
	if !r.MissingBin || !r.MissingPlugin {
		t.Fatalf("expected both missing, got %+v", r)
	}
	if r.String() != "needs binary + plugin" {
		t.Fatalf("String = %q", r.String())
	}

	oll, _ := Lookup("ollama:codex")
	r = Status(oll, home, fakeLookPath(map[string]bool{}))
	if r.String() != "needs ollama binary + plugin" {
		t.Fatalf("ollama String = %q", r.String())
	}
}
