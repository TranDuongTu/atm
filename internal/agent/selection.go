package agent

import (
	"fmt"
	"strings"
)

// Launcher is who starts the harness: the harness itself, or ollama serving
// a model to it. It is an axis of the selection, not an agent — the ollama
// binary being on PATH is one global fact shared by every agent row.
type Launcher string

const (
	LauncherNative Launcher = "native"
	LauncherOllama Launcher = "ollama"
)

// Selection is the user's stored host-agent choice: which harness, who
// launches it, and which model. An empty Model means the harness's own
// default, which ATM does not know and must not invent.
type Selection struct {
	Agent    string
	Launcher Launcher
	Model    string
}

// Key is the serialized (agent, launcher) pair. It is deliberately the exact
// string agents.json has always stored in "selected" and keyed "args" by, so
// this reshape needs no migration. Model is NOT part of the key.
func (s Selection) Key() string {
	if s.Launcher == LauncherOllama {
		return "ollama:" + s.Agent
	}
	return s.Agent
}

// ParseSelectionKey reads both stored forms. Unknown harness names are
// rejected here so a hand-edited agents.json fails loudly at the boundary
// rather than producing a Selection nothing downstream can launch.
func ParseSelectionKey(key string) (Selection, error) {
	name, launcher := key, LauncherNative
	if rest, ok := strings.CutPrefix(key, "ollama:"); ok {
		name, launcher = rest, LauncherOllama
	}
	if !isHarness(name) {
		return Selection{}, fmt.Errorf("unknown agent selection %q", key)
	}
	return Selection{Agent: name, Launcher: launcher}, nil
}

func isHarness(name string) bool {
	for _, h := range Harnesses() {
		if h.Name == name {
			return true
		}
	}
	return false
}

// TEMPORARY — deleted in Task 2 when catalog.go defines Harnesses().
func Harnesses() []Harness { return []Harness{{Name: "claude"}, {Name: "codex"}, {Name: "opencode"}} }

type Harness struct{ Name string }
