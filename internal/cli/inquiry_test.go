package cli

import "testing"

func TestGoldenInquiryAdd(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	out, _, code := h.run("inquiry", "--store", sp, "add", "--project", "FOO", "--query", "label conflicts", "--cited", "ATM-0001,ATM-0002", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "inquiry-add", out)
}

func TestGoldenInquiryAddNoActor(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	_, _, code := h.run("inquiry", "--store", sp, "add", "--project", "FOO", "--query", "q", "--cited", "ATM-0001")
	if code != 0 {
		t.Errorf("exit=%d, want 0 (inquiry add does not require --actor)", code)
	}
}
