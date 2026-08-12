//go:build !windows

package selfupdate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func replaceExecutable(path string, binary []byte) error {
	if path == "" {
		return errors.New("cannot locate the current executable")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect current executable: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".rts-update-*")
	if err != nil {
		return fmt.Errorf("prepare update next to %s: %w", path, err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(binary); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace %s: %w (try again with permission to write that directory)", path, err)
	}
	return nil
}
