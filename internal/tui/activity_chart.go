package tui

import (
	"fmt"
	"strings"
	"time"

	"atm/internal/activity"
	"atm/internal/actor"
	"atm/internal/core"
	"github.com/charmbracelet/lipgloss"
)

type chartRangeSpec struct {
	key        string
	buckets    int
	bucketDays int
}

var chartRanges = []chartRangeSpec{
	{key: "1w", buckets: 7, bucketDays: 1},
	{key: "1m", buckets: 30, bucketDays: 1},
	{key: "3m", buckets: 13, bucketDays: 7},
	{key: "6m", buckets: 26, bucketDays: 7},
	{key: "1y", buckets: 52, bucketDays: 7},
}

func chartWindow(spec chartRangeSpec, end time.Time) (start, endDay time.Time) {
	endDay = end.UTC().Truncate(24 * time.Hour)
	if spec.buckets <= 0 || spec.bucketDays <= 0 {
		return endDay, endDay
	}
	return endDay.AddDate(0, 0, -(spec.buckets*spec.bucketDays - 1)), endDay
}

func activityBucketCounts(entries []core.LogEntry, persona string, spec chartRangeSpec, end time.Time) []int {
	if spec.buckets <= 0 || spec.bucketDays <= 0 {
		return nil
	}
	counts := make([]int, spec.buckets)
	start, endDay := chartWindow(spec, end)
	for _, entry := range entries {
		if entry.At.IsZero() {
			continue
		}
		identity := actor.Resolve(entry.Actor)
		if persona != "" && identity.Persona != persona {
			continue
		}
		day := entry.At.UTC().Truncate(24 * time.Hour)
		if day.Before(start) || day.After(endDay) {
			continue
		}
		bucket := int(day.Sub(start) / (24 * time.Hour) / time.Duration(spec.bucketDays))
		if bucket >= 0 && bucket < len(counts) {
			counts[bucket]++
		}
	}
	return counts
}

func aggregateWindow(entries []core.LogEntry, persona string, spec chartRangeSpec, end time.Time) activity.Group {
	group := activity.Group{
		Key:     persona,
		Agents:  map[string]int{},
		Models:  map[string]int{},
		Actions: map[string]int{},
	}
	start, endDay := chartWindow(spec, end)
	for _, entry := range entries {
		if entry.At.IsZero() {
			continue
		}
		identity := actor.Resolve(entry.Actor)
		if persona != "" && identity.Persona != persona {
			continue
		}
		day := entry.At.UTC().Truncate(24 * time.Hour)
		if day.Before(start) || day.After(endDay) {
			continue
		}
		group.Count++
		if identity.Agent != "" {
			group.Agents[identity.Agent]++
		}
		if identity.Model != "" {
			group.Models[identity.Model]++
		}
		if entry.Action != "" {
			group.Actions[entry.Action]++
		}
	}
	return group
}

func relDayLabel(days int) string {
	if days <= 0 {
		return "Today"
	}
	if days >= 364 {
		return fmt.Sprintf("%dy ago", (days+182)/364)
	}
	if days >= 30 {
		return fmt.Sprintf("%dm ago", (days+15)/30)
	}
	if days%7 == 0 {
		return fmt.Sprintf("%dw ago", days/7)
	}
	return fmt.Sprintf("%dd ago", days)
}

type carouselEntry struct {
	key   string
	count int
}

func carouselEntries(groups []activity.Group) []carouselEntry {
	entries := make([]carouselEntry, 0, len(groups)+1)
	total := 0
	for _, group := range groups {
		total += group.Count
	}
	entries = append(entries, carouselEntry{count: total})
	for _, group := range groups {
		entries = append(entries, carouselEntry{key: group.Key, count: group.Count})
	}
	return entries
}

func carouselSelected(entries []carouselEntry, key string) string {
	for _, entry := range entries {
		if entry.key == key {
			return key
		}
	}
	return ""
}

func carouselIndex(entries []carouselEntry, key string) int {
	for i, entry := range entries {
		if entry.key == key {
			return i
		}
	}
	return 0
}

func carouselStep(entries []carouselEntry, key string, dir int) string {
	if len(entries) == 0 {
		return ""
	}
	idx := carouselIndex(entries, carouselSelected(entries, key))
	idx = (idx + dir%len(entries) + len(entries)) % len(entries)
	return entries[idx].key
}

const (
	iconDeveloper = "\U0001F6E0"
	iconConcierge = "\U0001F9ED"
	iconManager   = "\U0001F4BC"
	iconAdmin     = "\u2699"
	iconAll       = "\u2733"
	iconPersona   = "\U0001F464"
)

func personaIcon(key string) string {
	switch key {
	case "":
		return iconAll
	case "developer":
		return iconDeveloper
	case "concierge":
		return iconConcierge
	case "manager":
		return iconManager
	case "admin":
		return iconAdmin
	default:
		return iconPersona
	}
}

func carouselName(key string) string {
	if key == "" {
		return "All"
	}
	return key
}

func carouselLabel(key string) string {
	return personaIcon(key) + " " + carouselName(key)
}

func renderCarouselLines(entries []carouselEntry, selected string, width int, st Styles) []string {
	if width <= 0 {
		return []string{"", "", ""}
	}
	selected = carouselSelected(entries, selected)
	selectedBox := st.PaneActiveStrong.
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Render(carouselLabel(selected))
	boxLines := strings.Split(selectedBox, "\n")
	for len(boxLines) < 3 {
		boxLines = append(boxLines, "")
	}
	if len(boxLines) > 3 {
		boxLines = boxLines[:3]
	}
	boxWidth := lipgloss.Width(boxLines[1])
	if boxWidth > width {
		return []string{fitLine(boxLines[0], width), fitLine(boxLines[1], width), fitLine(boxLines[2], width)}
	}
	leftWidth := (width - boxWidth) / 2
	rightWidth := width - boxWidth - leftWidth
	left, right := carouselNeighbors(entries, selected, leftWidth, rightWidth, st)
	return []string{
		fitLine(spaces(leftWidth)+boxLines[0]+spaces(rightWidth), width),
		fitLine(left+boxLines[1]+right+spaces(rightWidth-lipgloss.Width(right)), width),
		fitLine(spaces(leftWidth)+boxLines[2]+spaces(rightWidth), width),
	}
}

func carouselNeighbors(entries []carouselEntry, selected string, leftWidth, rightWidth int, st Styles) (string, string) {
	idx := carouselIndex(entries, selected)
	left := spaces(leftWidth)
	right := ""
	for distance := 1; distance < len(entries); distance++ {
		if distance%2 == 1 {
			key := entries[(idx-distance+len(entries))%len(entries)].key
			label := st.Muted.Render(carouselName(key))
			if lipgloss.Width(label) < leftWidth {
				left = spaces(leftWidth-lipgloss.Width(label)) + label
			}
		} else {
			key := entries[(idx+distance)%len(entries)].key
			label := st.Muted.Render(carouselName(key))
			if lipgloss.Width(right)+lipgloss.Width(label) < rightWidth {
				right += label
			}
		}
	}
	return left, right
}

func renderCarouselCompact(entries []carouselEntry, selected string, width int, st Styles) string {
	if width <= 0 || len(entries) == 0 {
		return ""
	}
	selected = carouselSelected(entries, selected)
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		label := carouselName(entry.key)
		if entry.key == selected {
			parts = append(parts, st.PaneActiveStrong.Render("["+label+"]"))
		} else {
			parts = append(parts, st.Muted.Render(label))
		}
	}
	return fitLine(strings.Join(parts, " "), width)
}
