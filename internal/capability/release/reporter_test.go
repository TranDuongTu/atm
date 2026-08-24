package release

import (
	"strings"
	"testing"

	"atm/internal/capability"
	"atm/internal/core"
)

func findingFor(t *testing.T, rep *ProjectReport, taskID, substr string) bool {
	t.Helper()
	for _, f := range rep.Findings {
		if f.TaskID == taskID && strings.Contains(f.Detail, substr) {
			return true
		}
	}
	return false
}

func TestReportListsContainersWithTheirRosters(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	c, _ := r.Cut("ATM", "v1.2")
	m, _ := s.CreateTask("ATM", "work", "", []string{"ATM:scrum-stage:done", "ATM:qa:done"}, testActor)
	_ = r.Include(m.ID, c.ID)

	rep, err := (&Reporter{Store: s}).Report("ATM")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(rep.Containers) != 1 || rep.Containers[0].Version != "v1-2" || rep.Containers[0].Shipped {
		t.Fatalf("containers = %+v", rep.Containers)
	}
	ms := rep.Containers[0].Members
	if len(ms) != 1 || ms[0].TaskID != m.ID {
		t.Fatalf("members = %+v", ms)
	}
	// The snapshot shows the member's public labels verbatim, and hides only
	// this capability's own — release has no opinion about what they mean.
	if !containsString(ms[0].Labels, "ATM:scrum-stage:done") || !containsString(ms[0].Labels, "ATM:qa:done") {
		t.Fatalf("label snapshot = %v", ms[0].Labels)
	}
	for _, l := range ms[0].Labels {
		if strings.HasPrefix(l, "ATM:release:") {
			t.Fatalf("release's own labels leaked into the snapshot: %v", ms[0].Labels)
		}
	}
}

func TestReportFlagsDanglingRostersAndOrphanMembers(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	c, _ := r.Cut("ATM", "v1.2")
	cpl := payloadOf(t, s, c.ID)
	cpl.AddMember("ATM-gone99")
	enc, _ := cpl.Encode()
	_ = s.SetTaskCapabilityMeta(c.ID, CapabilityName, enc, testActor)

	orphan, _ := s.CreateTask("ATM", "orphan", "", nil, testActor)
	_ = s.SetTaskCapabilityMeta(orphan.ID, CapabilityName, `{"v":1,"release_of":"ATM-gone88"}`, testActor)

	rep, _ := (&Reporter{Store: s}).Report("ATM")
	if !findingFor(t, rep, c.ID, "ATM-gone99") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
	if !findingFor(t, rep, orphan.ID, "ATM-gone88") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
}

func TestReportFlagsAMemberLeftBehindByShip(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	c, _ := r.Cut("ATM", "v1.2")
	m, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	_ = r.Include(m.ID, c.ID)
	_, _ = r.Ship(c.ID)
	_ = s.TaskLabelRemove(m.ID, "ATM:release:done", testActor)

	rep, _ := (&Reporter{Store: s}).Report("ATM")
	if !findingFor(t, rep, m.ID, "not stamped shipped") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
}

func TestReportDoesNotTouchTheLedger(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	c, _ := r.Cut("ATM", "v1.2")
	m, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	_ = r.Include(m.ID, c.ID)
	bad, _ := s.CreateTask("ATM", "bad", "", nil, testActor)
	_ = s.SetTaskCapabilityMeta(bad.ID, CapabilityName, "not json", testActor)

	before := ledgerDigest(t, s)
	if _, err := (&Reporter{Store: s}).Report("ATM"); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if after := ledgerDigest(t, s); after != before {
		t.Fatal("the reporter appended to the ledger")
	}
}

func cellFor(labels []string, meta string) *capability.Cell {
	t := core.Task{ID: "ATM-abc123", Labels: labels}
	if meta != "" {
		t.Meta = map[string]string{CapabilityName: meta}
	}
	return Cap{}.Annotate(t)
}

func TestAnnotate(t *testing.T) {
	if c := cellFor(nil, ""); c != nil {
		t.Fatalf("cell = %+v, want nil for a task no release touches", c)
	}
	c := cellFor([]string{"ATM:release:v1-2"}, `{"v":1,"version":"v1-2","members":["ATM-a","ATM-b"]}`)
	if c == nil || c.Text != "v1-2 · 2 tasks" || c.Tone != capability.ToneNeutral {
		t.Fatalf("container cell = %+v", c)
	}
	c = cellFor([]string{"ATM:release:v1-2", "ATM:release:done"}, `{"v":1,"version":"v1-2"}`)
	if c == nil || c.Text != "✓ shipped v1-2" || c.Tone != capability.ToneOK {
		t.Fatalf("shipped container cell = %+v", c)
	}
	c = cellFor([]string{"ATM:release:v1-2"}, `{"v":1,"release_of":"ATM-c1"}`)
	if c == nil || c.Text != "→ release" {
		t.Fatalf("member cell = %+v", c)
	}
	c = cellFor([]string{"ATM:release:v1-2", "ATM:release:done"}, `{"v":1,"release_of":"ATM-c1"}`)
	if c == nil || c.Text != "✓ shipped" || c.Tone != capability.ToneOK {
		t.Fatalf("shipped member cell = %+v", c)
	}
	c = cellFor([]string{"ATM:release:v1-2"}, "not json")
	if c == nil || !strings.Contains(c.Text, "unreadable") || c.Tone != capability.ToneAttention {
		t.Fatalf("degraded cell = %+v", c)
	}
}
