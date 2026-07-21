package cli

import (
	"encoding/json"
	"testing"

	"atm/internal/core"
)

func TestMetaPresenceSortedSizesOnly(t *testing.T) {
	tk := &core.Task{ID: "PX-1", Meta: map[string]string{
		"workflow_ai": `{"v":1}`,
		"contextmap":  "cm",
	}}
	got := metaPresence(tk)
	if len(got) != 2 {
		t.Fatalf("presence = %+v", got)
	}
	if got[0].Capability != "contextmap" || got[0].Bytes != 2 {
		t.Errorf("first = %+v, want contextmap/2 (sorted by name, size only)", got[0])
	}
	if got[1].Capability != "workflow_ai" || got[1].Bytes != 7 {
		t.Errorf("second = %+v", got[1])
	}
	if metaPresence(&core.Task{ID: "PX-2"}) != nil {
		t.Error("no meta must yield nil, not empty slice noise")
	}
}

// TestTaskToJSONMetaEnvelope asserts the stable JSON shape of taskToJSON:
// a task WITH Meta carries a `meta` array of {capability, bytes} entries
// (content never leaked), and a task WITHOUT Meta omits `meta` entirely
// (the `omitempty` + nil return contract).
func TestTaskToJSONMetaEnvelope(t *testing.T) {
	withMeta := &core.Task{
		ID:          "PX-1",
		ProjectCode: "PX",
		Title:       "t",
		Meta:        map[string]string{"workflow_ai": `{"v":1}`},
	}
	data, err := json.Marshal(taskToJSON(withMeta, nil))
	if err != nil {
		t.Fatalf("marshal with meta: %v", err)
	}
	var env map[string]any
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal with meta: %v", err)
	}
	meta, ok := env["meta"]
	if !ok {
		t.Fatalf("with meta: `meta` field missing:\n%s", data)
	}
	arr, ok := meta.([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("with meta: `meta` = %+v, want 1-element array", meta)
	}
	entry, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("with meta: entry = %+v, want object", arr[0])
	}
	if entry["capability"] != "workflow_ai" || entry["bytes"] != float64(7) {
		t.Errorf("with meta: entry = %+v, want capability=workflow_ai bytes=7", entry)
	}

	withoutMeta := &core.Task{ID: "PX-2", ProjectCode: "PX", Title: "t"}
	data2, err := json.Marshal(taskToJSON(withoutMeta, nil))
	if err != nil {
		t.Fatalf("marshal without meta: %v", err)
	}
	var env2 map[string]any
	if err := json.Unmarshal(data2, &env2); err != nil {
		t.Fatalf("unmarshal without meta: %v", err)
	}
	if _, ok := env2["meta"]; ok {
		t.Errorf("without meta: `meta` field present (should be omitted):\n%s", data2)
	}
}
