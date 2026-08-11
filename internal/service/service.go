package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pmezard/go-difflib/difflib"

	"github.com/Inzaniak/rts/internal/core"
	"github.com/Inzaniak/rts/internal/fsx"
	"github.com/Inzaniak/rts/internal/store"
)

type Service struct {
	Registry   *core.Registry
	Store      *store.Store
	Executor   *fsx.Executor
	ConfigRoot string
}

type Filters struct {
	Harness core.Harness
	Kind    core.Kind
	Scope   core.Scope
	Query   string
}

type Drift struct {
	LinkID      string            `json:"linkId"`
	Source      core.Resource     `json:"source"`
	Targets     []core.Resource   `json:"targets"`
	Status      map[string]string `json:"status"`
	MissingKeys []string          `json:"missingKeys,omitempty"`
}

func Open(drivers []core.Driver) (*Service, error) {
	root := os.Getenv("RTS_CONFIG_HOME")
	if root == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(config, "rts")
	}
	state, err := store.Open(filepath.Join(root, "state.db"))
	if err != nil {
		return nil, err
	}
	return New(drivers, state, root), nil
}

func New(drivers []core.Driver, state *store.Store, configRoot string) *Service {
	return &Service{
		Registry:   core.NewRegistry(drivers...),
		Store:      state,
		Executor:   &fsx.Executor{BackupRoot: filepath.Join(configRoot, "backups")},
		ConfigRoot: configRoot,
	}
}

func (s *Service) Close() error { return s.Store.Close() }

func (s *Service) Inventory(ctx context.Context, project string, filters Filters) ([]core.Resource, error) {
	project, err := normalizeProject(project)
	if err != nil {
		return nil, err
	}
	var result []core.Resource
	for _, driver := range s.Registry.Drivers() {
		if filters.Harness != "" && driver.ID() != filters.Harness {
			continue
		}
		resources, discoverErr := driver.Discover(ctx, project)
		if discoverErr != nil {
			return nil, fmt.Errorf("discover %s: %w", driver.ID(), discoverErr)
		}
		for _, resource := range resources {
			if !resource.ReadOnly && resource.Has(core.CanDelete) && resource.Enabled == nil {
				enabled := true
				resource.Enabled = &enabled
				if !resource.Has(core.CanEnable) {
					resource.Capabilities = append(resource.Capabilities, core.CanEnable)
				}
			}
			result = append(result, resource)
		}
	}
	disabled, err := s.disabledResources(project)
	if err != nil {
		return nil, err
	}
	if len(disabled) > 0 {
		disabledByKey := make(map[string]int, len(disabled))
		for index := range disabled {
			disabledByKey[disabled[index].Key()] = index
		}
		active := result[:0]
		for _, resource := range result {
			if index, occupied := disabledByKey[resource.Key()]; occupied {
				disabled[index].Warnings = append(disabled[index].Warnings,
					"original location is occupied; re-enable will refuse to overwrite it")
				continue
			}
			active = append(active, resource)
		}
		result = active
	}
	result = append(result, disabled...)
	query := strings.ToLower(filters.Query)
	filtered := result[:0]
	for _, resource := range result {
		if filters.Harness != "" && resource.Harness != filters.Harness {
			continue
		}
		if filters.Kind != "" && resource.Kind != filters.Kind {
			continue
		}
		if filters.Scope != "" && resource.Scope != filters.Scope {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(resource.Name + " " + resource.Path + " " + string(resource.Kind))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		filtered = append(filtered, resource)
	}
	result = filtered
	sort.Slice(result, func(i, j int) bool {
		if result[i].Harness != result[j].Harness {
			return result[i].Harness < result[j].Harness
		}
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (s *Service) Installations(ctx context.Context) []core.Installation {
	var result []core.Installation
	for _, driver := range s.Registry.Drivers() {
		result = append(result, driver.Detect(ctx)...)
	}
	return result
}

func (s *Service) Find(ctx context.Context, project, idOrName string, filters Filters) (core.Resource, error) {
	resources, err := s.Inventory(ctx, project, filters)
	if err != nil {
		return core.Resource{}, err
	}
	var matches []core.Resource
	for _, resource := range resources {
		if resource.ID == idOrName || resource.Name == idOrName {
			matches = append(matches, resource)
		}
	}
	if len(matches) == 0 {
		return core.Resource{}, fmt.Errorf("resource %q was not found", idOrName)
	}
	if len(matches) > 1 {
		return core.Resource{}, fmt.Errorf("resource %q is ambiguous; use its ID or add harness/kind/scope filters", idOrName)
	}
	return matches[0], nil
}

func (s *Service) Read(resource core.Resource) ([]byte, error) {
	if isDisabledResource(resource) {
		return s.readDisabled(resource)
	}
	if resource.Kind == core.KindMCP && resource.Locator != "" {
		return core.PrettyJSON(resource.Metadata["config"]), nil
	}
	return os.ReadFile(EditablePath(resource))
}

// EditablePath returns the native file that should be opened to edit a
// resource. Directory resources such as skills expose their primary document
// through the mainFile metadata field.
func EditablePath(resource core.Resource) string {
	path := resource.Path
	if resource.Metadata != nil {
		if main, ok := resource.Metadata["mainFile"].(string); ok && main != "" {
			path = filepath.Join(path, filepath.FromSlash(main))
		}
	} else if resource.Format == "skill-directory" {
		path = filepath.Join(path, "SKILL.md")
	}
	return path
}

func (s *Service) PlanCreate(ctx context.Context, request core.Request) (core.ChangeSet, error) {
	driver, err := s.Registry.Driver(request.Harness)
	if err != nil {
		return core.ChangeSet{}, err
	}
	request.Project, err = normalizeProject(request.Project)
	if err != nil {
		return core.ChangeSet{}, err
	}
	return driver.PlanCreate(ctx, request)
}

func (s *Service) PlanUpdate(ctx context.Context, resource core.Resource, content []byte) (core.ChangeSet, error) {
	driver, err := s.Registry.Driver(resource.Harness)
	if err != nil {
		return core.ChangeSet{}, err
	}
	return driver.PlanUpdate(ctx, resource, content)
}

func (s *Service) PlanDelete(ctx context.Context, resource core.Resource) (core.ChangeSet, error) {
	driver, err := s.Registry.Driver(resource.Harness)
	if err != nil {
		return core.ChangeSet{}, err
	}
	return driver.PlanDelete(ctx, resource)
}

func (s *Service) PlanToggle(ctx context.Context, resource core.Resource, enabled bool) (core.ChangeSet, error) {
	if isDisabledResource(resource) {
		if !enabled {
			return core.ChangeSet{}, fmt.Errorf("resource is already disabled")
		}
		return s.planEnableStored(ctx, resource)
	}
	if !enabled {
		return s.planDisableStored(ctx, resource)
	}
	driver, err := s.Registry.Driver(resource.Harness)
	if err != nil {
		return core.ChangeSet{}, err
	}
	return driver.PlanEnable(ctx, resource)
}

func (s *Service) Doctor(ctx context.Context, project string, filters Filters) ([]core.Diagnostic, error) {
	resources, err := s.Inventory(ctx, project, filters)
	if err != nil {
		return nil, err
	}
	var diagnostics []core.Diagnostic
	for _, installation := range s.Installations(ctx) {
		if !installation.Detected {
			diagnostics = append(diagnostics, core.Diagnostic{
				Severity: "info", Harness: installation.Harness,
				Message: "harness executable and configuration were not detected",
			})
		}
		for _, warning := range installation.Warnings {
			diagnostics = append(diagnostics, core.Diagnostic{Severity: "warning", Harness: installation.Harness, Message: warning})
		}
	}
	for _, resource := range resources {
		driver, _ := s.Registry.Driver(resource.Harness)
		if !(resource.ReadOnly && len(resource.Warnings) > 0) {
			diagnostics = append(diagnostics, driver.Validate(ctx, resource)...)
		}
		for _, warning := range resource.Warnings {
			diagnostics = append(diagnostics, core.Diagnostic{
				Severity: "warning", Harness: resource.Harness, Resource: resource.ID, Message: warning,
			})
		}
	}
	return diagnostics, nil
}

func (s *Service) Diff(resource core.Resource, proposed []byte) (string, error) {
	current, err := s.Read(resource)
	if err != nil {
		return "", err
	}
	return difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A: difflib.SplitLines(string(current)), B: difflib.SplitLines(string(proposed)),
		FromFile: resource.Path, ToFile: resource.Path + " (proposed)", Context: 3,
	})
}

func (s *Service) Link(ctx context.Context, project, sourceID string, targetIDs []string) (store.Link, error) {
	resources, err := s.Inventory(ctx, project, Filters{})
	if err != nil {
		return store.Link{}, err
	}
	byID := map[string]core.Resource{}
	for _, resource := range resources {
		byID[resource.ID] = resource
	}
	source, ok := byID[sourceID]
	if !ok {
		return store.Link{}, fmt.Errorf("source resource %q was not found", sourceID)
	}
	link := store.Link{
		ID: uuid.NewString(), SourceKey: source.Key(), Fingerprints: map[string]string{source.Key(): source.Fingerprint},
	}
	for _, id := range targetIDs {
		target, ok := byID[id]
		if !ok {
			return store.Link{}, fmt.Errorf("target resource %q was not found", id)
		}
		if target.Kind != source.Kind {
			return store.Link{}, fmt.Errorf("cannot link %s to %s", source.Kind, target.Kind)
		}
		link.TargetKeys = append(link.TargetKeys, target.Key())
		link.Fingerprints[target.Key()] = target.Fingerprint
	}
	if err := s.Store.SaveLink(link); err != nil {
		return store.Link{}, err
	}
	return link, nil
}

func (s *Service) Drift(ctx context.Context, project string) ([]Drift, error) {
	resources, err := s.Inventory(ctx, project, Filters{})
	if err != nil {
		return nil, err
	}
	byKey := map[string]core.Resource{}
	for _, resource := range resources {
		byKey[resource.Key()] = resource
	}
	links, err := s.Store.Links()
	if err != nil {
		return nil, err
	}
	var result []Drift
	for _, link := range links {
		drift := Drift{LinkID: link.ID, Status: map[string]string{}}
		source, ok := byKey[link.SourceKey]
		if !ok {
			drift.MissingKeys = append(drift.MissingKeys, link.SourceKey)
		} else {
			drift.Source = source
			drift.Status[source.Key()] = fingerprintStatus(link.Fingerprints[source.Key()], source.Fingerprint)
		}
		for _, key := range link.TargetKeys {
			target, ok := byKey[key]
			if !ok {
				drift.MissingKeys = append(drift.MissingKeys, key)
				continue
			}
			drift.Targets = append(drift.Targets, target)
			drift.Status[key] = fingerprintStatus(link.Fingerprints[key], target.Fingerprint)
		}
		result = append(result, drift)
	}
	return result, nil
}

func (s *Service) Sync(ctx context.Context, project, linkID string, dryRun bool) ([]core.ChangeSet, []core.ApplyResult, error) {
	links, err := s.Store.Links()
	if err != nil {
		return nil, nil, err
	}
	var selected *store.Link
	for index := range links {
		if links[index].ID == linkID {
			selected = &links[index]
			break
		}
	}
	if selected == nil {
		return nil, nil, fmt.Errorf("link %q was not found", linkID)
	}
	resources, err := s.Inventory(ctx, project, Filters{})
	if err != nil {
		return nil, nil, err
	}
	byKey := map[string]core.Resource{}
	for _, resource := range resources {
		byKey[resource.Key()] = resource
	}
	source, ok := byKey[selected.SourceKey]
	if !ok {
		return nil, nil, errors.New("link source is missing")
	}
	content, err := s.Read(source)
	if err != nil {
		return nil, nil, err
	}
	var changes []core.ChangeSet
	var results []core.ApplyResult
	for _, key := range selected.TargetKeys {
		target, ok := byKey[key]
		if !ok {
			return changes, results, fmt.Errorf("link target is missing: %s", key)
		}
		if selected.Fingerprints[selected.SourceKey] != source.Fingerprint &&
			selected.Fingerprints[key] != target.Fingerprint {
			return changes, results, fmt.Errorf("both source and target %s changed; resolve the conflict before syncing", target.Name)
		}
		translated, translateErr := translatePortable(source, target, content)
		if translateErr != nil {
			return changes, results, translateErr
		}
		change, updateErr := s.PlanUpdate(ctx, target, translated)
		if updateErr != nil {
			return changes, results, updateErr
		}
		result, updateErr := s.apply(ctx, change, dryRun)
		if updateErr != nil {
			return changes, results, updateErr
		}
		changes = append(changes, change)
		results = append(results, result)
		if !dryRun {
			targetHash, _ := core.HashPath(target.Path)
			selected.Fingerprints[key] = targetHash
		}
	}
	if !dryRun {
		sourceHash, _ := core.HashPath(source.Path)
		selected.Fingerprints[selected.SourceKey] = sourceHash
		if err := s.Store.SaveLink(*selected); err != nil {
			return changes, results, err
		}
	}
	return changes, results, nil
}

func translatePortable(source, target core.Resource, content []byte) ([]byte, error) {
	if source.Kind != core.KindMCP {
		return content, nil
	}
	var entry map[string]any
	if err := json.Unmarshal(content, &entry); err != nil {
		return nil, fmt.Errorf("translate MCP %s: %w", source.Name, err)
	}
	if target.Harness == core.Antigravity {
		if url, ok := entry["url"]; ok {
			entry["serverUrl"] = url
			delete(entry, "url")
		}
	} else if serverURL, ok := entry["serverUrl"]; ok {
		entry["url"] = serverURL
		delete(entry, "serverUrl")
	}
	if target.Harness == core.OpenCode {
		if command, ok := entry["command"].(string); ok {
			commandLine := []any{command}
			if arguments, ok := entry["args"].([]any); ok {
				commandLine = append(commandLine, arguments...)
			}
			entry["command"] = commandLine
			delete(entry, "args")
			entry["type"] = "local"
			if env, ok := entry["env"]; ok {
				entry["environment"] = env
				delete(entry, "env")
			}
		} else if _, ok := entry["url"]; ok {
			entry["type"] = "remote"
		}
	} else if source.Harness == core.OpenCode {
		if commandLine, ok := entry["command"].([]any); ok && len(commandLine) > 0 {
			entry["command"] = fmt.Sprint(commandLine[0])
			if len(commandLine) > 1 {
				entry["args"] = commandLine[1:]
			}
		}
		if environment, ok := entry["environment"]; ok {
			entry["env"] = environment
			delete(entry, "environment")
		}
		delete(entry, "type")
	}
	delete(entry, "enabled")
	delete(entry, "disabled")
	return core.PrettyJSON(entry), nil
}

func (s *Service) PlanNativeLifecycle(harness core.Harness, action, spec string) (core.ChangeSet, error) {
	command, args, err := lifecycleCommand(harness, action, spec)
	if err != nil {
		return core.ChangeSet{}, err
	}
	change := core.ChangeSet{
		ID: uuid.NewString(), Summary: fmt.Sprintf("%s %s for %s", action, spec, harness), CreatedAt: time.Now().UTC(),
		Operations: []core.Operation{{Type: core.OpCommand, Command: command, Args: args, Description: strings.Join(append([]string{command}, args...), " ")}},
		Warnings:   []string{"native lifecycle commands may modify harness-managed caches or state outside RTS file backups"},
	}
	return change, nil
}

func (s *Service) Backups() ([]string, error) {
	entries, err := os.ReadDir(s.Executor.BackupRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []string
	for _, entry := range entries {
		if entry.IsDir() {
			result = append(result, filepath.Join(s.Executor.BackupRoot, entry.Name()))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(result)))
	return result, nil
}

func (s *Service) Restore(ctx context.Context, backup string) (core.ApplyResult, error) {
	if !filepath.IsAbs(backup) {
		backup = filepath.Join(s.Executor.BackupRoot, backup)
	}
	return s.Executor.Restore(ctx, backup)
}

func (s *Service) Apply(ctx context.Context, change core.ChangeSet) (core.ApplyResult, error) {
	return s.apply(ctx, change, false)
}

func (s *Service) apply(ctx context.Context, change core.ChangeSet, dryRun bool) (core.ApplyResult, error) {
	executor := *s.Executor
	executor.DryRun = dryRun
	return executor.Apply(ctx, change)
}

func normalizeProject(project string) (string, error) {
	if project == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(project)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project is not a directory: %s", absolute)
	}
	return absolute, nil
}

func fingerprintStatus(previous, current string) string {
	if previous == "" {
		return "untracked"
	}
	if previous == current {
		return "clean"
	}
	return "changed"
}

func lifecycleCommand(harness core.Harness, action, spec string) (string, []string, error) {
	command := string(harness)
	if _, err := exec.LookPath(command); err != nil {
		return "", nil, fmt.Errorf("%s executable was not found", command)
	}
	var verb string
	switch harness {
	case core.Claude, core.Grok, core.Copilot:
		if action == "install" || action == "update" || action == "uninstall" {
			verb = action
		}
	case core.Codex:
		verb = map[string]string{"install": "add", "update": "update", "uninstall": "remove"}[action]
	default:
		return "", nil, fmt.Errorf("%s does not expose a stable native plugin lifecycle command in this adapter", harness)
	}
	if verb == "" {
		return "", nil, fmt.Errorf("unsupported lifecycle action %q", action)
	}
	return command, []string{"plugin", verb, spec}, nil
}

func MarshalChange(change core.ChangeSet) []byte {
	type printableOperation struct {
		Type        core.OperationType `json:"type"`
		Path        string             `json:"path,omitempty"`
		Command     string             `json:"command,omitempty"`
		Args        []string           `json:"args,omitempty"`
		Description string             `json:"description"`
	}
	printable := struct {
		ID         string               `json:"id"`
		Summary    string               `json:"summary"`
		Operations []printableOperation `json:"operations"`
		Warnings   []string             `json:"warnings,omitempty"`
	}{ID: change.ID, Summary: change.Summary, Warnings: change.Warnings}
	for _, op := range change.Operations {
		printable.Operations = append(printable.Operations, printableOperation{
			Type: op.Type, Path: op.Path, Command: op.Command, Args: op.Args, Description: op.Description,
		})
	}
	b, _ := json.MarshalIndent(printable, "", "  ")
	return append(b, '\n')
}
