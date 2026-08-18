package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// With no chat model configured there is nothing to generate with, but the
// hits must still arrive and the exit code must still be 0: a script parses
// one shape whether or not ollama is up.
func TestGoldenAskDegradedWithoutChatModel(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "FOO", "--title", "label resolver", "--description", "the resolver walks the hierarchy", "--actor", "admin@cli:unset")
	out, _, code := h.run("ask", "how does the label resolver work?", "--store", sp, "--project", "FOO", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d, want 0 — a missing chat model degrades, it does not fail; stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "ask-degraded", out)
}

// Every key present, always: an agent must be able to index the document
// without existence checks.
func TestAskJSONAlwaysCarriesEveryKey(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	out, _, _ := h.run("ask", "anything", "--store", sp, "--project", "FOO", "--output", "json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	for _, k := range []string{"answer", "citations", "hits", "chat_model", "embed_model", "session", "behind", "degraded", "truncated", "error"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("key %q missing from %s", k, out)
		}
	}
	if !strings.Contains(out, `"citations": []`) && !strings.Contains(out, `"citations":[]`) {
		t.Errorf("citations must marshal as [] and never null: %s", out)
	}
}

// An empty question is a malformed call, not a broken answer.
func TestAskWithEmptyQuestionIsUsage(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	_, _, code := h.run("ask", "   ", "--store", sp, "--project", "FOO", "--output", "json")
	if code != ExitUsage {
		t.Errorf("exit=%d, want %d", code, ExitUsage)
	}
}
