package tui

import (
	"encoding/json"
	"testing"
	"time"

	"atm/internal/core"
)

func feedEntry(action string, payload string) core.LogEntry {
	e := core.LogEntry{Action: action}
	if payload != "" {
		e.Payload = json.RawMessage(payload)
	}
	return e
}

func TestShortEventID(t *testing.T) {
	if got := shortEventID("sha256:84fbf586004add7e"); got != "84fbf58" {
		t.Fatalf("shortEventID = %q, want 84fbf58", got)
	}
	if got := shortEventID(""); got != "" {
		t.Fatalf("shortEventID(v1) = %q, want empty", got)
	}
}

func TestCompactAge(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		at   time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "now"},
		{now.Add(-5 * time.Minute), "5m"},
		{now.Add(-3 * time.Hour), "3h"},
		{now.Add(-49 * time.Hour), "2d"},
		{time.Time{}, ""},
	}
	for _, c := range cases {
		if got := compactAge(c.at, now); got != c.want {
			t.Errorf("compactAge(%v) = %q, want %q", c.at, got, c.want)
		}
	}
}

func TestEventFeedActor(t *testing.T) {
	cases := []struct{ in, want string }{
		{"developer@claude:opus-4.8", "dev@clau"},
		{"admin@tui:unset", "adm@tui"},
		{"manager@ollama:unset", "man@olla"},
		{"default", "def"}, // v1-era actor without @
	}
	for _, c := range cases {
		if got := eventFeedActor(c.in); got != c.want {
			t.Errorf("eventFeedActor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEventFeedSubject(t *testing.T) {
	task := core.LogEntry{Subject: core.Subject{Kind: "task", ID: "ATM-90171b"}}
	if got := eventFeedSubject(task, "ATM"); got != "90171b" {
		t.Fatalf("task subject = %q, want 90171b", got)
	}
	comment := core.LogEntry{Subject: core.Subject{Kind: "comment", ID: "ATM-90171b-cdf66"}}
	if got := eventFeedSubject(comment, "ATM"); got != "90171b" {
		t.Fatalf("comment subject = %q, want parent task alias 90171b", got)
	}
	project := core.LogEntry{Subject: core.Subject{Kind: "project", Code: "ATM"}}
	if got := eventFeedSubject(project, "ATM"); got != "–" {
		t.Fatalf("project subject = %q, want –", got)
	}
}

func TestFeedLabel(t *testing.T) {
	if got := feedLabel("ATM:status:done", "ATM"); got != "done" {
		t.Fatalf("status label = %q, want bare value done", got)
	}
	if got := feedLabel("ATM:type:bug", "ATM"); got != "type:bug" {
		t.Fatalf("non-status label = %q, want type:bug", got)
	}
}

func TestEventDigestMessage(t *testing.T) {
	cases := []struct {
		action, payload, want string
	}{
		{"task.created", `{"title":"Fix the cache"}`, `created "Fix the cache"`},
		{"task.created", `{}`, "created"}, // v1-upgrade payloads may lack keys
		{"task.title-changed", `{"title":"New title"}`, `retitled "New title"`},
		{"task.description-changed", "", "description edited"},
		{"task.label-added", `{"label":"ATM:status:done"}`, "+done"},
		{"task.label-removed", `{"label":"ATM:status:in-progress"}`, "−in-progress"},
		{"task.label-added", `{"labels":["x"]}`, "+label"}, // v1 snapshot payload
		{"task.removed", "", "removed"},
		{"task.meta-changed", "", "meta"},
		{"comment.created", "", "commented"},
		{"comment.body-changed", "", "comment edited"},
		{"comment.label-added", `{"label":"ATM:comment:decision"}`, "comment +comment:decision"},
		{"comment.removed", "", "comment removed"},
		{"label.upserted", `{"name":"ATM:status:open"}`, "label status:open"},
		{"project.created", "", "project created"},
		{"project.name-changed", `{"name":"Acme"}`, `renamed "Acme"`},
		{"project.capability-enabled", `{"capability":"contextmap"}`, "+contextmap"},
		{"project.capability-disabled", `{"capability":"contextmap"}`, "−contextmap"},
	}
	for _, c := range cases {
		e := feedEntry(c.action, c.payload)
		if got := eventDigestMessage(e, "ATM"); got != c.want {
			t.Errorf("%s: digest = %q, want %q", c.action, got, c.want)
		}
	}
	// label.removed names its subject, not a payload field.
	e := core.LogEntry{Action: "label.removed", Subject: core.Subject{Kind: "label", Name: "ATM:status:open"}}
	if got := eventDigestMessage(e, "ATM"); got != "−label status:open" {
		t.Errorf("label.removed digest = %q, want −label status:open", got)
	}
}
