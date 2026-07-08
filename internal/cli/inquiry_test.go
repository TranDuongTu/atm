package cli

import "testing"

func TestGoldenInquiryAdd(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "tester")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "tester")
	out, _, code := h.run("inquiry", "--store", sp, "add", "--project", "FOO", "--query", "label conflicts", "--cited", "ATM-0001,ATM-0002", "--actor", "tester", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "inquiry-add", out)
}

func TestGoldenInquiryAddRequiresActor(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "tester")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "tester")
	_, _, code := h.run("inquiry", "--store", sp, "add", "--project", "FOO", "--query", "q", "--cited", "ATM-0001")
	if code != ExitUsage {
		t.Errorf("exit=%d, want %d (missing actor)", code, ExitUsage)
	}
}
