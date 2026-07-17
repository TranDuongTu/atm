package core

import (
	"encoding/json"
	"sort"
	"strings"
)

func MarshalSorted(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var dec any
	decUseNumber := json.NewDecoder(strings.NewReader(string(raw)))
	decUseNumber.UseNumber()
	if err := decUseNumber.Decode(&dec); err != nil {
		return nil, err
	}
	sorted := sortKeys(dec)
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(sorted); err != nil {
		return nil, err
	}
	out := buf.String()
	out = strings.TrimSuffix(out, "\n")
	return []byte(out), nil
}

func sortKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(t))
		for _, k := range keys {
			out[k] = sortKeys(t[k])
		}
		return out
	case []any:
		for i := range t {
			t[i] = sortKeys(t[i])
		}
		return t
	default:
		return v
	}
}
