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

	tgt, err := Detect(Config{}, fakeEnv(all, bins, &calls))
	if err != nil || tgt.Name() != "herdr" {
		t.Fatalf("herdr must win: %v %v", tgt, err)
	}
	noHerdr := map[string]string{"TMUX": "x", "KITTY_LISTEN_ON": "u"}
	tgt, err = Detect(Config{}, fakeEnv(noHerdr, bins, &calls))
	if err != nil || tgt.Name() != "tmux" {
		t.Fatalf("tmux must be second: %v %v", tgt, err)
	}
	kittyOnly := map[string]string{"KITTY_LISTEN_ON": "u"}
	tgt, err = Detect(Config{}, fakeEnv(kittyOnly, bins, &calls))
	if err != nil || tgt.Name() != "terminal" {
		t.Fatalf("terminal must be last: %v %v", tgt, err)
	}
	if _, err = Detect(Config{}, fakeEnv(map[string]string{}, map[string]bool{}, &calls)); err == nil {
		t.Fatal("nothing available must error")
	} else if !strings.Contains(err.Error(), "terminal_cmd") {
		t.Fatalf("error must name terminal_cmd: %v", err)
	}
}

func TestHerdrDetectionViaSocketPath(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{"HERDR_SOCKET_PATH": "/tmp/h.sock"}, map[string]bool{"herdr": true}, &calls)
	tgt, err := Detect(Config{}, env)
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
