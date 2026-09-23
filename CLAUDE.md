# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Vibe is a **local-first, encrypted, single-file agentic data workspace**. A `.vibe` file bundles DuckDB tables, notebooks, skills (domain context for the agent), and an AI agent into one portable, passphrase-protected file, and exposes all of it to the built-in Studio desktop app and to external agents over MCP. Think of it as the local, offline, file-shaped counterpart to MotherDuck's platform (tables, notebooks, guides, MCP).

**North star:** `docs/direction.md`. Read it before planning any feature work. It defines the seven primitives (Container, Tables, Notebooks, Skills, Artifacts/A2UI, Agent+MCP, Import/Export), what is being removed, and the sequencing (issues #62–#70).

**Direction change (2026-09-22):** Vibe was previously aimed at Microsoft Access 97 application parity (bound forms, subforms, switchboard, App Mode, macros). That direction is **superseded**. The Access application layer was removed in #68; datasheet editing stays. The parity docs are kept for history and carry a superseded banner. Do not add forms, macros, nav, field meta, or App Mode.

**Status:** Container, encryption, DuckDB engine, CRUD, import/export, Wails+Vue Studio, agent + MCP are all working. The **primitive migration** is complete: one agent tool surface (#63), Skills (#62), A2UI artifacts (#67), Notebooks with agent task-mode authoring (#66), headless `vibe mcp serve` (#64), guardrails (#70), removal of the Access layer (#68 — forms/subforms/relationships-UI/switchboard/macros/App Mode/banded reports are gone, foreign keys are plain SQL, reports migrate into notebooks), and retirement of the page system (#69 — web preview server, page renderers, Studio page views/bindings, and the page agent tools are gone; the TUI is a SQL REPL + read-only table browser + notebook list). Multi-user / Tailscale mesh remains deferred.

**Prior art to port from:** `../schoolwork-workspace/` (notebook model `src/notebook/`, A2UI `src/a2ui/`, skills and task mode `agent/{skills,task,tools}.go`, guardrails `src/agent/`). It is on the same `charmbracelet/fantasy` framework as `internal/llm`.

## Tech Stack

- **Language:** Go (1.24+)
- **CLI Framework:** Cobra + Viper (config management)
- **Container/Encryption:** go-sqlcipher (SQLite + AES-256-CBC, PBKDF2-HMAC-SHA512) — the `.vibe` container envelope
- **Query/Data Engine:** go-duckdb (embedded DuckDB) — **single engine** for queries and CRUD
- **Desktop GUI:** Wails v2 with **Vue 3** frontend (the Studio app under `studio/`). UI uses the **SchoolHouse** design system (`studio/frontend/src/schoolhouse/`); the editor is **CodeMirror** (`composables/useVibeEditor.ts`). Chart.js under the A2UI renderer (#67) — the old page renderer (`internal/renderer`) it replaced was removed in #69.
- **AI Agent:** `internal/llm` on `charmbracelet/fantasy` (providers: local OpenAI-compatible, Anthropic, Gemini — see `docs/llm-config.md`); tool registry + sandbox + approval queue in `internal/agents`; MCP server in `internal/mcp` (`mark3labs/mcp-go`)
- **TUI:** BubbleTea + Lipgloss — a SQL REPL, a read-only table browser, and a notebook list (cut to this shape in #69)
- **Logging:** `log/slog` (structured JSON logging)
- **Testing:** `testing` + `github.com/stretchr/testify`

## Build Commands

Build automation uses **Taskfile.yml** (not Makefile):

```bash
task              # Default: lint + test + build
task mod          # Download and tidy Go modules
task build        # Build binary
task test         # Run all tests (alias: task t)
task test:coverage # Tests with coverage report
task lint         # Run golangci-lint
task lint:fix     # Lint and auto-fix
task studio:dev   # Wails dev server (NOT bare `wails dev` — needs -tags webkit2_41,duckdb_arrow)
task studio:build # Build the Studio desktop app
task release:X.Y.Z # Tag, build, and publish a release
```

Running a single package's tests:
```bash
go test -v ./internal/container/
```

## Architecture

```
main.go                    # Entry point — calls cmd.Execute()
cmd/                       # Cobra commands: init, info, import, export, dump, verify, compact, rekey, passphrase, studio
                           #   (+ `mcp serve`, #64)

studio/                    # Wails v2 desktop app
├── app.go                 # Go backend — all Wails-bound methods
└── frontend/              # Vue 3 UI. Views: Dashboard, Notebooks/Notebook, TableBrowser,
                           #   QueryEditor, Skills, Import, Settings, Unlock, AgentPanel
                           #   Removed (#68): FormBuilder, FormView, MacroBuilder, SchemaBuilder,
                           #   SchemaDesigner, Relationships, AppShellView, ReportView, App Mode
                           #   Removed (#69): PagesView, PageEditorView, PageRuntimeView

internal/
├── container/             # .vibe lifecycle (open, save, close; SQLCipher + DuckDB blob; temp hardening)
├── engine/                # DuckDB query execution
├── crud/                  # Transactional, type-coercing row insert/update/delete (stays)
├── importer/              # CSV / xlsx import
├── queries/               # Named query registry (_vibe_queries)
├── agents/                # Tool registry (ToolDef), sandbox modes, approval queue, JSON-RPC handler
├── llm/                   # In-app agent runner on fantasy (consumes agents.Handler tools, #63; task mode, #66)
├── mcp/                   # MCP server bridging agents.Handler tools (SSE; stdio via `vibe mcp serve`, #64)
├── a2ui/                  # A2UI component schema + validator (#67); Go validates, Vue renders
├── notebooks/             # Notebook storage + model (#66), plus the page->notebook and report->notebook
                           #   one-time migrations (internal/notebooks/migrate*.go) run on open
├── pages/                 # Legacy page markup PARSER ONLY (#69) — no executor, no renderer, no Studio
                           #   bindings. Survives solely so internal/notebooks can parse an old page's
                           #   content during migration. Do not add execution/rendering here.
├── tui/                   # BubbleTea TUI (#69): SQL REPL, read-only table browser, notebook list —
                           #   no page viewer/editor, no directive execution
├── docsref/               # Test keeping docs/reference/notebooks.md in sync with the a2ui component
                           #   registry and container's starter templates
└── config/                # Configuration types
```

Removed in #68 (the Access application layer): `internal/schema` (field meta,
forms, nav, relationships), `internal/automation` (macros), `internal/reports`
(banded reports — migrated into notebooks on open), the App Mode concept, and
their Vue/binding/agent-tool surfaces. Datasheet editing (`internal/crud`),
foreign keys via plain SQL, and Notebooks/Skills/A2UI/MCP all stay.

Removed in #69 (the page system, and the rest of #66's "Removes" list):
`internal/server` (web preview HTTP server, `vibe studio --web`),
`internal/renderer` (`renderer/html`, `renderer/tui`, and the shared
`renderer` package — nothing else used it), the page executor/template/linker
(`internal/pages/{executor,template,linker}.go`), Studio's page bindings
(`ListPages`/`GetPage`/`CreatePage`/`CreateHTMLPage`/`UpdatePage`/`DeletePage`/
`RenderPageContent`/`CancelRender`) and views (`PagesView`, `PageEditorView`,
`PageRuntimeView`), the `_dashboard` system page (`GetDashboard`/`GetFileInfo`
now report live table/notebook/query counts directly, no page render), and
the agent tools `create_page`/`update_page`/`delete_page`/`list_pages`/
`get_page`. The old `page` tool-category was renamed `content` (it also
covers `create_or_update_skill`, `add_query_cell`, `create_dashboard`) —
grep for `"content"` in `internal/agents`/`internal/llm`/`cmd`/`studio` if you're
hunting write-category gating. `_vibe_pages` and `_vibe_components` are still
created for new files (additive-migrations policy: schema.go never
special-cases "new" vs "old" files, and an empty `_vibe_pages` costs nothing —
`internal/notebooks.MigratePages` just finds zero rows to migrate).

New packages per the direction doc: `internal/a2ui` (validator, #67), `internal/llm/skills/` (embedded SKILL.md, #62), notebook storage (#66).

## Conventions (from Sandwich Labs Go projects)

- **`internal/` not `pkg/`** — use `internal/` for application packages (enforces import boundaries)
- **Cobra + Viper** — all CLI commands via Cobra; config via Viper with YAML files + env var overrides
- **Structured logging** — use `log/slog` with JSON handler; pass `*slog.Logger` via dependency injection
- **Error wrapping** — always wrap errors with context: `fmt.Errorf("failed to open container: %w", err)`
- **Interface-based design** — define interfaces for testability; mock implementations in `mock.go` alongside the package
- **Co-located tests** — test files next to source (`service.go` / `service_test.go`); use `testify/assert` and `testify/require`
- **Functional options** — use builder/options pattern for complex object construction
- **Dependency injection** — create services in `main.go`/`cmd/root.go` and pass them down; no globals
- **CGO_ENABLED=1** — required for SQLCipher and DuckDB C bindings
- **Goreleaser** — use `.goreleaser.yaml` for cross-platform release builds with ldflags for version injection
- **Additive migrations** — `_vibe_*` tables are only ever added in `internal/container/schema.go`; removals stop creating a table and stop requiring it in `vibe verify`, never drop it from existing files
- **One tool surface** — new agent capabilities are `ToolDef`s in `internal/agents`, never bespoke `fantasy` tools in `internal/llm`
- **Model output never reaches a style attribute** — A2UI colors are a closed enum; validators reject unknown fields
- **QA in a real browser** — Rodney against the Wails-bound dev server at `:34115`; Vitest specs are supplementary, never the acceptance criterion (see `RODNEY.md`)

## Key Concepts

**`.vibe` file format:** An encrypted SQLite database (SQLCipher) acting as the container envelope. Internal tables use the `_vibe_` prefix (`_vibe_meta`, `_vibe_pages` → `_vibe_notebooks`, `_vibe_queries`, `_vibe_skills`, `_vibe_agent_config`, `_vibe_secrets`, `_vibe_assets`, `_vibe_components`). User data lives in a serialized **DuckDB blob** (`_vibe_duckdb`).

**File lifecycle:** Open (decrypt SQLCipher, materialize DuckDB blob to a hardened temp file) → Session (queries, CRUD, notebooks, agent) → Save (checkpoint DuckDB, re-serialize blob into SQLCipher) → Close (cleanup, zero temp files).

**Notebooks (#66):** JSON documents of `sql` / `markdown` / `a2ui` cells. No inter-cell references; outputs are never persisted, every data cell re-runs on open. SQL cells render by synthesizing a one-component A2UI page. The agent authors cells in **task mode**: `preview_query` before a terminal `write_cell` or `need_more_info`; the result is an envelope built from recorded tool calls, never model prose.

**Skills (#62):** `SKILL.md` documents (`name`, `description` frontmatter + markdown instructions). The system prompt advertises name and description; the agent fetches bodies with `read_skill`. Built-ins are embedded; user skills live in `_vibe_skills`. Table-bound skills load only when the table has rows.

**Artifacts / A2UI (#67):** `{ title, components[] }`, thirteen component types, flat list nested by `parent` id, closed color enum, unknown fields rejected. `stat` / `table` / `barChart` / `lineChart` take inline data or a live `sql` binding. Go validates, Vue renders.

**Agent interface:** One tool registry (`internal/agents`) consumed by the in-app runner and exposed over MCP. Sandbox modes `read-only`, `read-write`, `full`, with an approval queue for sensitive operations. Guardrails (#70): grounding check, empty-result explanation, tool-result budget.

**Pages (retired, #69):** The old markdown + named SQL blocks + directives (`{table:}`, `{chart:}`, `{metric:}`, …) system is gone — see `docs/reference/notebooks.md`, which replaces the retired `docs/reference/pages.md`. `internal/pages` survives only as the parser an old file's pages are migrated through on open (`internal/notebooks.MigratePages`); do not add directives, execution, or rendering.

## Design Principles

- **File is the app** — `.vibe` files are fully self-contained, no external dependencies
- **Local-first & data sovereignty** — the data lives in the building, not someone else's server
- **Encryption by default** — passphrase required, no plaintext data on disk
- **Single-user first** — no concurrent access or conflict resolution (multi-user mesh deferred)
- **One tool surface** — the built-in agent and external MCP clients see the same tools with the same gating
- **AI as collaborator, not dependency** — everything works without AI; notebooks and SQL are first-class by hand
- **Live, not frozen** — notebooks and artifacts store questions (SQL), not answers; outputs re-run on open
- **Progressive disclosure** — skills advertise a line and load on demand; complexity available, never imposed
