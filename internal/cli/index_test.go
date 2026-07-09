package cli

import "testing"

func TestGoldenIndexModelsEmpty(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "tester")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "tester")
	out, _, code := h.run("index", "--store", sp, "models", "--project", "FOO", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "index-models-empty", out)
}

func TestGoldenIndexStatusEmpty(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "tester")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "tester")
	out, _, code := h.run("index", "--store", sp, "status", "--project", "FOO", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "index-status-empty", out)
}

func TestGoldenIndexReindexNoConfigErrUsage(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "tester")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "tester")
	_, _, code := h.run("index", "--store", sp, "reindex", "--project", "FOO")
	if code != ExitUsage {
		t.Errorf("exit=%d, want %d (no embedding config)", code, ExitUsage)
	}
}

func TestGoldenIndexTopLevelNoConfigErrUsage(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "tester")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "tester")
	_, _, code := h.run("index", "--store", sp, "--project", "FOO")
	if code != ExitUsage {
		t.Errorf("exit=%d, want %d (no embedding config)", code, ExitUsage)
	}
}
