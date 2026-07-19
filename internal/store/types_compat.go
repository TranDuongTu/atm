package store

// Type aliases re-exporting the domain, read-model, and board-expression
// types that moved to internal/core in refactor step 4 (ATM-b9d83a). The
// error sentinels and function shims that once lived alongside them were
// removed in step 6 (ATM-3b873c); store internals now name core.X directly.
// These pure type aliases remain as the package's public vocabulary: store's
// own signatures, its tests, and the few external callers that still spell a
// return type `store.Task` resolve through them without a churny qualifier
// sweep across every field, receiver, and composite literal.

import "atm/internal/core"

// Board-expression AST (renamed on the move: Node -> Expr).
type Node = core.Expr
type AtomNode = core.ExprAtom
type NotNode = core.ExprNot
type AndNode = core.ExprAnd
type OrNode = core.ExprOr

// Domain and read-model types.
type Task = core.Task
type Label = core.Label
type Comment = core.Comment
type Project = core.Project
type Persona = core.Persona
type LabelRemoveResult = core.LabelRemoveResult
type QueryFilters = core.QueryFilters
type LabelGroup = core.LabelGroup
type LogEntry = core.LogEntry
type Subject = core.Subject
type HistoryView = core.HistoryView
type Vocabulary = core.Vocabulary
type VocabularyTerm = core.VocabularyTerm
type EmbeddingConfig = core.EmbeddingConfig
type ProjectConfig = core.ProjectConfig
type AgentsConfig = core.AgentsConfig
type SearchParams = core.SearchParams
type Hit = core.Hit
type IndexResult = core.IndexResult
type EmbedFunc = core.EmbedFunc
type ProgressFunc = core.ProgressFunc
type VectorMeta = core.VectorMeta
