package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultAPIURL = "https://api.github.com/repos/Inzaniak/rts/releases/latest"
	maxArchive    = 200 << 20
	maxChecksums  = 5 << 20
)

type Options struct {
	CurrentVersion string
	APIURL         string
	GOOS           string
	GOARCH         string
	Executable     string
	Client         *http.Client
}

type Result struct {
	PreviousVersion string `json:"previousVersion"`
	Version         string `json:"version"`
	Updated         bool   `json:"updated"`
}

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func Run(ctx context.Context, options Options) (Result, error) {
	options = defaults(options)
	if options.GOOS == "windows" {
		return Result{}, errors.New("self-update is not supported on Windows; install the latest release manually")
	}

	latest, err := fetchRelease(ctx, options)
	if err != nil {
		return Result{}, err
	}
	result := Result{PreviousVersion: options.CurrentVersion, Version: latest.TagName}
	if sameVersion(options.CurrentVersion, latest.TagName) {
		return result, nil
	}

	version := strings.TrimPrefix(latest.TagName, "v")
	assetName := fmt.Sprintf("rts_%s_%s_%s.tar.gz", version, options.GOOS, options.GOARCH)
	archiveURL, checksumURL := assetURLs(latest, assetName)
	if archiveURL == "" {
		return Result{}, fmt.Errorf("release %s has no binary for %s/%s", latest.TagName, options.GOOS, options.GOARCH)
	}
	if checksumURL == "" {
		return Result{}, fmt.Errorf("release %s has no checksums.txt", latest.TagName)
	}

	checksums, err := download(ctx, options.Client, checksumURL, maxChecksums)
	if err != nil {
		return Result{}, fmt.Errorf("download checksums: %w", err)
	}
	want, err := checksumFor(checksums, assetName)
	if err != nil {
		return Result{}, err
	}
	archive, err := download(ctx, options.Client, archiveURL, maxArchive)
	if err != nil {
		return Result{}, fmt.Errorf("download %s: %w", assetName, err)
	}
	got := sha256.Sum256(archive)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return Result{}, fmt.Errorf("checksum mismatch for %s", assetName)
	}
	binary, err := extractBinary(archive, options.GOOS)
	if err != nil {
		return Result{}, err
	}
	if err := replace(options.Executable, binary); err != nil {
		return Result{}, err
	}
	result.Updated = true
	return result, nil
}

func defaults(options Options) Options {
	if options.APIURL == "" {
		options.APIURL = defaultAPIURL
	}
	if options.GOOS == "" {
		options.GOOS = runtime.GOOS
	}
	if options.GOARCH == "" {
		options.GOARCH = runtime.GOARCH
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 2 * time.Minute}
	}
	if options.Executable == "" {
		options.Executable, _ = os.Executable()
	}
	return options
}

func fetchRelease(ctx context.Context, options Options) (release, error) {
	body, err := download(ctx, options.Client, options.APIURL, 5<<20)
	if err != nil {
		return release{}, fmt.Errorf("check latest release: %w", err)
	}
	var value release
	if err := json.Unmarshal(body, &value); err != nil {
		return release{}, fmt.Errorf("decode latest release: %w", err)
	}
	if value.TagName == "" {
		return release{}, errors.New("latest release has no tag")
	}
	return value, nil
}

func download(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "rts-self-update")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("HTTP %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("download exceeds size limit")
	}
	return body, nil
}

func assetURLs(value release, assetName string) (string, string) {
	var archiveURL, checksumURL string
	for _, asset := range value.Assets {
		switch asset.Name {
		case assetName:
			archiveURL = asset.URL
		case "checksums.txt":
			checksumURL = asset.URL
		}
	}
	return archiveURL, checksumURL
}

func checksumFor(contents []byte, name string) (string, error) {
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			if len(fields[0]) != sha256.Size*2 {
				break
			}
			if _, err := hex.DecodeString(fields[0]); err != nil {
				break
			}
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no valid checksum for %s", name)
}

func extractBinary(contents []byte, goos string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(contents))
	if err != nil {
		return nil, fmt.Errorf("open release archive: %w", err)
	}
	defer gz.Close()
	want := "rts"
	if goos == "windows" {
		want += ".exe"
	}
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read release archive: %w", err)
		}
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == want {
			binary, err := io.ReadAll(io.LimitReader(reader, maxArchive+1))
			if err != nil {
				return nil, err
			}
			if len(binary) > maxArchive {
				return nil, errors.New("binary exceeds size limit")
			}
			return binary, nil
		}
	}
	return nil, fmt.Errorf("release archive does not contain %s", want)
}

func replace(path string, binary []byte) error {
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

func sameVersion(current, latest string) bool {
	return current != "" && current != "dev" && strings.TrimPrefix(current, "v") == strings.TrimPrefix(latest, "v")
}
