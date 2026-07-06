package version

import (
	"fmt"
	"runtime"
	"strings"

	"atm/internal/store"
)

// Info returns the full version info map used by both formatters.
func Info() map[string]any {
	return map[string]any{
		"version": Version,
		"commit":  Commit,
		"date":    Date,
		"os":      runtime.GOOS,
		"arch":    runtime.GOARCH,
	}
}

// FormatText renders the human-readable version line. Empty commit and date
// segments are trimmed; when both are empty the parenthetical collapses to
// just "<os>/<arch>".
func FormatText(info map[string]string) string {
	var segs []string
	if info["commit"] != "" {
		segs = append(segs, "commit: "+info["commit"])
	}
	if info["date"] != "" {
		segs = append(segs, "built: "+info["date"])
	}
	segs = append(segs, info["os"]+"/"+info["arch"])
	return fmt.Sprintf("atm %s (%s)", info["version"], strings.Join(segs, ", "))
}

// EmitJSON renders the deterministic JSON object via the store's sorted
// marshaller so key order is stable: arch, commit, date, os, version
// (alphabetical, matching every other JSON-emitting CLI subcommand).
func EmitJSON(info map[string]any) string {
	data, err := store.MarshalSorted(info)
	if err != nil {
		return "{}\n"
	}
	return string(data)
}
