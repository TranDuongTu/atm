package update

import (
	"strings"
	"testing"
)

func TestTargetFor(t *testing.T) {
	for _, tc := range []struct {
		goos, goarch string
	}{
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
	} {
		t.Run(tc.goos+"/"+tc.goarch, func(t *testing.T) {
			got, err := targetFor(tc.goos, tc.goarch)
			if err != nil {
				t.Fatal(err)
			}
			if got.OS != tc.goos || got.Arch != tc.goarch {
				t.Fatalf("target = %+v", got)
			}
		})
	}
	if _, err := targetFor("windows", "amd64"); err == nil || !strings.Contains(err.Error(), "windows") {
		t.Fatalf("unsupported os err = %v", err)
	}
	if _, err := targetFor("linux", "386"); err == nil || !strings.Contains(err.Error(), "386") {
		t.Fatalf("unsupported arch err = %v", err)
	}
}

func TestAssetName(t *testing.T) {
	got := assetName("v1.2.3", Target{OS: "linux", Arch: "amd64"})
	if got != "atm_1.2.3_linux_amd64.tar.gz" {
		t.Fatalf("assetName = %q", got)
	}
}
