package store

import "time"

type Label struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type GuideRef struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
}

type GuideSection struct {
	Name string     `json:"name"`
	Refs []GuideRef `json:"refs"`
}

type Guide struct {
	Sections  []GuideSection `json:"sections"`
	UpdatedAt time.Time      `json:"updated_at"`
	UpdatedBy string         `json:"updated_by"`
}

type Project struct {
	Code                    string         `json:"code"`
	Name                    string         `json:"name"`
	TypeAxis                string         `json:"type_axis,omitempty"`
	Labels                  []Label        `json:"labels"`
	NextTaskN               int            `json:"next_task_n"`
	Guide                   *Guide         `json:"guide,omitempty"`
	GuideFreshnessThreshold string         `json:"guide_freshness_threshold,omitempty"`
	RepoPaths               []string       `json:"repo_paths,omitempty"`
	History                 []HistoryEntry `json:"history,omitempty"`
	NextHistoryN            int            `json:"next_history_n,omitempty"`
	CreatedAt               time.Time      `json:"created_at"`
	CreatedBy               string         `json:"created_by"`
	UpdatedAt               time.Time      `json:"updated_at"`
}

type Link struct {
	Type   string `json:"type"`
	Target string `json:"target"`
}

type Claim struct {
	Actor string    `json:"actor"`
	At    time.Time `json:"at"`
}

type Todo struct {
	ID     string    `json:"id"`
	Text   string    `json:"text"`
	Done   bool      `json:"done"`
	Author string    `json:"author"`
	At     time.Time `json:"at"`
}

type Followup struct {
	ID         string     `json:"id"`
	Text       string     `json:"text"`
	Assignee   string     `json:"assignee,omitempty"`
	Status     string     `json:"status"`
	Due        *time.Time `json:"due,omitempty"`
	Author     string     `json:"author"`
	At         time.Time  `json:"at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
}

type DiscussionEntry struct {
	ID     string    `json:"id"`
	Text   string    `json:"text"`
	Author string    `json:"author"`
	At     time.Time `json:"at"`
}

type HistoryEntry struct {
	ID     string         `json:"id"`
	Action string         `json:"action"`
	Actor  string         `json:"actor"`
	At     time.Time      `json:"at"`
	Meta   map[string]any `json:"meta,omitempty"`
}

type Task struct {
	ID          string            `json:"id"`
	ProjectCode string            `json:"project_code"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Status      string            `json:"status"`
	Labels      []string          `json:"labels"`
	Links       []Link            `json:"links"`
	Claim       *Claim            `json:"claim,omitempty"`
	Todos       []Todo            `json:"todos"`
	Followups   []Followup        `json:"followups"`
	Discussions []DiscussionEntry `json:"discussions"`
	History     []HistoryEntry    `json:"history"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}
