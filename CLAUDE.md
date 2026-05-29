# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Vibe is a portable, encrypted application container (`.vibe` files) that bundles data, interactive pages, forms, reports, and an AI agent interface into a single file. It is the **local-first spiritual successor to Microsoft Access 97** — a rapid application development platform for small businesses, reimagined with modern primitives (DuckDB, an AI agent as the "wizard" layer) instead of the cloud-SaaS tax.

**Product direction & UX:** the north-star UX target is Access 97 parity — relational tables, bound forms (columnar / continuous / datasheet / master-detail subform), visual querying, banded reports, and a Switchboard-style navigation shell with a locked-down end-user mode. See `docs/ms-access-parity-plan.md` for the phased roadmap and `docs/ms-access-ux-research.txt` for the UX research that grounds it.

**One app, two modes** (the Access dual-mode homage):
- **Studio Mode** — for builders (citizen + studio developers): schema designer, page/form/report editors, query explorer, agent chat.
- **App Mode** — for data-entry workers: a locked-down generated interface (switchboard navigation, forms, reports only — no schema, SQL, or destructive agent tools). App Mode is a **runtime toggle inside the Studio app**, not a separate binary.

A **BubbleTea TUI** (`internal/tui`) provides terminal-native page/table/query workflows for technical users.

**Status:** Substantially implemented (container, encryption, pages/directives, DuckDB engine, CRUD methods, Wails+Vue Studio, agent + MCP all working). The next arc is MS Access *application* parity — forms with write-back, relational schema, reports, and the app shell. Multi-user / Tailscale mesh is deferred to **v1.1**. See `PROJECT.md` for the original MVP spec, `PLAN.md` for the build phases, and `docs/ms-access-parity-plan.md` for the current roadmap.

## Tech Stack

- **Language:** Go (1.24+)
- **CLI Framework:** Cobra + Viper (config management)
- **Container/Encryption:** go-sqlcipher (SQLite + AES-256-CBC, PBKDF2-HMAC-SHA512) — the `.vibe` container envelope
- **Query/Data Engine:** go-duckdb (embedded DuckDB) — **single engine** for both analytical queries and CRUD/data storage. (The legacy SQLite query path exists but is unused; SQLCipher is the encrypted container only.)
- **TUI:** BubbleTea + Lipgloss + Glamour
- **Desktop GUI:** Wails v2 with **Vue 3** frontend + Chart.js (the Studio app under `studio/`; Studio Mode + App Mode). UI uses the **SchoolHouse** design system (`studio/frontend/src/schoolhouse/`); the page editor is **CodeMirror** (`composables/useVibeEditor.ts`).
- **Web Preview:** Go stdlib `net/http` + `html/template`
- **AI Agent / MCP:** JSON-RPC 2.0 agent (`internal/agents`) + MCP server (`internal/mcp`, `mark3labs/mcp-go`)
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
task release:X.Y.Z # Tag, build, and publish a release
```

Running a single package's tests:
```bash
go test -v ./internal/container/
```

## Architecture

```
main.go                    # Entry point — calls cmd.Execute()
cmd/
├── root.go                # Cobra root command, Viper config init
└── [command].go           # Feature commands (init, info, studio, etc.)

studio/                    # Wails v2 desktop app (Studio Mode + App Mode)
├── app.go                 # Go backend — all Wails-bound methods (file/page/table/query/agent ops)
└── frontend/              # Vue 3 UI (views/: Dashboard, Pages, PageEditor, TableBrowser,
                           #   QueryEditor, Import, Settings, Unlock)

internal/
├── container/             # .vibe file lifecycle (open, save, close; SQLCipher + DuckDB blob)
├── engine/                # Query execution (DuckDB single-engine; SQLite path legacy/unused)
├── pages/                 # Page parsing: markdown → AST → execute SQL blocks → resolve directives
├── renderer/              # Output rendering (html/ for web+GUI, tui/ for terminal)
├── agents/                # AI agent interface (JSON-RPC 2.0, tools, sandbox, approval queue)
├── mcp/                   # MCP server (SSE transport) bridging to the agent tool surface
├── server/                # Embedded HTTP server for web preview
├── queries/               # Named query registry (_vibe_queries)
├── config/                # Configuration types
└── tui/                   # BubbleTea TUI (sidebar, content, table browser, query editor, REPL)
```

## Conventions (from Sandwich Labs Go projects)

- **`internal/` not `pkg/`** — use `internal/` for application packages (enforces import boundaries)
- **Cobra + Viper** — all CLI commands via Cobra; config via Viper with YAML files + env var overrides
- **Structured logging** — use `log/slog` with JSON handler; pass `*slog.Logger` via dependency injection
- **Error wrapping** — always wrap errors with context: `fmt.Errorf("failed to open container: %w", err)`
- **Interface-based design** — define interfaces for testability; mock implementations in `mock.go` alongside the package
- **Co-located tests** — test files next to source (`service.go` / `service_test.go`); use `testify/assert` and `testify/require`
- **Functional options** — use builder/options pattern for complex object construction (e.g., `NewClient(WithNumThreads(4), WithPath("..."))`)
- **Dependency injection** — create services in `main.go`/`cmd/root.go` and pass them down; no globals
- **CGO_ENABLED=1** — required for SQLCipher and DuckDB C bindings
- **Goreleaser** — use `.goreleaser.yaml` for cross-platform release builds with ldflags for version injection

## Key Concepts

**`.vibe` file format:** An encrypted SQLite database (SQLCipher) that acts as the container envelope. Internal tables use the `_vibe_` prefix (pages, queries, components, sources, agent config, secrets, assets). User data and analytics live in a serialized **DuckDB blob** (`_vibe_duckdb`). New Access-parity metadata tables (`_vibe_field_meta`, `_vibe_relationships`, `_vibe_forms`, `_vibe_reports`, `_vibe_nav`, `_vibe_macros`) are added as additive migrations per the parity plan.

**File lifecycle:** Open (decrypt SQLCipher, materialize DuckDB blob to temp file) → Session (execute pages, run queries, CRUD) → Save (checkpoint DuckDB, re-serialize blob back into SQLCipher) → Close (cleanup temp files).

**Pages:** Markdown documents with two extensions — SQL blocks (named result sets) and directives (`{table:}`, `{chart:}`, `{metric:}`, `{form:}`, `{detail:}`, `{if:}`, `{row:}`). Executed top-to-bottom: parse AST → run SQL → resolve directives → render (HTML for GUI/web, terminal widgets for TUI).

**Single query engine:** All queries and CRUD run on **DuckDB**. The dual-engine design from the original spec was simplified to DuckDB-only; SQLCipher is no longer queried directly for user data, it stores the encrypted container and the DuckDB blob.

**Agent interface:** JSON-RPC 2.0 protocol, transport-agnostic (stdio/socket) plus an MCP server (SSE). Three sandbox modes: `read-only`, `read-write`, `full`, with an approval queue for sensitive operations. The agent is positioned as the **Wizard layer** (Table/Form/Report/Switchboard/Macro wizards) — the modern replacement for Access's wizard dialogs.

## Design Principles

- **File is the app** — `.vibe` files are fully self-contained, no external dependencies (the Access `.mdb` ethos)
- **Local-first & data sovereignty** — the data lives in the building, not someone else's server
- **Encryption by default** — passphrase required, no plaintext data on disk
- **Single-user first** — no concurrent access or conflict resolution (multi-user mesh is **v1.1**)
- **Dual-mode** — builders get Studio Mode; data-entry workers get a locked-down App Mode
- **AI as collaborator, not dependency** — the agent is the wizard layer, but everything works without AI
- **Progressive disclosure** — complexity available but never imposed (the Access citizen-developer promise)
