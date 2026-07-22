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
