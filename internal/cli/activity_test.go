package cli

import (
	"strings"
	"testing"
)

func TestActivityGroupsByPersona(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	if _, se, code := h.run("project", "create", "--store", sp, "--code", "AAA", "--name", "A", "--actor", "staff@claude:opus-4.8"); code != 0 {
		t.Fatalf("project create: code=%d stderr=%s", code, se)
	}
	if _, se, code := h.run("task", "create", "--store", sp, "--project", "AAA", "--title", "t", "--actor", "staff@claude:opus-4.8"); code != 0 {
		t.Fatalf("task create: code=%d stderr=%s", code, se)
	}
	out, _, code := h.run("activity", "--store", sp, "--project", "AAA")
	if code != 0 {
		t.Fatalf("activity code=%d", code)
	}
	if !strings.Contains(out, `"key": "staff"`) || !strings.Contains(out, `"claude"`) {
		t.Fatalf("activity = %s", out)
	}
}
