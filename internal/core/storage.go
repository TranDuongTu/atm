package core

import (
	"context"
	"time"
)

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

// SyncOptions selects sync direction; neither flag set means both.
type SyncOptions struct {
	Pull   bool
	Push   bool
	DryRun bool
}

// SyncReport is one project's sync outcome. PushErr carries a push-leg
// failure as text ("" = none); a hard failure is returned as an error
// instead of a report.
type SyncReport struct {
	Project        string
	Pulled         int
	Pushed         int
	Bootstrapped   bool
	NewlyContested int
	RemoteAbsent   bool
	DryRun         bool
	PushErr        string
}

// VerifyReport is the per-project outcome of `atm store verify`. Format
// crosses as an opaque string (was a store-internal StoreFormat kind); JSON
// output is unchanged since that kind is itself a string.
type VerifyReport struct {
	Project       string
	LogEntries    int
	LogOK         bool
	Truncated     int
	SeqGaps       []int
	Caches        []CacheCheck
	Diverged      bool
	VectorIndexes []VectorIndexInfo `json:"vector_indexes,omitempty"`
	InquiryCount  int               `json:"inquiry_count"`
	Format        string            `json:"format"`
	V2Events      int               `json:"v2_events,omitempty"`
	V2FileOK      bool              `json:"v2_file_ok,omitempty"`
}

type CacheCheck struct {
	Kind         string // "project" | "task" | "comment"
	ID           string // project code | task id | comment id
	Status       string // "ok" | "stale" | "missing" | "corrupt"
	CacheLogSeq  int
	LastEventSeq int
}

type VectorIndexInfo struct {
	Model      string `json:"model"`
	Count      int    `json:"count"`
	LastLogSeq int    `json:"last_log_seq"`
}

// RebuildReport is the outcome of `atm store rebuild`.
type RebuildReport struct {
	Projects int
	Tasks    int
	Labels   int
}

// UpgradeReport is the per-project outcome of an upgrade to v2 media.
// AlreadyV2 marks a project `upgrade --all` SKIPPED because its effective
// format was already v2 (nothing on disk was touched for it).
type UpgradeReport struct {
	Project   string `json:"project"`
	Format    string `json:"format"`
	Events    int    `json:"events"`
	AlreadyV2 bool   `json:"already_v2,omitempty"`
}

// PruneReport is the per-project outcome of `atm store prune-v1`.
type PruneReport struct {
	Project  string `json:"project"`
	Pruned   bool   `json:"pruned"`
	Archived string `json:"archived,omitempty"`
	Deleted  bool   `json:"deleted,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// StorageAdmin is the storage-maintenance seam the CLI's `atm store ...`
// command group consumes, beside Service (the composition root wires both to
// the same concrete store). Format identifiers cross as opaque strings the
// store validates.
type StorageAdmin interface {
	VerifyStorage() ([]VerifyReport, error)
	VerifyStorageProject(project string) (*VerifyReport, error)
	RebuildDerived() (*RebuildReport, error)
	UpgradeStorage(project string) (*UpgradeReport, error)
	UpgradeAllStorage() ([]UpgradeReport, error)
	PruneLegacy(project string, del bool) (*PruneReport, error)
	SetStorageFormat(format string) error
	StorageFormat(project string) (string, error)
	ReadChangeLog(project string) ([]LogView, error)
	SyncProject(ctx context.Context, project, url string, opts SyncOptions) (*SyncReport, error)
}
