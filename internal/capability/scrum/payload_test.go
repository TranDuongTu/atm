package scrum

import "testing"

func TestEmptyPayloadRoundTrips(t *testing.T) {
	p, err := DecodePayload("")
	if err != nil {
		t.Fatal(err)
	}
	if p.PartOf() != "" || len(p.DependsOn()) != 0 || p.Spec() != "" || p.Plan() != "" {
		t.Fatalf("empty payload is not empty: %+v", p)
	}
	out, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("empty payload encodes to %q, want \"\" (so the Meta key is deleted)", out)
	}
}

func TestMalformedPayloadIsAnError(t *testing.T) {
	if _, err := DecodePayload("not json"); err == nil {
		t.Fatal("malformed payload must error — verbs refuse rather than overwrite unreadable state")
	}
}

func TestUnknownFieldsSurviveRoundTrip(t *testing.T) {
	p, err := DecodePayload(`{"v":1,"future_field":"kept","part_of":"ATM-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	p.SetSpec("docs/specs/x.md")
	out, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	q, err := DecodePayload(out)
	if err != nil {
		t.Fatal(err)
	}
	if str(q.raw["future_field"]) != "kept" {
		t.Fatalf("future_field lost: %s", out)
	}
	if q.PartOf() != "ATM-1" {
		t.Fatalf("part_of = %q", q.PartOf())
	}
	if q.Spec() != "docs/specs/x.md" {
		t.Fatalf("spec = %q", q.Spec())
	}
}

func TestPartOfSetAndClear(t *testing.T) {
	p, _ := DecodePayload("")
	p.SetPartOf("ATM-parent")
	if p.PartOf() != "ATM-parent" {
		t.Fatalf("part_of = %q", p.PartOf())
	}
	p.ClearPartOf()
	if p.PartOf() != "" {
		t.Fatalf("part_of survived clear: %q", p.PartOf())
	}
}

func TestAddDependsOnDeduplicates(t *testing.T) {
	p, _ := DecodePayload("")
	if !p.AddDependsOn("ATM-a") {
		t.Fatal("first add must report added")
	}
	if p.AddDependsOn("ATM-a") {
		t.Fatal("duplicate add must report not-added")
	}
	p.AddDependsOn("ATM-b")
	if got := p.DependsOn(); len(got) != 2 || got[0] != "ATM-a" || got[1] != "ATM-b" {
		t.Fatalf("depends_on = %v, want order preserved", got)
	}
	if !p.RemoveDependsOn("ATM-a") {
		t.Fatal("remove must report present")
	}
	if p.RemoveDependsOn("ATM-a") {
		t.Fatal("second remove must report absent")
	}
	if got := p.DependsOn(); len(got) != 1 || got[0] != "ATM-b" {
		t.Fatalf("depends_on = %v", got)
	}
}

func TestEmptiedListDoesNotEncodeAsEmptyArray(t *testing.T) {
	p, _ := DecodePayload(`{"v":1,"depends_on":["ATM-a"]}`)
	p.RemoveDependsOn("ATM-a")
	out, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("payload with only an emptied list encodes to %q, want \"\"", out)
	}
}

func TestCoveredByAndLocatorsRoundTrip(t *testing.T) {
	p, _ := DecodePayload("")
	p.SetCoveredBy("ATM-other")
	p.SetSpec("docs/specs/a.md")
	p.SetPlan("docs/plans/a.md")
	out, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	q, err := DecodePayload(out)
	if err != nil {
		t.Fatal(err)
	}
	if q.CoveredBy() != "ATM-other" || q.Spec() != "docs/specs/a.md" || q.Plan() != "docs/plans/a.md" {
		t.Fatalf("round trip lost fields: %s", out)
	}
	q.ClearCoveredBy()
	q.ClearSpec()
	q.ClearPlan()
	if q.CoveredBy() != "" || q.Spec() != "" || q.Plan() != "" {
		t.Fatal("a cleared field survived")
	}
	// Every field this version owns is clearable, so release can empty the
	// payload through the accessors rather than reaching into the raw map.
	out, err = q.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("payload with every owned field cleared encodes to %q, want \"\"", out)
	}
}
