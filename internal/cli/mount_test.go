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
		{"project flag", []string{"scrum", "guide", "--project", "ATM"}, nil, "ATM"},
		{"project eq flag", []string{"--project=ATM", "conventions"}, nil, "ATM"},
		{"task id prefix", []string{"scrum", "claim", "--task", "ATM-3b873c"}, nil, "ATM"},
		{"task id eq", []string{"scrum", "claim", "--task=MY-PROJ-4f"}, nil, "MY-PROJ"},
		{"legacy id flag", []string{"scrum", "claim", "--id", "ATM-0042"}, nil, "ATM"},
		{"env fallback", []string{"conventions"}, map[string]string{"ATM_PROJECT": "ENV"}, "ENV"},
		{"flag beats env", []string{"--project", "FLAG"}, map[string]string{"ATM_PROJECT": "ENV"}, "FLAG"},
		{"nothing", []string{"conventions"}, nil, ""},
		{"task id no dash", []string{"scrum", "claim", "--task", "nodash"}, nil, ""},
		{"dangling flag", []string{"scrum", "claim", "--task"}, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mountProjectCode(c.args, env(c.env)); got != c.want {
				t.Fatalf("mountProjectCode(%v) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

// The gate end-to-end: a project that did not enable qa does not get the qa
// command mounted under `atm capability`; a capability it did enable is
// mounted; resolution failure mounts everything (degrade open).
func TestHardGateMountsOnlyEnabledCapabilities(t *testing.T) {
	h := newGoldenHarness(t)
	// NOCAP is a valid project code (^[A-Z]{3,6}$); a code that fails validation
	// would leave the project uncreated, GetProject would fail, and the mount
	// would degrade-open the FULL registry — masking the gate under test.
	if _, stderr, code := h.run("project", "create", "--code", "NOCAP", "--name", "no caps",
		"--capabilities", "scrum", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("create NOCAP: exit %d; stderr=%q", code, stderr)
	}

	_, _, code := h.run("capability", "qa", "seed", "--project", "NOCAP", "--actor", "admin@cli:unset")
	if code == 0 {
		t.Fatalf("qa must be unmounted for NOCAP")
	}
	// A scrum verb may legitimately fail on its own arguments; the point is the
	// command must be FOUND. Assert the failure is not "unknown command".
	if _, stderr, code := h.run("capability", "scrum", "lanes", "--project", "NOCAP"); code != 0 {
		if strings.Contains(stderr, "unknown command") {
			t.Fatal("scrum must stay mounted for NOCAP")
		}
	}
	// Unknown project: degrade open — qa under `atm capability` must be found.
	if _, stderr, _ := h.run("capability", "qa", "--help", "--project", "NOPE"); strings.Contains(stderr, "unknown command") {
		t.Fatal("resolution failure must mount the full registry under atm capability")
	}
}
