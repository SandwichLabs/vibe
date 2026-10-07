# Vibe — Implementation Plan

**Based on:** `PROJECT.md` v0.1.0-draft
**Structure:** 6 phases, each with concrete deliverables, files to create, and acceptance criteria.
**Convention:** All packages live under `internal/` per Sandwich Labs Go conventions (not `pkg/` as in the spec's diagrams).

> **Status (2026-09-22):** Phases 0–6 here are **complete** and this document is historical.
> The Access-parity arc that followed it is **superseded**. Forward planning lives in
> **[`docs/direction.md`](../direction.md)** (primitive migration, issues #62–#70).

---

## Phase 0 — Foundation

**Goal:** Open, read, write, and close `.vibe` files. Prove the container format (SQLCipher envelope + materialized DuckDB blob).

### 0.1 Project Scaffold

Create the Go module, build tooling, and directory structure.

**Files:**
```
go.mod
go.sum
main.go                         # calls cmd.Execute()
Taskfile.yml                    # task build/test/lint/mod
.goreleaser.yaml                # release config (stub)
.golangci.yml                   # linter config
cmd/
  root.go                       # Cobra root + Viper init, --config flag
```

**Acceptance:**
- `task build` produces a `vibe` binary.
- `task lint` passes.
- `vibe --help` prints usage.

### 0.2 Container Package

The core `.vibe` file lifecycle: create, open (decrypt), save, close.

**Files:**
```
internal/container/
  container.go                  # Container struct, Open(), Save(), Close()
  schema.go                     # DDL for all _vibe_* tables, migration runner
  crypto.go                     # SQLCipher PRAGMA setup, passphrase handling
  duckdb.go                     # Blob ↔ temp file lifecycle, DuckDB open/close
  options.go                    # Functional options: WithPassphrase, WithLogger, etc.
  container_test.go             # Round-trip tests
  mock.go                       # MockContainer interface for downstream tests
```

**Key decisions:**
- `Container` holds `*sql.DB` (SQLCipher) + `*sql.DB` (DuckDB) + metadata.
- `Open(path string, opts ...Option) (*Container, error)` — decrypts, runs migrations, materializes DuckDB.
- `Save() error` — checkpoints DuckDB, writes blob back to SQLCipher.
- `Close() error` — closes both DBs, securely deletes temp file.
- Schema version checked on open; migrations run sequentially.

**Acceptance:**
- Create a new `.vibe` file with passphrase → verify SQLCipher PRAGMAs are set.
- Write sample data to DuckDB → Save → Close → Reopen → verify data persists.
- Open with wrong passphrase → returns clear error.
- Close → temp directory is removed.
- `_vibe_meta` contains required keys (version, format_version, created_at, etc.).

### 0.3 CLI Commands: `init` and `info`

**Files:**
```
cmd/
  init.go                       # vibe init <file> [--from template] [--import csv]
  info.go                       # vibe info <file> — print metadata without passphrase
```

**Acceptance:**
- `vibe init project.vibe` prompts for passphrase, creates a valid `.vibe` file.
- `vibe info project.vibe` prints title, format version, page count, table count, file size.

### 0.4 Config Package

**Files:**
```
internal/config/
  config.go                     # Config struct, Viper binding, defaults
```

---

## Phase 1 — Page Engine

**Goal:** Parse the page format, execute SQL blocks, resolve directives into a structured AST. No rendering yet.

### 1.1 Query Engine

Query interface executing against DuckDB. *(As-built note: the original dual-engine
design routed to DuckDB **or** SQLite; this was simplified to a single DuckDB engine.
`sqlite.go` remains in the tree but is unused.)*

**Files:**
```
internal/engine/
  engine.go                     # Manager: Execute(sql, params) → ResultSet (DuckDB)
  duckdb.go                     # DuckDB driver implementation
  sqlite.go                     # legacy SQLite driver — present but unused
  params.go                     # Parameter binding, $param substitution, sanitization
  result.go                     # ResultSet type: Columns []Column, Rows [][]any
  engine_test.go
```

**Acceptance:**
- Execute a SELECT against DuckDB → returns ResultSet with typed columns.
- Parameter binding: `$customer_id` replaced safely, no SQL injection.
- All page SQL blocks and CRUD run on DuckDB (no engine selection).

### 1.2 Named Query Registry

CRUD for `_vibe_queries`.

**Files:**
```
internal/queries/
  registry.go                   # List, Get, Create, Update, Delete named queries
  registry_test.go
```

### 1.3 Page Parser & AST

Parse markdown pages with SQL blocks and directives into an AST.

**Files:**
```
internal/pages/
  ast.go                        # Node types: Text, SQLBlock, Directive, Frontmatter
  parser.go                     # Parse(content string) → *Page (frontmatter + []Node)
  executor.go                   # Execute(*Page, *Engine, params) → *ExecutedPage (nodes with results)
  linker.go                     # Resolve page links, validate params
  parser_test.go                # Test each directive syntax variant
  executor_test.go              # Test SQL execution + result binding
```

**AST node types:**
```go
type Node interface{ nodeType() string }

type TextNode struct{ Content string }
type SQLBlockNode struct {
    Name   string
    Cache  time.Duration
    SQL    string
}
type DirectiveNode struct {
    Type   string        // "table" | "chart" | "metric" | "form" | "detail" | "if"
    Config map[string]any
}
```

**Acceptance:**
- Parse the four sample pages from Appendix A → valid ASTs with correct node types.
- Execute a page with two SQL blocks → second block can reference first block's result.
- Directive config is parsed correctly (nested fields, arrays for form fields).
- Invalid directive syntax → clear parse error with line number.

---

## Phase 2 — TUI Shell (Vibe Studio)

**Goal:** A functional BubbleTea TUI that opens `.vibe` files and renders pages interactively.

### 2.1 TUI Application Scaffold

**Files:**
```
internal/tui/
  app.go                        # Top-level BubbleTea model, Init/Update/View
  layout.go                     # Three-panel layout: sidebar + content + REPL
  sidebar.go                    # Page list, table list, query list
  keymap.go                     # Key bindings (Tab, j/k, Enter, etc.)
  styles.go                     # Lipgloss style definitions
  help.go                       # Help overlay
```

### 2.2 TUI Renderers

Render AST nodes as terminal widgets.

**Files:**
```
internal/renderer/
  types.go                      # RenderNode interface, shared types
  tui/
    text.go                     # Markdown rendering via Glamour
    table.go                    # Scrollable, sortable table widget
    chart.go                    # ASCII bar charts, sparklines
    metric.go                   # Large-font styled values
    form.go                     # Interactive input fields, select dropdowns
    detail.go                   # Key-value card/list display
    renderer.go                 # TUIRenderer: []Node → []tea.Model
```

### 2.3 TUI Features

**Files:**
```
internal/tui/
  page_view.go                  # Render a page in the content pane
  table_browser.go              # Browse user tables (CRUD rows)
  query_editor.go               # Edit + run saved queries
  repl.go                       # SQL REPL in bottom panel
  page_wizard.go                # New page creation wizard
  editor.go                     # $EDITOR integration for page editing
  save.go                       # Save flow, unsaved-changes prompt
```

**Acceptance:**
- Open a `.vibe` file → sidebar shows pages, tables, queries.
- Select a page → content pane renders markdown, tables, metrics, charts.
- Form submission → inserts a row, page refreshes.
- SQL REPL → execute ad-hoc queries, see results inline.
- `e` on a page → opens `$EDITOR`, re-parses on close.
- `s` saves the file. `q` prompts if unsaved changes.
- Tab cycles focus between panels.

### 2.4 Studio CLI Command

**Files:**
```
cmd/
  studio.go                     # vibe studio [flags] <file.vibe>
```

---

## Phase 3 — Web Preview Server

**Goal:** Vibe Studio serves pages as HTML in a browser for live preview.

### 3.1 HTTP Server

**Files:**
```
internal/server/
  server.go                     # http.Server setup, lifecycle (start/stop)
  routes.go                     # Route registration
  handlers.go                   # Page render, form submit, table browse, API, SSE
  middleware.go                 # Logging, recovery, localhost-only enforcement
  sse.go                        # SSE event broadcaster for live reload
  static/                       # Embedded CSS, vendored Chart.js
  templates/
    base.html                   # Base layout: sidebar, content area, Chart.js
    page.html                   # Page template
    table_browser.html          # Datasette-style table view
    index.html                  # Page index/listing
```

### 3.2 HTML Renderers

**Files:**
```
internal/renderer/html/
  renderer.go                   # HTMLRenderer: []Node → template data
  table.go                      # <table> with sticky headers, sort, pagination
  chart.go                      # <canvas> + Chart.js init script
  metric.go                     # Styled metric card
  form.go                       # <form> with labeled inputs
  detail.go                     # <dl> key-value display
  layout.go                     # Page-level HTML assembly
```

**Routes:**
```
GET  /                          → page index
GET  /page/:id                  → render page
POST /page/:id/submit/:form_id  → form submission
GET  /table/:name               → table browser
GET  /table/:name.json          → JSON export
GET  /table/:name.csv           → CSV export
POST /api/query                 → ad-hoc SQL (opt-in)
GET  /api/schema                → schema introspection
GET  /events                    → SSE live reload
GET  /assets/:id                → binary assets
```

**Acceptance:**
- Press `w` in TUI → server starts on localhost:3456, URL printed.
- Browser renders pages with charts, tables, metrics, forms.
- Form submit → POST → data inserted → page re-renders.
- Edit a page in TUI → browser auto-refreshes via SSE.
- `/table/orders` shows paginated, sortable table browser.
- Export: `/table/orders.csv` downloads CSV.

---

## Phase 4 — Agent Interface

**Goal:** External AI agents can connect to Vibe Studio and interact with `.vibe` files via JSON-RPC.

### 4.1 Agent Core

**Files:**
```
internal/agents/
  protocol.go                   # JSON-RPC 2.0 message types, marshal/unmarshal
  tools.go                      # Tool definitions: introspect, query, CRUD, page ops, schema ops
  server.go                     # Transport: stdio reader/writer, socket listener
  sandbox.go                    # Permission enforcement per sandbox mode
  handler.go                    # Dispatch method → tool, validate params, enforce sandbox
  approval.go                   # Approval queue for write ops (hooks into TUI)
  agents_test.go
```

### 4.2 TUI Agent Panel

**Files:**
```
internal/tui/
  agent_panel.go                # Agent connection status, tool call log, approve/reject
```

### 4.3 CLI Integration

**Files:**
```
cmd/
  studio.go                     # Add --agent-cmd, --agent-listen, --agent-mode flags
```

**Acceptance:**
- `vibe studio --agent-mode stdio project.vibe` — agent sends JSON-RPC on stdin, gets responses on stdout.
- `introspect_schema` → returns all tables with columns and types.
- `run_query` with `engine=duckdb` → returns ResultSet as JSON.
- `create_record` in `read-only` mode → permission denied error.
- `create_page` in `full` mode with `require_approval=true` → TUI shows approval prompt.
- Agent panel shows real-time call log.

---

## Phase 5 — Desktop App (Wails GUI)

**Goal:** A polished Wails desktop app for both builders (Studio Mode) and non-technical users (App Mode). *(As-built: this became the `studio/` Wails app with a Vue 3 frontend and a runtime Studio/App mode toggle, not a separate `cmd/vibe-viewer` binary.)*

### 5.1 Wails Application

**Files:**
```
studio/
  main.go                       # Wails bootstrap, file association
  app.go                        # Go backend: Container + Engine + page execution + CRUD + agent
```

### 5.2 Vue 3 Frontend

**Files:**
```
studio/frontend/
  index.html
  package.json
  src/
    App.vue                     # Root: routes by mode (Unlock → Studio/App shell)
    views/
      UnlockView.vue            # Passphrase entry
      PageEditorView.vue        # Page editor + live preview (Studio Mode)
      DashboardView.vue         # Rendered dashboard
      TableBrowserView.vue      # Sortable/filterable table explorer + CRUD
      QueryEditorView.vue, ImportView.vue, SettingsView.vue, PagesView.vue
    components/                 # Chart, DataTable, Form, Metric, Detail, …
    router/                     # mode-aware routing + capability gating
```

### 5.3 Platform Integration

- macOS: `Info.plist` for `.vibe` file association.
- Windows: Registry setup in installer.
- Linux: `.desktop` file with `MimeType=application/x-vibe`.

**Acceptance:**
- Double-click a `.vibe` file → app opens, prompts for passphrase.
- Correct passphrase → sidebar shows pages, content renders with charts/tables/metrics.
- Form submission works (insert/update).
- Table browser with sort/filter/pagination.
- Export: CSV, JSON, print-to-PDF.
- Dark/light theme toggle.
- In **App Mode**, the user cannot edit pages, run SQL, or access internal tables.

---

## Phase 6 — Hardening & Polish

**Goal:** Production readiness.

### 6.1 Security Hardening

- Encrypted DuckDB temp file VFS (ChaCha20-Poly1305 streaming, HKDF-derived subkey).
- Memory zeroing on session close.
- Audit all SQL paths for injection.

### 6.2 CLI Utilities

**Files:**
```
cmd/
  export.go                     # vibe export --table/--page --format csv/html/json
  import.go                     # vibe import --table --csv
  rekey.go                      # vibe rekey (change passphrase)
  compact.go                    # vibe compact (VACUUM both engines)
  dump.go                       # vibe dump (schema + pages to stdout)
  verify.go                     # vibe verify (integrity check)
```

### 6.3 Performance & Reliability

- Profile large datasets (100MB+ DuckDB blobs, 50+ pages).
- Progress indicators for slow operations (blob materialization).
- Graceful crash recovery (orphaned temp file detection).
- DuckDB memory limit default (`512MB`), configurable in `_vibe_meta`.

### 6.4 Documentation & Samples

- User guide.
- Page format reference.
- Agent API reference.
- Sample `.vibe` files: project tracker, expense report, dataset explorer.

### 6.5 Release Pipeline

- `.goreleaser.yaml` for cross-platform builds (macOS, Windows, Linux).
- Signed binaries.
- `ldflags` version injection.
- Installers for the desktop app (DMG, MSI, AppImage).

**Acceptance:**
- All utility commands work correctly.
- 100MB DuckDB blob round-trips without corruption.
- Crash during save → file is not corrupted (atomic write via `BEGIN IMMEDIATE`).
- `vibe verify` detects and reports integrity issues.

---

## Dependency Graph

```
Phase 0 ─── Foundation (container, init, info)
   │
   ▼
Phase 1 ─── Page Engine (parser, AST, executor, query engine)
   │
   ├──────────────┬──────────────────────┐
   ▼              ▼                      ▼
Phase 2        Phase 3               Phase 4
TUI Shell      Web Server            Agent Interface
   │              │                      │
   └──────┬───────┘                      │
          ▼                              │
       Phase 5 ── Desktop App ◄──────────┘
          │
          ▼
       Phase 6 ── Hardening & Release
```

Phases 2, 3, and 4 can proceed in parallel once Phase 1 is complete — they all consume the same page engine and query infrastructure. Phase 5 reuses the HTML renderer from Phase 3 and the Go backend from Phase 2. Phase 6 is a cross-cutting pass.

---

## Technical Risks

| Risk | Mitigation | Phase |
|------|-----------|-------|
| go-sqlcipher + large DuckDB blobs | Test with 100MB+ blobs early in Phase 0 | 0 |
| go-duckdb serialize/deserialize stability | Write stress tests for blob round-trips | 0 |
| Temp file cleanup on crash | `defer` + signal handler + startup orphan detection | 0, 6 |
| BubbleTea complexity for nested interactive widgets | Keep widgets stateless where possible; isolate state per pane | 2 |
| Chart.js bundle size in the app | Vendor a minimal Chart.js build; tree-shake unused chart types | 3, 5 |
| Wails cross-platform CGO (SQLCipher + DuckDB) | Test CI builds for all three platforms early | 5 |
| DuckDB memory spikes on large aggregations | Default `memory_limit=512MB`, expose in config | 1, 6 |
