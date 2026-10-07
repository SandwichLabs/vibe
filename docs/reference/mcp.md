# MCP (Model Context Protocol)

Vibe exposes a **workspace's** tools (the same registry, `internal/agents`, that Studio's chat panel uses) to external MCP clients such as Claude Desktop and Claude Code:

- **`vibe mcp serve [--workspace DIR]`**: a headless server you launch from the client's own config (stdio, the default), or point a network client at (`--transport sse`).
- **Studio's built-in SSE server**: Studio owns the workspace it has open and can serve MCP on a loopback port (Settings → Workspace → Claude and other tools). A write that needs approval shows the same approval card the chat agent uses, in the Studio window; the card waits for you, and a declined or unanswered card changes nothing.

Every tool call, through either path, goes through the same sandbox gating, "Ask before changes" policy and result guardrails as Studio's chat agent (#63, #70, #79). There is one tool surface, not a looser one for MCP. The agent's SQL is an allow-list per tier ([reference/agent-policy.md](agent-policy.md)), and a write to a connection that is not a local file always needs approval, so headless MCP refuses it. `.vibe` files are no longer opened as workspaces: pass a workspace folder (a `.vibe` is a sharing bundle you import).

## `vibe mcp serve`

```
vibe mcp serve [--workspace DIR] [flags]

Flags:
      --workspace string   workspace folder (default: the nearest folder above the
                           current directory that holds a vibe.yaml)
      --transport string   stdio or sse (default "stdio")
      --addr string        listen address for --transport sse (default "127.0.0.1:0")
      --sandbox string     read-only, read-write, or full. Can only TIGHTEN the
                           workspace's own policy, never loosen it
```

The workspace is found the way git finds `.git`: `--workspace` (or the current directory) and then each parent until a `vibe.yaml` turns up. A `.vibe` path, as an argument or as `--workspace`, is refused with "`.vibe` files are no longer opened directly; open or create a workspace folder". The old `<file.vibe>` argument, `--read-only-file` and `VIBE_PASSPHRASE` are gone.

### One owner per workspace: owner mode and proxy mode

DuckDB allows one read-write process or many read-only ones per database file, never a mix, so one process owns a workspace's hub at a time, recorded in `.vibe-local/hub.lock` (see [workspaces.md](workspaces.md)). `vibe mcp serve` looks at the lock first:

| Who holds the hub | What `vibe mcp serve` does |
|---|---|
| Nobody | **Owner mode.** Takes the lock, opens the workspace and its DuckDB hub, serves the tools. Writes go straight to their sources: there is nothing to save on exit, and a crash loses nothing but the call in flight. |
| Studio, serving MCP | **Proxy mode.** Passes stdio through to Studio's MCP server. The client sees the same tools, Studio's approval cards apply to writes, and there is no DuckDB lock error. |
| Another `vibe mcp serve` | Proxy mode, to that one (an owner over stdio also serves loopback SSE and publishes it in `hub.lock`, so a second client such as Claude Code next to Claude Desktop just works). |
| Studio with MCP off | Fails with a message naming it: "Studio (pid N) has this workspace open but its MCP server is off. Turn it on in Settings → Workspace → MCP, or close Studio." |

Proxy mode is a transparent pipe, not an MCP client: every SSE `message` event is copied verbatim to stdout (responses, notifications, server-to-client requests) and every stdin line is POSTed verbatim, so the tool list cannot drift and there is no client-side request timeout (a write waiting for an approval card in Studio can take minutes and is never retried by the proxy). Only the stream ending, a 401 or a refused connection means the owner is gone: then every request in flight is answered with a JSON-RPC error (-32001), a short message goes to stderr, and the process exits non-zero; reconnect the client to continue. A failed POST answers just that request. A proxy ends when stdin closes (after a brief drain of replies in flight) and never asks Studio to quit. `--transport sse` needs this process to be the owner.

Two clients started at the same moment (Claude Desktop and Claude Code) both work: the first takes the lock and publishes `mcp: {pending: true}` in its very first write, the second waits up to 15 s for the real endpoint and then proxies.

`--sandbox` in proxy mode is sent to the owner as an `X-Vibe-Sandbox` header on the SSE stream and **enforced by the owner**: for that session the effective mode is the stricter of the owner's policy and the header (tighten-only, bound at connection time, applied to every tool, including content tools). Unknown values are rejected with 400. The owner advertises `mcp.caps: ["sandbox-header"]` in `hub.lock`; a proxy started with `--sandbox` refuses an owner without it.

### Stdout is the protocol channel

Over stdio, `vibe mcp serve` duplicates stdout to a private handle for the protocol and points file descriptor 1 at stderr for the rest of the process. A Go `fmt.Println`, a C library's `printf`, or a MotherDuck sign-in link printed by DuckDB therefore lands on stderr and cannot corrupt the JSON-RPC stream. Prompts, notices and logs are all stderr.

### Sandbox and "Ask before changes"

The effective policy is the workspace's: this person's chosen tier (default **read-write**) clamped to the committed `vibe.yaml` ceiling, and "Ask before changes" on unless this person turned it off and the ceiling allows it (`Workspace.AgentPolicy`). `--sandbox` can only make a session **more** restrictive, never less: a launch config, which may be shared in a dotfiles repo, can never grant more than the owner chose.

A write needs three gates (tier, writable connection, the person's approval); [agent-policy.md](agent-policy.md#writes-need-three-gates) owns that rule and its exact messages. What it means over MCP, where there is no one to show an approval card to:

- **While "Ask before changes" is on, an MCP client can read but any write is refused outright.** To let MCP clients change the workspace's local files without asking, turn the gate off in **Vibe Studio → Settings → Workspace → The agent**; there is no flag or environment variable that bypasses it from the MCP side.
- **A write to a connection that is not a local `duckdb`/`sqlite` file (Postgres, MotherDuck, ...) is refused even with the gate off**, because approval is forced for those connections. Nothing is changed.
- To make those changes with approval, open the workspace in Studio, which shows its approval card (naming the non-local connection) while `vibe mcp serve` proxies to it.

Reads are not gated by approval; what the agent may read, and the never-list, are in [agent-policy.md](agent-policy.md). Every denial returns its reason as a tool error, and `list_connections` tells a client what is connected.

### Take over

Studio, opening a workspace a headless `vibe mcp serve` owns, can offer to take over: `hub.TakeOver(ctx, ws, "Vibe Studio", timeout)` signals the server (`SIGUSR1`; on Linux through a pidfd, re-verifying the lock still names that pid after the pid is pinned, so a recycled pid is never signalled; it checks it is a live owner on this host that advertised release control; if a client auto-reconnects and a different releasable owner appears while waiting, that one is asked too), the server closes its session, releases the lock and **exits with status 0**, and Studio gets the lock. The Claude session that was using it sees its server end and must reconnect (Claude Code: `/mcp`; Claude Desktop: restart the server or the app); the reconnected `vibe mcp serve` then proxies to Studio. Nothing is deleted, and open notebooks are plain files so they are unaffected.

### For Studio (#111)

```go
hold, err := hub.AcquireHub(ws, hub.AppStudio, hub.WithReleaseHandler(onReleaseRequested))
var held *hub.HeldError              // errors.Is(err, hub.ErrHubHeld): open read-only, or take over
hold, err = hub.TakeOver(ctx, ws, hub.AppStudio, 10*time.Second, opts...)
sess, issues, _ := workspace.OpenSession(ctx, ws, logger)   // only after the lock is held
m, err := hub.StartWorkspaceMCP(ws, sess, hold, hub.WithMCPRememberPort())
m.Handler.SetApprover(approve)       // Studio's approval cards; proxies get them too
m.URL(); m.Token()                   // for the Settings screen; token is a secret
m.Stop()                             // MCP off: clears the endpoint from hub.lock
hold.Release()                       // after closing the session
cfg, _ := hub.BuildClaudeConfig(ws.Root(), exe)   // exe from hub.LocateVibe()
holder, err := hub.ReadHolder(ws.Root())          // who holds it, without the lock
```

How #111 wires it (`studio/workspace.go`):

1. **Opening a workspace:** `AcquireHub` first. On `ErrHubHeld`, read `held.Holder` and show the read-only banner (SPEC 7 C): "Open read-only" opens `workspace.Open` only (stores are files, so notebooks/queries/skills stay editable) and does **not** call `OpenSession` for the local `duckdb` connections, which show `locked-by(pid)`; "Take over" calls `TakeOver`, then continues as the owner. `ErrNotReleasable` or a timeout is shown as the reason, never a hang.
2. **Releasable owner:** pass `WithReleaseHandler` (one per process; Studio holds one workspace at a time). The handler must ask the person if needed, close the session, stop MCP, `Release()`, and switch the window to the launcher or read-only. A second releasable hold in the process is an error (`ErrOneReleasableHold`).
3. **MCP switch (Settings → Workspace → Claude and other tools):** On = `StartWorkspaceMCP` then `m.Handler.SetApprover(...)` wired to the existing approval card (an approval can take minutes; the proxy waits). Off = `m.Stop()`. Reflect whether a holder advertises MCP with `ReadHolder`. Wrap the handler with `WithMCPHandlerWrap` if Studio needs its own lock around calls.
4. **Copy Claude Desktop / Claude Code config:** `hub.BuildClaudeConfig(ws.Root(), exe)` with `exe` from `hub.LocateVibe()`; copy `.Desktop` or `.Code`. Never show or copy the token with these.
5. **Workspace switcher:** release the old workspace's hold (stop MCP, close the session, `hold.Release()`) **before** acquiring the new one. The release signal is process-wide, so only one releasable hold may exist at a time, and a second `WithReleaseHandler` fails with `ErrOneReleasableHold`.
6. **Quit/close:** `m.Stop()`, close the session, then `hold.Release()` (the lock goes last).

### Copy Claude config

`hub.BuildClaudeConfig(root, exe)` returns the two snippets Studio copies. **Neither contains a token**: they launch `vibe mcp serve --workspace DIR` over stdio, and when Studio owns the workspace that process reads the token from `hub.lock` at run time.

Claude Desktop (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "vibe-acme-analytics": {
      "command": "/path/to/vibe",
      "args": ["mcp", "serve", "--workspace", "/path/to/acme-analytics"]
    }
  }
}
```

Claude Code:

```
claude mcp add vibe-acme-analytics -- /path/to/vibe mcp serve --workspace /path/to/acme-analytics
```

The server name is `vibe-<folder>` so two workspaces do not collide. With "Ask before changes" on (the default), the client can read but every write is refused until the gate is off or Studio owns the workspace.

### Security notes

- `hub.lock` is `0600` and holds the endpoint and bearer token of a local server. The trust model is **same user only**: a reader refuses the file unless it is a regular file owned by the current uid with no group or world permission bits, ignores it unless the flock is really held, and accepts only a plain-`http` loopback URL, so a lock file committed to a cloned repository (or left by a crash) can never point a client, or its token, anywhere.
- The lock file is never written through a link: it is opened with `O_NOFOLLOW`, and must be a regular, single-link file owned by you (a committed `hub.lock -> victim` symlink, a hard link or a FIFO is refused, not truncated or hung on); `.vibe-local` itself must be a real directory owned by you.
- The token never appears on a command line, in the Claude config snippets, in the structured logs, or in error messages. It is regenerated each time a server starts. The one place it is printed is `vibe mcp serve --transport sse`, which writes it to stderr once so a network client can be configured; with stdio (what the Claude snippets use) it is never printed. Anything that can read your files as you can read it; that is the same trust as your own keychain-free local config.
- A proxy sends the token only as an `Authorization: Bearer` header.
- `SIGUSR1` is only sent to a pid whose lock file says it advertised release control, on this host, with the lock verified held and (Linux) the process pinned by a pidfd. One signal listener exists per process and is never torn down, so a stray signal cannot kill a Vibe process.

### SSE transport security

Both `--transport sse` and Studio's built-in MCP server bind to
`127.0.0.1` only and require a random bearer token, generated fresh each
time the server starts and printed to stderr (`--transport sse`). Studio never
shows it: the copied Claude config launches `vibe mcp serve`, which reads it from
`hub.lock` (same user only). A request is rejected if it:

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

See "Copy Claude config" above: no passphrase and no token are needed. The older `VIBE_PASSPHRASE` setup applied to `.vibe` files and is gone.

## Tool list

`vibe mcp serve` (owner or proxy) and Studio's SSE server all advertise exactly
`agents.ListToolsForMode(agents.ModeMCP)` — every non-task-mode tool
(`run_query`, `list_connections`, `introspect_schema`, `describe_table`, `create_record`,
`create_table`, `present_data`, skills, notebook-authoring tools, etc.).
Task-mode-only tools (`preview_query`, `write_cell`, `need_more_info`) are
never listed or dispatchable over MCP — those exist purely for the in-app
task-mode notebook-cell authoring loop (#66).
