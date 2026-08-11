package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"rts/internal/core"
)

const (
	disabledManifestKey = "rtsDisabledManifest"
	disabledVersion     = 1
)

type disabledEntry struct {
	Version     int           `json:"version"`
	ID          string        `json:"id"`
	Resource    core.Resource `json:"resource"`
	StoragePath string        `json:"storagePath,omitempty"`
	Content     []byte        `json:"content,omitempty"`
	Embedded    bool          `json:"embedded,omitempty"`
	DisabledAt  time.Time     `json:"disabledAt"`
}

func (s *Service) disabledRoot() string {
	return filepath.Join(s.ConfigRoot, "disabled")
}

func (s *Service) disabledManifestPath(id string) string {
	return filepath.Join(s.disabledRoot(), "manifests", id+".json")
}

func (s *Service) disabledStoragePath(id string) string {
	return filepath.Join(s.disabledRoot(), "payloads", id)
}

func (s *Service) disableStored(ctx context.Context, resource core.Resource, dryRun bool) (core.ChangeSet, core.ApplyResult, error) {
	if resource.ReadOnly || !resource.Has(core.CanDelete) {
		return core.ChangeSet{}, core.ApplyResult{}, fmt.Errorf("resource cannot be disabled: %s", resource.Path)
	}
	id := resource.ID
	if id == "" {
		id = core.ResourceID(resource)
	}
	manifestPath := s.disabledManifestPath(id)
	if _, err := os.Lstat(manifestPath); err == nil {
		return core.ChangeSet{}, core.ApplyResult{}, fmt.Errorf("disabled entry already exists: %s", id)
	} else if !errors.Is(err, os.ErrNotExist) {
		return core.ChangeSet{}, core.ApplyResult{}, err
	}
	entry := disabledEntry{
		Version: disabledVersion, ID: id, Resource: resource, DisabledAt: time.Now().UTC(),
	}
	var operations []core.Operation
	if resource.Kind == core.KindMCP && resource.Locator != "" {
		content, err := s.Read(resource)
		if err != nil {
			return core.ChangeSet{}, core.ApplyResult{}, err
		}
		entry.Embedded = true
		entry.Content = content
		driver, err := s.Registry.Driver(resource.Harness)
		if err != nil {
			return core.ChangeSet{}, core.ApplyResult{}, err
		}
		remove, err := driver.PlanDelete(ctx, resource)
		if err != nil {
			return core.ChangeSet{}, core.ApplyResult{}, err
		}
		operations = append(operations, remove.Operations...)
	} else {
		if _, err := os.Lstat(resource.Path); err != nil {
			return core.ChangeSet{}, core.ApplyResult{}, err
		}
		entry.StoragePath = s.disabledStoragePath(id)
		if _, err := os.Lstat(entry.StoragePath); err == nil {
			return core.ChangeSet{}, core.ApplyResult{}, fmt.Errorf("disabled payload already exists: %s", entry.StoragePath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return core.ChangeSet{}, core.ApplyResult{}, err
		}
		operations = append(operations, core.Operation{
			Type: core.OpMove, Source: resource.Path, Path: entry.StoragePath,
			ExpectedHash: resource.Fingerprint, Description: "move resource into RTS disabled storage",
		})
	}
	manifest, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return core.ChangeSet{}, core.ApplyResult{}, err
	}
	operations = append([]core.Operation{{
		Type: core.OpWrite, Path: manifestPath, Content: append(manifest, '\n'), Mode: 0o600,
		Description: "record disabled resource references",
	}}, operations...)
	change := core.ChangeSet{
		ID: uuid.NewString(), Summary: "disable " + resource.Name,
		CreatedAt: time.Now().UTC(), Operations: operations,
	}
	result, err := s.apply(ctx, change, dryRun)
	return change, result, err
}

func (s *Service) enableStored(ctx context.Context, resource core.Resource, dryRun bool) (core.ChangeSet, core.ApplyResult, error) {
	manifestPath, _ := resource.Metadata[disabledManifestKey].(string)
	entry, err := s.loadDisabledEntry(manifestPath)
	if err != nil {
		return core.ChangeSet{}, core.ApplyResult{}, err
	}
	manifestHash, err := core.HashFile(manifestPath)
	if err != nil {
		return core.ChangeSet{}, core.ApplyResult{}, err
	}
	var operations []core.Operation
	if entry.Embedded {
		driver, err := s.Registry.Driver(entry.Resource.Harness)
		if err != nil {
			return core.ChangeSet{}, core.ApplyResult{}, err
		}
		resources, err := driver.Discover(ctx, entry.Resource.ProjectRoot)
		if err != nil {
			return core.ChangeSet{}, core.ApplyResult{}, err
		}
		for _, candidate := range resources {
			if candidate.Kind == entry.Resource.Kind &&
				candidate.Scope == entry.Resource.Scope &&
				filepath.Clean(candidate.Path) == filepath.Clean(entry.Resource.Path) &&
				candidate.Locator == entry.Resource.Locator {
				return core.ChangeSet{}, core.ApplyResult{}, fmt.Errorf(
					"cannot enable %s: original embedded entry already exists", entry.Resource.Name,
				)
			}
		}
		restore, err := driver.PlanUpdate(ctx, entry.Resource, entry.Content)
		if err != nil {
			return core.ChangeSet{}, core.ApplyResult{}, err
		}
		operations = append(operations, restore.Operations...)
	} else {
		if _, err := os.Lstat(entry.Resource.Path); err == nil {
			return core.ChangeSet{}, core.ApplyResult{}, fmt.Errorf("cannot enable %s: destination already exists: %s", entry.Resource.Name, entry.Resource.Path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return core.ChangeSet{}, core.ApplyResult{}, err
		}
		if _, err := os.Lstat(entry.StoragePath); err != nil {
			return core.ChangeSet{}, core.ApplyResult{}, fmt.Errorf("disabled payload is unavailable: %w", err)
		}
		storedHash, _ := core.HashPath(entry.StoragePath)
		operations = append(operations, core.Operation{
			Type: core.OpMove, Source: entry.StoragePath, Path: entry.Resource.Path,
			ExpectedHash: storedHash, Description: "restore resource from RTS disabled storage",
		})
	}
	operations = append(operations, core.Operation{
		Type: core.OpRemove, Path: manifestPath, ExpectedHash: manifestHash,
		Description: "remove disabled resource record",
	})
	change := core.ChangeSet{
		ID: uuid.NewString(), Summary: "enable " + entry.Resource.Name,
		CreatedAt: time.Now().UTC(), Operations: operations,
	}
	result, err := s.apply(ctx, change, dryRun)
	return change, result, err
}

func (s *Service) disabledResources(project string, filters Filters) ([]core.Resource, error) {
	matches, err := filepath.Glob(filepath.Join(s.disabledRoot(), "manifests", "*.json"))
	if err != nil {
		return nil, err
	}
	var result []core.Resource
	for _, manifestPath := range matches {
		entry, err := s.loadDisabledEntry(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("read disabled entry %s: %w", manifestPath, err)
		}
		resource := entry.Resource
		if (resource.Scope == core.ScopeProject || resource.Scope == core.ScopeLocal) && resource.ProjectRoot != project {
			continue
		}
		if filters.Harness != "" && resource.Harness != filters.Harness {
			continue
		}
		if filters.Kind != "" && resource.Kind != filters.Kind {
			continue
		}
		if filters.Scope != "" && resource.Scope != filters.Scope {
			continue
		}
		if filters.Query != "" {
			haystack := strings.ToLower(resource.Name + " " + resource.Path + " " + string(resource.Kind))
			if !strings.Contains(haystack, strings.ToLower(filters.Query)) {
				continue
			}
		}
		enabled := false
		resource.Enabled = &enabled
		resource.ReadOnly = false
		resource.Capabilities = []core.Capability{core.CanRead, core.CanEnable}
		resource.Metadata = cloneMetadata(resource.Metadata)
		resource.Metadata[disabledManifestKey] = manifestPath
		resource.Warnings = append(resource.Warnings, "disabled; content is stored by RTS")
		if !entry.Embedded {
			if _, err := os.Lstat(entry.StoragePath); err != nil {
				resource.Warnings = append(resource.Warnings, "disabled payload is missing")
			}
		}
		result = append(result, resource)
	}
	return result, nil
}

func (s *Service) readDisabled(resource core.Resource) ([]byte, error) {
	manifestPath, _ := resource.Metadata[disabledManifestKey].(string)
	entry, err := s.loadDisabledEntry(manifestPath)
	if err != nil {
		return nil, err
	}
	if entry.Embedded {
		return append([]byte(nil), entry.Content...), nil
	}
	stored := entry.Resource
	stored.Path = entry.StoragePath
	return s.Read(stored)
}

func (s *Service) loadDisabledEntry(manifestPath string) (disabledEntry, error) {
	var entry disabledEntry
	if manifestPath == "" {
		return entry, errors.New("disabled resource manifest is missing")
	}
	if !pathWithin(filepath.Join(s.disabledRoot(), "manifests"), manifestPath) {
		return entry, errors.New("disabled resource manifest is outside RTS storage")
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return entry, err
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return entry, err
	}
	if entry.Version != disabledVersion || entry.ID == "" || entry.Resource.Path == "" {
		return entry, errors.New("invalid disabled resource manifest")
	}
	if !entry.Embedded && !pathWithin(filepath.Join(s.disabledRoot(), "payloads"), entry.StoragePath) {
		return entry, errors.New("disabled payload is outside RTS storage")
	}
	return entry, nil
}

func isDisabledResource(resource core.Resource) bool {
	if resource.Metadata == nil {
		return false
	}
	manifest, _ := resource.Metadata[disabledManifestKey].(string)
	return manifest != ""
}

func cloneMetadata(metadata map[string]any) map[string]any {
	result := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		result[key] = value
	}
	return result
}

func pathWithin(root, path string) bool {
	root, rootErr := filepath.Abs(root)
	path, pathErr := filepath.Abs(path)
	if rootErr != nil || pathErr != nil {
		return false
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
