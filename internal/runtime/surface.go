package runtime

import "strings"

// DetectSurface reads the launcher's own inherited environment. Precedence
// mirrors dispatch.Detect: herdr, then tmux, then a terminal with a remote
// focus API, then a bare terminal. tty is passed in (the caller knows how to
// read it); "" is fine.
func DetectSurface(getenv func(string) string, tty string) Surface {
	s := Surface{Kind: "terminal", TTY: tty}
	switch {
	case getenv("HERDR_ENV") == "1" && getenv("HERDR_PANE_ID") != "":
		s.Kind, s.HerdrPane, s.HerdrSocket = "herdr", getenv("HERDR_PANE_ID"), getenv("HERDR_SOCKET_PATH")
	case getenv("TMUX") != "" && getenv("TMUX_PANE") != "":
		sock, _, _ := strings.Cut(getenv("TMUX"), ",")
		s.Kind, s.TmuxSocket, s.TmuxPane = "tmux", sock, getenv("TMUX_PANE")
	case getenv("KITTY_WINDOW_ID") != "":
		s.Kind, s.KittyWindow, s.KittyListen = "kitty", getenv("KITTY_WINDOW_ID"), getenv("KITTY_LISTEN_ON")
	case getenv("WEZTERM_PANE") != "":
		s.Kind, s.WeztermPane = "wezterm", getenv("WEZTERM_PANE")
	}
	return s
}
