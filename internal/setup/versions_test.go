package setup

import (
	"context"
	"errors"
	"testing"
)

func TestProbeVersionExtractsTheNumber(t *testing.T) {
	cases := map[string]string{
		"2.1.233 (Claude Code)":     "2.1.233",
		"codex-cli 0.145.0":         "0.145.0",
		"1.18.18":                   "1.18.18",
		"ollama version is 0.30.10": "0.30.10",
	}
	for out, want := range cases {
		run := func(context.Context, string, ...string) ([]byte, error) { return []byte(out + "\n"), nil }
		if got := ProbeVersion(context.Background(), "x", run); got != want {
			t.Fatalf("ProbeVersion(%q) = %q, want %q", out, got, want)
		}
	}
}

// An unreadable version is blank, not a guess and not an error string in the
// UI's version column.
func TestProbeVersionFailureIsBlank(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("nope") }
	if got := ProbeVersion(context.Background(), "x", run); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestUpdateArgv(t *testing.T) {
	if got, ok := UpdateArgv("opencode"); !ok || got[0] != "upgrade" {
		t.Fatalf("opencode = %v ok=%v (it is `upgrade`, not `update`)", got, ok)
	}
	for _, ag := range []string{"claude", "codex"} {
		if got, ok := UpdateArgv(ag); !ok || got[0] != "update" {
			t.Fatalf("%s = %v ok=%v", ag, got, ok)
		}
	}
	if _, ok := UpdateArgv("ollama"); ok {
		t.Fatal("ollama has no update verb; offering one would be a lie")
	}
}
