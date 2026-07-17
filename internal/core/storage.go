package core

import "time"

// Storage-maintenance read models (refactor step 6). Kept storage-neutral:
// core knows maintenance exists, never that persistence is event-sourced.
// Field order and JSON tags are frozen — CLI output marshals these directly.

// LogView is one row of `atm store log`: the change history in its
// deterministic total order, subjects rendered the way a user names them.
type LogView struct {
	Ordinal int       `json:"ordinal"`
	ID      string    `json:"id"`
	At      time.Time `json:"at"`
	Actor   string    `json:"actor"`
	Action  string    `json:"action"`
	Subject string    `json:"subject"`
}
