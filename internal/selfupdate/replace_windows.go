package selfupdate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func replaceExecutable(path string, binary []byte) error {
	if path == "" {
		return errors.New("cannot locate the current executable")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("inspect current executable: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".rts-update-*.exe")
	if err != nil {
		return fmt.Errorf("prepare update next to %s: %w", path, err)
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(binary); err != nil {
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
	sourcePath, err := windows.UTF16PtrFromString(tempPath)
	if err != nil {
		return err
	}
	destinationPath, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(sourcePath, destinationPath, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err == nil {
		return nil
	} else if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return fmt.Errorf("replace %s: %w (try again with permission to write that directory)", path, err)
	}

	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	script := "$ErrorActionPreference='Stop'; " +
		"while (Get-Process -Id " + strconv.Itoa(os.Getpid()) + " -ErrorAction SilentlyContinue) { Start-Sleep -Milliseconds 100 }; " +
		"Move-Item -LiteralPath " + quote(tempPath) + " -Destination " + quote(path) + " -Force"
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if err := command.Start(); err != nil {
		return fmt.Errorf("schedule replacement of %s: %w", path, err)
	}
	keep = true
	_ = command.Process.Release()
	return nil
}
