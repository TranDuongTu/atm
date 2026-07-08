package cli

import (
	"strings"
	"testing"
)

func TestVocabularyShowEmptyStateJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	out, _, code := h.run("vocabulary", "show", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, `"vocabulary"`) || strings.Contains(out, `"terms": ["`) {
		t.Fatalf("empty-state show output should have null vocabulary; got:\n%s", out)
	}
}

func TestVocabularyWriteThenShow(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("vocabulary", "write", "--project", "FOO", "--actor", "opencode-manager",
		"--terms", `[{"term":"labels","weight":9},{"term":"audit log","weight":7}]`)
	if code != ExitSuccess {
		t.Fatalf("write exit = %d, want 0", code)
	}
	h.reset()
	out, _, code := h.run("vocabulary", "show", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("show exit = %d, want 0", code)
	}
	if !strings.Contains(out, `"labels"`) || !strings.Contains(out, `"audit log"`) {
		t.Fatalf("show after write missing terms; got:\n%s", out)
	}
}

func TestVocabularyWriteRejectsMalformedTerms(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("vocabulary", "write", "--project", "FOO", "--actor", "x",
		"--terms", `not json`)
	if code != ExitUsage {
		t.Fatalf("malformed terms exit = %d, want %d (usage)", code, ExitUsage)
	}
}

func TestVocabularyWriteRequiresActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("vocabulary", "write", "--project", "FOO",
		"--terms", `[{"term":"x","weight":1}]`)
	if code != ExitUsage {
		t.Fatalf("no-actor write exit = %d, want %d (usage)", code, ExitUsage)
	}
}

func TestVocabularyShowMissingProject(t *testing.T) {
	h := newGoldenHarness(t)
	h.reset()
	_, _, code := h.run("vocabulary", "show", "--project", "NOPE")
	if code != ExitNotFound {
		t.Fatalf("show missing project exit = %d, want %d", code, ExitNotFound)
	}
}
