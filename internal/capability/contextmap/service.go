package contextmap

import "atm/internal/core"

// Service is the slice of core this capability consumes: tasks it labels
// and describes, comments it writes provenance stamps into, and labels it
// seeds. The concrete *store.Store satisfies it structurally; nothing here
// names persistence.
type Service interface {
	core.TaskService
	core.CommentService
	core.LabelService
}
