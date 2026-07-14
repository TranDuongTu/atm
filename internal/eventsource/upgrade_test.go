package eventsource

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// serializeUpgrade renders upgraded events one canonical line per event —
// the golden file format.
func serializeUpgrade(res *UpgradeResult) []byte {
	var buf bytes.Buffer
	for _, e := range res.Events {
		buf.Write(e.Raw)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func TestUpgradeV1Golden(t *testing.T) {
	res, err := UpgradeV1(readFixture(t, "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	got := serializeUpgrade(res)
	golden := filepath.Join("testdata", "v2-golden.jsonl")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("upgrade output differs from golden (run with -update after intentional changes)")
	}
}

func TestUpgradeV1IsPure(t *testing.T) {
	data := readFixture(t, "v1-log.jsonl")
	a, err := UpgradeV1(data)
	if err != nil {
		t.Fatal(err)
	}
	b, err := UpgradeV1(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Events) != len(b.Events) {
		t.Fatal("length differs across runs")
	}
	for i := range a.Events {
		if a.Events[i].ID != b.Events[i].ID {
			t.Fatalf("event %d id differs across runs — upgrade is not pure", i)
		}
		if !bytes.Equal(a.Events[i].Raw, b.Events[i].Raw) {
			t.Fatalf("event %d bytes differ across runs — upgrade is not pure", i)
		}
	}
}

func TestUpgradeV1Envelope(t *testing.T) {
	res, err := UpgradeV1(readFixture(t, "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	evs := res.Events
	if len(evs) != 12 {
		t.Fatalf("upgraded %d events, want 12", len(evs))
	}
	for i, e := range evs {
		if e.Replica != ReplicaV1 {
			t.Errorf("event %d replica = %q", i, e.Replica)
		}
		if e.HLC.L != int64(i+1) {
			t.Errorf("event %d hlc.l = %d, want v1 seq %d", i, e.HLC.L, i+1)
		}
		if e.HLC.P != e.At.UnixMilli() {
			t.Errorf("event %d hlc.p = %d, want at-in-ms %d", i, e.HLC.P, e.At.UnixMilli())
		}
		if i == 0 {
			if len(e.Parents) != 0 {
				t.Errorf("first event parents = %v", e.Parents)
			}
		} else if len(e.Parents) != 1 || e.Parents[0] != evs[i-1].ID {
			t.Errorf("event %d parents = %v, want [previous]", i, e.Parents)
		}
	}
}

func TestUpgradeV1IdentityAndAlias(t *testing.T) {
	res, err := UpgradeV1(readFixture(t, "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	evs := res.Events
	// Creation events: no subject.id, alias in payload.
	taskCreated := evs[2]
	if taskCreated.Action != ActionTaskCreated || taskCreated.Subject.ID != "" {
		t.Fatalf("task.created subject = %+v", taskCreated.Subject)
	}
	if alias, _ := taskCreated.PayloadString("alias"); alias != "ATM-0001" {
		t.Errorf("task alias = %q", alias)
	}
	if res.IdentityByAlias["ATM-0001"] != taskCreated.ID {
		t.Errorf("IdentityByAlias mismatch")
	}
	// Non-creation events: subject.id is the creation identity.
	retitle := evs[3]
	if retitle.Subject.ID != taskCreated.ID {
		t.Errorf("retitle subject.id = %q, want task identity", retitle.Subject.ID)
	}
	// Comment references become identities; v1 keys survive verbatim.
	reply := evs[6]
	if ref, _ := reply.PayloadString("task_ref"); ref != taskCreated.ID {
		t.Errorf("reply task_ref = %q", ref)
	}
	c1 := evs[4]
	if ref, _ := reply.PayloadString("reply_to_ref"); ref != c1.ID {
		t.Errorf("reply reply_to_ref = %q", ref)
	}
	if v1, _ := reply.PayloadString("task_id"); v1 != "ATM-0001" {
		t.Errorf("v1 task_id not preserved: %q", v1)
	}
}

func TestUpgradeV1MembershipDeltas(t *testing.T) {
	res, err := UpgradeV1(readFixture(t, "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	added := res.Events[8] // seq 9: labels gained ATM:status:done
	if got := added.PayloadStringOrList("label"); !slices.Equal(got, []string{"ATM:status:done"}) {
		t.Errorf("added delta = %v", got)
	}
	removed := res.Events[9] // seq 10: labels lost ATM:status:open
	if got := removed.PayloadStringOrList("label"); !slices.Equal(got, []string{"ATM:status:open"}) {
		t.Errorf("removed delta = %v", got)
	}
}

func TestUpgradeV1LabelUpsertMaterializesEmptyFields(t *testing.T) {
	res, err := UpgradeV1(readFixture(t, "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	up := res.Events[1] // seq 2: bare {"name": ...} upsert
	if d, ok := up.PayloadString("description"); !ok || d != "" {
		t.Errorf("description = %q, %v — want materialized empty", d, ok)
	}
	if x, ok := up.PayloadString("expr"); !ok || x != "" {
		t.Errorf("expr = %q, %v — want materialized empty", x, ok)
	}
}

// The retired task.meta-changed must ride through preserved and inert (D5):
// a full causal DAG node that writes no slot.
func TestUpgradeV1RetiredActionSurvivesInert(t *testing.T) {
	res, err := UpgradeV1(readFixture(t, "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	meta := res.Events[5] // seq 6
	if meta.Action != "task.meta-changed" {
		t.Fatalf("event 5 action = %q, want the retired task.meta-changed preserved", meta.Action)
	}
	if meta.Subject.ID != res.IdentityByAlias["ATM-0001"] {
		t.Errorf("retired event subject.id = %q, want the task identity", meta.Subject.ID)
	}
	if ws := writesOf(meta); len(ws) != 0 {
		t.Errorf("retired action wrote %d slots, want 0 (inert)", len(ws))
	}
	d, err := BuildDAG(res.Events)
	if err != nil {
		t.Fatal(err)
	}
	if d.Get(meta.ID) == nil {
		t.Error("retired event dropped from the DAG")
	}
}

func TestUpgradeV1FoldedStateMatchesV1Semantics(t *testing.T) {
	res, err := UpgradeV1(readFixture(t, "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := FoldEvents(res.Events)
	if err != nil {
		t.Fatal(err)
	}
	t1 := st.Tasks[res.IdentityByAlias["ATM-0001"]]
	if t1 == nil || t1.Title != "First task, retitled" || t1.Tombstoned {
		t.Fatalf("task 1 = %+v", t1)
	}
	if !slices.Equal(t1.Labels, []string{"ATM:status:done"}) {
		t.Errorf("task 1 labels = %v", t1.Labels)
	}
	t2 := st.Tasks[res.IdentityByAlias["ATM-0002"]]
	if t2 == nil || !t2.Tombstoned {
		t.Fatalf("task 2 should exist and be tombstoned: %+v", t2)
	}
	if len(st.CommentsByCreation(t1.ID)) != 2 {
		t.Errorf("comments = %d, want 2", len(st.CommentsByCreation(t1.ID)))
	}
	if len(st.Contested) != 0 {
		t.Errorf("a linear v1 history can never be contested: %+v", st.Contested)
	}
}

// TestUpgradeV1ProjectSubjectResolutionSucceeds exercises the SUCCESS side
// of the project-subject-resolution arm (upgrade.go's default case, kind
// "project"), which the golden fixture and the error table never reach: the
// error table only ever hits the dangling-project abort. Real v1 logs do
// emit project.name-changed and project.removed (internal/store/project.go),
// so this arm runs for real and needs its own coverage.
func TestUpgradeV1ProjectSubjectResolutionSucceeds(t *testing.T) {
	log := `{"seq":1,"at":"2026-07-01T10:00:00Z","actor":"a","action":"project.created","subject":{"kind":"project","code":"ATM"},"payload":{"code":"ATM","name":"Attention Task Manager"}}` + "\n" +
		`{"seq":2,"at":"2026-07-01T10:00:01Z","actor":"a","action":"project.name-changed","subject":{"kind":"project","code":"ATM"},"payload":{"name":"Renamed"}}` + "\n"
	res, err := UpgradeV1([]byte(log))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 2 {
		t.Fatalf("upgraded %d events, want 2", len(res.Events))
	}
	created, renamed := res.Events[0], res.Events[1]

	wantID := res.IdentityByAlias["ATM"]
	if wantID == "" {
		t.Fatal("IdentityByAlias[\"ATM\"] is empty, want the project.created identity")
	}
	if wantID != created.ID {
		t.Errorf("IdentityByAlias[\"ATM\"] = %q, want the project.created event id %q", wantID, created.ID)
	}
	if renamed.Subject.ID != wantID {
		t.Errorf("project.name-changed subject.id = %q, want resolved identity %q", renamed.Subject.ID, wantID)
	}
	if renamed.Subject.Code != "ATM" {
		t.Errorf("project.name-changed subject.code = %q, want %q", renamed.Subject.Code, "ATM")
	}
}

func TestUpgradeV1Errors(t *testing.T) {
	cases := map[string]string{
		"malformed line": "{not json}\n",
		"dangling alias": `{"seq":1,"at":"2026-07-01T10:00:00Z","actor":"a","action":"task.title-changed","subject":{"kind":"task","id":"ATM-0009"},"payload":{"title":"x"}}` + "\n",
		// A project event whose code has no project.created must abort too —
		// tolerating it would emit an event with an empty subject.id that the
		// fold then silently drops (spec decision 13).
		"dangling project":      `{"seq":1,"at":"2026-07-01T10:00:00Z","actor":"a","action":"project.name-changed","subject":{"kind":"project","code":"ZZZ"},"payload":{"name":"x"}}` + "\n",
		"duplicate creation":    `{"seq":1,"at":"2026-07-01T10:00:00Z","actor":"a","action":"task.created","subject":{"kind":"task","id":"ATM-0001"},"payload":{"id":"ATM-0001","title":"a","labels":[]}}` + "\n" + `{"seq":2,"at":"2026-07-01T10:00:01Z","actor":"a","action":"task.created","subject":{"kind":"task","id":"ATM-0001"},"payload":{"id":"ATM-0001","title":"b","labels":[]}}` + "\n",
		"non-monotonic seq":     `{"seq":2,"at":"2026-07-01T10:00:00Z","actor":"a","action":"project.created","subject":{"kind":"project","code":"ATM"},"payload":{"code":"ATM","name":"x"}}` + "\n" + `{"seq":2,"at":"2026-07-01T10:00:01Z","actor":"a","action":"project.name-changed","subject":{"kind":"project","code":"ATM"},"payload":{"name":"y"}}` + "\n",
		"dangling comment task": `{"seq":1,"at":"2026-07-01T10:00:00Z","actor":"a","action":"comment.created","subject":{"kind":"comment","id":"ATM-0009-c0001"},"payload":{"id":"ATM-0009-c0001","task_id":"ATM-0009","body":"x","labels":[]}}` + "\n",
	}
	for name, log := range cases {
		if _, err := UpgradeV1([]byte(log)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
