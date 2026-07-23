package dispatch

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config is the user-level dispatch configuration, stored as dispatch.json
// at the store root (sibling of agents.json). Hand-edited; no writer here.
type Config struct {
	// TerminalCmd overrides terminal detection: a command template run via
	// `sh -c` with {cmd}, {dir}, {title} placeholders.
	TerminalCmd string `json:"terminal_cmd,omitempty"`
}

// LoadConfig reads path; a missing file is a zero Config, a malformed file
// is an error naming the path.
func LoadConfig(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return c, nil
}