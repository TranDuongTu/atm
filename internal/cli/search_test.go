package cli

import "testing"

func TestGoldenSearchTextFallback(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "FOO", "--title", "label resolver refactor", "--actor", "admin@cli:unset")
	out, _, code := h.run("search", "--store", sp, "--project", "FOO", "--model", "m", "--query-vector", "[]", "label resolver", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "search-text-fallback", out)
}

func TestGoldenSearchPureQueryVectorEmpty(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	out, _, code := h.run("search", "--store", sp, "--project", "FOO", "--model", "m", "--query-vector", "[0.1,0.2]", "q", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "search-pure-empty", out)
}
