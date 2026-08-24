package tui

// Select-path timing probe for ATM-40faff: times each stage of the
// projects-pane 's' (select) handler against a COPY of a live store, with
// the SAME capability registry as cmd/atm/main.go. Points at the copy via
// ATM_BENCH_STORE; skipped otherwise.

import (
	"os"
	"testing"
	"time"

	"atm/internal/capability"
	"atm/internal/capability/contextmap"
	"atm/internal/capability/workflow"
	"atm/internal/capability/workflowai"
	"atm/internal/store"
)

func TestSelectPathTimings(t *testing.T) {
	root := os.Getenv("ATM_BENCH_STORE")
	if root == "" {
		t.Skip("ATM_BENCH_STORE not set")
	}
	s, err := store.Open(root)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	reg := capability.NewRegistry(workflow.New(), contextmap.New(), workflowai.New())
	m, err := NewModel(NewModelOpts{Service: s, Actor: benchActor, Registry: reg})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	code := "ATM"
	var ensure1, ensure2 time.Duration
	stage := func(name string, d *time.Duration, f func()) {
		st := time.Now()
		f()
		el := time.Since(st)
		if d != nil {
			*d = el
		}
		t.Logf("%-24s %v", name, el)
	}

	m.projectScope = code
	stage("EnsureVocabulary", &ensure1, func() {
		if _, err := m.regFor(code).EnsureVocabulary(m.store, code, m.actor); err != nil {
			t.Fatalf("EnsureVocabulary: %v", err)
		}
	})
	stage("capability.refresh", nil, func() { m.capability.refresh() })
	stage("lanes.refresh", nil, func() { m.lanes.refresh() })
	stage("lanes.selectDefault", nil, func() { m.lanes.selectDefault() })
	stage("tasks.refresh", nil, func() { m.tasks.refresh() })
	stage("refreshStoreStats", nil, func() { m.refreshStoreStats() })
	stage("refreshSummary", nil, func() { m.projects.refreshSummary() })
	stage("EnsureVocabulary#2", &ensure2, func() {
		if _, err := m.regFor(code).EnsureVocabulary(m.store, code, m.actor); err != nil {
			t.Fatalf("EnsureVocabulary#2: %v", err)
		}
	})

	// ATM-40faff acceptance: converged ensure < 400ms (was ~8.5s at the
	// production registry's 34 seeds). Generous headroom over the ~250ms
	// target so machine variance does not flake the probe.
	for name, d := range map[string]time.Duration{"EnsureVocabulary": ensure1, "EnsureVocabulary#2": ensure2} {
		if d > 400*time.Millisecond {
			t.Errorf("%s took %v, want < 400ms (ATM-40faff)", name, d)
		}
	}
}
