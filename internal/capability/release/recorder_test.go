package release

import (
	"strings"
	"testing"

	"atm/internal/capability"
)

// The kind distinction is mechanical, and this is where it is pinned: release
// must NOT satisfy the flow contract, or it would grow lanes, wiring and a
// place in the manager's triage loop it has no use for.
func TestReleaseIsARegistryCapabilityNotAFlow(t *testing.T) {
	if _, ok := interface{}(New()).(capability.Flow); ok {
		t.Fatal("release must not implement capability.Flow")
	}
	var _ capability.Capability = New()
}

func TestARegistryCapabilitySeedsNoBoards(t *testing.T) {
	if got := Vocabulary("ATM")[0].Name; got != "ATM:release:*" {
		t.Fatalf("vocabulary starts with %q, want the namespace descriptor", got)
	}
	for _, l := range Vocabulary("ATM") {
		if l.Expr != "" {
			t.Fatalf("a registry capability seeded a board: %+v", l)
		}
	}
}

func TestSanitizeVersionFitsTheLabelGrammar(t *testing.T) {
	for in, want := range map[string]string{
		"v1.2":   "v1-2",
		"V1.2.3": "v1-2-3",
		" 2024 ": "2024",
		"a_b":    "a-b",
	} {
		got, err := SanitizeVersion(in)
		if err != nil {
			t.Fatalf("SanitizeVersion(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("SanitizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "   ", "-leading", "has space", "π", "done"} {
		if got, err := SanitizeVersion(bad); err == nil {
			t.Errorf("SanitizeVersion(%q) = %q, want an error", bad, got)
		}
	}
}

func TestCutCreatesTheContainerWithItsVersionLabel(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	c, err := r.Cut("ATM", "v1.2")
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if !containsString(labelsOf(t, s, c.ID), "ATM:release:v1-2") {
		t.Fatalf("labels = %v", labelsOf(t, s, c.ID))
	}
	if payloadOf(t, s, c.ID).Version() != "v1-2" {
		t.Fatalf("version = %q", payloadOf(t, s, c.ID).Version())
	}
	if !strings.Contains(c.Title, "v1.2") {
		t.Fatalf("title = %q, want the version as humans wrote it", c.Title)
	}
}

func TestIncludeRecordsBothEndsAndStampsTheMember(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	c, _ := r.Cut("ATM", "v1.2")
	m, _ := s.CreateTask("ATM", "some work", "", nil, testActor)
	if err := r.Include(m.ID, c.ID); err != nil {
		t.Fatalf("Include: %v", err)
	}
	if !containsString(labelsOf(t, s, m.ID), "ATM:release:v1-2") {
		t.Fatalf("member labels = %v", labelsOf(t, s, m.ID))
	}
	if payloadOf(t, s, m.ID).ReleaseOf() != c.ID {
		t.Fatalf("release_of = %q", payloadOf(t, s, m.ID).ReleaseOf())
	}
	roster := payloadOf(t, s, c.ID).Members()
	if len(roster) != 1 || roster[0] != m.ID {
		t.Fatalf("roster = %v", roster)
	}
	// Idempotent.
	if err := r.Include(m.ID, c.ID); err != nil {
		t.Fatalf("re-Include: %v", err)
	}
	if len(payloadOf(t, s, c.ID).Members()) != 1 {
		t.Fatalf("roster grew on re-include: %v", payloadOf(t, s, c.ID).Members())
	}
}

// Selection is judgment, and judgment lives in the guide. The verb must not
// quietly enforce a certification rule nobody can see.
func TestIncludeDoesNotSecondGuessCertification(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	c, _ := r.Cut("ATM", "v1.2")
	uncertified, _ := s.CreateTask("ATM", "not certified by anyone", "", nil, testActor)
	if err := r.Include(uncertified.ID, c.ID); err != nil {
		t.Fatalf("Include must be mechanical, got: %v", err)
	}
}

func TestIncludeRefusesAcrossReleasesAndNonContainers(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	a, _ := r.Cut("ATM", "v1.2")
	b, _ := r.Cut("ATM", "v1.3")
	m, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	_ = r.Include(m.ID, a.ID)
	if err := r.Include(m.ID, b.ID); err == nil || !strings.Contains(err.Error(), "exclude it first") {
		t.Fatalf("err = %v", err)
	}
	plain, _ := s.CreateTask("ATM", "not a release", "", nil, testActor)
	if err := r.Include(m.ID, plain.ID); err == nil || !strings.Contains(err.Error(), "not a release container") {
		t.Fatalf("err = %v", err)
	}
	if err := r.Include(a.ID, a.ID); err == nil {
		t.Fatal("a release must not contain itself")
	}
}

func TestExcludeReversesInclude(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	c, _ := r.Cut("ATM", "v1.2")
	m, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	_ = r.Include(m.ID, c.ID)
	if err := r.Exclude(m.ID, c.ID); err != nil {
		t.Fatalf("Exclude: %v", err)
	}
	if containsString(labelsOf(t, s, m.ID), "ATM:release:v1-2") {
		t.Fatalf("version label survived exclude: %v", labelsOf(t, s, m.ID))
	}
	if payloadOf(t, s, m.ID).ReleaseOf() != "" {
		t.Fatal("release_of survived exclude")
	}
	if len(payloadOf(t, s, c.ID).Members()) != 0 {
		t.Fatalf("roster = %v", payloadOf(t, s, c.ID).Members())
	}
	if err := r.Exclude(m.ID, c.ID); err == nil {
		t.Fatal("excluding a non-member must fail")
	}
}

// A shipped release is history. Editing its roster would rewrite the record
// rather than correct it.
func TestExcludeRefusesAfterShip(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	c, _ := r.Cut("ATM", "v1.2")
	m, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	_ = r.Include(m.ID, c.ID)
	if _, err := r.Ship(c.ID); err != nil {
		t.Fatalf("Ship: %v", err)
	}
	if err := r.Exclude(m.ID, c.ID); err == nil || !strings.Contains(err.Error(), "history") {
		t.Fatalf("err = %v", err)
	}
}

func TestShipStampsTheContainerAndEveryMemberAndLogsIt(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	c, _ := r.Cut("ATM", "v1.2")
	a, _ := s.CreateTask("ATM", "a", "", nil, testActor)
	b, _ := s.CreateTask("ATM", "b", "", nil, testActor)
	_ = r.Include(a.ID, c.ID)
	_ = r.Include(b.ID, c.ID)
	stamped, err := r.Ship(c.ID)
	if err != nil {
		t.Fatalf("Ship: %v", err)
	}
	if len(stamped) != 3 {
		t.Fatalf("stamped = %v, want the container and both members", stamped)
	}
	for _, id := range []string{c.ID, a.ID, b.ID} {
		if !containsString(labelsOf(t, s, id), "ATM:release:done") {
			t.Fatalf("%s not stamped shipped: %v", id, labelsOf(t, s, id))
		}
	}
	comments, _ := s.ListComments(c.ID)
	if len(comments) == 0 || !strings.Contains(comments[len(comments)-1].Body, "shipped v1-2") {
		t.Fatalf("release log entry missing: %v", comments)
	}
}

func TestShipRefusesANonContainer(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	plain, _ := s.CreateTask("ATM", "plain", "", nil, testActor)
	if _, err := r.Ship(plain.ID); err == nil {
		t.Fatal("shipping a non-container must fail")
	}
}

func TestRecorderFailsOnMalformedPayload(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	c, _ := r.Cut("ATM", "v1.2")
	m, _ := s.CreateTask("ATM", "bad", "", nil, testActor)
	_ = s.SetTaskCapabilityMeta(m.ID, CapabilityName, "not json", testActor)
	if err := r.Include(m.ID, c.ID); err == nil || !strings.Contains(err.Error(), "hand-repair") {
		t.Fatalf("err = %v", err)
	}
}
