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
