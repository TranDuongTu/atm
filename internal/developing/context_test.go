package developing

import (
	"strings"
	"testing"
)

func TestRenderContextSubstitutesAllPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{
		Code:      "FOO",
		Name:      "Foo Project",
		ATMBin:    "/usr/local/bin/atm",
		Actor:     "codex-dev",
		RunID:     "FOO-20260705120000-a1b2c3",
		Timestamp: "2026-07-05T12:00:00Z",
	})
	for _, placeholder := range []string{
		"<CODE>", "<PROJECT_NAME>", "<ATM_BIN>", "<ACTOR>",
		"<RUN_ID>", "<TIMESTAMP>",
	} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered context still contains %s", placeholder)
		}
	}
	for _, want := range []string{
		"ATM developing session FOO-20260705120000-a1b2c3",
		"Project: `FOO` (`Foo Project`)",
		"ATM binary: `/usr/local/bin/atm`",
		"Actor: `codex-dev`",
		"atm task comment add --task <ID>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q", want)
		}
	}
}