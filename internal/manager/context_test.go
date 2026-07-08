package manager

import (
	"strings"
	"testing"
)

func TestRenderContextSubstitutesAllPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{
		Code:      "ATM",
		Name:      "Agent Tasks Management",
		ATMBin:    "/usr/local/bin/atm",
		Actor:     "opencode-manager",
		RunID:     "ATM-20260706120000-a1b2c3",
		Timestamp: "2026-07-06T12:00:00Z",
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
		"ATM manager session ATM-20260706120000-a1b2c3",
		"Project: `ATM` (`Agent Tasks Management`)",
		"ATM binary: `/usr/local/bin/atm`",
		"Actor: `opencode-manager`",
		"knowledge-base owner",
		"ubiquitous language",
		"vocabulary.json",
		"onboarding",
		"ATM_ONBOARD",
		"needs clarification",
		"cross-project",
		"atm vocabulary write",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q", want)
		}
	}
	for _, mustNot := range []string{
		"agent-generated keyword bubbles pending",
	} {
		if strings.Contains(got, mustNot) {
			t.Errorf("rendered context should not contain %q", mustNot)
		}
	}
}

func TestRenderContextOnboardingSectionIsEnvConditional(t *testing.T) {
	got := RenderContext(ContextData{Code: "ATM", ATMBin: "/bin/atm"})
	if !strings.Contains(got, "ATM_ONBOARD") {
		t.Errorf("onboarding section must reference ATM_ONBOARD as its activation signal")
	}
	if !strings.Contains(got, "onboarding") {
		t.Errorf("onboarding responsibility must be present in the prompt")
	}
}

func TestRenderContextGenericKeepsPlaceholders(t *testing.T) {
	// The env-driven generic body is produced by leaving placeholders in place
	// so the subagent resolves them from env at dispatch time. RenderContext
	// with empty fields must NOT strip placeholders.
	got := RenderContext(ContextData{})
	for _, placeholder := range []string{"<CODE>", "<ATM_BIN>", "<ACTOR>"} {
		if !strings.Contains(got, placeholder) {
			t.Errorf("generic render stripped %s; placeholders must survive for env-driven use", placeholder)
		}
	}
}
