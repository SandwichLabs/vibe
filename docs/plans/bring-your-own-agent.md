# Bring your own agent and subscription — implementation plan

**Status:** ACCEPTED · 2026-10-07 · epic #157 (issues #147–#156) · owner decisions in §7 · review log in §9
**Touches:** `docs/direction.md` "One tool surface" and "Read-only until asked" (they hold for Vibe's tools, not for everything an external agent can do); CLAUDE.md *Design principles* "Trust rules" (see §4.5).

---

## 1. The shift in one paragraph

Today the only agent is Vibe's own loop (`internal/llm`, on fantasy), in Studio's agent panel, against a model reached by API key (`anthropic`, `gemini`) or a local OpenAI-compatible endpoint (`local`). People already pay for Claude, ChatGPT/Codex or Ollama and want to use that plan, and usually the agent they already use. This plan adds: (1) the missing providers for Vibe's own agent (OpenAI, an Ollama preset); (2) a better **MCP** experience for agents you run yourself (Claude Code, Codex, Gemini CLI, OpenCode in your terminal): approving writes from inside the agent, an orientation the server hands every client, and a `vibe mcp config` that mirrors Studio's copy-config buttons; (3) the same external agents in **Studio's agent panel** over the Agent Client Protocol (ACP). There is no agent-specific CLI (D10): an agent uses Vibe over MCP or through the same `vibe` commands a person types. External agents keep their own shell and file tools. Vibe is about organizing connections and sharing artifacts, not about being a security boundary around an agent you run as yourself: it says plainly what changes and otherwise treats an external agent like one you started in your own terminal (§4.5).

## 2. Where the subscription vendors stand (checked 2026-10-07)

These rules moved several times in 2026. Re-check before shipping each phase; the table is why the design avoids touching anyone's subscription credentials.

| Vendor | What a third-party app may do | Consequence for Vibe |
|---|---|---|
| **Anthropic** (Claude Free/Pro/Max) | OAuth tokens from consumer plans may only be used by Claude Code and Claude.ai; using them in any other product, including through the Agent SDK, breaks the Consumer Terms (docs clarified 2026-02-19; enforced since 2026-04-04). Running the person's own Claude Code (interactive, `claude -p`, or ACP via `claude-agent-acp`) draws on their plan. A move of that usage to a separate capped "Agent SDK credit" ($20 Pro, $100–200 Max) was announced 2026-05-13 to take effect 2026-06-15, and paused that day; Anthropic says it will give notice before any change. | Vibe never reads `~/.claude` credentials or runs a Claude login. The subscription route is **launching the person's installed Claude Code** (Phases B and C). It works today; the economics may change. |
| **OpenAI** (ChatGPT Plus/Pro, Codex) | "Sign in with ChatGPT" for third-party apps, expanded at DevDay 2026-09-29: open-source and locally running apps self-register, other commercial tools are waitlisted. Codex CLI signs in with ChatGPT itself. | ChatGPT plans are covered by launching the person's Codex. A native Sign in with ChatGPT provider is **deferred** (D8): Vibe does not qualify yet. |
| **Ollama** | Local server: no account. Ollama Cloud: an API key from ollama.com. | Local works today through `llm.provider: local`. Phase A adds a preset and the cloud key. |
| **Google** (Gemini) | API key; Gemini CLI signs in with a Google account and speaks ACP. | API key unchanged; Gemini CLI via Phases B and C. |

Sources: [The Register, 2026-02-20](https://www.theregister.com/software/2026/02/20/anthropic-clarifies-ban-on-third-party-tool-access-to-claude/5014546) · [Claude support: Agent SDK with your plan](https://support.claude.com/en/articles/15036540-use-the-claude-agent-sdk-with-your-claude-plan) · [The New Stack: Anthropic pauses the change](https://thenewstack.io/anthropic-pauses-claude-agent-sdk-subscription-change/) · [Zed: Anthropic billing and ACP](https://zed.dev/blog/anthropic-subscription-changes) · [The New Stack: Sign in with ChatGPT](https://thenewstack.io/sign-in-with-chatgpt/) · [OpenAI: Codex app-server token sharing](https://developers.openai.com/siwc/token-sharing-open-source/codex-app-server)

## 3. What exists (the seams)

- **Provider config** is app-global and viper-driven (`~/.vibe.yaml` `llm.*`, `VIBE_LLM_*`), resolved in `internal/llm/runner.go` `languageModel()`: `local` (openaicompat), `anthropic`, `gemini`. No `openai` provider; the default Anthropic model is the stale `claude-sonnet-4-5`. API keys: `internal/config/llmkey.go`, keychain items `llm-<provider>` ([llm-config.md](../reference/llm-config.md)). fantasy is pinned at v0.28.0 and ships `providers/openai` with Responses API support.
- **One tool surface.** `internal/agents` owns every tool, the sandbox tiers, the SQL allow-list, three-gate writes and the approval queue (`approval.go`). `Handler.SetApprover` routes approvals to Studio. Studio's own MCP server already has one (`studio/workspace.go` calls `enableApprovals("mcp")`), so a `vibe mcp serve` proxied to Studio gets Studio's approval card ([mcp.md](../reference/mcp.md)). Only a headless owner (`vibe mcp serve` with no Studio) has no approver and refuses gated writes. (The comment at `agents/handler.go:72` still says the SSE server has none; it is stale.)
- **Guardrails** (#70): the row budget (`agents/budget.go`) and empty-result explanation (`agents/empty_result.go`) live in the tool layer, so any agent gets them. The **grounding check** (`llm/grounding.go`) runs on the final answer inside Vibe's loop only.
- **Notebook editing.** Chat-mode tools `add_query_cell` and `create_dashboard` persist straight to the notebook store (`agents/notebook_cells.go`). **Task mode** (`llm/task.go`, `agents/notebook_task.go`) is Studio's "ask the agent to write this cell" dialog: `preview_query`, then a terminal `write_cell` or `need_more_info` captured by a recorder in `internal/llm`. Task-only tools are hidden unless the handler's mode is `task`.
- **MCP and the hub.** `internal/mcp` on mark3labs/mcp-go v0.44.1 (SSE with a fresh bearer token per start, stdio). In that library only the stdio and Streamable HTTP sessions can send MCP elicitation requests; the SSE session cannot. `hub.lock` publishes one endpoint per owner and there is no IPC beyond the SIGUSR1 release, so another process cannot ask Studio for a new endpoint. `internal/hub` (one owner per workspace; `vibe mcp serve` walks up to find the workspace, owns it when nobody does, proxies stdio to Studio when Studio owns it with MCP on, and fails with a clear message when Studio owns it with MCP off; `X-Vibe-Sandbox` can only tighten; `BuildClaudeConfig` for the copy-config buttons). So **an agent in a terminal can already use Vibe today** via `claude mcp add …`; what is missing is one-command setup, a way to approve writes, and the agent seeing the workspace's context.
- **Studio.** `studio/app_agent.go` (`AgentChatStream`, `AgentTask`), `AgentPanel.vue`, the approval card, Settings → App → "The agent's model".

## 4. Target model

### 4.1 Phase A — providers for Vibe's own agent

- `llm.provider: openai` with an API key (`OPENAI_API_KEY`, keychain `llm-openai`), Responses API through fantasy's `providers/openai`. Bump fantasy if needed.
- `llm.provider: ollama`: a preset over the openaicompat path, default endpoint `http://localhost:11434/v1`, a model list from `GET /api/tags`, and for Ollama Cloud an optional key (`OLLAMA_API_KEY`, keychain `llm-ollama`) and the cloud endpoint. A short check first on whether the cloud endpoint is OpenAI-compatible and which models do tool calling; if not, Cloud waits.
- Refresh per-provider default models (`defaultModelFor`). Settings shows a model list where the provider offers one.

### 4.2 Shared piece — how Vibe's tools reach an external agent

No new endpoint type and no new IPC. An external agent gets Vibe's tools the way any MCP client already does:

- **In the terminal (Phase B):** you add the **stdio** server `vibe mcp serve --workspace DIR [--sandbox TIER]` to your agent, as today (the snippet comes from Studio's copy-config buttons or `vibe mcp config`). That process owns the workspace when nobody else does, and proxies to Studio when Studio owns it. Stdio carries no token, so there is nothing secret to hand over.
- **In Studio's panel (Phase C):** Studio is the owner *and* the ACP client, so it can start a per-session endpoint in-process with no cross-process problem (§4.4).

**Approvals, by who owns the hub:**

| Owner | Where a gated write is approved |
|---|---|
| Studio | Studio's approval card. This already works today through the proxy. |
| `vibe mcp serve` itself (headless owner) over stdio, client declared elicitation | **New (B1):** an elicitation approver. The owner attaches an `ApproveFunc` that asks the client through MCP elicitation ("Run this UPDATE on prod_pg?"), using the client session found in the call's context (`server.ClientSessionFromContext`; the context already flows Handle → awaitApproval → approver). |
| Headless owner, client without elicitation | Refused as today, with a message saying how to approve: open the workspace in Studio, or use an agent that supports elicitation. |

Exactly one approver answers any request, so there is no race between a card and a prompt. 

**Agent support (B0 spike, observed 2026-10-08, [findings](spike-byo-agent-findings.md)):** Claude Code and the Codex TUI both declare and show MCP elicitation (form and url); openai/codex#13405 was reopened and Codex now ships it. Non-interactive runs (`claude -p`, `codex exec`) answer an elicitation at once without asking anyone, so the approver must treat that as a decline and keep its own deadline. Gemini CLI and OpenCode do not declare elicitation: with them, non-local writes are approved in Studio or refused.

**Context for the agent.** The MCP server returns a short orientation in the `initialize` response's `instructions` field (mcp-go `server.WithInstructions`), which clients add to the model's context. Every client gets it, whatever launched it. It covers the workspace name, the connection names and what they are, that Vibe's tools are available and `help` explains them, and the workspace's skills by name and description (bodies via `read_skill`, as today). Nothing secret is in it.

### 4.3 Phase B — agents you run yourself, over MCP

You start your agent the way you always do. Vibe adds no command to launch it (D10). What Phase B improves:

- **Approvals in the agent** (§4.2): elicitation for the headless stdio owner, so a write to a non-local connection can be approved from Claude Code without Studio open.
- **Orientation** (§4.2): server `instructions`, so the agent knows the workspace, its connections and skills from the first turn.
- **`vibe mcp config [--client claude-code|claude-desktop|codex|gemini|opencode] [--workspace DIR]`**: prints the snippet or command that registers this workspace's `vibe mcp serve` with that client. It mirrors Studio's "Copy Claude Desktop config" / "Copy Claude Code config" buttons (`hub.BuildClaudeConfig`), and both gain the extra clients. It prints only; it never edits a client's config file, never installs anything, and the snippet contains no token (stdio).
- **The human CLI is the agent CLI.** An agent with a shell may also run `vibe query --format json`, `vibe conn …` and the other commands you would. They behave identically for both; nothing is added for agents alone.

**Notebook editing (D2).** The chat-mode notebook tools (`add_query_cell`, `create_dashboard`) work over MCP. With its own file tools the agent can also edit `notebooks/*.vnb` and `queries/*.sql` directly; the files are plain and a Studio session picks the changes up through the watcher. Task mode is a Studio dialog and stays in Studio (§4.4).

### 4.4 Phase C — external agents in Studio's panel (ACP)

Studio becomes an ACP *client* (`github.com/coder/acp-go-sdk`). When you pick an external agent in Settings → App, the agent panel spawns it over stdio, `cwd` = the workspace root, one ACP session per panel conversation.

| Agent | Launch (default; overridable per person) | Signs in with |
|---|---|---|
| Claude Code | `claude-agent-acp` (npm `@agentclientprotocol/claude-agent-acp`) | your Claude Code login, or `ANTHROPIC_API_KEY` |
| Codex | `codex-acp` (npm `@agentclientprotocol/codex-acp`) | your Codex login (ChatGPT plan), or `OPENAI_API_KEY` |
| Gemini CLI | `gemini --acp` (`--experimental-acp` on older versions) | Google account, or `GEMINI_API_KEY` |
| OpenCode | `opencode acp` | its own config |
| Custom | any command in `agent.external.<NAME>` in `~/.vibe.yaml` | — |

- **Install and sign-in stay the agent's business.** Studio detects what is on `PATH` and shows the rest as "not installed" with the install command to copy; it never downloads or `npx`-runs anything itself. If an agent reports it is not signed in, the panel says how to sign in with that agent (e.g. run `claude`, then `/login`) and to retry.
- **Vibe's tools.** Studio starts an in-process **Streamable HTTP** MCP endpoint per ACP session: loopback, a fresh bearer token passed in the `session/new` `mcpServers` headers (never written to disk), and its own `agents.Handler` with the panel's approval card as approver, so task mode can be set per session. It closes with the session. This is used when the agent advertises `mcpCapabilities.http` (claude-agent-acp does; codex-acp is a B0 item). Otherwise Studio turns on its shared MCP server if it is off and passes the stdio `vibe mcp serve` proxy to it; that works, but without per-session task mode.
- **The panel** maps `session/update` onto what it already shows. Message chunks stream as text. Tool calls become chips: Vibe's own tools render as today, and the agent's own tools render as "Claude Code ran …" with output collapsed. `plan` updates render as a checklist. Stop sends `session/cancel`.
- **The agent's own permission requests** (`session/request_permission`, e.g. to run a shell command) appear as a card with the options the agent offers (allow once / always / reject). Vibe adds no auto-allow of its own.
- **Client capabilities** `fs` and `terminal` are advertised **off** in v1, so the agent uses its own tools.
- **Notebook task mode (D2).** The "write this cell" dialog works with an external agent. The session's handler is put in `task` mode and the dialog's request is sent as the prompt with the task instructions. The terminal `write_cell` / `need_more_info` call is captured by a recorder that moves from `internal/llm/task.go` into `internal/agents`, so both loops share it. That move takes `TaskEnvelope` / `EnvelopeCell` with it: Studio returns them over the Wails bridge from `AgentTask`, so `llm` keeps type aliases or `studio/app_agent.go` switches to the agents types. Nothing is written until you click Apply.
- **Grounding.** Vibe can't re-prompt inside another agent's loop. After the turn, Studio runs the existing grounding check over the text and the Vibe tool calls it saw (the check moves to `internal/agents` with a step type of its own; `llm.ToolStep` adapts to it). When it fails, Studio shows a "not checked against your data" badge with an "Ask it to verify" follow-up.

### 4.5 Security posture (D1, D9)

**Principle.** An external agent runs as you, with its own shell, and can do what you could do in a terminal: read files, run `duckdb` or `psql` against data you can already reach, read your keychain. Vibe does not try to wall that off. Vibe's value here is organizing connections, sharing artifacts and giving the agent good tools. So in external-agent mode Vibe:

1. **Says so once and keeps a visible marker.** The first time you use an external agent in a workspace, Vibe shows a short notice: "Claude Code runs as you, with its own tools. Vibe's read-only rules and approvals apply to Vibe's tools only." In Studio's panel this is a dialog, acknowledged once per person. For agents you run yourself over MCP, the same text is in `mcp.md` and in the `vibe mcp config` output. While a session is open, the Studio panel shows an "External agent" chip that opens the same text.
2. **Keeps Vibe's own tools as they are.** The tier, the allow-list, the three gates and always-approve for non-local connections still apply to every Vibe tool call. That costs nothing, and it is what makes approvals meaningful when the agent goes through Vibe.
3. **Adds nothing secret.** The agent inherits the environment it would have had in your terminal. Vibe adds nothing it resolved from the keychain or from a connection's `env:` sign-in. Over stdio MCP there is no token at all. In Studio the only Vibe value the agent gets is its own session endpoint's token, which dies with the session.
4. **Never takes a command from the folder.** Agent commands come only from your own `~/.vibe.yaml`, never from `vibe.yaml`, `.vibe-local/` or a bundle. Otherwise opening a cloned repo could run a program. This is about the repo, not the agent, and stays.

**Not done, by design:** environment scrubbing, sandboxing the agent, watching for the agent editing your Vibe settings, and limiting which files it may touch. The CLAUDE.md trust rule "never … anything the agent can read" is reworded to say it governs Vibe's agent and Vibe's tool surface. An external agent's boundary is your OS account and its own permission settings.

## 5. Delivery

Same process as epic #105: one issue at a time, Sonnet implementer, Opus adversarial review, live rodney QA for Studio steps.

### Phase A — providers (small)
- **A1** `openai` provider (API key, Responses API), fantasy bump if needed, default models refreshed, docs. **Done (#147, PR #159).**
- **A2** `ollama` preset: default endpoint, model list, Ollama Cloud key (after the compatibility check), Settings UI, docs. **Done (#148):** the Settings provider picker, `vibe config llm`, the model list from `/api/tags`, and the chat error hint. Ollama's docs show `https://ollama.com/v1` as an OpenAI-compatible base URL with an API key (checked 2026-10-07; they say the cloud API does not replay custom tool calls, so Chat Completions tool calling is expected to work but was not tried live here).
- **A3** `cloudflare` provider (#160): Cloudflare AI Gateway REST API (`/ai/v1/chat/completions`), `llm.account_id` / `llm.gateway`, Cloudflare API token. **Done, shipped with #148.** Not run against a live account (no token available).

### Phase B — agents you run yourself, over MCP (small–medium)
- **B0 Spike. Done (#149):** [`spike-byo-agent-findings.md`](spike-byo-agent-findings.md). Corrections it made: Codex supports elicitation; Codex and Gemini scrub the MCP server's environment (keychain access needs variables forwarded by name); the server's cwd is the agent's, so `--workspace` is always needed; Claude Code truncates `instructions` at 2,048 characters; under ACP both adapters ask permission for every Vibe tool call. No product code; findings go in `docs/plans/spike-byo-agent-findings.md`. For Claude Code, Codex, Gemini CLI and OpenCode, establish:
  1. the config snippet or command each uses to register a stdio MCP server;
  2. whether each shows the server's `instructions` to the model;
  3. elicitation as each actually shows it, end to end against an mcp-go stdio server (Codex was then believed to lack it; the spike found it does);
  4. for Phase C, ahead of time: `claude-agent-acp` and `codex-acp` driven from `acp-go-sdk` (initialize, new session, prompt, stream, cancel); their not-signed-in errors; their `mcpCapabilities`; and the `session/request_permission` shapes;
  5. runtime prerequisites (Node, etc.) to document.
- **B1** Elicitation approver for the headless stdio owner (`internal/hub` / `internal/mcp`):
  - attached only when the client declared the capability;
  - accept / decline / cancel map to the approval outcomes, and a timeout gives the existing "expired" result;
  - the stale approver comment in `agents/handler.go` gets fixed;
  - the server's `instructions` orientation is added.

  Tests drive an mcp-go client with and without elicitation through a real stdio owner.
- **B2** `vibe mcp config` and the matching Studio buttons for the extra clients (snippets from B0). Tests are golden snippets with no token.
- **B3** Docs: `docs/reference/external-agents.md` (setup per agent, the posture, approvals), and updates to `mcp.md`, `agent-policy.md` and the CLAUDE.md trust-rule wording.

### Phase C — Studio panel (medium–large)
- **C1** `internal/extagent`: the ACP client session, process lifecycle (process group, killed on close or quit), mapping events into the panel's model, and the per-session Streamable HTTP endpoint (§4.4) with the stdio fallback. An interface plus mock; contract tests against a fake ACP agent.
- **C2** Studio UI: the agent picker in Settings → App (stored in `~/.vibe.yaml` `agent.default` / `agent.external.<NAME>`; `docsref` gains a test for `~/.vibe.yaml` keys, or they are documented by hand), the notice dialog and chip, AgentPanel streaming for ACP sessions, the agent-permission card, Stop, and error states (not installed, not signed in, crashed).
- **C3** Notebook task mode for external agents (the shared recorder, plus the envelope types moving with it) and grounding moved to `internal/agents`, with the badge and "Ask it to verify". Keep the CLAUDE.md architecture tree current.
- **C4** Docs and live QA.

### Deferred
- Native Sign in with ChatGPT (D8).

## 6. Risks

| Risk | Mitigation |
|---|---|
| Anthropic changes subscription terms again (a credit cap, or ACP excluded). | Vibe holds no Claude credential and never signs in for you, so a change affects your billing, not Vibe's compliance. Keep §2 current, and show the agent's own error when a plan limit is hit. Running Claude Code yourself with Vibe as an MCP server is the most clearly permitted use. |
| Agents differ in MCP config, elicitation and ACP support, and all of it changes fast. | The B0 spike sets the compatibility list. Per-agent snippets and ACP launch recipes each live in one catalog. Contract tests use fake agents. Docs name the versions tested. |
| Without elicitation (Gemini CLI, OpenCode, and any non-interactive run), terminal users cannot approve non-local writes. | The Studio card when Studio is open, and a clear refusal message otherwise. Local-file writes follow your "Ask before changes" setting as today. |
| A cloned repo makes Vibe run a program. | Commands come only from per-person config. A hostile-`vibe.yaml` test covers it. |
| Two writers at once (the agent editing a notebook file while you edit it in Studio). | The existing compare-and-write, the watcher reload and the conflict UI. |
| Grounding is weaker for external agents. | The badge plus "Ask it to verify" in Studio, documented in `external-agents.md` for agents you run yourself. |

## 7. Decisions

**Owner, 2026-10-07:**
- **D1. External agents keep their own shell and file tools.** Vibe warns about the changed posture rather than restricting them.
- **D2. External agents may edit notebooks**, through Vibe's notebook tools and directly on disk.
- **D8. Native Sign in with ChatGPT is deferred.** Vibe does not qualify yet; ChatGPT plans are served through the person's Codex.
- **D9. Agents working from the terminal come first**, with the risks acknowledged. Vibe is about organizing connections and sharing artifacts, not adding security beyond what the host already allows: an agent can already reach the same data with `duckdb` or `psql`. Hence §4.5's short list.
- **D3. Phase order A → B (MCP for your own agents) → C (Studio panel).**
- **D4. Vibe never installs agents** (no downloads, no `npx`). It detects them and shows the install command.
- **D5. Vibe handles no sign-in**, for any agent or vendor.
- **D6. Vibe's own agent stays the Studio default** unless the person configures an external agent as their default.
- **D7. Agent commands come only from per-person config**, never from the folder.
- **D10. No agent-specific CLI.** `vibe` commands mirror what Studio's UI does. Agents use Vibe over MCP or through the same CLI a person uses. (`vibe agent` from v2 is dropped.)

## 8. Out of scope

- Reading or reusing any vendor's subscription credentials from disk (`~/.claude`, `~/.codex`, …).
- Native Sign in with ChatGPT (D8).
- Sandboxing external agents, scrubbing their environment, or guarding Vibe's settings files against them (D9).
- ACP `fs`/`terminal` client capabilities (reviewing an agent's file edits as diffs in Studio): later.
- Multiple agents in parallel in one panel.

## 9. Review log

**v1 → v2, 2026-10-07 (Fable adversarial review; findings checked against the code by the orchestrator):**
1. *Major.* Studio's SSE server already has an approver (`studio/workspace.go` `enableApprovals("mcp")`). §3 is corrected, and the Studio-card path reuses what exists.
2. *Blocker.* An SSE session cannot send elicitation in mcp-go v0.44.1, and Codex attaches URL servers only as Streamable HTTP. The terminal path is now stdio (`vibe mcp serve`), which supports elicitation. Studio's per-session endpoint is Streamable HTTP.
3. *Blocker.* `vibe agent` had no way to get a per-session endpoint from Studio (one endpoint in `hub.lock`, no IPC). Per-session endpoints were dropped for the CLI. The CLI reuses `vibe mcp serve` owner/proxy, so Studio's card applies when Studio owns the workspace and elicitation applies when the stdio process owns it. The MCP-off case fails early.
4. *Major.* Codex has no MCP elicitation (openai/codex#13405, not planned). This is stated in §4.2 and §6.
5. *Minor.* The ACP adapter package names and Gemini's `--acp` flag are corrected.
6. *Minor.* `${VAR}` expansion in `--mcp-config` headers is not documented. This is moot now that the CLI path is stdio with no token.
7. *Minor.* The Anthropic credit change's announcement date is corrected.
8. *Minor.* Moving the task recorder and grounding drags `TaskEnvelope` / `ToolStep` across the Wails bridge. This is now stated in §4.4 and C3.
9. *Minor.* `docsref` does not cover `~/.vibe.yaml` keys. B3 now adds a test or documents them by hand.
10. *Minor.* A "first answer wins" race between the card and elicitation could not occur: exactly one approver per owner (§4.2).

**v2 → v3, 2026-10-07 (owner):** dropped `vibe agent` (D10). Phase B is now MCP improvements:
- the elicitation approver;
- the orientation via the server's `instructions`;
- `vibe mcp config`, which mirrors Studio's copy-config buttons.

The agent picker's `~/.vibe.yaml` keys move to C2.
