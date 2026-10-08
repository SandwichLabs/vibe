# Vibe documentation

Start with [`direction.md`](direction.md), then the reference for the surface you are touching. Architecture and conventions for contributors are in [`../CLAUDE.md`](../CLAUDE.md).

## Product

| Doc | What it is |
|---|---|
| [`direction.md`](direction.md) | The north star: workspaces, connections, the eight primitives, and (§9) what changed from the plan |
| [`plans/workspaces-and-connections.md`](plans/workspaces-and-connections.md) | The implementation plan for epic #105 (delivered; "as built" summary at the top) |
| [`plans/bring-your-own-agent.md`](plans/bring-your-own-agent.md) | Accepted plan for epic #157: OpenAI, Ollama and Cloudflare providers (done), your own Claude Code / Codex / Gemini CLI / OpenCode over MCP, then in Studio's panel over ACP, and the security posture of that mode |
| [`plans/spike-duckdb-hub-findings.md`](plans/spike-duckdb-hub-findings.md) | Findings from the DuckDB hub spike that fed the plan |
| [`plans/spike-byo-agent-findings.md`](plans/spike-byo-agent-findings.md) | Findings from the bring-your-own-agent spike (#149): per-agent MCP registration, `instructions`, elicitation and ACP behaviour for Claude Code, Codex, Gemini CLI and OpenCode |

## Reference

The shipped surface. [`reference/README.md`](reference/README.md) is the index; `internal/docsref` keeps it in step with the code.

| Doc | Owns |
|---|---|
| [`reference/workspaces.md`](reference/workspaces.md) | The folder layout, `vibe.yaml`, scopes, the CLI, Studio's launcher, `.vibe` bundles |
| [`reference/connections.md`](reference/connections.md) | Every connection type, fields, credentials, read-only, Test, extensions |
| [`reference/agent-policy.md`](reference/agent-policy.md) | The agent's SQL policy: tiers, the three-gate write rule, file and URL rules, the never-list |
| [`reference/mcp.md`](reference/mcp.md) | `vibe mcp serve` (stdio/SSE), client config, SSE auth |
| [`reference/llm-config.md`](reference/llm-config.md) | LLM provider configuration and the API key |
| [`reference/notebooks.md`](reference/notebooks.md) | Notebooks and the A2UI component reference |
| [`reference/import-office.md`](reference/import-office.md) | Import (CSV/TSV/XLSX) and export |
| [`reference/templates.md`](reference/templates.md) | Starter templates |
| [`reference/themes.md`](reference/themes.md) | Studio themes |

## Design

| Doc | What it is |
|---|---|
| [`design/workspaces-connections/`](design/workspaces-connections/index.html) | The mockup set and [spec](design/workspaces-connections/SPEC.md) for #106 (launcher, shell switcher, connections rail and editor, states, approval card). Open `index.html` in a browser; [`SHOTS.md`](design/workspaces-connections/SHOTS.md) lists the captured screenshots. The design record, not a description of the current UI. |

## Packaging

| Doc | What it is |
|---|---|
| [`packaging.md`](packaging.md) | Packaging and release plan (epic #77), file association wiring, extensions |

## Reviews

| Doc | What it is |
|---|---|
| [`reviews/cleanup-2026-10/FINDINGS.md`](reviews/cleanup-2026-10/FINDINGS.md) | The post-#105 cleanup survey (#137) and its review verdicts |
| [`reviews/ux-panel-2026-09-26/`](reviews/ux-panel-2026-09-26/) | The three-persona UX panel behind epic #93 |

## Archive

[`archive/`](archive/README.md) holds superseded plans and research (Access parity, the original MVP spec, early system design). It is history, not guidance.
