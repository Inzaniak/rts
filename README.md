# RTS

RTS is a terminal application for managing the editable configuration surfaces
of AI coding harnesses:

- Claude Code
- Codex
- Grok Build
- Google Antigravity
- OpenCode, including its current and legacy MCP layouts
- GitHub Copilot CLI and repository configuration

It discovers native files instead of replacing them with a proprietary source
of truth. Every mutation is previewed, hash-guarded, locked, backed up, applied
atomically, and rolled back when a later operation fails.

## Install

Install the latest checksum-verified release on macOS or Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/Inzaniak/rts/main/install.sh | sh
```

The installer writes to `~/.local/bin` by default. Set `RTS_INSTALL_DIR` to use
another directory:

```sh
curl -fsSL https://raw.githubusercontent.com/Inzaniak/rts/main/install.sh | RTS_INSTALL_DIR=/usr/local/bin sh
```

On Windows, run this from PowerShell:

```powershell
irm https://raw.githubusercontent.com/Inzaniak/rts/main/install.ps1 | iex
```

The Windows installer writes to `%LOCALAPPDATA%\Programs\rts` and adds that
directory to the user `PATH`. Set `RTS_INSTALL_DIR` to choose another location.

Alternatively, install directly from the Go module:

```sh
go install github.com/Inzaniak/rts/cmd/rts@latest
```

RTS supports macOS, Linux, and Windows on `amd64` or `arm64`. Update an installed
binary from the latest GitHub release with:

```sh
rts update
```

Use `rts update --yes` in a non-interactive script. `rts --version` prints the
installed version. Existing plugin updates remain available as
`rts update PLUGIN --harness HARNESS`.

## Build from source

RTS requires Go 1.25 or newer.

```sh
go build -o rts ./cmd/rts
go test ./...
```

Run the TUI for the current project:

```sh
./rts .
```

Inspect resources non-interactively:

```sh
./rts list --project . --harness codex
./rts get RESOURCE_ID --project .
./rts doctor --project .
```

## Common operations

Open a resource's native file directly in VS Code (or the system editor):

```sh
./rts edit RESOURCE_ID --project .
```

Direct editor changes are saved by the editor itself. Use `--content` or
`--file` when you want RTS's transactional mutation and backup workflow.

Create a project skill. Mutations print a plan and ask for confirmation unless
`--yes` is supplied:

```sh
./rts add skill review \
  --harness claude \
  --scope project \
  --project .
```

Add an MCP server:

```sh
./rts add mcp docs \
  --harness codex \
  --scope user \
  --url https://example.com/mcp
```

Preview the same operation without writing:

```sh
./rts add mcp docs \
  --harness antigravity \
  --scope project \
  --project . \
  --command npx \
  --arg=-y,@example/docs-mcp \
  --dry-run
```

Link existing resources and synchronize them explicitly:

```sh
./rts link SOURCE_ID TARGET_ID --project .
./rts sync
./rts sync LINK_ID --dry-run --project .
./rts sync LINK_ID --yes --project .
```

Manage native plugin lifecycles:

```sh
./rts install plugin-name --harness claude --dry-run
./rts install plugin-name --harness claude --yes
```

Temporarily remove a resource from its harness and keep it in RTS-managed
storage, then restore it to the exact recorded location:

```sh
./rts disable RESOURCE_ID --yes
./rts enable RESOURCE_ID --yes
```

Standalone files, directories, and symbolic links are moved intact. Embedded
resources such as MCP entries retain their native file path and in-file locator.

All machine-readable commands support `--json`. A JSON mutation requires
`--dry-run` or `--yes`; RTS never waits for an interactive confirmation while
emitting JSON.

## TUI keys

| Key | Action |
| --- | --- |
| `tab` | Move focus between the resource list and filter section |
| `up` / `down` | Select Harness, Kind, or Scope while filters are focused |
| `left` / `right` | Change the selected filter value |
| `left click` | Select a visible Harness, Kind, or Scope value |
| `up` / `down` | Scroll content one line in the detail view |
| `left` / `right` | Switch resources in the detail view |
| `page up` / `page down` | Scroll content one page in the detail view |
| `j` / `k` | Move through the resource list |
| `enter` | Open or close details |
| `/` | Search |
| `n` | Create another resource of the selected kind/scope |
| `e` | Open the native resource file directly in VS Code when available, otherwise the system text editor |
| `d` | Delete after confirmation |
| `space` | Disable a resource into RTS storage or restore it |
| `r` | Reload native files |
| `?` | Show help |
| `q` | Quit |

## Safety model

- Native harness files are authoritative.
- Unknown JSON/TOML keys and unrelated sections are retained.
- JSONC comments are retained by targeted MCP edits.
- Inline credential values are never intentionally printed by mutation plans.
- OAuth tokens, authentication files, sessions, logs, caches, transcripts, and
  harness databases are not managed.
- Managed enterprise policy is discovered as read-only.
- Symbolic links are preserved when disabled and restored; editing still targets
  the linked resource.
- Backups live under the OS user config directory in `rts/backups`.
- Disabled resources and restoration manifests live under `rts/disabled`.
- Re-enabling refuses to overwrite a path created while a resource was disabled.
- `RTS_CONFIG_HOME` can override RTS's own state directory for isolated runs.

## Adapter model

The core depends only on the `core.Driver` interface. Each driver owns:

- executable and version detection;
- user, project, local, plugin, and managed paths;
- native precedence and legacy locations;
- resource discovery and validation;
- changeset generation;
- documented feature limitations.

Straightforward new harnesses can be added as path/schema definitions. Drivers
with migrations or native lifecycle behavior can implement the interface
directly without changing the service, CLI, or TUI.

See [docs/architecture.md](docs/architecture.md) for the resource model,
transaction contract, and current path coverage.
