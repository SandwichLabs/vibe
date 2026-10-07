# Vibe — MVP Specification

**Version:** 0.1.0-draft
**Date:** March 2026
**Status:** Original MVP specification — substantially implemented.

> **Direction note (2026-09-22):** This document is the *original* spec and remains the
> reference for the `.vibe` file format, container lifecycle, and agent protocol. Its
> product framing is **superseded**: Vibe is now an agentic data workspace (tables,
> notebooks, skills, A2UI artifacts, one agent tool surface over MCP), not an Access
> successor. The dual-engine design became DuckDB-only; Studio/Viewer and the Studio/App
> mode split are gone; forms, switchboard, macros and the page-directive model are being
> removed. See **[`docs/direction.md`](../direction.md)** for the plan of record and
> **[`CLAUDE.md`](../../CLAUDE.md)** for the current architecture.

---

## 1. Vision

Vibe is a modern, single-user reimagining of Microsoft Access: a portable, encrypted application container that bundles data, interactive pages, forms, reports, and an AI agent interface into a single `.vibe` file. One desktop app presents two modes — like the editor/reader distinction in a PDF tool, but in a single program:

- **Studio Mode** — the builder surface (Wails v2 + Vue 3) for technical and citizen developers to create, edit, and manage `.vibe` files: schema, pages, forms, reports, queries, and agent chat. A built-in web server previews pages in a browser. This is where data apps are built, by hand or with AI agents. A terminal-native **BubbleTea TUI** offers the same building blocks for power users.
- **App Mode** — the locked-down end-user surface in the *same* app. Open a `.vibe` file, enter the passphrase, and use the switchboard, forms, dashboards, and reports inside. No terminal, no SQL, no schema, no code.

### 1.1 Design Principles

- **File is the app.** A `.vibe` file is fully self-contained. No external dependencies, no server required, no database to configure.
- **Two audiences, one format.** Builders use the TUI + AI agents to create. Everyone else uses the GUI to consume. The file format is the contract between them.
- **AI as collaborator, not dependency.** Agents can create pages, run queries, and manipulate data — but the format is human-readable and every `.vibe` file works without AI.
- **Encryption by default.** Data is encrypted at rest. Opening a `.vibe` file requires a passphrase. No plaintext data touches disk.

### 1.2 Target Users

**Studio Mode (builders):** Power-user individuals — citizen developers and studio developers — who leverage AI agents to build portable data applications. Engineers sharing datasets, analysts building interactive reports, operations leads who currently reach for a Jupyter notebook + SQLite combo (or an Access `.mdb`) but want something more structured and shareable.

**App Mode (data-entry workers):** The recipients. A project manager who receives a `.vibe` file with a team dashboard. An operations lead who fills out data-entry forms packaged by a builder. A client who opens an interactive report. These users never see SQL, never edit schema, and never need to understand the internals — they get a locked-down switchboard, forms, and reports. App Mode is a runtime toggle inside the same app, not a separate program.

### 1.3 Product Analogy

```
                  PDF Ecosystem          Vibe Ecosystem
                  ─────────────          ──────────────
  Author mode:    Adobe Acrobat Pro      Studio Mode (builder + agents)
  Reader mode:    Preview / Reader       App Mode (locked-down end user)
  File format:    .pdf                   .vibe
  Key trait:      Portable, self-        Portable, encrypted,
                  contained documents    self-contained data apps
```

> Unlike the PDF analogy's two separate programs, Studio Mode and App Mode are
> two faces of **one** Wails desktop app (a terminal-native BubbleTea TUI also
> exists for builders). The mode boundary is enforced at the application layer.

---

## 2. File Format Specification

### 2.1 Container Structure

A `.vibe` file is a **SQLite database encrypted with SQLCipher**. It serves as the single container for all application state, configuration, and data.

```
my_project.vibe
└── SQLite database (SQLCipher encrypted)
    ├── _vibe_meta          — file metadata and version
    ├── _vibe_pages         — page definitions (markdown+SQL)
    ├── _vibe_queries       — named, reusable query library
    ├── _vibe_components    — reusable UI component definitions
    ├── _vibe_sources       — data source registry
    ├── _vibe_agent_config  — agent permissions and tool surface
    ├── _vibe_secrets       — encrypted connection strings
    ├── _vibe_assets        — binary assets (images, files)
    └── _vibe_duckdb        — serialized DuckDB database (BLOB)
```

All internal tables use the `_vibe_` prefix. **User data and analytics live inside
the DuckDB blob, not in SQLite-native tables** — the SQLCipher database is the
encrypted *container envelope* (config + the DuckDB blob), not a second query engine.
(The original design allowed optional SQLite-native user tables with per-block engine
routing; this was simplified to a single DuckDB engine. See §10.)

### 2.2 SQLite Schema

```sql
-- File metadata
CREATE TABLE _vibe_meta (
    key     TEXT PRIMARY KEY,
    value   TEXT NOT NULL
);
-- Required keys: version, created_at, updated_at, 
--                format_version, title, description

-- Page definitions
CREATE TABLE _vibe_pages (
    id          TEXT PRIMARY KEY,           -- slug: "sales-dashboard"
    title       TEXT NOT NULL,
    content     TEXT NOT NULL,              -- markdown+SQL page body
    page_type   TEXT DEFAULT 'notebook',    -- notebook | form | dashboard
    sort_order  INTEGER DEFAULT 0,
    created_at  TEXT DEFAULT (datetime('now')),
    updated_at  TEXT DEFAULT (datetime('now')),
    metadata    TEXT DEFAULT '{}'           -- JSON: tags, icon, etc.
);

-- Named query library
CREATE TABLE _vibe_queries (
    id          TEXT PRIMARY KEY,           -- "revenue-by-month"
    title       TEXT,
    sql         TEXT NOT NULL,
    engine      TEXT DEFAULT 'duckdb',     -- duckdb | sqlite
    parameters  TEXT DEFAULT '[]',          -- JSON param definitions
    created_at  TEXT DEFAULT (datetime('now')),
    updated_at  TEXT DEFAULT (datetime('now'))
);

-- Reusable component definitions
CREATE TABLE _vibe_components (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,              -- chart | form | table | metric
    config      TEXT NOT NULL,              -- JSON component config
    created_at  TEXT DEFAULT (datetime('now'))
);

-- Data source registry
CREATE TABLE _vibe_sources (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    source_type TEXT NOT NULL,              -- duckdb | sqlite | remote
    config      TEXT DEFAULT '{}',          -- JSON: connection details
    is_default  BOOLEAN DEFAULT 0
);

-- Agent configuration
CREATE TABLE _vibe_agent_config (
    key     TEXT PRIMARY KEY,
    value   TEXT NOT NULL
);
-- Keys: mode (read-only|read-write|full), allowed_tables,
--        max_query_rows, allowed_operations

-- Encrypted secrets (connection strings, API keys)
CREATE TABLE _vibe_secrets (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    value       TEXT NOT NULL,              -- encrypted with file key
    created_at  TEXT DEFAULT (datetime('now'))
);

-- Binary assets
CREATE TABLE _vibe_assets (
    id          TEXT PRIMARY KEY,
    filename    TEXT NOT NULL,
    mime_type   TEXT NOT NULL,
    data        BLOB NOT NULL,
    created_at  TEXT DEFAULT (datetime('now'))
);

-- DuckDB storage
CREATE TABLE _vibe_duckdb (
    id      INTEGER PRIMARY KEY CHECK (id = 1),  -- singleton
    data    BLOB NOT NULL,
    updated TEXT DEFAULT (datetime('now'))
);
```

### 2.3 Format Versioning

The `format_version` key in `_vibe_meta` uses semver. The runtime checks this on open:

- **Same major version:** open normally.
- **Newer minor version:** open with warning (forward-compatible).
- **Newer major version:** refuse to open, prompt for upgrade.
- **Older version:** run migrations sequentially.

---

## 3. Encryption Specification

### 3.1 Approach: SQLCipher

The entire SQLite database is encrypted using SQLCipher 4. Since DuckDB data is stored as a BLOB within SQLite, all data — application state, user tables, and analytical data — is encrypted at rest with a single key.

### 3.2 Key Derivation

```
User passphrase
       │
       ▼
   PBKDF2-HMAC-SHA512
   (256,000 iterations, 16-byte salt)
       │
       ▼
   256-bit key
       │
       ▼
   SQLCipher AES-256-CBC
   (per-page IV, HMAC-SHA512 page auth)
```

SQLCipher parameters (set via `PRAGMA`):

```sql
PRAGMA key = '<passphrase>';           -- or raw key
PRAGMA cipher_page_size = 4096;
PRAGMA kdf_iter = 256000;
PRAGMA cipher_hmac_algorithm = HMAC_SHA512;
PRAGMA cipher_kdf_algorithm = PBKDF2_HMAC_SHA512;
```

### 3.3 Key Management

- **Passphrase-based (default).** User provides a passphrase on file open. The passphrase is passed directly to SQLCipher which handles KDF internally.
- **Keyfile (optional).** A 256-bit raw key stored in a separate `.vibekey` file. Useful for automated/agent workflows. The keyfile is never stored inside the `.vibe` file.
- **Passphrase change.** Supported via SQLCipher's `PRAGMA rekey`.

### 3.4 Runtime Security

- The decrypted database exists only in memory. No plaintext data is written to disk.
- The DuckDB temp file (materialized from the blob) uses an encrypted temp directory backed by a `tmpfs` mount when available, or is encrypted at the application layer using a derived subkey.
- On application crash, the DuckDB temp file is orphaned but encrypted.
- Memory is zeroed on session close (best-effort, depends on Go runtime/GC).

### 3.5 DuckDB Blob Encryption Detail

When the DuckDB blob is extracted for use:

1. Read `_vibe_duckdb.data` blob from SQLCipher (decrypted on read).
2. Derive a subkey: `HKDF-SHA256(master_key, salt="vibe-duckdb-tempfile", 32 bytes)`.
3. Write blob to a temp file, encrypted with the subkey using ChaCha20-Poly1305 streaming encryption.
4. DuckDB opens via a custom VFS layer that decrypts on read, encrypts on write.
5. On save: DuckDB data is read back through the VFS, re-serialized, and written to the SQLCipher blob in a single transaction.
6. On close: temp file is overwritten with zeros, then deleted.

**MVP simplification:** For the first version, accept the risk of the plaintext temp file and focus on SQLCipher for at-rest encryption. The encrypted VFS for DuckDB temp files is a v2 hardening step.

---

## 4. Page Format Specification

### 4.1 Overview

A **page** is the core authoring primitive. It is a markdown document with two extensions: **SQL blocks** that produce named result sets, and **directives** that bind results to interactive components. Pages unify the concepts of forms, dashboards, and notebooks into a single format.

### 4.2 Anatomy of a Page

```markdown
---
title: Sales Dashboard
type: dashboard
refresh: 30s
---

Introductory prose renders as styled text in both TUI and web.

## Revenue Overview

'''sql revenue_by_month
SELECT strftime(order_date, '%Y-%m') AS month,
       SUM(amount) AS total
FROM orders
GROUP BY 1
ORDER BY 1
'''

{chart: revenue_by_month, type: bar, x: month, y: total, title: "Monthly Revenue"}

The chart above shows monthly revenue trends. Below is the raw data:

{table: revenue_by_month, page_size: 20}

## Quick Stats

{metric: "SELECT SUM(amount) FROM orders WHERE order_date > current_date - INTERVAL '30 days'", label: "Last 30 Days", format: currency}

{metric: "SELECT COUNT(*) FROM orders", label: "Total Orders", format: number}

## Add New Order

{form: orders, fields: [
    {name: customer_id, label: "Customer", type: select, source: "SELECT id, name FROM customers"},
    {name: amount, label: "Amount", type: currency},
    {name: order_date, label: "Date", type: date, default: today}
], on_submit: refresh}
```

### 4.3 SQL Blocks

Fenced code blocks with the `sql` language tag produce named result sets.

**Syntax:**
````
```sql <result_name> [cache=<duration>]
SELECT ...
```
````

- `result_name` is required. It becomes a named dataset available to directives.
- All blocks execute on **DuckDB** (single engine). The original `engine=duckdb|sqlite` option was removed.
- `cache` is optional. If set, results are cached for the specified duration (e.g., `cache=5m`).
- SQL blocks execute top-to-bottom. Later blocks can reference earlier result names as CTEs (implementation detail: the runtime injects previous results as temp views).

### 4.4 Directives

Directives are enclosed in `{}` on their own line. They bind data to UI components.

#### 4.4.1 Table Directive

```
{table: <result_name>, page_size: <n>, sortable: true, searchable: true}
```

Renders a paginated, sortable data table. In the TUI, this is a scrollable table widget. In the web view, an HTML table with client-side sorting.

#### 4.4.2 Chart Directive

```
{chart: <result_name>, type: <chart_type>, x: <column>, y: <column>, 
 title: "<title>", color: <column>}
```

Supported chart types for MVP: `bar`, `line`, `area`, `scatter`, `pie`.

- **TUI rendering:** ASCII bar charts and sparklines via `lipgloss`/`termenv`. Scatter and pie degrade to tables with a note.
- **Web rendering:** Rendered with a JS charting library (Chart.js or similar).

#### 4.4.3 Metric Directive

```
{metric: "<inline_sql>" | <result_name>, label: "<label>", format: <format>}
```

Displays a single value prominently. Can reference a named result (uses first row, first column) or contain inline SQL.

Formats: `number`, `currency`, `percent`, `bytes`, `duration`.

#### 4.4.4 Form Directive

```
{form: <table_name>, fields: [...], on_submit: refresh | redirect:<page_id> | noop}
```

Renders an input form bound to a database table. Field definitions:

```
{name: <column>, label: "<label>", type: <field_type>, 
 default: <value>, required: true, source: "<sql>"}
```

Field types: `text`, `number`, `currency`, `date`, `datetime`, `select`, `checkbox`, `textarea`, `email`, `url`.

The `source` property on `select` fields provides a SQL query for populating options.

On submit, the form generates an `INSERT` statement against the target table. If a record `id` is provided (via URL param or context), it generates an `UPDATE`.

#### 4.4.5 Detail Directive

```
{detail: <result_name>, layout: card | list}
```

Renders a single record (first row of result set) as a key-value display. Useful for record detail pages.

#### 4.4.6 Conditional Rendering

```
{if: "<sql_expression>"}
Content shown when expression returns truthy.
{/if}
```

### 4.5 Parameters

Pages can accept parameters, enabling linked navigation:

```markdown
---
title: Customer Detail
type: detail
params:
  - name: customer_id
    type: integer
    required: true
---

'''sql customer
SELECT * FROM customers WHERE id = $customer_id
'''

{detail: customer, layout: card}
```

Parameters are passed via URL query strings in the web view or via prompt in the TUI.

### 4.6 Page Links

Pages can link to other pages:

```markdown
See [Customer Detail](/page/customer-detail?customer_id=42) for more info.
```

In the TUI, these render as navigable links. In the web view, standard anchor tags.

---

## 5. Runtime Architecture

### 5.1 Shared Core vs. Mode Split

All surfaces share the same core packages under `internal/`. The split is about
**capability surface (Studio Mode vs. App Mode)**, not separate binaries — the Wails
app gates privileged operations at runtime, and a terminal-native TUI reuses the same
core for builders.

```
┌──────────────────────────────────────────────────────────────┐
│                  internal/ (shared core)                     │
│  container/ — file open/close/save/migrate                   │
│  engine/    — DuckDB query execution (single engine)         │
│  pages/     — parser, AST, executor                          │
│  renderer/  — html/ (web + GUI) + tui/ rendering             │
│  queries/   — named query registry                           │
│  agents/    — agent tool surface + transport                 │
│  mcp/       — MCP server (SSE)                                │
│  server/    — embedded HTTP server                           │
└──────────┬───────────────────────────┬─────────────┬─────────┘
           │                           │             │
   ┌───────┴──────────┐    ┌───────────┴─────────────┴────────┐
   │  TUI (internal/  │    │   Wails app (studio/)            │
   │  tui) — builders │    │  ┌────────────┬───────────────┐  │
   │  ├─ page editor  │    │  │ Studio Mode│ App Mode       │  │
   │  ├─ SQL REPL     │    │  │ full r/w   │ read+interact  │  │
   │  ├─ table mgmt   │    │  │ page/form  │ forms submit   │  │
   │  ├─ agent host   │    │  │  editors   │ no SQL/schema  │  │
   │  └─ web preview  │    │  │ schema edit│ no agent write │  │
   └──────────────────┘    │  │ agent chat │ switchboard nav│  │
                           │  └────────────┴───────────────┘  │
                           └──────────────────────────────────┘
```

### 5.2 Core Library (`internal/`)

```
internal/
├── container/           # .vibe file lifecycle
│   ├── container.go     # Open(path, passphrase) → *Container
│   ├── schema.go        # internal table DDL, migrations
│   ├── crypto.go        # SQLCipher PRAGMA setup
│   └── duckdb.go        # blob ↔ temp file lifecycle
│
├── engine/              # query execution (DuckDB single-engine)
│   ├── engine.go        # query interface (Manager routes to DuckDB)
│   ├── duckdb.go        # DuckDB driver, connection pool
│   ├── sqlite.go        # legacy SQLite driver — present but unused
│   └── params.go        # parameter binding, sanitization
│
├── pages/               # page format
│   ├── parser.go        # markdown+directives → AST
│   ├── ast.go           # node types: Text, SQLBlock, Directive, etc.
│   ├── executor.go      # execute page: run SQL blocks, resolve directives
│   └── linker.go        # resolve page links, parameter passing
│
├── renderer/
│   ├── tui/             # BubbleTea components per AST node
│   │   ├── table.go
│   │   ├── chart.go
│   │   ├── form.go
│   │   ├── metric.go
│   │   └── markdown.go
│   ├── html/            # HTML template rendering per AST node
│   │   ├── table.go
│   │   ├── chart.go
│   │   ├── form.go
│   │   ├── metric.go
│   │   └── layout.go
│   └── types.go         # shared RenderResult types
│
├── agents/              # AI agent interface
│   ├── tools.go         # tool definitions
│   ├── server.go        # stdio + socket transport
│   ├── sandbox.go       # permission enforcement
│   └── approval.go      # approval queue for write ops
│
├── mcp/                 # MCP server (SSE) bridging to the agent tools
│
├── server/              # built-in web server
│   ├── server.go        # HTTP server, routes
│   ├── handlers.go      # page rendering, form submission, API
│   ├── static/          # embedded CSS, JS, assets
│   └── templates/       # HTML base templates
│
└── queries/             # named query registry
    └── registry.go      # CRUD for _vibe_queries
```

### 5.2 File Lifecycle

```
Open(path, passphrase)
    │
    ├── 1. Open SQLite with SQLCipher, set PRAGMAs
    ├── 2. Verify format_version, run migrations if needed
    ├── 3. Read _vibe_duckdb blob
    ├── 4. Write blob to encrypted temp file
    ├── 5. Open DuckDB against temp file
    └── 6. Return *Container{sqlite, duckdb, meta}

    ... interactive session (TUI / web / agent) ...

Save()
    │
    ├── 1. Checkpoint DuckDB (CHECKPOINT)
    ├── 2. Read DuckDB file into memory
    ├── 3. BEGIN IMMEDIATE on SQLite
    ├── 4. UPDATE _vibe_duckdb SET data = ?, updated = datetime('now')
    ├── 5. UPDATE _vibe_meta SET value = datetime('now') WHERE key = 'updated_at'
    ├── 6. COMMIT
    └── 7. Return

Close()
    │
    ├── 1. Close DuckDB connection
    ├── 2. Zero-overwrite temp file, delete
    ├── 3. Close SQLite connection
    └── 4. Release file lock
```

### 5.3 Query Execution Flow

```
Page Render Request
    │
    ├── Parse page content → AST
    ├── For each SQLBlock node (top to bottom):
    │   ├── Bind parameters ($param_name → value)
    │   ├── Check cache (if cache directive set)
    │   ├── Execute query → ResultSet{columns, rows}
    │   └── Store in session context as named result
    ├── For each Directive node:
    │   ├── Resolve referenced result set
    │   ├── Validate directive config against result schema
    │   └── Emit RenderNode with data + config
    ├── For each Text/Markdown node:
    │   └── Emit as-is
    └── Return ordered []RenderNode to renderer
```

---

## 6. Vibe Studio — TUI Application (Charm/BubbleTea)

### 6.1 Application Model

The TUI is a BubbleTea application with a persistent layout and swappable content panes.

```
┌─────────────────────────────────────────────────────────────────┐
│  Vibe ─ my_project.vibe                          [?] Help  [Q] │
├──────────────┬──────────────────────────────────────────────────┤
│              │                                                  │
│  PAGES       │  Sales Dashboard                                 │
│  ──────      │  ════════════════                                │
│  ▸ Overview  │                                                  │
│    Sales     │  Revenue by Month                                │
│    Customers │  ┌──────────────────────────────────┐            │
│    Inventory │  │ ██████████████████████  $124,500  │            │
│              │  │ ████████████████       $ 98,200  │            │
│  TABLES      │  │ ██████████████████████████ $142k │            │
│  ──────      │  └──────────────────────────────────┘            │
│    orders    │                                                  │
│    customers │  Total Orders: 1,847                             │
│    products  │                                                  │
│              │  ┌─ Add New Order ───────────────────┐           │
│  QUERIES     │  │ Customer: [Select...         ▾]  │           │
│  ──────      │  │ Amount:   [                   ]  │           │
│    rev_month │  │ Date:     [2026-03-03         ]  │           │
│    top_cust  │  │                     [Submit]     │           │
│              │  └──────────────────────────────────┘            │
│              │                                                  │
├──────────────┴──────────────────────────────────────────────────┤
│ sql▸ SELECT count(*) FROM orders WHERE amount > 100             │
│ → 423                                                           │
└─────────────────────────────────────────────────────────────────┘
```

### 6.2 Navigation Model

| Key           | Action                                    |
|---------------|-------------------------------------------|
| `Tab`         | Cycle focus: sidebar → content → REPL     |
| `j/k` or `↑↓`| Navigate within focused pane              |
| `Enter`       | Select page, expand table, submit form    |
| `` ` ``       | Toggle SQL REPL                           |
| `e`           | Edit current page (opens `$EDITOR`)       |
| `n`           | New page wizard                           |
| `d`           | Delete current page (with confirmation)   |
| `/`           | Search pages and queries                  |
| `s`           | Save file                                 |
| `w`           | Start/stop web server                     |
| `a`           | Toggle agent panel                        |
| `q`           | Quit (prompt to save if unsaved changes)  |
| `?`           | Help overlay                              |

### 6.3 TUI Views

**Page View** — The main content area. Renders a page's AST:
- Markdown text → styled via `glamour`.
- SQL blocks → collapsible sections showing query + result count. Expand to see results.
- Table directives → scrollable `lipgloss` table with column sorting (toggle with `<`/`>`).
- Chart directives → ASCII horizontal bar charts, sparklines for line/area. Scatter and pie fall back to table view with a label.
- Metric directives → large-font styled values using `lipgloss`.
- Form directives → interactive input fields with tab navigation. Select fields show a fuzzy-searchable dropdown.

**Table Browser** — Selected from sidebar under "TABLES." Shows all user tables and their row counts. Selecting a table opens a paginated data view with:
- Column headers with type annotations.
- Inline editing (Enter on a cell to edit, Esc to cancel).
- Row insertion (I), deletion (D with confirmation).
- Column-level filtering (`f` opens a filter prompt for the focused column).

**Query Editor** — Selected from sidebar under "QUERIES." Shows saved queries. Selecting one opens a split view: query SQL on top, results on bottom. Execute with `Ctrl+Enter`.

**SQL REPL** — Bottom panel toggled with `` ` ``. Single-line by default, expand to multiline with `Ctrl+E`. History with `↑↓`. Results display inline. All queries run on DuckDB (single engine).

**Agent Panel** — Side panel toggled with `a`. Shows:
- Agent connection status (connected/disconnected).
- Real-time log of tool calls with arguments.
- Approve/reject buttons for write operations (when sandbox mode requires approval).
- Agent output/messages.

### 6.4 Page Editing

Pages are edited externally: pressing `e` exports the current page's markdown to a temp file and opens `$EDITOR` (default: `vim`). On editor close, the file is re-parsed and the page is updated in the database. If parsing fails, the user is prompted to re-edit or discard.

For the MVP, this keeps the TUI simple — no need for a built-in text editor. The structured directives (forms, charts) are defined in markdown and render live after the edit cycle.

### 6.5 New Page Wizard

Pressing `n` opens a step-through wizard:

1. **Page type** — notebook, dashboard, or form.
2. **Title and slug** — auto-generates slug from title.
3. **Template** — offers starter templates per type:
   - Notebook: blank page with one SQL block.
   - Dashboard: two metric directives + one chart + one table.
   - Form: form directive bound to a selected table, auto-populates fields from columns.
4. **Confirm** — creates the page and opens it.

---

## 7. Vibe Studio — Built-in Web Preview Server

### 7.1 Overview

Studio Mode includes an embedded HTTP server for previewing pages in a browser during authoring. This is the "live preview" counterpart to the TUI's editing environment — analogous to a markdown editor's preview pane, but running as a local web server so the author can see exactly what App Mode users will experience. Starting the server is a single keypress (`w`) and serves on `localhost:<port>`.

```
┌──────────────────────────────────────────┐
│  Vibe Studio (TUI)                       │
│  ├── Press 'w' to start preview server   │
│  ├── Serves on http://localhost:3456     │
│  └── Hot-reloads on page save            │
│                                          │
│  Browser (live preview)                  │
│  ├── Renders pages as HTML               │
│  ├── Charts via Chart.js                │
│  ├── Forms submit via POST              │
│  └── SSE for live reload                │
└──────────────────────────────────────────┘
```

The preview server shares the same HTML renderer used by App Mode. What you see in the browser during Studio Mode preview is what App Mode users will see.

### 7.2 Routes

```
GET  /                          → page index (list all pages with links)
GET  /page/:id                  → render page as HTML
POST /page/:id/submit/:form_id  → form submission handler
GET  /table/:name               → Datasette-style table browser
GET  /table/:name.json          → table data as JSON (paginated)
GET  /table/:name.csv           → table data as CSV export
GET  /api/query                 → execute ad-hoc SQL (POST body), returns JSON
GET  /api/schema                → full schema introspection
GET  /events                    → SSE stream for live reload
GET  /assets/:id                → serve binary assets from _vibe_assets
```

### 7.3 HTML Rendering

Pages are rendered server-side using Go's `html/template`. The base template provides:

- A clean, minimal layout inspired by Datasette and Evidence.dev.
- A sidebar with page navigation.
- Embedded CSS (no external dependencies, single-file HTML delivery).
- Chart.js loaded from a vendored copy stored in `_vibe_assets` or embedded in the binary.

Each directive type maps to an HTML template:

| Directive | HTML output |
|-----------|-------------|
| `table`   | `<table>` with sticky headers, sortable columns, pagination controls |
| `chart`   | `<canvas>` with Chart.js initialization script |
| `metric`  | Styled `<div>` with large number, label, and optional trend indicator |
| `form`    | `<div>` with labeled inputs, bound to POST endpoint |
| `detail`  | Definition list (`<dl>`) with styled key-value pairs |

### 7.4 Form Submission Flow

```
Browser: User fills form, clicks Submit
    │
    POST /page/:id/submit/:form_id
    │
    ├── Parse form data
    ├── Validate against field definitions (required, type)
    ├── Generate INSERT or UPDATE SQL
    ├── Execute against DuckDB
    ├── on_submit == refresh → redirect to same page
    ├── on_submit == redirect:X → redirect to page X
    └── on_submit == noop → return 200 with success message

    Error: return 400 with validation errors, re-render form with values
```

### 7.5 Live Reload

The web server exposes an SSE endpoint at `/events`. When a page is saved in the TUI, the server emits a `reload` event. The base HTML template includes a small JS snippet:

```javascript
const evtSource = new EventSource('/events');
evtSource.addEventListener('reload', () => window.location.reload());
```

This gives a tight authoring loop: edit a page in the TUI, save, and the browser refreshes automatically.

### 7.6 Security

- The web server binds to `localhost` only. No remote access by default.
- A `--host` flag can open it to the network (with a warning).
- No authentication on localhost (the file is already passphrase-protected).
- SQL injection is prevented by parameterized queries. Ad-hoc query endpoint is disabled by default, enabled with `--allow-sql` flag.
- Form submissions validate field types and reject unknown fields.

---

## 8. App Mode — The Locked-Down End-User Experience

> **Reconciliation note:** This section originally described a separate "Vibe Viewer"
> binary. It is now realized as **App Mode** — a runtime mode of the single Wails desktop
> app (`studio/`), gated at the application layer. The capabilities, boundaries, and
> bindings below describe that mode; the standalone-binary framing is historical.

### 8.1 Overview

App Mode is the consumer-facing face of the Wails desktop app. When a builder hands off a
`.vibe` file (or sets it to launch in App Mode), the user opens it, enters the passphrase,
and is presented with the application inside — switchboard navigation, forms, dashboards,
reports — in a polished, native-feeling GUI. No terminal. No SQL. No awareness of the
underlying file format.

The mental model is: someone hands you a `.vibe` file the way someone hands you a PDF. You open it and use it.

### 8.2 User Experience Flow

```
Double-click invoice-tracker.vibe
           │
           ▼
┌──────────────────────────────┐
│    Enter Passphrase          │
│    ┌──────────────────────┐  │
│    │ ••••••••             │  │
│    └──────────────────────┘  │
│              [Open]          │
└──────────────────────────────┘
           │
           ▼
┌──────────────────────────────────────────────────────────┐
│  Invoice Tracker                              ─ □ ✕     │
├──────────┬───────────────────────────────────────────────┤
│          │                                               │
│ Overview │  Monthly Revenue                              │
│ ──────── │  ┌─────────────────────────────────────────┐  │
│ Dashboard│  │         ╱\    ╱\                        │  │
│ Invoices │  │   ╱\  ╱   \╱   \     ╱\               │  │
│ New Entry│  │ ╱   \/            \╱   \_              │  │
│          │  └─────────────────────────────────────────┘  │
│          │                                               │
│          │  ┌─────────┐  ┌──────────┐  ┌─────────────┐  │
│          │  │ $42,800 │  │ 156      │  │ 94%         │  │
│          │  │ Revenue │  │ Invoices │  │ Paid Rate   │  │
│          │  └─────────┘  └──────────┘  └─────────────┘  │
│          │                                               │
│          │  Recent Invoices                              │
│          │  ┌──────┬──────────┬─────────┬────────────┐  │
│          │  │ ID   │ Client   │ Amount  │ Status     │  │
│          │  ├──────┼──────────┼─────────┼────────────┤  │
│          │  │ 1047 │ Acme Co  │ $3,200  │ ● Paid     │  │
│          │  │ 1048 │ Initech  │ $1,800  │ ○ Pending  │  │
│          │  └──────┴──────────┴─────────┴────────────┘  │
├──────────┴───────────────────────────────────────────────┤
│  Invoice Tracker v1.2  •  Last updated: 2026-03-03      │
└──────────────────────────────────────────────────────────┘
```

### 8.3 Capabilities and Boundaries

**What App Mode CAN do:**

- Open `.vibe` files with passphrase authentication.
- Navigate between pages via the switchboard / sidebar.
- View rendered dashboards with full chart interactivity (hover, zoom, filter).
- Submit forms (data entry, updates).
- Browse tables with sorting, filtering, and pagination.
- Export data: individual tables as CSV/JSON, charts as PNG, entire pages as PDF (print-to-PDF via webview).
- Follow page links and pass parameters.
- View detail pages for individual records.
- Dark/light theme toggle.

**What App Mode CANNOT do:**

- Edit page definitions (no access to the markdown source).
- Run arbitrary SQL.
- Create, rename, or delete pages.
- Modify the database schema (add/drop tables or columns).
- Manage data sources or connections.
- Invoke destructive agent tools (the agent sandbox is forced to read-only / read-write).
- Change encryption settings or passphrase (Studio Mode only).
- Access `_vibe_` internal tables.

This boundary is enforced at the application layer, not the file format. App Mode opens the same SQLCipher database with the same key — it simply gates the privileged `studio/app.go` methods and switches the UI to the switchboard shell. Switching back to Studio Mode (Access's Shift-to-bypass equivalent) restores full capability.

### 8.4 Wails Architecture

App Mode is not a separate project — it is the App-Mode view set of the single Wails app:

```
studio/
├── main.go              # Wails app bootstrap, file association
├── app.go               # Go backend: container, engine, page execution,
│                        #   CRUD, agent/MCP — gated per mode
└── frontend/            # Vue 3 SPA (shared by Studio Mode + App Mode)
    └── src/
        ├── App.vue              # Root; routes by mode
        ├── views/
        │   ├── UnlockView.vue    # Passphrase entry
        │   ├── AppShellView.vue  # App Mode switchboard shell
        │   ├── PageEditorView.vue, DashboardView.vue, …
        │   └── TableBrowserView.vue
        ├── components/           # Chart, DataTable, Form, Metric, Detail, …
        └── router/               # mode-aware routing + capability gating
```

### 8.5 Go ↔ Frontend Bindings

Wails exposes Go methods (`studio/app.go`) to the Vue frontend. In App Mode the frontend
calls a read/interact subset of those methods (others are gated):

```go
// app.go — methods bound to frontend

// OpenFile prompts for a .vibe file and passphrase, returns metadata.
func (a *App) OpenFile(path, passphrase string) (*FileMeta, error)

// ListPages returns all pages with id, title, type, sort_order.
func (a *App) ListPages() ([]PageSummary, error)

// RenderPage executes a page's queries and returns rendered HTML.
func (a *App) RenderPage(pageID string, params map[string]string) (string, error)

// SubmitForm validates and inserts/updates a record.
func (a *App) SubmitForm(pageID, formID string, data map[string]interface{}) error

// ListTables returns user table names with row counts.
func (a *App) ListTables() ([]TableSummary, error)

// QueryTable returns paginated, sorted, filtered table data.
func (a *App) QueryTable(table string, opts TableQueryOpts) (*TableResult, error)

// ExportTable returns table data as CSV or JSON string.
func (a *App) ExportTable(table, format string) (string, error)

// GetAsset returns a binary asset by ID (for images in pages).
func (a *App) GetAsset(assetID string) ([]byte, error)

// CloseFile cleanly shuts down the container.
func (a *App) CloseFile() error
```

### 8.6 File Association

The app registers as the default handler for `.vibe` files on install:

- **macOS:** `Info.plist` declares `CFBundleDocumentTypes` for `.vibe`.
- **Windows:** Registry entries associate `.vibe` extension with the executable.
- **Linux:** `.desktop` file with `MimeType=application/x-vibe`.

Double-clicking a `.vibe` file opens the app with the file path as an argument; a file's
`_vibe_meta.default_mode` (App vs. Studio) decides which mode it opens in.

### 8.7 Offline-First

The app has zero network dependencies. All rendering happens locally. Chart.js and the CSS framework are bundled into the binary. The only network activity occurs if DuckDB remote connectors are configured in the `.vibe` file (e.g., `httpfs` for remote parquet files), and even then, the user is warned before any outbound connection.

---

## 9. AI Agent Interface

### 9.1 Design Philosophy

The agent interface follows a "bring your own AI" model. Vibe Studio hosts a tool server that any LLM-based agent can connect to. The protocol is deliberately simple — JSON over stdio or a local socket — so it works with Claude, GPT, local models, or custom agent frameworks.

Agents interact with the `.vibe` file through a constrained tool surface. They can introspect schema, run queries, manipulate data, and create/edit pages. Permissions are configurable per-file.

### 9.2 Transport

Two transport modes, selectable at Studio startup:

**stdio (default):** Agent communicates via stdin/stdout. Studio spawns the agent process (or is spawned by it). This is the simplest integration path and works with Claude Code, custom scripts, and any tool-use agent framework.

```bash
# Studio spawns agent
vibe studio --agent-cmd "python my_agent.py" project.vibe

# Agent spawns Studio (as a tool server)
vibe studio --agent-mode stdio project.vibe
```

**Local socket:** Studio listens on a Unix socket or TCP port. Multiple agents can connect. Each connection gets its own sandbox context.

```bash
vibe studio --agent-listen /tmp/vibe.sock project.vibe
vibe studio --agent-listen localhost:9090 project.vibe
```

### 9.3 Protocol

Request/response JSON, one object per line (JSON-RPC 2.0 compatible):

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "run_query",
  "params": {
    "sql": "SELECT * FROM orders LIMIT 10",
    "engine": "duckdb"
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "columns": ["id", "customer_id", "amount", "order_date"],
    "rows": [...],
    "row_count": 10,
    "truncated": false
  }
}

// Error
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32600,
    "message": "Permission denied: write operations not allowed in read-only mode"
  }
}
```

### 9.4 Tool Surface

#### Schema & Introspection

| Method | Description | Params |
|--------|-------------|--------|
| `introspect_schema` | All tables, columns, types, primary keys | _(none)_ |
| `describe_table` | Detailed info for one table | `table: string` |
| `list_pages` | All page definitions with metadata | — |
| `get_page` | Full page content (markdown source) | `page_id: string` |
| `list_queries` | Named query library | — |

#### Data Operations

| Method | Description | Params |
|--------|-------------|--------|
| `run_query` | Execute read-only SQL | `sql, engine?, max_rows?` |
| `create_record` | INSERT into a table | `table, data: {}` |
| `update_record` | UPDATE by primary key | `table, id, data: {}` |
| `delete_record` | DELETE by primary key | `table, id` |
| `bulk_insert` | INSERT multiple rows | `table, rows: [{}]` |

#### Page Operations (full mode only)

| Method | Description | Params |
|--------|-------------|--------|
| `create_page` | Add a new page | `id, title, content, page_type?` |
| `update_page` | Modify page content | `page_id, content?, title?` |
| `delete_page` | Remove a page | `page_id` |

#### Schema Operations (full mode only)

| Method | Description | Params |
|--------|-------------|--------|
| `create_table` | DDL: create a new table | `table, columns: [{name, type, ...}]` |
| `add_column` | DDL: add column to table | `table, column: {name, type, ...}` |
| `drop_table` | DDL: drop a table | `table` |
| `import_csv` | Load CSV data into a table | `table, csv_data, create_table?` |

### 9.5 Sandbox Modes

Configured per-file in `_vibe_agent_config`:

| Mode | Introspect | Read | Write | Pages | Schema |
|------|-----------|------|-------|-------|--------|
| `read-only` | ✓ | ✓ | ✗ | ✗ | ✗ |
| `read-write` | ✓ | ✓ | ✓ | ✗ | ✗ |
| `full` | ✓ | ✓ | ✓ | ✓ | ✓ |

Additional constraints:

- `allowed_tables`: whitelist of tables the agent can access (default: all user tables).
- `max_query_rows`: maximum rows returned per query (default: 10,000).
- `require_approval`: if true, write operations display in the TUI agent panel and require the user to approve before execution.
- `blocked_operations`: explicit deny list (e.g., `["drop_table", "delete_record"]`).

### 9.6 Agent Panel (TUI)

When an agent is connected, the Studio TUI shows an agent panel (toggled with `a`):

```
┌─ Agent ──────────────────────────────────────────┐
│ ● Connected: claude-agent (pid 48291)            │
│                                                  │
│ 14:32:01 → introspect_schema                     │
│ 14:32:01 ← 4 tables, 23 columns                 │
│ 14:32:03 → run_query: SELECT count(*) FROM...    │
│ 14:32:03 ← 1,847 rows                           │
│ 14:32:05 → create_page: "sales-summary"          │
│ 14:32:05 ⚠ APPROVE? [y/n]                       │
│                                                  │
│ Total calls: 3  |  Approved: 2  |  Pending: 1   │
└──────────────────────────────────────────────────┘
```

### 9.7 MCP Compatibility (Future)

The tool surface is designed to be forward-compatible with MCP (Model Context Protocol). The JSON-RPC transport and tool/method naming align with MCP conventions. A thin adapter layer can expose the Vibe agent interface as an MCP server, allowing any MCP-compatible client to connect without modification.

---

## 10. DuckDB Integration Details

### 10.1 Role Separation

SQLCipher and DuckDB serve **non-overlapping** roles. SQLCipher is the encrypted
*container*; DuckDB is the *single query/data engine*. There is no engine routing — all
queries and CRUD run on DuckDB.

```
SQLite (via SQLCipher)              DuckDB
──────────────────────              ──────
Encrypted container only            The single query + data engine
_vibe_* config tables               ALL user data tables
Page/form/report definitions        Views, materialized queries, CRUD
Agent config, secrets, assets       Remote data connectors
The DuckDB blob (at rest)           Columnar storage, fast aggregation
Always on disk (encrypted)          Materialized at runtime from blob
```

SQLCipher is **not queried for user data** — it stores the `_vibe_*` metadata and the
serialized DuckDB blob. The original per-block `engine` flag (and SQLite-native user
tables) were removed when the design collapsed to a single engine.

### 10.2 Lifecycle Management

```
Container.Open()
    │
    ├── Read _vibe_duckdb.data (BLOB, ~N MB)
    ├── Write to temp file: /tmp/vibe-<uuid>/data.duckdb
    ├── Open DuckDB connection to temp file
    └── Return engine ready

    ... session ...

Container.Save()
    │
    ├── CHECKPOINT (force DuckDB WAL flush)
    ├── Read temp file into memory ([]byte)
    ├── BEGIN IMMEDIATE on the SQLCipher container
    ├── UPDATE _vibe_duckdb SET data = ?
    ├── COMMIT
    └── Return

Container.Close()
    │
    ├── Close DuckDB connection
    ├── Securely delete temp file
    └── Remove temp directory
```

### 10.3 Cross-Engine Queries — dropped

This subsection originally described joining DuckDB against SQLite-native tables via the
DuckDB `sqlite` scanner. With the move to a **single DuckDB engine**, there are no
SQLite-native user tables and nothing to cross-join — all data already lives in DuckDB.
The mechanism is retained only conceptually for the rare case of reading an *external*
SQLite file via `ATTACH ... (TYPE SQLITE)`, which is just another remote connector (§10.4).

### 10.4 Remote Connectors

DuckDB's extension ecosystem provides remote data access without building custom connectors:

```sql
-- Read a remote Parquet file
INSTALL httpfs; LOAD httpfs;
SELECT * FROM read_parquet('https://example.com/data/sales.parquet');

-- Query a remote PostgreSQL database
INSTALL postgres; LOAD postgres;
ATTACH 'postgresql://user:pass@host/db' AS remote (TYPE POSTGRES);
SELECT * FROM remote.public.orders;

-- Read from S3
SET s3_region='us-east-1';
SET s3_access_key_id='...';
SET s3_secret_access_key='...';
SELECT * FROM read_parquet('s3://bucket/data/*.parquet');
```

Connection strings and credentials for remote sources are stored in `_vibe_secrets` (encrypted within the SQLCipher container). The `_vibe_sources` table maps friendly names to connection configurations.

Remote connector support is a natural extension point. For MVP, focus on local data. Add remote connectors when the core is stable.

### 10.5 Performance Considerations

- **Blob size:** DuckDB databases can grow large with heavy data. For MVP, set a soft warning at 100MB and a hard limit at 1GB for the blob. Beyond that, the user should use remote connectors.
- **Materialization time:** Opening a `.vibe` file with a 100MB DuckDB blob takes a few seconds to write the temp file. Show a progress indicator in both Studio Mode and App Mode.
- **Memory:** DuckDB is memory-efficient for analytical queries but can spike during large aggregations. Consider setting `SET memory_limit='512MB'` as a default, configurable in `_vibe_meta`.
- **Concurrent access:** Single-user design means no concurrent write concerns (multi-user mesh is v1.1). DuckDB and the SQLCipher container both have single-writer models that align with this.

---

## 11. CLI Interface

### 11.1 Vibe Studio CLI

```bash
vibe studio [flags] <file.vibe>
```

Opens the TUI for the given `.vibe` file. If the file doesn't exist, creates a new one.

**Flags:**

| Flag | Description |
|------|-------------|
| `--new` | Create a new `.vibe` file (prompts for passphrase) |
| `--web` | Auto-start the preview web server on open |
| `--port <n>` | Web server port (default: 3456) |
| `--host <addr>` | Web server bind address (default: localhost) |
| `--agent-cmd <cmd>` | Spawn an agent process and connect via stdio |
| `--agent-listen <addr>` | Listen for agent connections on socket/port |
| `--allow-sql` | Enable the ad-hoc SQL API endpoint on the web server |
| `--no-encrypt` | Create an unencrypted `.vibe` file (development only) |

### 11.2 Desktop App / App Mode launch

```bash
vibe studio [file.vibe]            # launch the Wails app (opens in the file's default mode)
vibe studio --app-mode [file.vibe] # force App Mode (locked-down end-user shell)
```

Launches the Wails desktop app. If a file is provided, opens it directly (prompts for
passphrase); the file's `_vibe_meta.default_mode` decides Studio vs. App Mode unless
`--app-mode` overrides it. With no file, shows a file picker.

### 11.3 Utility Commands

```bash
# File inspection (no passphrase needed for these)
vibe info <file.vibe>          # Print metadata: version, page count, table count, size
vibe verify <file.vibe>        # Verify file integrity, check format version

# File operations (passphrase required)
vibe export <file.vibe> --table <name> --format csv > data.csv
vibe export <file.vibe> --page <id> --format html > page.html
vibe import <file.vibe> --table <name> --csv data.csv
vibe rekey <file.vibe>         # Change passphrase
vibe compact <file.vibe>       # VACUUM the DuckDB blob + SQLCipher container, reduce file size
vibe dump <file.vibe>          # Dump schema + page definitions to stdout (for backup/debug)

# New file creation
vibe init my_project.vibe                        # Interactive setup
vibe init my_project.vibe --from template.vibe   # Clone from template
vibe init my_project.vibe --import data.csv      # Create with initial data
```

---

## 12. Implementation Roadmap

### Phase 0 — Foundation (Weeks 1–3)

**Goal:** Open, read, write, and close `.vibe` files. Prove the container design.

**Deliverables:**
- `pkg/container`: Open/Close/Save with SQLCipher encryption.
- SQLite schema creation and migration framework.
- DuckDB blob lifecycle (materialize, query, flush, cleanup).
- `vibe init` command: create a new `.vibe` file with sample data.
- `vibe info` command: print file metadata.
- Unit tests: round-trip create → write data → save → reopen → verify.

**Technical risks to retire:**
- SQLCipher Go bindings (go-sqlcipher) compatibility with DuckDB blob sizes.
- DuckDB Go driver (go-duckdb) stability for the serialize/deserialize pattern.
- Temp file cleanup reliability on crash.

### Phase 1 — Page Engine (Weeks 4–6)

**Goal:** Parse pages, execute SQL blocks, resolve directives into a renderable AST.

**Deliverables:**
- `internal/pages/parser`: Markdown + SQL blocks + directive parsing.
- `internal/pages/ast`: Node types for all directive kinds.
- `internal/pages/executor`: Execute SQL blocks top-to-bottom, bind named results to directives.
- `internal/engine`: Query interface executing against DuckDB (single engine).
- Parameter binding and sanitization.
- Test suite: parse → execute → verify result structure for each directive type.

**No rendering yet.** This phase validates the page format and execution model independent of any UI.

### Phase 2 — TUI Shell (Weeks 7–10)

**Goal:** A functional TUI that opens `.vibe` files and renders pages.

**Deliverables:**
- BubbleTea application scaffold with sidebar + content + REPL layout.
- `pkg/renderer/tui`: Render AST nodes as terminal widgets.
  - Text/markdown via glamour.
  - Tables via lipgloss (scrollable, sortable).
  - Metrics as styled text blocks.
  - Charts as ASCII bar charts and sparklines.
  - Forms as interactive input fields.
- Navigation: sidebar page list, page switching, parameter passing.
- SQL REPL: ad-hoc query execution with result display.
- Page editing via `$EDITOR` integration.
- New page wizard.
- Save/quit flow.

### Phase 3 — Web Preview Server (Weeks 11–13)

**Goal:** Studio serves pages as HTML in a browser.

**Deliverables:**
- `pkg/server`: Embedded HTTP server with routes.
- `pkg/renderer/html`: Render AST nodes as HTML.
  - Tables with client-side sorting.
  - Charts via Chart.js.
  - Forms with POST submission.
  - Metrics as styled cards.
- SSE live reload on page save.
- Datasette-style table browser at `/table/:name`.
- CSS: clean, minimal, Evidence.dev-inspired design. Embedded, no CDN.
- Export endpoints: CSV, JSON.

### Phase 4 — Agent Interface (Weeks 14–16)

**Goal:** AI agents can connect and interact with `.vibe` files.

**Deliverables:**
- `pkg/agents`: Tool definitions, sandbox enforcement.
- stdio transport: spawn agent or be spawned.
- JSON-RPC protocol implementation.
- Sandbox modes: read-only, read-write, full.
- Approval workflow for write operations in TUI.
- Agent panel in TUI.
- Test with: a Python script agent, Claude Code, a custom GPT wrapper.

### Phase 5 — Desktop App & App Mode (Weeks 17–22)

**Goal:** Non-technical users can open and use `.vibe` files in the desktop GUI's locked-down App Mode.

**Deliverables:**
- Wails application scaffold.
- Passphrase entry screen.
- Page renderer (Vue 3 frontend consuming Go backend via Wails bindings).
- Sidebar / switchboard navigation.
- Chart interactivity (hover, zoom via Chart.js).
- Form submission.
- Table browser with sort/filter.
- Data export (CSV, JSON, print-to-PDF).
- File association registration (macOS, Windows, Linux).
- Dark/light theme.
- Application icon and branding.
- Installer/packager for each platform.

### Phase 6 — Hardening & Polish (Weeks 23–26)

**Goal:** Production readiness.

**Deliverables:**
- Encrypted DuckDB temp file VFS (upgrade from plaintext temp).
- Performance profiling: large datasets (100MB+ blobs), many pages.
- Error handling audit: corrupt files, invalid pages, malformed SQL.
- Accessibility: keyboard navigation in App Mode, screen reader support.
- Documentation: user guide, page format reference, agent API reference.
- Sample `.vibe` files: project tracker, expense report, survey tool, dataset explorer.
- Release pipeline: signed binaries for macOS, Windows, Linux.

---

## 13. Open Questions & Future Considerations

### 13.1 Resolved for MVP

- **Single vs. multi-user:** Single-user for v1.0. No concurrent access, no conflict resolution (multi-user Tailscale mesh is v1.1).
- **Editor vs. consumer separation:** **One app, two modes.** Studio Mode edits; App Mode consumes (locked-down, gated at the app layer). A terminal-native TUI also serves builders. *(Superseded the original "two separate binaries" decision.)*
- **Query engine:** **Single-engine DuckDB** for both analytics and CRUD. *(Superseded the original dual SQLite+DuckDB routing.)*
- **DuckDB temp file encryption:** Plaintext temp for MVP. Encrypted VFS in Phase 6.
- **UI framework:** Wails v2 with a **Vue 3** frontend. *(Superseded the original React choice.)*
- **Agent protocol:** JSON-RPC over stdio/socket, plus an MCP server (SSE).

### 13.2 Open for Future Versions

- **Collaboration:** Could `.vibe` files support a "publish" mode where Studio pushes pages to a shared server? This would move toward a lightweight BI tool.
- **Versioning/history:** Track page edit history within the file. Git-like diffing of page content.
- **Custom themes:** Allow `.vibe` files to include CSS themes that App Mode applies. Brand a data app.
- **Plugin system:** Let pages include custom JavaScript components (sandboxed). Extends beyond the built-in chart/form/table primitives.
- **Mobile App Mode:** A lightweight mobile build (iOS/Android) of App Mode using a webview-based approach.
- **Template marketplace:** Share `.vibe` templates (empty data, pre-built pages) for common use cases: CRM, inventory, project tracker.
- **Remote connectors in App Mode:** Currently only Studio Mode manages connections. App Mode could allow configured remote sources to refresh data (with appropriate security controls).
- **Multi-file references:** A `.vibe` file that imports data from another `.vibe` file. Composable data apps.

---

## Appendix A: Sample `.vibe` Page Templates

### A.1 Minimal Dashboard

```markdown
---
title: Overview
type: dashboard
---

'''sql total_records
SELECT count(*) as n FROM main_table
'''

'''sql recent_activity
SELECT * FROM main_table ORDER BY created_at DESC LIMIT 10
'''

{metric: total_records, label: "Total Records", format: number}

{table: recent_activity, page_size: 10, sortable: true}
```

### A.2 Data Entry Form

```markdown
---
title: Add Record
type: form
---

# New Entry

{form: main_table, fields: [
    {name: title, label: "Title", type: text, required: true},
    {name: category, label: "Category", type: select,
     source: "SELECT DISTINCT category FROM main_table ORDER BY 1"},
    {name: amount, label: "Amount", type: number},
    {name: notes, label: "Notes", type: textarea},
    {name: date, label: "Date", type: date, default: today}
], on_submit: redirect:overview}
```

### A.3 Analytical Notebook

```markdown
---
title: Q1 Analysis
type: notebook
---

# Q1 2026 Performance Analysis

This notebook explores first-quarter trends in the sales data.

'''sql monthly_summary
SELECT strftime(sale_date, '%Y-%m') AS month,
       COUNT(*) AS transactions,
       SUM(amount) AS revenue,
       AVG(amount) AS avg_deal
FROM sales
WHERE sale_date >= '2026-01-01' AND sale_date < '2026-04-01'
GROUP BY 1 ORDER BY 1
'''

{chart: monthly_summary, type: line, x: month, y: revenue, title: "Revenue Trend"}

{table: monthly_summary}

'''sql top_products
SELECT product_name, SUM(amount) AS total, COUNT(*) AS deals
FROM sales
WHERE sale_date >= '2026-01-01' AND sale_date < '2026-04-01'
GROUP BY 1 ORDER BY 2 DESC LIMIT 10
'''

The top-performing products this quarter:

{chart: top_products, type: bar, x: product_name, y: total, title: "Top 10 Products"}
```

### A.4 Detail Page with Parameters

```markdown
---
title: Customer Detail
type: detail
params:
  - name: id
    type: integer
    required: true
---

'''sql customer
SELECT * FROM customers WHERE id = $id
'''

{detail: customer, layout: card}

'''sql customer_orders
SELECT id, amount, status, order_date
FROM orders
WHERE customer_id = $id
ORDER BY order_date DESC
'''

## Order History

{table: customer_orders, page_size: 20}

{metric: "SELECT SUM(amount) FROM orders WHERE customer_id = $id", label: "Lifetime Value", format: currency}
```

---

## Appendix B: Dependency Matrix

| Component | Library | Language | Purpose |
|-----------|---------|----------|---------|
| SQLite + encryption | go-sqlcipher | Go (CGO) | Encrypted container envelope (not a query engine) |
| DuckDB | go-duckdb | Go (CGO) | Single query + data engine (analytics + CRUD) |
| TUI framework | BubbleTea | Go | Terminal UI |
| TUI styling | Lipgloss | Go | Component styling |
| TUI markdown | Glamour | Go | Markdown rendering |
| Web framework | net/http (stdlib) | Go | Preview server |
| HTML templates | html/template (stdlib) | Go | Page rendering |
| Desktop GUI | Wails v2 | Go + JS | Studio app (Studio Mode + App Mode) |
| Frontend UI | Vue 3 | JavaScript | App frontend |
| MCP server | mark3labs/mcp-go | Go | Agent tool surface over SSE |
| Charts (web) | Chart.js | JavaScript | Chart rendering |
| Key derivation | SQLCipher (internal) | C | PBKDF2-HMAC-SHA512 |
| Temp file crypto | golang.org/x/crypto | Go | ChaCha20-Poly1305 (v2) |

