# Vibe direction — agentic data exploration

**Date:** 2026-09-22
**Status:** Adopted. This document is the product north star and supersedes the Access-parity roadmap.
**Tracking:** GitHub issues #62–#70 (all shipped in PR #71, 2026-09-23). Follow-ups: #9 CLI notebook import/export, #56 Studio Home, #72 CLI commands on DuckDB, #73 notebook HTML/PDF/xlsx export.

---

## 1. The change in one paragraph

Vibe is a **local-first, encrypted, single-file agentic data workspace**: a `.vibe` file holds DuckDB tables, notebooks, skills, and an AI agent, and exposes all of it to the built-in Studio and to external agents over MCP. It is the local, offline, file-shaped counterpart to MotherDuck's platform (tables, notebooks, guides, MCP), built on code Vibe already has.

Vibe is **no longer** a Microsoft Access successor. Bound forms, subforms, field properties, the switchboard, App Mode, and macros are removed. The data-entry persona is out of scope. Datasheet editing stays, because a data tool where you cannot fix a typo is painful — that is MotherDuck's `query_rw`, not a form builder.

## 2. Why

Two reviews drove this:

1. **`motherduck-research.md`** maps MotherDuck's four primitives (Tables, Notebooks, Guides, Flights, all behind MCP). Vibe already had three under other names — the DuckDB engine, Pages, and the page directives — and lacked the fourth, Guides. The doc's proposed Zip64 container and Python Flights were rejected: they contradict encryption-by-default, the single Go binary, and "file is the app".
2. **`../schoolwork-workspace`** (a SandwichLabs experiment) has a working notebook cell model, a SKILL.md context layer, and an A2UI artifact spec, all written on `charmbracelet/fantasy`, the same agent framework as `internal/llm`. Its contracts are simpler than Vibe's page-directive DSL and were hardened against documented incidents. They port almost directly.

The Access application layer was most of the last five merged PRs and roughly a fifth of the codebase, and it served a persona the product is dropping. Removing it is what makes the remaining surface coherent.

## 3. The primitives

Seven nouns. Everything in the product is one of these.

### 3.1 Container
Unchanged. A `.vibe` is a SQLCipher database (AES-256, PBKDF2-HMAC-SHA512) holding `_vibe_*` metadata tables and a serialized DuckDB blob (`_vibe_duckdb`). Open materializes the blob to a hardened temp file; Save checkpoints it back. The plaintext DuckDB copy exists only while the file is open, in a 0700 dir under `$TMPDIR` (file 0600; its `.wal` inherits the process umask instead, protected by the 0700 dir rather than its own chmod), zeroed and removed on Close, with stale copies swept on the next start. Point `TMPDIR` at a tmpfs to keep it off disk. Table changes reach the encrypted file on Save. SQLCipher-side content (notebooks, queries, skills) is written immediately. Revert to last save discards table changes since the last Save by re-materializing the blob in place, without closing the file (#98). `internal/container`.

### 3.2 Tables
DuckDB, single engine, in-process. Schema DDL, CRUD, import (CSV, xlsx via the `excel` extension), export (CSV, JSON, xlsx, Parquet). The table browser keeps inline edit / insert / delete and filter-by-form. `internal/engine`, `internal/crud`, `internal/importer`.

### 3.3 Notebooks — #66
Replaces Pages. A notebook is JSON: a title and an ordered list of cells of three types:

| cell | fields | renders as |
|---|---|---|
| `sql` | `source`, `view` (`table` / `bar` / `line` / `stat`), `viewPinned`, `label` | a one-component A2UI page synthesized from the result |
| `markdown` | `source` | markdown |
| `a2ui` | `page` | an A2UI page (§3.5) |

Rules: no inter-cell references (the database is the only shared state); **outputs are never persisted**, every data cell re-runs on open; import is a whitelist rebuild, never trusted. Stored in `_vibe_notebooks`. Unpinned SQL cells auto-fit a view from the result shape; a stale flag marks output that no longer matches the editor.

The agent authors cells through **task mode**: a bounded tool loop that must `preview_query` before it may call one of two terminal tools, `write_cell` or `need_more_info`. The result is a JSON envelope built from recorded tool calls, never from model prose, with `unresolved` as an honest third outcome.

### 3.4 Skills — #62
Replaces the proposed Guides. Domain context in `SKILL.md` form (agentskills.io layout): frontmatter `name` + `description`, markdown body as instructions. The system prompt advertises name and description only; the agent fetches a body with `read_skill`. Built-in skills are embedded in the binary; user and domain skills live in `_vibe_skills` inside the file. Skills bound to a `table_name` load only when that table has rows. `task-*` skills are visible only to task mode. The import wizard writes one skill per imported table with a generated, authoritative "columns as stored" section.

### 3.5 Artifacts (A2UI) — #67
The one thing the agent draws. A page is `{ title, components[] }`: a flat list of thirteen typed components nested by `parent` id, closed color enum, unknown fields rejected. `stat`, `table`, `barChart`, `lineChart` accept inline data or a `sql` field that re-runs on every render, so an artifact is live, not a snapshot. Validation errors are JSON-path-prefixed and handed straight back to the model for one-turn self-correction. The Go side validates (`internal/a2ui`); the Vue frontend renders (`studio/frontend/src/a2ui/`). `present_data` renders inline in chat; `create_dashboard` persists an a2ui cell.

### 3.6 Agent + MCP — #63, #64, #70
One tool registry in `internal/agents`, consumed by the in-app runner in `internal/llm` **and** exposed by the MCP server in `internal/mcp`. Sandbox modes (`read-only` / `read-write` / `full`) and the approval queue apply identically to both. `vibe mcp serve <file>` runs the server headless over stdio for Claude Desktop, Cursor, and similar. Guardrails from the workspace: a grounding check that bounces unverified numeric claims, a zero-row explanation, and a tool-result budget.

### 3.7 Import / Export
The import wizard (CSV, xlsx) stays and gains skill generation. Exports stay. Banded report export becomes a notebook export (HTML, PDF, xlsx) — #73; today notebooks export as JSON only.

## 4. What stays, what goes, what changes

| | |
|---|---|
| **Stays** | Container, DuckDB engine, CRUD + table browser editing, import/export, agent sandbox + approval queue, MCP server, SchoolHouse UI, Query Explorer, Studio Home, CLI (`init`, `info`, `import`, `export`, `dump`, `verify`, `compact`, `rekey`, `passphrase`, `studio`) |
| **Goes** (#68, #69) | Forms, subforms, bound controls, field meta, relationships UI, switchboard, App Mode, macros / AutoExec, page directives, Go HTML renderer, web preview server, TUI page viewer/editor, starter-template forms |
| **Changes** | Pages → Notebooks (#66). Page directives → A2UI (#67). Pinned context → Skills (#62). Two tool surfaces → one (#63). TUI → SQL REPL + read-only browser (#69). Reports → dashboard cells + export. |

## 5. Sequencing

```
#63 single tool surface ─┬─► #62 skills ──────┐
#67 A2UI (Go validator,  │                    ├─► #66 notebooks + task mode ─► #68 remove Access layer ─► #69 TUI/web cut
     Vue renderer) ──────┘                    │
#64 vibe mcp serve ───────────────────────────┘        #70 guardrails (any time after #63)
#65 drop _vibe_sources (any time)
```

- **#63 first.** Everything else adds tools, and they should be added once.
- **#67 and #62** are independent and can run in parallel.
- **#66** is the milestone: when a notebook can be opened, edited, and authored by the agent, Pages can be migrated and the Access layer removed.
- **#68 and #69** are removals, one PR per bullet group so each is revertable.

## 6. Non-goals

- Bound forms, App Mode, macros, a data-entry persona.
- A Python execution plane (MotherDuck Flights). Scheduled SQL refresh may come later as a notebook feature.
- A Zip64 or plaintext container.
- Multi-user / Tailscale mesh. Still deferred; the single tool surface and MCP are the seams it would attach to.
- A terminal or server-side A2UI renderer.

## 7. Prior art and references

- `motherduck-research.md` — MotherDuck primitives and the local mapping.
- `../schoolwork-workspace/` — `docs/ARCHITECTURE.md`, `docs/AI_CELL_AUTHORING.md`, `src/notebook/`, `src/a2ui/`, `agent/{skills,task,tools}.go`.
- `docs/ms-access-parity-plan.md` and the docs it references — the superseded roadmap, kept for history.
