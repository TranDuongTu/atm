package tui

import (
	"fmt"
	"strings"
	"time"

	"atm/internal/activity"
	"atm/internal/actor"
	"atm/internal/core"
	"github.com/NimbleMarkets/ntcharts/linechart"
	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/lipgloss"
)

type chartRangeSpec struct {
	key        string
	label      string
	buckets    int
	bucketDays int
}

var chartRanges = []chartRangeSpec{
	{key: "1w", label: "One week", buckets: 7, bucketDays: 1},
	{key: "1m", label: "One month", buckets: 30, bucketDays: 1},
	{key: "3m", label: "Three months", buckets: 13, bucketDays: 7},
	{key: "6m", label: "Six months", buckets: 26, bucketDays: 7},
	{key: "1y", label: "One year", buckets: 52, bucketDays: 7},
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

func relXLabelFormatter(end time.Time) linechart.LabelFormatter {
	endDay := end.UTC().Truncate(24 * time.Hour)
	return func(_ int, value float64) string {
		day := time.Unix(int64(value), 0).UTC().Truncate(24 * time.Hour)
		return relDayLabel(int(endDay.Sub(day) / (24 * time.Hour)))
	}
}

func renderActivityPulse(counts []int, spec chartRangeSpec, width, height int, end time.Time, graph, axis, label lipgloss.Style) string {
	if width < 12 || height < 2 || len(counts) == 0 {
		return ""
	}

	start, endDay := chartWindow(spec, end)
	maxCount := 0
	for _, count := range counts {
		if count > maxCount {
			maxCount = count
		}
	}
	if maxCount == 0 {
		maxCount = 1
	}

	chart := timeserieslinechart.New(
		width,
		height,
		timeserieslinechart.WithTimeRange(start, endDay),
		timeserieslinechart.WithYRange(0, float64(maxCount)),
		timeserieslinechart.WithXYSteps(4, 2),
		timeserieslinechart.WithXLabelFormatter(relXLabelFormatter(end)),
		timeserieslinechart.WithAxesStyles(axis, label),
		timeserieslinechart.WithStyle(graph),
	)
	for i, count := range counts {
		chart.Push(timeserieslinechart.TimePoint{
			Time:  start.AddDate(0, 0, i*spec.bucketDays),
			Value: float64(count),
		})
	}
	chart.DrawBraille()
	return chart.View()
}

type carouselEntry struct {
	key   string
	count int
}

type personaCardEntry struct {
	key    string
	count  int
	models map[string]int
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

func personaCardEntries(entries []core.LogEntry, groups []activity.Group, spec chartRangeSpec, end time.Time) []personaCardEntry {
	cards := make([]personaCardEntry, 0, len(groups)+1)
	all := aggregateWindow(entries, "", spec, end)
	cards = append(cards, personaCardEntry{key: "", count: all.Count, models: all.Models})
	for _, group := range groups {
		agg := aggregateWindow(entries, group.Key, spec, end)
		cards = append(cards, personaCardEntry{key: group.Key, count: agg.Count, models: agg.Models})
	}
	return cards
}

func activityCountLabel(count int) string {
	if count == 1 {
		return "1 activity"
	}
	return fmt.Sprintf("%d activities", count)
}

func topModelLabel(models map[string]int, limit int) string {
	rows := make([]kvRow, 0, len(models))
	for key, count := range models {
		rows = append(rows, kvRow{k: key, v: count})
	}
	sortKV(rows)
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, row.k)
	}
	if len(parts) == 0 {
		return "no models"
	}
	return strings.Join(parts, ", ")
}

func renderPersonaCardRows(cards []personaCardEntry, selected string, focused bool, width int, st Styles) []string {
	if width <= 0 || len(cards) == 0 {
		return nil
	}
	selected = selectedPersonaCard(cards, selected)
	if width < 16 {
		return []string{fitLine(carouselLabel(selected), width)}
	}

	const gap = 1
	visible := (width + gap) / (16 + gap)
	if visible < 1 {
		visible = 1
	}
	if visible > len(cards) {
		visible = len(cards)
	}
	selectedIdx := personaCardIndex(cards, selected)
	start := selectedIdx - visible/2
	if start < 0 {
		start = 0
	}
	if maxStart := len(cards) - visible; start > maxStart {
		start = maxStart
	}

	cardW := (width - (visible-1)*gap) / visible
	if cardW < 8 {
		return []string{fitLine(carouselLabel(selected), width)}
	}
	used := cardW*visible + (visible-1)*gap
	leftPad := (width - used) / 2
	rows := make([]string, 5)
	for i := range rows {
		rows[i] = spaces(leftPad)
	}
	for i := 0; i < visible; i++ {
		if i > 0 {
			for row := range rows {
				rows[row] += spaces(gap)
			}
		}
		cardLines := renderPersonaCard(cards[start+i], selected, focused, cardW, st)
		for row := range rows {
			rows[row] += cardLines[row]
		}
	}
	for i := range rows {
		rows[i] = fitLine(rows[i], width)
	}
	return rows
}

func renderPersonaMiniCardRows(cards []personaCardEntry, selected string, focused bool, width int, st Styles) []string {
	if width <= 0 || len(cards) == 0 {
		return nil
	}
	selected = selectedPersonaCard(cards, selected)
	if width < 16 {
		return []string{fitLine(carouselLabel(selected), width)}
	}

	const gap = 1
	visible := (width + gap) / (18 + gap)
	if visible < 1 {
		visible = 1
	}
	if visible > len(cards) {
		visible = len(cards)
	}
	selectedIdx := personaCardIndex(cards, selected)
	start := selectedIdx - visible/2
	if start < 0 {
		start = 0
	}
	if maxStart := len(cards) - visible; start > maxStart {
		start = maxStart
	}

	cardW := (width - (visible-1)*gap) / visible
	if cardW < 10 {
		return []string{fitLine(carouselLabel(selected), width)}
	}
	used := cardW*visible + (visible-1)*gap
	leftPad := (width - used) / 2
	rows := []string{spaces(leftPad), spaces(leftPad), spaces(leftPad)}
	for i := 0; i < visible; i++ {
		if i > 0 {
			for row := range rows {
				rows[row] += spaces(gap)
			}
		}
		cardLines := renderPersonaMiniCard(cards[start+i], selected, focused, cardW, st)
		for row := range rows {
			rows[row] += cardLines[row]
		}
	}
	for i := range rows {
		rows[i] = fitLine(rows[i], width)
	}
	return rows
}

func selectedPersonaCard(cards []personaCardEntry, key string) string {
	for _, card := range cards {
		if card.key == key {
			return key
		}
	}
	return ""
}

func personaCardIndex(cards []personaCardEntry, key string) int {
	for i, card := range cards {
		if card.key == key {
			return i
		}
	}
	return 0
}

func renderPersonaCard(card personaCardEntry, selected string, focused bool, width int, st Styles) []string {
	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	border := st.Muted
	body := st.Muted
	if focused && card.key == selected {
		border = st.HeaderLabel
		body = st.PaneActiveStrong
	}
	lines := []string{
		"╭" + repeat("─", innerW) + "╮",
		"│" + padDisplay(fitLine(carouselLabel(card.key), innerW), innerW) + "│",
		"│" + padDisplay(fitLine(activityCountLabel(card.count), innerW), innerW) + "│",
		"│" + padDisplay(fitLine(topModelLabel(card.models, 3), innerW), innerW) + "│",
		"╰" + repeat("─", innerW) + "╯",
	}
	for i, line := range lines {
		if i == 0 || i == len(lines)-1 {
			lines[i] = border.Render(line)
		} else {
			lines[i] = border.Render(line[:3]) + body.Render(line[3:len(line)-3]) + border.Render(line[len(line)-3:])
		}
	}
	return lines
}

func renderPersonaMiniCard(card personaCardEntry, selected string, focused bool, width int, st Styles) []string {
	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	border := st.Muted
	body := st.Muted
	if focused && card.key == selected {
		border = st.HeaderLabel
		body = st.PaneActiveStrong
	}
	label := fitLine(carouselLabel(card.key)+" · "+activityCountLabel(card.count)+" · "+topModelLabel(card.models, 3), innerW)
	lines := []string{
		"╭" + repeat("─", innerW) + "╮",
		"│" + padDisplay(label, innerW) + "│",
		"╰" + repeat("─", innerW) + "╯",
	}
	lines[0] = border.Render(lines[0])
	lines[1] = border.Render(lines[1][:3]) + body.Render(lines[1][3:len(lines[1])-3]) + border.Render(lines[1][len(lines[1])-3:])
	lines[2] = border.Render(lines[2])
	return lines
}

func renderCarouselCompact(entries []carouselEntry, selected string, width int, st Styles) string {
	if width <= 0 || len(entries) == 0 {
		return ""
	}
	selected = carouselSelected(entries, selected)
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		label := carouselLabel(entry.key)
		if entry.key == selected {
			parts = append(parts, st.PaneActiveStrong.Render("["+label+"]"))
		} else {
			parts = append(parts, st.Muted.Render(label))
		}
	}
	return fitLine(strings.Join(parts, " "), width)
}
