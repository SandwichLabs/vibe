# Vibe reference

This is the shipped reference documentation for the Vibe application surface —
an agentic data workspace: a folder of plain files that points at your data, which stays in its connections. It is split by
surface so each area is easy to find:

| Reference | Covers |
|---|---|
| [`notebooks.md`](notebooks.md) | **Notebooks** (sql/markdown/a2ui cells) and the **A2UI** component reference |
| [`workspaces.md`](workspaces.md) | **Workspaces**: the folder layout, `vibe.yaml`, connection scopes and precedence, the agent limit, file formats, conflicts |
| [`import-office.md`](import-office.md) | **Import** (CSV/TSV/XLSX) into a connection's table and table/query **export** (CSV, Parquet, XLSX, JSON) |
| [`templates.md`](templates.md) | Bundled starter **templates** |
| [`themes.md`](themes.md) | Studio **themes**: built-ins, the theme file format, colour roles, where preferences live |
| [`connections.md`](connections.md) | **Connections**: every type (DuckDB file, Postgres, MySQL, MotherDuck, SQLite, S3/GCS/R2, Azure, DuckLake, Iceberg, Delta), fields, credentials, read-only, Test, extensions |
| [`mcp.md`](mcp.md) | **MCP**: `vibe mcp serve` (stdio/SSE), client configuration, SSE auth and security |
| [`llm-config.md`](llm-config.md) | **LLM provider** configuration and the API key |
| [`agent-policy.md`](agent-policy.md) | **The agent's SQL policy**: what each tier allows, the three-gate rule for writes, file and URL rules, the never-list, `list_connections` |

A test in `internal/docsref/` keeps these docs honest: every A2UI component
type registered in `internal/a2ui`, every starter template in
`internal/templates` and every `vibe.yaml` / connections.yaml key in the
workspace schema must appear here.

> **Direction note.** Per [`../direction.md`](../direction.md), the page
> system (`pages.md`, page directives, the web preview server, the Studio page
> editor/runtime views) was retired in #69 — Notebooks (#66) and A2UI artifacts
> (#67) are the only authoring surface now. Older data-holding `.vibe` files (and their pages) are not opened or
> imported at all (#110 was dropped), so there is nothing left to document about pages. Forms, banded reports, macros, the switchboard, and App Mode —
> the Access application layer — were removed in #68. Do not reintroduce either
> surface.
