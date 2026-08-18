package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"atm/internal/activity"
	"atm/internal/core"
	"github.com/charmbracelet/lipgloss"
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

func TestPersonaIcons(t *testing.T) {
	tests := map[string]string{
		"developer": "\U0001F6E0",
		"concierge": "\U0001F9ED",
		"manager":   "\U0001F4BC",
		"admin":     "\u2699",
		"":          "\u2733",
		"custom":    "\U0001F464",
	}
	for key, want := range tests {
		if got := personaIcon(key); got != want {
			t.Errorf("personaIcon(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestCarouselEntriesPutAllFirstWithTotalCount(t *testing.T) {
	groups := []activity.Group{
		{Key: "manager", Count: 3},
		{Key: "developer", Count: 2},
	}
	want := []carouselEntry{
		{key: "", count: 5},
		{key: "manager", count: 3},
		{key: "developer", count: 2},
	}
	if got := carouselEntries(groups); !reflect.DeepEqual(got, want) {
		t.Fatalf("carouselEntries() = %#v, want %#v", got, want)
	}
}

func TestCarouselSelectionNavigationAndFallback(t *testing.T) {
	entries := []carouselEntry{{key: "", count: 5}, {key: "manager", count: 3}, {key: "developer", count: 2}}
	if got := carouselSelected(entries, "missing"); got != "" {
		t.Fatalf("carouselSelected(missing) = %q, want All", got)
	}
	if got := carouselStep(entries, "", -1); got != "developer" {
		t.Fatalf("carouselStep(All, left) = %q, want developer", got)
	}
	if got := carouselStep(entries, "developer", 1); got != "" {
		t.Fatalf("carouselStep(developer, right) = %q, want All", got)
	}
	if got := carouselIndex(entries, "manager"); got != 1 {
		t.Fatalf("carouselIndex(manager) = %d, want 1", got)
	}
}

func TestCarouselNames(t *testing.T) {
	if got := carouselName(""); got != "All" {
		t.Fatalf("carouselName(All) = %q, want All", got)
	}
	if got := carouselName("developer"); got != "developer" {
		t.Fatalf("carouselName(developer) = %q, want developer", got)
	}
}

func TestRenderCarouselCompactBracketsSelectedLabel(t *testing.T) {
	entries := []carouselEntry{{key: "", count: 5}, {key: "developer", count: 2}}
	got := renderCarouselCompact(entries, "developer", 30, buildStyles(themeGraphite))
	if !strings.Contains(got, "["+personaIcon("developer")+" developer]") {
		t.Fatalf("renderCarouselCompact() = %q, want bracketed selected icon label", got)
	}
	if lipgloss.Width(got) > 30 {
		t.Fatalf("compact carousel display width = %d, want <= 30", lipgloss.Width(got))
	}
}

func TestRenderActivityPulse(t *testing.T) {
	end := time.Date(2026, 8, 17, 18, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	got := renderActivityPulse(
		[]int{0, 2, 1, 4, 0, 3, 1},
		chartRanges[0],
		80,
		6,
		end,
		lipgloss.NewStyle(),
		lipgloss.NewStyle(),
		lipgloss.NewStyle(),
	)
	if got == "" {
		t.Fatal("renderActivityPulse() returned empty output for sample counts")
	}
	if lines := strings.Count(got, "\n") + 1; lines != 6 {
		t.Fatalf("renderActivityPulse() returned %d lines, want 6: %q", lines, got)
	}
	for _, r := range got {
		if r >= '\u2800' && r <= '\u28ff' {
			return
		}
	}
	t.Fatalf("renderActivityPulse() = %q, want at least one braille rune", got)
}

func TestRenderActivityPulseNilCountsReturnsEmpty(t *testing.T) {
	got := renderActivityPulse(nil, chartRanges[0], 80, 6, time.Now(), lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle())
	if got != "" {
		t.Fatalf("renderActivityPulse(nil) = %q, want empty output", got)
	}
}

func TestRelXLabelFormatterUsesChartWindowBoundary(t *testing.T) {
	end := time.Date(2026, 8, 17, 18, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	_, endDay := chartWindow(chartRanges[0], end)
	format := relXLabelFormatter(end)
	if got := format(0, float64(endDay.Unix())); got != "Today" {
		t.Fatalf("relXLabelFormatter(end)(window end) = %q, want Today", got)
	}
}

func TestRenderActivityPulseEmptyAndTooSmallReturnsEmpty(t *testing.T) {
	end := time.Date(2026, 8, 17, 18, 30, 0, 0, time.UTC)
	style := lipgloss.NewStyle()
	tests := []struct {
		name          string
		counts        []int
		width, height int
	}{
		{name: "empty non-nil counts", counts: []int{}, width: 80, height: 6},
		{name: "width below minimum", counts: []int{1}, width: 11, height: 6},
		{name: "height below minimum", counts: []int{1}, width: 12, height: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := renderActivityPulse(test.counts, chartRanges[0], test.width, test.height, end, style, style, style); got != "" {
				t.Fatalf("renderActivityPulse() = %q, want empty output", got)
			}
		})
	}
}
