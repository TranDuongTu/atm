package dispatch

import (
	"reflect"
	"strings"
	"testing"
)

func TestDetectPrecedence(t *testing.T) {
	var calls [][]string
	all := map[string]string{"HERDR_ENV": "1", "TMUX": "x", "KITTY_LISTEN_ON": "u"}
	bins := map[string]bool{"herdr": true, "tmux": true, "kitty": true}

	tgt, err := Detect(Config{}, fakeEnv(all, bins, &calls), "")
	if err != nil || tgt.Name() != "herdr" {
		t.Fatalf("herdr must win: %v %v", tgt, err)
	}
	noHerdr := map[string]string{"TMUX": "x", "KITTY_LISTEN_ON": "u"}
	tgt, err = Detect(Config{}, fakeEnv(noHerdr, bins, &calls), "")
	if err != nil || tgt.Name() != "tmux" {
		t.Fatalf("tmux must be second: %v %v", tgt, err)
	}
	kittyOnly := map[string]string{"KITTY_LISTEN_ON": "u"}
	tgt, err = Detect(Config{}, fakeEnv(kittyOnly, bins, &calls), "")
	if err != nil || tgt.Name() != "terminal" {
		t.Fatalf("terminal must be last: %v %v", tgt, err)
	}
	if _, err = Detect(Config{}, fakeEnv(map[string]string{}, map[string]bool{}, &calls), ""); err == nil {
		t.Fatal("nothing available must error")
	} else if !strings.Contains(err.Error(), "terminal_cmd") {
		t.Fatalf("error must name terminal_cmd: %v", err)
	}
}

func TestHerdrDetectionViaSocketPath(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{"HERDR_SOCKET_PATH": "/tmp/h.sock"}, map[string]bool{"herdr": true}, &calls)
	tgt, err := Detect(Config{}, env, "")
	if err != nil || tgt.Name() != "herdr" {
		t.Fatalf("HERDR_SOCKET_PATH must detect herdr: %v %v", tgt, err)
	}
}

func TestHerdrSpawnTwoStep(t *testing.T) {
	calls := [][]string{}
	env := Env{
		Getenv:   func(k string) string { return map[string]string{"HERDR_ENV": "1"}[k] },
		LookPath: func(string) (string, error) { return "/usr/bin/herdr", nil },
		Run: func(argv []string) (string, error) {
			calls = append(calls, argv)
			if argv[1] == "pane" && argv[2] == "split" {
				return "w1:p3", nil
			}
			return "", nil
		},
	}
	spec := Spec{Title: "ATM · developer · ATM-1", Argv: []string{"atm", "--persona", "developer"}, Dir: "/w"}
	if err := (herdrTarget{env: env}).Spawn(spec); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"herdr", "pane", "split", "--direction", "down", "--cwd", "/w"},
		{"herdr", "pane", "rename", "w1:p3", "ATM · developer · ATM-1"},
		{"herdr", "pane", "run", "w1:p3", `'atm' '--persona' 'developer'`},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestTmuxWinsOverConfigTemplate(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{"TMUX": "x"}, map[string]bool{"tmux": true}, &calls)
	tgt, err := Detect(Config{TerminalCmd: "echo {cmd}"}, env, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tgt.Name() != "tmux" {
		t.Fatalf("tmux must win over configured terminal_cmd, got %q", tgt.Name())
	}
}

func TestDetectForcedTarget(t *testing.T) {
	var calls [][]string
	noEnv := fakeEnv(map[string]string{}, map[string]bool{}, &calls)

	// Forcing herdr bypasses detection — should succeed even with no env.
	tgt, err := Detect(Config{}, noEnv, "herdr")
	if err != nil {
		t.Fatalf("forced herdr must not error on absence: %v", err)
	}
	if tgt.Name() != "herdr" {
		t.Fatalf("forced herdr name = %q", tgt.Name())
	}

	// Forcing tmux.
	tgt, err = Detect(Config{}, noEnv, "tmux")
	if err != nil || tgt.Name() != "tmux" {
		t.Fatalf("forced tmux: %v %v", tgt, err)
	}

	// Forcing terminal without any terminal available must error.
	_, err = Detect(Config{}, noEnv, "terminal")
	if err == nil {
		t.Fatal("forced terminal with no terminal available must error")
	}
	if !strings.Contains(err.Error(), "no terminal target") {
		t.Fatalf("forced terminal error must say no terminal target: %v", err)
	}

	// Forcing an unknown target.
	_, err = Detect(Config{}, noEnv, "nope")
	if err == nil || !strings.Contains(err.Error(), "unknown dispatch target") {
		t.Fatalf("unknown target must error: %v", err)
	}
}

func TestDetectForceTerminalWithConfig(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{}, map[string]bool{}, &calls)
	tgt, err := Detect(Config{TerminalCmd: "kitty @ launch -- {cmd}"}, env, "terminal")
	if err != nil {
		t.Fatalf("forced terminal with config must succeed: %v", err)
	}
	if tgt.Name() != "terminal" {
		t.Fatalf("forced terminal name = %q", tgt.Name())
	}
}
