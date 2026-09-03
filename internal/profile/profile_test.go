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

	mgr, _ := out.Persona("manager")
	if !strings.Contains(mgr.Prompt, "manager of ATM") {
		t.Fatalf("persona body not substituted: %q", mgr.Prompt)
	}
	pl, _ := out.Checklist("planning")
	if pl.Purpose != "the weekly planning pass for ATM" {
		t.Fatalf("purpose = %q", pl.Purpose)
	}
	if !strings.Contains(pl.Steps[0].Children[0].Text, "#planning") {
		t.Fatalf("nested step lost: %+v", pl.Steps)
	}
	sc, _ := out.Checklist("scrum-coding")
	if strings.Contains(sc.Targets, "<CODE>") || !strings.Contains(sc.Targets, "ATM:scrum:task") {
		t.Fatalf("targets = %q", sc.Targets)
	}
	ch, _ := out.Channel("planning")
	if !strings.Contains(ch.Purpose, "discussion for ATM") {
		t.Fatalf("channel purpose = %q", ch.Purpose)
	}

	// The receiver is untouched: one loaded profile serves many projects.
	if src, _ := p.Checklist("planning"); src.Purpose != "the weekly planning pass for <CODE>" {
		t.Fatalf("ForProject mutated the source: %q", src.Purpose)
	}
	if src, _ := p.Checklist("planning"); strings.Contains(src.Steps[0].Children[0].Text, "ATM") {
		t.Fatal("ForProject aliased the source step tree")
	}
}

func TestOriginRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		in      string
		kind    OriginKind
		profile string
		version string
	}{
		{"user", OriginUser, "", ""},
		{"scrumban@1.0.0", OriginProfile, "scrumban", "1.0.0"},
		{"scrumban@dev", OriginProfile, "scrumban", "dev"},
	} {
		o, err := ParseOrigin(tc.in)
		if err != nil {
			t.Fatalf("ParseOrigin(%q): %v", tc.in, err)
		}
		if o.Kind != tc.kind || o.Profile != tc.profile || o.Version != tc.version {
			t.Fatalf("ParseOrigin(%q) = %+v", tc.in, o)
		}
		if got := o.String(); got != tc.in {
			t.Fatalf("String() = %q, want %q", got, tc.in)
		}
	}
}

// Records stamped before profiles existed must keep reading. They decode to
// a legacy origin that round-trips verbatim and reports no profile to reset
// against — the caller warns rather than crashing (plan §3.2).
func TestOriginLegacyReadCompat(t *testing.T) {
	for _, in := range []string{"shipped:atm", "shipped:scrum", "builtin"} {
		o, err := ParseOrigin(in)
		if err != nil {
			t.Fatalf("ParseOrigin(%q): %v", in, err)
		}
		if o.Kind != OriginLegacy {
			t.Fatalf("ParseOrigin(%q).Kind = %v, want legacy", in, o.Kind)
		}
		if o.Profile != "" || o.Version != "" {
			t.Fatalf("legacy origin %q must name no profile to reset against: %+v", in, o)
		}
		if got := o.String(); got != in {
			t.Fatalf("String() = %q, want the stored value %q back", got, in)
		}
	}
}

func TestOriginRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "scrumban@", "@1.0.0", "scrumban@1.0", "Scrumban@1.0.0", "shipped:", "profile@1.0.0@2"} {
		if o, err := ParseOrigin(in); err == nil {
			t.Fatalf("ParseOrigin(%q) accepted, got %+v", in, o)
		}
	}
}

func TestProfileOriginFromManifest(t *testing.T) {
	p, err := Load(goodFiles())
	if err != nil {
		t.Fatal(err)
	}
	o := p.Origin()
	if o.Kind != OriginProfile || o.String() != "scrumban@1.0.0" {
		t.Fatalf("Origin() = %+v", o)
	}
}

func TestOriginIsUser(t *testing.T) {
	o, _ := ParseOrigin("user")
	if !o.IsUser() {
		t.Fatal("user origin must report IsUser")
	}
	p, _ := ParseOrigin("scrumban@1.0.0")
	if p.IsUser() {
		t.Fatal("profile origin must not report IsUser")
	}
}
