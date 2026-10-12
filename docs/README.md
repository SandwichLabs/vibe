# Vibe documentation

New here? Start with [Install](install.md) and the [Quickstart](quickstart.md). To understand why Vibe is shaped the way it is, read [`direction.md`](direction.md), then the reference for the surface you are touching. Architecture and conventions for contributors are in [`../CLAUDE.md`](../CLAUDE.md).

## Using Vibe

Task-oriented guides for people who use Vibe. They are written to be linkable by heading.

| Doc | What it is |
|---|---|
| [`install.md`](install.md) | The install script and its options, supported platforms, macOS and Gatekeeper, upgrade, uninstall |
| [`quickstart.md`](quickstart.md) | Install to a first query, a notebook and Claude Code over MCP, with no accounts |
| [`connections.md`](connections.md) | Recipes for every connection type and sign-in method (the field-level reference is [`reference/connections.md`](reference/connections.md)) |
| [`mcp-clients.md`](mcp-clients.md) | Claude Code, Claude Desktop and other MCP clients; sandbox tiers and approvals in plain words |
| [`troubleshooting.md`](troubleshooting.md) | Symptom-to-fix list, where the logs are, how to report a bug |
| [`privacy.md`](privacy.md) | What leaves your machine (nothing goes to Sandwich Labs) |
| [`architecture.md`](architecture.md) | An outsider-level overview of how the pieces fit |

> **The "using Vibe Studio" agent skill derives from these pages.** [SandwichLabs/agent-marketplace#1](https://github.com/SandwichLabs/agent-marketplace/issues/1) builds a skill for agents and support from `install.md`, `quickstart.md`, `connections.md`, `mcp-clients.md`, `troubleshooting.md` and `reference/`. These docs are the single source: change them here, keep headings stable (other docs and the skill link to them), and the skill follows.

## Product

| Doc | What it is |
|---|---|
| [`direction.md`](direction.md) | The north star: workspaces, connections, the eight primitives, and (§9) what changed from the plan |
| [`plans/workspaces-and-connections.md`](plans/workspaces-and-connections.md) | The implementation plan for epic #105 (delivered; "as built" summary at the top) |
| [`plans/bring-your-own-agent.md`](plans/bring-your-own-agent.md) | Accepted plan for epic #157: OpenAI, Ollama and Cloudflare providers (done), your own Claude Code / Codex / Gemini CLI / OpenCode over MCP, then in Studio's panel over ACP, and the security posture of that mode |
| [`plans/open-source-launch.md`](plans/open-source-launch.md) | The open-source launch plan (epic #51): audit findings, decisions (name, licence, history, reporting, DCO) and the v0.1.0 shape |
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

## Contributing

| Doc | What it is |
|---|---|
| [`contributing/rodney.md`](contributing/rodney.md) | Driving Studio in a headless browser with Rodney for QA |
| [`../tools/ast-grep/`](../tools/ast-grep/sgconfig.yml) | The architecture lint rules CI runs (`ast-grep scan -c tools/ast-grep/sgconfig.yml cmd internal studio`) |

## Packaging

| Doc | What it is |
|---|---|
| [`packaging.md`](packaging.md) | How v0.1.0 ships (unsigned tarballs and `install.sh`), the macOS spike findings, file association wiring, and the later signed-distribution plan (epic #77) |

## Reviews

| Doc | What it is |
|---|---|
| [`reviews/cleanup-2026-10/FINDINGS.md`](reviews/cleanup-2026-10/FINDINGS.md) | The post-#105 cleanup survey (#137) and its review verdicts |
| [`reviews/arch-2026-10/`](reviews/arch-2026-10/) | The architecture review for #54: rubric, findings, judge rulings. Its ast-grep rules are in `tools/ast-grep/` |

## Archive

[`archive/`](archive/README.md) holds superseded plans and research (Access parity, the original MVP spec, early system design). It is history, not guidance.
