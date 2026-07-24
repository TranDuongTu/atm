package update

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestVerifyChecksum(t *testing.T) {
	data := []byte("tarball")
	sum := sha256.Sum256(data)
	sums := []byte(fmt.Sprintf("%x  other.tar.gz\n%x  atm_1.2.3_linux_amd64.tar.gz\n", sha256.Sum256([]byte("other")), sum))
	if err := VerifyChecksum(sums, "atm_1.2.3_linux_amd64.tar.gz", data); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyChecksumFailures(t *testing.T) {
	tests := []struct {
		name    string
		sums    string
		data    []byte
		wantErr string
	}{
		{"missing", "", []byte("x"), "missing"},
		{"malformed", "abc  atm.tar.gz\n", []byte("x"), "malformed"},
		{"mismatch", strings.Repeat("0", 64) + "  atm.tar.gz\n", []byte("x"), "mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyChecksum([]byte(tt.sums), "atm.tar.gz", tt.data)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
