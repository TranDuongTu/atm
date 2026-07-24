package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func VerifyChecksum(sums []byte, filename string, data []byte) error {
	want, err := checksumFor(sums, filename)
	if err != nil {
		return err
	}
	gotRaw := sha256.Sum256(data)
	got := hex.EncodeToString(gotRaw[:])
	if got != want {
		return fmt.Errorf("checksum mismatch for %s", filename)
	}
	return nil
}

func checksumFor(sums []byte, filename string) (string, error) {
	for _, line := range bytes.Split(sums, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		fields := bytes.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if string(fields[1]) != filename {
			continue
		}
		hash := string(fields[0])
		if len(hash) != sha256.Size*2 {
			return "", fmt.Errorf("malformed checksum for %s", filename)
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return "", fmt.Errorf("malformed checksum for %s", filename)
		}
		return hash, nil
	}
	return "", fmt.Errorf("checksum missing for %s", filename)
}
