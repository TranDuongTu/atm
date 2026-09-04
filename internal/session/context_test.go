package session

import (
	"slices"
	"strings"
	"testing"
)

func TestRenderContextFull(t *testing.T) {
	out := RenderContext(ContextData{
		Code:          "ATM",
		Name:          "Agent Tasks Management",
		Actor:         "developer@claude:unset",
		TaskID:        "ATM-212d04",
		PersonaPrompt: "## Persona: developer\n\nTest prompt.",
	})
	if !strings.Contains(out, "- Project: `ATM` (`Agent Tasks Management`)") {
		t.Fatal("project line missing")
	}
	if !strings.Contains(out, "- Actor: `developer@claude:unset`") {
		t.Fatal("actor line missing")
	}
	if !strings.Contains(out, "- Task: `ATM-212d04`") {
		t.Fatal("task line missing")
	}
	if !strings.Contains(out, "## Persona: developer") {
		t.Fatal("persona prompt missing")
	}
}

// TestRenderContextAnatomy pins the v3 framing: three top-level headers in
// who / what / where order, each introducing the material it frames. The
// anatomy is the whole point of the template — a section that drifts out of
// order stops answering the question its header asks.
func TestRenderContextAnatomy(t *testing.T) {
	out := RenderContext(ContextData{
		Code: "ATM", Name: "Agent Tasks Management", Actor: "a@b:c",
		PersonaPrompt: "## Persona: developer\n\nTest prompt.",
	})
	iWho := strings.Index(out, "# Who you are")
	iWhat := strings.Index(out, "# What you do")
	iWhere := strings.Index(out, "# Where you work")
	if iWho < 0 || iWhat < 0 || iWhere < 0 {
		t.Fatalf("missing framing headers (who=%d what=%d where=%d):\n%s", iWho, iWhat, iWhere, out)
	}
	if !(iWho < iWhat && iWhat < iWhere) {
		t.Fatalf("want who < what < where, got %d/%d/%d:\n%s", iWho, iWhat, iWhere, out)
	}
	if i := strings.Index(out, "## Persona: "); i < iWho || i > iWhat {
		t.Errorf("the persona prompt belongs under 'Who you are' (at %d, section %d..%d)", i, iWho, iWhat)
	}
	// The three framing headers plus the title are the ONLY "#"-level lines:
	// anything else at that level reads as a peer of the anatomy instead of
	// as material inside it.
	var top []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "# ") {
			top = append(top, line)
		}
	}
	want := []string{"# ATM session — ATM", "# Who you are", "# What you do", "# Where you work"}
	if !slices.Equal(top, want) {
		t.Errorf("top-level headers = %v, want %v", top, want)
	}
	// v2's tail sections are what the anatomy replaces.
	for _, gone := range []string{"## Orientation", "## Persona Prompting", "## Persona and checklists"} {
		if strings.Contains(out, gone) {
			t.Errorf("v2 section %q survived the v3 anatomy", gone)
		}
	}
}

// TestRenderContextWorkingPrinciples pins the two ATM principles the "where"
// section carries. They are the behaviour every session is expected to hold
// to, so they are template content, not persona content: a project that
// rewords its personas must not be able to lose them.
func TestRenderContextWorkingPrinciples(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM", Actor: "a@b:c", PersonaPrompt: "P"})
	for _, want := range []string{
		"ATM is this project's shared memory",
		"**Journal as you go.**",
		"**Preserve the truth.**",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing working-principles content %q:\n%s", want, out)
		}
	}
	iWhere := strings.Index(out, "# Where you work")
	if i := strings.Index(out, "**Journal as you go.**"); i < iWhere {
		t.Errorf("the principles belong under 'Where you work' (at %d, section starts %d)", i, iWhere)
	}
}

func TestRenderContextNoProjectLeavesPlaceholders(t *testing.T) {
	out := RenderContext(ContextData{Actor: "manager@claude:unset"})
	if !strings.Contains(out, "<CODE>") {
		t.Fatal("empty project must leave <CODE> placeholders literal")
	}
	if !strings.Contains(out, "<TASK_ID>") {
		t.Fatal("empty TaskID must remain as placeholder literal")
	}
}

func TestRenderContextPersonaPromptInjected(t *testing.T) {
	out := RenderContext(ContextData{
		Code:          "ATM",
		Name:          "n",
		Actor:         "a",
		PersonaPrompt: "## Persona: custom\n\nCustom content.\n",
	})
	if !strings.Contains(out, "Custom content.") {
		t.Fatal("persona prompt not injected")
	}
	if strings.Contains(out, "<PERSONA_PROMPT>") {
		t.Fatal("placeholder not replaced")
	}
}

func TestRenderContextEmptyActor(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM", Name: "n"})
	if !strings.Contains(out, "<ACTOR>") {
		t.Fatal("empty Actor must remain as placeholder literal")
	}
}

func TestRenderContextCapabilityScope(t *testing.T) {
	d := ContextData{Code: "ATM", Name: "Acme", Actor: "manager@claude:test", Capability: "channel", PersonaPrompt: "PROMPT"}
	out := RenderContext(d)
	for _, want := range []string{
		"## Session scope",
		"scoped to the `channel` capability",
		"atm capability channel guide",
		"for project ATM",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("scoped context missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "<CAPABILITY_SCOPE>") {
		t.Error("placeholder leaked into scoped render")
	}
	if idx := strings.Index(out, "## Session scope"); idx > strings.Index(out, "# Where you work") {
		t.Error("scope section must precede 'Where you work'")
	}
}

func TestRenderContextNoCapabilityNoResidue(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM", Actor: "a@b:c", PersonaPrompt: "PROMPT"})
	if strings.Contains(out, "CAPABILITY_SCOPE") || strings.Contains(out, "Session scope") {
		t.Errorf("unscoped context must carry no scope residue:\n%s", out)
	}
}

// TestRenderContextCapabilityNames proves the enabled set reaches the context
// as a NAME LIST, not as pasted briefs. v2 pasted every capability's brief
// into a ## Capabilities block; v3 names them and points at the guide, so
// the context file stops carrying content `atm capability <name> guide`
// already answers.
func TestRenderContextCapabilityNames(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM", CapabilityNames: "channel, scrum, qa"})
	if !strings.Contains(out, "Enabled capabilities: channel, scrum, qa") {
		t.Fatalf("capability names not rendered:\n%s", out)
	}
	if strings.Contains(out, "<CAPABILITY_NAMES>") {
		t.Fatalf("placeholder residue:\n%s", out)
	}
	if strings.Contains(out, "## Capabilities") {
		t.Fatalf("the v2 briefs block must not survive:\n%s", out)
	}
	if i, iWhere := strings.Index(out, "Enabled capabilities:"), strings.Index(out, "# Where you work"); i < iWhere {
		t.Fatalf("the capability names belong under 'Where you work' (at %d, section starts %d)", i, iWhere)
	}
}

// TestRenderContextNoCapabilityNamesLeavesPlaceholder: an empty set is the
// generic-template case (`atm session-context` with no project), which every
// other empty field renders as its literal placeholder. The capability names
// follow that rule rather than the Session-scope one, because a context with
// no capability list is still a legible template, while a scope section
// naming no capability would be meaningless.
func TestRenderContextNoCapabilityNamesLeavesPlaceholder(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM"})
	if !strings.Contains(out, "<CAPABILITY_NAMES>") {
		t.Fatalf("empty CapabilityNames must leave the placeholder literal:\n%s", out)
	}
}

func TestRenderContextChecklistSections(t *testing.T) {
	out := RenderContext(ContextData{
		Code: "ATM", Name: "Agent Tasks Management", Actor: "a@b:c",
		PersonaPrompt: "P",
		Capability:    "scrum",
		Checklists: []ChecklistSection{
			{Name: "scrum-coding", Purpose: "How one task flows.", StepsRendered: "1. claim\n2. build\n"},
			{Name: "pr-review", Purpose: "PR shape.", StepsRendered: "1. title\n"},
		},
	})
	i1 := strings.Index(out, "## Checklist: scrum-coding")
	i2 := strings.Index(out, "## Checklist: pr-review")
	iWhat := strings.Index(out, "# What you do")
	iScope := strings.Index(out, "## Session scope")
	if i1 < 0 || i2 < 0 || i2 < i1 {
		t.Fatalf("checklist sections missing/misordered:\n%s", out)
	}
	if i1 < iWhat {
		t.Fatalf("checklists belong under 'What you do':\n%s", out)
	}
	if iScope >= 0 && i2 > iScope {
		t.Fatalf("checklists must precede the capability scope:\n%s", out)
	}
	// Purpose renders italic, per the template's rendering rules.
	if !strings.Contains(out, "*How one task flows.*") {
		t.Errorf("purpose must render italic:\n%s", out)
	}
	if !strings.Contains(out, "1. claim\n2. build") {
		t.Errorf("step tree missing:\n%s", out)
	}
	if strings.Contains(out, "<CHECKLISTS_SECTIONS>") {
		t.Fatalf("placeholder residue:\n%s", out)
	}
}

// TestRenderContextRereadInstructionIsStatedOnce: v2 appended a re-read line
// to every rendered checklist; v3 states the rule once in the "What you do"
// prose. N copies of one instruction is noise that scales with the checklist
// count.
func TestRenderContextRereadInstructionIsStatedOnce(t *testing.T) {
	out := RenderContext(ContextData{
		Code: "ATM", PersonaPrompt: "P",
		Checklists: []ChecklistSection{
			{Name: "a", StepsRendered: "1. x\n"},
			{Name: "b", StepsRendered: "1. y\n"},
		},
	})
	if n := strings.Count(out, "atm checklist show --project ATM --name"); n != 1 {
		t.Fatalf("the re-read instruction must be stated once, got %d:\n%s", n, out)
	}
	if iWhat, iFirst := strings.Index(out, "# What you do"), strings.Index(out, "atm checklist show --project ATM --name"); iFirst < iWhat {
		t.Fatalf("the re-read instruction belongs in the 'What you do' prose:\n%s", out)
	}
}

// TestRenderContextNoChecklistsFallback: a session dispatched without a
// checklist is told so explicitly. Silence would read as "there is no
// procedure section", which is indistinguishable from a rendering bug.
func TestRenderContextNoChecklistsFallback(t *testing.T) {
	out := RenderContext(ContextData{Code: "X", PersonaPrompt: "P"})
	if !strings.Contains(out, "No checklists were selected for this session.") {
		t.Fatalf("missing the no-checklist fallback:\n%s", out)
	}
	// "## Checklist: " with the trailing space is a section header; the
	// prose mentions the backticked command form, which must not count.
	if strings.Contains(out, "## Checklist: ") || strings.Contains(out, "<CHECKLISTS_SECTIONS>") {
		t.Fatalf("residue without checklists:\n%s", out)
	}
}
