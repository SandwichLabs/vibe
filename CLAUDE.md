# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Vibe is an **agentic desktop workspace for working with data where it already lives** — DataGrip/DBeaver-shaped, with an AI agent, live notebooks, skills and A2UI artifacts, all exposed to external agents over MCP. Data stays resident in **connections** (local DuckDB, MotherDuck, Postgres, SQLite, S3/GCS/R2, … attached into one DuckDB hub); a **workspace** is a folder (optionally a git repo) holding notebooks, queries, skills and secret-free connection definitions; **credentials** live in the OS keychain or env vars; `.vibe` is an import/export format.

**North star:** `docs/direction.md` (2026-10-06). Read it before planning any feature work. **Plan:** `docs/plans/workspaces-and-connections.md`. **Tracking:** epic #105 (issues #106–#119), workspaces first, then connections.

**Direction change (2026-10-06) — status:** Slice 1 is done. **Studio, the CLI, the TUI and `vibe mcp serve` all open a workspace folder** (`vibe.yaml`, file-backed `notebooks/` `queries/` `skills/`, a DuckDB hub over its connections). The SQLCipher `.vibe` container and `internal/container` are **gone** (#111, with the no-legacy-support decision on #105): nothing opens a `.vibe` file, there is no Save / Revert / dirty tracking, and data writes go straight to the connection. Still to come: the connections UI and keychain (#114–#117) and the `.vibe` *bundle* exchange format (#118). New work goes behind the interfaces the plan names (`internal/session`, `internal/workspace`, `notebooks.Store`/`queries.Store`/`skills.Store`, `internal/hub`, `internal/secrets`, `internal/connections`). Never store a credential in a config file, a bundle, a log line, or anything the agent can read.

**Earlier direction change (2026-09-22):** Vibe was previously aimed at Microsoft Access 97 application parity (bound forms, subforms, switchboard, App Mode, macros). That direction is **superseded**. The Access application layer was removed in #68; datasheet editing stays. The parity docs are kept for history and carry a superseded banner. Do not add forms, macros, nav, field meta, or App Mode.

**Status:** Workspaces, the DuckDB hub, CRUD, import/export, Wails+Vue Studio (launcher, switcher, read-only/take-over, conflict banners), agent + MCP are all working. The **primitive migration** is complete: one agent tool surface (#63), Skills (#62), A2UI artifacts (#67), Notebooks with agent task-mode authoring (#66), headless `vibe mcp serve` (#64), guardrails (#70), removal of the Access layer (#68 — forms/subforms/relationships-UI/switchboard/macros/App Mode/banded reports are gone, foreign keys are plain SQL, reports migrate into notebooks), and retirement of the page system (#69 — web preview server, page renderers, Studio page views/bindings, and the page agent tools are gone; the TUI is a SQL REPL + read-only table browser + notebook list). Multi-user / Tailscale mesh remains deferred.

**Prior art to port from:** `../schoolwork-workspace/` (notebook model `src/notebook/`, A2UI `src/a2ui/`, skills and task mode `agent/{skills,task,tools}.go`, guardrails `src/agent/`). It is on the same `charmbracelet/fantasy` framework as `internal/llm`.

## Tech Stack

- **Language:** Go (1.24+)
- **CLI Framework:** Cobra + Viper (config management)
- **Workspace:** a folder with `vibe.yaml`; notebooks, queries and skills are plain files written with compare-and-write by content hash (`internal/filestore`); per-person settings live outside the folder (`internal/workspace`)
- **Query/Data Engine:** go-duckdb (embedded DuckDB) — **single engine** for queries and CRUD; `internal/session` attaches every connection into one hub
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
go test -v ./internal/workspace/
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
├── app.go                 # Go backend — workspace-bound methods; one a.requireWorkspace() gate
├── workspace.go           # open/create/switch/close, hub lock, watcher, recents, settings, conflicts,
│                          #   Studio's MCP switch, Claude config (#111)
├── jsc_linux.go           # moves WebKit's GC signal off SIGUSR1 so a hub release request can't crash Studio
└── frontend/              # Vue 3 UI. Views: Launcher (replaced Unlock), Dashboard (Home), Notebooks/Notebook,
                           #   TableBrowser (tables grouped by connection), QueryEditor, Skills, Import,
                           #   Settings (Workspace | App), AgentPanel. Shell: WorkspaceSwitcher, WorkspaceBanner
                           #   (read-only), TakeOverDialog, ConflictBanner, NewWorkspaceDialog, McpApprovals.
                           #   State and flows: composables/useWorkspace.ts. Styles: workspace.css
                           #   Removed (#68): FormBuilder, FormView, MacroBuilder, SchemaBuilder,
                           #   SchemaDesigner, Relationships, AppShellView, ReportView, App Mode
                           #   Removed (#69): PagesView, PageEditorView, PageRuntimeView
                           #   Removed (#111): UnlockView, useFileDirty, Save/Revert/Close file, file:* events

internal/
├── workspace/             # #109: a folder + vibe.yaml. Strict vibe.yaml schema, walk-up Discover, Init, Open,
│                          #   three connection scopes (user < shared < local; the local file lives in the user config dir, never in the folder) merged with provenance, EffectivePolicy
│                          #   (committed sandbox/ask = a ceiling that can only tighten), ${ENV} interpolation,
│                          #   credential Fingerprint, OpenSession (session.New + connections), fsnotify Watcher,
│                          #   duplicate-id detection. Reference: docs/reference/workspaces.md
├── filestore/             # #109: safe paths (never outside the root), atomic writes, compare-and-write (hash) -> ErrConflict
├── session/               # DuckDB hub (#108): hardened Session (catalog-aware Query/Exec/Conn, Attach/Detach,
                           #   Connections() status, schema cache, quoting helpers); New() is the workspace hub
├── engine/                # Query layer over a session.Session (Manager); table-ref parsing (ParseTable/ResolveTable/TableSQL)
├── crud/                  # Transactional, type-coercing row insert/update/delete (stays)
├── importer/              # CSV / xlsx import
├── queries/               # Named query `Store` (`FileStore` -> queries/<id>.sql with a `-- vibe:` header; a legacy SQLite Registry survives for tests)
├── agents/                # Tool registry (ToolDef), sandbox modes, approval queue, JSON-RPC handler;
│                          #   reads content via ContainerAccess.Notebooks/Queries/Skills stores (never the SQLite
│                          #   handle) and policy via `PolicySource` (`Workspace.AgentPolicy()`)
├── llm/                   # In-app agent runner on fantasy (consumes agents.Handler tools, #63; task mode, #66)
├── mcp/                   # MCP server bridging agents.Handler tools (SSE; stdio via `vibe mcp serve`, #64),
                           #   plus ProxyStdio: the stdio ⇄ SSE bridge `vibe mcp serve` uses when Studio owns the hub (#112)
├── filelock/              # crash-safe non-blocking flock + holder payload; behind the workspace hub lock (#112)
├── hub/                   # who owns a workspace's DuckDB hub (#112): AcquireHub / ReadHolder / TakeOver on
                           #   .vibe-local/hub.lock, Serve (`vibe mcp serve` owner/proxy), StartWorkspaceMCP,
                           #   NewAgentHandler, BuildClaudeConfig, ProtectStdout. Same-user trust, see docs/mcp.md
├── workspace/             # workspace folder, scopes, file stores, watcher (#109); AgentAccess adapts a
                           #   workspace + session to agents.ContainerAccess
├── prefs/                 # app preferences and the recent-workspaces list (outside every workspace)
├── a2ui/                  # A2UI component schema + validator (#67); Go validates, Vue renders
├── notebooks/             # Notebook model (#66) + `Store` (`FileStore` -> notebooks/<id>.vnb; a legacy SQLite impl survives for tests)
├── connections/           # Connection type registry (#115): postgres, motherduck, sqlite, s3/gcs/r2 on session.ConnectionType;
                           #   field metadata, secret/attach SQL emitters, Test (throwaway session; MotherDuck in a subprocess).
                           #   See docs/reference/connections.md. Credentials never in an ATTACH string.
├── tui/                   # BubbleTea TUI (#69, on workspaces in #113): SQL REPL on the session, read-only table
                           #   browser across every attached connection, notebook list from the workspace's
                           #   store. No save step (writes land in their connection). No page viewer/editor.
├── templates/             # Starter templates (tables + seed rows + a notebook): `vibe workspace init --template`
                           #   and Studio's New workspace dialog
├── docsref/               # Test keeping docs/reference/notebooks.md in sync with the a2ui component
                           #   registry and the starter templates
├── secrets/               # Credentials (#114): Store (keychain via go-keyring, timeout on every call; env:PREFIX;
                           #   prompt/in-memory), Resolver (per-person sign-in + fingerprint binding), Redactor +
                           #   slog handler, CredentialStatus. Leaf package: imports neither session nor workspace
└── config/                # Configuration types; LLM key resolution + ~/.vibe.yaml migration (llmkey.go, yamledit.go)
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
(the page executor/template/linker, and later the whole `internal/pages` parser with the page/report migrations in #111), Studio's page bindings
(`ListPages`/`GetPage`/`CreatePage`/`CreateHTMLPage`/`UpdatePage`/`DeletePage`/
`RenderPageContent`/`CancelRender`) and views (`PagesView`, `PageEditorView`,
`PageRuntimeView`), the `_dashboard` system page (`GetDashboard`/`GetFileInfo`
now report live table/notebook/query counts directly, no page render), and
the agent tools `create_page`/`update_page`/`delete_page`/`list_pages`/
`get_page`. The old `page` tool-category was renamed `content` (it also
covers `create_or_update_skill`, `add_query_cell`, `create_dashboard`) —
grep for `"content"` in `internal/agents`/`internal/llm`/`cmd`/`studio` if you're
hunting write-category gating.

Removed in #111 (the container): `internal/container` (the SQLCipher `.vibe` envelope with its DuckDB blob,
`_vibe_*` tables, Save/Revert, dirty tracking, the legacy SSE MCP and the passphrase flows), Studio's
`OpenFile`/`CreateFile`/`CreateFromTemplate`/`SaveFile`/`RevertFile`/`CloseFile*` bindings, `SelectVibeFile`,
`GetAllMeta`/`SetMeta`, and the `file:opened`/`file:closed`/`file:dirty`/`file:reverted` events (now
`workspace:opened`/`workspace:closed`/`workspace:locked`/`workspace:releasing`/`workspace:changed`/
`workspace:changed-on-disk`). The container-era engine spike tests went with it; FK behaviour is covered by the
crud and studio tests.

New packages per the direction doc: `internal/a2ui` (validator, #67), `internal/llm/skills/` (embedded SKILL.md, #62; also the skills `Store`: `FileStore` -> skills/<name>/SKILL.md, SQLite impl for .vibe), notebook storage (#66), `internal/workspace` + `internal/filestore` (#109), `internal/hub` (#112).

## Conventions (from Sandwich Labs Go projects)

- **`internal/` not `pkg/`** — use `internal/` for application packages (enforces import boundaries)
- **Cobra + Viper** — all CLI commands via Cobra; config via Viper with YAML files + env var overrides
- **Structured logging** — use `log/slog` with JSON handler; pass `*slog.Logger` via dependency injection
- **Error wrapping** — always wrap errors with context: `fmt.Errorf("failed to open workspace: %w", err)`
- **Interface-based design** — define interfaces for testability; mock implementations in `mock.go` alongside the package
- **Co-located tests** — test files next to source (`service.go` / `service_test.go`); use `testify/assert` and `testify/require`
- **Functional options** — use builder/options pattern for complex object construction
- **Dependency injection** — create services in `main.go`/`cmd/root.go` and pass them down; no globals
- **CGO_ENABLED=1** — required for the DuckDB C bindings
- **Goreleaser** — use `.goreleaser.yaml` for cross-platform release builds with ldflags for version injection
- **No hard-coded catalog** — never write `'main'` / `'memory'` / `'analytics'` in code. Ask `Session.DefaultCatalog()`, introspect through `Session.ListTables/DescribeTable` (filtered by `database_name`), and name tables `catalog.schema.table` through `session.QuoteIdent`/`TableRef.SQL()`. Unqualified names resolve against the connection's current catalog (`current_database()`/`current_schema()` in SQL), which is the session default unless a run pinned one with `USE`
- **One tool surface** — new agent capabilities are `ToolDef`s in `internal/agents`, never bespoke `fantasy` tools in `internal/llm`
- **Model output never reaches a style attribute** — A2UI colors are a closed enum; validators reject unknown fields
- **No hex in stylesheets** — every colour in `studio/frontend` is a `--vibe-*` role (or a bridged `--sh-*` token); translucent variants use `color-mix()` on a role. Theme files are validated by `theme/schema.ts` (hex only, closed role list). Built-ins must pass the AA contrast spec in `theme/schema.spec.ts`
- **Focus is visible** — `theme/bridge.css` owns one `:focus-visible` ring; never `outline: none` without it. Ad-hoc modals use `composables/useModal.ts` (Escape, focus move/return)
- **QA in a real browser** — Rodney against the Wails-bound dev server at `:34115`; Vitest specs are supplementary, never the acceptance criterion (see `RODNEY.md`)

## Key Concepts

**Workspace (#109, #111):** a folder holding `vibe.yaml` (id, name, default connection, the agent's team limit, shared connections) beside `notebooks/*.vnb`, `queries/*.sql`, `skills/<name>/SKILL.md` and `data/`. Per-person settings (agent loosening, sign-in methods, private connections, the MCP port) live in the user config directory, never in the folder, and are ignored for a folder that is not the one they were made for. The committed config can only *tighten* the agent policy. `.vibe` is reserved for the future bundle exchange format (#118); today a `.vibe` file is refused with a plain message.

**Workspace lifecycle (Studio):** Open = `workspace.Open` → `hub.AcquireHub` (held by another process ⇒ open **read-only**: local DuckDB files show as locked, a banner offers Take over) → `workspace.OpenSession` (local `duckdb` connections attach eagerly, the rest lazily) → file watcher. Close = MCP stop → watcher stop → session close → hub release (the lock goes last) → workspace close. Switching flushes pending notebook saves first (frontend `flushAll`), releases the old hold, then opens the next; one releasable hold per process. A folder with no `vibe.yaml` is never initialised silently (the UI asks first, then `CreateWorkspace` with `inPlace`). Another program can ask Studio to let go (SIGUSR1; `studio/jsc_linux.go` moves WebKit's GC signal to SIGUSR2). `App.sessionID` is regenerated on every open and every `SaveNotebook` must carry the one it loaded under.

**Autosave replaces Save:** notebooks, queries and skills are files saved with a revision (content hash); a stale save returns `ErrConflict`, which Studio marks `conflict: …` so the view shows the conflict banner (Reload from disk / Keep mine / Save mine as a copy, via `ResolveConflict`). The watcher reports external changes (`workspace:changed-on-disk`); with no unsaved edit the view reloads quietly. Table data is written straight to its connection; there is no Save, Revert or unsaved-changes prompt.

**Credentials and secrets (#114):** `internal/secrets`. One JSON blob per connection in the OS keychain (`user`, `password`, `token`, `key_id`, `secret`, fingerprint, server) keyed by `{Scope, Owner, Name}` (workspace id + connection name; user connections by name; the LLM key `{app, llm-<provider>}`). The per-person sign-in method (`keychain`, `prompt`, `env:PREFIX` -> `PREFIX_USER|PASSWORD|TOKEN|KEY_ID|SECRET`; MotherDuck is `env:MOTHERDUCK` -> `MOTHERDUCK_TOKEN`) lives in `local.yaml` / the user file. **No plaintext-file fallback, ever**; every keychain call runs under a timeout and a hang is `ErrUnavailable`. Only secrets are write-only (set / clear / `CredentialStatus`, never read across the Wails bridge, MCP or agent); username and key id are readable. A credential is bound to `workspace.Fingerprint`; a mismatch is `ErrFingerprintChanged` and hands out no secret. `secrets.Redactor` (every resolved value plus `password='...'`/`token=`/`secret=` patterns) is shared with the session (`session.WithRedactor`, `Session.Redact`) and wraps the slog handlers. Secret DDL goes only through `Session.CreateTemporarySecret` (dedicated conn, attach mutex, `QuoteLiteral`, never logged), never `engine.Manager` (which logs statements at Debug). LLM key order: `VIBE_LLM_API_KEY` -> provider env -> keychain -> legacy `~/.vibe.yaml` `llm.api_key` (read-only; the one-time migration in Studio and `vibe config llm-key migrate` blanks it). Tests/QA: `VIBE_KEYRING_SERVICE`, `VIBE_CONFIG_FILE`.

**Notebooks (#66; format v2 #108):** JSON documents of `sql` / `markdown` / `a2ui` cells. v2 adds an optional `connection` on sql cells and a2ui `sql` bindings (the default catalog for that statement's unqualified names, on a pinned connection; unset = session default); v1 loads as v2. No inter-cell references; outputs are never persisted, every data cell re-runs on open. SQL cells render by synthesizing a one-component A2UI page. The agent authors cells in **task mode**: `preview_query` before a terminal `write_cell` or `need_more_info`; the result is an envelope built from recorded tool calls, never model prose.

**Skills (#62):** `SKILL.md` documents (`name`, `description` frontmatter + markdown instructions). The system prompt advertises name and description; the agent fetches bodies with `read_skill`. Built-ins are embedded; user skills live in the workspace's `skills/` folder. Table-bound skills load only when the table has rows.

**Artifacts / A2UI (#67):** `{ title, components[] }`, thirteen component types, flat list nested by `parent` id, closed color enum, unknown fields rejected. `stat` / `table` / `barChart` / `lineChart` take inline data or a live `sql` binding. Go validates, Vue renders.

**Hub ownership (#112):** one process per workspace owns the DuckDB hub, recorded by a flock on `.vibe-local/hub.lock` (0600 JSON: app/pid/host, plus `mcp {url, token}` while serving MCP and `control: sigusr1` when releasable). `vibe mcp serve` owns the hub if free, else proxies stdio to the owner's MCP; `hub.TakeOver` asks an owner to release via SIGUSR1. The lock file is trusted for the same user only (owner uid, no group/world bits, flock really held, loopback http endpoint); the token never goes in argv, config snippets, structured logs or errors (only `--transport sse` prints it, once, to stderr). The lock is opened `O_NOFOLLOW` and must be a single-link regular file you own; `--sandbox` in proxy mode is enforced by the owner via `X-Vibe-Sandbox` (tighten-only per session). Over stdio, fd 1 is redirected to stderr (`hub.ProtectStdout`) so nothing but JSON-RPC reaches stdout. No save-on-exit: writes go straight to their sources.

**Agent interface:** One tool registry (`internal/agents`) consumed by the in-app runner and exposed over MCP. Sandbox modes `read-only`, `read-write`, `full`, with the workspace's "ask before changes" gate (default on): Studio chat shows an inline approval card; a write through Studio's own MCP switch shows the same card in the shell; headless `vibe mcp serve` refuses (nobody to ask). Guardrails (#70): grounding check, empty-result explanation, tool-result budget.

**Pages (retired, #69; code deleted #111):** The old markdown + named SQL blocks + directives system is gone, along with `internal/pages` and the page/report-to-notebook migrations: old `.vibe` files are not opened at all. See `docs/reference/notebooks.md`. Do not add directives, execution, or rendering.

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
