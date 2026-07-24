package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path"
)

func ExtractBinary(tarball []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if path.Base(hdr.Name) != "atm" {
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("archive entry %s is not a regular file", hdr.Name)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read atm from archive: %w", err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("archive missing atm binary")
}
