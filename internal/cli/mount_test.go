package cli

import (
	"strings"
	"testing"
)

func TestMountProjectCode(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	cases := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{"project flag", []string{"workflow", "guide", "--project", "ATM"}, nil, "ATM"},
		{"project eq flag", []string{"--project=ATM", "conventions"}, nil, "ATM"},
		{"task id prefix", []string{"workflow", "start", "--task", "ATM-3b873c"}, nil, "ATM"},
		{"task id eq", []string{"workflow", "start", "--task=MY-PROJ-4f"}, nil, "MY-PROJ"},
		{"legacy id flag", []string{"workflow", "start", "--id", "ATM-0042"}, nil, "ATM"},
		{"env fallback", []string{"conventions"}, map[string]string{"ATM_PROJECT": "ENV"}, "ENV"},
		{"flag beats env", []string{"--project", "FLAG"}, map[string]string{"ATM_PROJECT": "ENV"}, "FLAG"},
		{"nothing", []string{"conventions"}, nil, ""},
		{"task id no dash", []string{"workflow", "start", "--task", "nodash"}, nil, ""},
		{"dangling flag", []string{"workflow", "start", "--task"}, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mountProjectCode(c.args, env(c.env)); got != c.want {
				t.Fatalf("mountProjectCode(%v) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

// The gate end-to-end: a project that disabled workflow does not get the
// workflow command mounted; a project that kept it does; resolution failure
// mounts everything (degrade open).
func TestHardGateMountsOnlyEnabledCapabilities(t *testing.T) {
	h := newGoldenHarness(t)
	// NOCAP is a valid project code (^[A-Z]{3,6}$); a code that fails validation
	// would leave the project uncreated, GetProject would fail, and the mount
	// would degrade-open the FULL registry — masking the gate under test.
	if _, stderr, code := h.run("project", "create", "--code", "NOCAP", "--name", "no caps",
		"--capabilities", "contextmap", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("create NOCAP: exit %d; stderr=%q", code, stderr)
	}

	_, stderr, code := h.run("workflow", "seed", "--project", "NOCAP", "--actor", "admin@cli:unset")
	if code == 0 {
		t.Fatalf("workflow must be unmounted for NOCAP; stderr=%q", stderr)
	}
	// context check may legitimately fail on a non-repo cwd; the point is the
	// command must be FOUND. Assert the failure is not "unknown command".
	if _, stderr, code := h.run("context", "check", "--project", "NOCAP"); code != 0 {
		if strings.Contains(stderr, "unknown command") {
			t.Fatal("context must stay mounted for NOCAP")
		}
	}
	// Unknown project: degrade open — workflow help must be found.
	if _, stderr, _ := h.run("workflow", "--help", "--project", "NOPE"); strings.Contains(stderr, "unknown command") {
		t.Fatal("resolution failure must mount the full registry")
	}
}
