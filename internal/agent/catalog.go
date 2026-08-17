// Package agent defines the host harnesses ATM can launch, the launchers
// that start them, and their live readiness.
package agent

// Harness is one host agent ATM can launch. Plugin names the developing/
// manager plugin backing it — shared by both launchers, because ollama
// launching claude still runs the claude plugin.
type Harness struct {
	Name   string
	Plugin string
}

// Harnesses is the real table: three agents, each with one plugin. The
// launcher is a separate axis (see Launcher), and whether ollama is on PATH
// is ONE global fact — not three.
func Harnesses() []Harness {
	return []Harness{
		{Name: "opencode", Plugin: "opencode"},
		{Name: "codex", Plugin: "codex"},
		{Name: "claude", Plugin: "claude"},
	}
}

func LookupHarness(name string) (Harness, bool) {
	for _, h := range Harnesses() {
		if h.Name == name {
			return h, true
		}
	}
	return Harness{}, false
}

// PluginAgent is the harness whose plugin backs this selection.
func (s Selection) PluginAgent() string { return s.Agent }

// Base is the launch base argv: the native binary, or `ollama launch <agent> --`.
func (s Selection) Base() []string {
	if s.Launcher == LauncherOllama {
		return []string{"ollama", "launch", s.Agent, "--"}
	}
	return []string{s.Agent}
}

// LauncherBinary is the binary that must be on PATH to launch s.
func (s Selection) LauncherBinary() string {
	if s.Launcher == LauncherOllama {
		return "ollama"
	}
	return s.Agent
}

// --- Legacy shim -----------------------------------------------------------
// Entry and Catalog are the pre-reshape flat view. They are derived from
// Harnesses x Launcher and kept because init, dispatch, and the agents CLI
// still read them. Delete once every consumer takes a Selection.

type Entry struct {
	Name        string
	Launcher    string
	Integration string
}

func Catalog() []Entry {
	hs := Harnesses()
	out := make([]Entry, 0, len(hs)*2)
	for _, h := range hs {
		out = append(out, Entry{Name: h.Name, Launcher: h.Name})
	}
	for _, h := range hs {
		out = append(out, Entry{Name: "ollama:" + h.Name, Launcher: "ollama", Integration: h.Name})
	}
	return out
}

func Lookup(name string) (Entry, bool) {
	for _, e := range Catalog() {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

func (e Entry) PluginAgent() string {
	if e.Launcher == "ollama" {
		return e.Integration
	}
	return e.Launcher
}

func (e Entry) Base() []string {
	if e.Launcher == "ollama" {
		return []string{"ollama", "launch", e.Integration, "--"}
	}
	return []string{e.Launcher}
}

// Selection converts a legacy entry to the new shape.
func (e Entry) Selection() Selection {
	if e.Launcher == "ollama" {
		return Selection{Agent: e.Integration, Launcher: LauncherOllama}
	}
	return Selection{Agent: e.Launcher, Launcher: LauncherNative}
}
