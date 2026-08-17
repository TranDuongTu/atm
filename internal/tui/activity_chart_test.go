package tui

import (
	"reflect"
	"testing"
	"time"

	"atm/internal/core"
)

func TestActivityBucketCountsDailyWindow(t *testing.T) {
	end := time.Date(2026, 8, 17, 18, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	entries := []core.LogEntry{
		{At: time.Date(2026, 8, 11, 23, 59, 0, 0, time.UTC), Actor: "developer@codex:gpt-5", Action: "oldest"},
		{At: time.Date(2026, 8, 12, 0, 1, 0, 0, time.UTC), Actor: "developer@codex:gpt-5", Action: "start"},
		{At: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC), Actor: "developer@codex:gpt-5", Action: "end"},
		{At: time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC), Actor: "developer@codex:gpt-5", Action: "outside"},
	}

	got := activityBucketCounts(entries, "", chartRanges[0], end)
	want := []int{1, 1, 0, 0, 0, 0, 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("activityBucketCounts() = %#v, want %#v", got, want)
	}
}

func TestActivityBucketCountsFiltersPersonaAndPlacesWeeklyEntries(t *testing.T) {
	end := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	entries := []core.LogEntry{
		{At: time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC), Actor: "developer@codex:gpt-5"},
		{At: time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC), Actor: "manager@claude:opus"},
		{At: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), Actor: "developer@codex:gpt-5"},
		{At: time.Date(2026, 8, 17, 23, 0, 0, 0, time.UTC), Actor: "developer@codex:gpt-5"},
	}

	got := activityBucketCounts(entries, "developer", chartRanges[2], end)
	want := make([]int, 13)
	want[0] = 1
	want[1] = 1
	want[12] = 1
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("activityBucketCounts() = %#v, want %#v", got, want)
	}
}

func TestAggregateWindowCountsModelsForPersonaAndAll(t *testing.T) {
	end := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	entries := []core.LogEntry{
		{At: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC), Actor: "developer@codex:gpt-5", Action: "task.create"},
		{At: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC), Actor: "developer@codex:gpt-5", Action: "task.update"},
		{At: time.Date(2026, 8, 17, 13, 0, 0, 0, time.UTC), Actor: "manager@claude:opus", Action: "task.create"},
		{At: time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC), Actor: "developer@codex:gpt-5", Action: "outside"},
	}

	persona := aggregateWindow(entries, "developer", chartRanges[0], end)
	if persona.Count != 2 || !reflect.DeepEqual(persona.Models, map[string]int{"gpt-5": 2}) {
		t.Fatalf("persona aggregate = %#v, want count 2 and model gpt-5:2", persona)
	}

	all := aggregateWindow(entries, "", chartRanges[0], end)
	if all.Count != 3 || !reflect.DeepEqual(all.Models, map[string]int{"gpt-5": 2, "opus": 1}) {
		t.Fatalf("all aggregate = %#v, want count 3 and models gpt-5:2, opus:1", all)
	}
}

func TestRelDayLabel(t *testing.T) {
	tests := map[int]string{
		0:   "Today",
		3:   "3d ago",
		7:   "1w ago",
		14:  "2w ago",
		30:  "1m ago",
		91:  "3m ago",
		182: "6m ago",
		364: "1y ago",
	}
	for days, want := range tests {
		if got := relDayLabel(days); got != want {
			t.Errorf("relDayLabel(%d) = %q, want %q", days, got, want)
		}
	}
}
