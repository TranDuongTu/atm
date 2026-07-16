package core

type QueryFilters struct {
	Project string
	// Labels AND-intersect; full label names. A name may be a board — a
	// computed label — in which case its expression is evaluated. Suffix
	// wildcards (e.g. "ATM:status:*", "ATM:*") declare facets and do NOT
	// restrict; see GroupTasks.
	Labels []string
	// Expr is an ad-hoc board expression (AND/OR/NOT/parens over bare label
	// names). Empty means no expression filter. ANDs with Labels.
	Expr string
}

type LabelGroup struct {
	Label string
	Tasks []*Task
}
