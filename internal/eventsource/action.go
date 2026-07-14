package eventsource

// The v2 action vocabulary (the L2 action table). Strings are identical to
// v1 (internal/store/log.go) with two deliberate differences:
// task.restored is new (deletion must not be irreversible, L2-4), and
// task.meta-changed has no constant — it is retired. A v1 instance rides
// through the D6 upgrade and, like any unknown action, participates in
// causality but writes no slots (D5, spec decision 8).
const (
	ActionProjectCreated      = "project.created"
	ActionProjectNameChanged  = "project.name-changed"
	ActionProjectRemoved      = "project.removed"
	ActionTaskCreated         = "task.created"
	ActionTaskTitleChanged    = "task.title-changed"
	ActionTaskDescChanged     = "task.description-changed"
	ActionTaskLabelAdded      = "task.label-added"
	ActionTaskLabelRemoved    = "task.label-removed"
	ActionTaskRemoved         = "task.removed"
	ActionTaskRestored        = "task.restored"
	ActionLabelUpserted       = "label.upserted"
	ActionLabelRemoved        = "label.removed"
	ActionCommentCreated      = "comment.created"
	ActionCommentBodyChanged  = "comment.body-changed"
	ActionCommentLabelAdded   = "comment.label-added"
	ActionCommentLabelRemoved = "comment.label-removed"
	ActionCommentRemoved      = "comment.removed"
)
