package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"atm/internal/store"
)

type Client struct {
	cfg    store.EmbeddingConfig
	client *http.Client
}

func New(cfg store.EmbeddingConfig) *Client {
	return &Client{cfg: cfg, client: &http.Client{}}
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

func (c *Client) Embed(text, role string) ([]float64, error) {
	prefix := c.cfg.QueryPrefix
	if role == "document" {
		prefix = c.cfg.DocPrefix
	}
	body, err := json.Marshal(embedRequest{Model: c.cfg.Model, Input: []string{prefix + text}})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.cfg.Endpoint+"/embeddings", bytes.NewReader(body))
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

func (c *Client) EmbedBatch(items []EmbedItem) ([][]float64, error) {
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
	req, err := http.NewRequest(http.MethodPost, c.cfg.Endpoint+"/embeddings", bytes.NewReader(body))
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
