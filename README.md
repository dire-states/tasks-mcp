# tasks-mcp

A small MCP server that manages tasks stored in a JSON file. Each task has an
`id`, `title`, `description`, `created_at` and `modified_at` (RFC 3339, UTC).

## Tools

| Tool | Arguments |
|---|---|
| `create_task` | `title`, `description?` |
| `list_tasks` | — (ordered by ID) |
| `get_task` | `id` |
| `update_task` | `id`, `title?`, `description?` (only given fields change) |
| `delete_task` | `id` |
| `search_tasks` | `query` (case-insensitive match on title/description; empty matches all) |

An unknown `id` or a blank `title` is reported as a tool error, not a protocol
error. Titles are trimmed of surrounding whitespace.

## Build & run

Requires Go 1.27+. Runs on Linux, macOS and Windows.

```sh
go build -o tasks-mcp .                              # tasks-mcp.exe on Windows
./tasks-mcp --file tasks.json                        # stdio (default)
./tasks-mcp --file tasks.json --http 127.0.0.1:8080  # Streamable HTTP
```

`--file` defaults to `tasks.json`, or `$TASKS_FILE` if set.

The HTTP mode has no authentication or TLS; bind it to localhost unless
something in front of it provides those.

## File format

```json
{
  "next_id": 3,
  "tasks": [
    {"id": 2, "title": "…", "description": "…", "created_at": "…", "modified_at": "…"}
  ]
}
```

The file is created on first write and rewritten atomically (temp file +
rename) on every change. IDs are never reused.

Several processes may share one file: each operation takes an advisory lock
on `<file>.lock` (`flock` on Unix, `LockFileEx` on Windows) and re-reads the
file, so changes made by one server are visible to the others immediately.
Do not delete the `.lock` file while a server is running. On Windows, keep
`tasks.json` on a local NTFS volume rather than in a cloud-synced folder such
as OneDrive; atomic replacement is only guaranteed there.

## Use with Claude Code

Build the binary, then create a project-scoped `.mcp.json` in this folder with
absolute paths (it is git-ignored because the paths are machine-specific; on
Windows point `command` at the `.exe`):

```json
{
  "mcpServers": {
    "tasks": {
      "type": "stdio",
      "command": "/path/to/tasks-mcp/tasks-mcp",
      "args": ["--file", "/path/to/tasks-mcp/tasks.json"]
    }
  }
}
```

Claude Code asks once to approve the server when started in this folder.

Elsewhere, register it manually:

```sh
claude mcp add tasks -- /path/to/tasks-mcp --file /path/to/tasks.json
```

## Test

```sh
go test -race ./...
```

The race detector needs a C toolchain (gcc) on Windows; plain `go test ./...`
does not. The GitHub Actions workflow in `.github/workflows/test.yml` runs the
suite on Linux, macOS and Windows.
