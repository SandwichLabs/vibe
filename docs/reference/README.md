# Vibe v1.0 Reference

> **Changing (epic #105).** `workspaces.md` (#109) describes the folder-shaped workspace that replaces the single `.vibe` file; its Go packages, the CLI (`vibe workspace`, `query`, `import`, `export`, `studio`, #113) and MCP (#112) are on it, and Studio switches over in #111. The other references still describe the current single-file build. `connections.md` arrives with #115.

This is the shipped reference documentation for the Vibe application surface —
a local-first, encrypted, single-file agentic data workspace. It is split by
surface so each area is easy to find:

| Reference | Covers |
|---|---|
| [`notebooks.md`](notebooks.md) | **Notebooks** (sql/markdown/a2ui cells) and the **A2UI** component reference |
| [`workspaces.md`](workspaces.md) | **Workspaces**: the folder layout, `vibe.yaml`, connection scopes and precedence, the agent limit, file formats, conflicts |
| [`import-office.md`](import-office.md) | **Import** (CSV/XLSX) and table/query **export** (CSV, Parquet, XLSX, JSON) |
| [`templates.md`](templates.md) | Bundled starter **templates** |
| [`themes.md`](themes.md) | Studio **themes**: built-ins, the theme file format, colour roles, where preferences live |

A test in `internal/docsref/` keeps these docs honest: every A2UI component
type registered in `internal/a2ui`, every starter template in
`internal/templates` and every `vibe.yaml` / connections.yaml key in the
workspace schema must appear here.

> **Direction note (2026-09-23).** Per [`../direction.md`](../direction.md), the page
> system (`pages.md`, page directives, the web preview server, the Studio page
> editor/runtime views) was retired in #69 — Notebooks (#66) and A2UI artifacts
> (#67) are the only authoring surface now. An old `.vibe` file's pages are
> migrated to notebooks automatically the first time it is opened; the parser
> that migration depends on survives internally (`internal/pages`, parser-only,
> no executor/renderer), but there is nothing left to document about pages
> themselves. Forms, banded reports, macros, the switchboard, and App Mode —
> the Access application layer — were removed in #68. Do not reintroduce either
> surface.
