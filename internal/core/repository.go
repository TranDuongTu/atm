package core

// The persistence seam, reshaped by refactor step 6 (ATM-3b873c) from the
// CRUD placeholders step 4 declared. An event-sourced store records INTENTS,
// so the writer interfaces mirror the closed action set the log records —
// PutTask(diff-the-struct) would erase the distinctions the history views
// render. internal/store/eventlog implements this contract; the store facade
// consumes it. Nothing here names an event, replica, HLC, or projector.

// TaskDraft carries a task-creation intent.
type TaskDraft struct {
	Title       string
	Description string
	Labels      []string
}

// CommentDraft carries a comment-creation intent. TaskID and ReplyTo are the
// user-facing aliases; resolution to identities happens behind the seam.
type CommentDraft struct {
	TaskID  string
	ReplyTo string
	Body    string
	Labels  []string
}

// LabelFields is a partial label upsert: a nil field is not asserted by the
// write, so a concurrent writer's value for it is never clobbered.
type LabelFields struct {
	Description *string
	Expr        *string
}

// ProjectSnapshot is one project's full live state in domain terms, ready
// for a read-model to project: creation-ordered tasks and comments with
// display ordinals precomputed, name-sorted labels, plus the names of labels
// that are no longer live (rows a projection must delete). ChangeCount is
// the freshness key: the number of committed changes the snapshot reflects.
type ProjectSnapshot struct {
	Project       *Project
	Tasks         []*Task
	Comments      []*Comment // TaskID / ReplyTo carry aliases
	Labels        []Label
	RemovedLabels []string
	ChangeCount   int
	// TotalTasks is every task the project ever created, tombstoned included
	// (Tasks holds only the live ones). It exists solely for the `store
	// rebuild` report's task count, which has always been tombstone-inclusive;
	// keeping the field preserves that output byte-for-byte.
	TotalTasks int
}

type ProjectWriter interface {
	// CreateProject records the project's birth. Valid only inside
	// WithProjectBirth (the file must be empty).
	CreateProject(name, actor string) error
	SetProjectName(name, actor string) error
	// EnableCapability / DisableCapability record the project's capability
	// choice as membership events on the project entity.
	EnableCapability(name, actor string) error
	DisableCapability(name, actor string) error
	// ForgetProject drops the project's storage registration so a removed
	// project can be recreated. The caller deletes the media and read-model.
	ForgetProject() error
}

type TaskWriter interface {
	CreateTask(d TaskDraft, actor string) (id string, err error)
	SetTaskTitle(id, title, actor string) error
	SetTaskDescription(id, description, actor string) error
	AddTaskLabel(id, label, actor string) error
	RemoveTaskLabel(id, label, actor string) error
	RemoveTask(id, actor string) error
	// SetTaskCapabilityMeta writes one capability's opaque payload slot on a
	// task; empty payload clears the key.
	SetTaskCapabilityMeta(id, capability, payload, actor string) error
}

type CommentWriter interface {
	CreateComment(d CommentDraft, actor string) (id string, err error)
	SetCommentBody(id, body, actor string) error
	AddCommentLabel(id, label, actor string) error
	RemoveCommentLabel(id, label, actor string) error
	RemoveComment(id, actor string) error
}

type LabelWriter interface {
	// UpsertLabel asserts exactly the non-nil fields.
	UpsertLabel(name string, f LabelFields, actor string) error
	// SeedLabel upserts description/expr unless the label is already live
	// (idempotent vocabulary seeding; never overwrites a curated entry).
	SeedLabel(name, description, expr, actor string) error
	// EnsureLabels registers any of the names not already live, asserting no
	// fields (auto-registration at assign time).
	EnsureLabels(names []string, actor string) error
	// RemoveLabel unregisters a live label; ErrNotFound otherwise.
	RemoveLabel(name, actor string) error
}

// ChangeSet is one locked write transaction against a single project: intent
// writes, existence guards answered from the same consistent state the
// writes apply to, and a Snapshot of the state including this transaction's
// own writes. It is only valid inside the WithProject* closure it was handed
// to.
type ChangeSet interface {
	ProjectWriter
	TaskWriter
	CommentWriter
	LabelWriter

	// RequireProject errors with ErrNotFound unless the project is live.
	RequireProject() error
	// ResolveTask / ResolveComment error with ErrNotFound (unknown alias) or
	// ErrUsage (ambiguous, or wrong kind) exactly like the mutating verbs.
	ResolveTask(id string) error
	ResolveComment(id string) error
	TaskHasLabel(id, label string) (bool, error)
	CommentHasLabel(id, label string) (bool, error)
	HasLiveTasks() (bool, error)

	// Dirty reports whether this transaction has recorded at least one
	// change. Idempotent verbs (SeedLabel on a live label, EnsureLabels
	// with only live names) leave it false; the facade uses it to skip
	// read-model projection for transactions that changed nothing.
	Dirty() bool

	Snapshot() (*ProjectSnapshot, error)
}

// Journal is the write-engine seam the store facade consumes. A transaction
// sees and appends the project's full change history under the project lock;
// birth establishes a brand-new project (WithProjectWrite would refuse it).
type Journal interface {
	WithProjectWrite(code string, fn func(ChangeSet) error) error
	// WithProjectBirth establishes a brand-new project (WithProjectWrite would
	// refuse it). preflight runs under the project lock BEFORE the storage
	// registration is established — it is the caller's existence guard, and a
	// preflight error leaves storage metadata untouched. Only once preflight
	// passes does birth register the project and run fn to record the birth.
	WithProjectBirth(code string, preflight func() error, fn func(ChangeSet) error) error
	// Snapshot is a strict, lock-free point-in-time read (integrity errors
	// wrap ErrIntegrity).
	Snapshot(code string) (*ProjectSnapshot, error)
	// ChangeCount is the freshness key: committed changes on disk now.
	ChangeCount(code string) (int, error)
	// LogEntries renders the history as display entries; on an integrity
	// failure it returns the recoverable prefix ALONGSIDE the error.
	LogEntries(code string) ([]LogEntry, error)
	// MediaExists reports ErrConflict when the project has media on disk in
	// any format, nil when genuinely absent.
	MediaExists(code string) error
}
