# Use Vibe from Claude and other MCP clients

`vibe mcp serve` lets an AI client that speaks the Model Context Protocol (MCP) work in a Vibe workspace: list connections, inspect schemas, run queries, read skills and notebooks, and (when you allow it) author notebooks and change data. It is the same tool set, with the same limits, as Studio's built-in agent. Reference: [MCP reference](reference/mcp.md).

You do not need Studio open, and you do not need a Vibe-side API key: the client brings its own model.

## Before you start

You need a workspace ([Quickstart](quickstart.md)) and the **absolute path** of the `vibe` binary and of the workspace folder:

```sh
command -v vibe          # for example /home/you/.local/bin/vibe
pwd                      # run inside the workspace
```

Use the absolute path to `vibe` in client configs: desktop apps often start with a shorter `PATH` than your shell.

## Claude Code

```sh
claude mcp add --scope user vibe-sales-demo -- /home/you/.local/bin/vibe mcp serve --workspace /home/you/sales-demo
```

- Put the server name first; everything after `--` is the command.
- `--scope user` makes it available in every project. The default (`local`) registers it for the directory where you ran the command only. `--scope project` writes a `.mcp.json` you can commit.
- In Claude Code, `/mcp` shows whether it connected.
- The name `vibe-<folder>` keeps two workspaces from colliding.

## Claude Desktop

Edit `claude_desktop_config.json` (Claude Desktop, Settings, Developer, Edit Config) and restart the app:

```json
{
  "mcpServers": {
    "vibe-sales-demo": {
      "command": "/Users/you/.local/bin/vibe",
      "args": ["mcp", "serve", "--workspace", "/Users/you/sales-demo"]
    }
  }
}
```

## Let Studio write the config

With the workspace open in Studio, **Settings, Workspace, Claude and other tools** has **Copy Claude Code config** and **Copy Claude Desktop config**. They copy to your clipboard (you paste them yourself), use the right paths and never contain a token. The Claude Code command Studio copies has no `--scope`, so it registers the server for the current directory only; add `--scope user` for every project.

## Cursor, Codex, Gemini CLI, OpenCode and others

Any client that can launch a stdio MCP server takes the same three pieces: the command `vibe`, the arguments `mcp serve --workspace DIR`, and no environment variables.

| Client | How to register |
|---|---|
| Cursor | Add the same `mcpServers` JSON as Claude Desktop to `~/.cursor/mcp.json` (or a project's `.cursor/mcp.json`). Not tested by us. |
| Codex | `codex mcp add vibe-sales-demo -- /path/to/vibe mcp serve --workspace /path/to/sales-demo` (writes `~/.codex/config.toml`) |
| Gemini CLI | `gemini mcp add -s user vibe-sales-demo /path/to/vibe -- mcp serve --workspace /path/to/sales-demo` (default scope is the current project) |
| OpenCode | `opencode mcp add` (interactive), or edit `opencode.json` |

How those four behave (environment scrubbing, approvals, trust prompts) was explored in the [bring-your-own-agent findings](plans/spike-byo-agent-findings.md). Some clients start the server with a trimmed environment; Vibe's stdio server needs none, but `env:` sign-ins read their variables from that environment, so give such a client the variables or use the workspace `.env` ([connection recipes](connections.md#sign-in-methods)).

### A client that only speaks network transport

```sh
vibe mcp serve --transport sse --addr 127.0.0.1:8765
```

It binds to loopback only and prints a random bearer token to stderr once, which the client must send as `Authorization: Bearer ...`. This process must be the workspace owner. Details and the exact checks: [SSE transport security](reference/mcp.md#sse-transport-security).

## What the client may do

Vibe's limits are the same as for the built-in agent ([agent policy](reference/agent-policy.md)):

- **Sandbox tiers.** **Look only** (`read-only`) reads only. **Change rows** (`read-write`, the default) can also insert, update and delete rows. **Full control** (`full`) can also create, drop and alter tables. Your choice lives in Settings, Workspace, The agent; a committed `vibe.yaml` can only make it stricter, never looser. `vibe mcp serve --sandbox read-only` tightens a single session further.
- **Approvals.** A write needs three things at once: the tier allows it, the connection is writable (`read_only: false`), and **you approve it**. "Ask me before it changes data" is on by default.
- **Over plain `vibe mcp serve`, nobody is there to approve.** So with "Ask me before it changes data" on, the client can read but every write is refused. Turn it off in Studio (Settings, Workspace, The agent) to let a client change **local** DuckDB and SQLite files without asking.
- **Non-local writes are never silent.** A write to Postgres, MySQL, MotherDuck or any connection that is not a local file always needs your approval, even with the gate off. Headless MCP has no way to ask, so it **refuses** them and changes nothing.
- **Want approvals?** Open the workspace in Studio and press **Start** under Settings, Workspace, Claude and other tools. A client's `vibe mcp serve` then passes through to Studio, and every write shows an approval card in the Studio window.
- Credentials are never visible to the client. Tool results are size-limited, and the agent cannot read files outside the workspace's `data/` folder.

## One owner per workspace

DuckDB allows one process to own a workspace database at a time. If Studio has the workspace open with MCP on, `vibe mcp serve` proxies to it (so Claude Code and Studio work together). If Studio has it open with MCP off, `vibe mcp serve` stops and says so. If a headless server owns it and you open Studio, Studio offers to take over; the client must then reconnect (Claude Code: `/mcp`). See [Troubleshooting](troubleshooting.md#workspace-is-in-use-by-another-process).
