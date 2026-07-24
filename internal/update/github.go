package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultGitHubBaseURL = "https://api.github.com"

type GitHubClient struct {
	Repo       string
	BaseURL    string
	HTTPClient *http.Client
}

func NewGitHubClient(repo string, hc *http.Client) *GitHubClient {
	if repo == "" {
		repo = DefaultRepo
	}
	return &GitHubClient{Repo: repo, BaseURL: defaultGitHubBaseURL, HTTPClient: hc}
}

func (c *GitHubClient) Latest(ctx context.Context) (Release, error) {
	return c.getRelease(ctx, fmt.Sprintf("/repos/%s/releases/latest", c.Repo))
}

func (c *GitHubClient) ByTag(ctx context.Context, tag string) (Release, error) {
	return c.getRelease(ctx, fmt.Sprintf("/repos/%s/releases/tags/%s", c.Repo, tag))
}

func (c *GitHubClient) Download(ctx context.Context, asset Asset) ([]byte, error) {
	if asset.DownloadURL == "" {
		return nil, fmt.Errorf("asset %s has no download URL", asset.Name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.DownloadURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: %s", asset.DownloadURL, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func (c *GitHubClient) getRelease(ctx context.Context, path string) (Release, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = defaultGitHubBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return Release{}, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Release{}, fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	var gr struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			URL                string `json:"url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return Release{}, err
	}
	r := Release{Tag: gr.TagName, Assets: make([]Asset, 0, len(gr.Assets))}
	for _, a := range gr.Assets {
		u := a.BrowserDownloadURL
		if u == "" {
			u = a.URL
		}
		r.Assets = append(r.Assets, Asset{Name: a.Name, DownloadURL: u})
	}
	return r, nil
}

func (c *GitHubClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}
