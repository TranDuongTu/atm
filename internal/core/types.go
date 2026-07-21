package core

import "time"

type Label struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Expr, when non-empty, makes this a computed label (a "board"): its
	// membership is derived by evaluating the expression over other labels
	// rather than asserted by tasks. See docs/superpowers/specs/2026-07-13-computed-labels-boards-design.md
	Expr    string `json:"expr,omitempty"`
	Ordinal int    `json:"ordinal,omitempty"`
}

// IsComputed reports whether membership is derived rather than asserted.
// True for boards (Expr set) and for namespace labels (name ends in ":*",
// whose expression is the prefix pattern implicit in the name).
func (l Label) IsComputed() bool { return l.Expr != "" || IsNamespaceName(l.Name) }

type Project struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Ordinal int    `json:"ordinal,omitempty"`
	// Capabilities is the project's enabled capability set. nil means no
	// capability choice was ever recorded — a legacy project — which
	// consumers (the capability registry) read as "all built-ins enabled".
	// A non-nil empty slice means explicitly none.
	Capabilities []string  `json:"capabilities,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	CreatedBy    string    `json:"created_by"`
	UpdatedAt    time.Time `json:"updated_at"`
	UpdatedBy    string    `json:"updated_by"`
}

type Task struct {
	ID          string    `json:"id"`
	ProjectCode string    `json:"project_code"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Labels      []string  `json:"labels"`
	// Meta maps capability name → opaque payload. The store never interprets
	// a payload; only the owning capability's verbs read or write its own key
	// (docs/architecture/label-substrate-and-capabilities.md, "The metadata
	// column"). Absent key = no state; clearing is writing the empty string.
	Meta        map[string]string `json:"meta,omitempty"`
	Ordinal     int       `json:"ordinal,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
	UpdatedAt   time.Time `json:"updated_at"`
	UpdatedBy   string    `json:"updated_by"`
}

type Comment struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	ReplyTo   string    `json:"reply_to,omitempty"`
	Body      string    `json:"body"`
	Labels    []string  `json:"labels"`
	Ordinal   int       `json:"ordinal,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}

type Persona struct {
	Name        string    `json:"name"`
	Prompt      string    `json:"prompt"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	CreatedBy   string    `json:"created_by"`
	UpdatedBy   string    `json:"updated_by"`
}

type LabelRemoveResult struct {
	RetainedUsage int `json:"retained_usage"`
}
