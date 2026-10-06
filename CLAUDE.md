# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Vibe is an **agentic desktop workspace for working with data where it already lives** — DataGrip/DBeaver-shaped, with an AI agent, live notebooks, skills and A2UI artifacts, all exposed to external agents over MCP. Data stays resident in **connections** (local DuckDB, MotherDuck, Postgres, SQLite, S3/GCS/R2, … attached into one DuckDB hub); a **workspace** is a folder (optionally a git repo) holding notebooks, queries, skills and secret-free connection definitions; **credentials** live in the OS keychain or env vars; `.vibe` is an import/export format.

**North star:** `docs/direction.md` (2026-10-06). Read it before planning any feature work. **Plan:** `docs/plans/workspaces-and-connections.md`. **Tracking:** epic #105 (issues #106–#119), workspaces first, then connections.

**Direction change (2026-10-06) — today vs target:** Studio (the desktop app) **still** opens one SQLCipher `.vibe` container holding a DuckDB blob (everything under *Architecture* and *Key Concepts* below describes that, and stays accurate until Studio moves to workspaces in #111; the CLI, TUI and `mcp serve` already run on workspaces). Do not extend the container as the place data lives: no new `_vibe_*` tables for content, no new per-file settings, no new Save/Revert semantics for table data. New work goes behind the interfaces the plan names (`internal/session`, `internal/workspace`, `notebooks.Store`/`queries.Store`/`skills.Store`, `internal/secrets`, `internal/connections`). Never store a credential in a config file, a `.vibe`, a log line, or anything the agent can read.

**Earlier direction change (2026-09-22):** Vibe was previously aimed at Microsoft Access 97 application parity (bound forms, subforms, switchboard, App Mode, macros). That direction is **superseded**. The Access application layer was removed in #68; datasheet editing stays. The parity docs are kept for history and carry a superseded banner. Do not add forms, macros, nav, field meta, or App Mode.

**Status:** Container, encryption, DuckDB engine, CRUD, import/export, Wails+Vue Studio, agent + MCP are all working. The **primitive migration** is complete: one agent tool surface (#63), Skills (#62), A2UI artifacts (#67), Notebooks with agent task-mode authoring (#66), headless `vibe mcp serve` (#64), guardrails (#70), removal of the Access layer (#68 — forms/subforms/relationships-UI/switchboard/macros/App Mode/banded reports are gone, foreign keys are plain SQL, reports migrate into notebooks), and retirement of the page system (#69 — web preview server, page renderers, Studio page views/bindings, and the page agent tools are gone; the TUI is a SQL REPL + read-only table browser + notebook list). Multi-user / Tailscale mesh remains deferred.

**Prior art to port from:** `../schoolwork-workspace/` (notebook model `src/notebook/`, A2UI `src/a2ui/`, skills and task mode `agent/{skills,task,tools}.go`, guardrails `src/agent/`). It is on the same `charmbracelet/fantasy` framework as `internal/llm`.

## Tech Stack

- **Language:** Go (1.24+)
- **CLI Framework:** Cobra + Viper (config management)
- **Container/Encryption:** go-sqlcipher (SQLite + AES-256-CBC, PBKDF2-HMAC-SHA512) — the `.vibe` container envelope
- **Query/Data Engine:** go-duckdb (embedded DuckDB) — **single engine** for queries and CRUD
- **Desktop GUI:** Wails v2 with **Vue 3** frontend (the Studio app under `studio/`). UI uses the **SchoolHouse** design system (`studio/frontend/src/schoolhouse/`) **themed through semantic roles** (`studio/frontend/src/theme/`, `docs/reference/themes.md`): stylesheets reference `--vibe-*` roles (or the bridged `--sh-*` names), never hex; four built-in themes plus user JSON themes in `~/.config/vibe/themes/`. The editor is **CodeMirror** (`composables/useVibeEditor.ts`), themed via the same vars. A2UI charts (#67) are hand-drawn CSS/SVG in `studio/frontend/src/a2ui/` (no Chart.js) — the old page renderer (`internal/renderer`) they replaced was removed in #69.
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
task build:studio # Build the Studio desktop app (build:studio:linux on Linux)
task release:X.Y.Z # Tag, build, and publish a release
```

Wails tasks pin `GOTOOLCHAIN` via the `WAILS_GOTOOLCHAIN` Taskfile var (Wails v2.11 can't load Go 1.27 packages, #92); run `wails` through `task`, or prefix `GOTOOLCHAIN=<WAILS_GOTOOLCHAIN>` (value in Taskfile.yml) when running it directly.

Running a single package's tests:
```bash
go test -v ./internal/container/
```

## Architecture

```
main.go                    # Entry point — calls cmd.Execute()
cmd/                       # Cobra commands, all on workspaces (#113): `workspace init|info`, `init` (alias),
                           #   `query`, `import --into [conn.]table`, `export`, `studio [dir] [--gui]` (TUI is the default),
                           #   `mcp serve [--workspace DIR]` (owner or proxy to Studio, #64/#112).
                           #   Writers take the hub lock (internal/hub) or say who holds it; `--read-only` skips it.
                           #   wsopen.go is the one place that opens workspace + hub + session for a command.
                           #   No command opens a `.vibe` any more; info/verify/compact/rekey/dump/passphrase
                           #   survive only as hidden stubs that point to `workspace init`.

studio/                    # Wails v2 desktop app
├── app.go                 # Go backend — all Wails-bound methods
└── frontend/              # Vue 3 UI. Views: Dashboard, Notebooks/Notebook, TableBrowser,
                           #   QueryEditor, Skills, Import, Settings, Unlock, AgentPanel
                           #   Removed (#68): FormBuilder, FormView, MacroBuilder, SchemaBuilder,
                           #   SchemaDesigner, Relationships, AppShellView, ReportView, App Mode
                           #   Removed (#69): PagesView, PageEditorView, PageRuntimeView

internal/
├── container/             # .vibe lifecycle (open, save, close; SQLCipher + DuckDB blob; temp hardening);
│                          #   provides the legacy SQLite impls of notebooks/queries/skills Store (#109)
├── workspace/             # #109: a folder + vibe.yaml. Strict vibe.yaml schema, walk-up Discover, Init, Open,
│                          #   three connection scopes (user < shared < local; the local file lives in the user config dir, never in the folder) merged with provenance, EffectivePolicy
│                          #   (committed sandbox/ask = a ceiling that can only tighten), ${ENV} interpolation,
│                          #   credential Fingerprint, OpenSession (session.New + connections), fsnotify Watcher,
│                          #   duplicate-id detection. Reference: docs/reference/workspaces.md
├── filestore/             # #109: safe paths (never outside the root), atomic writes, compare-and-write (hash) -> ErrConflict
├── session/               # DuckDB hub (#108): hardened Session (catalog-aware Query/Exec/Conn, Attach/Detach,
                           #   Connections() status, schema cache, quoting helpers). The container's DuckDB is
                           #   wrapped by it (Container.Session()); in-memory New() becomes the workspace hub (#109)
├── engine/                # Query layer over a session.Session (Manager); table-ref parsing (ParseTable/ResolveTable/TableSQL)
├── crud/                  # Transactional, type-coercing row insert/update/delete (stays)
├── importer/              # CSV / xlsx import
├── queries/               # Named query `Store` (Registry over _vibe_queries; `FileStore` -> queries/<id>.sql with a `-- vibe:` header)
├── agents/                # Tool registry (ToolDef), sandbox modes, approval queue, JSON-RPC handler;
│                          #   reads content via ContainerAccess.Notebooks/Queries/Skills stores (never the SQLite
│                          #   handle) and policy via `PolicySource` (container: `MetaPolicy`; workspace: `AgentPolicy()`)
├── llm/                   # In-app agent runner on fantasy (consumes agents.Handler tools, #63; task mode, #66)
├── mcp/                   # MCP server bridging agents.Handler tools (SSE; stdio via `vibe mcp serve`, #64),
                           #   plus ProxyStdio: the stdio ⇄ SSE bridge `vibe mcp serve` uses when Studio owns the hub (#112)
├── filelock/              # crash-safe non-blocking flock + holder payload; behind container's .vibe lock (#80)
                           #   and the workspace hub lock (#112)
├── hub/                   # who owns a workspace's DuckDB hub (#112): AcquireHub / ReadHolder / TakeOver on
                           #   .vibe-local/hub.lock, Serve (`vibe mcp serve` owner/proxy), StartWorkspaceMCP,
                           #   NewAgentHandler, BuildClaudeConfig, ProtectStdout. Same-user trust, see docs/mcp.md
├── workspace/             # workspace folder, scopes, file stores, watcher (#109); AgentAccess adapts a
                           #   workspace + session to agents.ContainerAccess
├── a2ui/                  # A2UI component schema + validator (#67); Go validates, Vue renders
├── notebooks/             # Notebook model (#66) + `Store` (SQLite impl for .vibe, `FileStore` -> notebooks/<id>.vnb), plus the page->notebook and report->notebook
                           #   one-time migrations (internal/notebooks/migrate*.go) run on open
├── pages/                 # Legacy page markup PARSER ONLY (#69) — no executor, no renderer, no Studio
                           #   bindings. Survives solely so internal/notebooks can parse an old page's
                           #   content during migration. Do not add execution/rendering here.
├── tui/                   # BubbleTea TUI (#69, on workspaces in #113): SQL REPL on the session, read-only table
                           #   browser across every attached connection, notebook list from the workspace's
                           #   store. No save step (writes land in their connection). No page viewer/editor.
├── templates/             # Starter templates (tables + seed rows + a notebook): shared by `vibe workspace init
                           #   --template` and the legacy container's WithTemplate (#113)
├── docsref/               # Test keeping docs/reference/notebooks.md in sync with the a2ui component
                           #   registry and the starter templates
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

New packages per the direction doc: `internal/a2ui` (validator, #67), `internal/llm/skills/` (embedded SKILL.md, #62; also the skills `Store`: `FileStore` -> skills/<name>/SKILL.md, SQLite impl for .vibe), notebook storage (#66), `internal/workspace` + `internal/filestore` (#109; Studio/CLI/MCP still open `.vibe` until #110–#113).

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
- **Additive migrations** (legacy container) — `_vibe_*` tables are only ever added in `internal/container/schema.go`; removals stop creating a table and stop requiring it in `vibe verify`, never drop it from existing files
- **No hard-coded catalog** — never write `'main'` / `'memory'` / `'analytics'` in code. Ask `Session.DefaultCatalog()`, introspect through `Session.ListTables/DescribeTable` (filtered by `database_name`), and name tables `catalog.schema.table` through `session.QuoteIdent`/`TableRef.SQL()`. Unqualified names resolve against the connection's current catalog (`current_database()`/`current_schema()` in SQL), which is the session default unless a run pinned one with `USE`
- **One tool surface** — new agent capabilities are `ToolDef`s in `internal/agents`, never bespoke `fantasy` tools in `internal/llm`
- **Model output never reaches a style attribute** — A2UI colors are a closed enum; validators reject unknown fields
- **No hex in stylesheets** — every colour in `studio/frontend` is a `--vibe-*` role (or a bridged `--sh-*` token); translucent variants use `color-mix()` on a role. Theme files are validated by `theme/schema.ts` (hex only, closed role list). Built-ins must pass the AA contrast spec in `theme/schema.spec.ts`
- **Focus is visible** — `theme/bridge.css` owns one `:focus-visible` ring; never `outline: none` without it. Ad-hoc modals use `composables/useModal.ts` (Escape, focus move/return)
- **QA in a real browser** — Rodney against the Wails-bound dev server at `:34115`; Vitest specs are supplementary, never the acceptance criterion (see `RODNEY.md`)

## Key Concepts

**`.vibe` file format (current; becomes legacy-import-only in #110, replaced by the bundle format in #118):** An encrypted SQLite database (SQLCipher) acting as the container envelope. Internal tables use the `_vibe_` prefix (`_vibe_meta`, `_vibe_pages` → `_vibe_notebooks`, `_vibe_queries`, `_vibe_skills`, `_vibe_agent_config`, `_vibe_secrets`, `_vibe_assets`, `_vibe_components`). User data lives in a serialized **DuckDB blob** (`_vibe_duckdb`).

**File lifecycle:** Open (decrypt SQLCipher, materialize DuckDB blob to a hardened temp file) → Session (queries, CRUD, notebooks, agent) → Save (checkpoint DuckDB, re-serialize blob into SQLCipher) → Close (cleanup, zero temp files). Open takes an exclusive writer lock (`<file>.lock`, flock; #80); read-only opens don't. Studio tracks unsaved table changes (`Container.Dirty`, `file:dirty`) and asks Save / Don't save / Cancel on Close and quit. Everything except an explicit Don't save still saves. Revert to last save re-materializes the DuckDB copy from the saved blob in place (`Container.Revert`), keeping the lock; the last save is also the undo point for agent changes (#98).

**Notebooks (#66; format v2 #108):** JSON documents of `sql` / `markdown` / `a2ui` cells. v2 adds an optional `connection` on sql cells and a2ui `sql` bindings (the default catalog for that statement's unqualified names, on a pinned connection; unset = session default); v1 loads as v2. No inter-cell references; outputs are never persisted, every data cell re-runs on open. SQL cells render by synthesizing a one-component A2UI page. The agent authors cells in **task mode**: `preview_query` before a terminal `write_cell` or `need_more_info`; the result is an envelope built from recorded tool calls, never model prose.

**Skills (#62):** `SKILL.md` documents (`name`, `description` frontmatter + markdown instructions). The system prompt advertises name and description; the agent fetches bodies with `read_skill`. Built-ins are embedded; user skills live in `_vibe_skills`. Table-bound skills load only when the table has rows.

**Artifacts / A2UI (#67):** `{ title, components[] }`, thirteen component types, flat list nested by `parent` id, closed color enum, unknown fields rejected. `stat` / `table` / `barChart` / `lineChart` take inline data or a live `sql` binding. Go validates, Vue renders.

**Hub ownership (#112):** one process per workspace owns the DuckDB hub, recorded by a flock on `.vibe-local/hub.lock` (0600 JSON: app/pid/host, plus `mcp {url, token}` while serving MCP and `control: sigusr1` when releasable). `vibe mcp serve` owns the hub if free, else proxies stdio to the owner's MCP; `hub.TakeOver` asks an owner to release via SIGUSR1. The lock file is trusted for the same user only (owner uid, no group/world bits, flock really held, loopback http endpoint); the token never goes in argv, config snippets, structured logs or errors (only `--transport sse` prints it, once, to stderr). The lock is opened `O_NOFOLLOW` and must be a single-link regular file you own; `--sandbox` in proxy mode is enforced by the owner via `X-Vibe-Sandbox` (tighten-only per session). Over stdio, fd 1 is redirected to stderr (`hub.ProtectStdout`) so nothing but JSON-RPC reaches stdout. No save-on-exit: writes go straight to their sources.

**Agent interface:** One tool registry (`internal/agents`) consumed by the in-app runner and exposed over MCP. Sandbox modes `read-only`, `read-write`, `full`, with per-file "ask before changes" (default on): Studio chat shows an inline approval card; MCP clients are refused. Guardrails (#70): grounding check, empty-result explanation, tool-result budget.

**Pages (retired, #69):** The old markdown + named SQL blocks + directives (`{table:}`, `{chart:}`, `{metric:}`, …) system is gone — see `docs/reference/notebooks.md`, which replaces the retired `docs/reference/pages.md`. `internal/pages` survives only as the parser an old file's pages are migrated through on open (`internal/notebooks.MigratePages`); do not add directives, execution, or rendering.

## Design Principles

(2026-10-06; replaces "file is the app" and "encryption by default" — see `docs/direction.md` §6)

- **Data stays where it lives** — Vibe reaches data in its connections; it doesn't copy it into its own format
- **The workspace is the app** — a folder of plain, diff-friendly files you can version, review and share via git
- **Credentials in the keychain** — never in the folder, a bundle, logs, or the agent's context; write-only across the Wails bridge and MCP
- **Read-only until asked** — connections default to read-only; agent writes need the sandbox tier, a writable connection, and user approval
- **Single-user first** — sharing is via shared backends + git, not live collaboration (multi-user mesh deferred)
- **One tool surface** — the built-in agent and external MCP clients see the same tools with the same gating
- **AI as collaborator, not dependency** — everything works without AI; notebooks and SQL are first-class by hand
- **Live, not frozen** — notebooks and artifacts store questions (SQL), not answers; outputs re-run on open
- **Progressive disclosure** — skills advertise a line and load on demand; connections connect on first use; complexity available, never imposed
