package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"atm/internal/eventsource"
)

// UpgradeReport is the per-project outcome of an upgrade to v2 media.
// AlreadyV2 marks a project `upgrade --all` SKIPPED because its effective
// format was already v2 (nothing on disk was touched for it).
type UpgradeReport struct {
	Project      string      `json:"project"`
	Format       StoreFormat `json:"format"`
	Events       int         `json:"events"`
	ArchivedPath string      `json:"archived_path,omitempty"`
	AlreadyV2    bool        `json:"already_v2,omitempty"`
}

type RollbackReport struct {
	Project string      `json:"project"`
	Format  StoreFormat `json:"format"`
}

// UpgradeProjectToV2 converts a v1-active project's frozen log.jsonl into
// events.v2.jsonl and cuts the project over to v2 media. log.jsonl is never
// written by this path: it stays byte-identical, and is what a rollback (and
// a later re-upgrade) reads back.
//
// The ordering below is load-bearing: write a TEMP candidate, verify it,
// semantically compare it to the v1 replay, and only then displace any prior
// v2 file and rename into place. Every failure before step 4 leaves the v1
// log and any existing events.v2.jsonl exactly as they were.
func (s *Store) UpgradeProjectToV2(code string) (*UpgradeReport, error) {
	// GUARD (spec L3-5): upgrade reads FROM the frozen v1 log, so it is
	// legal only while the project's EFFECTIVE format is v1 — a fresh
	// upgrade or a post-rollback re-upgrade. Running it against an
	// effective-v2 project would rebuild from stale v1 bytes and archive
	// the LIVE events.v2.jsonl as events.v2.reupgrade.*, silently
	// discarding every post-cutover write (archives are manual-recovery
	// evidence, never auto-merged); against a v2-BORN project it would
	// hard-fail on the missing log.jsonl.
	if f, err := s.projectFormat(code); err != nil {
		return nil, err
	} else if f == StoreFormatV2 {
		return nil, fmt.Errorf("%w: project %q is already v2-active; upgrade reads from the v1 log and is only legal for v1-active projects (roll back first to rebuild from v1)", ErrConflict, code)
	}
	rep := &UpgradeReport{Project: code, Format: StoreFormatV2}
	err := s.WithLock(code, func() error {
		// Re-check under the lock: a concurrent upgrade of the same project
		// may have cut it over between the guard above and here, and the
		// stale v1 bytes we would rebuild from would archive its live file.
		if f, err := s.projectFormat(code); err != nil {
			return err
		} else if f == StoreFormatV2 {
			return fmt.Errorf("%w: project %q is already v2-active; upgrade reads from the v1 log and is only legal for v1-active projects (roll back first to rebuild from v1)", ErrConflict, code)
		}
		raw, err := os.ReadFile(s.logPath(code))
		if err != nil {
			return err
		}
		up, err := eventsource.UpgradeV1(raw)
		if err != nil {
			return err
		}

		// 1. Write the candidate file. Nothing existing is touched yet.
		tmp := s.eventsV2Path(code) + ".tmp"
		if err := os.MkdirAll(filepath.Dir(tmp), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		for _, ev := range up.Events {
			if _, err := f.Write(ev.Raw); err != nil {
				_ = f.Close()
				_ = os.Remove(tmp)
				return err
			}
			if _, err := f.Write([]byte("\n")); err != nil {
				_ = f.Close()
				_ = os.Remove(tmp)
				return err
			}
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(tmp)
			return err
		}

		// 2. Verify the candidate BEFORE it becomes events.v2.jsonl (L3-3):
		// re-read it, recompute every id, validate parents, build the DAG,
		// and fold.
		snap, err := s.readV2FileAt(tmp, false)
		if err != nil {
			_ = os.Remove(tmp)
			return err
		}
		state, err := eventsource.FoldEvents(snap.Events)
		if err != nil {
			_ = os.Remove(tmp)
			return err
		}

		// 3. Semantic comparison against the current v1 replay (spec upgrade
		// step 6). The package-level equivalence test guards the code path;
		// this guards the user's actual data at cutover time.
		if err := s.compareV2FoldToV1Replay(code, state); err != nil {
			_ = os.Remove(tmp)
			return err
		}

		// 4. Only now displace the previous v2 file (re-upgrade) and cut over.
		// A failed upgrade must leave both the v1 log and any prior v2 file
		// exactly as they were.
		if _, err := os.Stat(s.eventsV2Path(code)); err == nil {
			archived, err := s.archiveV2FileLocked(code, "reupgrade")
			if err != nil {
				return err
			}
			rep.ArchivedPath = archived
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(tmp, s.eventsV2Path(code)); err != nil {
			return err
		}
		if err := s.cacheProjectFromV2State(code, state, snap.EventCount); err != nil {
			return err
		}
		if err := s.setProjectFormat(code, StoreFormatV2); err != nil {
			return err
		}
		// 5. Wipe the vector indexes (spec L3-15). Their entries are keyed
		// by the v1 log seq, which is meaningless under v2 and would poison
		// dedupVectorsByID and staleness checks; the indexer re-embeds from
		// the v2 fold by text hash (Task 9b).
		_ = os.RemoveAll(s.vectorsDir(code))
		// 6. Drop any memoized v1 log snapshot: a long-lived process (the
		// TUI) must not keep serving pre-cutover ReadLogCached entries
		// across the format switch.
		s.invalidateLogSnapshot(code)
		rep.Events = snap.EventCount
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rep, nil
}

// compareV2FoldToV1Replay fails the upgrade when the v2 fold and the v1
// replay disagree on any semantic field ATM exposes today, keyed by alias:
// the project's name; the live task set with title, description, and sorted
// labels per alias; the live comment set with body, sorted labels, task
// alias, and reply-to alias per alias; and each label's name, description,
// and expr.
//
// Membership of COMPUTED labels (boards, which carry an expr, and namespace
// labels) is excluded from the task/comment label comparison on BOTH sides:
// under L2-6 such membership is derived, never asserted, so the v2 fold
// drops an asserted board label from an entity's list while v1's Replay
// still lists it. That is the one intentional, documented v1↔v2 divergence
// (see internal/eventsource/equivalence_test.go); the raw v1 assertion is
// still preserved verbatim in the upgraded event, so nothing is lost. Every
// other difference aborts the upgrade before anything on disk moves.
func (s *Store) compareV2FoldToV1Replay(code string, st *eventsource.State) error {
	replay, err := s.Replay(code)
	if err != nil {
		return err
	}

	// --- Project.
	var v2proj *eventsource.ProjectState
	for _, p := range st.Projects {
		if p.Code == code && !p.Tombstoned {
			v2proj = p
			break
		}
	}
	if (replay.Project == nil) != (v2proj == nil) {
		return fmt.Errorf("%w: upgrade of %q: project existence differs (v1 present=%v, v2 present=%v)",
			ErrIntegrity, code, replay.Project != nil, v2proj != nil)
	}
	if replay.Project != nil && replay.Project.Name != v2proj.Name {
		return fmt.Errorf("%w: upgrade of %q: project name: v2 %q, v1 %q", ErrIntegrity, code, v2proj.Name, replay.Project.Name)
	}

	// --- Labels (name → description, expr), live set on both sides.
	v1Labels := map[string]Label{}
	for _, l := range replay.Labels {
		v1Labels[l.Name] = l
	}
	v2Labels := map[string]*eventsource.LabelState{}
	for name, l := range st.Labels {
		if !l.Tombstoned {
			v2Labels[name] = l
		}
	}
	for name, want := range v1Labels {
		got, ok := v2Labels[name]
		if !ok {
			return fmt.Errorf("%w: upgrade of %q: label %q is live in v1 but missing from the v2 fold", ErrIntegrity, code, name)
		}
		if got.Description != want.Description {
			return fmt.Errorf("%w: upgrade of %q: label %q description: v2 %q, v1 %q", ErrIntegrity, code, name, got.Description, want.Description)
		}
		if got.Expr != want.Expr {
			return fmt.Errorf("%w: upgrade of %q: label %q expr: v2 %q, v1 %q", ErrIntegrity, code, name, got.Expr, want.Expr)
		}
	}
	for name := range v2Labels {
		if _, ok := v1Labels[name]; !ok {
			return fmt.Errorf("%w: upgrade of %q: label %q is live in the v2 fold but absent from v1", ErrIntegrity, code, name)
		}
	}

	// computed reports whether membership in name is derived (L2-6) and so
	// is not comparable between the two sides. Checked against both sides'
	// label records so a label defined on only one side still counts.
	computed := func(name string) bool {
		if IsNamespaceName(name) {
			return true
		}
		if l, ok := v1Labels[name]; ok && l.Expr != "" {
			return true
		}
		if l, ok := v2Labels[name]; ok && l.Expr != "" {
			return true
		}
		return false
	}
	assertedLabels := func(in []string) []string {
		out := make([]string, 0, len(in))
		for _, name := range in {
			if !computed(name) {
				out = append(out, name)
			}
		}
		sort.Strings(out)
		return out
	}

	// --- Tasks, keyed by alias, live only.
	v2Tasks := map[string]*eventsource.TaskState{}
	for _, tk := range st.Tasks {
		if !tk.Tombstoned {
			v2Tasks[tk.Alias] = tk
		}
	}
	if len(v2Tasks) != len(replay.Tasks) {
		return fmt.Errorf("%w: upgrade of %q: live tasks: v2 %d, v1 %d", ErrIntegrity, code, len(v2Tasks), len(replay.Tasks))
	}
	for _, want := range replay.Tasks {
		got, ok := v2Tasks[want.ID]
		if !ok {
			return fmt.Errorf("%w: upgrade of %q: task %s is live in v1 but missing from the v2 fold", ErrIntegrity, code, want.ID)
		}
		if got.Title != want.Title {
			return fmt.Errorf("%w: upgrade of %q: task %s title: v2 %q, v1 %q", ErrIntegrity, code, want.ID, got.Title, want.Title)
		}
		if got.Description != want.Description {
			return fmt.Errorf("%w: upgrade of %q: task %s description: v2 %q, v1 %q", ErrIntegrity, code, want.ID, got.Description, want.Description)
		}
		gotLabels, wantLabels := assertedLabels(got.Labels), assertedLabels(want.Labels)
		if !equalStrings(gotLabels, wantLabels) {
			return fmt.Errorf("%w: upgrade of %q: task %s labels: v2 %v, v1 %v", ErrIntegrity, code, want.ID, gotLabels, wantLabels)
		}
	}

	// --- Comments, keyed by alias, live only. Cross-entity references are
	// identities in the fold; map them back to aliases as the projector does.
	v2Comments := map[string]*eventsource.CommentState{}
	for _, cm := range st.Comments {
		if !cm.Tombstoned {
			v2Comments[cm.Alias] = cm
		}
	}
	taskAliasOf := func(id string) string {
		if tk, ok := st.Tasks[id]; ok {
			return tk.Alias
		}
		return ""
	}
	commentAliasOf := func(id string) string {
		if cm, ok := st.Comments[id]; ok {
			return cm.Alias
		}
		return ""
	}
	if len(v2Comments) != len(replay.Comments) {
		return fmt.Errorf("%w: upgrade of %q: live comments: v2 %d, v1 %d", ErrIntegrity, code, len(v2Comments), len(replay.Comments))
	}
	for _, want := range replay.Comments {
		got, ok := v2Comments[want.ID]
		if !ok {
			return fmt.Errorf("%w: upgrade of %q: comment %s is live in v1 but missing from the v2 fold", ErrIntegrity, code, want.ID)
		}
		if got.Body != want.Body {
			return fmt.Errorf("%w: upgrade of %q: comment %s body: v2 %q, v1 %q", ErrIntegrity, code, want.ID, got.Body, want.Body)
		}
		if a := taskAliasOf(got.TaskRef); a != want.TaskID {
			return fmt.Errorf("%w: upgrade of %q: comment %s task: v2 %q, v1 %q", ErrIntegrity, code, want.ID, a, want.TaskID)
		}
		gotReply := ""
		if got.ReplyToRef != "" {
			gotReply = commentAliasOf(got.ReplyToRef)
		}
		if gotReply != want.ReplyTo {
			return fmt.Errorf("%w: upgrade of %q: comment %s reply-to: v2 %q, v1 %q", ErrIntegrity, code, want.ID, gotReply, want.ReplyTo)
		}
		gotLabels, wantLabels := assertedLabels(got.Labels), assertedLabels(want.Labels)
		if !equalStrings(gotLabels, wantLabels) {
			return fmt.Errorf("%w: upgrade of %q: comment %s labels: v2 %v, v1 %v", ErrIntegrity, code, want.ID, gotLabels, wantLabels)
		}
	}
	return nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// UpgradeAllToV2 upgrades every v1-active project on disk and then flips the
// store default so NEW projects are born v2. A failure on any project returns
// the reports collected so far WITHOUT flipping: the already-cut-over
// projects stay v2 (their media is complete), and a retry resumes.
func (s *Store) UpgradeAllToV2() ([]UpgradeReport, error) {
	codes, err := s.projectCodesOnDisk()
	if err != nil {
		return nil, err
	}
	out := make([]UpgradeReport, 0, len(codes))
	for _, code := range codes {
		// SKIP effective-v2 projects instead of letting the per-project
		// guard error: a RETRY of a partially-failed `upgrade --all` (A cut
		// over, B failed, no flip) must neither re-upgrade A from its frozen
		// v1 log nor hard-fail on a v2-born project's missing log.jsonl
		// after damaging earlier projects. Skipped projects count as
		// already-upgraded for the ActiveFormat flip decision below.
		if f, err := s.projectFormat(code); err != nil {
			return out, err
		} else if f == StoreFormatV2 {
			out = append(out, UpgradeReport{Project: code, Format: StoreFormatV2, AlreadyV2: true})
			continue
		}
		rep, err := s.UpgradeProjectToV2(code)
		if err != nil {
			return out, err
		}
		out = append(out, *rep)
	}
	// Every project now holds an explicit ProjectFormats entry, so flipping
	// the store default cannot change how any existing project is read — it
	// only makes NEW projects be born v2 (spec L3-14). A partial failure
	// returns above without flipping. SetActiveFormat re-checks the
	// explicit-entry invariant, which is trivially satisfied here.
	if err := s.SetActiveFormat(StoreFormatV2); err != nil {
		return out, err
	}
	return out, nil
}

// RollbackProjectToV1 switches the project format AND rebuilds the project's
// cache rows from the v1 replay. The cache still holds v2-derived rows whose
// LogSeq ordinals mean nothing to the v1 freshness checks (`cache LogSeq >
// log LastSeq` → ErrIntegrity) and whose NextTaskN is unset; leaving them in
// place would break v1 reads and writes immediately after rollback. The
// vector indexes are wiped for the mirror-image reason (v2 creation
// ordinals poison v1 dedup/staleness; spec L3-15). Rollback writes the
// explicit per-project entry and NEVER touches StoreMeta.ActiveFormat: new
// projects keep being born in whatever format the store default names, and
// `atm store set-format --format v1` is the operator surface for changing
// that (Task 6).
//
// events.v2.jsonl is left in place (a later re-upgrade archives it) and the
// v1 log is never written: rollback does not export v2-only writes back into
// v1, so any post-cutover v2 write survives only in that file.
func (s *Store) RollbackProjectToV1(code string) (*RollbackReport, error) {
	rep := &RollbackReport{Project: code, Format: StoreFormatV1}
	err := s.WithLock(code, func() error {
		// GUARD: rollback replays the v1 log, and ReadLog returns (nil, nil)
		// for a missing file. Rolling back a project with no log.jsonl (a
		// v2-BORN project) would flip the format, wipe the vectors, replay
		// an EMPTY log — cache rows deleted, nothing reinserted — and leave
		// a zombie that is neither readable as v1 (no media) nor recreatable
		// (the Task 8 existence check still sees events.v2.jsonl). Refuse.
		if _, err := os.Stat(s.logPath(code)); os.IsNotExist(err) {
			return fmt.Errorf("%w: project %q has no v1 log to roll back to (born v2); rollback is only legal for upgraded projects", ErrConflict, code)
		} else if err != nil {
			return err
		}
		// Rebuild the cache from the v1 replay BEFORE flipping the format:
		// a failure here leaves the project fully v2 (format entry, cache
		// rows, media all agree) instead of stranding it as v1-declared with
		// v2-derived rows.
		if err := s.rebuildProjectCacheFromV1Locked(code); err != nil {
			return err
		}
		if err := s.setProjectFormat(code, StoreFormatV1); err != nil {
			return err
		}
		_ = os.RemoveAll(s.vectorsDir(code))
		// Same snapshot rule as upgrade: never serve a stale ReadLogCached
		// snapshot across a format switch.
		s.invalidateLogSnapshot(code)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rep, nil
}

// rebuildProjectCacheFromV1Locked mirrors the per-project body of Rebuild:
// sweep the project's cache rows, then replay its v1 log and re-insert the
// live set (project row, tasks, comments, labels).
func (s *Store) rebuildProjectCacheFromV1Locked(code string) error {
	st, err := s.Replay(code)
	if err != nil && !IsIntegrity(err) {
		return err
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	if err := cacheDeleteProjectRows(db, code); err != nil {
		return err
	}
	if st.Project != nil {
		if err := cacheUpsertProject(db, st.Project); err != nil {
			return err
		}
	}
	for _, t := range st.Tasks {
		if err := cacheUpsertTask(db, t); err != nil {
			return err
		}
	}
	for _, c := range st.Comments {
		if err := cacheUpsertComment(db, c); err != nil {
			return err
		}
	}
	for _, l := range st.Labels {
		if err := cacheUpsertLabel(db, l); err != nil {
			return err
		}
	}
	return nil
}
