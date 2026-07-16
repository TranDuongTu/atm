package store

// Temporary re-exports of symbols that moved to internal/core in refactor
// step 4 (ATM-b9d83a). They keep store's internals and the CLI compiling
// unchanged while the adapters migrate; step 6 (ATM-3b873c) removes them.

import "atm/internal/core"

var (
	ErrNotFound  = core.ErrNotFound
	ErrConflict  = core.ErrConflict
	ErrIntegrity = core.ErrIntegrity
	ErrUsage     = core.ErrUsage
)

var (
	IsNotFound          = core.IsNotFound
	IsConflict          = core.IsConflict
	IsIntegrity         = core.IsIntegrity
	IsUsage             = core.IsUsage
	Now                 = core.Now
	RFC3339UTC          = core.RFC3339UTC
	TaskIDRe            = core.TaskIDRe
	ParseTaskID         = core.ParseTaskID
	CommentIDRe         = core.CommentIDRe
	ParseCommentID      = core.ParseCommentID
	ValidatePersonaName = core.ValidatePersonaName
	IsNamespaceName     = core.IsNamespaceName
)

// Board-expression AST (renamed on the move: Node -> Expr).
type Node = core.Expr
type AtomNode = core.ExprAtom
type NotNode = core.ExprNot
type AndNode = core.ExprAnd
type OrNode = core.ExprOr

var (
	ParseExpr = core.ParseExpr
	Atoms     = core.Atoms
)
