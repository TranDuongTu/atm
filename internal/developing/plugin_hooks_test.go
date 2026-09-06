package developing

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeAtm is a PATH shim that appends its argv to a log file, so a hook can
// be run for real without an atm binary or a store.
func fakeAtm(t *testing.T) (dir, log string) {
	t.Helper()
	dir = t.TempDir()
	log = filepath.Join(dir, "calls.log")
	script := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> " + log + "\n"
	if err := os.WriteFile(filepath.Join(dir, "atm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, log
}

func runHook(t *testing.T, agent, state, stdin string, env map[string]string) (string, error) {
	t.Helper()
	assets, ok := PluginAssets(agent)
	if !ok {
		t.Fatalf("no assets for %s", agent)
	}
	var script Asset
	for _, a := range assets {
		if a.Path == "hooks/session-status" {
			script = a
		}
	}
	if script.Path == "" {
		t.Fatalf("%s plugin has no hooks/session-status asset", agent)
	}
	if script.Mode != 0o755 {
		t.Fatalf("hooks/session-status mode = %o, want 755", script.Mode)
	}
	dir, log := fakeAtm(t)
	p := filepath.Join(dir, "session-status")
	_ = os.WriteFile(p, script.Content, 0o755)
	cmd := exec.Command(p, state)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = []string{"PATH=" + dir + ":" + os.Getenv("PATH"), "HOME=" + dir}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if _, err := cmd.CombinedOutput(); err != nil {
		return "", err
	}
	b, _ := os.ReadFile(log)
	return strings.TrimSpace(string(b)), nil
}

func TestSessionStatusHookStampsState(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		calls, err := runHook(t, agent, "working", `{"hook_event_name":"UserPromptSubmit"}`, map[string]string{"ATM_RUN_ID": "ATM-1"})
		if err != nil {
			t.Fatalf("%s: %v", agent, err)
		}
		if calls != "session status --state working" {
			t.Fatalf("%s: atm called with %q", agent, calls)
		}
	}
}

func TestSessionStatusHookBlockedCarriesMessage(t *testing.T) {
	calls, err := runHook(t, "claude", "blocked", `{"hook_event_name":"Notification","notification_type":"permission_prompt","message":"Claude needs your permission to use Bash"}`, map[string]string{"ATM_RUN_ID": "ATM-1"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != "session status --state blocked --text Claude needs your permission to use Bash" {
		t.Fatalf("atm called with %q", calls)
	}
}

func TestSessionStatusHookNoRunIDIsSilent(t *testing.T) {
	calls, err := runHook(t, "claude", "idle", `{}`, nil)
	if err != nil {
		t.Fatalf("hook must exit 0 without ATM_RUN_ID: %v", err)
	}
	if calls != "" {
		t.Fatalf("atm must not be called without a run id, got %q", calls)
	}
}

func TestHooksJSONRegistersStatusEvents(t *testing.T) {
	for agent, events := range map[string][]string{
		"claude": {"SessionStart", "UserPromptSubmit", "Stop", "Notification"},
		"codex":  {"SessionStart", "UserPromptSubmit", "Stop", "PermissionRequest"},
	} {
		assets, _ := PluginAssets(agent)
		var raw []byte
		for _, a := range assets {
			if a.Path == "hooks/hooks.json" {
				raw = a.Content
			}
		}
		var doc struct {
			Hooks map[string]json.RawMessage `json:"hooks"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s hooks.json: %v", agent, err)
		}
		for _, ev := range events {
			if _, ok := doc.Hooks[ev]; !ok {
				t.Errorf("%s hooks.json missing %s", agent, ev)
			}
		}
	}
}

func TestOpenCodePluginHandlesEvents(t *testing.T) {
	assets, _ := PluginAssets("opencode")
	var js string
	for _, a := range assets {
		if a.Path == "atm-developing.js" {
			js = string(a.Content)
		}
	}
	for _, want := range []string{`"session.idle"`, `"permission.asked"`, `"chat.message"`, `"session", "status"`, "ATM_RUN_ID"} {
		if !strings.Contains(js, want) {
			t.Errorf("opencode plugin missing %s", want)
		}
	}
}
