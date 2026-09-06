package dispatch

import (
	"errors"
	"fmt"

	"atm/internal/runtime"
)

// ErrFocusUnsupported: the surface has no remote focus API (a bare tty,
// alacritty, gnome-terminal, konsole, foot). The caller shows the locator.
var ErrFocusUnsupported = errors.New("focus: surface has no remote focus API")

// Focus brings a recorded surface to the front (spec ATM-9339a7 §8). It is
// keyed by the surface's own kind, not by Detect: the TUI may be in tmux
// while the target is a kitty tab. Every command runs through env so tests
// assert exact argv.
func Focus(env Env, s runtime.Surface) error {
	switch s.Kind {
	case "tmux":
		if s.TmuxPane == "" || s.TmuxSocket == "" {
			return fmt.Errorf("focus: tmux surface without socket or pane id")
		}
		if _, err := env.LookPath("tmux"); err != nil {
			return fmt.Errorf("focus: tmux not on PATH")
		}
		tmux := func(args ...string) (string, error) {
			return env.Run(append([]string{"tmux", "-S", s.TmuxSocket}, args...))
		}
		// switch-client moves THIS client to the target's session; it fails
		// harmlessly when the caller is not a tmux client (TUI outside
		// tmux), in which case select-window still changes what attached
		// clients see.
		if session, err := tmux("display-message", "-p", "-t", s.TmuxPane, "#{session_name}"); err == nil && session != "" {
			_, _ = tmux("switch-client", "-t", session)
		}
		if _, err := tmux("select-window", "-t", s.TmuxPane); err != nil {
			return fmt.Errorf("focus: %w", err)
		}
		if _, err := tmux("select-pane", "-t", s.TmuxPane); err != nil {
			return fmt.Errorf("focus: %w", err)
		}
		return nil
	case "herdr":
		if s.HerdrPane == "" {
			return fmt.Errorf("focus: herdr surface without pane id")
		}
		if _, err := env.LookPath("herdr"); err != nil {
			return fmt.Errorf("focus: herdr not on PATH")
		}
		_, err := env.Run([]string{"herdr", "agent", "focus", s.HerdrPane})
		return err
	case "kitty":
		if s.KittyWindow == "" {
			return fmt.Errorf("focus: kitty surface without window id")
		}
		if _, err := env.LookPath("kitty"); err != nil {
			return fmt.Errorf("focus: kitty not on PATH")
		}
		argv := []string{"kitty", "@"}
		if s.KittyListen != "" {
			argv = append(argv, "--to", s.KittyListen)
		}
		_, err := env.Run(append(argv, "focus-window", "--match", "id:"+s.KittyWindow))
		return err
	case "wezterm":
		if s.WeztermPane == "" {
			return fmt.Errorf("focus: wezterm surface without pane id")
		}
		if _, err := env.LookPath("wezterm"); err != nil {
			return fmt.Errorf("focus: wezterm not on PATH")
		}
		_, err := env.Run([]string{"wezterm", "cli", "activate-pane", "--pane-id", s.WeztermPane})
		return err
	}
	return ErrFocusUnsupported
}
