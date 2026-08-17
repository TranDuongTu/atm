package agent

import "testing"

func TestSelectionKeyRoundTrip(t *testing.T) {
	cases := []struct {
		key  string
		want Selection
	}{
		{"claude", Selection{Agent: "claude", Launcher: LauncherNative}},
		{"codex", Selection{Agent: "codex", Launcher: LauncherNative}},
		{"ollama:claude", Selection{Agent: "claude", Launcher: LauncherOllama}},
		{"ollama:opencode", Selection{Agent: "opencode", Launcher: LauncherOllama}},
	}
	for _, c := range cases {
		got, err := ParseSelectionKey(c.key)
		if err != nil {
			t.Fatalf("ParseSelectionKey(%q): %v", c.key, err)
		}
		if got != c.want {
			t.Fatalf("ParseSelectionKey(%q) = %+v, want %+v", c.key, got, c.want)
		}
		if back := got.Key(); back != c.key {
			t.Fatalf("Key() = %q, want %q", back, c.key)
		}
	}
}

// The model is NOT part of the key: it is stored beside the selection, not
// inside it, so args and models share one key space.
func TestSelectionKeyIgnoresModel(t *testing.T) {
	s := Selection{Agent: "claude", Launcher: LauncherOllama, Model: "glm-5.2"}
	if s.Key() != "ollama:claude" {
		t.Fatalf("Key() = %q, want ollama:claude", s.Key())
	}
}

func TestParseSelectionKeyRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "ollama:", ":claude", "ollama:claude:extra", "ollama", "nope"} {
		if _, err := ParseSelectionKey(bad); err == nil {
			t.Fatalf("ParseSelectionKey(%q) = nil error, want error", bad)
		}
	}
}
