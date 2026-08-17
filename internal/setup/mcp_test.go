package setup

import (
	"context"
	"errors"
	"testing"
)

const claudeMCPOut = "Checking MCP server health…\n" +
	"\n" +
	"claude.ai Google Drive: https://drivemcp.googleapis.com/mcp/v1 - ✔ Connected\n" +
	"notion: https://mcp.notion.com/mcp (HTTP) - ✔ Connected\n"

// Captured live from `opencode mcp list` on this machine: opencode dims the
// status word and the URL with an ANSI SGR code even when stdout isn't a
// TTY, so the fixture keeps the escape bytes to prove the parser tolerates
// them. The parser decides Connected from the ✓/✗ glyph alone — never from
// the trailing status word — precisely because that word sits right next to
// the escape code here, and "disconnected" would otherwise substring-match
// "connected" and report a down server as up.
const opencodeMCPOut = "┌  MCP Servers\n" +
	"│\n" +
	"●  ✓ notion \x1b[90mconnected\n" +
	"│      \x1b[90mhttps://mcp.notion.com/mcp\n" +
	"│\n" +
	"└  1 server(s)\n\n"

const codexMCPOut = `[{"name":"notion","url":"https://mcp.notion.com/mcp"}]`

func namesOf(t *testing.T, ag, out string) []string {
	t.Helper()
	a, ok := MCPAdapterFor(ag)
	if !ok {
		t.Fatalf("no adapter for %q", ag)
	}
	servers, err := a.ParseList([]byte(out))
	if err != nil {
		t.Fatalf("%s ParseList: %v", ag, err)
	}
	var names []string
	for _, s := range servers {
		names = append(names, s.Name)
	}
	return names
}

func TestParseClaudeListKeepsNamesWithSpaces(t *testing.T) {
	got := namesOf(t, "claude", claudeMCPOut)
	want := []string{"claude.ai Google Drive", "notion"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

func TestParseOpencodeList(t *testing.T) {
	if got := namesOf(t, "opencode", opencodeMCPOut); len(got) != 1 || got[0] != "notion" {
		t.Fatalf("names = %v", got)
	}
}

func TestParseCodexJSON(t *testing.T) {
	if got := namesOf(t, "codex", codexMCPOut); len(got) != 1 || got[0] != "notion" {
		t.Fatalf("names = %v", got)
	}
	if got := namesOf(t, "codex", "[]"); len(got) != 0 {
		t.Fatalf("empty catalog = %v", got)
	}
}

// THE most important test in this package. Every failure mode must produce
// unknown. A surface that reports "missing" because a probe timed out tells
// the user to fix something that is not broken.
func TestProbeFailureIsUnknownNeverMissing(t *testing.T) {
	cases := []struct {
		name string
		run  RunFunc
	}{
		{"command error", func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("exit status 1")
		}},
		{"timeout", func(context.Context, string, ...string) ([]byte, error) {
			return nil, context.DeadlineExceeded
		}},
		{"unparseable", func(context.Context, string, ...string) ([]byte, error) {
			return []byte("{ this is not the format we expected"), nil
		}},
	}
	for _, c := range cases {
		for _, ag := range []string{"claude", "codex", "opencode"} {
			_, state := ProbeMCP(context.Background(), ag, c.run)
			if state != FactUnknown {
				t.Fatalf("%s/%s: state = %v, want unknown", c.name, ag, state)
			}
		}
	}
}

func TestAddAndLoginArgv(t *testing.T) {
	r := Recipe{Server: "notion", Transport: "http", URL: "https://mcp.notion.com/mcp"}
	cases := map[string][]string{
		"claude":   {"mcp", "add", "--transport", "http", "notion", "https://mcp.notion.com/mcp"},
		"codex":    {"mcp", "add", "notion", "--url", "https://mcp.notion.com/mcp"},
		"opencode": {"mcp", "add", "notion", "--url", "https://mcp.notion.com/mcp"},
	}
	for ag, want := range cases {
		a, _ := MCPAdapterFor(ag)
		got := a.AddArgv(r)
		if len(got) != len(want) {
			t.Fatalf("%s AddArgv = %v, want %v", ag, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s AddArgv = %v, want %v", ag, got, want)
			}
		}
	}
	if got := mustAdapter(t, "opencode").LoginArgv("notion"); got[1] != "auth" {
		t.Fatalf("opencode uses `mcp auth`, got %v", got)
	}
	if got := mustAdapter(t, "claude").LoginArgv("notion"); got[1] != "login" {
		t.Fatalf("claude uses `mcp login`, got %v", got)
	}
}

func mustAdapter(t *testing.T, ag string) MCPAdapter {
	t.Helper()
	a, ok := MCPAdapterFor(ag)
	if !ok {
		t.Fatalf("no adapter for %q", ag)
	}
	return a
}
