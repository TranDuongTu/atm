package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPersonaCreateListShowEditRemove(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()

	if _, se, code := h.run("persona", "create", "--store", sp, "--name", "staff", "--prompt", "high bar", "--actor", "t"); code != 0 {
		t.Fatalf("create: code=%d stderr=%s", code, se)
	}

	out, se, code := h.run("persona", "list", "--store", sp, "--actor", "t")
	if code != 0 {
		t.Fatalf("list: code=%d stderr=%s", code, se)
	}
	var listed struct {
		Personas []struct{ Name string } `json:"personas"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Personas) != 1 || listed.Personas[0].Name != "staff" {
		t.Fatalf("list = %s", out)
	}

	out, se, code = h.run("persona", "show", "--store", sp, "--name", "staff", "--actor", "t")
	if code != 0 {
		t.Fatalf("show: code=%d stderr=%s", code, se)
	}
	if !strings.Contains(out, "high bar") {
		t.Fatalf("show = %s", out)
	}

	if _, se, code := h.run("persona", "edit", "--store", sp, "--name", "staff", "--description", "reviewer", "--actor", "t"); code != 0 {
		t.Fatalf("edit: code=%d stderr=%s", code, se)
	}

	out, se, code = h.run("persona", "show", "--store", sp, "--name", "staff", "--actor", "t")
	if code != 0 {
		t.Fatalf("show after edit: code=%d stderr=%s", code, se)
	}
	if !strings.Contains(out, "reviewer") || !strings.Contains(out, "high bar") {
		t.Fatalf("edit lost data: %s", out)
	}

	if _, se, code := h.run("persona", "remove", "--store", sp, "--name", "staff", "--actor", "t"); code != 0 {
		t.Fatalf("remove: code=%d stderr=%s", code, se)
	}

	if _, se, code := h.run("persona", "show", "--store", sp, "--name", "staff", "--actor", "t"); code == 0 {
		t.Fatalf("show after remove should fail, stderr=%s", se)
	}
}

func TestPersonaPromptMutualExclusion(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()

	_, se, code := h.run("persona", "create", "--store", sp, "--name", "x", "--prompt", "a", "--prompt-file", "/tmp/x", "--actor", "t")
	if code == 0 {
		t.Fatalf("want mutual-exclusion error, got code=0")
	}
	if !strings.Contains(se, "prompt") {
		t.Fatalf("want mutual-exclusion error mentioning prompt, got stderr=%s", se)
	}
}
