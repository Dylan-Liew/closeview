# CloseView

CloseView is a local web viewer for OpenCode, Codex, and Claude Code session
history. It reads native local session stores directly—without invoking an agent
CLI—and provides two session-level operations: view and delete.

OpenCode v2 session and message projections are supported. CloseView still reads
legacy v1 rows when present so a migrated database remains fully visible, while
v2 deletions go through OpenCode's authenticated API to keep service state consistent.

## Build

```bash
npm ci
npm run typecheck
npm run build
go build -o closeview ./cmd/closeview
```

## Web viewer

```bash
closeview serve --open
```

CloseView binds to `127.0.0.1:3434` by default and automatically reads:

- OpenCode: `~/.local/share/opencode/opencode.db`
- Codex: `$CODEX_HOME/sessions` or `~/.codex/sessions`
- Claude Code: `~/.claude/projects`

Override these locations with `CLOSEVIEW_OPENCODE_DB`,
`CLOSEVIEW_CODEX_HOME`, or `CLOSEVIEW_CLAUDE_HOME`. For OpenCode v2 deletion,
set `CLOSEVIEW_OPENCODE_URL` and either `CLOSEVIEW_OPENCODE_PASSWORD` or
`CLOSEVIEW_OPENCODE_PASSWORD_FILE`. The password file may be OpenCode v2's
`~/.local/state/opencode/service.json` registration file.

### Docker

Run CloseView as a restartable container, optionally bound to a private
Tailscale address:

```bash
npm ci
npm run build
CGO_ENABLED=0 go build -o closeview ./cmd/closeview
CLOSEVIEW_HOST="$(tailscale ip -4)" docker compose up -d --build
```

The Compose service mounts the OpenCode, Codex, and Claude session stores plus
OpenCode v2's service registration file. The session stores are writable because
confirmed deletion updates the native source; the service registration is read-only.

The viewer includes a unified history, nested OpenCode, Codex, and Claude Code
sub-sessions, source filters, search, stable deep links, structured messages,
collapsed context and reasoning, tool calls, code, copy controls, prompt
navigation, and explicit session deletion. Sub-sessions whose parent history is
no longer available remain accessible in a collapsed detached group.

Deleting in CloseView permanently removes the selected session from its native
local store. The UI requires confirmation for every deletion. CloseView never
uses a bulk-delete route.

## Legacy imports

The earlier user-selected file workflow remains available and uses CloseView's
own SQLite database:

```bash
closeview import [path] [--source auto] [--title title] [--force]
closeview list [--search query]
closeview select
closeview show [session_id]
closeview export [session_id...] [--format html|json] [--out path]
```

Generic import supports Markdown, JSON, JSONL, and plain-text logs. OpenCode's
database can also be imported explicitly with `closeview import --source
opencode`, but import is not required by the web viewer.

## Development

```bash
npm ci
npm run typecheck
npm run build
go test ./...
go build -o /tmp/closeview ./cmd/closeview
```

The React + TypeScript frontend uses shadcn-style, source-owned primitives and
Tabler Icons. Vite builds `internal/server/websrc` into
`internal/server/web`, which is embedded in the Go binary.

To enable the conventional commit hook locally:

```bash
git config core.hooksPath .githooks
```

Legacy imported data defaults to the user config directory. Set `CLOSEVIEW_DB`
or pass `--db path` to the legacy database commands to override it.
