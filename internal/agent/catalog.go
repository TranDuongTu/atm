// Package agent defines the fixed catalog of host agents ATM can launch and
// computes their live readiness. Entries are selected via `atm agents` and
// consumed by the unified `atm --persona` launcher.
package agent

// Entry is one selectable launch profile. Native harnesses use their own name
// as the launcher; ollama-driven variants set Launcher to "ollama" and name the
// harness ollama drives in Integration.
type Entry struct {
	Name        string
	Launcher    string // opencode | codex | claude | ollama
	Integration string // set iff Launcher == "ollama"
}

var integrations = []string{"opencode", "codex", "claude"}

// Catalog returns the fixed set of supported agents in stable order: the native
// harnesses first, then each ollama-driven variant.
func Catalog() []Entry {
	out := make([]Entry, 0, len(integrations)*2)
	for _, name := range integrations {
		out = append(out, Entry{Name: name, Launcher: name})
	}
	for _, integ := range integrations {
		out = append(out, Entry{Name: "ollama:" + integ, Launcher: "ollama", Integration: integ})
	}
	return out
}

// Lookup returns the catalog entry with the given name.
func Lookup(name string) (Entry, bool) {
	for _, e := range Catalog() {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// PluginAgent is the harness whose plugin backs this entry: the integration for
// ollama entries (ollama has no plugin of its own), else the launcher.
func (e Entry) PluginAgent() string {
	if e.Launcher == "ollama" {
		return e.Integration
	}
	return e.Launcher
}

// Base is the launch base argv for display and launch: the native binary, or
// `ollama launch <integration> --` for ollama entries.
func (e Entry) Base() []string {
	if e.Launcher == "ollama" {
		return []string{"ollama", "launch", e.Integration, "--"}
	}
	return []string{e.Launcher}
}
