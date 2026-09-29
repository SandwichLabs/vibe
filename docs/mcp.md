# MCP (Model Context Protocol)

Vibe exposes a `.vibe` file's tools — the same registry (`internal/agents`)
Studio's own chat panel uses — to external MCP clients (Claude Desktop, any
other MCP-speaking agent) in two ways:

- **`vibe mcp serve <file.vibe>`** — a standalone headless server, for
  launching from an MCP client's own config (stdio, the default) or for
  pointing a network client at (`--transport sse`).
- **Studio's built-in SSE server** — Settings → MCP Server → Start, for
  reaching a file that's already open in the desktop app.

Every tool called through either path goes through the exact same sandbox
gating, per-file "ask before changes" policy, and result
truncation/empty-result/row-budget guardrails as Studio's own chat agent
(#63, #70, #79) — there is one tool surface, not a separate, looser one for
MCP.

## `vibe mcp serve`

```
vibe mcp serve <file.vibe> [flags]

Flags:
      --transport string      stdio or sse (default "stdio")
      --addr string            listen address for --transport sse (default "127.0.0.1:0")
      --sandbox string         read-only, read-write, or full — can only TIGHTEN
                                the file's own agent_sandbox setting, never loosen it
      --read-only-file          open the container itself read-only, alongside a writer
                                (Studio or another vibe mcp serve): implies a read-only
                                sandbox and never saves on exit
```

### Sandbox

`--sandbox` can only make a session **more** restrictive than the file's own
sandbox mode (set in Studio's Settings → Agent) — never less. Passing
`--sandbox full` against a file saved as `read-only` has no effect; the
file's own setting still wins. This means a launch config (which may be
shared, checked into a dotfiles repo, etc.) can never silently grant more
access than the file's owner configured in Studio. A fresh file's default
mode is **read-write** (not full — see #79).

### Ask before changes (#79)

Every file also has an **"Ask before changes"** switch, on by default —
including for every file created before #79 shipped, which had no such
setting at all. In Studio chat, a pending write shows an inline card the
user approves or cancels before it runs. Over MCP there is no one to show
that card to, so **while "Ask before changes" is on, an MCP client can read
but any write is refused outright** — the error text names the file's
sandbox mode and points back to the switch. This is true for both
`vibe mcp serve` and Studio's own SSE MCP server; the desktop app being open
doesn't count as "someone is watching this specific request".

To let MCP clients make changes, open the file in **Vibe Studio → Settings →
Agent** and turn **"Ask before changes" off** — there is no flag or
environment variable that bypasses it from the MCP side; a launch config
must never be able to loosen what the file owner set.

### Passphrase

The passphrase is read from the `VIBE_PASSPHRASE` environment variable if
set (the non-interactive path, for a client like Claude Desktop that has no
TTY to prompt on), otherwise from an interactive terminal prompt — the same
two-step resolution `vibe export`/`vibe info` use. Either way, prompts and
all logging go to **stderr**, never stdout: for `--transport stdio`, stdout
is the JSON-RPC protocol channel and must carry nothing else.

### Lifecycle

The container is checkpointed and saved once, on the way out, whichever of
three things ends the session first: stdin closing (the client
disconnected), `SIGINT`, or `SIGTERM` — all treated as a clean, expected
shutdown (exit code 0), not an error. Shutdown order: stop accepting new SSE
requests (SSE only), stop the debounced saver (waiting for any save already
in flight to finish), one final synchronous save, then close.

In between, a debounced background save runs ~5 seconds after the last tool
call that actually **wrote** something, so a session that's killed
uncleanly (`kill -9`, an OOM) loses at most a few seconds of real writes
rather than the whole session. "Wrote something" is judged per call, not
per category: a plain `SELECT` via `run_query` is classified the same way
`run_query` itself classifies it and never marks the file dirty, so a
read-only or read-heavy session never pays a save cost early at all —
only `INSERT`/`UPDATE`/`DELETE` (via `run_query` or `create_record`/
`update_record`/`delete_record`/`bulk_insert`) and schema/content
tools do.

### One writer per file (#80)

A `.vibe` file can be open for **writing** by one process at a time — Studio,
`vibe mcp serve`, or a CLI command (`compact`, `rekey`, `import`, `studio`
TUI). Whichever opens it second is refused, with the holder's app name and
pid: `f.vibe is already open in Vibe Studio (pid 4242); close it there
first`. `vibe info`/`export`/`dump`/`verify` and `mcp serve
--read-only-file` don't take this lock and can read the file's last saved
state alongside a writer.

If you're already working in a file in Studio and also want an MCP client
(e.g. Claude Desktop) connected to it, don't launch a second
`vibe mcp serve` against it — start **Studio's own MCP server** (Settings →
MCP Server) and point the client at that SSE endpoint instead. Or pass
`--read-only-file` to read the file's last saved state: unsaved Studio edits
won't be visible, and the sandbox is forced to read-only regardless of
`--sandbox` or the file's own setting.

The lock is a sidecar `f.vibe.lock` next to the file, held for the life of
the writable session. If the holder is killed (`kill -9`, a crash, an OOM),
the operating system releases the lock immediately — the leftover
`f.vibe.lock` file is harmless and the next writer overwrites it
automatically; there is nothing to clean up by hand. This is not enforced
across machines: a `.vibe` file on a synced folder (Dropbox, a network
share) opened for writing on two machines at once is not caught by this
lock.

### SSE transport security

Both `--transport sse` and Studio's built-in MCP server bind to
`127.0.0.1` only and require a random bearer token, generated fresh each
time the server starts and printed to stderr (`--transport sse`) or shown
in Settings → MCP Server with a copy button (Studio). A request is rejected
if it:

- carries a browser `Origin` header at all (a legitimate MCP client never
  sets one — only a website running in a browser tab does, via
  fetch/XHR/EventSource — so its mere presence, regardless of value, is
  treated as a CSRF/DNS-rebinding attempt and blocked with `403`);
- has a non-loopback `Host` header (`404`/`403` defense in depth against DNS
  rebinding);
- is missing or has the wrong `Authorization: Bearer <token>` (`401`).

For the initial `GET /sse` connection only, a `?token=<token>` query
parameter is also accepted as a fallback, for a non-browser client whose SSE
library can't set custom headers on that specific request. (A browser
`EventSource` still can't use it: the browser attaches an `Origin` header,
which is rejected first.) **Every
other request — in particular the `POST` a client sends to make an actual
tool call — must use the `Authorization: Bearer <token>` header; `?token=`
on a `POST` is always rejected with `401`.** This is deliberate, not a gap:
the URL an SSE session posts tool calls to is generated by the MCP server
library itself and never carries the token, so there is no "generate the
token into the POST URL" option without forking that library — and every
real MCP client (Claude Desktop, an SDK, curl) can set arbitrary headers on
a POST, so the header is never actually a burden there. Only a hand-rolled
`EventSource`-based client needs the query-param fallback at all, and only
for its one `GET`.

## Claude Desktop configuration

Add to Claude Desktop's `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "vibe": {
      "command": "/path/to/vibe",
      "args": ["mcp", "serve", "/path/to/your-file.vibe", "--sandbox", "read-write"],
      "env": {
        "VIBE_PASSPHRASE": "your-passphrase-here"
      }
    }
  }
}
```

If the file still has "Ask before changes" on (the default — see above),
Claude Desktop will be able to read through this connection but every write
will come back refused. Turn the switch off in Vibe Studio → Settings →
Agent first if you want this connection to be able to write.

Claude Desktop launches this as a subprocess and speaks stdio JSON-RPC to
it, so `--transport stdio` (the default) is correct here — omit
`--transport` entirely. Storing a real passphrase in this file is a
tradeoff the same way any MCP server credential is; treat
`claude_desktop_config.json` as sensitive once you've added this.

## Tool list

`vibe mcp serve` and Studio's SSE server both advertise exactly
`agents.ListToolsForMode(agents.ModeMCP)` — every non-task-mode tool
(`run_query`, `introspect_schema`, `describe_table`, `create_record`,
`create_table`, `present_data`, skills, notebook-authoring tools, etc.).
Task-mode-only tools (`preview_query`, `write_cell`, `need_more_info`) are
never listed or dispatchable over MCP — those exist purely for the in-app
task-mode notebook-cell authoring loop (#66).
