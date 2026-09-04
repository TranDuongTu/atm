package cli

import (
	"fmt"
	"os"

	"atm/internal/agent"
	"atm/internal/core"
)

// resolveAgentName picks the agent entry name for a launch: an explicit
// --agent flag wins, then the ATM_AGENT env override, then the stored
// selection. None set is a usage error.
func resolveAgentName(flagAgent string, cfg core.AgentsConfig) (string, error) {
	if flagAgent != "" {
		return flagAgent, nil
	}
	if v := os.Getenv("ATM_AGENT"); v != "" {
		return v, nil
	}
	if cfg.Selected != "" {
		return cfg.Selected, nil
	}
	return "", fmt.Errorf("%w: no agent selected; run `atm agents select <name>` or `atm init`", ErrUsage)
}

// resolveSelectionKey picks a selection key for a launch: an explicit --agent
// flag wins, then the ATM_AGENT env override, then the stored selection.
// Unlike resolveAgentName it accepts a launcher-prefixed key ("ollama:claude")
// and rejects nothing a selection key can name — dispatches like profile
// verify hand the key straight to launchSession, which resolves entries
// itself. ATM_AGENT is skipped: inside a launched session it names the
// launcher ("ollama"), not a selectable agent, and a verify dispatch that
// re-reads its own launch env would attest the wrong axis.
func resolveSelectionKey(flagAgent string, cfg core.AgentsConfig) (string, error) {
	key := flagAgent
	if key == "" && cfg.Selected != "" {
		key = cfg.Selected
	}
	if key == "" {
		return "", fmt.Errorf("%w: no agent selected; run `atm agents select <name>` or `atm init`", ErrUsage)
	}
	if _, err := agent.ParseSelectionKey(key); err != nil {
		return "", fmt.Errorf("%w: unknown agent %q (see `atm agents list`)", ErrUsage, key)
	}
	return key, nil
}

// resolveEntry resolves the launch agent to a catalog entry plus its stored
// default passthrough args.
func resolveEntry(flagAgent string, cfg core.AgentsConfig) (agent.Entry, []string, error) {
	name, err := resolveAgentName(flagAgent, cfg)
	if err != nil {
		return agent.Entry{}, nil, err
	}
	e, ok := agent.Lookup(name)
	if !ok {
		return agent.Entry{}, nil, fmt.Errorf("%w: unknown agent %q (see `atm agents list`)", ErrUsage, name)
	}
	return e, cfg.Args[name], nil
}
