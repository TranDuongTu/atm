package store

import "time"

type Label struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	LogSeq      int    `json:"log_seq,omitempty"`
}

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
	ID          string    `json:"id"`
	ProjectCode string    `json:"project_code"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Labels      []string  `json:"labels"`
	LogSeq      int       `json:"log_seq"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
	UpdatedAt   time.Time `json:"updated_at"`
	UpdatedBy   string    `json:"updated_by"`
}
