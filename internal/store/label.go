package store

import (
	"errors"
	"fmt"
	"strings"

	"atm/internal/core"
	"atm/internal/seed"
)

var (
	// ErrComputedLabelOnTask: a task may only carry stored labels. A computed
	// label (a board, or a ":*" namespace) is derived, so asserting it on a
	// task would make the label mean two things at once - see conventions
	// rule 5, "one label, one meaning".
	ErrComputedLabelOnTask = errors.New("computed labels cannot be assigned to a task")
	// ErrBoardNameCollision: ATM:status and ATM:status:* are distinct strings
	// but both render as "status" in the Boards pane.
	ErrBoardNameCollision = errors.New("board name collides with a namespace name")
)

// LabelAdd is the explicit "force upsert" path for a label: it always
// appends a label.upserted event to the project's log, then write-throughs
// the cache row. If `description` is empty, the existing description on the
// live row (if any) is preserved; a non-empty description overwrites it.
// Contrast with LabelSeed, which is a no-op when the label already exists.
func (s *Store) LabelAdd(name, description, expr, actor string) error {
	if err := ValidateLabelName(name); err != nil {
		return err
	}
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if err := s.labelProjectExists(name); err != nil {
		return err
	}
	if expr != "" {
		if err := s.validateExpr(name, expr); err != nil {
			return err
		}
	}
	code := labelProject(name)
	if _, err := s.eng.DispatchFormat(code); err != nil {
		return err
	}
	// Only the fields being SET are asserted (the writesOf action table): a nil
	// field writes no slot, so the label's existing description/expr survives —
	// exactly the v1 "empty means keep" rule, expressed in the event model
	// instead of by re-reading the cache.
	f := core.LabelFields{}
	if description != "" {
		f.Description = &description
	}
	if expr != "" {
		f.Expr = &expr
	}
	return s.labelUpsertV2(code, name, actor, f)
}

// validateExpr parses expr, rejects a name collision, and walks the board
// reference graph to reject cycles. Called on the write path. It is NOT the
// only cycle defence: a merge can synthesize a cycle no replica wrote, which
// is why resolve.go carries a visited-set guard too. See ATM-0105-c0004.
func (s *Store) validateExpr(name, expr string) error {
	code := labelProject(name)

	// I3 - a board may not shadow a namespace.
	for _, l := range s.LabelList(code, "") {
		if core.IsNamespaceName(l.Name) && strings.TrimSuffix(l.Name, ":*") == name {
			return fmt.Errorf("%w: %s vs %s", ErrBoardNameCollision, name, l.Name)
		}
	}

	n, err := core.ParseExpr(expr)
	if err != nil {
		return fmt.Errorf("%w: %v", core.ErrUsage, err)
	}

	// I2 (write half) - walk references depth-first from this label.
	live := map[string]Label{}
	for _, l := range s.LabelList(code, "") {
		live[l.Name] = l
	}
	live[name] = Label{Name: name, Expr: expr} // the label as it WOULD be

	visiting := map[string]bool{}
	var walk func(full string, node Node) error
	walk = func(full string, node Node) error {
		for _, atom := range core.Atoms(node) {
			ref := code + ":" + atom
			l, ok := live[ref]
			if !ok || l.Expr == "" {
				continue // stored label or namespace - a leaf, cannot cycle
			}
			if visiting[ref] {
				return fmt.Errorf("%w: %s", ErrCyclicExpr, ref)
			}
			visiting[ref] = true
			sub, err := core.ParseExpr(l.Expr)
			if err != nil {
				return fmt.Errorf("board %s: %w", ref, err)
			}
			if err := walk(ref, sub); err != nil {
				return err
			}
			delete(visiting, ref)
		}
		return nil
	}
	visiting[name] = true
	return walk(name, n)
}

// LabelSeed upserts a label but only sets the description when the label is
// newly created. Existing labels keep their descriptions.
func (s *Store) LabelSeed(name, description, expr, actor string) error {
	if err := ValidateLabelName(name); err != nil {
		return err
	}
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if err := s.labelProjectExists(name); err != nil {
		return err
	}
	code := labelProject(name)
	if _, err := s.eng.DispatchFormat(code); err != nil {
		return err
	}
	return s.labelSeedV2(code, name, description, expr, actor)
}

// SeedLabels applies the default seed labels (internal/seed.Labels) to the
// project. Idempotent.
func (s *Store) SeedLabels(code, actor string) error {
	for _, l := range seed.Labels {
		full := code + ":" + l.Suffix
		if err := s.LabelSeed(full, l.Description, l.Expr, actor); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LabelRemove(name, actor string) (*LabelRemoveResult, error) {
	if err := ValidateLabelName(name); err != nil {
		return nil, err
	}
	if err := s.validateActor(actor); err != nil {
		return nil, err
	}
	code := labelProject(name)
	if _, err := s.eng.DispatchFormat(code); err != nil {
		return nil, err
	}
	return s.labelRemoveV2(code, name, actor)
}

// ---- v2 label mutators ----

// labelUpsertV2 emits label.upserted through the engine's write transaction.
// A label's NAME is its identity in the fold, so there is nothing to resolve.
func (s *Store) labelUpsertV2(code, name, actor string, f core.LabelFields) error {
	return s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		if err := cs.UpsertLabel(name, f, actor); err != nil {
			return err
		}
		return s.reprojectTxn(code, cs)
	})
}

// labelSeedV2 is LabelSeed's v2 body. cs.SeedLabel is itself the no-op guard:
// it folds under the lock and appends nothing when the label is already live,
// so it carries the exact begin/observe sequence (and therefore the exact HLC
// trajectory) the pre-carve labelSeedV2 had. When it appends nothing the txn
// stays clean and reprojectTxn skips entirely — the pre-carve early-return
// semantics (ATM-d402aa restored them after the carve briefly reprojected
// unconditionally, freezing the TUI on every project select).
func (s *Store) labelSeedV2(code, name, description, expr, actor string) error {
	return s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		if err := cs.SeedLabel(name, description, expr, actor); err != nil {
			return err
		}
		return s.reprojectTxn(code, cs)
	})
}

func (s *Store) labelRemoveV2(code, name, actor string) (*LabelRemoveResult, error) {
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var result *LabelRemoveResult
	err = s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		if err := cs.RemoveLabel(name, actor); err != nil {
			return err
		}
		if err := s.reprojectTxn(code, cs); err != nil {
			return err
		}
		// Retained usage: entities still carrying the (now unregistered) name.
		count, err := cacheCountTasksWithLabelGlobally(db, name)
		if err != nil {
			return err
		}
		result = &LabelRemoveResult{RetainedUsage: count}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// labelProjectExistsV2Locked validates a label's project from inside a v2
// mutator that already holds `code`'s lock. For a label in the project being
// mutated the fold has already proved the project live, so there is nothing to
// check; a label naming a FOREIGN project falls back to the cache row rather
// than getProjectLocked, whose v1 freshness checks are meaningless for a
// v2-active project (Task 9 branches those).
func (s *Store) labelProjectExistsV2Locked(name, code string) error {
	lc := labelProject(name)
	if lc == code {
		return nil
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	if _, ok, err := cacheGetProject(db, lc); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: project %q for label %q does not exist", core.ErrUsage, lc, name)
	}
	return nil
}

func (s *Store) LabelList(project, namespace string) []Label {
	db, err := s.cacheDB()
	if err != nil {
		return nil
	}
	out, err := cacheListLabels(db, project, namespace)
	if err != nil {
		return nil
	}
	return out
}

func (s *Store) LabelShow(name string) (Label, error) {
	db, err := s.cacheDB()
	if err != nil {
		return Label{}, err
	}
	l, ok, err := cacheGetLabel(db, name)
	if err != nil {
		return Label{}, err
	}
	if !ok {
		return Label{}, fmt.Errorf("%w: label %q", core.ErrNotFound, name)
	}
	return l, nil
}

func (s *Store) Namespaces(code string) []string {
	db, err := s.cacheDB()
	if err != nil {
		return nil
	}
	ns, err := cacheNamespaces(db, code)
	if err != nil {
		return nil
	}
	return ns
}

func (s *Store) labelProjectExists(name string) error {
	code := labelProject(name)
	if _, err := s.GetProject(code); err != nil {
		return fmt.Errorf("%w: project %q for label %q does not exist", core.ErrUsage, code, name)
	}
	return nil
}

// labelProjectExistsLocked is identical to labelProjectExists except that it
// calls getProjectLocked instead of GetProject. It exists ONLY for callers
// that already hold the label's project lock (i.e. are running inside their
// own s.WithLock(code, ...) closure) — calling labelProjectExists in that
// situation would re-enter the (non-reentrant) mutex via GetProject and
// deadlock. Used by CreateTask, which validates supplied labels from inside
// its own WithLock closure.
func (s *Store) labelProjectExistsLocked(name string) error {
	code := labelProject(name)
	if _, err := s.getProjectLocked(code); err != nil {
		return fmt.Errorf("%w: project %q for label %q does not exist", core.ErrUsage, code, name)
	}
	return nil
}

func labelProject(name string) string {
	return strings.SplitN(name, ":", 2)[0]
}

// LabelUsage counts entities in the given project carrying the label —
// tasks plus comments. Exported for the TUI's Labels pane "(N entities)"
// suffix and the CLI's retained_usage report. A label like
// <CODE>:comment:open-question can have zero tasks but many comments, so
// counting only tasks understated real adoption. Backed by two indexed
// COUNT queries — see docs/superpowers/specs/2026-07-06-atm-storage-sync-design.md
// and ATM-0027-c0003.
func (s *Store) LabelUsage(projectCode, label string) (int, error) {
	db, err := s.cacheDB()
	if err != nil {
		return 0, err
	}
	return cacheLabelUsage(db, projectCode, label)
}

// LabelUsageGrouped returns a map of label -> usage count (tasks + comments)
// for every label in projectCode in two grouped queries instead of 2N
// queries for N labels. Used by the TUI Labels pane refresh path.
func (s *Store) LabelUsageGrouped(projectCode string) (map[string]int, error) {
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	return cacheLabelUsageGrouped(db, projectCode)
}
