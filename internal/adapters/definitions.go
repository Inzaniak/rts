package adapters

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Inzaniak/rts/internal/core"
	"github.com/Inzaniak/rts/internal/documents"
)

func All() []core.Driver {
	return []core.Driver{claude(), codex(), grok(), antigravity(), opencode(), copilot()}
}

func claude() core.Driver {
	return &genericDriver{
		id: core.Claude, displayName: "Claude Code", commands: []string{"claude"},
		docs: []string{"https://code.claude.com/docs/en/claude-directory", "https://code.claude.com/docs/en/settings"},
		locations: func(project string) []location {
			user := envOr("CLAUDE_CONFIG_DIR", filepath.Join(homeDir(), ".claude"))
			var result []location
			result = append(result,
				single(core.KindSettings, core.ScopeUser, filepath.Join(user, "settings.json"), "jsonc", "claude-code"),
				single(core.KindInstructions, core.ScopeUser, filepath.Join(user, "CLAUDE.md"), "markdown", "claude-code"),
				files(core.KindRule, core.ScopeUser, filepath.Join(user, "rules"), "*.md", "markdown", "claude-code"),
				dirs(core.KindSkill, core.ScopeUser, filepath.Join(user, "skills"), "SKILL.md", "skill-directory", "claude-code"),
				files(core.KindCommand, core.ScopeUser, filepath.Join(user, "commands"), "*.md", "markdown", "claude-code"),
				files(core.KindAgent, core.ScopeUser, filepath.Join(user, "agents"), "*.md", "markdown", "claude-code"),
				files(core.KindWorkflow, core.ScopeUser, filepath.Join(user, "workflows"), "*.js", "javascript", "claude-code"),
				files(core.KindOutputStyle, core.ScopeUser, filepath.Join(user, "output-styles"), "*.md", "markdown", "claude-code"),
				single(core.KindKeybindings, core.ScopeUser, filepath.Join(user, "keybindings.json"), "jsonc", "claude-code"),
				files(core.KindTheme, core.ScopeUser, filepath.Join(user, "themes"), "*.json", "json", "claude-code"),
			)
			plugins := dirs(core.KindPlugin, core.ScopePlugin, filepath.Join(user, "plugins", "cache"), ".claude-plugin/plugin.json", "plugin-directory", "claude-code")
			plugins.ReadOnly = true
			result = append(result, plugins)
			if project != "" {
				root := filepath.Join(project, ".claude")
				result = append(result,
					single(core.KindSettings, core.ScopeProject, filepath.Join(root, "settings.json"), "jsonc", "claude-code"),
					single(core.KindSettings, core.ScopeLocal, filepath.Join(root, "settings.local.json"), "jsonc", "claude-code"),
					single(core.KindInstructions, core.ScopeProject, filepath.Join(project, "CLAUDE.md"), "markdown", "claude-code"),
					single(core.KindInstructions, core.ScopeProject, filepath.Join(root, "CLAUDE.md"), "markdown", "claude-code"),
					single(core.KindInstructions, core.ScopeLocal, filepath.Join(project, "CLAUDE.local.md"), "markdown", "claude-code"),
					files(core.KindRule, core.ScopeProject, filepath.Join(root, "rules"), "*.md", "markdown", "claude-code"),
					dirs(core.KindSkill, core.ScopeProject, filepath.Join(root, "skills"), "SKILL.md", "skill-directory", "claude-code"),
					files(core.KindCommand, core.ScopeProject, filepath.Join(root, "commands"), "*.md", "markdown", "claude-code"),
					files(core.KindAgent, core.ScopeProject, filepath.Join(root, "agents"), "*.md", "markdown", "claude-code"),
					files(core.KindWorkflow, core.ScopeProject, filepath.Join(root, "workflows"), "*.js", "javascript", "claude-code"),
					files(core.KindOutputStyle, core.ScopeProject, filepath.Join(root, "output-styles"), "*.md", "markdown", "claude-code"),
					single(core.KindWorktree, core.ScopeProject, filepath.Join(project, ".worktreeinclude"), "text", "claude-code"),
				)
			}
			return result
		},
		mcp: func(project string) []mcpLocation {
			state := filepath.Join(homeDir(), ".claude.json")
			result := []mcpLocation{{
				Scope: core.ScopeUser, Path: state, Format: "jsonc",
				JSONPath: []string{"mcpServers"}, Surface: "claude-code",
			}}
			if project != "" {
				result = append(result, mcpLocation{
					Scope: core.ScopeProject, Path: filepath.Join(project, ".mcp.json"), Format: "jsonc",
					JSONPath: []string{"mcpServers"}, Surface: "claude-code",
				})
				result = append(result, mcpLocation{
					Scope: core.ScopeLocal, Path: state, Format: "jsonc",
					JSONPath: []string{"projects", project, "mcpServers"}, Surface: "claude-code",
				})
			}
			return result
		},
	}
}

func codex() core.Driver {
	return &genericDriver{
		id: core.Codex, displayName: "Codex", commands: []string{"codex"},
		docs: []string{"https://learn.chatgpt.com/docs/config-file/config-reference", "https://learn.chatgpt.com/docs/agent-configuration/agents-md"},
		locations: func(project string) []location {
			home := envOr("CODEX_HOME", filepath.Join(homeDir(), ".codex"))
			sharedSkills := filepath.Join(homeDir(), ".agents", "skills")
			result := []location{
				single(core.KindSettings, core.ScopeUser, filepath.Join(home, "config.toml"), "toml", "codex"),
				single(core.KindInstructions, core.ScopeUser, filepath.Join(home, "AGENTS.md"), "markdown", "codex"),
				single(core.KindInstructions, core.ScopeUser, filepath.Join(home, "AGENTS.override.md"), "markdown", "codex"),
				dirs(core.KindSkill, core.ScopeUser, sharedSkills, "SKILL.md", "skill-directory", "codex"),
				recursiveDirs(core.KindSkill, core.ScopeUser, filepath.Join(home, "skills"), "SKILL.md", "skill-directory", "codex"),
				files(core.KindRule, core.ScopeUser, filepath.Join(home, "rules"), "*.rules", "rules", "codex"),
				single(core.KindHook, core.ScopeUser, filepath.Join(home, "hooks.json"), "jsonc", "codex"),
				files(core.KindCommand, core.ScopeUser, filepath.Join(home, "prompts"), "*.md", "markdown", "codex"),
				files(core.KindProfile, core.ScopeUser, home, "*.config.toml", "toml", "codex"),
			}
			systemSkills := dirs(core.KindSkill, core.ScopeManaged, filepath.Join(home, "skills", ".system"), "SKILL.md", "skill-directory", "codex")
			systemSkills.ReadOnly = true
			result = append(result, systemSkills)
			result = append(result, codexPluginSkillLocations(home)...)
			if project != "" {
				result = append(result,
					single(core.KindSettings, core.ScopeProject, filepath.Join(project, ".codex", "config.toml"), "toml", "codex"),
					single(core.KindHook, core.ScopeProject, filepath.Join(project, ".codex", "hooks.json"), "jsonc", "codex"),
					single(core.KindInstructions, core.ScopeProject, filepath.Join(project, "AGENTS.md"), "markdown", "codex"),
					single(core.KindInstructions, core.ScopeProject, filepath.Join(project, "AGENTS.override.md"), "markdown", "codex"),
					dirs(core.KindSkill, core.ScopeProject, filepath.Join(project, ".agents", "skills"), "SKILL.md", "skill-directory", "codex"),
					files(core.KindRule, core.ScopeProject, filepath.Join(project, ".codex", "rules"), "*.rules", "rules", "codex"),
				)
			}
			managed := single(core.KindPermissions, core.ScopeManaged, osManagedPath("codex", "requirements.toml"), "toml", "codex")
			managed.ReadOnly = true
			return append(result, managed)
		},
		mcp: func(project string) []mcpLocation {
			home := envOr("CODEX_HOME", filepath.Join(homeDir(), ".codex"))
			result := []mcpLocation{{Scope: core.ScopeUser, Path: filepath.Join(home, "config.toml"), Format: "toml", TOMLPrefix: "mcp_servers", Surface: "codex"}}
			if project != "" {
				result = append(result, mcpLocation{Scope: core.ScopeProject, Path: filepath.Join(project, ".codex", "config.toml"), Format: "toml", TOMLPrefix: "mcp_servers", Surface: "codex"})
			}
			return result
		},
	}
}

func grok() core.Driver {
	return &genericDriver{
		id: core.Grok, displayName: "Grok Build", commands: []string{"grok"},
		docs: []string{
			"https://docs.x.ai/build/settings",
			"https://docs.x.ai/build/features/project-rules",
			"https://docs.x.ai/build/features/skills-plugins-marketplaces",
			"https://docs.x.ai/build/features/mcp-servers",
		},
		locations: func(project string) []location {
			home := envOr("GROK_HOME", filepath.Join(homeDir(), ".grok"))
			result := []location{
				single(core.KindSettings, core.ScopeUser, filepath.Join(home, "config.toml"), "toml", "grok"),
				single(core.KindInstructions, core.ScopeUser, filepath.Join(home, "AGENTS.md"), "markdown", "grok"),
				single(core.KindInstructions, core.ScopeUser, filepath.Join(home, "Agents.md"), "markdown", "grok"),
				single(core.KindInstructions, core.ScopeUser, filepath.Join(home, "AGENT.md"), "markdown", "grok"),
				files(core.KindRule, core.ScopeUser, filepath.Join(home, "rules"), "*.md", "markdown", "grok"),
				dirs(core.KindSkill, core.ScopeUser, filepath.Join(home, "skills"), "SKILL.md", "skill-directory", "grok"),
				dirs(core.KindSkill, core.ScopeUser, filepath.Join(homeDir(), ".agents", "skills"), "SKILL.md", "skill-directory", "grok"),
				files(core.KindCommand, core.ScopeUser, filepath.Join(home, "commands"), "*.md", "markdown", "grok"),
				files(core.KindCommand, core.ScopeUser, filepath.Join(homeDir(), ".agents", "commands"), "*.md", "markdown", "grok"),
				files(core.KindAgent, core.ScopeUser, filepath.Join(home, "agents"), "*.md", "markdown", "grok"),
				files(core.KindAgent, core.ScopeUser, filepath.Join(home, "roles"), "*.toml", "toml", "grok"),
				files(core.KindAgent, core.ScopeUser, filepath.Join(home, "personas"), "*.toml", "toml", "grok"),
				files(core.KindHook, core.ScopeUser, filepath.Join(home, "hooks"), "*.json", "json", "grok"),
				dirs(core.KindPlugin, core.ScopeUser, filepath.Join(home, "plugins"), ".grok-plugin/plugin.json", "plugin-directory", "grok"),
				dirs(core.KindPlugin, core.ScopeUser, filepath.Join(home, "plugins"), "plugin.json", "plugin-directory", "grok"),
				recursiveDirs(core.KindPlugin, core.ScopePlugin, filepath.Join(home, "plugins", "marketplaces"), ".grok-plugin/plugin.json", "plugin-directory", "grok"),
				recursiveDirs(core.KindPlugin, core.ScopePlugin, filepath.Join(home, "plugins", "marketplaces"), "plugin.json", "plugin-directory", "grok"),
				single(core.KindMarketplace, core.ScopeUser, filepath.Join(home, "plugins", "known_marketplaces.json"), "json", "grok"),
			}
			for name, kind := range map[string]core.Kind{
				"managed_config.toml": core.KindSettings,
				"requirements.toml":   core.KindPermissions,
			} {
				managed := single(kind, core.ScopeManaged, filepath.Join(home, name), "toml", "grok")
				managed.ReadOnly = true
				result = append(result, managed)
			}
			for name, kind := range map[string]core.Kind{
				"managed_config.toml": core.KindSettings,
				"requirements.toml":   core.KindPermissions,
			} {
				managed := single(kind, core.ScopeManaged, filepath.Join("/etc", "grok", name), "toml", "grok")
				managed.ReadOnly = true
				result = append(result, managed)
			}
			if project != "" {
				root := filepath.Join(project, ".grok")
				result = append(result,
					single(core.KindSettings, core.ScopeProject, filepath.Join(root, "config.toml"), "toml", "grok"),
					single(core.KindInstructions, core.ScopeProject, filepath.Join(project, "AGENTS.md"), "markdown", "grok"),
					single(core.KindInstructions, core.ScopeProject, filepath.Join(project, "Agents.md"), "markdown", "grok"),
					single(core.KindInstructions, core.ScopeProject, filepath.Join(project, "AGENT.md"), "markdown", "grok"),
					files(core.KindRule, core.ScopeProject, filepath.Join(root, "rules"), "*.md", "markdown", "grok"),
					dirs(core.KindSkill, core.ScopeProject, filepath.Join(root, "skills"), "SKILL.md", "skill-directory", "grok"),
					files(core.KindCommand, core.ScopeProject, filepath.Join(root, "commands"), "*.md", "markdown", "grok"),
					files(core.KindAgent, core.ScopeProject, filepath.Join(root, "agents"), "*.md", "markdown", "grok"),
					files(core.KindAgent, core.ScopeProject, filepath.Join(root, "roles"), "*.toml", "toml", "grok"),
					files(core.KindAgent, core.ScopeProject, filepath.Join(root, "personas"), "*.toml", "toml", "grok"),
					files(core.KindHook, core.ScopeProject, filepath.Join(root, "hooks"), "*.json", "json", "grok"),
					dirs(core.KindPlugin, core.ScopeProject, filepath.Join(root, "plugins"), ".grok-plugin/plugin.json", "plugin-directory", "grok"),
					dirs(core.KindPlugin, core.ScopeProject, filepath.Join(root, "plugins"), "plugin.json", "plugin-directory", "grok"),
				)
			}
			return result
		},
		mcp: func(project string) []mcpLocation {
			home := envOr("GROK_HOME", filepath.Join(homeDir(), ".grok"))
			result := []mcpLocation{{
				Scope: core.ScopeUser, Path: filepath.Join(home, "config.toml"), Format: "toml",
				TOMLPrefix: "mcp_servers", EnableKey: "enabled", Surface: "grok",
			}}
			if project != "" {
				result = append(result, mcpLocation{
					Scope: core.ScopeProject, Path: filepath.Join(project, ".grok", "config.toml"), Format: "toml",
					TOMLPrefix: "mcp_servers", EnableKey: "enabled", Surface: "grok",
				})
			}
			return result
		},
	}
}

func antigravity() core.Driver {
	return &genericDriver{
		id: core.Antigravity, displayName: "Antigravity", commands: []string{"antigravity"},
		docs: []string{"https://antigravity.google/docs/mcp", "https://antigravity.google/docs/skills", "https://antigravity.google/docs/rules-workflows"},
		locations: func(project string) []location {
			gemini := filepath.Join(homeDir(), ".gemini")
			config := filepath.Join(gemini, "config")
			result := []location{
				single(core.KindSettings, core.ScopeUser, filepath.Join(config, "config.json"), "jsonc", "antigravity"),
				single(core.KindSettings, core.ScopeUser, filepath.Join(gemini, "antigravity-cli", "settings.json"), "jsonc", "antigravity-cli"),
				single(core.KindInstructions, core.ScopeUser, filepath.Join(gemini, "GEMINI.md"), "markdown", "antigravity"),
				dirs(core.KindSkill, core.ScopeUser, filepath.Join(config, "skills"), "SKILL.md", "skill-directory", "antigravity"),
				files(core.KindAgent, core.ScopeUser, filepath.Join(config, "agents"), "*.md", "markdown", "antigravity"),
				single(core.KindHook, core.ScopeUser, filepath.Join(config, "hooks.json"), "jsonc", "antigravity"),
				dirs(core.KindPlugin, core.ScopeUser, filepath.Join(config, "plugins"), "plugin.json", "plugin-directory", "antigravity"),
			}
			if project != "" {
				root := filepath.Join(project, ".agents")
				result = append(result,
					files(core.KindRule, core.ScopeProject, filepath.Join(root, "rules"), "*.md", "markdown", "antigravity"),
					dirs(core.KindSkill, core.ScopeProject, filepath.Join(root, "skills"), "SKILL.md", "skill-directory", "antigravity"),
					files(core.KindAgent, core.ScopeProject, filepath.Join(root, "agents"), "*.md", "markdown", "antigravity"),
					single(core.KindHook, core.ScopeProject, filepath.Join(root, "hooks.json"), "jsonc", "antigravity"),
				)
				legacyRules := files(core.KindRule, core.ScopeProject, filepath.Join(project, ".agent", "rules"), "*.md", "markdown", "antigravity")
				legacyRules.Legacy = true
				legacySkills := dirs(core.KindSkill, core.ScopeProject, filepath.Join(project, ".agent", "skills"), "SKILL.md", "skill-directory", "antigravity")
				legacySkills.Legacy = true
				result = append(result, legacyRules, legacySkills)
			}
			return result
		},
		mcp: func(project string) []mcpLocation {
			config := filepath.Join(homeDir(), ".gemini", "config")
			result := []mcpLocation{{
				Scope: core.ScopeUser, Path: filepath.Join(config, "mcp_config.json"), Format: "jsonc",
				JSONPath: []string{"mcpServers"}, DisableKey: "disabled", Surface: "antigravity",
			}, {
				Scope: core.ScopeUser, Path: filepath.Join(homeDir(), ".gemini", "antigravity-cli", "mcp_config.json"), Format: "jsonc",
				JSONPath: []string{"mcpServers"}, DisableKey: "disabled", Surface: "antigravity-cli",
			}}
			if project != "" {
				result = append(result, mcpLocation{
					Scope: core.ScopeProject, Path: filepath.Join(project, ".agents", "mcp_config.json"),
					Format: "jsonc", JSONPath: []string{"mcpServers"}, DisableKey: "disabled", Surface: "antigravity",
				})
			}
			return result
		},
	}
}

func opencode() core.Driver {
	return &genericDriver{
		id: core.OpenCode, displayName: "OpenCode", commands: []string{"opencode", "opencode2"},
		docs: []string{"https://dev.opencode.ai/docs/config", "https://opencode.ai/v2/docs/config"},
		locations: func(project string) []location {
			config := envOr("OPENCODE_CONFIG_DIR", filepath.Join(homeDir(), ".config", "opencode"))
			result := []location{
				single(core.KindSettings, core.ScopeUser, filepath.Join(config, "opencode.json"), "jsonc", "opencode"),
				single(core.KindSettings, core.ScopeUser, filepath.Join(config, "opencode.jsonc"), "jsonc", "opencode"),
				single(core.KindSettings, core.ScopeUser, filepath.Join(config, "tui.json"), "jsonc", "opencode"),
				single(core.KindInstructions, core.ScopeUser, filepath.Join(config, "AGENTS.md"), "markdown", "opencode"),
				dirs(core.KindSkill, core.ScopeUser, filepath.Join(config, "skills"), "SKILL.md", "skill-directory", "opencode"),
				files(core.KindAgent, core.ScopeUser, filepath.Join(config, "agents"), "*.md", "markdown", "opencode"),
				files(core.KindCommand, core.ScopeUser, filepath.Join(config, "commands"), "*.md", "markdown", "opencode"),
				files(core.KindTool, core.ScopeUser, filepath.Join(config, "tools"), "*.*", "source", "opencode"),
				files(core.KindPlugin, core.ScopeUser, filepath.Join(config, "plugins"), "*.*", "source", "opencode"),
				files(core.KindTheme, core.ScopeUser, filepath.Join(config, "themes"), "*.json", "json", "opencode"),
			}
			if project != "" {
				root := filepath.Join(project, ".opencode")
				result = append(result,
					single(core.KindSettings, core.ScopeProject, filepath.Join(project, "opencode.json"), "jsonc", "opencode"),
					single(core.KindSettings, core.ScopeProject, filepath.Join(project, "opencode.jsonc"), "jsonc", "opencode"),
					single(core.KindSettings, core.ScopeProject, filepath.Join(project, "tui.json"), "jsonc", "opencode"),
					single(core.KindInstructions, core.ScopeProject, filepath.Join(project, "AGENTS.md"), "markdown", "opencode"),
					dirs(core.KindSkill, core.ScopeProject, filepath.Join(root, "skills"), "SKILL.md", "skill-directory", "opencode"),
					files(core.KindAgent, core.ScopeProject, filepath.Join(root, "agents"), "*.md", "markdown", "opencode"),
					files(core.KindCommand, core.ScopeProject, filepath.Join(root, "commands"), "*.md", "markdown", "opencode"),
					files(core.KindTool, core.ScopeProject, filepath.Join(root, "tools"), "*.*", "source", "opencode"),
					files(core.KindPlugin, core.ScopeProject, filepath.Join(root, "plugins"), "*.*", "source", "opencode"),
					files(core.KindTheme, core.ScopeProject, filepath.Join(root, "themes"), "*.json", "json", "opencode"),
				)
			}
			return result
		},
		mcp: func(project string) []mcpLocation {
			config := envOr("OPENCODE_CONFIG_DIR", filepath.Join(homeDir(), ".config", "opencode"))
			global := firstExisting(filepath.Join(config, "opencode.jsonc"), filepath.Join(config, "opencode.json"))
			result := []mcpLocation{openCodeMCPLocation(global, core.ScopeUser)}
			if project != "" {
				projectConfig := firstExisting(filepath.Join(project, "opencode.jsonc"), filepath.Join(project, "opencode.json"))
				result = append(result, openCodeMCPLocation(projectConfig, core.ScopeProject))
			}
			return result
		},
	}
}

func copilot() core.Driver {
	return &genericDriver{
		id: core.Copilot, displayName: "GitHub Copilot", commands: []string{"copilot"},
		docs: []string{"https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-config-dir-reference"},
		locations: func(project string) []location {
			home := envOr("COPILOT_HOME", filepath.Join(homeDir(), ".copilot"))
			result := []location{
				single(core.KindSettings, core.ScopeUser, filepath.Join(home, "settings.json"), "jsonc", "copilot-cli"),
				single(core.KindInstructions, core.ScopeUser, filepath.Join(home, "copilot-instructions.md"), "markdown", "copilot-cli"),
				files(core.KindInstructions, core.ScopeUser, filepath.Join(home, "instructions"), "*.instructions.md", "markdown", "copilot-cli"),
				dirs(core.KindSkill, core.ScopeUser, filepath.Join(home, "skills"), "SKILL.md", "skill-directory", "copilot-cli"),
				files(core.KindAgent, core.ScopeUser, filepath.Join(home, "agents"), "*.agent.md", "markdown", "copilot-cli"),
				files(core.KindHook, core.ScopeUser, filepath.Join(home, "hooks"), "*.json", "json", "copilot-cli"),
				files(core.KindExtension, core.ScopeUser, filepath.Join(home, "extensions"), "*.*", "source", "copilot-cli"),
				single(core.KindLSP, core.ScopeUser, filepath.Join(home, "lsp-config.json"), "jsonc", "copilot-cli"),
			}
			if project != "" {
				github := filepath.Join(project, ".github")
				result = append(result,
					single(core.KindSettings, core.ScopeProject, filepath.Join(github, "copilot", "settings.json"), "jsonc", "copilot"),
					single(core.KindSettings, core.ScopeLocal, filepath.Join(github, "copilot", "settings.local.json"), "jsonc", "copilot-cli"),
					single(core.KindInstructions, core.ScopeProject, filepath.Join(github, "copilot-instructions.md"), "markdown", "copilot"),
					single(core.KindInstructions, core.ScopeProject, filepath.Join(project, "AGENTS.md"), "markdown", "copilot"),
					single(core.KindInstructions, core.ScopeProject, filepath.Join(project, "CLAUDE.md"), "markdown", "copilot"),
					single(core.KindInstructions, core.ScopeProject, filepath.Join(project, "GEMINI.md"), "markdown", "copilot"),
					files(core.KindInstructions, core.ScopeProject, filepath.Join(github, "instructions"), "*.instructions.md", "markdown", "copilot"),
					dirs(core.KindSkill, core.ScopeProject, filepath.Join(github, "skills"), "SKILL.md", "skill-directory", "copilot"),
					dirs(core.KindSkill, core.ScopeProject, filepath.Join(project, ".agents", "skills"), "SKILL.md", "skill-directory", "copilot"),
					dirs(core.KindSkill, core.ScopeProject, filepath.Join(project, ".claude", "skills"), "SKILL.md", "skill-directory", "copilot"),
					files(core.KindAgent, core.ScopeProject, filepath.Join(github, "agents"), "*.agent.md", "markdown", "copilot"),
					files(core.KindHook, core.ScopeProject, filepath.Join(github, "hooks"), "*.json", "json", "copilot"),
				)
			}
			return result
		},
		mcp: func(project string) []mcpLocation {
			home := envOr("COPILOT_HOME", filepath.Join(homeDir(), ".copilot"))
			result := []mcpLocation{{
				Scope: core.ScopeUser, Path: filepath.Join(home, "mcp-config.json"), Format: "jsonc",
				JSONPath: []string{"mcpServers"}, EnableKey: "enabled", Surface: "copilot-cli",
			}}
			if project != "" {
				result = append(result,
					mcpLocation{Scope: core.ScopeProject, Path: filepath.Join(project, ".mcp.json"), Format: "jsonc", JSONPath: []string{"mcpServers"}, EnableKey: "enabled", Surface: "copilot"},
					mcpLocation{Scope: core.ScopeProject, Path: filepath.Join(project, ".github", "mcp.json"), Format: "jsonc", JSONPath: []string{"mcpServers"}, EnableKey: "enabled", Surface: "copilot"},
				)
			}
			return result
		},
	}
}

func single(kind core.Kind, scope core.Scope, path, format, surface string) location {
	return location{Kind: kind, Scope: scope, Path: path, Format: format, Surface: surface}
}

func files(kind core.Kind, scope core.Scope, path, glob, format, surface string) location {
	return location{Kind: kind, Scope: scope, Path: path, Glob: glob, Format: format, Surface: surface}
}

func dirs(kind core.Kind, scope core.Scope, path, main, format, surface string) location {
	return location{Kind: kind, Scope: scope, Path: path, DirMain: main, Format: format, Surface: surface}
}

func recursiveDirs(kind core.Kind, scope core.Scope, path, main, format, surface string) location {
	return location{Kind: kind, Scope: scope, Path: path, DirMain: main, Recursive: true, Format: format, Surface: surface}
}

func codexPluginSkillLocations(home string) []location {
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		return nil
	}
	var config struct {
		Plugins map[string]struct {
			Enabled bool `toml:"enabled"`
		} `toml:"plugins"`
	}
	if toml.Unmarshal(raw, &config) != nil {
		return nil
	}
	var result []location
	for key, plugin := range config.Plugins {
		if !plugin.Enabled {
			continue
		}
		name, marketplace, ok := strings.Cut(key, "@")
		if !ok || name == "" || marketplace == "" {
			continue
		}
		root := firstExisting(
			filepath.Join(home, "plugins", "cache", marketplace+"-remote", name),
			filepath.Join(home, "plugins", "cache", marketplace, name),
		)
		version := latestPluginVersionDir(root)
		if version == "" {
			continue
		}
		loc := dirs(core.KindSkill, core.ScopePlugin, filepath.Join(version, "skills"), "SKILL.md", "skill-directory", "codex")
		loc.ReadOnly = true
		loc.Origin = key
		loc.NamePrefix = name + ":"
		result = append(result, loc)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Origin < result[j].Origin })
	return result
}

func latestPluginVersionDir(root string) string {
	if target, err := filepath.EvalSymlinks(filepath.Join(root, "latest")); err == nil {
		if fileExists(filepath.Join(target, ".codex-plugin", "plugin.json")) {
			return target
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() {
			path := filepath.Join(root, entry.Name())
			if fileExists(filepath.Join(path, ".codex-plugin", "plugin.json")) {
				candidates = append(candidates, path)
			}
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[len(candidates)-1]
}

func firstExisting(paths ...string) string {
	for _, path := range paths {
		if fileExists(path) {
			return path
		}
	}
	return paths[len(paths)-1]
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func openCodeMCPLocation(path string, scope core.Scope) mcpLocation {
	version2 := false
	if _, err := exec.LookPath("opencode2"); err == nil {
		version2 = true
	}
	if raw, err := os.ReadFile(path); err == nil {
		var root map[string]any
		if documents.DecodeJSONC(raw, &root) == nil {
			if mcp, ok := root["mcp"].(map[string]any); ok {
				_, version2 = mcp["servers"]
			}
		}
	}
	if version2 {
		return mcpLocation{
			Scope: scope, Path: path, Format: "jsonc", JSONPath: []string{"mcp", "servers"},
			DisableKey: "disabled", Surface: "opencode-v2",
		}
	}
	return mcpLocation{
		Scope: scope, Path: path, Format: "jsonc", JSONPath: []string{"mcp"},
		EnableKey: "enabled", Surface: "opencode-v1",
	}
}
