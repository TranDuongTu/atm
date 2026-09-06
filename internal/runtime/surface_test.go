package runtime

import (
	"reflect"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDetectSurface(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]string
		want Surface
	}{
		{"herdr wins over tmux", map[string]string{"HERDR_ENV": "1", "HERDR_PANE_ID": "p7", "HERDR_SOCKET_PATH": "/h.sock", "TMUX": "/tmp/t,1,0", "TMUX_PANE": "%3"},
			Surface{Kind: "herdr", HerdrPane: "p7", HerdrSocket: "/h.sock", TTY: "/dev/pts/1"}},
		{"tmux socket is the first field of $TMUX", map[string]string{"TMUX": "/tmp/tmux-1000/default,6099,0", "TMUX_PANE": "%30"},
			Surface{Kind: "tmux", TmuxSocket: "/tmp/tmux-1000/default", TmuxPane: "%30", TTY: "/dev/pts/1"}},
		{"kitty", map[string]string{"KITTY_WINDOW_ID": "42", "KITTY_LISTEN_ON": "unix:/tmp/k"},
			Surface{Kind: "kitty", KittyWindow: "42", KittyListen: "unix:/tmp/k", TTY: "/dev/pts/1"}},
		{"wezterm", map[string]string{"WEZTERM_PANE": "9"},
			Surface{Kind: "wezterm", WeztermPane: "9", TTY: "/dev/pts/1"}},
		{"plain terminal", map[string]string{"TERM": "xterm"},
			Surface{Kind: "terminal", TTY: "/dev/pts/1"}},
		{"herdr env without a pane id is not herdr", map[string]string{"HERDR_ENV": "1", "TMUX": "/s,1,0", "TMUX_PANE": "%1"},
			Surface{Kind: "tmux", TmuxSocket: "/s", TmuxPane: "%1", TTY: "/dev/pts/1"}},
	}
	for _, c := range cases {
		if got := DetectSurface(env(c.vars), "/dev/pts/1"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}
