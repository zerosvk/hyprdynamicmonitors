package utils

import (
	"fmt"
	"os"
	"path/filepath"
)

func WriteAtomic(filename string, data []byte) error {
	fi, err := os.Stat(filename)
	if err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("%s already exists and is not a regular file", filename)
	}
	f, err := os.CreateTemp(filepath.Dir(filename), filepath.Base(filename)+".tmp")
	if err != nil {
		return fmt.Errorf("cant create temp file: %w", err)
	}
	tmpName := f.Name()
	// remove the temp file on any failure below, the rename consumes it on success
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := f.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("cant write data to a file %s: %w", filename, err)
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("err while sync: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("err while close: %w", err)
	}
	if err := os.Rename(tmpName, filename); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to rename temp config %s to %s: %w", tmpName, filename, err)
	}
	return nil
}
