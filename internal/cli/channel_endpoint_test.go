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
