package store

import (
	"os"
	"path/filepath"
)

// AgentsConfig is the global (store-root) record of the user's host-agent
// preference: which catalog entry is selected for atm dev / atm manage, and
// per-entry default passthrough args. It lives at <root>/agents.json, distinct
// from the per-project config.json.
type AgentsConfig struct {
	UpdatedAt string              `json:"updated_at,omitempty"`
	UpdatedBy string              `json:"updated_by,omitempty"`
	Selected  string              `json:"selected,omitempty"`
	Args      map[string][]string `json:"args,omitempty"`
}

func (s *Store) agentsConfigPath() string {
	return filepath.Join(s.Root, "agents.json")
}

// GetAgentsConfig returns the stored agents config, or a zero value when the
// file does not yet exist.
func (s *Store) GetAgentsConfig() (AgentsConfig, error) {
	var c AgentsConfig
	if err := ReadJSON(s.agentsConfigPath(), &c); err != nil {
		if os.IsNotExist(err) {
			return AgentsConfig{}, nil
		}
		return AgentsConfig{}, err
	}
	return c, nil
}

func (s *Store) writeAgentsConfig(mutate func(*AgentsConfig), actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	c, err := s.GetAgentsConfig()
	if err != nil {
		return err
	}
	mutate(&c)
	c.UpdatedAt = RFC3339UTC(Now())
	c.UpdatedBy = actor
	return WriteFileAtomic(s.agentsConfigPath(), &c)
}

// SetSelectedAgent records name as the active catalog entry.
func (s *Store) SetSelectedAgent(name, actor string) error {
	return s.writeAgentsConfig(func(c *AgentsConfig) {
		c.Selected = name
	}, actor)
}

// SetAgentArgs records name's default passthrough args. Passing nil or an empty
// slice clears the entry.
func (s *Store) SetAgentArgs(name string, args []string, actor string) error {
	return s.writeAgentsConfig(func(c *AgentsConfig) {
		if len(args) == 0 {
			delete(c.Args, name)
			return
		}
		if c.Args == nil {
			c.Args = map[string][]string{}
		}
		c.Args[name] = args
	}, actor)
}
