package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
)

type fakeClient struct {
	latestCalled int
	tagCalled    []string
	release      Release
	downloads    map[string][]byte
	downloaded   []string
}

func (f *fakeClient) Latest(context.Context) (Release, error) {
	f.latestCalled++
	return f.release, nil
}

func (f *fakeClient) ByTag(_ context.Context, tag string) (Release, error) {
	f.tagCalled = append(f.tagCalled, tag)
	return f.release, nil
}

func (f *fakeClient) Download(_ context.Context, a Asset) ([]byte, error) {
	f.downloaded = append(f.downloaded, a.Name)
	b, ok := f.downloads[a.Name]
	if !ok {
		return nil, fmt.Errorf("missing fake download %s", a.Name)
	}
	return b, nil
}

func TestSameReleaseVersion(t *testing.T) {
	if !sameReleaseVersion("v1.2.3", "1.2.3") {
		t.Fatal("v-prefixed and bare versions should match")
	}
	if sameReleaseVersion("dev", "v1.2.3") {
		t.Fatal("dev must not compare equal to a release")
	}
}

func TestRunResolvesLatestAndPinned(t *testing.T) {
	tarball := tarGz(t, "atm", []byte("new"))
	sums := sumsFor("atm_1.2.3_linux_amd64.tar.gz", tarball)
	fc := &fakeClient{
		release: Release{Tag: "v1.2.3", Assets: []Asset{
			{Name: "atm_1.2.3_linux_amd64.tar.gz"},
			{Name: "SHA256SUMS"},
		}},
		downloads: map[string][]byte{
			"atm_1.2.3_linux_amd64.tar.gz": tarball,
			"SHA256SUMS":                   sums,
		},
	}
	var replacedPath string
	res, err := Run(context.Background(), Options{
		CurrentVersion: "v1.2.2",
		GOOS:           "linux",
		GOARCH:         "amd64",
		ExecutablePath: "/tmp/atm",
		Client:         fc,
		Replace: func(path string, data []byte) error {
			replacedPath = path
			if string(data) != "new" {
				t.Fatalf("replacement data = %q", data)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fc.latestCalled != 1 || len(fc.tagCalled) != 0 {
		t.Fatalf("latest/tag calls = %d/%v", fc.latestCalled, fc.tagCalled)
	}
	if !res.Updated || res.OldVersion != "v1.2.2" || res.NewVersion != "v1.2.3" || replacedPath != "/tmp/atm" {
		t.Fatalf("result/path = %+v %q", res, replacedPath)
	}

	fc.latestCalled = 0
	fc.tagCalled = nil
	_, err = Run(context.Background(), Options{
		Version:        "1.2.3",
		CurrentVersion: "dev",
		GOOS:           "linux",
		GOARCH:         "amd64",
		ExecutablePath: "/tmp/atm",
		Client:         fc,
		Replace:        func(string, []byte) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if fc.latestCalled != 0 || len(fc.tagCalled) != 1 || fc.tagCalled[0] != "v1.2.3" {
		t.Fatalf("pinned calls = latest %d tags %v", fc.latestCalled, fc.tagCalled)
	}
}

func TestRunAlreadyCurrentDoesNotDownload(t *testing.T) {
	fc := &fakeClient{release: Release{Tag: "v1.2.3"}}
	res, err := Run(context.Background(), Options{
		CurrentVersion: "1.2.3",
		GOOS:           "linux",
		GOARCH:         "amd64",
		Client:         fc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated {
		t.Fatalf("already current should not update: %+v", res)
	}
	if len(fc.downloaded) != 0 {
		t.Fatalf("downloads = %v", fc.downloaded)
	}
}

func TestRunMissingAssets(t *testing.T) {
	tests := []struct {
		name    string
		assets  []Asset
		wantErr string
	}{
		{"missing tarball", []Asset{{Name: "SHA256SUMS"}}, "missing asset"},
		{"missing sums", []Asset{{Name: "atm_1.2.3_linux_amd64.tar.gz"}}, "missing SHA256SUMS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Run(context.Background(), Options{
				CurrentVersion: "v1.2.2",
				GOOS:           "linux",
				GOARCH:         "amd64",
				Client:         &fakeClient{release: Release{Tag: "v1.2.3", Assets: tt.assets}},
			})
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte(tt.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func sumsFor(name string, data []byte) []byte {
	sum := sha256.Sum256(data)
	return []byte(fmt.Sprintf("%x  %s\n", sum[:], name))
}

func tarGz(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
