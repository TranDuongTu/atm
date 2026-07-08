package cli

import (
	"io"
	"os"
	"os/exec"
)

// tmuxLabelTUI and tmuxLabelOnboarding are the labels shown in the
// enclosing tmux window while the corresponding CLI surfaces run. They
// are scoped to tmux: outside a tmux session setTmuxWindowLabel is a
// no-op. tmux's automatic-rename and the next command run in the pane
// reassert the usual name, so no restore is emitted on exit.
const (
	tmuxLabelTUI        = "\u2261 ATM TUI"
	tmuxLabelOnboarding = "\u21bb ATM Onboarding"
)

// OSC 2 (set window title) updates tmux's pane title (#T) even when
// allow-rename is off (allow-rename only guards OSC 0 window-NAME
// renames). Most tmux status bars display #T, so OSC 2 is what actually
// surfaces a custom label. OSC 2 wraps the label in "\x1b]2;<s>\x07".
const (
	oscWindowTitlePrefix = "\x1b]2;"
	oscWindowTitleSuffix = "\x07"
)

// setTmuxWindowLabel emits the tmux window/pane label for label to w.
// It is a no-op when the process is not running inside a tmux session
// (detected via the TMUX environment variable). Two mechanisms are used
// together to be robust to either status-bar format:
//
//   - OSC 2 sets the pane title (#T), which most status formats display
//     and which works regardless of the allow-rename option.
//   - `tmux rename-window` updates the window name (#W) for formats that
//     use it; tmux honors this regardless of allow-rename.
//
// Both are best-effort; failures are silently ignored to keep the CLI
// behavior identical outside tmux.
func setTmuxWindowLabel(w io.Writer, label string) {
	if os.Getenv("TMUX") == "" {
		return
	}
	io.WriteString(w, oscWindowTitlePrefix+label+oscWindowTitleSuffix)
	if bin, err := exec.LookPath("tmux"); err == nil {
		cmd := exec.Command(bin, "rename-window", label)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
		_ = cmd.Run()
	}
}