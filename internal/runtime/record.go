// Package runtime is the machine-local registry of atm-launched agent
// sessions: one JSON record per run under <store>/runtime/sessions/. The
// launcher creates and ends records, agent-plugin hooks stamp status into
// them, and the TUI and `atm session` read them. It knows nothing about
// tmux, herdr, the store, or the event log — those live in dispatch and
// store; this package is a leaf.
package runtime

import (
	"crypto/rand"
	"fmt"
	"path/filepath"
	"time"
)

// Schema is the record format version. A reader that meets a record with a
// higher schema reports it as unreadable instead of failing, so a dev build
// never breaks the installed TUI (spec §4.2).
const Schema = 1

// State is what the session says it is doing (spec §4.3).
type State string

const (
	StateWorking  State = "working"
	StateIdle     State = "idle"
	StateBlocked  State = "blocked"
	StateWatching State = "watching"
	StateEnded    State = "ended"
)

// ValidState reports whether s is one of the five states.
func ValidState(s string) bool {
	switch State(s) {
	case StateWorking, StateIdle, StateBlocked, StateWatching, StateEnded:
		return true
	}
	return false
}

// Liveness is derived at read time and never stored: live when the launcher
// pid is alive and the record is not ended; lost when the pid is dead and
// the record is not ended; ended when ended_at is set; unreadable when the
// schema is newer than this binary knows.
type Liveness string

const (
	LiveLive       Liveness = "live"
	LiveLost       Liveness = "lost"
	LiveEnded      Liveness = "ended"
	LiveUnreadable Liveness = "unreadable"
)

// Surface is where the session's terminal lives. Kind decides which locator
// fields are meaningful (spec §4.2, §8).
type Surface struct {
	Kind        string `json:"kind"` // tmux | herdr | kitty | wezterm | terminal
	TmuxSocket  string `json:"tmux_socket,omitempty"`
	TmuxPane    string `json:"tmux_pane,omitempty"`
	HerdrPane   string `json:"herdr_pane,omitempty"`
	HerdrSocket string `json:"herdr_socket,omitempty"`
	KittyWindow string `json:"kitty_window,omitempty"`
	KittyListen string `json:"kitty_listen,omitempty"`
	WeztermPane string `json:"wezterm_pane,omitempty"`
	TTY         string `json:"tty,omitempty"`
}

// Status is the last thing anyone said about the session.
type Status struct {
	State  State  `json:"state"`
	Text   string `json:"text,omitempty"`
	At     string `json:"at"`
	Source string `json:"source"` // launcher | hook | herdr
}

// Watch is the residual-agent block: what the session is polling (spec §6.2).
type Watch struct {
	Channel    string `json:"channel"`
	LastPollAt string `json:"last_poll_at,omitempty"`
	NextPollAt string `json:"next_poll_at,omitempty"`
	ItemsSeen  int    `json:"items_seen"`
}

// Record is one run. Identity fields are copied from the compose plan so the
// record is self-describing without the store.
type Record struct {
	Schema      int     `json:"schema"`
	RunID       string  `json:"run_id"`
	Project     string  `json:"project,omitempty"`
	Task        string  `json:"task,omitempty"`
	Persona     string  `json:"persona"`
	Checklist   string  `json:"checklist,omitempty"`
	Mode        string  `json:"mode,omitempty"`
	Capability  string  `json:"capability,omitempty"`
	Agent       string  `json:"agent"`
	Model       string  `json:"model,omitempty"`
	Actor       string  `json:"actor"`
	LauncherPID int     `json:"launcher_pid"`
	AtmBin      string  `json:"atm_bin,omitempty"`
	Host        string  `json:"host,omitempty"`
	Cwd         string  `json:"cwd,omitempty"`
	ContextPath string  `json:"context_path,omitempty"`
	StartedAt   string  `json:"started_at"`
	EndedAt     string  `json:"ended_at,omitempty"`
	ExitCode    *int    `json:"exit_code,omitempty"`
	Surface     Surface `json:"surface"`
	Status      Status  `json:"status"`
	Watch       *Watch  `json:"watch,omitempty"`
}

// NewRunID mints <CODE>-<YYYYMMDDHHMMSS>-<6 hex>. It moved here from
// internal/cli so the TUI can pre-mint one for the dispatch toast.
func NewRunID(code string) string {
	var b [3]byte
	suffix := "000000"
	if _, err := rand.Read(b[:]); err == nil {
		suffix = fmt.Sprintf("%x", b[:])
	}
	return fmt.Sprintf("%s-%s-%s", code, time.Now().UTC().Format("20060102150405"), suffix)
}

// ContextDir is where a project's per-run context files live; the
// store-level dir when code is empty (project-optional personas).
func ContextDir(storePath, code string) string {
	if code == "" {
		return filepath.Join(storePath, "cache", "sessions")
	}
	return filepath.Join(storePath, "projects", code, "cache", "sessions")
}

// ContextPath is the per-run context file (spec §5.2).
func ContextPath(storePath, code, runID string) string {
	return filepath.Join(ContextDir(storePath, code), runID+".md")
}
