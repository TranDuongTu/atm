package core

type Hit struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Score   float64  `json:"score"`
	Title   string   `json:"title,omitempty"`
	Snippet string   `json:"snippet"`
	Labels  []string `json:"labels,omitempty"`
	Match   string   `json:"match"`
}

type SearchParams struct {
	Project     string
	Model       string
	QueryVector []float64
	QueryText   string
	Kind        string
	K           int
	Threshold   float64
}

type IndexResult struct {
	Indexed int
	Model   string
	LogSeq  int
}

type EmbedFunc func(text, role string) ([]float64, error)

type ProgressFunc func(msg string)

type VectorMeta struct {
	Model           string `json:"model"`
	Dim             int    `json:"dim"`
	LastLogSeq      int    `json:"last_log_seq"`
	LastReindexedAt string `json:"last_reindexed_at"`
	Count           int    `json:"count"`
}
