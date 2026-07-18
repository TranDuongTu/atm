package manager

import (
	"strings"
	"testing"
)

func TestRenderContextSubstitutesAllPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{
		Code:   "ATM",
		Name:   "Agent Tasks Management",
		ATMBin: "/usr/local/bin/atm",
		Actor:  "opencode-manager",
	})
	for _, placeholder := range []string{"<CODE>", "<PROJECT_NAME>", "<ATM_BIN>", "<ACTOR>"} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered context still contains %s", placeholder)
		}
	}
	for _, want := range []string{
		"ATM manager — ATM",
		"Project `ATM` (`Agent Tasks Management`)",
		"atm `/usr/local/bin/atm`",
		"autonomous owner",
		"conventions",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q", want)
		}
	}
}

func TestRenderContextPrinciplesPresent(t *testing.T) {
	got := RenderContext(ContextData{Code: "ATM", Name: "ATM", ATMBin: "/bin/atm", Actor: "m"})
	for _, frag := range []string{
		"autonomous owner",
		"relentlessly and frequently organize",
		"self-improvement",
		"label substrate",
	} {
		if !strings.Contains(got, frag) {
			t.Errorf("principle missing %q", frag)
		}
	}
}

func TestRenderContextActionCatalogPresent(t *testing.T) {
	got := RenderContext(ContextData{
		Code: "ATM", Name: "ATM", ATMBin: "/bin/atm", Actor: "m",
		CapabilityActions: []CapabilityAction{
			{Name: "mapping", Summary: "reconcile the project's context map against reality", Command: "contextmap"},
		},
	})
	for _, frag := range []string{"Curate", "Recall", "Mapping"} {
		if !strings.Contains(got, frag) {
			t.Errorf("action catalog missing %q", frag)
		}
	}
	for _, old := range []string{"Tracking", "Asking", "Glossary", "Planning", "Grooming", "Onboarding"} {
		if strings.Contains(got, old) {
			t.Errorf("rendered context still contains old role %q", old)
		}
	}
}

func TestRenderContextGenericKeepsPlaceholders(t *testing.T) {
	// The generic body (no project) is produced by leaving placeholders in place
	// so `atm manage-context` with no --project still renders a template.
	got := RenderContext(ContextData{})
	for _, placeholder := range []string{"<CODE>", "<ATM_BIN>"} {
		if !strings.Contains(got, placeholder) {
			t.Errorf("generic render stripped %s; placeholders must survive for template use", placeholder)
		}
	}
}

func TestManagerContextInjectsPersona(t *testing.T) {
	out := RenderContext(ContextData{
		Persona:            "manager",
		PersonaPrompt:      "curate the ledger",
		PersonaDescription: "Curates the ledger and oversees work.",
	})
	if !strings.Contains(out, "curate the ledger") {
		t.Error("manager context missing persona prompt")
	}
}

// The mapping procedure lives in the contextmap guide (capability obligation
// 4: it explains itself). The prompt template must point at the guide and
// must not restate any step of the procedure — restated prose is the drift
// class this initiative removes (see ATM-0114 for the original bug).
func TestTemplateMappingRolePointsAtGuide(t *testing.T) {
	rendered := RenderContext(ContextData{
		Code: "X", Name: "X", ATMBin: "atm", Actor: "a@b:c",
		CapabilityActions: []CapabilityAction{
			{Name: "mapping", Summary: "reconcile the project's context map against reality", Command: "contextmap"},
		},
	})
	if !strings.Contains(rendered, "atm contextmap guide") {
		t.Error("Mapping role must tell the manager to consult `atm contextmap guide`")
	}
	for _, banned := range []string{"context stamp", "context retarget", "context supersede", "DRIFT", "UNVERIFIED", "**Verify.**", "**Discover.**"} {
		if strings.Contains(rendered, banned) {
			t.Errorf("template still restates mapping procedure fragment %q", banned)
		}
	}
}

func TestRenderCapabilityRoles(t *testing.T) {
	rendered := RenderContext(ContextData{
		Code: "X", Name: "X", ATMBin: "atm", Actor: "a@b:c",
		CapabilityActions: []CapabilityAction{
			{Name: "mapping", Summary: "reconcile the context map", Command: "contextmap"},
		},
	})
	for _, want := range []string{
		"**Mapping**",
		"reconcile the context map",
		"`atm contextmap guide`",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered prompt missing %q", want)
		}
	}
	if strings.Contains(rendered, "<CAPABILITY_ROLES>") {
		t.Error("placeholder must be substituted when a project is rendered")
	}
}

func TestRenderNoCapabilityActions(t *testing.T) {
	rendered := RenderContext(ContextData{Code: "X", Name: "X", ATMBin: "atm", Actor: "a@b:c"})
	if strings.Contains(rendered, "<CAPABILITY_ROLES>") || strings.Contains(rendered, "**Mapping**") {
		t.Error("no contributed actions: role list must render empty, not leak placeholder or stale roles")
	}
	// The core roles survive composition untouched.
	for _, want := range []string{"**Curate**", "**Recall**"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("core role %q missing", want)
		}
	}
}

func TestRenderActionBlockConsult(t *testing.T) {
	rendered := RenderContext(ContextData{
		Code: "X", Name: "X", ATMBin: "atm", Actor: "a@b:c",
		Action: "mapping", ActionConsult: "contextmap",
	})
	if !strings.Contains(rendered, "Focus this session on **mapping**") {
		t.Error("action block missing")
	}
	if !strings.Contains(rendered, "atm contextmap guide") {
		t.Error("capability action block must point at the capability guide")
	}
}
