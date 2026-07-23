package dispatch

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fakeEnv builds an Env from a var map and a set of available binaries,
// recording every Run invocation into calls.
func fakeEnv(vars map[string]string, bins map[string]bool, calls *[][]string) Env {
	return Env{
		Getenv: func(k string) string { return vars[k] },
		LookPath: func(bin string) (string, error) {
			if bins[bin] {
				return "/usr/bin/" + bin, nil
			}
			return "", errors.New("not found")
		},
		Run: func(argv []string) (string, error) {
			*calls = append(*calls, argv)
			return "", nil
		},
	}
}

func TestShellCommandQuotes(t *testing.T) {
	got := ShellCommand([]string{"atm", "--persona", "developer", "--task", "it's"})
	want := `'atm' '--persona' 'developer' '--task' 'it'\''s'`
	if got != want {
		t.Fatalf("ShellCommand = %s, want %s", got, want)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	if c, err := LoadConfig(filepath.Join(dir, "absent.json")); err != nil || c.TerminalCmd != "" {
		t.Fatalf("missing file must be zero config, got %+v err %v", c, err)
	}
	p := filepath.Join(dir, "dispatch.json")
	os.WriteFile(p, []byte(`{"terminal_cmd":"kitty @ launch -- {cmd}"}`), 0o644)
	c, err := LoadConfig(p)
	if err != nil || c.TerminalCmd != "kitty @ launch -- {cmd}" {
		t.Fatalf("config = %+v, err %v", c, err)
	}
	os.WriteFile(p, []byte(`{nope`), 0o644)
	if _, err := LoadConfig(p); err == nil {
		t.Fatal("malformed config must error")
	}
}

func TestTmuxAvailability(t *testing.T) {
	var calls [][]string
	if tmuxAvailable(fakeEnv(map[string]string{}, map[string]bool{"tmux": true}, &calls)) {
		t.Fatal("no $TMUX must be unavailable")
	}
	if tmuxAvailable(fakeEnv(map[string]string{"TMUX": "/tmp/t,1,0"}, map[string]bool{}, &calls)) {
		t.Fatal("missing tmux binary must be unavailable")
	}
	if !tmuxAvailable(fakeEnv(map[string]string{"TMUX": "/tmp/t,1,0"}, map[string]bool{"tmux": true}, &calls)) {
		t.Fatal("inside tmux with binary must be available")
	}
}

func TestTmuxSpawnArgv(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{"TMUX": "x"}, map[string]bool{"tmux": true}, &calls)
	spec := Spec{Title: "ATM · manager", Argv: []string{"atm", "--persona", "manager", "--project", "ATM", "--agent", "claude"}, Dir: "/work"}
	if err := (tmuxTarget{env: env}).Spawn(spec); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"tmux", "new-window", "-n", "ATM · manager", "-c", "/work",
		`'atm' '--persona' 'manager' '--project' 'ATM' '--agent' 'claude'`}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}