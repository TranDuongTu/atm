package store

import (
	"atm/internal/core"
	"errors"
	"fmt"
	"strings"
)

// ErrCyclicExpr is returned when board references form a cycle. Write-time
// validation rejects cycles (see LabelAdd), but a MERGE can synthesize one
// that no replica ever wrote — replica A points board a at b while replica B
// points b at a, both writes individually valid. See ATM-0105-c0004 and
// docs/eventsource/00-architecture.md D4. The guard below is what keeps that
// case from recursing forever.
var ErrCyclicExpr = errors.New("cyclic board expression")

// resolver evaluates board expressions against tasks for one project. It is
// built once per query and holds a memo, so a board referenced by several
// other boards is parsed once.
type resolver struct {
	code   string
	labels map[string]Label // full name -> label
	parsed map[string]Node  // full name -> parsed Expr (memo)
}

func newResolver(code string, labels []Label) *resolver {
	m := make(map[string]Label, len(labels))
	for _, l := range labels {
		m[l.Name] = l
	}
	return &resolver{code: code, labels: m, parsed: map[string]Node{}}
}

// qualify turns a bare atom name ("status:open") into a full label name
// ("ATM:status:open"). core.Atoms in an expression omit the project prefix.
func (r *resolver) qualify(atom string) string { return r.code + ":" + atom }

// Matches reports whether t satisfies n.
func (r *resolver) Matches(t *Task, n Node) (bool, error) {
	return r.eval(t, n, map[string]bool{})
}

func (r *resolver) eval(t *Task, n Node, visiting map[string]bool) (bool, error) {
	switch node := n.(type) {
	case *NotNode:
		v, err := r.eval(t, node.X, visiting)
		return !v, err
	case *AndNode:
		l, err := r.eval(t, node.L, visiting)
		if err != nil || !l {
			return false, err
		}
		return r.eval(t, node.R, visiting)
	case *OrNode:
		l, err := r.eval(t, node.L, visiting)
		if err != nil || l {
			return l, err
		}
		return r.eval(t, node.R, visiting)
	case *AtomNode:
		return r.evalAtom(t, node.Name, visiting)
	}
	return false, fmt.Errorf("unknown expression node %T", n)
}

// evalAtom resolves an atom BY LOOKUP, not by syntax. That is what lets a
// bare-tag stored label ("stale") and a board ("next-sprint") share one name
// space unambiguously: whichever the live label record says it is, it is.
func (r *resolver) evalAtom(t *Task, atom string, visiting map[string]bool) (bool, error) {
	// The bare '*' tautology atom: matches every task, including unlabeled
	// ones. MUST short-circuit before qualify — qualify("*") yields
	// "<CODE>:*", which core.IsNamespaceName reads as the namespace predicate
	// "has any label" and so misses naked unlabeled jottings. See
	// docs/superpowers/specs/2026-07-17-all-tasks-board-design.md.
	if atom == "*" {
		return true, nil
	}
	full := r.qualify(atom)

	// Namespace predicate: task carries ANY label in the namespace.
	if core.IsNamespaceName(full) {
		prefix := strings.TrimSuffix(full, "*")
		for _, l := range t.Labels {
			if strings.HasPrefix(l, prefix) {
				return true, nil
			}
		}
		return false, nil
	}

	// Board: recurse into its expression, guarding against cycles.
	if l, ok := r.labels[full]; ok && l.Expr != "" {
		if visiting[full] {
			return false, fmt.Errorf("%w: %s", ErrCyclicExpr, full)
		}
		visiting[full] = true
		defer delete(visiting, full)

		n, ok := r.parsed[full]
		if !ok {
			var err error
			if n, err = core.ParseExpr(l.Expr); err != nil {
				return false, fmt.Errorf("board %s: %w", full, err)
			}
			r.parsed[full] = n
		}
		return r.eval(t, n, visiting)
	}

	// Stored label (or an atom naming no live label, which is simply absent).
	for _, l := range t.Labels {
		if l == full {
			return true, nil
		}
	}
	return false, nil
}
