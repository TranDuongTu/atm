package store

import "time"

type Label struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Expr, when non-empty, makes this a computed label (a "board"): its
	// membership is derived by evaluating the expression over other labels
	// rather than asserted by tasks. See docs/superpowers/specs/2026-07-13-computed-labels-boards-design.md
	Expr   string `json:"expr,omitempty"`
	LogSeq int    `json:"log_seq,omitempty"`
}

// IsComputed reports whether membership is derived rather than asserted.
// True for boards (Expr set) and for namespace labels (name ends in ":*",
// whose expression is the prefix pattern implicit in the name).
func (l Label) IsComputed() bool { return l.Expr != "" || IsNamespaceName(l.Name) }

type Project struct {
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	NextTaskN int       `json:"next_task_n"`
	LogSeq    int       `json:"log_seq"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}

type Task struct {
	ID           string    `json:"id"`
	ProjectCode  string    `json:"project_code"`
	Title        string    `json:"title"`
	Description  string    `json:"description,omitempty"`
	Labels       []string  `json:"labels"`
	LogSeq       int       `json:"log_seq"`
	CreatedAt    time.Time `json:"created_at"`
	CreatedBy    string    `json:"created_by"`
	UpdatedAt    time.Time `json:"updated_at"`
	UpdatedBy    string    `json:"updated_by"`
	NextCommentN int       `json:"next_comment_n,omitempty"`
}

type Comment struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	ReplyTo   string    `json:"reply_to,omitempty"`
	Body      string    `json:"body"`
	Labels    []string  `json:"labels"`
	LogSeq    int       `json:"log_seq"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}
