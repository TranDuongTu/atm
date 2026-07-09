package cli

import "testing"

func TestResolveActorDefaults(t *testing.T) {
	cases := map[string]string{
		"":                      "admin@cli:unset",
		"reviewer":              "reviewer@cli:unset",
		"developer@claude:opus": "developer@claude:opus",
	}
	for in, want := range cases {
		st := &cliState{flags: globalFlags{actor: in}}
		got, err := st.resolveActor(true)
		if err != nil || got != want {
			t.Errorf("resolveActor(%q) = %q,%v; want %q", in, got, err, want)
		}
	}
}
