package cli

import "testing"

func TestGoldenEmbedNoConfigErrUsage(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	_, _, code := h.run("embed", "--store", sp, "--project", "FOO", "--role", "query", "hello")
	if code != ExitUsage {
		t.Errorf("exit=%d, want %d (no embedding config)", code, ExitUsage)
	}
}

func TestGoldenEmbedMissingProjectErrUsage(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	_, _, code := h.run("embed", "--store", sp, "--project", "NOPE", "--role", "query", "hello")
	if code == 0 {
		t.Error("want non-zero exit (missing project), got 0")
	}
}
