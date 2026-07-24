package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"atm/internal/store"
)

type Client struct {
	cfg    store.EmbeddingConfig
	client *http.Client
}

// requestTimeout is a backstop so a hung endpoint can never wedge an embed
// call forever, even under a background context. Cancellation via the
// request context remains the prompt path (ATM-4c476c).
const requestTimeout = 60 * time.Second

func New(cfg store.EmbeddingConfig) *Client {
	return &Client{cfg: cfg, client: &http.Client{Timeout: requestTimeout}}
}

type EmbedItem struct {
	Text string
	Role string
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}

func (c *Client) Embed(ctx context.Context, text, role string) ([]float64, error) {
	prefix := c.cfg.QueryPrefix
	if role == "document" {
		prefix = c.cfg.DocPrefix
	}
	body, err := json.Marshal(embedRequest{Model: c.cfg.Model, Input: []string{prefix + text}})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed endpoint %s: status %d: %s", c.cfg.Endpoint, resp.StatusCode, string(raw))
	}
	var er embedResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	if er.Error != "" {
		return nil, fmt.Errorf("embed error: %s", er.Error)
	}
	if len(er.Data) == 0 {
		return nil, fmt.Errorf("embed response: no data")
	}
	return er.Data[0].Embedding, nil
}

func (c *Client) EmbedBatch(ctx context.Context, items []EmbedItem) ([][]float64, error) {
	if len(items) == 0 {
		return nil, nil
	}
	inputs := make([]string, len(items))
	for i, it := range items {
		prefix := c.cfg.QueryPrefix
		if it.Role == "document" {
			prefix = c.cfg.DocPrefix
		}
		inputs[i] = prefix + it.Text
	}
	body, err := json.Marshal(embedRequest{Model: c.cfg.Model, Input: inputs})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed endpoint %s: status %d: %s", c.cfg.Endpoint, resp.StatusCode, string(raw))
	}
	var er embedResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	if er.Error != "" {
		return nil, fmt.Errorf("embed error: %s", er.Error)
	}
	out := make([][]float64, len(er.Data))
	for i, d := range er.Data {
		out[i] = d.Embedding
	}
	return out, nil
}
