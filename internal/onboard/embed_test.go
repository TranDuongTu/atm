package onboard

import (
	"strings"
	"testing"
)

func TestRenderLatestSubstitutesAllPlaceholders(t *testing.T) {
	data := Data{
		Code:          "FOO",
		Name:          "Foo Project",
		ATMBin:        "/usr/local/bin/atm",
		Actor:         "opencode-onboard",
		RunID:         "FOO-20260704121530-a1b2c3",
		Timestamp:     "2026-07-04T12:15:30Z",
		ExistingTasks: "| FOO-0001 | existing task | status:open |",
	}
	got, err := Render(Latest, data)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, placeholder := range []string{
		"<CODE>", "<PROJECT_NAME>", "<ATM_BIN>", "<ACTOR>",
		"<RUN_ID>", "<TIMESTAMP>", "<EXISTING_TASKS>",
	} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered output still contains placeholder %q", placeholder)
		}
	}
	for _, want := range []string{
		"FOO", "Foo Project", "/usr/local/bin/atm", "opencode-onboard",
		"FOO-20260704121530-a1b2c3", "2026-07-04T12:15:30Z",
		"| FOO-0001 | existing task | status:open |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered output missing %q", want)
		}
	}
}

func TestRenderUnknownVersionErrors(t *testing.T) {
	if _, err := Render("vNonexistent", Data{}); err == nil {
		t.Fatal("expected error for unknown version, got nil")
	}
}