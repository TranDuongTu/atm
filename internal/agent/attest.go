package agent

import (
	"sort"

	"atm/internal/core"
)

// ConfiguredKeys lists the selection keys this machine has configured — the
// selected agent plus any that carry args or a model — sorted, deduplicated,
// and filtered to keys that actually parse.
//
// It is the roster every readiness surface grades against: `atm profile
// status`, the channels overlay's attestation matrix, and the Profiles
// overlay all mean the same thing by "configured agent", so they read it
// from here rather than each deciding.
func ConfiguredKeys(cfg core.AgentsConfig) []string {
	seen := map[string]bool{}
	var out []string
	add := func(k string) {
		if k == "" || seen[k] {
			return
		}
		if _, err := ParseSelectionKey(k); err != nil {
			return
		}
		seen[k] = true
		out = append(out, k)
	}
	add(cfg.Selected)
	for k := range cfg.Args {
		add(k)
	}
	for k := range cfg.Models {
		add(k)
	}
	sort.Strings(out)
	return out
}

// AttestingSegment maps a selection key to the actor segment its sessions
// record stamps under. A session actor is persona@LAUNCHER:model, so a native
// claude session attests as "claude" while an ollama-launched one — whatever
// harness it hosts — attests as "ollama". The attestation matrix must key on
// what stamps actually SAY, not on the harness the launcher hosts.
func AttestingSegment(key string) string {
	if sel, err := ParseSelectionKey(key); err == nil {
		if sel.Launcher == LauncherOllama {
			return "ollama"
		}
		return sel.Agent
	}
	// A bare "ollama" (ATM_AGENT inside an ollama-launched session) is a
	// legitimate segment — it is what those sessions' stamps record.
	return key
}

// AttestingAgents is the deduplicated set of attesting segments for this
// machine's configured agents: the columns of the attestation matrix.
func AttestingAgents(cfg core.AgentsConfig) []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range ConfiguredKeys(cfg) {
		if h := AttestingSegment(k); !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}
