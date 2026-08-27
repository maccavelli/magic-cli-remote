package appdirs

import (
	"fmt"
	"path/filepath"
)

// absClean validates and cleans a path argument shared by the platform
// security helpers.
func absClean(path, what string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("appdirs: empty %s", what)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("appdirs: %s must be absolute: %q", what, path)
	}
	return filepath.Clean(path), nil
}
