package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExpandHome replaces a leading "~" or "~/" with the user's home directory
func ExpandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cant get user home directory: %w", err)
	}
	if path == "~" {
		return homeDir, nil
	}
	return filepath.Join(homeDir, path[2:]), nil
}
