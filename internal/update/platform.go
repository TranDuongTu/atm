package update

import "fmt"

type Target struct {
	OS   string
	Arch string
}

func targetFor(goos, goarch string) (Target, error) {
	switch goos {
	case "linux", "darwin":
	default:
		return Target{}, fmt.Errorf("unsupported os: %s", goos)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return Target{}, fmt.Errorf("unsupported arch: %s", goarch)
	}
	return Target{OS: goos, Arch: goarch}, nil
}

func assetName(tag string, target Target) string {
	return fmt.Sprintf("atm_%s_%s_%s.tar.gz", trimOneV(tag), target.OS, target.Arch)
}
