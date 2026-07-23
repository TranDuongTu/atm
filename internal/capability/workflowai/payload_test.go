package workflowai

import (
	"encoding/json"
	"testing"
)

func TestPayloadEmptyDecodesAndEncodesEmpty(t *testing.T) {
	pl, err := DecodePayload("")
	if err != nil {
		t.Fatalf("DecodePayload(\"\"): %v", err)
	}
	out, err := pl.Encode()
	if err != nil || out != "" {
		t.Fatalf("Encode = %q, %v; want \"\" (empty payload deletes the key)", out, err)
	}
}

func TestPayloadPlanRoundTrip(t *testing.T) {
	pl, _ := DecodePayload("")
	pl.SetPlan(PlanRecord{Kind: PlanKindFile, Ref: "docs/p.md", RecordedAt: "2026-07-21T00:00:00Z", Actor: "a"})
	out, err := pl.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := DecodePayload(out)
	if err != nil {
		t.Fatalf("DecodePayload(%q): %v", out, err)
	}
	p := back.Plan()
	if p == nil || p.Kind != PlanKindFile || p.Ref != "docs/p.md" || p.RecordedAt != "2026-07-21T00:00:00Z" || p.Actor != "a" {
		t.Errorf("Plan = %+v", p)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if v, ok := m["v"].(float64); !ok || v != 1 {
		t.Errorf("v = %v, want 1", m["v"])
	}
}

func TestPayloadPreservesUnknownFields(t *testing.T) {
	in := `{"v":1,"future_field":{"x":1},"plan":{"kind":"file","ref":"docs/p.md","recorded_at":"t0","actor":"a"}}`
	pl, err := DecodePayload(in)
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	pl.SetPlan(PlanRecord{Kind: PlanKindCommit, Ref: "abc123", RecordedAt: "t1", Actor: "b"})
	out, _ := pl.Encode()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["future_field"].(map[string]any); !ok {
		t.Errorf("future_field lost: %v", out)
	}
	plan := m["plan"].(map[string]any)
	if plan["kind"] != "commit" || plan["ref"] != "abc123" {
		t.Errorf("plan not updated: %v", plan)
	}
}

func TestPayloadClearLastFieldEncodesEmpty(t *testing.T) {
	pl, _ := DecodePayload("")
	pl.SetPlan(PlanRecord{Kind: PlanKindEphemeral, Ref: "session x", RecordedAt: "t", Actor: "a"})
	pl.ClearPlan()
	if out, _ := pl.Encode(); out != "" {
		t.Errorf("Encode = %q, want \"\" after clearing the only field", out)
	}
}

func TestDecodePayloadMalformed(t *testing.T) {
	if _, err := DecodePayload("not json"); err == nil {
		t.Fatal("DecodePayload of malformed input must error (verbs never overwrite what they cannot read)")
	}
}

func TestRelatesToAddRemove(t *testing.T) {
	pl, _ := DecodePayload("")
	if !pl.AddRelatesTo("ATM-bbbbbb") {
		t.Error("first add should report true")
	}
	if pl.AddRelatesTo("ATM-bbbbbb") {
		t.Error("duplicate add should report false")
	}
	if got := pl.RelatesTo(); len(got) != 1 || got[0] != "ATM-bbbbbb" {
		t.Errorf("RelatesTo = %v", got)
	}
	if pl.RemoveRelatesTo("ATM-cccccc") {
		t.Error("removing an absent link should report false")
	}
	if !pl.RemoveRelatesTo("ATM-bbbbbb") {
		t.Error("removing the present link should report true")
	}
	if out, _ := pl.Encode(); out != "" {
		t.Errorf("Encode = %q, want \"\" after the list emptied (key removed, not [])", out)
	}
}

func TestRevisionOfAndDemoted(t *testing.T) {
	pl, _ := DecodePayload("")
	pl.SetRevisionOf("ATM-aaaaaa")
	pl.SetDemoted(Demotion{At: "t", By: "a", Reason: "plan lost"})
	out, _ := pl.Encode()
	back, _ := DecodePayload(out)
	if back.RevisionOf() != "ATM-aaaaaa" {
		t.Errorf("RevisionOf = %q", back.RevisionOf())
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(out), &m)
	d := m["demoted"].(map[string]any)
	if d["reason"] != "plan lost" || d["at"] != "t" || d["by"] != "a" {
		t.Errorf("demoted = %v", d)
	}
	back.ClearRevisionOf()
	if back.RevisionOf() != "" {
		t.Error("ClearRevisionOf did not clear")
	}
}

func TestPayloadSpecRoundTrip(t *testing.T) {
	pl, _ := DecodePayload("")
	pl.SetSpec(SpecRecord{Kind: PlanKindFile, Ref: "docs/superpowers/specs/x.md", RecordedAt: "2026-07-23T00:00:00Z", Actor: "a"})
	out, err := pl.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := DecodePayload(out)
	if err != nil {
		t.Fatalf("DecodePayload(%q): %v", out, err)
	}
	s := back.Spec()
	if s == nil || s.Kind != PlanKindFile || s.Ref != "docs/superpowers/specs/x.md" || s.RecordedAt != "2026-07-23T00:00:00Z" || s.Actor != "a" {
		t.Errorf("Spec = %+v", s)
	}
}

func TestPayloadSpecAndPlanCoexist(t *testing.T) {
	pl, _ := DecodePayload("")
	pl.SetSpec(SpecRecord{Kind: PlanKindFile, Ref: "spec.md", RecordedAt: "t0", Actor: "a"})
	pl.SetPlan(PlanRecord{Kind: PlanKindFile, Ref: "plan.md", RecordedAt: "t1", Actor: "b"})
	out, _ := pl.Encode()
	back, _ := DecodePayload(out)
	if s := back.Spec(); s == nil || s.Ref != "spec.md" {
		t.Errorf("spec lost: %+v", s)
	}
	if p := back.Plan(); p == nil || p.Ref != "plan.md" {
		t.Errorf("plan lost: %+v", p)
	}
}

func TestPayloadClearSpec(t *testing.T) {
	pl, _ := DecodePayload("")
	pl.SetSpec(SpecRecord{Kind: PlanKindFile, Ref: "s.md", RecordedAt: "t", Actor: "a"})
	pl.ClearSpec()
	if out, _ := pl.Encode(); out != "" {
		t.Errorf("Encode = %q, want \"\" after clearing the only field", out)
	}
}

func TestFirstStageValue(t *testing.T) {
	labels := []string{"ATM:status:open", "ATM:stage:clarified", "ATM:wfai:revision"}
	if got := firstStageValue(labels, "ATM"); got != StageClarified {
		t.Errorf("firstStageValue = %q, want %q", got, StageClarified)
	}
	if got := firstStageValue([]string{"ATM:status:open"}, "ATM"); got != StageNew {
		t.Errorf("firstStageValue = %q, want StageNew", got)
	}
}
