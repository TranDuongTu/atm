package profile

import (
	"strings"
	"testing"
)

func TestForProjectSubstitutesCode(t *testing.T) {
	p, err := Load(goodFiles())
	if err != nil {
		t.Fatal(err)
	}
	out := p.ForProject("ATM")

	mgr, _ := out.ProfilePersona("manager")
	if !strings.Contains(mgr.Prompt, "manager of ATM") {
		t.Fatalf("persona body not substituted: %q", mgr.Prompt)
	}
	pl, _ := out.ProfileChecklist("planning")
	if pl.Purpose != "the weekly planning pass for ATM" {
		t.Fatalf("purpose = %q", pl.Purpose)
	}
	if !strings.Contains(pl.Steps[0].Children[0].Text, "#planning") {
		t.Fatalf("nested step lost: %+v", pl.Steps)
	}
	sc, _ := out.ProfileChecklist("scrum-coding")
	if strings.Contains(sc.Targets, "<CODE>") || !strings.Contains(sc.Targets, "ATM:scrum:task") {
		t.Fatalf("targets = %q", sc.Targets)
	}
	ch, _ := out.ProfileChannel("planning")
	if !strings.Contains(ch.Purpose, "discussion for ATM") {
		t.Fatalf("channel purpose = %q", ch.Purpose)
	}

	// The receiver is untouched: one loaded profile serves many projects.
	if src, _ := p.ProfileChecklist("planning"); src.Purpose != "the weekly planning pass for <CODE>" {
		t.Fatalf("ForProject mutated the source: %q", src.Purpose)
	}
	if src, _ := p.ProfileChecklist("planning"); strings.Contains(src.Steps[0].Children[0].Text, "ATM") {
		t.Fatal("ForProject aliased the source step tree")
	}
}
