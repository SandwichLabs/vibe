# CLAUDE.md

Guidance for Claude Code (claude.ai/code) when working in this repository.

## Overview

Vibe is an **agentic desktop workspace for working with data where it already lives** — DataGrip/DBeaver-shaped, with an AI agent, live notebooks, skills and A2UI artifacts, all exposed to external agents over MCP.

- **Connections** keep the data where it is (local DuckDB, MotherDuck, Postgres, MySQL, SQLite, S3/GCS/R2, Azure, DuckLake, Iceberg/Delta). They are attached on first use into one in-memory **DuckDB hub** per workspace.
- A **workspace** is a folder (optionally a git repo): `vibe.yaml`, `notebooks/*.vnb`, `queries/*.sql`, `skills/<name>/SKILL.md`, `data/`. Machine-local state lives in `.vibe-local/` (git-ignored).
- **Credentials** live in the OS keychain or env vars, never in the folder.
- **`.vibe`** is an exchange **bundle** (zip, optional age passphrase) of notebooks, queries, skills and connection definitions. It never holds data or credentials. Older data-holding SQLCipher `.vibe` files are not opened (#110 was dropped).

**Read first:** `docs/direction.md` (north star; §9 "As delivered" lists what changed from the plan in #105; the earlier Access-parity and page-system removals are recorded in `docs/archive/` and git history) and `docs/README.md` (index of every doc). Shipped surface reference: `docs/reference/`. Closed epic: #105; cleanup survey: #137 (`docs/reviews/cleanup-2026-10/`).

## Tech stack

- **Go 1.26+**, `CGO_ENABLED=1` (DuckDB bindings). Cobra + Viper for the CLI and config.
- **Engine:** `go-duckdb` (embedded DuckDB) is the single query/CRUD engine. There is no SQLite or SQLCipher dependency (removed in #137): tests use DuckDB, FileStores in `t.TempDir()`, or the store `Mock`s.
- **Desktop GUI:** Wails v2 + **Vue 3** (`studio/`). UI uses SchoolHouse (`studio/frontend/src/schoolhouse/`), themed through `--vibe-*` semantic roles (`docs/reference/themes.md`). Editor is CodeMirror. A2UI charts are hand-drawn CSS/SVG.
- **Agent:** `internal/llm` on `charmbracelet/fantasy` (providers: `docs/reference/llm-config.md`); tools, sandbox and approvals in `internal/agents`; MCP in `internal/mcp` and `internal/hub` (`docs/reference/mcp.md`).
- **TUI:** BubbleTea + Lipgloss (`vibe studio` without `--gui`): SQL REPL, read-only table browser, notebook list.
- **Logging:** `log/slog`. **Tests:** `testing` + testify.

## Build commands

Automation is **Taskfile.yml**, not a Makefile.

```bash
task              # default: lint + test + build
task build        # build the vibe binary (./cmd/vibe; also `go install github.com/SandwichLabs/vibe/cmd/vibe@latest`)
task test         # Go + Vitest tests (alias: task t); task test:frontend is Vitest only
task lint         # golangci-lint (task lint:fix to auto-fix)
task studio:dev   # Wails dev server (NOT bare `wails dev`: needs -tags webkit2_41,duckdb_arrow)
task build:studio # Studio desktop app (build:studio:linux on Linux)
go test -v ./internal/workspace/   # one package
```

**CI** (`.github/workflows/ci.yml`): every PR runs the Linux `test` job (vet, `xvfb-run go test`, golangci-lint, vitest, frontend build, shellcheck) and a Linux amd64/arm64 build smoke on ubuntu-22.04 (CLI round trip + `wails build`). macOS runs only on push to main and `workflow_dispatch` (10x minutes). govulncheck (#41) and vue-tsc (#34/#35) are report-only. Actions are pinned to commit SHAs; the golangci-lint version is pinned in the workflow, so bump it with the local one.

Wails tasks pin `GOTOOLCHAIN` via `WAILS_GOTOOLCHAIN` in Taskfile.yml (Wails v2.11 can't load newer Go, #92). Run `wails` through `task`, or prefix `GOTOOLCHAIN=<that value>`.

CLI: `vibe workspace init`, `query`, `import`, `export`, `conn`, `config` (`config llm` mirrors Settings → App's provider picker; keys stay on `config llm-key`), `studio` (`--gui` for desktop), `mcp serve`. `init` aliases `workspace init`. Old container commands (`info`, `verify`, `compact`, `rekey`, `dump`, `passphrase`) are hidden stubs that point to `vibe workspace init`.

## Architecture

```
cmd/vibe/, cmd/   main (cmd/vibe) and the Cobra commands (workspace, query, import, export, conn, config, bundle, mcp, studio)
studio/              Wails app: app.go (bindings), workspace.go, connections.go, secrets.go, bundle.go,
                     templates.go; frontend/ is Vue 3 (views: Launcher, Dashboard, Notebooks/Notebook,
                     TableBrowser, QueryEditor, ConnectionEditor, Skills, Import, Settings, Agent)
internal/
  workspace/         the folder model: vibe.yaml, scopes (user/shared/local), discovery, init, watcher
  connections/       connection type registry and attach SQL
  session/           the DuckDB hub for a workspace; connection lifecycle and status
  secrets/           keychain + env credential store (write-only across every boundary)
  dotenv/            workspace .env parser and per-person allow/deny (hash-keyed allow, path-keyed deny)
  agents/            ToolDef registry, sandbox tiers, approval queue, SQL allow-list policy, JSON-RPC handler
  llm/               in-app agent runner (fantasy), task mode, grounding; llm/skills = SKILL.md loading
  mcp/, hub/         MCP server (SSE, stdio) and which process owns a workspace's hub (Studio vs `vibe mcp serve`)
  notebooks/, queries/  file-backed stores (notebooks.Store, queries.Store), same shape as skills.Store
  a2ui/              component schema + validator (Go validates, Vue renders)
  bundle/            .vibe bundle v2 export/import, optional age encryption
  crud/, importer/, engine/, usersql/  datasheet CRUD, CSV/xlsx import, query exec, courtesy SQL checks
  filestore/, filelock/, storageurl/, prefs/, config/, templates/, tui/
  docsref/           test keeping docs/reference/ in sync with the a2ui registry, templates and vibe.yaml keys
docs/                see docs/README.md
```

## Key concepts

**Workspace and files.** Files are the source of truth; writes are compare-and-write and a watcher picks up outside changes (git, editors, other processes). Notebook outputs are never persisted. A committed `vibe.yaml` can only *tighten* the agent's limits; looser per-person settings live outside the folder. A workspace `.env` is loaded only after the person allows it (direnv-style: allow is keyed by path + content hash, deny by path; decisions in the user config dir), into a per-workspace overlay read by `env:` sign-ins, never `os.Setenv`.

**Connections.** Read-only by default, connect on first use, with a visible status. The connection name is the catalog alias in SQL (`prod_pg.public.orders`). Metadata-driven formats (DuckLake, Iceberg, Delta) may only be defined in your own scopes, never in a committed `vibe.yaml`. MotherDuck cannot be attached read-only.

**Notebooks (#66).** JSON documents of `sql` / `markdown` / `a2ui` cells (schemaVersion 2; a cell may name a `connection`). Every data cell re-runs on open. The agent authors cells in **task mode**: `preview_query` before a terminal `write_cell` or `need_more_info`.

**Skills (#62).** `SKILL.md` documents (`name`, `description` + markdown). The prompt advertises name and description; the agent fetches bodies with `read_skill`. Built-ins are embedded. Table-bound skills load only when the table has rows.

**A2UI artifacts (#67).** `{ title, components[] }`, thirteen component types, closed color enum, unknown fields rejected. `stat` / `table` / `barChart` / `lineChart` take inline data or a live `sql` binding.

**Agent policy.** One tool registry, consumed by the in-app runner and by MCP. Sandbox tiers `read-only`, `read-write`, `full`. A write needs all three: the tier allows it, the connection is writable, and the person approves (always asked for non-local connections; headless MCP refuses them). SQL is an allow-list parsed before `EXPLAIN` binds it (`docs/reference/agent-policy.md`). Guardrails (#70): grounding check, empty-result explanation, tool-result budget.

## Conventions

- **`internal/` not `pkg/`**; Cobra + Viper; `log/slog` passed by dependency injection; services created in `cmd/vibe/main.go`/`cmd/` and passed down, no globals.
- **Wrap errors** with context: `fmt.Errorf("failed to open workspace: %w", err)`.
- **Interfaces for testability**; a `mock.go` only when a test needs it. Co-located tests with testify. Functional options for complex constructors.
- **One tool surface**: new agent capabilities are `ToolDef`s in `internal/agents`, never bespoke `fantasy` tools in `internal/llm`.
- **Model output never reaches a style attribute**: A2UI colors are a closed enum; validators reject unknown fields.
- **No hex in stylesheets**: use `--vibe-*` roles (or bridged `--sh-*`); translucent variants via `color-mix()`. Themes are validated by `theme/schema.ts` and must pass the AA contrast spec.
- **Focus is visible**: `theme/bridge.css` owns the `:focus-visible` ring; never `outline: none` without it. Ad-hoc modals use `composables/useModal.ts`.
- **QA in a real browser**: Rodney against the Wails-bound dev server at `:34115` (see `RODNEY.md`); Vitest specs are supplementary, never the acceptance criterion. The older Rodney verification protocol is in `docs/archive/parity-remediation-plan.md` §1.
- **Docs move with code**: keep `docs/reference/` and the architecture tree above current; `internal/docsref` fails when they drift.
- **Goreleaser** (`.goreleaser.yaml`) for releases, ldflags for version injection.

## Design principles

- **Trust rules:** never store a credential in a config file, a `.vibe` bundle, a log line, an error shown to the UI or agent, or anything the agent can read. Credentials are write-only across the Wails bridge and MCP.
- **Data stays where it lives**: Vibe reaches data in its connections; it doesn't copy it into its own format.
- **The workspace is the app**: a folder of plain, diff-friendly files you can version, review and share via git.
- **Read-only until asked**: agent writes need the tier, a writable connection, and approval.
- **Single-user first**: sharing is via shared backends and git; multi-user mesh is deferred.
- **One tool surface**: the built-in agent and MCP clients see the same tools with the same gating.
- **AI as collaborator, not dependency**: everything works without AI.
- **Live, not frozen**: notebooks and artifacts store questions (SQL), not answers.
- **Progressive disclosure**: skills advertise a line and load on demand; connections connect on first use.
