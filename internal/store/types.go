package store

import "time"

type Label struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type HistoryEntry struct {
	ID     string         `json:"id"`
	Action string         `json:"action"`
	Actor  string         `json:"actor"`
	At     time.Time      `json:"at"`
	Meta   map[string]any `json:"meta,omitempty"`
}

type Project struct {
	Code         string         `json:"code"`
	Name         string         `json:"name"`
	NextTaskN    int            `json:"next_task_n"`
	History      []HistoryEntry `json:"history,omitempty"`
	NextHistoryN int            `json:"next_history_n,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	CreatedBy    string         `json:"created_by"`
	UpdatedAt    time.Time      `json:"updated_at"`
	UpdatedBy    string         `json:"updated_by"`
}

type Task struct {
	ID          string         `json:"id"`
	ProjectCode string         `json:"project_code"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Labels      []string       `json:"labels"`
	History     []HistoryEntry `json:"history"`
	CreatedAt   time.Time      `json:"created_at"`
	CreatedBy   string         `json:"created_by"`
	UpdatedAt   time.Time      `json:"updated_at"`
	UpdatedBy   string         `json:"updated_by"`
}