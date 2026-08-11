package adapters

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	toml "github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"

	"rts/internal/core"
	"rts/internal/documents"
)

type location struct {
	Kind       core.Kind
	Scope      core.Scope
	Path       string
	Glob       string
	DirMain    string
	Recursive  bool
	Format     string
	ReadOnly   bool
	Origin     string
	NamePrefix string
	Surface    string
	Legacy     bool
}

type mcpLocation struct {
	Scope      core.Scope
	Path       string
	Format     string
	JSONPath   []string
	TOMLPrefix string
	DisableKey string
	EnableKey  string
	Surface    string
	ReadOnly   bool
}

type genericDriver struct {
	id          core.Harness
	displayName string
	commands    []string
	docs        []string
	locations   func(project string) []location
	mcp         func(project string) []mcpLocation
}

func (d *genericDriver) ID() core.Harness    { return d.id }
func (d *genericDriver) DisplayName() string { return d.displayName }
func (d *genericDriver) Docs() []string      { return append([]string(nil), d.docs...) }

func (d *genericDriver) Detect(ctx context.Context) []core.Installation {
	var result []core.Installation
	for _, command := range d.commands {
		path, err := exec.LookPath(command)
		if err != nil {
			continue
		}
		version := detectVersion(ctx, path)
		result = append(result, core.Installation{
			Harness: d.id, Surface: command, Version: version, Executable: path, Detected: true,
		})
	}
	for _, location := range d.locations("") {
		if location.Scope != core.ScopeUser {
			continue
		}
		if _, err := os.Stat(location.Path); err == nil {
			found := false
			for _, installation := range result {
				if installation.Surface == location.Surface {
					found = true
				}
			}
			if !found {
				result = append(result, core.Installation{
					Harness: d.id, Surface: location.Surface, ConfigHome: inferConfigHome(location.Path),
					Detected: true, Warnings: []string{"configuration found but executable was not detected on PATH"},
				})
			}
		}
	}
	if len(result) == 0 {
		result = append(result, core.Installation{Harness: d.id, Surface: d.displayName, Detected: false})
	}
	return result
}

func (d *genericDriver) Discover(ctx context.Context, project string) ([]core.Resource, error) {
	_ = ctx
	var result []core.Resource
	for _, loc := range d.locations(project) {
		resources, err := discoverLocation(d.id, project, loc)
		if err != nil {
			return nil, err
		}
		result = append(result, resources...)
	}
	for _, loc := range d.mcp(project) {
		resources, err := discoverMCP(d.id, project, loc)
		if err != nil {
			result = append(result, core.Resource{
				Harness: d.id, Surface: loc.Surface, Kind: core.KindMCP, Scope: loc.Scope,
				ProjectRoot: project, Name: filepath.Base(loc.Path), Path: loc.Path, Format: loc.Format,
				ReadOnly: true, Capabilities: []core.Capability{core.CanRead},
				Warnings: []string{err.Error()},
			})
			continue
		}
		result = append(result, resources...)
	}
	for index := range result {
		result[index].ID = core.ResourceID(result[index])
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		if result[i].Scope != result[j].Scope {
			return result[i].Scope < result[j].Scope
		}
		return result[i].Name < result[j].Name
	})
	return dedupeResources(result), nil
}

func (d *genericDriver) PlanCreate(ctx context.Context, request core.Request) (core.ChangeSet, error) {
	_ = ctx
	if request.Kind == core.KindMCP {
		return d.planCreateMCP(request)
	}
	var selected *location
	for _, loc := range d.locations(request.Project) {
		if loc.Kind == request.Kind && loc.Scope == request.Scope && !loc.ReadOnly && !loc.Legacy {
			selected = &loc
			break
		}
	}
	if selected == nil {
		return core.ChangeSet{}, fmt.Errorf("%s adapter cannot create %s resources at %s scope", d.id, request.Kind, request.Scope)
	}
	target, err := targetFor(*selected, request.Name)
	if err != nil {
		return core.ChangeSet{}, err
	}
	if _, err := os.Lstat(target); err == nil && !request.Force {
		return core.ChangeSet{}, fmt.Errorf("target already exists: %s", target)
	}
	content, err := creationContent(request, *selected)
	if err != nil {
		return core.ChangeSet{}, err
	}
	return core.ChangeSet{
		ID: uuid.NewString(), Summary: fmt.Sprintf("create %s %s for %s", request.Kind, request.Name, d.id),
		CreatedAt: time.Now().UTC(),
		Operations: []core.Operation{{
			Type: core.OpWrite, Path: target, Content: content, Mode: 0o644,
			Description: "create " + target,
		}},
	}, nil
}

func (d *genericDriver) PlanUpdate(ctx context.Context, resource core.Resource, content []byte) (core.ChangeSet, error) {
	_ = ctx
	if resource.ReadOnly || !resource.Has(core.CanUpdate) {
		return core.ChangeSet{}, fmt.Errorf("resource is read-only: %s", resource.Path)
	}
	target := resourceMainPath(resource)
	var updated []byte
	var err error
	if resource.Kind == core.KindMCP && resource.Locator != "" {
		updated, err = d.updateMCP(resource, content, false, false)
	} else {
		updated = content
	}
	if err != nil {
		return core.ChangeSet{}, err
	}
	hash, err := core.HashPath(resource.Path)
	if target != resource.Path {
		hash, err = core.HashPath(target)
	}
	if err != nil {
		return core.ChangeSet{}, err
	}
	return core.ChangeSet{
		ID: uuid.NewString(), Summary: fmt.Sprintf("update %s %s for %s", resource.Kind, resource.Name, d.id),
		CreatedAt: time.Now().UTC(),
		Operations: []core.Operation{{
			Type: core.OpWrite, Path: target, Content: updated, ExpectedHash: hash,
			Description: "update " + target,
		}},
	}, nil
}

func (d *genericDriver) PlanDelete(ctx context.Context, resource core.Resource) (core.ChangeSet, error) {
	_ = ctx
	if resource.ReadOnly || !resource.Has(core.CanDelete) {
		return core.ChangeSet{}, fmt.Errorf("resource is read-only: %s", resource.Path)
	}
	hash, err := core.HashPath(resource.Path)
	if err != nil {
		return core.ChangeSet{}, err
	}
	if resource.Kind == core.KindMCP && resource.Locator != "" {
		updated, updateErr := d.updateMCP(resource, nil, true, false)
		if updateErr != nil {
			return core.ChangeSet{}, updateErr
		}
		return core.ChangeSet{
			ID: uuid.NewString(), Summary: fmt.Sprintf("remove MCP server %s from %s", resource.Name, d.id),
			CreatedAt: time.Now().UTC(),
			Operations: []core.Operation{{
				Type: core.OpWrite, Path: resource.Path, Content: updated, ExpectedHash: hash,
				Description: "remove MCP entry from " + resource.Path,
			}},
		}, nil
	}
	return core.ChangeSet{
		ID: uuid.NewString(), Summary: fmt.Sprintf("remove %s %s from %s", resource.Kind, resource.Name, d.id),
		CreatedAt: time.Now().UTC(),
		Operations: []core.Operation{{
			Type: core.OpRemove, Path: resource.Path, ExpectedHash: hash, Description: "remove " + resource.Path,
		}},
	}, nil
}

func (d *genericDriver) PlanToggle(ctx context.Context, resource core.Resource, enabled bool) (core.ChangeSet, error) {
	_ = ctx
	if resource.Kind != core.KindMCP || resource.Locator == "" {
		return core.ChangeSet{}, fmt.Errorf("enable/disable is currently supported for MCP resources")
	}
	if resource.ReadOnly || !resource.Has(core.CanEnable) {
		return core.ChangeSet{}, fmt.Errorf("resource cannot be enabled or disabled")
	}
	updated, err := d.updateMCP(resource, nil, false, enabled)
	if err != nil {
		return core.ChangeSet{}, err
	}
	hash, err := core.HashPath(resource.Path)
	if err != nil {
		return core.ChangeSet{}, err
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	return core.ChangeSet{
		ID: uuid.NewString(), Summary: action + " MCP server " + resource.Name,
		CreatedAt: time.Now().UTC(),
		Operations: []core.Operation{{
			Type: core.OpWrite, Path: resource.Path, Content: updated, ExpectedHash: hash,
			Description: action + " entry in " + resource.Path,
		}},
	}, nil
}

func (d *genericDriver) Validate(ctx context.Context, resource core.Resource) []core.Diagnostic {
	_ = ctx
	var result []core.Diagnostic
	info, err := os.Lstat(resource.Path)
	if err != nil {
		return []core.Diagnostic{{Severity: "error", Harness: d.id, Resource: resource.ID, Message: err.Error()}}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		result = append(result, core.Diagnostic{
			Severity: "warning", Harness: d.id, Resource: resource.ID,
			Message: "resource is a symbolic link", Hint: "RTS will not replace symlink targets transactionally",
		})
	}
	target := resource.Path
	if info.IsDir() {
		target = resourceMainPath(resource)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		return append(result, core.Diagnostic{Severity: "error", Harness: d.id, Resource: resource.ID, Message: err.Error()})
	}
	switch resource.Format {
	case "json", "jsonc", "mcp-json":
		var value any
		if err := documents.DecodeJSONC(content, &value); err != nil {
			result = append(result, core.Diagnostic{Severity: "error", Harness: d.id, Resource: resource.ID, Message: err.Error()})
		}
	case "skill-directory":
		if diagnostic := validateSkill(content, resource); diagnostic != nil {
			result = append(result, *diagnostic)
		}
	}
	return result
}

func (d *genericDriver) planCreateMCP(request core.Request) (core.ChangeSet, error) {
	var selected *mcpLocation
	for _, loc := range d.mcp(request.Project) {
		if loc.Scope == request.Scope && !loc.ReadOnly {
			selected = &loc
			break
		}
	}
	if selected == nil {
		return core.ChangeSet{}, fmt.Errorf("%s adapter cannot create MCP resources at %s scope", d.id, request.Scope)
	}
	entry, err := parseMCPContent(request.Content)
	if err != nil {
		return core.ChangeSet{}, err
	}
	raw, err := os.ReadFile(selected.Path)
	if errors.Is(err, os.ErrNotExist) {
		raw = nil
	} else if err != nil {
		return core.ChangeSet{}, err
	}
	var updated []byte
	if selected.Format == "toml" {
		updated, err = documents.SetTOMLTable(raw, selected.TOMLPrefix, request.Name, entry)
	} else {
		updated, err = documents.SetJSONEntry(raw, selected.JSONPath, request.Name, entry)
	}
	if err != nil {
		return core.ChangeSet{}, err
	}
	var expected string
	if len(raw) > 0 {
		expected = core.HashBytes(raw)
	}
	return core.ChangeSet{
		ID: uuid.NewString(), Summary: fmt.Sprintf("add MCP server %s to %s", request.Name, d.id),
		CreatedAt: time.Now().UTC(),
		Operations: []core.Operation{{
			Type: core.OpWrite, Path: selected.Path, Content: updated, ExpectedHash: expected,
			Description: "add MCP entry to " + selected.Path,
		}},
	}, nil
}

func (d *genericDriver) updateMCP(resource core.Resource, content []byte, remove, enabled bool) ([]byte, error) {
	raw, err := os.ReadFile(resource.Path)
	if err != nil {
		return nil, err
	}
	loc := d.findMCP(resource)
	if loc == nil {
		return nil, fmt.Errorf("MCP location is no longer recognized")
	}
	if remove {
		if loc.Format == "toml" {
			return documents.DeleteTOMLTable(raw, loc.TOMLPrefix, resource.Locator)
		}
		return documents.DeleteJSONEntry(raw, loc.JSONPath, resource.Locator)
	}
	var entry map[string]any
	if content != nil {
		entry, err = parseMCPContent(content)
	} else {
		entry, err = readMCPEntry(raw, *loc, resource.Locator)
		if err == nil {
			if loc.DisableKey != "" {
				entry[loc.DisableKey] = !enabled
			}
			if loc.EnableKey != "" {
				entry[loc.EnableKey] = enabled
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if loc.Format == "toml" {
		return documents.SetTOMLTable(raw, loc.TOMLPrefix, resource.Locator, entry)
	}
	return documents.SetJSONEntry(raw, loc.JSONPath, resource.Locator, entry)
}

func (d *genericDriver) findMCP(resource core.Resource) *mcpLocation {
	for _, loc := range d.mcp(resource.ProjectRoot) {
		if filepath.Clean(loc.Path) == filepath.Clean(resource.Path) && loc.Scope == resource.Scope {
			return &loc
		}
	}
	return nil
}

func discoverLocation(harness core.Harness, project string, loc location) ([]core.Resource, error) {
	var paths []string
	if loc.DirMain != "" {
		if loc.Recursive {
			err := filepath.WalkDir(loc.Path, func(path string, entry os.DirEntry, walkErr error) error {
				if errors.Is(walkErr, os.ErrNotExist) {
					return nil
				}
				if walkErr != nil {
					return walkErr
				}
				if entry.Type()&os.ModeSymlink != 0 {
					target, err := os.Stat(path)
					if err == nil && target.IsDir() {
						if _, err := os.Stat(filepath.Join(path, loc.DirMain)); err == nil {
							paths = append(paths, path)
						}
					}
					return nil
				}
				if path == loc.Path || !entry.IsDir() {
					return nil
				}
				if strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
				if _, err := os.Stat(filepath.Join(path, loc.DirMain)); err == nil {
					paths = append(paths, path)
					return filepath.SkipDir
				}
				return nil
			})
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
		} else {
			entries, err := os.ReadDir(loc.Path)
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				path := filepath.Join(loc.Path, entry.Name())
				info, err := os.Stat(path)
				if err == nil && info.IsDir() {
					if _, err := os.Stat(filepath.Join(path, loc.DirMain)); err == nil {
						paths = append(paths, path)
					}
				}
			}
		}
	} else if loc.Glob != "" {
		matches, err := filepath.Glob(filepath.Join(loc.Path, loc.Glob))
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			info, statErr := os.Lstat(match)
			if statErr == nil && !info.IsDir() {
				paths = append(paths, match)
			}
		}
	} else if _, err := os.Lstat(loc.Path); err == nil {
		paths = []string{loc.Path}
	}
	var result []core.Resource
	for _, path := range paths {
		name := resourceName(path, loc)
		hash, err := core.HashPath(path)
		if err != nil {
			hash = ""
		}
		capabilities := []core.Capability{core.CanRead}
		if !loc.ReadOnly {
			capabilities = append(capabilities, core.CanCreate, core.CanUpdate, core.CanDelete)
		}
		warnings := []string(nil)
		if loc.Legacy {
			warnings = append(warnings, "legacy location; new resources use the current canonical path")
		}
		resource := core.Resource{
			Harness: harness, Surface: loc.Surface, Kind: loc.Kind, Scope: loc.Scope,
			ProjectRoot: project, Name: name, Path: path, Format: loc.Format, Origin: loc.Origin,
			Fingerprint: hash, ReadOnly: loc.ReadOnly, Capabilities: capabilities, Warnings: warnings,
		}
		if loc.DirMain != "" {
			resource.Metadata = map[string]any{"mainFile": loc.DirMain}
		}
		result = append(result, resource)
	}
	return result, nil
}

func discoverMCP(harness core.Harness, project string, loc mcpLocation) ([]core.Resource, error) {
	raw, err := os.ReadFile(loc.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries map[string]any
	if loc.Format == "toml" {
		var root map[string]any
		if err := toml.Unmarshal(raw, &root); err != nil {
			return nil, fmt.Errorf("parse TOML: %w", err)
		}
		value := any(root)
		for _, segment := range strings.Split(loc.TOMLPrefix, ".") {
			object, ok := value.(map[string]any)
			if !ok {
				value = nil
				break
			}
			next, exists := object[segment]
			if !exists {
				return nil, nil
			}
			value = next
		}
		entries, _ = value.(map[string]any)
		if entries == nil {
			entries = map[string]any{}
		}
	} else {
		var root map[string]any
		if err := documents.DecodeJSONC(raw, &root); err != nil {
			return nil, err
		}
		value := any(root)
		for _, segment := range loc.JSONPath {
			object, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s is not an object", strings.Join(loc.JSONPath, "."))
			}
			next, exists := object[segment]
			if !exists {
				return nil, nil
			}
			value = next
		}
		entries, _ = value.(map[string]any)
	}
	hash := core.HashBytes(raw)
	var result []core.Resource
	for name, value := range entries {
		enabled := true
		if object, ok := value.(map[string]any); ok {
			if loc.DisableKey != "" {
				if disabled, ok := object[loc.DisableKey].(bool); ok {
					enabled = !disabled
				}
			}
			if loc.EnableKey != "" {
				if configured, ok := object[loc.EnableKey].(bool); ok {
					enabled = configured
				}
			}
		}
		capabilities := []core.Capability{core.CanRead}
		if !loc.ReadOnly {
			capabilities = append(capabilities, core.CanCreate, core.CanUpdate, core.CanDelete, core.CanEnable)
		}
		result = append(result, core.Resource{
			Harness: harness, Surface: loc.Surface, Kind: core.KindMCP, Scope: loc.Scope,
			ProjectRoot: project, Name: name, Path: loc.Path, Locator: name, Format: loc.Format,
			Fingerprint: hash, Enabled: &enabled, ReadOnly: loc.ReadOnly, Capabilities: capabilities,
			Metadata: map[string]any{"config": value},
		})
	}
	return result, nil
}

func readMCPEntry(raw []byte, loc mcpLocation, name string) (map[string]any, error) {
	if loc.Format == "toml" {
		var root map[string]any
		if err := toml.Unmarshal(raw, &root); err != nil {
			return nil, fmt.Errorf("parse TOML: %w", err)
		}
		value := any(root)
		for _, segment := range strings.Split(loc.TOMLPrefix, ".") {
			object, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("TOML path %s is not an object", loc.TOMLPrefix)
			}
			value = object[segment]
		}
		entries, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("TOML path %s is not an object", loc.TOMLPrefix)
		}
		entry, ok := entries[name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("MCP entry %q is not an object", name)
		}
		return entry, nil
	}
	var root map[string]any
	if err := documents.DecodeJSONC(raw, &root); err != nil {
		return nil, err
	}
	value := any(root)
	for _, segment := range loc.JSONPath {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s is not an object", strings.Join(loc.JSONPath, "."))
		}
		value = object[segment]
	}
	entries, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("MCP entries are not an object")
	}
	entry, ok := entries[name].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("MCP entry %q is not an object", name)
	}
	return entry, nil
}

func parseMCPContent(content []byte) (map[string]any, error) {
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil, errors.New("MCP configuration JSON is required")
	}
	var entry map[string]any
	if err := documents.DecodeJSONC(content, &entry); err != nil {
		return nil, err
	}
	if _, command := entry["command"]; !command {
		if _, url := entry["url"]; !url {
			if _, serverURL := entry["serverUrl"]; !serverURL {
				return nil, errors.New("MCP entry must contain command, url, or serverUrl")
			}
		}
	}
	return entry, nil
}

func creationContent(request core.Request, loc location) ([]byte, error) {
	if len(request.Content) > 0 {
		return request.Content, nil
	}
	if request.File != "" {
		return os.ReadFile(request.File)
	}
	switch loc.Kind {
	case core.KindSkill:
		return []byte("---\nname: " + request.Name + "\ndescription: Describe when this skill should be used.\n---\n\n# " + request.Name + "\n\nAdd instructions here.\n"), nil
	case core.KindAgent:
		return []byte("---\nname: " + request.Name + "\ndescription: Describe when this agent should be used.\n---\n\n# Instructions\n"), nil
	case core.KindRule, core.KindInstructions, core.KindCommand, core.KindWorkflow, core.KindOutputStyle:
		return []byte("# " + request.Name + "\n\n"), nil
	case core.KindSettings, core.KindHook, core.KindKeybindings, core.KindTheme, core.KindLSP:
		return []byte("{}\n"), nil
	default:
		return nil, errors.New("content or --file is required for this resource kind")
	}
}

func targetFor(loc location, name string) (string, error) {
	if loc.DirMain != "" {
		if err := safeName(name); err != nil {
			return "", err
		}
		return filepath.Join(loc.Path, name, loc.DirMain), nil
	}
	if loc.Glob != "" {
		if err := safeName(name); err != nil {
			return "", err
		}
		ext := filepath.Ext(loc.Glob)
		if ext == "" || ext == ".*" {
			ext = ""
		}
		if ext != "" && !strings.HasSuffix(name, ext) {
			name += ext
		}
		return filepath.Join(loc.Path, name), nil
	}
	return loc.Path, nil
}

func safeName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid resource name %q", name)
	}
	return nil
}

func resourceName(path string, loc location) string {
	if loc.DirMain != "" {
		return loc.NamePrefix + filepath.Base(path)
	}
	if loc.Glob != "" {
		return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return filepath.Base(path)
}

func resourceMainPath(resource core.Resource) string {
	if resource.Metadata != nil {
		if main, ok := resource.Metadata["mainFile"].(string); ok && main != "" {
			return filepath.Join(resource.Path, filepath.FromSlash(main))
		}
	}
	if resource.Kind == core.KindSkill && resource.Format == "skill-directory" {
		return filepath.Join(resource.Path, "SKILL.md")
	}
	return resource.Path
}

func validateSkill(content []byte, resource core.Resource) *core.Diagnostic {
	text := string(content)
	if !strings.HasPrefix(text, "---\n") {
		return &core.Diagnostic{Severity: "error", Harness: resource.Harness, Resource: resource.ID, Message: "SKILL.md is missing YAML frontmatter"}
	}
	rest := strings.TrimPrefix(text, "---\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return &core.Diagnostic{Severity: "error", Harness: resource.Harness, Resource: resource.ID, Message: "SKILL.md frontmatter is not closed"}
	}
	var metadata map[string]any
	if err := yaml.Unmarshal([]byte(rest[:end]), &metadata); err != nil {
		return &core.Diagnostic{Severity: "error", Harness: resource.Harness, Resource: resource.ID, Message: "invalid skill frontmatter: " + err.Error()}
	}
	if strings.TrimSpace(fmt.Sprint(metadata["description"])) == "" {
		return &core.Diagnostic{Severity: "error", Harness: resource.Harness, Resource: resource.ID, Message: "skill description is required"}
	}
	return nil
}

func detectVersion(ctx context.Context, executable string) string {
	for _, args := range [][]string{{"--version"}, {"version"}} {
		command := exec.CommandContext(ctx, executable, args...)
		output, err := command.Output()
		if err == nil {
			return strings.TrimSpace(string(output))
		}
	}
	return ""
}

func inferConfigHome(path string) string {
	for {
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") || base == "opencode" {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}

func dedupeResources(resources []core.Resource) []core.Resource {
	seen := map[string]bool{}
	result := resources[:0]
	for _, resource := range resources {
		key := resource.Key()
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, resource)
	}
	return result
}

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func osManagedPath(app, file string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join("/Library/Application Support", app, file)
	case "windows":
		return filepath.Join(os.Getenv("ProgramData"), app, file)
	default:
		return filepath.Join("/etc", strings.ToLower(app), file)
	}
}
