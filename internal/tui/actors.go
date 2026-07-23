package tui

// kvRow is a sortable (key, count) pair used for the agents/models/actions
// breakdown rows in the persona detail view (renderPersonaDetailChart).
type kvRow struct {
	k string
	v int
}

// sortKV sorts rows by count desc, then key asc (deterministic display order).
func sortKV(rows []kvRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0; j-- {
			x, y := rows[j-1], rows[j]
			if y.v > x.v || (y.v == x.v && y.k < x.k) {
				rows[j-1], rows[j] = rows[j], rows[j-1]
			} else {
				break
			}
		}
	}
}
