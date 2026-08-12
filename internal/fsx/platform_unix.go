//go:build !windows

package fsx

import (
	"errors"
	"os"
	"syscall"
)

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}

func isCrossDeviceError(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}
