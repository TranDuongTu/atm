package version

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
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

// EmitJSON renders the deterministic JSON object: encoding/json marshals
// map keys in sorted order (arch, commit, date, os, version), two-space
// indent, no HTML escaping, no trailing newline — byte-identical to the
// store-backed marshaller this replaced.
func EmitJSON(info map[string]any) string {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(info); err != nil {
		return "{}\n"
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
