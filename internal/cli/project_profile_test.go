package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// projectCapabilities reads the project's recorded capability set through
// the agent endpoint (`project capability list --output json`, key
// "capabilities").
func projectCapabilities(t *testing.T, st *testCLI, code string) []string {
	t.Helper()
	out := runArgsOut(t, st, "project", "capability", "list", "--project", code, "--output", "json")
	var got struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	sort.Strings(got.Capabilities)
	return got.Capabilities
}

func TestProjectCreateWithProfileByName(t *testing.T) {
	st := applyCLI(t)
	installDemo(t, st, "1.0.0")
	out := runArgsOut(t, st, "project", "create", "--code", "FRESH", "--name", "fresh", "--profile", "demo", "--actor", "admin@cli:unset")
	mustContain(t, out, "created project FRESH")
	mustContain(t, out, "applied demo@1.0.0 to FRESH")
	for _, line := range []string{"persona\tcoder\tcreate", "checklist\twork\tcreate", "channel\tdesign\tcreate"} {
		mustContain(t, out, line)
	}
	mustContain(t, out, "Remaining setup")
	// Exactly what the profile requires: scrum + channel from the
	// manifest, checklist implied by shipping checklists — not the
	// registry default set.
	if got := projectCapabilities(t, st, "FRESH"); strings.Join(got, ",") != "channel,checklist,scrum" {
		t.Fatalf("capabilities = %v", got)
	}
	show := runArgsOut(t, st, "checklist", "show", "--project", "FRESH", "--name", "work")
	mustContain(t, show, "origin: demo@1.0.0")
}

func TestProjectCreateWithProfileAtVersionAndExtraCapabilities(t *testing.T) {
	st := applyCLI(t)
	installDemo(t, st, "1.0.0")
	installDemo(t, st, "1.1.0")
	out := runArgsOut(t, st, "project", "create", "--code", "FRESH", "--name", "fresh", "--profile", "demo@1.0.0", "--capabilities", "qa", "--actor", "admin@cli:unset")
	mustContain(t, out, "applied demo@1.0.0 to FRESH")
	if got := projectCapabilities(t, st, "FRESH"); strings.Join(got, ",") != "channel,checklist,qa,scrum" {
		t.Fatalf("capabilities = %v", got)
	}
}

func TestProjectCreateWithProfileDirAppliesAsDev(t *testing.T) {
	st := applyCLI(t)
	dir := writeApplyProfileDir(t, "1.0.0")
	out := runArgsOut(t, st, "project", "create", "--code", "FRESH", "--name", "fresh", "--profile", dir, "--actor", "admin@cli:unset")
	mustContain(t, out, "applied demo@dev to FRESH")
}

func TestProjectCreateWithProfileValidatesBeforeCreating(t *testing.T) {
	st := applyCLI(t)
	dir := writeApplyProfileDir(t, "1.0.0")
	// The profile is internally consistent — it still requires scrum and
	// channel for its own documents — but names a capability no build
	// provides, so only the registry check can refuse it.
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte("name: demo\nversion: 1.0.0\nformat: 1\nrequires_capabilities: [scrum, channel, ghost]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg, code := runChecklistErrText(t, st, "project", "create", "--code", "FRESH", "--name", "fresh", "--profile", dir, "--actor", "admin@cli:unset")
	if code == ExitSuccess {
		t.Fatal("create succeeded with an unsatisfiable profile")
	}
	mustContain(t, msg, "ghost")
	if strings.Contains(runArgsOut(t, st, "project", "list"), "FRESH") {
		t.Fatal("project was created before validation failed")
	}
}

func TestProjectCreateWithUnknownProfileRef(t *testing.T) {
	st := applyCLI(t)
	msg, code := runChecklistErrText(t, st, "project", "create", "--code", "FRESH", "--name", "fresh", "--profile", "nope", "--actor", "admin@cli:unset")
	if code == ExitSuccess {
		t.Fatal("create succeeded with an unknown profile")
	}
	mustContain(t, msg, "nope")
	if strings.Contains(runArgsOut(t, st, "project", "list"), "FRESH") {
		t.Fatal("project was created")
	}
}

func TestProjectCreateWithProfileJSON(t *testing.T) {
	st := applyCLI(t)
	installDemo(t, st, "1.0.0")
	out := runArgsOut(t, st, "project", "create", "--code", "FRESH", "--name", "fresh", "--profile", "demo", "--actor", "admin@cli:unset", "--output", "json")
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	for _, key := range []string{"project", "plan", "applied", "setup"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("json lacks %q: %s", key, out)
		}
	}
}
