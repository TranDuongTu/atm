package cli

import (
	"strings"
	"testing"
)

func channelCLI(t *testing.T) *testCLI {
	t.Helper()
	t.Setenv("ATM_PROJECT", "")
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_ = runArgsOut(t, st, "channel", "add", "--project", "ATM", "--name", "design", "--type", "notion",
		"--purpose", "specs and plans", "--workspace", "acme", "--database", "db1", "--actor", "developer@test:unit")
	return st
}

// The design channel becomes a Notion database that holds the documents and
// a Slack channel that is told when one lands.
func TestChannelEndpointAddGivesASecondMedium(t *testing.T) {
	st := channelCLI(t)
	out := runArgsOut(t, st, "channel", "endpoint", "add", "--project", "ATM", "--name", "design",
		"--type", "slack", "--workspace", "acme", "--channel-id", "C1", "--actor", "developer@test:unit")
	mustContain(t, out, "slack endpoint is now broadcast")

	st.output = outputJSON
	out = runArgsOut(t, st, "channel", "show", "--project", "ATM", "--name", "design")
	mustContain(t, out, `"endpoints"`)
	mustContain(t, out, `"role": "home"`)
	mustContain(t, out, `"role": "broadcast"`)
	mustContain(t, out, `"channel_id": "C1"`)
	mustContain(t, out, `"database": "db1"`)
}

func TestChannelEndpointRoleCanBeForced(t *testing.T) {
	st := channelCLI(t)
	out := runArgsOut(t, st, "channel", "endpoint", "add", "--project", "ATM", "--name", "design",
		"--type", "repo", "--url", "https://example.invalid/x.git", "--role", "broadcast", "--actor", "developer@test:unit")
	mustContain(t, out, "repo endpoint is now broadcast")
}

// Content lands in exactly one place, so a second home is refused rather
// than quietly accepted.
func TestChannelEndpointRefusesASecondHome(t *testing.T) {
	st := channelCLI(t)
	msg, code := runChecklistErrText(t, st, "channel", "endpoint", "add", "--project", "ATM", "--name", "design",
		"--type", "repo", "--role", "home", "--url", "https://example.invalid/x.git", "--actor", "developer@test:unit")
	if code == ExitSuccess {
		t.Fatal("accepted a second home endpoint")
	}
	if !strings.Contains(msg, "home") {
		t.Fatalf("error = %q, want it to name the home role", msg)
	}
}

func TestChannelEndpointRemoveKeepsTheHandle(t *testing.T) {
	st := channelCLI(t)
	out := runArgsOut(t, st, "channel", "endpoint", "remove", "--project", "ATM", "--name", "design",
		"--type", "notion", "--actor", "developer@test:unit")
	mustContain(t, out, "removed the notion endpoint")

	st.output = outputJSON
	out = runArgsOut(t, st, "channel", "show", "--project", "ATM", "--name", "design")
	mustContain(t, out, `"name": "design"`)
	mustContain(t, out, "specs and plans")
	if strings.Contains(out, `"endpoints"`) {
		t.Fatalf("endpoints survived removal:\n%s", out)
	}
}

// Wiring and stamps are per ENDPOINT: the Notion database is reached through
// an MCP server, the Slack channel through a different one, and neither
// borrows the other's setup.
func TestChannelWireAndStampPerEndpoint(t *testing.T) {
	st := channelCLI(t)
	_ = runArgsOut(t, st, "channel", "endpoint", "add", "--project", "ATM", "--name", "design",
		"--type", "slack", "--workspace", "acme", "--channel-id", "C1", "--actor", "developer@test:unit")

	_ = runArgsOut(t, st, "channel", "wire", "--project", "ATM", "--name", "design",
		"--type", "notion", "--mcp-server", "notion-mcp", "--actor", "developer@test:unit")
	_ = runArgsOut(t, st, "channel", "stamp", "--project", "ATM", "--name", "design",
		"--type", "notion", "--kind", "use", "--note", "posted the plan", "--actor", "developer@test:unit")
	_ = runArgsOut(t, st, "channel", "stamp", "--project", "ATM", "--name", "design",
		"--type", "slack", "--kind", "probe", "--note", "resolved channel info", "--actor", "manager@ollama:unit")

	st.output = outputJSON
	out := runArgsOut(t, st, "channel", "show", "--project", "ATM", "--name", "design")
	mustContain(t, out, `"mcp_server": "notion-mcp"`)
	mustContain(t, out, `"kind": "probe"`)
	mustContain(t, out, "posted the plan")
	mustContain(t, out, "manager@ollama:unit")
}

// Naming no medium is fine when there is only one, and refused when there
// are several: wiring the wrong endpoint silently is worse than asking.
func TestChannelWireWithoutTypeNeedsASingleEndpoint(t *testing.T) {
	st := channelCLI(t)
	if out := runArgsOut(t, st, "channel", "stamp", "--project", "ATM", "--name", "design",
		"--note", "reached it", "--actor", "developer@test:unit"); !strings.Contains(out, "stamped channel design") {
		t.Fatalf("single-endpoint stamp without --type failed: %s", out)
	}
	_ = runArgsOut(t, st, "channel", "endpoint", "add", "--project", "ATM", "--name", "design",
		"--type", "slack", "--workspace", "acme", "--channel-id", "C1", "--actor", "developer@test:unit")

	msg, code := runChecklistErrText(t, st, "channel", "stamp", "--project", "ATM", "--name", "design",
		"--note", "reached it", "--actor", "developer@test:unit")
	if code == ExitSuccess {
		t.Fatal("stamped an ambiguous channel without --type")
	}
	if !strings.Contains(msg, "--type") || !strings.Contains(msg, "notion") {
		t.Fatalf("error = %q, want it to name --type and the choices", msg)
	}
}

func TestChannelStampRejectsAnUnknownKind(t *testing.T) {
	st := channelCLI(t)
	msg, code := runChecklistErrText(t, st, "channel", "stamp", "--project", "ATM", "--name", "design",
		"--kind", "guess", "--note", "x", "--actor", "developer@test:unit")
	if code == ExitSuccess || !strings.Contains(msg, "guess") {
		t.Fatalf("error = %q (code %d), want a refusal naming the bad kind", msg, code)
	}
}
