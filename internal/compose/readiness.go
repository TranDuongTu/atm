package compose

import (
	"atm/internal/core"
	"atm/internal/profile"
)

// ReadinessFor gathers the readiness inputs through the domain port and runs
// THE readiness computation. agents are harness names; readiness is
// per (endpoint × machine × agent), so the answer differs between them.
//
// It lives here because every adapter needs it and none of them should own
// it: `atm profile status`, a launch's warnings, and the dispatch dialog's
// greyed rows are the same question, and a second assembly of these inputs
// is a second answer waiting to disagree. internal/profile cannot hold it —
// that package is the FORMAT, and reading a project's current state is the
// adapters' work — so the shared binding layer is where it belongs.
func ReadinessFor(s core.Service, code string, agents []string) (*profile.Readiness, error) {
	return readinessFor(s, code, agents, true)
}

// ReadinessForUnprobed is ReadinessFor without the repo probes: the channel
// view comes from records and this machine's wiring alone.
//
// It exists for the dispatch dialog, which opens on a KEYPRESS and must not
// shell out to git once per repo to draw itself. The cost is precision at
// exactly one rung: a repo endpoint whose recorded path has since vanished
// reads as wired here, where the probing version would catch it. That is the
// right trade for a dialog — it warns, and the launch path, which probes,
// gives the authoritative answer a moment later.
func ReadinessForUnprobed(s core.Service, code string, agents []string) (*profile.Readiness, error) {
	return readinessFor(s, code, agents, false)
}

func readinessFor(s core.Service, code string, agents []string, probe bool) (*profile.Readiness, error) {
	proj, err := s.GetProject(code)
	if err != nil {
		return nil, err
	}
	in := profile.ReadinessInput{Code: code, Agents: agents, Now: core.Now()}
	in.Current.Enabled = proj.Capabilities
	if in.Current.Personas, err = s.PersonaRecords(code); err != nil {
		return nil, err
	}
	if in.Current.Checklists, err = s.ChecklistRecords(code); err != nil {
		return nil, err
	}
	if in.Current.Channels, err = s.ChannelRecords(code); err != nil {
		return nil, err
	}
	if probe {
		if in.Channels, err = s.ProjectChannels(code); err != nil {
			return nil, err
		}
	} else {
		in.Channels = channelViewsUnprobed(s, code)
	}
	if in.Available, err = s.ListProfiles(); err != nil {
		return nil, err
	}
	in.Profile = func(ref string) *core.Profile {
		o, err := core.ParseOrigin(ref)
		if err != nil || o.Kind != core.OriginProfile {
			return nil
		}
		p, _, err := s.GetProfile(o.Profile, o.Version)
		if err != nil {
			return nil
		}
		return p.ForProject(code)
	}
	return profile.ComputeReadiness(in), nil
}

// ReadinessInjection is the Service.Readiness func an adapter installs when
// it has a live store: the shared assembly, with errors swallowed into nil
// because a readiness that cannot be computed must degrade to the
// capability/channel fallback rather than fail a dispatch.
func ReadinessInjection(s core.Service) func(string, []string) *profile.Readiness {
	return readinessInjection(s, ReadinessFor)
}

// ReadinessInjectionUnprobed is the injection for keypress-time surfaces —
// the dispatch dialog — which cannot afford the repo probes. See
// ReadinessForUnprobed for what that costs.
func ReadinessInjectionUnprobed(s core.Service) func(string, []string) *profile.Readiness {
	return readinessInjection(s, ReadinessForUnprobed)
}

func readinessInjection(s core.Service, fn func(core.Service, string, []string) (*profile.Readiness, error)) func(string, []string) *profile.Readiness {
	return func(code string, agents []string) *profile.Readiness {
		if code == "" {
			return nil
		}
		r, err := fn(s, code, agents)
		if err != nil {
			return nil
		}
		return r
	}
}
