package compose

// Unit pins for the helpers that moved here from internal/cli with the
// Compose port (ATM-bfe7e5); the behaviors are unchanged.

import (
	"strings"
	"testing"
)

func TestSessionActorCarriesTheSelectedModel(t *testing.T) {
	if got := sessionActor("developer", "ollama", "glm-5.2"); got != "developer@ollama:glm-5.2" {
		t.Fatalf("actor = %q", got)
	}
}

// No model chosen means the harness default, which ATM does not know.
// :unset is the honest answer, not a placeholder to fill in.
func TestSessionActorFallsBackToUnset(t *testing.T) {
	if got := sessionActor("developer", "claude", ""); got != "developer@claude:unset" {
		t.Fatalf("actor = %q", got)
	}
}

// TestSessionEnvIncludesATMValues verifies sessionEnvValues builds the right
// env map (no ATM_BIN / ATM_MANAGER_ACTION / ATM_MANAGER_CAPABILITY).
func TestSessionEnvIncludesATMValues(t *testing.T) {
	got := sessionEnvValues("FOO", "developer@codex:unset", "FOO-RUNID", "/tmp/context.md", "codex", "developer", "developing", "", "", "2026-07-19T00:00:00Z")
	for k, want := range map[string]string{
		"ATM_ROLE":         "developing",
		"ATM_PROJECT":      "FOO",
		"ATM_ACTOR":        "developer@codex:unset",
		"ATM_RUN_ID":       "FOO-RUNID",
		"ATM_TIMESTAMP":    "2026-07-19T00:00:00Z",
		"ATM_CONTEXT_FILE": "/tmp/context.md",
		"ATM_AGENT":        "codex",
		"ATM_PERSONA":      "developer",
	} {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
	for _, gone := range []string{"ATM_BIN", "ATM_MANAGER_ACTION", "ATM_MANAGER_CAPABILITY", "ATM_CAPABILITY", "ATM_TASK"} {
		if _, ok := got[gone]; ok {
			t.Errorf("session env must not set %s", gone)
		}
	}
}

func TestSessionEnvSetsCapabilityAndTaskWhenPresent(t *testing.T) {
	got := sessionEnvValues("FOO", "a", "r", "/c.md", "opencode", "manager", "manager", "scrum", "FOO-12", "2026-07-19T00:00:00Z")
	if got["ATM_CAPABILITY"] != "scrum" || got["ATM_TASK"] != "FOO-12" {
		t.Fatalf("capability/task env = %v", got)
	}
}

// Argv append semantics (formerly appendAgentArgs): base + envArgs +
// extraArgs, in order, no dedup — the host agent's flag parser resolves
// conflicts. Pinned through Compose in TestComposeArgvAppendsInOrder.

func TestContextCachePaths(t *testing.T) {
	if got, want := contextCachePath("/STORE", "FOO", "developer", "", ""), "/STORE/projects/FOO/cache/session-developer.md"; got != want {
		t.Fatalf("contextCachePath developer = %q, want %q", got, want)
	}
	if got, want := contextCachePath("/STORE", "", "concierge", "", ""), "/STORE/cache/session-concierge.md"; got != want {
		t.Fatalf("contextCachePath no-project = %q, want %q", got, want)
	}
	if got, want := contextCachePath("/STORE", "FOO", "Dev-Staff", "", ""), "/STORE/projects/FOO/cache/session-dev-staff.md"; got != want {
		t.Fatalf("contextCachePath normalize = %q, want %q", got, want)
	}
}

// TestCacheKeyWithTask verifies the task id joins the cache key so two
// concurrent sessions on different tasks never share a context file.
func TestCacheKeyWithTask(t *testing.T) {
	if got, want := cacheKey("developer", "ATM-4b7e24", ""), "session-developer-atm-4b7e24"; got != want {
		t.Fatalf("cacheKey = %q, want %q", got, want)
	}
	if got, want := cacheKey("developer", "", ""), "session-developer"; got != want {
		t.Fatalf("cacheKey no-task = %q, want %q", got, want)
	}
}

func TestCacheKeyCapabilitySegment(t *testing.T) {
	if got := cacheKey("concierge", "", "channel"); got != "session-concierge-channel" {
		t.Errorf("cacheKey with capability = %q, want session-concierge-channel", got)
	}
	if got := cacheKey("concierge", "ATM-3714db", "channel"); got != "session-concierge-atm-3714db-channel" {
		t.Errorf("cacheKey with task+capability = %q", got)
	}
	if got := cacheKey("concierge", "", ""); got != "session-concierge" {
		t.Errorf("cacheKey without capability changed: %q", got)
	}
}

// TestComposeArgvAppendsInOrder pins the former appendAgentArgs contract
// through Compose: base argv + DefaultArgs + EnvArgs + ExtraArgs, in order,
// no dedup.
func TestComposeArgvAppendsInOrder(t *testing.T) {
	s := actionService()
	req := actionRequest("scrum-coding")
	req.Task = "ATM-1"
	req.DefaultArgs = []string{"--default"}
	req.EnvArgs = []string{"--yolo"}
	req.ExtraArgs = []string{"--yolo", "--extra"}
	plan, err := s.Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	want := "claude --default --yolo --yolo --extra"
	if got := strings.Join(plan.Argv, " "); got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}
