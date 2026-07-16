package core

// Repository interfaces are the persistence seam DECLARED by refactor step 4
// and IMPLEMENTED by step 6 (ATM-3b873c), which carves the store's event-log
// write-engine behind them. Nothing consumes them yet; step 6 may refine the
// shapes when the carve is studied. They mirror what the store's cache layer
// provides today, in domain terms only.

type TaskRepository interface {
	PutTask(t *Task) error
	GetTask(id string) (*Task, error)
	ListTasksForProject(code string) ([]*Task, error)
	DeleteTask(id string) error
}

type LabelRepository interface {
	PutLabel(l Label) error
	GetLabel(name string) (Label, error)
	ListLabels(project string) ([]Label, error)
	DeleteLabel(name string) error
}

type ProjectRepository interface {
	PutProject(p *Project) error
	GetProject(code string) (*Project, error)
	ListProjects() ([]*Project, error)
	DeleteProject(code string) error
}

type CommentRepository interface {
	PutComment(c *Comment) error
	GetComment(id string) (*Comment, error)
	ListCommentsForTask(taskID string) ([]*Comment, error)
	DeleteComment(id string) error
}
