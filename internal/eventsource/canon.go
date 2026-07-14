// Package eventsource implements the ATM v2 distributed event model:
// content-addressed events (L0), stored display aliases (L1), the
// convergent fold (L2), and the one-time v1→v2 upgrade (D6). See
// docs/eventsource/01-core-data-model.md for the model and
// docs/superpowers/specs/2026-07-14-eventsource-core-v2-design.md for the
// implementation decisions.
package eventsource

import (
	"encoding/json"

	"github.com/gowebpki/jcs"
)

// Canonicalize returns the RFC 8785 (JCS) canonical form of raw JSON.
// Event identity is the SHA-256 of these bytes (L0-1), so every hash in
// the system flows through this one function.
func Canonicalize(raw []byte) ([]byte, error) {
	// Validate that the input is valid JSON (jcs.Transform is lenient and
	// returns {} with nil error on malformed input, so this check is necessary)
	if !json.Valid(raw) {
		var dummy interface{}
		if err := json.Unmarshal(raw, &dummy); err != nil {
			return nil, err
		}
	}

	return jcs.Transform(raw)
}
