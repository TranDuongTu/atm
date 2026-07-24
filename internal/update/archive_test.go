package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

func TestExtractBinary(t *testing.T) {
	got, err := ExtractBinary(tarGz(t, "atm", []byte("binary")))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary" {
		t.Fatalf("binary = %q", got)
	}
}

func TestExtractBinaryFailures(t *testing.T) {
	tests := []struct {
		name    string
		tarball []byte
		wantErr string
	}{
		{"missing", tarGz(t, "not-atm", []byte("x")), "missing"},
		{"directory", tarGzHeader(t, &tar.Header{Name: "atm", Typeflag: tar.TypeDir}), "not a regular"},
		{"symlink", tarGzHeader(t, &tar.Header{Name: "atm", Typeflag: tar.TypeSymlink, Linkname: "x"}), "not a regular"},
		{"invalid", []byte("nope"), "open archive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ExtractBinary(tt.tarball)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func tarGzHeader(t *testing.T, hdr *tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(hdr); err != nil {
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
