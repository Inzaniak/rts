# Architecture

## Data flow

```text
native files / native CLIs
          │
          ▼
 versioned harness drivers
          │
          ▼
 inventory + validation + changesets
       ┌──┴──────────────┐
       ▼                 ▼
  Cobra CLI        Bubble Tea TUI
       │                 │
       └──── transaction executor
                       │
              locks + hashes + backups
```

RTS's SQLite database contains only saved projects, resource links,
fingerprints, and migrations. It does not mirror the complete
contents of native configuration.

## Resource identity

A resource key includes harness, scope, kind, logical name, native path, and
an optional in-file locator. The short public ID is a stable hash of that key.
The content fingerprint is separate and changes whenever the native resource
changes.

An MCP server is an example of an in-file resource: multiple resource records
can point to one JSON/TOML file with different locators. A skill is a directory
resource whose primary editable document is `SKILL.md`.

## Transactions

A driver never writes directly. It returns a changeset containing:

- expected input hashes;
- paths and replacement bytes;
- directory creation or removal operations;
- optional native CLI invocations;
- warnings and a human-readable summary.

The executor locks targets in sorted path order, checks hashes, creates durable
backups, writes through same-directory temporary files, renames atomically, and
supports transactional moves into RTS storage. Moves preserve files,
directories, permissions, and symbolic-link references. If any later operation
fails, backed-up file state is restored.

Native CLI operations can touch harness-owned state that RTS cannot snapshot.
They are therefore clearly identified in previews and delegated only to
documented harness commands.

## Disabled resources

Disabling a standalone resource moves it from its native path into
`rts/disabled/payloads` and records its complete resource identity in
`rts/disabled/manifests`. Embedded resources store their exact content, native
document path, and in-file locator in the manifest before a targeted removal.

Disabled resources remain in inventory under their original IDs with
`enabled=false`. Re-enabling moves or writes the content back and removes the
manifest in one transaction. RTS refuses restoration if the original path or
embedded locator has been occupied while disabled.

## Adapter coverage

### Claude Code

- `CLAUDE_CONFIG_DIR` or `~/.claude`
- user/project/local settings
- `CLAUDE.md`, rules, skills, commands, agents, workflows, output styles,
  keybindings, themes, worktree includes, and installed plugin inventory
- user, project, and local MCP entries, including targeted edits in
  `~/.claude.json`

### Codex

- `CODEX_HOME` or `~/.codex`
- global and project config, profiles, hooks, executable rules, legacy prompts,
  `AGENTS.md`, `.agents/skills`, recursive Codex user/system skills, and skills
  contributed by enabled plugins
- TOML MCP tables without rewriting unrelated config
- managed `requirements.toml` as read-only

### Grok Build

- `GROK_HOME` or `~/.grok`
- user/project TOML settings and MCP tables, with targeted edits that preserve
  unrelated configuration
- `AGENTS.md`, rules, skills, commands, agents, roles, personas, hooks, plugins,
  and marketplace inventory, including shared `~/.agents` resources
- user and system managed/requirements policy layers as read-only
- native plugin install, update, and uninstall lifecycle commands

### Antigravity

- global `.gemini/config` and Antigravity CLI settings
- `.agents` rules, skills, agents, hooks, MCP, and plugins
- legacy `.agent` skills/rules are discovered with migration warnings
- undocumented application state and OAuth tokens are excluded

### OpenCode

- `OPENCODE_CONFIG_DIR` or the XDG config directory
- global/project JSONC and TUI config, instructions, skills, agents, commands,
  tools, plugins, and themes
- v1 `mcp.<name>` and v2 `mcp.servers.<name>` selected from the installed
  surface and actual document shape
- singular legacy directories are readable; new resources use plural paths

### GitHub Copilot

- `COPILOT_HOME` or `~/.copilot`
- personal and repository settings, instructions, skills, agents, hooks,
  extensions, LSP, plugins, and marketplaces
- user `.mcp-config.json`, workspace `.mcp.json`, and `.github/mcp.json`
- application state, permissions history, OAuth, and session data are excluded

## Adding a harness

1. Implement `core.Driver` or instantiate the generic driver with canonical and
   legacy locations.
2. Declare every surface and scope explicitly.
3. Mark managed, runtime, or undocumented locations read-only.
4. Add fixture tests for discovery, targeted edits, precedence, legacy formats,
   and invalid input.
5. Register the driver in `adapters.All`.

No CLI or TUI changes are required for a normal adapter.
