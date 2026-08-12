package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunReplacesExecutableAfterVerification(t *testing.T) {
	binary := []byte("new rts binary")
	archive := testArchiveForOS(t, binary, runtime.GOOS)
	sum := sha256.Sum256(archive)
	assetName := fmt.Sprintf("rts_1.2.3_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)

	client := testClient(func(request *http.Request) (string, int) {
		switch request.URL.Path {
		case "/latest":
			return fmt.Sprintf(`{"tag_name":"v1.2.3","assets":[{"name":%q,"browser_download_url":"https://test/archive"},{"name":"checksums.txt","browser_download_url":"https://test/checksums"}]}`, assetName), http.StatusOK
		case "/archive":
			return string(archive), http.StatusOK
		case "/checksums":
			return fmt.Sprintf("%x  %s\n", sum, assetName), http.StatusOK
		default:
			return "not found", http.StatusNotFound
		}
	})

	executable := filepath.Join(t.TempDir(), "rts")
	if err := os.WriteFile(executable, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), Options{
		CurrentVersion: "v1.0.0", APIURL: "https://test/latest", Executable: executable, Client: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || result.Version != "v1.2.3" {
		t.Fatalf("unexpected result: %+v", result)
	}
	got, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatalf("installed binary = %q, want %q", got, binary)
	}
}

func testArchiveForOS(t *testing.T, binary []byte, goos string) []byte {
	t.Helper()
	if goos == "windows" {
		return testZipArchive(t, binary)
	}
	return testArchive(t, binary)
}

func TestRunDoesNotDownloadWhenAlreadyCurrent(t *testing.T) {
	client := testClient(func(request *http.Request) (string, int) {
		return `{"tag_name":"v1.2.3","assets":[]}`, http.StatusOK
	})
	result, err := Run(context.Background(), Options{CurrentVersion: "1.2.3", APIURL: "https://test/latest", Client: client})
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated {
		t.Fatalf("unexpected update: %+v", result)
	}
}

func TestRunUsesWindowsZipRelease(t *testing.T) {
	binary := []byte("new windows rts binary")
	archive := testZipArchive(t, binary)
	sum := sha256.Sum256(archive)
	assetName := "rts_1.2.3_windows_amd64.zip"
	client := testClient(func(request *http.Request) (string, int) {
		switch request.URL.Path {
		case "/latest":
			return fmt.Sprintf(`{"tag_name":"v1.2.3","assets":[{"name":%q,"browser_download_url":"https://test/archive"},{"name":"checksums.txt","browser_download_url":"https://test/checksums"}]}`, assetName), http.StatusOK
		case "/archive":
			return string(archive), http.StatusOK
		case "/checksums":
			return fmt.Sprintf("%x  %s\n", sum, assetName), http.StatusOK
		default:
			return "not found", http.StatusNotFound
		}
	})
	executable := filepath.Join(t.TempDir(), "rts.exe")
	if err := os.WriteFile(executable, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), Options{
		CurrentVersion: "1.0.0", APIURL: "https://test/latest", GOOS: "windows", GOARCH: "amd64",
		Executable: executable, Client: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated {
		t.Fatalf("unexpected result: %+v", result)
	}
	got, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatalf("installed binary = %q, want %q", got, binary)
	}
}

func TestWindowsArchiveNameAndExtraction(t *testing.T) {
	if got, want := archiveName("1.2.3", "windows", "amd64"), "rts_1.2.3_windows_amd64.zip"; got != want {
		t.Fatalf("archive name = %q, want %q", got, want)
	}
	binary := []byte("windows rts binary")
	archive := testZipArchive(t, binary)
	got, err := extractBinary(archive, "windows")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatalf("extracted binary = %q, want %q", got, binary)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func testClient(response func(*http.Request) (string, int)) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, status := response(request)
		return &http.Response{
			StatusCode: status,
			Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}
}

func testArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gz)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "rts", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func testZipArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	file, err := writer.Create("rts.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
