package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPersonaCreateListShowEditRemove(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()

	if _, se, code := h.run("persona", "create", "--store", sp, "--name", "staff", "--prompt", "high bar", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("create: code=%d stderr=%s", code, se)
	}

	out, se, code := h.run("persona", "list", "--store", sp, "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("list: code=%d stderr=%s", code, se)
	}
	var listed struct {
		Personas []struct{ Name string } `json:"personas"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}
	// Built-ins (developer/manager/admin) are lazily seeded by validateActor,
	// so the list contains them plus the staff persona created above.
	var hasStaff bool
	for _, p := range listed.Personas {
		if p.Name == "staff" {
			hasStaff = true
		}
	}
	if !hasStaff {
		t.Fatalf("list missing staff: %s", out)
	}

	out, se, code = h.run("persona", "show", "--store", sp, "--name", "staff", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("show: code=%d stderr=%s", code, se)
	}
	if !strings.Contains(out, "high bar") {
		t.Fatalf("show = %s", out)
	}

	if _, se, code := h.run("persona", "edit", "--store", sp, "--name", "staff", "--description", "reviewer", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("edit: code=%d stderr=%s", code, se)
	}

	out, se, code = h.run("persona", "show", "--store", sp, "--name", "staff", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("show after edit: code=%d stderr=%s", code, se)
	}
	if !strings.Contains(out, "reviewer") || !strings.Contains(out, "high bar") {
		t.Fatalf("edit lost data: %s", out)
	}

	if _, se, code := h.run("persona", "remove", "--store", sp, "--name", "staff", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("remove: code=%d stderr=%s", code, se)
	}

	if _, se, code := h.run("persona", "show", "--store", sp, "--name", "staff", "--actor", "admin@cli:unset"); code == 0 {
		t.Fatalf("show after remove should fail, stderr=%s", se)
	}
}

func TestPersonaPromptMutualExclusion(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()

	_, se, code := h.run("persona", "create", "--store", sp, "--name", "x", "--prompt", "a", "--prompt-file", "/tmp/x", "--actor", "admin@cli:unset")
	if code == 0 {
		t.Fatalf("want mutual-exclusion error, got code=0")
	}
	if !strings.Contains(se, "prompt") {
		t.Fatalf("want mutual-exclusion error mentioning prompt, got stderr=%s", se)
	}
}

func TestPersonaShowPositional(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.output = outputText

	out, se, code := h.run("persona", "show", "--store", sp, "manager", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("show manager: code=%d stderr=%s", code, se)
	}
	if !strings.Contains(out, "manager\t") {
		t.Fatalf("show manager output missing name line: %s", out)
	}
	if strings.Contains(out, "modes:") {
		t.Fatalf("manager declares no modes; output must not contain a modes line: %s", out)
	}
}

func TestPersonaPersonalityRoundTrip(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()

	if _, se, code := h.run("persona", "personality", "--store", sp, "manager", "--set", "Dry wit.", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("set personality: code=%d stderr=%s", code, se)
	}
	out, se, code := h.run("persona", "personality", "--store", sp, "manager", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("get personality: code=%d stderr=%s", code, se)
	}
	if !strings.Contains(out, "Dry wit.") {
		t.Fatalf("get personality = %s, want Dry wit.", out)
	}
	if _, se, code := h.run("persona", "personality", "--store", sp, "manager", "--clear", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("clear personality: code=%d stderr=%s", code, se)
	}
	out, se, code = h.run("persona", "personality", "--store", sp, "manager", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("get personality after clear: code=%d stderr=%s", code, se)
	}
	if strings.Contains(out, "Dry wit.") {
		t.Fatalf("personality after clear still customized: %s", out)
	}
}

func TestPersonaEditBuiltinRefusedWithHint(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()

	_, se, code := h.run("persona", "edit", "--store", sp, "--name", "manager", "--prompt", "x", "--actor", "admin@cli:unset")
	if code == 0 {
		t.Fatalf("edit manager should fail, got code=0")
	}
	if !strings.Contains(se, "atm persona personality") {
		t.Fatalf("edit manager error should mention `atm persona personality`, got stderr=%s", se)
	}
}
