package agent

import (
	"reflect"
	"testing"
)

func TestCatalogEntries(t *testing.T) {
	names := map[string]Entry{}
	for _, e := range Catalog() {
		names[e.Name] = e
	}
	for _, want := range []string{"opencode", "codex", "claude", "ollama:opencode", "ollama:codex", "ollama:claude"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("catalog missing %q", want)
		}
	}
	if len(Catalog()) != 6 {
		t.Fatalf("expected 6 catalog entries, got %d", len(Catalog()))
	}
}

func TestLookupAndDerivations(t *testing.T) {
	e, ok := Lookup("ollama:opencode")
	if !ok {
		t.Fatal("expected ollama:opencode in catalog")
	}
	if e.Launcher != "ollama" || e.Integration != "opencode" {
		t.Fatalf("bad entry: %+v", e)
	}
	if e.PluginAgent() != "opencode" {
		t.Fatalf("PluginAgent = %q", e.PluginAgent())
	}
	if got := e.Base(); !reflect.DeepEqual(got, []string{"ollama", "launch", "opencode", "--"}) {
		t.Fatalf("Base = %v", got)
	}

	n, _ := Lookup("codex")
	if n.PluginAgent() != "codex" {
		t.Fatalf("native PluginAgent = %q", n.PluginAgent())
	}
	if got := n.Base(); !reflect.DeepEqual(got, []string{"codex"}) {
		t.Fatalf("native Base = %v", got)
	}

	if _, ok := Lookup("gemini"); ok {
		t.Fatal("gemini should not be in catalog")
	}
}

func TestHarnessesAreTheRealTable(t *testing.T) {
	hs := Harnesses()
	if len(hs) != 3 {
		t.Fatalf("expected 3 harnesses, got %d: %+v", len(hs), hs)
	}
	byName := map[string]Harness{}
	for _, h := range hs {
		byName[h.Name] = h
	}
	for _, want := range []string{"opencode", "codex", "claude"} {
		h, ok := byName[want]
		if !ok {
			t.Fatalf("harnesses missing %q", want)
		}
		if h.Plugin != want {
			t.Fatalf("%s plugin = %q, want %q", want, h.Plugin, want)
		}
	}
}

// The six-entry catalog must remain EXACTLY what it was, because init,
// dispatch, and the agents CLI still read it. This pins the shim.
func TestCatalogShimMatchesLegacyExactly(t *testing.T) {
	want := []string{"opencode", "codex", "claude", "ollama:opencode", "ollama:codex", "ollama:claude"}
	got := make([]string, 0, len(Catalog()))
	for _, e := range Catalog() {
		got = append(got, e.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Catalog() names = %v, want %v (order is load-bearing: init prints a numbered menu)", got, want)
	}
}

func TestSelectionDerivations(t *testing.T) {
	ollama := Selection{Agent: "opencode", Launcher: LauncherOllama}
	if got := ollama.Base(); !reflect.DeepEqual(got, []string{"ollama", "launch", "opencode", "--"}) {
		t.Fatalf("ollama Base = %v", got)
	}
	if ollama.PluginAgent() != "opencode" {
		t.Fatalf("ollama PluginAgent = %q", ollama.PluginAgent())
	}
	native := Selection{Agent: "codex", Launcher: LauncherNative}
	if got := native.Base(); !reflect.DeepEqual(got, []string{"codex"}) {
		t.Fatalf("native Base = %v", got)
	}
	if native.PluginAgent() != "codex" {
		t.Fatalf("native PluginAgent = %q", native.PluginAgent())
	}
}
