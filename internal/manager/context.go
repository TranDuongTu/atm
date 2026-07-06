package manager

import (
	_ "embed"
	"strings"
)

//go:embed context_v1.md
var contextV1 string

type ContextData struct {
	Code      string
	Name      string
	ATMBin    string
	Actor     string
	RunID     string
	Timestamp string
}

// RenderContext substitutes the ContextData placeholders into the manager
// prompt template. Empty fields leave their placeholder in place so the
// env-driven generic body (used by atm-manager subagent definitions) can be
// produced by calling RenderContext with zero values.
func RenderContext(data ContextData) string {
	pairs := []string{
		"<CODE>", data.Code,
		"<PROJECT_NAME>", data.Name,
		"<ATM_BIN>", data.ATMBin,
		"<ACTOR>", data.Actor,
		"<RUN_ID>", data.RunID,
		"<TIMESTAMP>", data.Timestamp,
	}
	// Build a replacer that only substitutes non-empty values; empty values
	// are replaced with the placeholder itself so it survives.
	final := make([]string, 0, len(pairs))
	for i := 0; i < len(pairs); i += 2 {
		key, val := pairs[i], pairs[i+1]
		if val == "" {
			final = append(final, key, key)
		} else {
			final = append(final, key, val)
		}
	}
	return strings.NewReplacer(final...).Replace(contextV1)
}