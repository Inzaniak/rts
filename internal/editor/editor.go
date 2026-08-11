package editor

import (
	"fmt"
	"os/exec"
	"runtime"
)

const macOSVSCodeCLI = "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code"

// Command returns a blocking command for editing path. VS Code is preferred
// when its CLI is available, including from the standard macOS app bundle;
// otherwise the operating system's text editor is used.
func Command(path string) (*exec.Cmd, error) {
	name, args, err := commandFor(runtime.GOOS, path, exec.LookPath)
	if err != nil {
		return nil, err
	}
	return exec.Command(name, args...), nil
}

type lookPathFunc func(string) (string, error)

func commandFor(goos, path string, lookPath lookPathFunc) (string, []string, error) {
	if code, err := lookPath("code"); err == nil {
		return code, []string{"--wait", path}, nil
	}
	if goos == "darwin" {
		if code, err := lookPath(macOSVSCodeCLI); err == nil {
			return code, []string{"--wait", path}, nil
		}
	}

	var name string
	var args []string
	switch goos {
	case "darwin":
		name = "open"
		args = []string{"-W", "-n", "-t", path}
	case "windows":
		name = "notepad.exe"
		args = []string{path}
	default:
		name = "xdg-open"
		args = []string{path}
	}

	executable, err := lookPath(name)
	if err != nil {
		return "", nil, fmt.Errorf("find a system text editor: %w", err)
	}
	return executable, args, nil
}
