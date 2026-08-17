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

// The three EMPTY-catalog outputs, captured live under an isolated $HOME from
// claude 2.1.233, opencode 1.18.18 and codex 0.145.0. Only codex answers in a
// shape the row-parsers can read; the other two say it in prose, which is why
// mcpEmptyCatalogBanner exists. These are the fixtures a fresh machine really
// produces — the state the wizard exists to get a user out of — and inventing
// a plausible empty output instead (an empty string, say, which parses fine
// and proves nothing) is exactly how this stayed broken.
const claudeMCPOutEmpty = "No MCP servers configured. Use `claude mcp add` to add a server.\n"

const opencodeMCPOutEmpty = "┌  MCP Servers\n" +
	"│\n" +
	"▲  No MCP servers configured\n" +
	"│\n" +
	"└  Add servers with: opencode mcp add\n"

const codexMCPOutEmpty = `[]`

// mcpOutRewordedRows is neither parseable rows NOR an explicit empty-catalog
// banner: a harness that changed its output format. It must still be unknown
// after the banner exception above, for both row-parsers — the exception is a
// new CASE, not a loosening of the rule.
const mcpOutRewordedRows = "┌  MCP Servers\n" +
	"│\n" +
	"◆  notion, reworded beyond recognition\n"

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

// The other half of the unknown-not-missing rule, and the one that was
// missing: a harness that ANSWERED — and whose answer was "none" — must land
// as a clean, empty catalog, not as unknown. Unknown is for "could not tell";
// reporting it here makes the wizard refuse the very `mcp add` that would fix
// the machine, forever, because re-probing cannot change the answer.
func TestExplicitEmptyCatalogIsAnAnswerNotAnUnknown(t *testing.T) {
	fixtures := map[string]string{
		"claude":   claudeMCPOutEmpty,
		"opencode": opencodeMCPOutEmpty,
		"codex":    codexMCPOutEmpty,
	}
	for ag, out := range fixtures {
		run := func(context.Context, string, ...string) ([]byte, error) { return []byte(out), nil }
		servers, state := ProbeMCP(context.Background(), ag, run)
		if state != FactPresent {
			t.Fatalf("%s: state = %v, want present — the harness said there are none", ag, state)
		}
		if len(servers) != 0 {
			t.Fatalf("%s: servers = %v, want none", ag, servers)
		}
	}
}

// The banner is a new CASE, not a loosening: output that is neither readable
// rows nor an explicit "none" is still a format change, and still unknown.
func TestRewordedOutputIsStillUnknownAfterTheEmptyCatalogException(t *testing.T) {
	for _, ag := range []string{"claude", "opencode"} {
		run := func(context.Context, string, ...string) ([]byte, error) {
			return []byte(mcpOutRewordedRows), nil
		}
		if _, state := ProbeMCP(context.Background(), ag, run); state != FactUnknown {
			t.Fatalf("%s: state = %v, want unknown — no rows parsed and the harness never said 'none'", ag, state)
		}
	}
}

// The gate that makes the banner exception safe: saysEmptyCatalog is only
// ever consulted when NO row parsed. Output can carry both — a trailer, a
// warning about another scope, a summary line — and a refactor that hoisted
// the banner check above the `matched == 0` guard would then report a list of
// servers as an empty catalog, with the servers right there in the output.
// That is the false negative that made `[a] mcp add` refuse forever on a
// fresh machine, and it already broke once in production, so it is pinned
// here rather than left to the guard's placement being noticed in review.
func TestEmptyCatalogBannerNeverOverridesParsedRows(t *testing.T) {
	cases := []struct{ agent, out string }{
		{"claude", claudeMCPOut + "\n" + claudeMCPOutEmpty},
		{"opencode", opencodeMCPOut + "▲  No MCP servers configured\n"},
	}
	for _, tc := range cases {
		run := func(context.Context, string, ...string) ([]byte, error) { return []byte(tc.out), nil }
		servers, state := ProbeMCP(context.Background(), tc.agent, run)
		if state != FactPresent {
			t.Fatalf("%s: state = %v, want present", tc.agent, state)
		}
		found := false
		for _, s := range servers {
			if s.Name == "notion" {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: servers = %v — a banner alongside parsed rows must not swallow them; "+
				"saysEmptyCatalog belongs BEHIND the matched == 0 guard", tc.agent, servers)
		}
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
