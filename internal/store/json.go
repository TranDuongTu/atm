package store

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func WriteFileAtomic(path string, v any) error {
	data, err := MarshalSorted(v)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func WriteJSON(path string, v any) error { return WriteFileAtomic(path, v) }

func ReadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	return dec.Decode(v)
}
