package core

import (
	"context"
	"time"
)

// The role interfaces below are the service seam of the hexagonal
// architecture (docs/architecture/logical-components.md): adapters consume
// them, internal/store satisfies them structurally. They cover exactly the
// union of what internal/tui and internal/cli invoke on the store today,
// minus the storage-format admin surface, which deliberately stays on the
// concrete store (core never knows persistence is event-sourced).

type TaskService interface {
	CreateTask(projectCode, title, description string, labels []string, actor string) (*Task, error)
	GetTask(id string) (*Task, error)
	ListTasks(filters QueryFilters) []*Task
	ListTasksErr(filters QueryFilters) ([]*Task, error)
	GroupTasks(filters QueryFilters) ([]LabelGroup, []*Task)
	GroupTasksErr(filters QueryFilters) ([]LabelGroup, []*Task, error)
	SetTitle(id, title, actor string) error
	SetDescription(id, description, actor string) error
	TaskLabelAdd(id, label, actor string) error
	TaskLabelRemove(id, label, actor string) error
	RemoveTask(id, actor string) error
}

type ProjectService interface {
	CreateProject(code, name, actor string) (*Project, error)
	GetProject(code string) (*Project, error)
	ListProjects() []*Project
	ProjectCodes() ([]string, error)
	SetProjectName(code, name, actor string) error
	EnableProjectCapability(code, name, actor string) error
	DisableProjectCapability(code, name, actor string) error
	RemoveProject(code, actor string) error
	GetProjectConfig(code string) (*ProjectConfig, error)
	GetBoardsConfig(code string) (*BoardsConfig, error)
	SetProjectBoards(code string, b *BoardsConfig, actor string) error
	ProjectRemotes(code string) (map[string]string, error)
	SetProjectRemote(code, name, url, actor string) error
	RemoveProjectRemote(code, name, actor string) error
}

type LabelService interface {
	LabelAdd(name, description, expr, actor string) error
	LabelSeed(name, description, expr, actor string) error
	LabelList(project, namespace string) []Label
	LabelShow(name string) (Label, error)
	LabelRemove(name, actor string) (*LabelRemoveResult, error)
	LabelUsageGrouped(projectCode string) (map[string]int, error)
}

type CommentService interface {
	CreateComment(taskID, body string, labels []string, replyTo, actor string) (*Comment, error)
	GetComment(id string) (*Comment, error)
	ListComments(taskID string) ([]*Comment, error)
	SetCommentBody(id, body, actor string) error
	RemoveComment(id, actor string) error
	CommentLabelAdd(id, label, actor string) error
	CommentLabelRemove(id, label, actor string) error
}

type PersonaService interface {
	CreatePersona(name, prompt, description, actor string) (*Persona, error)
	GetPersona(name string) (*Persona, error)
	ListPersonas() []*Persona
	EditPersona(name string, prompt, description *string, actor string) (*Persona, error)
	RemovePersona(name string) error
}

type VocabularyService interface {
	GetVocabulary(code string) (*Vocabulary, error)
	WriteVocabulary(code string, v *Vocabulary) error
}

type ActivityService interface {
	ReadLogCached(code string) ([]LogEntry, error)
	LastLogSeq(code string) (int, error)
	History(code string, subject Subject) []HistoryView
	HistoryE(code string, subject Subject) ([]HistoryView, error)
	AppendInquiry(code, query string, citedIDs []string) error
}

type IndexService interface {
	ReindexOnce(ctx context.Context, code string, embed EmbedFunc, log ProgressFunc) (IndexResult, error)
	Watch(ctx context.Context, code string, embed EmbedFunc, log ProgressFunc) error
	ListVectorModels(code string) ([]string, error)
	VectorMeta(code, slug string) (*VectorMeta, error)
	DropVectors(code, slug string) error
	SetEmbeddingConfig(code string, cfg EmbeddingConfig, actor string) error
	Search(p SearchParams) (hits []Hit, fallbackUsed bool, err error)
}

type AgentService interface {
	GetAgentsConfig() (AgentsConfig, error)
	SetSelectedAgent(name, actor string) error
	SetAgentArgs(name string, args []string, actor string) error
}

type MaintenanceService interface {
	Init(storePath string) error
	StorePath() string
	Now() time.Time
}

// Service is the composite the composition root injects into adapters.
type Service interface {
	TaskService
	ProjectService
	LabelService
	CommentService
	PersonaService
	VocabularyService
	ActivityService
	IndexService
	AgentService
	MaintenanceService
}
