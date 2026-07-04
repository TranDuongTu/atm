package onboard

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"
)

//go:embed prompt_opencode_v1.md
var promptOpencodeV1 string

// Latest is the prompt version used when --prompt-version is not specified.
// A new prompt version = a new prompt_opencode_v<N>.md file + a bump here.
const Latest = "v1"

// errUnknownVersion is returned by Render when the requested version does not
// match any embedded prompt asset.
var errUnknownVersion = errors.New("unknown prompt version")

// Data carries the values substituted into the prompt template at render time.
type Data struct {
	Code          string
	Name          string
	ATMBin        string
	Actor         string
	RunID         string
	Timestamp     string
	ExistingTasks string // pre-rendered markdown table (or "(none)" if empty)
}

// Render substitutes the placeholders in the prompt template for the requested
// version and returns the rendered markdown. Unknown versions return
// errUnknownVersion (wrapped with the requested version for the CLI to map to
// exit 2).
func Render(version string, data Data) (string, error) {
	var tmpl string
	switch version {
	case "v1":
		tmpl = promptOpencodeV1
	default:
		return "", fmt.Errorf("%w: %q", errUnknownVersion, version)
	}
	if data.ExistingTasks == "" {
		data.ExistingTasks = "(none)"
	}
	replacer := strings.NewReplacer(
		"<CODE>", data.Code,
		"<PROJECT_NAME>", data.Name,
		"<ATM_BIN>", data.ATMBin,
		"<ACTOR>", data.Actor,
		"<RUN_ID>", data.RunID,
		"<TIMESTAMP>", data.Timestamp,
		"<EXISTING_TASKS>", data.ExistingTasks,
	)
	return replacer.Replace(tmpl), nil
}
