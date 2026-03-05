# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Vibe is a portable, encrypted application container (`.vibe` files) that bundles data, interactive pages, and an AI agent interface into a single file. Think "modern Microsoft Access" — like the PDF ecosystem but for data apps.

Two applications share one file format:
- **Vibe Studio** — Charm/BubbleTea TUI for technical users to create and edit `.vibe` files
- **Vibe Viewer** — Wails v2 desktop GUI for non-technical users to open and interact with `.vibe` files

**Status:** Pre-implementation specification. See `PROJECT.md` for the full MVP spec (~1,450 lines).

## Tech Stack

- **Language:** Go (1.24+)
- **CLI Framework:** Cobra + Viper (config management)
- **Container/Encryption:** go-sqlcipher (SQLite + AES-256-CBC, PBKDF2-HMAC-SHA512)
- **Analytics Engine:** go-duckdb (embedded DuckDB for analytical queries)
- **TUI:** BubbleTea + Lipgloss + Glamour
- **Desktop GUI:** Wails v2 with React frontend + Chart.js
- **Web Preview:** Go stdlib `net/http` + `html/template`
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
├── [command].go           # Feature commands (init, info, serve, etc.)
└── vibe-viewer/           # Vibe Viewer Wails GUI entry point
    └── frontend/          # React UI for Viewer

internal/
├── container/             # .vibe file lifecycle (open, save, close with SQLCipher)
├── engine/                # Query execution (DuckDB + SQLite dual-engine)
├── pages/                 # Page parsing: markdown → AST → execute SQL blocks → resolve directives
├── renderer/              # Output rendering (TUI terminal + HTML web)
├── agents/                # AI agent interface (JSON-RPC 2.0, stdio/socket transport)
├── server/                # Embedded HTTP server for web preview
├── queries/               # Named query registry
├── config/                # Configuration types
├── db/                    # Database layer
└── tui/                   # BubbleTea TUI (model.go, update.go, view.go)
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

**`.vibe` file format:** An encrypted SQLite database (SQLCipher). Internal tables use `_vibe_` prefix. Contains pages, queries, components, agent config, assets, and a serialized DuckDB blob for analytics.

**File lifecycle:** Open (decrypt SQLite, materialize DuckDB blob to temp) → Session (execute pages, run queries) → Save (checkpoint DuckDB, write blob back) → Close (cleanup temp files).

**Pages:** Markdown documents with two extensions — SQL blocks (named result sets) and directives (`{table:}`, `{chart:}`, `{metric:}`, `{form:}`, `{detail:}`). Executed top-to-bottom: parse AST → run SQL → resolve directives → render.

**Dual query engines:** SQLite for transactional/CRUD, DuckDB for analytical queries. SQL blocks specify engine via `[engine=duckdb|sqlite]`.

**Agent interface:** JSON-RPC 2.0 protocol, transport-agnostic (stdio or socket). Three sandbox modes: `read-only`, `read-write`, `full`. Approval workflow for sensitive operations.

## Design Principles

- **File is the app** — `.vibe` files are fully self-contained, no external dependencies
- **Encryption by default** — passphrase required, no plaintext data on disk
- **Single-user first** — no concurrent access or conflict resolution
- **AI as collaborator, not dependency** — everything works without AI
