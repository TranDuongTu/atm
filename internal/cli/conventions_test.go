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
	if !strings.Contains(out, "What ATM is") {
		t.Fatalf("expected 'What ATM is' in text output: %s", out)
	}
	if !strings.Contains(out, "Agent code-of-conduct") {
		t.Fatalf("expected 'Agent code-of-conduct' in text output")
	}
	if !strings.Contains(out, "read every label's description first") {
		t.Fatalf("expected 'read every label's description first' in text output")
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
	if !strings.Contains(out, `"code_of_conduct"`) {
		t.Fatalf("expected 'code_of_conduct' key in JSON output")
	}
	if !strings.Contains(out, `"seeded_labels"`) {
		t.Fatalf("expected 'seeded_labels' key in JSON output")
	}
	if !strings.Contains(out, `"how_to_search"`) {
		t.Fatalf("expected 'how_to_search' key in JSON output")
	}
	compareGolden(t, "conventions-json", out)
}
