package developing

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

func RenderContext(data ContextData) string {
	replacer := strings.NewReplacer(
		"<CODE>", data.Code,
		"<PROJECT_NAME>", data.Name,
		"<ATM_BIN>", data.ATMBin,
		"<ACTOR>", data.Actor,
		"<RUN_ID>", data.RunID,
		"<TIMESTAMP>", data.Timestamp,
	)
	return replacer.Replace(contextV1)
}
