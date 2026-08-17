package tui

import (
	"fmt"
	"time"

	"atm/internal/activity"
	"atm/internal/actor"
	"atm/internal/core"
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
