// internal/workflow/recorder_test.go
package workflow

import (
	"strings"
	"testing"

	"atm/internal/store"
)

// getTaskOrFatal is a test helper: the brief's draft referenced a
// hypothetical s.GetTaskOrFatal(t, id); the real store API is
// s.GetTask(id) (*store.Task, error), so this wraps it with t.Fatalf.
func getTaskOrFatal(t *testing.T, s *store.Store, id string) *store.Task {
	t.Helper()
	tk, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return tk
}

func TestRecorderSetStatusFromUntriaged(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	prior, err := r.SetStatus(tk.ID, StatusOpen)
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if prior != "" {
		t.Errorf("prior = %q, want \"\" (was untriaged)", prior)
	}
	got, _ := (&Reporter{Store: s}).Status(tk.ID)
	if got != StatusOpen {
		t.Errorf("after SetStatus, status = %q, want %q", got, StatusOpen)
	}
}

func TestRecorderSetStatusSwapsExisting(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:status:open"}, "admin@cli:unset")
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	prior, err := r.SetStatus(tk.ID, StatusInProgress)
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if prior != StatusOpen {
		t.Errorf("prior = %q, want %q", prior, StatusOpen)
	}
	if got, _ := (&Reporter{Store: s}).Status(tk.ID); got != StatusInProgress {
		t.Errorf("status = %q, want %q", got, StatusInProgress)
	}
	if n := countStatusLabels(getTaskOrFatal(t, s, tk.ID), "ATM"); n != 1 {
		t.Errorf("status label count = %d, want 1", n)
	}
}

func TestRecorderSetStatusNoOpWhenAlreadyAtTarget(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:status:done"}, "admin@cli:unset")
	code, _, _ := store.ParseTaskID(tk.ID)
	before, _ := s.LastLogSeq(code)
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	prior, err := r.SetStatus(tk.ID, StatusDone)
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if prior != StatusDone {
		t.Errorf("prior = %q, want %q (already done)", prior, StatusDone)
	}
	after, _ := s.LastLogSeq(code)
	if before != after {
		t.Fatalf("no-op SetStatus advanced log seq %d -> %d", before, after)
	}
}

func TestRecorderSetStatusPreservesNonStatusLabels(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:priority:high", "ATM:status:open"}, "admin@cli:unset")
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	if _, err := r.SetStatus(tk.ID, StatusDone); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	hasPrio := false
	for _, l := range getTaskOrFatal(t, s, tk.ID).Labels {
		if l == "ATM:priority:high" {
			hasPrio = true
		}
	}
	if !hasPrio {
		t.Error("priority:high label was dropped by the status swap")
	}
}

func TestRecorderSetStatusRemovesMultipleStatusLabels(t *testing.T) {
	// A hand-edited task may carry several status:* labels (the store permits
	// it). SetStatus must remove ALL of them and add the target, restoring
	// exactly-one-status as a capability-maintained invariant.
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	_ = s.TaskLabelAdd(tk.ID, "ATM:status:open", "admin@cli:unset")
	_ = s.TaskLabelAdd(tk.ID, "ATM:status:done", "admin@cli:unset")
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	if _, err := r.SetStatus(tk.ID, StatusInProgress); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if n := countStatusLabels(getTaskOrFatal(t, s, tk.ID), "ATM"); n != 1 {
		t.Errorf("status label count = %d, want 1 (after collapsing hand-edit)", n)
	}
}

func TestRecorderScrumVerbsMapToCorrectStatus(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	cases := []struct {
		fn   func(string) (string, error)
		want string
	}{
		{r.Start, StatusInProgress},
		{r.Open, StatusOpen},
		{r.Block, StatusBlocked},
		{r.Complete, StatusDone},
	}
	for i, c := range cases {
		if _, err := c.fn(tk.ID); err != nil {
			t.Fatalf("verb %d: %v", i, err)
		}
		got, _ := (&Reporter{Store: s}).Status(tk.ID)
		if got != c.want {
			t.Errorf("verb %d: status = %q, want %q", i, got, c.want)
		}
	}
}

func TestRecorderSetStatusUnknownTask(t *testing.T) {
	s := newTestStore(t)
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	if _, err := r.SetStatus("ATM-deadbeef", StatusOpen); err == nil {
		t.Error("expected error for unknown task id")
	}
}

// countStatusLabels counts labels with the <code>:status:* prefix.
func countStatusLabels(tk *store.Task, code string) int {
	prefix := code + ":" + StatusNamespace + ":"
	n := 0
	for _, l := range tk.Labels {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}
