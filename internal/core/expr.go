package core

import (
	"fmt"
	"sort"
	"strings"
)

// Expr is one node of a parsed board expression. The grammar, lowest
// precedence first:
//
//	or   := and ("OR" and)*
//	and  := not ("AND" not)*
//	not  := "NOT" not | atom
//	atom := NAME | "(" or ")"
//
// NAME is a label name with the project prefix omitted: a stored label
// ("status:open"), a namespace predicate ("status:*"), or a board
// reference ("next-sprint"). Which one it is, is decided at resolve time
// by looking the name up — not by its syntax. See resolve.go.
type Expr interface{ isExpr() }

type ExprAtom struct{ Name string }
type ExprNot struct{ X Expr }
type ExprAnd struct{ L, R Expr }
type ExprOr struct{ L, R Expr }

func (*ExprAtom) isExpr() {}
func (*ExprNot) isExpr()  {}
func (*ExprAnd) isExpr()  {}
func (*ExprOr) isExpr()   {}

// ParseExpr parses a board expression. Operators are case-sensitive
// (AND/OR/NOT) so they cannot collide with label names, which the label
// grammar constrains to lowercase.
func ParseExpr(src string) (Expr, error) {
	toks, err := lexExpr(src)
	if err != nil {
		return nil, err
	}
	p := &exprParser{toks: toks}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if !p.done() {
		return nil, fmt.Errorf("unexpected %q after expression", p.peek())
	}
	return n, nil
}

// Atoms returns every atom name in the tree, deduped and sorted.
func Atoms(n Expr) []string {
	seen := map[string]bool{}
	var walk func(Expr)
	walk = func(n Expr) {
		switch t := n.(type) {
		case *ExprAtom:
			seen[t.Name] = true
		case *ExprNot:
			walk(t.X)
		case *ExprAnd:
			walk(t.L)
			walk(t.R)
		case *ExprOr:
			walk(t.L)
			walk(t.R)
		}
	}
	walk(n)
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

func lexExpr(src string) ([]string, error) {
	var toks []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range src {
		switch {
		case r == '(' || r == ')':
			flush()
			toks = append(toks, string(r))
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	if len(toks) == 0 {
		return nil, fmt.Errorf("empty expression")
	}
	return toks, nil
}

type exprParser struct {
	toks []string
	i    int
}

func (p *exprParser) done() bool { return p.i >= len(p.toks) }
func (p *exprParser) peek() string {
	if p.done() {
		return ""
	}
	return p.toks[p.i]
}
func (p *exprParser) next() string {
	t := p.peek()
	p.i++
	return t
}

func (p *exprParser) parseOr() (Expr, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek() == "OR" {
		p.next()
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = &ExprOr{L: l, R: r}
	}
	return l, nil
}

func (p *exprParser) parseAnd() (Expr, error) {
	l, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek() == "AND" {
		p.next()
		r, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		l = &ExprAnd{L: l, R: r}
	}
	return l, nil
}

func (p *exprParser) parseNot() (Expr, error) {
	if p.peek() == "NOT" {
		p.next()
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &ExprNot{X: x}, nil
	}
	return p.parseAtom()
}

func (p *exprParser) parseAtom() (Expr, error) {
	if p.done() {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	t := p.next()
	if t == "(" {
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek() != ")" {
			return nil, fmt.Errorf("missing closing paren")
		}
		p.next()
		return n, nil
	}
	if t == ")" || t == "AND" || t == "OR" || t == "NOT" {
		return nil, fmt.Errorf("unexpected %q", t)
	}
	return &ExprAtom{Name: t}, nil
}
