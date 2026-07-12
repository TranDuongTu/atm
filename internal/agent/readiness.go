package agent

import "atm/internal/developing"

// Readiness captures whether an agent entry can be launched: whether its
// launcher binary is on PATH and whether its harness plugin is installed.
type Readiness struct {
	launcher      string
	MissingBin    bool
	MissingPlugin bool
}

// Ready reports whether the entry is fully installed and launchable.
func (r Readiness) Ready() bool { return !r.MissingBin && !r.MissingPlugin }

// Status computes live readiness for e: lookPath resolves the launcher binary
// and developing.PluginStatus reports the integration plugin's state. home is
// the user's home dir (where plugins install).
func Status(e Entry, home string, lookPath func(string) (string, error)) Readiness {
	r := Readiness{launcher: e.Launcher}
	if _, err := lookPath(e.Launcher); err != nil {
		r.MissingBin = true
	}
	if developing.PluginStatus(e.PluginAgent(), home).State != "installed" {
		r.MissingPlugin = true
	}
	return r
}

// String renders a short human-readable status. The binary label names ollama
// explicitly, since an ollama entry's integration plugin may be present while
// the ollama binary itself is missing.
func (r Readiness) String() string {
	bin := "needs binary"
	if r.launcher == "ollama" {
		bin = "needs ollama binary"
	}
	switch {
	case r.Ready():
		return "ready"
	case r.MissingBin && r.MissingPlugin:
		return bin + " + plugin"
	case r.MissingBin:
		return bin
	default:
		return "needs plugin (atm init)"
	}
}
