package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubClientLatestAndByTag(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		fmt.Fprint(w, `{"tag_name":"v1.2.3","assets":[{"name":"atm_1.2.3_linux_amd64.tar.gz","browser_download_url":"http://example/tb"},{"name":"SHA256SUMS","browser_download_url":"http://example/sums"}]}`)
	}))
	defer srv.Close()

	c := &GitHubClient{Repo: "owner/repo", BaseURL: srv.URL, HTTPClient: srv.Client()}
	latest, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if latest.Tag != "v1.2.3" || len(latest.Assets) != 2 || latest.Assets[0].DownloadURL != "http://example/tb" {
		t.Fatalf("latest = %+v", latest)
	}
	if _, err := c.ByTag(context.Background(), "v1.2.2"); err != nil {
		t.Fatal(err)
	}
	if paths[0] != "/repos/owner/repo/releases/latest" || paths[1] != "/repos/owner/repo/releases/tags/v1.2.2" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestGitHubClientDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "asset")
	}))
	defer srv.Close()
	c := &GitHubClient{HTTPClient: srv.Client()}
	got, err := c.Download(context.Background(), Asset{Name: "asset", DownloadURL: srv.URL + "/asset"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "asset" {
		t.Fatalf("download = %q", got)
	}
}

func TestGitHubClientHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer srv.Close()
	c := &GitHubClient{Repo: "owner/repo", BaseURL: srv.URL, HTTPClient: srv.Client()}
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
