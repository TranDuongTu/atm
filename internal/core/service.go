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
	SetTaskCapabilityMeta(id, capability, payload, actor string) error
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
	SetProjectArtOn(code string, on bool, pair []string, actor string) error
	ProjectRemotes(code string) (map[string]string, error)
	SetProjectRemote(code, name, url, actor string) error
	RemoveProjectRemote(code, name, actor string) error
	ProjectRepos(code string) ([]RepoConfig, error)
	SetProjectRepo(code, name, path, url, actor string) error
	RemoveProjectRepo(code, name, actor string) error
}

type ChannelService interface {
	CreateChannel(code string, rec ChannelRecord, actor string) (*Task, error)
	EditChannel(code, name string, purpose *string, addr *ChannelAddress, actor string) error
	RemoveChannel(code, name, actor string) error
	// AddChannelEndpoint adds or corrects the channel's endpoint for one
	// medium; RemoveChannelEndpoint drops it, leaving the handle.
	AddChannelEndpoint(code, name string, ep ChannelEndpoint, actor string) error
	RemoveChannelEndpoint(code, name, typ, actor string) error
	ChannelRecords(code string) ([]ChannelRecord, error)
	ProjectChannels(code string) ([]ChannelView, error)
	GetChannelByName(code, name string) (*ChannelView, error)
	RepoChannelTargets(code string) ([]RepoConfig, error)
	// SetChannelWiring records how this machine reaches one endpoint; an
	// empty typ resolves to the channel's only one.
	SetChannelWiring(code, name, typ, path, mcpServer, actor string) error
	// AddChannelStamp vouches that this agent reached one endpoint. An
	// empty kind reads as use.
	AddChannelStamp(code, name, typ, kind, note, actor string) error
	MigrateReposToChannels(code, actor string) (migrated int, unwired []string, skipped []string, err error)
	// ResetChannelRecord restores the channel's purpose and role hint from
	// its OWN origin version; endpoints and wiring are untouched.
	ResetChannelRecord(code, name, actor string) (*ChannelRecord, error)
}

type ChecklistService interface {
	CreateChecklist(code string, rec ChecklistRecord, actor string) (*Task, error)
	SetChecklist(code, name string, rec ChecklistRecord, actor string) error
	RemoveChecklist(code, name, taskID, actor string) error // taskID "" unless disambiguating
	ChecklistRecords(code string) ([]ChecklistRecord, error)
	SuitedChecklists(code, persona string) ([]ChecklistRecord, error)
	GetChecklist(code, name string) (*ChecklistRecord, error)
	// ResetChecklistRecord restores the record from its OWN origin version.
	ResetChecklistRecord(code, name, actor string) (*ChecklistRecord, error)
}

type LabelService interface {
	LabelAdd(name, description, expr, actor string) error
	LabelSeed(name, description, expr, actor string) error
	// LabelSeedBatch converges a whole vocabulary in ONE write transaction
	// (one event-log fold for a converged batch — ATM-40faff), with
	// LabelSeed's exact per-label semantics. All labels must belong to one
	// project (ErrUsage otherwise); an empty batch is a no-op.
	LabelSeedBatch(labels []Label, actor string) error
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

// PersonaRecordService is the PROJECT-scoped persona surface: an identity a
// project owns, imported from a document and restorable to the profile it
// came from. It sits beside PersonaService while the machine-global personas
// are still being retired (ATM-207ab8, second PR).
type PersonaRecordService interface {
	PersonaRecords(code string) ([]Persona, error)
	GetPersonaRecord(code, name string) (*Persona, error)
	// SetPersonaRecord imports a document as the record, creating it or
	// replacing it wholesale. Name and origin come from the existing record
	// on replace; a new record takes the caller's origin, defaulting to user.
	SetPersonaRecord(code string, p Persona, actor string) (*Task, error)
	// ResetPersonaRecord restores the record from its OWN origin version.
	ResetPersonaRecord(code, name, actor string) (*Persona, error)
	RemovePersonaRecord(code, name, taskID, actor string) error
}

type PersonaService interface {
	CreatePersona(name, prompt, description, actor string) (*Persona, error)
	GetPersona(name string) (*Persona, error)
	ListPersonas() []*Persona
	RemovePersona(name string) error
	// PersonaDoc returns a custom persona's raw markdown document (usage
	// error for built-ins, which ship inside the binary).
	PersonaDoc(name string) (string, error)
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
	AppendInquiry(code, query string, returnedIDs, citedIDs, openedIDs []string) error
	AppendAskTurn(code, sessionID string, t AskTurn) error
	ReadAskTurns(code, sessionID string) ([]AskTurn, error)
}

type IndexService interface {
	ReindexOnce(ctx context.Context, code string, embed EmbedFunc, log ProgressFunc) (IndexResult, error)
	Watch(ctx context.Context, code string, embed EmbedFunc, log ProgressFunc) error
	ListVectorModels(code string) ([]string, error)
	VectorMeta(code, slug string) (*VectorMeta, error)
	DropVectors(code, slug string) error
	SetEmbeddingConfig(code string, cfg EmbeddingConfig, actor string) error
	SetChatConfig(code string, cfg ChatConfig, actor string) error
	SetInquiryLog(code string, enabled bool, actor string) error
	PendingIndexCount(code, slug string) (int, error)
	// Documents returns full document text keyed by entity ID, for hydrating
	// search hits whose Snippet is only an 80-rune truncation. On IndexService
	// rather than store-local because internal/cli may not import
	// internal/store, and core.Service is what the answer engine's Searcher is
	// satisfied by (ATM-d4ceed).
	Documents(code string, ids []string) (map[string]string, error)
	Search(p SearchParams) (hits []Hit, fallbackUsed bool, err error)
}

type AgentService interface {
	GetAgentsConfig() (AgentsConfig, error)
	SetSelectedAgent(name, actor string) error
	SetAgentArgs(name string, args []string, actor string) error
	SetAgentModel(key, model, actor string) error
}

// StoreStats is the display summary the TUI status bar renders: event-log
// bytes and lines for the requested scope, and the storage format version.
// Version is always store-wide ("v1", "v2", or "mixed" when per-project
// formats disagree) — it describes the store, not one project's slice.
type StoreStats struct {
	SizeBytes  int64
	EventCount int
	Version    string
}

type MaintenanceService interface {
	Init(storePath string) error
	StorePath() string
	// StoreStats totals one project's event log, or every project's when
	// project is "".
	StoreStats(project string) (StoreStats, error)
	Now() time.Time
}

// Service is the composite the composition root injects into adapters.
type Service interface {
	TaskService
	ProjectService
	ChannelService
	ChecklistService
	LabelService
	CommentService
	PersonaService
	PersonaRecordService
	ProfileService
	VocabularyService
	ActivityService
	IndexService
	AgentService
	MaintenanceService
}

// ProfileService is the machine's profile registry: what operating-model
// bundles are available here, and how one gets installed. Like personas, it
// is plain-file side-store state under the store root rather than ledger
// data — installing a profile changes no project.
type ProfileService interface {
	// ListProfiles returns every available profile, newest version first
	// within a name — so the first row for a name is what a bare name
	// resolves to.
	ListProfiles() ([]ProfileEntry, error)
	// GetProfile loads one. An empty version takes the highest available
	// across installed AND embedded copies.
	GetProfile(name, version string) (*Profile, ProfileEntry, error)
	// InstallProfile installs a built artifact from a local file. A
	// non-empty wantDigest must match the artifact's own.
	InstallProfile(artifactPath, wantDigest string) (ProfileEntry, error)
	// PlanProfile reports what applying p to the project would do, writing
	// nothing. ApplyProfile does it: records are created, updated, or
	// restamped per the plan; conflicts stay untouched unless force.
	// Neither enables capabilities — the caller that owns the registry
	// does that, so an unknown name is refused before any write.
	PlanProfile(code string, p *Profile) (*ApplyPlan, error)
	ApplyProfile(code string, p *Profile, force bool, actor string) (*ApplyPlan, error)
}
