package update

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"

	"atm/internal/version"
)

const DefaultRepo = "TranDuongTu/atm"

type Asset struct {
	Name        string
	DownloadURL string
}

type Release struct {
	Tag    string
	Assets []Asset
}

type ReleaseClient interface {
	Latest(context.Context) (Release, error)
	ByTag(context.Context, string) (Release, error)
	Download(context.Context, Asset) ([]byte, error)
}

type ReplaceFunc func(string, []byte) error

type Options struct {
	Version        string
	CurrentVersion string
	ExecutablePath string
	GOOS           string
	GOARCH         string
	Client         ReleaseClient
	HTTPClient     *http.Client
	Replace        ReplaceFunc
}

type Result struct {
	OldVersion string `json:"old_version"`
	NewVersion string `json:"new_version"`
	TargetPath string `json:"target_path,omitempty"`
	Updated    bool   `json:"updated"`
}

func Run(ctx context.Context, opts Options) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	current := opts.CurrentVersion
	if current == "" {
		current = version.Version
	}
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := opts.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	target, err := targetFor(goos, goarch)
	if err != nil {
		return Result{}, err
	}
	client := opts.Client
	if client == nil {
		client = NewGitHubClient(DefaultRepo, opts.HTTPClient)
	}
	release, err := resolveRelease(ctx, client, opts.Version)
	if err != nil {
		return Result{}, err
	}
	res := Result{OldVersion: current, NewVersion: release.Tag}
	if sameReleaseVersion(current, release.Tag) {
		return res, nil
	}
	name := assetName(release.Tag, target)
	tarAsset, ok := findAsset(release.Assets, name)
	if !ok {
		return Result{}, fmt.Errorf("release %s missing asset %s", release.Tag, name)
	}
	sumsAsset, ok := findAsset(release.Assets, "SHA256SUMS")
	if !ok {
		return Result{}, fmt.Errorf("release %s missing SHA256SUMS", release.Tag)
	}
	tarball, err := client.Download(ctx, tarAsset)
	if err != nil {
		return Result{}, fmt.Errorf("download %s: %w", tarAsset.Name, err)
	}
	sums, err := client.Download(ctx, sumsAsset)
	if err != nil {
		return Result{}, fmt.Errorf("download SHA256SUMS: %w", err)
	}
	if err := VerifyChecksum(sums, name, tarball); err != nil {
		return Result{}, err
	}
	binary, err := ExtractBinary(tarball)
	if err != nil {
		return Result{}, err
	}
	path := opts.ExecutablePath
	if path == "" {
		path, err = os.Executable()
		if err != nil {
			return Result{}, fmt.Errorf("resolve executable path: %w", err)
		}
	}
	replace := opts.Replace
	if replace == nil {
		replace = ReplaceBinary
	}
	if err := replace(path, binary); err != nil {
		return Result{}, err
	}
	res.TargetPath = path
	res.Updated = true
	return res, nil
}

func resolveRelease(ctx context.Context, client ReleaseClient, requested string) (Release, error) {
	if requested == "" || requested == "latest" {
		r, err := client.Latest(ctx)
		if err != nil {
			return Release{}, fmt.Errorf("resolve latest release: %w", err)
		}
		return r, nil
	}
	r, err := client.ByTag(ctx, normalizeRequestedTag(requested))
	if err != nil {
		return Release{}, fmt.Errorf("resolve release %s: %w", requested, err)
	}
	return r, nil
}

func normalizeRequestedTag(tag string) string {
	if tag == "" || strings.HasPrefix(tag, "v") {
		return tag
	}
	return "v" + tag
}

func findAsset(assets []Asset, name string) (Asset, bool) {
	for _, a := range assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

func sameReleaseVersion(current, release string) bool {
	if current == "" || current == "dev" {
		return false
	}
	return trimOneV(current) == trimOneV(release)
}

func trimOneV(s string) string {
	return strings.TrimPrefix(s, "v")
}
