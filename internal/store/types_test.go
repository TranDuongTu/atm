package store

import (
	"encoding/json"
	"testing"
)

func TestTaskHasNoV1Fields(t *testing.T) {
	raw, _ := json.Marshal(Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "x"})
	var dec map[string]any
	_ = json.Unmarshal(raw, &dec)
	for _, banned := range []string{"status", "claim", "links", "todos", "followups", "discussions", "history"} {
		if _, ok := dec[banned]; ok {
			t.Fatalf("Task JSON must not contain %q, got %s", banned, raw)
		}
	}
	if _, ok := dec["labels"]; !ok {
		t.Fatalf("Task JSON must contain labels")
	}
}

func TestProjectHasNoV1Fields(t *testing.T) {
	raw, _ := json.Marshal(Project{Code: "ATM", Name: "x"})
	var dec map[string]any
	_ = json.Unmarshal(raw, &dec)
	for _, banned := range []string{"type_axis", "guide", "repo_paths", "guide_freshness_threshold", "labels", "history", "next_history_n"} {
		if _, ok := dec[banned]; ok {
			t.Fatalf("Project JSON must not contain %q, got %s", banned, raw)
		}
	}
}
