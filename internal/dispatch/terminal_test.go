package dispatch

import (
	"reflect"
	"strings"
	"testing"
)

func TestTemplateTargetSubstitutes(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{}, map[string]bool{}, &calls)
	tgt, ok := terminalTarget(Config{TerminalCmd: "kitty @ launch --type=tab --cwd {dir} --tab-title {title} -- {cmd}"}, env)
	if !ok {
		t.Fatal("config template must always yield a target")
	}
	spec := Spec{Title: "ATM · manager", Argv: []string{"atm", "--persona", "manager"}, Dir: "/w"}
	if err := tgt.Spawn(spec); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"sh", "-c", `kitty @ launch --type=tab --cwd '/w' --tab-title 'ATM · manager' -- 'atm' '--persona' 'manager'`}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestEmulatorDetection(t *testing.T) {
	var calls [][]string
	// kitty fingerprint + binary present.
	env := fakeEnv(map[string]string{"KITTY_LISTEN_ON": "unix:/tmp/k"}, map[string]bool{"kitty": true}, &calls)
	tgt, ok := terminalTarget(Config{}, env)
	if !ok || !strings.Contains(tgt.Describe(), "kitty") {
		t.Fatalf("kitty must be detected, got ok=%v %v", ok, tgt)
	}
	// Fingerprint without binary: skipped.
	env = fakeEnv(map[string]string{"KITTY_LISTEN_ON": "unix:/tmp/k"}, map[string]bool{}, &calls)
	if _, ok := terminalTarget(Config{}, env); ok {
		t.Fatal("fingerprint without binary must not detect")
	}
	// Nothing detected, no template.
	env = fakeEnv(map[string]string{}, map[string]bool{}, &calls)
	if _, ok := terminalTarget(Config{}, env); ok {
		t.Fatal("no emulator and no template must fail")
	}
}

func TestEmulatorSpawnArgv(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{"WEZTERM_UNIX_SOCKET": "/tmp/w"}, map[string]bool{"wezterm": true}, &calls)
	tgt, ok := terminalTarget(Config{}, env)
	if !ok {
		t.Fatal("wezterm must be detected")
	}
	spec := Spec{Title: "ATM · manager", Argv: []string{"atm", "--persona", "manager"}, Dir: "/w"}
	if err := tgt.Spawn(spec); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"wezterm", "cli", "spawn", "--cwd", "/w", "--", "atm", "--persona", "manager"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestConfigTemplateWinsOverDetection(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{"KITTY_LISTEN_ON": "u"}, map[string]bool{"kitty": true}, &calls)
	tgt, _ := terminalTarget(Config{TerminalCmd: "echo {cmd}"}, env)
	if tgt.Describe() != "terminal · configured command" {
		t.Fatalf("template must win over detection, got %q", tgt.Describe())
	}
}
