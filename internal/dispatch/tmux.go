package dispatch

// tmuxAvailable reports whether we are inside a tmux session and the tmux
// binary resolves.
func tmuxAvailable(env Env) bool {
	if env.Getenv("TMUX") == "" {
		return false
	}
	_, err := env.LookPath("tmux")
	return err == nil
}

type tmuxTarget struct{ env Env }

func (t tmuxTarget) Name() string     { return "tmux" }
func (t tmuxTarget) Describe() string { return "tmux · new window" }

// Spawn opens a named tmux window in dir. tmux runs its shell-command
// argument via the user's shell, so argv is passed as one quoted string.
func (t tmuxTarget) Spawn(s Spec) error {
	_, err := t.env.Run([]string{"tmux", "new-window", "-n", s.Title, "-c", s.Dir, ShellCommand(s.Argv)})
	return err
}
