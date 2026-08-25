package store

import (
	"errors"
	"testing"

	"atm/internal/core"
)

func TestSetTaskCapabilityMetaRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("PXA", "Proj X", testActor); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("PXA", "a task", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}

	// Set two capabilities' payloads; they are independent keys.
	if err := s.SetTaskCapabilityMeta(tk.ID, "qa", `{"v":1,"stage":"planned"}`, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskCapabilityMeta(tk.ID, "scrum", "cm", testActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta["qa"] != `{"v":1,"stage":"planned"}` || got.Meta["scrum"] != "cm" {
		t.Errorf("Meta = %+v", got.Meta)
	}

	// Overwrite one key; the sibling survives.
	if err := s.SetTaskCapabilityMeta(tk.ID, "qa", `{"v":2}`, testActor); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetTask(tk.ID)
	if got.Meta["qa"] != `{"v":2}` || got.Meta["scrum"] != "cm" {
		t.Errorf("after overwrite Meta = %+v", got.Meta)
	}

	// Clear via empty payload: key absent.
	if err := s.SetTaskCapabilityMeta(tk.ID, "qa", "", testActor); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetTask(tk.ID)
	if _, ok := got.Meta["qa"]; ok {
		t.Errorf("cleared key present: %+v", got.Meta)
	}

	// The list path carries Meta too (the TUI reads through ListTasks).
	ts := s.ListTasks(core.QueryFilters{Project: "PXA"})
	if len(ts) != 1 || ts[0].Meta["scrum"] != "cm" {
		t.Errorf("ListTasks Meta = %+v", ts)
	}
}

func TestSetTaskCapabilityMetaGuards(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("PXA", "Proj X", testActor); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("PXA", "a task", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskCapabilityMeta(tk.ID, "", "x", testActor); !errors.Is(err, core.ErrUsage) {
		t.Errorf("empty capability: err = %v, want ErrUsage", err)
	}
	if err := s.SetTaskCapabilityMeta(tk.ID, "qa", "x", ""); err == nil {
		t.Error("missing actor accepted")
	}
	if err := s.SetTaskCapabilityMeta("PXA-ffffff", "qa", "x", testActor); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("unknown task: err = %v, want ErrNotFound", err)
	}
}
