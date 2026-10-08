# Spike #149 findings: external agents over MCP and ACP

Part of epic #157 ([bring-your-own-agent.md](bring-your-own-agent.md) §5 B0). No product code: the throwaway server, the ACP driver and a mock model server lived outside the repo and are not committed. Run on Fedora, 2026-10-08.

Every claim carries a label:

- **[observed]**: I ran it here; the command and the trimmed output are quoted.
- **[docs: URL]**: from vendor documentation, checked 2026-10-08, not run.
- **[source]**: read from the library or package source on disk.

Versions tested:

| Thing | Version |
|---|---|
| Claude Code | 2.1.295 (signed in as the owner; used with `--model haiku`) |
| Codex CLI (`@openai/codex`) | 0.162.0 |
| Gemini CLI (`@google/gemini-cli`) | 0.63.0 |
| OpenCode (`opencode-ai`) | 1.18.35 |
| `@agentclientprotocol/claude-agent-acp` | 0.88.0 (bundles Claude Code 2.1.293) |
| `@agentclientprotocol/codex-acp` | 2.1.1 (bundles Codex 0.159.3) |
| `github.com/coder/acp-go-sdk` | v0.13.5 (ACP protocol version 1) |
| `github.com/mark3labs/mcp-go` | v0.44.1 (same as the repo) |
| Node | v22.19.0 |

## 0. Method, and how Codex, Gemini and OpenCode ran without signing in

- A throwaway mcp-go v0.44.1 stdio server (`spike-mcp`) with `server.WithInstructions("…codeword PELICAN-7741…")`, `server.WithElicitation()`, and tools `caps`, `approve_write` (calls `RequestElicitation` with a one-boolean form) and `approve_empty` (a form with no fields). It logs the `initialize` params it received, the process environment and every elicitation result to a file. It can also serve Streamable HTTP with a bearer-token check.
- Claude Code: the owner's login, an isolated `--mcp-config mcp.json --strict-mcp-config`, a scratch cwd, `--model haiku`.
- Codex, Gemini and OpenCode: private npm prefix, private `HOME`/`CODEX_HOME`/`XDG_*`, **no sign-in**. To see what each client sends and shows the model, I pointed each at a tiny local **mock model server** (127.0.0.1) that records the request it receives and answers with one tool call, then echoes the tool result. Codex: a `model_providers.mock` entry in its private `config.toml`. Gemini: `GOOGLE_GEMINI_BASE_URL` plus a dummy key string. OpenCode: a custom `@ai-sdk/openai-compatible` provider. So "Codex run un-signed-in" below means "run against a mock model", which exercises the real MCP client code path.
- ACP: a Go driver on `acp-go-sdk` v0.13.5 that also tees every JSON-RPC line to a trace file, so the shapes below are raw wire shapes.

## 1. Summary and support matrix

**Result: the plan holds, with one correction (Codex does elicitation) and several things the plan did not know (§8).**

| | Claude Code 2.1.295 | Codex 0.162.0 | Gemini CLI 0.63.0 | OpenCode 1.18.35 |
|---|---|---|---|---|
| Register stdio server | `claude mcp add NAME -- CMD ARGS…` | `codex mcp add NAME -- CMD ARGS…` | `gemini mcp add -s user NAME CMD -- ARGS…` | `opencode mcp add` (interactive) or edit `opencode.json` |
| Stored in | `~/.claude.json` (local, user) or `./.mcp.json` (project) | `~/.codex/config.toml`; `.codex/config.toml` (trusted projects) | `~/.gemini/settings.json` (user), `./.gemini/settings.json` (project, the default) | `~/.config/opencode/opencode.jsonc`; `./opencode.json` |
| Server `instructions` reach the model | **Yes** [observed]; truncated at 2,048 chars | **Yes** [observed]; as the tool namespace description; 8 KB not truncated | **Yes** [observed]; in the first user turn, `<project_context>` block | **Yes** [observed]; system prompt, `<mcp_instructions>` block |
| Declares MCP `elicitation` | **form + url** [observed] | **form + url** [observed] | **No** [observed] | **No** [observed] |
| Elicitation shown to the person | Interactive dialog [observed]; `-p` auto-cancels in 2 ms [observed] | TUI form [observed]; `codex exec` auto-declines in 0.2 ms [observed] | n/a | n/a |
| Env passed to the MCP child | everything | **almost nothing** (no `DBUS_SESSION_BUS_ADDRESS`, no `XDG_RUNTIME_DIR`) [observed] | everything except `*TOKEN*`, `*KEY*`, … [observed] | everything |
| ACP adapter | `claude-agent-acp` | `codex-acp` | `gemini --acp` | `opencode acp` |
| `mcpCapabilities` | `http`, `sse` | `http` only | `http`, `sse` | `http`, `sse` |
| `authMethods` | `[]`, or 2 `terminal` methods if the client sends `auth.terminal: true` | `api-key`, `chat-gpt` | `oauth-personal`, `gemini-api-key`, `vertex-ai`, `gateway` | `opencode-login` |
| Not signed in | `initialize` and `session/new` succeed; `session/prompt` fails `-32000 "Authentication required"` | `session/new` fails `-32000 "Authentication required"` | `session/new` fails `-32000 "Gemini API key is missing or not configured."` | **no failure**: free "OpenCode Zen" models work with no account |
| Needs the CLI on PATH | No (bundles its own) | No (bundles its own) | Is the CLI | Is the CLI |
| Needs `node` on PATH | Yes (`#!/usr/bin/env node`, engines `>=22`) | Yes (shebang) | Yes (engines `>=20`) | No (native binary) |

## 2. Registering `vibe mcp serve` (question 1)

Target: `vibe mcp serve --workspace DIR [--sandbox TIER]`. Every command below was run with a private config root; `/usr/local/bin/vibe` does not exist here, so the registration was inspected, not connected.

**What matters for #151 across all four [observed]:**

- The server process's **cwd is the agent's cwd, not the workspace**. The env probe in the spike server logged `cwd=…/scratch/<agent-run-dir>` for all four. So `--workspace DIR` must always be in the snippet; relying on "walk up from cwd to a `vibe.yaml`" only works when the agent is started inside the workspace.
- Use an absolute path to the `vibe` binary (all four launch it without a shell).
- Args containing spaces survive in all four because they are stored as an array (`"/tmp/my ws"` round-tripped in Claude, Codex and Gemini).

### 2.1 Claude Code

```
claude mcp add vibe-acme -- /usr/local/bin/vibe mcp serve --workspace "/tmp/my ws"
```

Output: `Added stdio MCP server vibe-acme with command: /usr/local/bin/vibe mcp serve --workspace /tmp/my ws to local config` [observed, with `CLAUDE_CONFIG_DIR` pointed into a temp dir].

| Scope flag | Stored in | Notes |
|---|---|---|
| `--scope local` (**the default**) | `~/.claude.json` under `projects["<cwd where you ran it>"].mcpServers` [observed] | **Only loads when Claude Code is started in that directory.** |
| `--scope user` | `~/.claude.json` top-level `mcpServers` [observed] | Every project. |
| `--scope project` | `./.mcp.json` [observed] | Shared via git. Interactive sessions ask for approval before using it; `-p` loads it without asking [docs: https://code.claude.com/docs/en/mcp]. |

Stored entry: `{"type":"stdio","command":"/usr/local/bin/vibe","args":["mcp","serve","--workspace","/tmp/my ws"],"env":{}}` [observed].

Gotchas:

- `-e/--env` is variadic and eats the server name: `claude mcp add -e FOO=bar vibe-proj -- …` fails with `Invalid environment variable format: vibe-proj` [observed]. Put the name first: `claude mcp add vibe-proj -e FOO=bar -- …` works [observed].
- `--mcp-config` is also variadic: a positional prompt after it is swallowed; put the prompt first [observed].
- Server names may contain only letters, digits, hyphen, underscore [docs]. `vibe-<slug>` is fine.
- `${VAR}` and `${VAR:-default}` expand in `.mcp.json` `command`/`args`/`env` [docs]; there is a `timeout` field per server and `MCP_TOOL_TIMEOUT` (default about 28 hours) [docs].
- Windows: not verified. Docs only mention a `cmd /c` wrapper for `npx`-launched servers; a real `vibe.exe` should not need it [docs, unverified].

The Studio button's current snippet (`hub.BuildClaudeConfig().Code`, `claude mcp add NAME -- exe …`) uses the **local** scope, so it works only from the directory where it was pasted. That is a latent bug in the existing button, not only a #151 concern (§8).

### 2.2 Codex

```
codex mcp add vibe-acme --env FOO=bar -- /usr/local/bin/vibe mcp serve --workspace "/tmp/my ws"
```

Written to `$CODEX_HOME/config.toml` (default `~/.codex/config.toml`) [observed]:

```toml
[mcp_servers.vibe-acme]
command = "/usr/local/bin/vibe"
args = ["mcp", "serve", "--workspace", "/tmp/my ws"]

[mcp_servers.vibe-acme.env]
FOO = "bar"
```

- `codex mcp add` only writes the global file. A project-scoped `.codex/config.toml` is documented ("trusted projects only") but has no add command [docs: https://developers.openai.com/codex/mcp].
- Config keys that matter [docs: https://developers.openai.com/codex/config-reference]: `env`, **`env_vars`** (names to forward from Codex's environment), `cwd`, `startup_timeout_sec`, `tool_timeout_sec`, `default_tools_approval_mode` and `tools.<tool>.approval_mode` (`auto | prompt | writes | approve`), `enabled_tools`/`disabled_tools`.
- **Codex starts MCP children with a scrubbed environment** [observed]. With `VIBE_PROBE_TOKEN=x VIBE_PROBE_PLAIN=y` exported, the child saw neither; `HOME`, `PATH`, `USER`, `LANG` were set, but `DBUS_SESSION_BUS_ADDRESS` and `XDG_RUNTIME_DIR` were **not**. Adding `env_vars = ["VIBE_PROBE_TOKEN", "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR"]` made all three arrive [observed]. Consequence: on Linux, `vibe mcp serve` under Codex cannot reach the Secret Service keychain, and cannot see `env:` credentials, unless the snippet forwards the right names (§9, #151).
- A Codex MCP tool call asks for Codex's own approval first (`MCP tool call requires approval, but approval policy is never` under `codex exec` with `approval_policy = "never"`) [observed]. `tools.<tool>.approval_mode = "approve"` skips that prompt [observed].

### 2.3 Gemini CLI

```
gemini mcp add -s user -e FOO=bar vibe-user /usr/local/bin/vibe -- mcp serve --workspace /tmp/ws
```

[observed; with a temp `HOME`.] Writes `~/.gemini/settings.json`:

```json
{ "mcpServers": { "vibe-user": {
    "command": "/usr/local/bin/vibe",
    "args": ["mcp", "serve", "--workspace", "/tmp/ws"],
    "env": { "FOO": "bar" } } } }
```

- **The default scope is `project`**, which writes `./.gemini/settings.json` in the cwd [observed]. Use `-s user` for a workspace-independent registration.
- In an untrusted folder, project **and user** servers show `Disabled` (`User-level servers are also suppressed in untrusted folders`) [observed with `gemini mcp list`]. Headless runs need `GEMINI_CLI_TRUST_WORKSPACE=true` or `--skip-trust` (otherwise exit 55) [observed].
- Other keys: `cwd`, `timeout` (default 600,000 ms), `trust`, `includeTools`/`excludeTools`; `$VAR`, `${VAR}` and `%VAR%` are expanded inside the `env` block [docs: https://geminicli.com/docs/tools/mcp-server].
- **Environment sanitization.** Gemini strips `GEMINI_API_KEY`, `GOOGLE_API_KEY` and anything matching `*TOKEN*`, `*SECRET*`, `*PASSWORD*`, `*KEY*`, `*AUTH*`, `*CREDENTIAL*` from the child's inherited environment unless it is named in the server's `env` block [docs; confirmed]. Observed: `VIBE_PROBE_TOKEN` did not arrive, `VIBE_PROBE_PLAIN`, `DBUS_SESSION_BUS_ADDRESS` and `XDG_RUNTIME_DIR` did. So `MOTHERDUCK_TOKEN` and similar are dropped unless the snippet lists them (value `"$MOTHERDUCK_TOKEN"`, so the file holds only the reference).
- Tool names are prefixed `mcp_<server>_<tool>` (e.g. `mcp_spike_approve_write`) [observed].

### 2.4 OpenCode

`opencode mcp add` is an interactive wizard (not scriptable) [observed in tmux]: name, then `Local`/`Remote`, then one "command to run" line, which it turned into an array (apparently split on whitespace; I did not try a path with spaces). It wrote `~/.config/opencode/opencode.jsonc` (global) [observed]:

```json
{ "$schema": "https://opencode.ai/config.json",
  "mcp": { "vibe-acme": { "type": "local",
    "command": ["/usr/local/bin/vibe", "mcp", "serve", "--workspace", "/tmp/ws"] } } }
```

For a snippet, print this JSON (a path with spaces is safe in the array form; the wizard is not known to handle one). Project scope is `./opencode.json` with the same `mcp` key [observed: used it for the mock run]. Optional keys: `environment` (object), `enabled`, `timeout` (ms to fetch tools, default 5000) [docs: https://opencode.ai/docs/mcp-servers]. `command` is an array, not a string. Tool names are `<server>_<tool>` [observed]. All env was inherited [observed].

## 3. Server `instructions` (question 2)

**Claude Code: yes [observed], truncated at 2,048 characters [observed + docs].**

```
claude -p "$Q" --model haiku --mcp-config mcp.json --strict-mcp-config --tools "" </dev/null
```

with `$Q` = "What is the orientation codeword you were told about by the MCP server? Do not call any tools. Reply with only the codeword, or UNKNOWN…". Results:

| Setup | Answer |
|---|---|
| Server codeword `WALRUS-1209` (a word the model cannot know) | `WALRUS-1209` |
| Control: no MCP server | `UNKNOWN` |
| Codeword at the end of 1,989 chars of instructions | found |
| Codeword at the end of 2,123 chars | `UNKNOWN` (cut off) |
| 2,610 and 8,110 chars | `UNKNOWN` |

Docs: "Claude Code truncates each tool description and each server's instructions at 2,048 characters" [docs: https://code.claude.com/docs/en/mcp].

**Codex: yes [observed].** The recorded model request carries the string in the description of a tool namespace, not in the system prompt: `{"type":"namespace","name":"mcp__spike","description":"This workspace has a secret orientation codeword: PELICAN-7741 …","tools":[{"type":"function","name":"approve_write",…}]}`. With 8,154 characters of instructions the description was 8,154 characters long and ended with the codeword sentence: **no truncation seen** [observed]. Docs: "Keep the first 512 characters self-contained" [docs: https://developers.openai.com/codex/mcp].

**Gemini CLI: yes [observed].** The request's first user turn (not `systemInstruction`) contains:

```
<loaded_context>
<project_context>
The following are instructions provided by the tool server 'spike':
---[start of server instructions]---
This workspace has a secret orientation codeword: PELICAN-7741. …
---[end of server instructions]---
</project_context>
```

(Docs say the instructions are "appended to the system instructions"; the wire shows the first user turn. Truncation not tested.)

**OpenCode: yes [observed].** System prompt contains `<mcp_instructions>\n  <server name="spike">\n    This workspace has a secret orientation codeword: PELICAN-7741. …\n  </server>\n</mcp_instructions>`. Truncation not tested.

**Also [observed]:** the instructions reach the model through both ACP adapters. With `claude-agent-acp` over an HTTP MCP server the model answered `PELICAN-7741`; with `codex-acp` the `initialize` result was logged by the server (the mock model's request was not inspected for that path).

Implications for #150:

- Budget the orientation at **well under 2,000 characters** and put the most important lines first. Claude Code cuts silently.
- The instructions are computed once, when the `MCPServer` is built. A client reads them once per connection, and MCP has no "instructions changed" notification. Connections or skills added later are not reflected until reconnect. Keep the text to stable facts (workspace name, connection names, "call `help`") and point at tools for anything live.
- Where to add it: `internal/mcp/server.go` `buildMCPServer(extra ...server.ServerOption)` already takes options; add `server.WithInstructions(...)` there so the stdio owner and Studio's SSE server both advertise it [source]. In proxy mode the `initialize` reply is Studio's, copied verbatim by the pipe, so Studio's builder is what matters.

## 4. Elicitation (question 3)

### 4.1 mcp-go v0.44.1 API [source + observed]

- `(*server.MCPServer).RequestElicitation(ctx, mcp.ElicitationRequest) (*mcp.ElicitationResult, error)` takes the session from `ClientSessionFromContext(ctx)` and returns `ErrNoActiveSession` or `ErrElicitationNotSupported` otherwise. It validates that a form request has `RequestedSchema` non-nil (`Validate()`).
- The stdio, Streamable HTTP and in-process sessions implement `SessionWithElicitation`. The SSE session does not (the plan already says this).
- How the server learns the client declared it: `sess, ok := server.ClientSessionFromContext(ctx).(server.SessionWithClientInfo)`, then `sess.GetClientCapabilities().Elicitation != nil` [observed working inside a tool handler; the log line `approve_write: client did NOT declare elicitation` fired for Gemini and OpenCode]. The raw `initialize` params are also available in a `Hooks.AddAfterInitialize` callback.
- `server.WithElicitation()` only adds a non-standard `"elicitation":{}` key to the server's own `capabilities` [source: `server.go:843`]. `RequestElicitation` itself does not check it.
- The repo's stdio path uses `StdioServer.Listen`, which registers a session and puts it in the context (`s.server.WithContext(ctx, &stdioSessionInstance)`), so `ClientSessionFromContext` works there too [source]. The existing `makeToolHandler` already calls `server.ClientSessionFromContext(ctx)`.
- Request context: `RequestElicitation` waits on `ctx.Done()` only. There is **no built-in timeout**; a pending request sits until the client answers or the context ends.

### 4.2 Claude Code [observed]

`initialize` params from Claude Code 2.1.295: `{"protocolVersion":"2025-11-25","capabilities":{"roots":{"listChanged":true},"elicitation":{"form":{},"url":{}}},"clientInfo":{"name":"claude-code","version":"2.1.295","title":"Claude Code"}}`.

Interactive (tmux). Form with one required boolean:

```
  MCP server “spike” requests your input
  Run this write on prod_pg? UPDATE orders SET paid=true
  ❯ * Approve: ☐
        Run it
    Accept    Decline
  Esc to cancel · ↑/↓ to navigate · Backspace to unset · Space to toggle
```

| Input | Server receives |
|---|---|
| Space (tick), then Accept | `{"action":"accept","content":{"approve":true}}` |
| Accept without ticking | blocked: `⚠ Approve: ☐ … This field is required` (no result) |
| Decline | `{"action":"decline"}` |
| Esc | `{"action":"cancel"}` |

Empty schema (`{"type":"object","properties":{}}`): the dialog is just `Accept  Decline`; Accept returns `{"action":"accept","content":{}}` [observed]. This is the cleanest approval prompt.

`-p` (non-interactive) mode: capability still declared, but the request is **cancelled immediately**: `approve_write: result after 2.203603ms: {"action":"cancel"}` [observed]. Docs: an `Elicitation` hook can auto-respond without a dialog [docs: https://code.claude.com/docs/en/mcp].

Timeout: with `MCP_TOOL_TIMEOUT=8000`, the model got `MCP server "spike" tool "approve_write" timed out after 8s` after 8 s, the dialog **stayed open**, and the server was **not told** (no context cancel); my handler returned `{"action":"cancel"}` only when I pressed Esc 27 s after asking [observed].

Through ACP (`claude-agent-acp`), the embedded Claude Code still declares elicitation but answers it with `decline` in 5 ms: `approve_write: result after 5.39ms: {"action":"decline"}` [observed]. So elicitation cannot be relied on in the Studio panel; it does not need to be, because Studio owns the hub there.

### 4.3 Codex [observed], and the plan is wrong about it

`initialize` params from Codex 0.162.0: `{"protocolVersion":"2025-06-18","capabilities":{"experimental":{"codex/auth-change":{}},"elicitation":{"form":{},"url":{}}},"clientInfo":{"name":"codex-mcp-client","version":"0.162.0","title":"Codex"}}`.

TUI, run against the mock model, form with one required boolean:

```
  Field 1/1 (1 required unanswered)
  Run this write on prod_pg? UPDATE orders SET paid=true
  Approve
  Run it
  › 1. True
    2. False
  enter to submit | esc to cancel
```

| Input | Server receives |
|---|---|
| Enter on True | `{"action":"accept","content":{"approve":true}}` |
| Down, Enter on False | `{"action":"accept","content":{"approve":false}}` (**accept with false**) |
| Esc | `{"action":"cancel"}` |

Empty schema renders as three choices, which map one to one onto the MCP actions [observed]:

```
  Run this write on prod_pg? (empty schema)
  › 1. Allow   Allow this request and continue
    2. Deny    Decline this request and continue
    3. Cancel  Cancel this request
```

Deny returned `{"action":"decline"}`. `codex exec` (non-interactive) auto-declines in 0.2 ms: `result after 223.761µs: {"action":"decline"}` [observed]. Docs: `approval_policy.granular.mcp_elicitations` controls whether elicitation prompts surface or are auto-rejected [docs: https://developers.openai.com/codex/config-reference].

Timeout: `tool_timeout_sec = 10` did not end a pending elicitation; the prompt and the server's wait were still live 16 s later [observed].

**Contradiction with plan §4.2 and §6.** The plan says Codex lacks elicitation (openai/codex#13405 "closed as not planned 2026-04-02"). Today: #13405 is **open** (re-opened and relabelled a spec-compliance bug, last comment 2026-04-04) and Codex 0.162.0 declares and renders elicitation. Merged PRs include "Fix MCP elicitation cancellation" (#44238, 2026-09-09), "Make URLs clickable in MCP elicitation prompts" (#51450, 2026-10-06) and "Signal abandonment of unanswered MCP elicitations" (#51611, 2026-10-07) [gh search, 2026-10-08]. Codex in the terminal can approve non-local writes without Studio, in the TUI. Not in `codex exec`.

### 4.4 Gemini CLI and OpenCode [observed]

Neither declares elicitation:

- Gemini 0.63.0: `"capabilities":{"roots":{"listChanged":true}},"clientInfo":{"name":"gemini-cli-mcp-client","version":"0.63.0"}`
- OpenCode 1.18.35: `"capabilities":{"roots":{}},"clientInfo":{"name":"opencode","version":"1.18.35"}`

With both, the spike tool took its "client does not support elicitation" branch. Those two keep today's refusal message.

## 5. ACP (question 4)

Driver: `acp-go-sdk` v0.13.5. Sent `clientCapabilities` is `{"auth":{},"fs":{}}` (fs and terminal off); `-terminal-auth` sets `auth.terminal: true`. The SDK's `ClientSideConnection` takes the child's stdin/stdout directly, so the process lifecycle is the caller's.

### 5.1 initialize

Request [observed]: `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientCapabilities":{"auth":{},"fs":{}},"clientInfo":{"name":"vibe-spike","version":"0"},"protocolVersion":1}}`

Responses (trimmed) [observed]:

```
claude-agent-acp 0.88.0:
 "agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true},
   "promptCapabilities":{"image":true,"embeddedContext":true},"auth":{"logout":{}},
   "sessionCapabilities":{"additionalDirectories":{},"close":{},"delete":{},"fork":{},"list":{},"resume":{}}},
 "agentInfo":{"name":"@agentclientprotocol/claude-agent-acp","title":"Claude Agent","version":"0.88.0"},
 "authMethods":[]

codex-acp 2.1.1:
 "agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true},
   "promptCapabilities":{"image":true,"embeddedContext":true}, …},
 "authMethods":[{"id":"api-key","name":"API Key","_meta":{"api-key":{"provider":"openai"}}},
                {"id":"chat-gpt","name":"ChatGPT"}]

gemini --acp (0.63.0):
 "agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true},
   "promptCapabilities":{"audio":true,"embeddedContext":true,"image":true}},
 "authMethods":[{"id":"oauth-personal"},{"id":"gemini-api-key"},{"id":"vertex-ai"},{"id":"gateway"}]

opencode acp (1.18.35):
 "agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true},
   "promptCapabilities":{"embeddedContext":true,"image":true}},
 "authMethods":[{"id":"opencode-login","name":"Login with opencode","description":"Run `opencode auth login` in the terminal"}]
```

- `claude-agent-acp` lists its two sign-in methods (`claude-ai-login`, `console-login`, both `"type":"terminal"`) **only if the client advertises `auth.terminal: true`** (or `_meta["terminal-auth"]: true`) [observed + source `acp-agent.js:1286`]. Its `authMethods` is `[]` whether or not you are signed in.
- `mcpCapabilities.http` is **true for all four**, so the plan's per-session Streamable HTTP endpoint works with every agent. `codex-acp` has no `sse` (do not offer SSE).

### 5.2 session/new with `mcpServers`

Wire shape [observed]:

```json
{"method":"session/new","params":{"cwd":"/abs/workspace","mcpServers":[
  {"name":"spike","command":"/abs/spike-mcp","args":[],"env":[{"name":"SPIKE_LOG","value":"…"}]},
  {"name":"spikehttp","type":"http","url":"http://127.0.0.1:18710/mcp",
   "headers":[{"name":"Authorization","value":"Bearer tok-123"}]}]}}
```

- The stdio entry has **no `type` field**; the http entry has `"type":"http"`. In Go: `acp.McpServer{Stdio: &acp.McpServerStdio{…}}` and `acp.McpServer{Http: &acp.McpServerHttpInline{Type:"http", …}}`.
- Both transports worked with `claude-agent-acp`. HTTP with a bearer header worked with `claude-agent-acp` and `codex-acp` [observed]: the server logged `http POST /mcp authorized=true`, the MCP `initialize` (from `claude-code 2.1.293` / `codex-mcp-client 0.159.3`), then the tool call. Server instructions reached the model through `claude-agent-acp`.
- MCP tools appear as `mcp__<name>__<tool>` inside Claude Code (`mcp__spikehttp__approve_write`), and as `{"server","tool"}` in Codex's `rawInput`.
- The response carries `configOptions` (not only `modes`/`models`): `mode` (Claude: `default`, `acceptEdits`, `plan`, `auto`, `bypassPermissions`; Codex: `read-only`, `workspace-write`, `agent` ("Auto review"), `agent-full-access`) and `model`. Set with `session/set_config_option`: `{"sessionId":…,"configId":"model","value":"haiku"}` [observed; worked].

### 5.3 prompt, streaming and `session/update` kinds [observed]

`session/prompt`: `{"sessionId":"…","prompt":[{"type":"text","text":"…"}]}`; the call returns `{"stopReason":"end_turn","usage":{…},"_meta":{"quota":{…}}}` when the turn ends.

Notification kinds seen (`params.update.sessionUpdate`):

| Kind | Seen from | Notes |
|---|---|---|
| `agent_message_chunk` | all | `content:{type:"text",text:"…"}`; Claude adds `messageId`; Codex prefixes a warning chunk when it lacks model metadata |
| `agent_thought_chunk` | not seen (haiku, no thinking) | in the SDK |
| `tool_call` | all | `toolCallId`, `title`, `kind` (`other`, `execute`, `think`), `status` (`pending`/`in_progress`), `rawInput`; Claude sends it with `rawInput:{}` and fills it in the next update |
| `tool_call_update` | all | `status` `completed`/`failed`, `content:[{type:"content",content:{type:"text",text}}]`, `rawOutput` |
| `available_commands_update` | all | slash commands/skills; Claude lists the owner's own skills (long) |
| `usage_update` | Claude, Codex | `size`, `used`, optional `cost` |
| `session_info_update` | Codex | `title`, `_meta.codex.threadStatus` |
| `plan` | not produced by these prompts | in the SDK (`Plan`, `PlanUpdate`) |

Codex also emits a **"Guardian Review"** `tool_call` (`kind:"think"`, title `Guardian Review`) before MCP tool calls in its default `agent` mode: Codex's automatic approval reviewer. With my mock model it failed (`guardian assessment was not valid JSON`) and denied the call. With a real model it would call the model again. Use `workspace-write` or `read-only` for predictable `request_permission` behaviour, or hide Guardian cards.

### 5.4 `session/cancel` [observed]

`{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":"…"}}` (a notification). The in-flight `session/prompt` returned `{"stopReason":"cancelled",…}` 11 ms later (`claude-agent-acp`, a long-output prompt cancelled after 4 s). Late chunks can still arrive before the response; Studio should stop appending after sending cancel.

### 5.5 `session/request_permission` [observed]

The agent sends a request; the client answers with an `outcome`.

`claude-agent-acp`, for **every call to a Vibe-like MCP tool** (read-only ones too) in the default `Manual` mode:

```json
{"id":0,"method":"session/request_permission","params":{"sessionId":"…","toolCall":{
   "toolCallId":"toolu_…","name":"mcp__spike__approve_write","title":"mcp__spike__approve_write",
   "kind":"other","status":"pending","rawInput":{"sql":"UPDATE t SET x=1"},"content":[],
   "_meta":{"claudeCode":{"toolName":"mcp__spike__approve_write","mcpServer":{"name":"spike","source":"dynamic"}}}},
 "options":[{"optionId":"allow-once","name":"Yes","kind":"allow_once"},
            {"optionId":"allow-with-updates","name":"Yes, and don't ask again for Approve Write commands","kind":"allow_always"},
            {"optionId":"reject","name":"No","kind":"reject_once"}]}}
```

`codex-acp`, in `workspace-write` mode:

```json
{"method":"session/request_permission","params":{"_meta":{"is_mcp_tool_approval":true},
 "sessionId":"…","toolCall":{"toolCallId":"call_1","kind":"execute","status":"pending"},
 "options":[{"optionId":"allow_once","name":"Allow","kind":"allow_once"},
            {"optionId":"allow_session","name":"Allow for this session","kind":"allow_always"},
            {"optionId":"allow_always","name":"Always allow","kind":"allow_always"},
            {"optionId":"cancel","name":"Cancel","kind":"reject_once"}]}}
```

(The `toolCall` here has **no title or rawInput**; the card must match `toolCallId` to the earlier `tool_call` update, whose `rawInput` is `{"server":"spike","tool":"approve_write","arguments":{…}}`.)

`gemini --acp`: `options` = `proceed_always_server` ("Allow all server tools for this session", `allow_always`), `proceed_always_tool`, `proceed_once` ("Allow", `allow_once`), `cancel` ("Reject", `reject_once`); `toolCall.title` = `approve_write(sql: UPDATE orders SET paid=true)`.

`opencode acp`: `options` = `once` ("Allow once"), `always` ("Always allow"), `reject` ("Reject").

Responses [observed on the wire]:

```json
{"id":0,"result":{"outcome":{"outcome":"selected","optionId":"allow-once"}}}
{"id":0,"result":{"outcome":{"outcome":"cancelled"}}}
```

- Claude, option `reject`: the tool update is `status:"failed"`, `rawOutput:"User refused permission to run tool"`, and the model reports it was refused.
- Claude, `outcome: cancelled`: `"Tool permission request failed: Error: Tool use aborted"`; the turn still ends `end_turn` (the model explains).
- **`allow-with-updates` ("don't ask again") makes `claude-agent-acp` write `./.claude/settings.local.json` in the session `cwd`**: `{"permissions":{"allow":["mcp__spikehttp__caps"]}}` [observed]. With `cwd` = the workspace root, that file lands in the user's workspace (possibly a git repo). Studio should either not offer `allow_always` options, or be ready to gitignore that file.
- Elicitation from an MCP server through either adapter was answered by the adapter itself (decline, 5 ms for Claude; cancel, 0.5 ms for Codex) [observed]. The ACP client never sees it.

### 5.6 Not signed in [observed]

Claude: `CLAUDE_CONFIG_DIR` and `HOME` pointed at empty dirs, `ANTHROPIC_*` unset:

```
INITIALIZE ok (authMethods: [])  →  NEW SESSION ok  →  PROMPT ERROR: {"code":-32000,"message":"Authentication required"}
```

So Studio cannot tell from `initialize` or `session/new`; the first prompt reveals it (`code -32000`, `message "Authentication required"`). With `auth.terminal: true` the sign-in methods are listed (`claude-ai-login`, `console-login`) but are listed signed in or not.

Codex: `session/new` itself fails: `NEW SESSION ERROR: {"code":-32000,"message":"Authentication required"}`, with `authMethods` `api-key` and `chat-gpt`. Gemini: `session/new` fails `{"code":-32000,"message":"Gemini API key is missing or not configured."}` (stderr also prints `When using Gemini API, you must specify the GEMINI_API_KEY environment variable`); the client is expected to call `authenticate` with a method id first [observed]. OpenCode: no error; it answers `hi` with its free Zen model [observed, one network call to OpenCode's service].

The CLI forms of the same condition, for the "how to sign in" text:

- `codex login status` prints `Not logged in`; `codex exec` retries `HTTP error: 401 Unauthorized, url: wss://api.openai.com/v1/responses` five times and falls back to HTTPS [observed].
- `gemini -p hi` with no auth prints `Please set an Auth method in your …/settings.json or specify one of the following environment variables before running: GEMINI_API_KEY, GOOGLE_GENAI_USE_VERTEXAI, GOOGLE_GENAI_USE_GCA`, exit **41** [observed].

### 5.7 Process lifecycle [observed]

- `gemini --acp` relaunches itself as a child node process (`node --max-old-space-size=… gemini …`). Killing only the PID the driver started left that child running as an orphan. Studio must kill the **process group**, as the plan says (C1).
- Codex: the TUI/CLI leaves a `codex app-server` daemon behind. The ACP adapter did not, in my runs. Not a Studio concern.
- `claude-agent-acp` and `codex-acp` start their own bundled CLI; they work with **no `claude`/`codex` on PATH** (`PATH=$NODEDIR:/usr/bin:/bin`) [observed].

## 6. Runtime prerequisites and what to show users (question 5)

Install commands to show (Vibe never runs them; D4):

| Agent | Command | Needs | Size seen |
|---|---|---|---|
| Claude Code | the Anthropic installer (already installed here) | its own | n/a |
| `claude-agent-acp` | `npm install -g @agentclientprotocol/claude-agent-acp` | **Node >= 22** (`engines`) | 2 MB (the Agent SDK + its bundled Claude Code arrive as dependencies) |
| Codex | `npm install -g @openai/codex` | Node >= 16 | 432 MB platform binary |
| `codex-acp` | `npm install -g @agentclientprotocol/codex-acp` | Node on PATH (shebang); bundles Codex | 426 MB |
| Gemini CLI | `npm install -g @google/gemini-cli` | **Node >= 20** | 96 MB |
| OpenCode | `npm install -g opencode-ai` | none (native binary via `optionalDependencies`; a `postinstall` links it inside the package) | 178 MB |

[observed: all installed with `npm install --prefix … --ignore-scripts`, then OpenCode's `postinstall.mjs` run by hand; it only links a binary inside its own package dir.]

- Both ACP adapters have a `#!/usr/bin/env node` shebang, so **`node` must be on the GUI app's PATH**. Studio launched from a desktop entry often has a minimal PATH that lacks nvm/asdf/Homebrew node. Detection must resolve `node` too and show a distinct "Node not found" state.
- The adapter versions track their own CLI (`codex-acp` bundles Codex 0.159.3, `claude-agent-acp` bundles Claude Code 2.1.293), not the one the user runs interactively. They share the user's login files (`~/.claude`, `~/.codex`) but not the CLI's version.
- Windows and macOS: not tested.

## 7. Detail for the three consumers

### #150: elicitation approver and instructions

1. Attach the approver only when `ClientSessionFromContext(ctx).(server.SessionWithClientInfo).GetClientCapabilities().Elicitation != nil`. Claude Code and Codex declare it. Gemini and OpenCode do not: keep the refusal message and name Studio (or Claude Code / Codex) as the way to approve.
2. Use a **form with no fields**: `RequestedSchema: map[string]any{"type":"object","properties":map[string]any{}}`. Claude shows Accept/Decline, Codex shows Allow/Deny/Cancel. No ambiguity from a checkbox. If a field is ever added, remember both clients can return `accept` with a false value (Codex "False" does exactly that).
3. Outcome map: `accept` approves; `decline`, `cancel`, an error, or a timeout deny. Decline means the person said no; cancel and errors mean "no answer".
4. **Treat a near-instant cancel/decline as "no UI".** `claude -p`, `codex exec`, and both ACP adapters answer in 0.2–5 ms. The denial message should say "this client could not show the prompt; approve in Studio or run the agent interactively", not "you declined".
5. Add a server-side timeout (`context.WithTimeout` around `RequestElicitation`) and map expiry to the existing "expired" result. Neither client tells the server when its own tool timeout fires (Claude's dialog stayed open 19 s past an 8 s `MCP_TOOL_TIMEOUT`; Codex's `tool_timeout_sec` did not apply during a pending elicitation). What a late answer does to mcp-go after the context ended was not tested.
6. Message text: SQL statement, connection name, truncated. Long messages were not tried in either UI.
7. `instructions`: see §3 (< 2,000 characters, front-loaded, stable facts only; add via `buildMCPServer`).

### #151: `vibe mcp config`

Always emit an absolute `vibe` path and `--workspace DIR`. Candidate output per client:

```
# claude-code (user scope so it works from any directory; project scope is the alternative)
claude mcp add --scope user vibe-acme -- /abs/vibe mcp serve --workspace /abs/acme

# codex  (config.toml fragment; or `codex mcp add vibe-acme -- …` for the basics)
[mcp_servers.vibe-acme]
command = "/abs/vibe"
args = ["mcp", "serve", "--workspace", "/abs/acme"]
env_vars = ["DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR"]   # keychain on Linux; plus the env: names the workspace's connections use

# gemini  (~/.gemini/settings.json fragment)
{ "mcpServers": { "vibe-acme": { "command": "/abs/vibe",
    "args": ["mcp","serve","--workspace","/abs/acme"],
    "env": { "MOTHERDUCK_TOKEN": "$MOTHERDUCK_TOKEN" } } } }   # names only, expanded at spawn

# opencode  (opencode.json fragment)
{ "mcp": { "vibe-acme": { "type": "local", "command": ["/abs/vibe","mcp","serve","--workspace","/abs/acme"] } } }
```

Notes for the generator:

- The env-forwarding lines are the part with real consequences (Codex and Gemini scrub). They hold only variable **names**, so the "no credential in any config file" invariant holds. Vibe knows which names a workspace needs from the connections' `env:` references.
- Print only; never edit the client's file (as planned). For Claude/Codex/Gemini, a printed `… mcp add` command is fine; for OpenCode print JSON, because its wizard is interactive and splits on whitespace.
- `--sandbox TIER` is just two more args in the array; no quoting issue.
- Fix the existing Studio "Copy Claude Code config" (`hub.BuildClaudeConfig().Code`): add `--scope user`, or say "run it from the project folder". Its golden test will need updating.
- Windows quoting and paths: not verified.

### #153: ACP client

- Spawn with a **process group** and kill the group on close (gemini leaves an orphan otherwise).
- Advertise `auth.terminal: true` if Studio wants `claude-agent-acp` to list its sign-in methods; otherwise show Vibe's own "run `claude`, then `/login`" text. For all four, treat `-32000` + "Authentication required"/"…API key…" as the not-signed-in state, from `session/new` (Codex, Gemini) or `session/prompt` (Claude). Do not treat OpenCode as needing sign-in.
- Offer HTTP MCP to all four. Pass the bearer token only in `headers`; the stdio entry has no `type`.
- **Double approval.** Both adapters send `session/request_permission` for each Vibe MCP tool call (reads too) in their default modes, before Vibe's own approval card for a gated write. Plan §4.4 says the agent's own permission requests appear as a card and Vibe adds no auto-allow. Expect one agent-permission card per Vibe tool call, plus Vibe's approval card for gated writes. Decide before C2 whether to (a) pass `allow_always` options through, (b) set the agent's mode (`acceptEdits`/`auto` in Claude, `agent-full-access` in Codex) which weakens the agent's own protection and contradicts "Vibe adds nothing", or (c) accept the noise. Hide `allow-with-updates` from Claude (it writes into the workspace), or gitignore `.claude/settings.local.json`.
- Match `request_permission.toolCall.toolCallId` to the earlier `tool_call` for the title and input (Codex sends a bare `toolCall`).
- `session/cancel`, then expect `stopReason: "cancelled"`.
- Pick `workspace-write`/`read-only` for Codex via `session/set_config_option` (`configId: "mode"`) to avoid Guardian reviews calling a model.
- Detection: look for the **adapter binary** (`claude-agent-acp`, `codex-acp`) and `node`, not for `claude`/`codex`.

## 8. Risks, surprises, and what contradicts the plan

1. **Codex does support elicitation (form and url) in the TUI.** Plan §4.2 ("Codex does not"), §4.2's agent-support paragraph and §6's third risk row are out of date. Codex `exec` still cannot (auto-declines).
2. **Env scrubbing breaks keychain and `env:` credentials under Codex and Gemini** unless forwarded by name. Plan §4.5 item 3 says the agent inherits what it would have had in the terminal, and Vibe adds nothing secret; true, but the child process of an MCP server does not inherit that env under two of the four clients. Without `DBUS_SESSION_BUS_ADDRESS` the Linux Secret Service is unreachable from `vibe mcp serve`.
3. **The existing "Copy Claude Code config" uses the default `local` scope**, so it only works from the directory it was pasted in.
4. **`instructions` are truncated at 2,048 characters by Claude Code** (silently). The plan's orientation (workspace, connections, skills by name and description) can exceed that on a real workspace.
5. **ACP adapters answer MCP elicitation themselves**, so elicitation is a terminal-only feature. Fine for the plan, as Studio is the owner under ACP. But the "stdio `vibe mcp serve` fallback" in §4.4 only works because it proxies to Studio; a headless owner under an ACP agent would see a declared-but-dead elicitation and refuse writes.
6. **Double permission prompts under ACP** and **`.claude/settings.local.json` written into the workspace** (§5.5, §7 #153).
7. **Not-signed-in is discovered late for Claude** (first prompt) and **never for OpenCode**.
8. **Orphaned child under `gemini --acp`**; process-group kill is required.
9. The adapters **bundle their own CLI at their own version**, and are the thing to detect. `node` must be resolvable from a GUI process.
10. No client times out an open elicitation for the server; the approver needs its own deadline.
11. `codex-acp`'s MCP support over ACP is HTTP only (no SSE).

## 9. What I did not verify

- Windows and macOS behaviour for any of this.
- Gemini's and OpenCode's truncation limit for `instructions`.
- Claude Code's `Elicitation` hook (docs only).
- Gemini's and OpenCode's ACP `mcpServers` with a live stdio entry (I used the config-file MCP server for OpenCode; for Gemini the stdio entry via `session/new` worked and reached the tool).
- A long statement in an elicitation message.
- A real (non-mock) Codex or Gemini model call. The mock model proves what each client sends and renders, not how a real model uses it.
- `codex-acp`'s full sign-in flow (`authenticate` with `chat-gpt`): not done by design (D5).
