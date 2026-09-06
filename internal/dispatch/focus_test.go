package dispatch

import (
	"errors"
	"reflect"
	"testing"

	"atm/internal/runtime"
)

func TestFocusTmuxSelectsWindowAndPane(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{}, map[string]bool{"tmux": true}, &calls)
	env.Run = func(argv []string) (string, error) {
		calls = append(calls, argv)
		if len(argv) > 3 && argv[3] == "display-message" {
			return "main", nil
		}
		return "", nil
	}
	err := Focus(env, runtime.Surface{Kind: "tmux", TmuxSocket: "/tmp/tmux-1000/default", TmuxPane: "%30"})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"tmux", "-S", "/tmp/tmux-1000/default", "display-message", "-p", "-t", "%30", "#{session_name}"},
		{"tmux", "-S", "/tmp/tmux-1000/default", "switch-client", "-t", "main"},
		{"tmux", "-S", "/tmp/tmux-1000/default", "select-window", "-t", "%30"},
		{"tmux", "-S", "/tmp/tmux-1000/default", "select-pane", "-t", "%30"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v\nwant  %v", calls, want)
	}
}

func TestFocusTmuxToleratesNoClient(t *testing.T) {
	var calls [][]string
	env := fakeEnv(map[string]string{}, map[string]bool{"tmux": true}, &calls)
	env.Run = func(argv []string) (string, error) {
		calls = append(calls, argv)
		if argv[3] == "switch-client" {
			return "", errors.New("tmux: no current client")
		}
		return "main", nil
	}
	if err := Focus(env, runtime.Surface{Kind: "tmux", TmuxSocket: "/s", TmuxPane: "%1"}); err != nil {
		t.Fatalf("switch-client failing (TUI outside tmux) must not fail the focus: %v", err)
	}
	if len(calls) != 4 {
		t.Fatalf("select-window/select-pane must still run: %v", calls)
	}
}

func TestFocusOtherSurfaces(t *testing.T) {
	cases := []struct {
		s    runtime.Surface
		bins map[string]bool
		want []string
	}{
		{runtime.Surface{Kind: "herdr", HerdrPane: "p7"}, map[string]bool{"herdr": true}, []string{"herdr", "agent", "focus", "p7"}},
		{runtime.Surface{Kind: "kitty", KittyWindow: "42", KittyListen: "unix:/tmp/k"}, map[string]bool{"kitty": true}, []string{"kitty", "@", "--to", "unix:/tmp/k", "focus-window", "--match", "id:42"}},
		{runtime.Surface{Kind: "wezterm", WeztermPane: "9"}, map[string]bool{"wezterm": true}, []string{"wezterm", "cli", "activate-pane", "--pane-id", "9"}},
	}
	for _, c := range cases {
		var calls [][]string
		if err := Focus(fakeEnv(map[string]string{}, c.bins, &calls), c.s); err != nil {
			t.Fatalf("%s: %v", c.s.Kind, err)
		}
		if len(calls) != 1 || !reflect.DeepEqual(calls[0], c.want) {
			t.Fatalf("%s: calls = %v, want %v", c.s.Kind, calls, c.want)
		}
	}
}

func TestFocusUnsupportedAndMissingBinary(t *testing.T) {
	var calls [][]string
	if err := Focus(fakeEnv(nil, nil, &calls), runtime.Surface{Kind: "terminal", TTY: "/dev/pts/3"}); !errors.Is(err, ErrFocusUnsupported) {
		t.Fatalf("terminal: err = %v, want ErrFocusUnsupported", err)
	}
	if err := Focus(fakeEnv(nil, nil, &calls), runtime.Surface{Kind: "tmux", TmuxSocket: "/s", TmuxPane: "%1"}); err == nil || errors.Is(err, ErrFocusUnsupported) {
		t.Fatalf("tmux without binary must be a plain error, got %v", err)
	}
	if err := Focus(fakeEnv(nil, map[string]bool{"tmux": true}, &calls), runtime.Surface{Kind: "tmux"}); err == nil {
		t.Fatal("tmux surface without a pane id must error")
	}
	if len(calls) != 0 {
		t.Fatalf("nothing must run on refusal: %v", calls)
	}
}
