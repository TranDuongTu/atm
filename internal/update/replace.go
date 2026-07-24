package update

import (
	"fmt"
	"os"
	"path/filepath"
)

func ReplaceBinary(target string, data []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".atm-update-*")
	if err != nil {
		return fmt.Errorf("create replacement temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write replacement temp file: %w", err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod replacement temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close replacement temp file: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("replace %s: %w", target, err)
	}
	cleanup = false
	return nil
}
