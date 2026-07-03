package cli

import (
	"strings"
	"testing"
)

func TestConventionsText(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	out, _, code := h.run("conventions")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, "Suggested seed namespaces") {
		t.Fatalf("expected 'Suggested seed namespaces' in text output: %s", out)
	}
	compareGolden(t, "conventions-text", out)
}

func TestConventionsJSON(t *testing.T) {
	h := newGoldenHarness(t)
	out, _, code := h.run("conventions")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"conventions"`) {
		t.Fatalf("expected 'conventions' key in JSON output: %s", out)
	}
	compareGolden(t, "conventions-json", out)
}
