package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Inzaniak/rts/internal/adapters"
	"github.com/Inzaniak/rts/internal/buildinfo"
	"github.com/Inzaniak/rts/internal/core"
	"github.com/Inzaniak/rts/internal/editor"
	"github.com/Inzaniak/rts/internal/selfupdate"
	"github.com/Inzaniak/rts/internal/service"
	"github.com/Inzaniak/rts/internal/store"
	"github.com/Inzaniak/rts/internal/tui"
)

type app struct {
	service *service.Service
	project string
	json    bool
	dryRun  bool
	yes     bool
	noColor bool
	harness string
	kind    string
	scope   string
	query   string
}

type mutation func() (core.ChangeSet, error)

func Execute() error {
	svc, err := service.Open(adapters.All())
	if err != nil {
		return err
	}
	defer svc.Close()
	a := &app{service: svc}
	root := a.rootCommand()
	return root.Execute()
}

func (a *app) rootCommand() *cobra.Command {
	command := &cobra.Command{
		Use:           "rts [project]",
		Short:         "Manage configuration across AI coding harnesses",
		Version:       buildinfo.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project := a.project
			if len(args) == 1 {
				project = args[0]
			}
			return tui.Run(a.service, project, a.noColor)
		},
	}
	command.SetVersionTemplate("rts {{.Version}}\n")
	flags := command.PersistentFlags()
	flags.StringVarP(&a.project, "project", "p", "", "project root (defaults to no project-scoped discovery)")
	flags.BoolVar(&a.json, "json", false, "emit stable JSON output")
	flags.BoolVar(&a.dryRun, "dry-run", false, "preview without changing files")
	flags.BoolVarP(&a.yes, "yes", "y", false, "apply without an interactive confirmation")
	flags.BoolVar(&a.noColor, "no-color", false, "disable color in the TUI")
	flags.StringVar(&a.harness, "harness", "", "filter by harness")
	flags.StringVar(&a.kind, "kind", "", "filter by resource kind")
	flags.StringVar(&a.scope, "scope", "", "filter by scope")
	flags.StringVarP(&a.query, "query", "q", "", "search resource name and path")

	command.AddCommand(
		a.listCommand(), a.getCommand(), a.addCommand(), a.editCommand(), a.removeCommand(),
		a.toggleCommand(true), a.toggleCommand(false), a.diffCommand(), a.doctorCommand(),
		a.linkCommand(), a.unlinkCommand(), a.syncCommand(),
		a.projectCommand(), a.backupCommand(), a.adapterCommand(),
		a.lifecycleCommand("install"), a.updateCommand(), a.lifecycleCommand("uninstall"),
	)
	return command
}

func (a *app) listCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list [kind]",
		Short: "List discovered resources",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filters, err := a.filters()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				filters.Kind, err = core.ParseKind(args[0])
				if err != nil {
					return err
				}
			}
			resources, err := a.service.Inventory(a.project, filters)
			if err != nil {
				return err
			}
			if a.json {
				return writeJSON(cmd.OutOrStdout(), core.NewEnvelope(service.RedactResources(resources)))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-21s %-11s %-15s %-9s %-24s %s\n", "ID", "HARNESS", "KIND", "SCOPE", "NAME", "PATH")
			for _, resource := range resources {
				fmt.Fprintf(cmd.OutOrStdout(), "%-21s %-11s %-15s %-9s %-24s %s\n",
					resource.ID, resource.Harness, resource.Kind, resource.Scope, truncate(resource.Name, 24), resource.Path)
			}
			return nil
		},
	}
}

func (a *app) getCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id-or-name>",
		Short: "Read one resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resource, err := a.find(args[0])
			if err != nil {
				return err
			}
			content, err := a.service.ReadRedacted(resource)
			if err != nil {
				return err
			}
			if a.json {
				return writeJSON(cmd.OutOrStdout(), core.NewEnvelope(map[string]any{
					"resource": service.RedactResource(resource), "content": string(content),
				}))
			}
			_, err = cmd.OutOrStdout().Write(content)
			return err
		},
	}
}

func (a *app) addCommand() *cobra.Command {
	var content, file, commandName, url string
	var args, env, headers []string
	var force bool
	cmd := &cobra.Command{
		Use:   "add <kind> <name>",
		Short: "Create a resource",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, positional []string) error {
			if a.harness == "" {
				return errors.New("--harness is required")
			}
			harness, err := core.ParseHarness(a.harness)
			if err != nil {
				return err
			}
			kind, err := core.ParseKind(positional[0])
			if err != nil {
				return err
			}
			scope := core.ScopeUser
			if a.scope != "" {
				scope, err = core.ParseScope(a.scope)
				if err != nil {
					return err
				}
			} else if a.project != "" {
				scope = core.ScopeProject
			}
			body := []byte(content)
			if file != "" {
				body, err = os.ReadFile(file)
				if err != nil {
					return err
				}
			}
			if kind == core.KindMCP && len(body) == 0 {
				entry := map[string]any{}
				if commandName != "" {
					entry["command"] = commandName
					entry["args"] = args
					if len(env) > 0 {
						entry["env"] = keyValues(env)
					}
				} else if url != "" {
					if harness == core.Antigravity {
						entry["serverUrl"] = url
					} else {
						entry["url"] = url
						entry["type"] = "remote"
					}
					if len(headers) > 0 {
						entry["headers"] = keyValues(headers)
					}
				} else {
					return errors.New("MCP creation requires --command, --url, --content, or --file")
				}
				body = core.PrettyJSON(entry)
			}
			request := core.Request{
				Harness: harness, Kind: kind, Scope: scope, Name: positional[1],
				Project: a.project, Content: body, Force: force,
			}
			return a.runMutation(cmd, func() (core.ChangeSet, error) {
				return a.service.PlanCreate(request)
			})
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&content, "content", "", "literal resource content")
	flags.StringVarP(&file, "file", "f", "", "read resource content from a file")
	flags.StringVar(&commandName, "command", "", "MCP stdio command")
	flags.StringSliceVar(&args, "arg", nil, "MCP command argument (repeatable or comma-separated)")
	flags.StringSliceVar(&env, "env", nil, "MCP environment KEY=VALUE")
	flags.StringVar(&url, "url", "", "MCP remote URL")
	flags.StringSliceVar(&headers, "header", nil, "MCP header KEY=VALUE")
	flags.BoolVar(&force, "force", false, "replace an existing target")
	return cmd
}

func (a *app) editCommand() *cobra.Command {
	var content, file string
	cmd := &cobra.Command{
		Use:   "edit <id-or-name>",
		Short: "Edit a resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resource, err := a.find(args[0])
			if err != nil {
				return err
			}
			if file == "" && content == "" {
				if a.dryRun {
					return errors.New("--dry-run cannot be used with direct external editing; use --content or --file")
				}
				if resource.ReadOnly || !resource.Has(core.CanUpdate) {
					return errors.New("resource is read-only")
				}
				path, err := editExternally(resource)
				if err != nil {
					return err
				}
				if a.json {
					return writeJSON(cmd.OutOrStdout(), core.NewEnvelope(map[string]string{"path": path}))
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Edited %s\n", path)
				return nil
			}
			body := []byte(content)
			if file != "" {
				body, err = os.ReadFile(file)
			}
			if err != nil {
				return err
			}
			return a.runMutation(cmd, func() (core.ChangeSet, error) {
				return a.service.PlanUpdate(resource, body)
			})
		},
	}
	cmd.Flags().StringVar(&content, "content", "", "literal replacement content")
	cmd.Flags().StringVarP(&file, "file", "f", "", "read replacement content from a file")
	return cmd
}

func (a *app) removeCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <id-or-name>",
		Aliases: []string{"delete", "rm"},
		Short:   "Remove a resource transactionally",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resource, err := a.find(args[0])
			if err != nil {
				return err
			}
			return a.runMutation(cmd, func() (core.ChangeSet, error) {
				return a.service.PlanDelete(resource)
			})
		},
	}
}

func (a *app) toggleCommand(enabled bool) *cobra.Command {
	action := "disable"
	if enabled {
		action = "enable"
	}
	return &cobra.Command{
		Use:   action + " <id-or-name>",
		Short: action + " a resource using native flags or RTS disabled storage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resource, err := a.find(args[0])
			if err != nil {
				return err
			}
			return a.runMutation(cmd, func() (core.ChangeSet, error) {
				return a.service.PlanToggle(resource, enabled)
			})
		},
	}
}

func (a *app) diffCommand() *cobra.Command {
	var content, file string
	cmd := &cobra.Command{
		Use:   "diff <id-or-name>",
		Short: "Preview replacement content as a unified diff",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resource, err := a.find(args[0])
			if err != nil {
				return err
			}
			body := []byte(content)
			if file != "" {
				body, err = os.ReadFile(file)
			}
			if err != nil {
				return err
			}
			if len(body) == 0 {
				return errors.New("--content or --file is required")
			}
			diff, err := a.service.Diff(resource, body)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), diff)
			return nil
		},
	}
	cmd.Flags().StringVar(&content, "content", "", "literal proposed content")
	cmd.Flags().StringVarP(&file, "file", "f", "", "read proposed content from a file")
	return cmd
}

func (a *app) doctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "doctor",
		Aliases: []string{"validate"},
		Short:   "Validate detected harness resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			filters, err := a.filters()
			if err != nil {
				return err
			}
			diagnostics, err := a.service.Doctor(cmd.Context(), a.project, filters)
			if err != nil {
				return err
			}
			if a.json {
				return writeJSON(cmd.OutOrStdout(), core.NewEnvelope(diagnostics))
			}
			for _, diagnostic := range diagnostics {
				fmt.Fprintf(cmd.OutOrStdout(), "%-7s %-11s %s\n", strings.ToUpper(diagnostic.Severity), diagnostic.Harness, diagnostic.Message)
				if diagnostic.Hint != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "                    hint: %s\n", diagnostic.Hint)
				}
			}
			if len(diagnostics) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No configuration problems found.")
			}
			return nil
		},
	}
}

func (a *app) linkCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "link <source-id> <target-id>...",
		Short: "Link resources for explicit drift-aware synchronization",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			link, err := a.service.Link(a.project, args[0], args[1:])
			if err != nil {
				return err
			}
			return a.output(cmd, link)
		},
	}
}

func (a *app) unlinkCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <link-id>",
		Short: "Remove a synchronization relationship",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.service.Store.RemoveLink(args[0]); err != nil {
				return err
			}
			return a.output(cmd, map[string]string{"removed": args[0]})
		},
	}
}

func (a *app) syncCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync [link-id]",
		Short: "Show drift or synchronize one linked resource",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				drift, err := a.service.Drift(a.project)
				if err != nil {
					return err
				}
				return a.output(cmd, drift)
			}
			if a.dryRun {
				changes, results, err := a.service.Sync(cmd.Context(), a.project, args[0], true)
				if err != nil {
					return err
				}
				return a.output(cmd, map[string]any{"changes": changes, "results": results})
			}
			if !a.yes {
				fmt.Fprintf(cmd.OutOrStdout(), "Synchronize link %s? [y/N] ", args[0])
				if !confirm(cmd.InOrStdin()) {
					return errors.New("cancelled")
				}
			}
			changes, results, err := a.service.Sync(cmd.Context(), a.project, args[0], false)
			if err != nil {
				return err
			}
			return a.output(cmd, map[string]any{"changes": changes, "results": results})
		},
	}
	return cmd
}

func (a *app) projectCommand() *cobra.Command {
	parent := &cobra.Command{Use: "project", Short: "Manage saved project roots"}
	var label string
	add := &cobra.Command{
		Use: "add <path>", Args: cobra.ExactArgs(1), Short: "Save a project root",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			if err := a.service.Store.AddProject(store.Project{Path: path, Label: label, CreatedAt: time.Now().UTC()}); err != nil {
				return err
			}
			return a.output(cmd, map[string]string{"path": path, "label": label})
		},
	}
	add.Flags().StringVar(&label, "label", "", "display label")
	list := &cobra.Command{
		Use: "list", Short: "List saved projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			projects, err := a.service.Store.Projects()
			if err != nil {
				return err
			}
			return a.output(cmd, projects)
		},
	}
	remove := &cobra.Command{
		Use: "remove <path>", Args: cobra.ExactArgs(1), Short: "Forget a saved project",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := filepath.Abs(args[0])
			return a.service.Store.RemoveProject(path)
		},
	}
	parent.AddCommand(add, list, remove)
	return parent
}

func (a *app) backupCommand() *cobra.Command {
	parent := &cobra.Command{Use: "backup", Short: "List and restore transaction backups"}
	parent.AddCommand(
		&cobra.Command{
			Use: "list", Short: "List backups",
			RunE: func(cmd *cobra.Command, args []string) error {
				backups, err := a.service.Backups()
				if err != nil {
					return err
				}
				return a.output(cmd, backups)
			},
		},
		&cobra.Command{
			Use: "restore <backup>", Args: cobra.ExactArgs(1), Short: "Restore one backup",
			RunE: func(cmd *cobra.Command, args []string) error {
				if !a.yes {
					fmt.Fprint(cmd.OutOrStdout(), "Restore this backup and overwrite current files? [y/N] ")
					if !confirm(cmd.InOrStdin()) {
						return errors.New("cancelled")
					}
				}
				result, err := a.service.Restore(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return a.output(cmd, result)
			},
		},
	)
	return parent
}

func (a *app) adapterCommand() *cobra.Command {
	parent := &cobra.Command{Use: "adapter", Short: "Inspect modular harness adapters"}
	parent.AddCommand(
		&cobra.Command{
			Use: "list", Short: "List built-in adapters",
			RunE: func(cmd *cobra.Command, args []string) error {
				type adapterInfo struct {
					ID   core.Harness `json:"id"`
					Name string       `json:"name"`
					Docs []string     `json:"docs"`
				}
				var result []adapterInfo
				for _, driver := range a.service.Registry.Drivers() {
					result = append(result, adapterInfo{ID: driver.ID(), Name: driver.DisplayName(), Docs: driver.Docs()})
				}
				return a.output(cmd, result)
			},
		},
		&cobra.Command{
			Use: "detect", Short: "Detect installed harness surfaces and versions",
			RunE: func(cmd *cobra.Command, args []string) error {
				return a.output(cmd, a.service.Installations(cmd.Context()))
			},
		},
	)
	return parent
}

func (a *app) lifecycleCommand(action string) *cobra.Command {
	return &cobra.Command{
		Use:   action + " <plugin>",
		Short: action + " a plugin through the harness native CLI",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.harness == "" {
				return errors.New("--harness is required")
			}
			harness, err := core.ParseHarness(a.harness)
			if err != nil {
				return err
			}
			return a.runMutation(cmd, func() (core.ChangeSet, error) {
				return a.service.PlanNativeLifecycle(harness, action, args[0])
			})
		},
	}
}

func (a *app) updateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update [plugin]",
		Short: "Update RTS, or update a plugin through a harness native CLI",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				if a.harness == "" {
					return errors.New("--harness is required when updating a plugin")
				}
				harness, err := core.ParseHarness(a.harness)
				if err != nil {
					return err
				}
				return a.runMutation(cmd, func() (core.ChangeSet, error) {
					return a.service.PlanNativeLifecycle(harness, "update", args[0])
				})
			}
			if a.dryRun {
				return errors.New("--dry-run is not supported for self-update")
			}
			if a.json && !a.yes {
				return errors.New("JSON mutations require --yes")
			}
			if !a.yes {
				fmt.Fprintf(cmd.OutOrStdout(), "Update RTS %s from the latest GitHub release? [y/N] ", buildinfo.Version)
				if !confirm(cmd.InOrStdin()) {
					return errors.New("cancelled")
				}
			}
			result, err := selfupdate.Run(cmd.Context(), selfupdate.Options{CurrentVersion: buildinfo.Version})
			if err != nil {
				return err
			}
			if a.json {
				return writeJSON(cmd.OutOrStdout(), core.NewEnvelope(result))
			}
			if result.Updated {
				fmt.Fprintf(cmd.OutOrStdout(), "Updated RTS from %s to %s.\n", result.PreviousVersion, result.Version)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "RTS %s is already up to date.\n", result.Version)
			}
			return nil
		},
	}
}

func (a *app) filters() (service.Filters, error) {
	var filters service.Filters
	var err error
	if a.harness != "" {
		filters.Harness, err = core.ParseHarness(a.harness)
		if err != nil {
			return filters, err
		}
	}
	if a.kind != "" {
		filters.Kind, err = core.ParseKind(a.kind)
		if err != nil {
			return filters, err
		}
	}
	if a.scope != "" {
		filters.Scope, err = core.ParseScope(a.scope)
		if err != nil {
			return filters, err
		}
	}
	filters.Query = a.query
	return filters, nil
}

func (a *app) find(id string) (core.Resource, error) {
	filters, err := a.filters()
	if err != nil {
		return core.Resource{}, err
	}
	return a.service.Find(a.project, id, filters)
}

func (a *app) runMutation(cmd *cobra.Command, mutate mutation) error {
	change, err := mutate()
	if err != nil {
		return err
	}
	if a.json {
		if a.dryRun || !a.yes {
			return writeJSON(cmd.OutOrStdout(), core.NewEnvelope(map[string]any{"preview": json.RawMessage(service.MarshalChange(change))}))
		}
	} else {
		fmt.Fprint(cmd.OutOrStdout(), string(service.MarshalChange(change)))
	}
	if a.dryRun {
		return nil
	}
	if !a.yes {
		if a.json {
			return errors.New("JSON mutations require --yes or --dry-run")
		}
		fmt.Fprint(cmd.OutOrStdout(), "Apply this change? [y/N] ")
		if !confirm(cmd.InOrStdin()) {
			return errors.New("cancelled")
		}
	}
	result, err := a.service.Apply(cmd.Context(), change)
	if err != nil {
		return err
	}
	if a.json {
		return writeJSON(cmd.OutOrStdout(), core.NewEnvelope(map[string]any{"change": change, "result": result}))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Applied %s\nBackup: %s\n", result.TransactionID, result.BackupDir)
	return nil
}

func (a *app) output(cmd *cobra.Command, value any) error {
	if a.json {
		return writeJSON(cmd.OutOrStdout(), core.NewEnvelope(value))
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(b))
	return nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func confirm(reader io.Reader) bool {
	scanner := bufio.NewScanner(reader)
	if !scanner.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}

func editExternally(resource core.Resource) (string, error) {
	path := service.EditablePath(resource)
	command, err := editor.Command(path)
	if err != nil {
		return "", err
	}
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return "", err
	}
	return path, nil
}

func keyValues(values []string) map[string]string {
	result := map[string]string{}
	for _, item := range values {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			result[key] = value
		}
	}
	return result
}

func truncate(value string, length int) string {
	runes := []rune(value)
	if len(runes) <= length {
		return value
	}
	return string(runes[:length-1]) + "…"
}
