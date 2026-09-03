package tui

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"atm/internal/capability/qa"
	"atm/internal/capability/scrum"
	"atm/internal/core"
)

func TestNewFlowSocketsScrumExcludesEvictFromClaim(t *testing.T) {
	s := newFlowSockets(scrum.New(), "ATM")
	if !reflect.DeepEqual(s.claimPrefixes, []string{"ATM:scrum:"}) {
		t.Fatalf("claimPrefixes = %#v, want [ATM:scrum:]", s.claimPrefixes)
	}
	if s.finish != "ATM:scrum-stage:done" {
		t.Fatalf("finish = %q", s.finish)
	}
	if s.evictPrefix != "ATM:scrum-out:" {
		t.Fatalf("evictPrefix = %q", s.evictPrefix)
	}
}

func TestNewFlowSocketsQAFinishOnClaimAxis(t *testing.T) {
	s := newFlowSockets(qa.New(), "ATM")
	if !reflect.DeepEqual(s.claimPrefixes, []string{"ATM:qa:"}) {
		t.Fatalf("claimPrefixes = %#v", s.claimPrefixes)
	}
	if s.finish != "ATM:qa:done" {
		t.Fatalf("finish = %q", s.finish)
	}
}

func TestFlowSocketsStateOf(t *testing.T) {
	s := newFlowSockets(scrum.New(), "ATM")
	cases := []struct {
		name   string
		labels []string
		want   flowState
	}{
		{"none", nil, flowNone},
		{"claimed", []string{"ATM:scrum:task"}, flowOpen},
		{"claimed+stage", []string{"ATM:scrum:task", "ATM:scrum-stage:planned"}, flowOpen},
		{"finished", []string{"ATM:scrum:task", "ATM:scrum-stage:done"}, flowFinished},
		{"evicted", []string{"ATM:scrum-out:duplicate"}, flowEvicted},
		{"evict wins over finish", []string{"ATM:scrum-stage:done", "ATM:scrum-out:not-worth-it"}, flowEvicted},
		{"stage without claim is not open", []string{"ATM:scrum-stage:planned"}, flowNone},
	}
	for _, c := range cases {
		m := map[string]bool{}
		for _, l := range c.labels {
			m[l] = true
		}
		if got := s.stateOf(m); got != c.want {
			t.Errorf("%s: stateOf = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFlowSocketsQAStateOfDoneOnClaimAxis(t *testing.T) {
	s := newFlowSockets(qa.New(), "ATM")
	if got := s.stateOf(map[string]bool{"ATM:qa:done": true}); got != flowFinished {
		t.Fatalf("qa:done alone = %v, want flowFinished", got)
	}
	if got := s.stateOf(map[string]bool{"ATM:qa:testing": true}); got != flowOpen {
		t.Fatalf("qa:testing = %v, want flowOpen", got)
	}
}

func TestFlowSocketsRelevant(t *testing.T) {
	s := newFlowSockets(scrum.New(), "ATM")
	for _, l := range []string{"ATM:scrum:task", "ATM:scrum-stage:done", "ATM:scrum-out:duplicate"} {
		if !s.relevant(l) {
			t.Errorf("relevant(%q) = false", l)
		}
	}
	for _, l := range []string{"ATM:qa:testing", "ATM:scrum-stage:planned"} {
		if s.relevant(l) {
			t.Errorf("relevant(%q) = true", l)
		}
	}
}

func labelEvent(at time.Time, action, task, label string) core.LogEntry {
	return core.LogEntry{
		At:      at,
		Actor:   "developer@claude:test",
		Action:  action,
		Subject: core.Subject{Kind: "task", ID: task},
		Payload: json.RawMessage(`{"label":"` + label + `"}`),
	}
}

func createdEvent(at time.Time, task string, labels ...string) core.LogEntry {
	b, _ := json.Marshal(map[string]any{"title": task, "labels": labels})
	return core.LogEntry{At: at, Actor: "developer@claude:test", Action: "task.created", Subject: core.Subject{Kind: "task", ID: task}, Payload: b}
}

func day(d int) time.Time { // day d of a fixed August 2026 window, noon UTC
	return time.Date(2026, 8, d, 12, 0, 0, 0, time.UTC)
}

func TestMomentumBucketsCountsInDoneEvictAndSamplesOpen(t *testing.T) {
	s := newFlowSockets(scrum.New(), "ATM")
	end := day(17) // window: Aug 11 .. Aug 17 (1w, daily)
	entries := []core.LogEntry{
		labelEvent(day(5), "task.label-added", "ATM-a", "ATM:scrum:task"),           // before window: open at left edge
		labelEvent(day(11), "task.label-added", "ATM-b", "ATM:scrum:task"),          // in, bucket 0
		labelEvent(day(12), "task.label-added", "ATM-b", "ATM:scrum-stage:planned"), // irrelevant
		labelEvent(day(13), "task.label-added", "ATM-a", "ATM:scrum-stage:done"),    // done, bucket 2
		labelEvent(day(15), "task.label-added", "ATM-c", "ATM:scrum:bug"),           // in, bucket 4
		labelEvent(day(16), "task.label-added", "ATM-c", "ATM:scrum-out:duplicate"), // evict, bucket 5
	}
	got := momentumBuckets(entries, s, chartRanges[0], end)
	want := momentumSeries{
		In:    []int{1, 0, 0, 0, 1, 0, 0},
		Done:  []int{0, 0, 1, 0, 0, 0, 0},
		Evict: []int{0, 0, 0, 0, 0, 1, 0},
		Open:  []int{2, 2, 1, 1, 2, 1, 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("momentumBuckets() =\n%#v\nwant\n%#v", got, want)
	}
}

func TestMomentumBucketsReopenAndRedoneCountsTwice(t *testing.T) {
	s := newFlowSockets(scrum.New(), "ATM")
	end := day(17)
	entries := []core.LogEntry{
		labelEvent(day(11), "task.label-added", "ATM-a", "ATM:scrum:task"),
		labelEvent(day(12), "task.label-added", "ATM-a", "ATM:scrum-stage:done"),
		labelEvent(day(14), "task.label-removed", "ATM-a", "ATM:scrum-stage:done"), // reopen: back to open, no count
		labelEvent(day(16), "task.label-added", "ATM-a", "ATM:scrum-stage:done"),   // redone: second Done
	}
	got := momentumBuckets(entries, s, chartRanges[0], end)
	if !reflect.DeepEqual(got.Done, []int{0, 1, 0, 0, 0, 1, 0}) {
		t.Fatalf("Done = %#v", got.Done)
	}
	if !reflect.DeepEqual(got.In, []int{1, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("In = %#v (reopen must not count as an arrival)", got.In)
	}
	if !reflect.DeepEqual(got.Open, []int{1, 0, 0, 1, 1, 0, 0}) {
		t.Fatalf("Open = %#v", got.Open)
	}
}

func TestMomentumBucketsSilentExitsAndCreatedWithLabels(t *testing.T) {
	s := newFlowSockets(qa.New(), "ATM")
	end := day(17)
	entries := []core.LogEntry{
		createdEvent(day(11), "ATM-sc", "ATM:qa:testing"),                     // born claimed: In
		labelEvent(day(11), "task.label-added", "ATM-o", "ATM:qa:testing"),    // original: In
		labelEvent(day(13), "task.label-removed", "ATM-sc", "ATM:qa:testing"), // scaffold pass: silent exit
		labelEvent(day(14), "task.label-added", "ATM-o", "ATM:qa:done"),       // swap step 1: finished
		labelEvent(day(14), "task.label-removed", "ATM-o", "ATM:qa:testing"),  // swap step 2: still finished
		labelEvent(day(15), "task.label-added", "ATM-x", "ATM:qa:testing"),
		{At: day(16), Action: "task.removed", Subject: core.Subject{Kind: "task", ID: "ATM-x"}}, // removed: silent exit
	}
	got := momentumBuckets(entries, s, chartRanges[0], end)
	want := momentumSeries{
		In:    []int{2, 0, 0, 0, 1, 0, 0},
		Done:  []int{0, 0, 0, 1, 0, 0, 0},
		Evict: []int{0, 0, 0, 0, 0, 0, 0},
		Open:  []int{2, 2, 1, 0, 1, 0, 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("momentumBuckets() =\n%#v\nwant\n%#v", got, want)
	}
}

func TestMomentumBucketsSwapOrderDoesNotMatter(t *testing.T) {
	s := newFlowSockets(qa.New(), "ATM")
	end := day(17)
	entries := []core.LogEntry{
		labelEvent(day(11), "task.label-added", "ATM-o", "ATM:qa:testing"),
		labelEvent(day(14), "task.label-removed", "ATM-o", "ATM:qa:testing"), // remove first
		labelEvent(day(14), "task.label-added", "ATM-o", "ATM:qa:done"),      // then add done
	}
	got := momentumBuckets(entries, s, chartRanges[0], end)
	if !reflect.DeepEqual(got.Done, []int{0, 0, 0, 1, 0, 0, 0}) {
		t.Fatalf("Done = %#v", got.Done)
	}
	if !reflect.DeepEqual(got.In, []int{1, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("In = %#v (the re-add of done must not count a second arrival)", got.In)
	}
}

func TestMomentumBucketsWeeklyAndIgnoresOtherSubjects(t *testing.T) {
	s := newFlowSockets(scrum.New(), "ATM")
	end := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	entries := []core.LogEntry{
		labelEvent(time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC), "task.label-added", "ATM-a", "ATM:scrum:task"),
		{At: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), Action: "comment.label-added", Subject: core.Subject{Kind: "comment", ID: "ATM-a-c1"}, Payload: json.RawMessage(`{"label":"ATM:scrum-stage:done"}`)},
		labelEvent(time.Date(2026, 8, 17, 23, 0, 0, 0, time.UTC), "task.label-added", "ATM-a", "ATM:scrum-stage:done"),
	}
	got := momentumBuckets(entries, s, chartRanges[2], end) // 3m: 13 weekly buckets
	if len(got.In) != 13 || got.In[0] != 1 || got.Done[12] != 1 {
		t.Fatalf("weekly placement wrong: In=%#v Done=%#v", got.In, got.Done)
	}
	for i := 0; i < 12; i++ {
		if got.Open[i] != 1 {
			t.Fatalf("Open[%d] = %d, want 1", i, got.Open[i])
		}
	}
	if got.Open[12] != 0 {
		t.Fatalf("Open[12] = %d, want 0 after done", got.Open[12])
	}
}

func TestMomentumSeriesTotals(t *testing.T) {
	s := momentumSeries{In: []int{1, 2}, Done: []int{0, 1}, Evict: []int{1, 0}}
	in, done, evict := s.totals()
	if in != 3 || done != 1 || evict != 1 {
		t.Fatalf("totals = %d %d %d", in, done, evict)
	}
}
