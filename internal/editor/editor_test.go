package editor

import (
	"errors"
	"reflect"
	"testing"
)

func TestCommandForPrefersVSCode(t *testing.T) {
	lookPath := func(name string) (string, error) {
		if name == "code" {
			return "/tools/code", nil
		}
		return "", errors.New("not found")
	}

	name, args, err := commandFor("darwin", "/tmp/resource.json", lookPath)
	if err != nil {
		t.Fatal(err)
	}
	if name != "/tools/code" {
		t.Fatalf("command = %q, want /tools/code", name)
	}
	wantArgs := []string{"--wait", "/tmp/resource.json"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestCommandForFindsVSCodeInMacOSAppBundle(t *testing.T) {
	lookPath := func(name string) (string, error) {
		if name == macOSVSCodeCLI {
			return name, nil
		}
		return "", errors.New("not found")
	}

	name, args, err := commandFor("darwin", "/tmp/resource.json", lookPath)
	if err != nil {
		t.Fatal(err)
	}
	if name != macOSVSCodeCLI {
		t.Fatalf("command = %q, want %q", name, macOSVSCodeCLI)
	}
	wantArgs := []string{"--wait", "/tmp/resource.json"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestCommandForUsesSystemEditor(t *testing.T) {
	tests := []struct {
		goos     string
		command  string
		wantArgs []string
	}{
		{"darwin", "open", []string{"-W", "-n", "-t", "/tmp/resource.json"}},
		{"windows", "notepad.exe", []string{"/tmp/resource.json"}},
		{"linux", "xdg-open", []string{"/tmp/resource.json"}},
	}

	for _, test := range tests {
		t.Run(test.goos, func(t *testing.T) {
			lookPath := func(name string) (string, error) {
				if name == test.command {
					return "/system/" + name, nil
				}
				return "", errors.New("not found")
			}
			name, args, err := commandFor(test.goos, "/tmp/resource.json", lookPath)
			if err != nil {
				t.Fatal(err)
			}
			if name != "/system/"+test.command {
				t.Fatalf("command = %q, want %q", name, "/system/"+test.command)
			}
			if !reflect.DeepEqual(args, test.wantArgs) {
				t.Fatalf("args = %#v, want %#v", args, test.wantArgs)
			}
		})
	}
}

func TestCommandForReportsMissingSystemEditor(t *testing.T) {
	_, _, err := commandFor("linux", "/tmp/resource.json", func(string) (string, error) {
		return "", errors.New("not found")
	})
	if err == nil {
		t.Fatal("expected an error")
	}
}
