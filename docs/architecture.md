# Architecture

How Assay fits together, at the level an outsider or new contributor needs. The contributor map of every package is the tree in [`CLAUDE.md`](../CLAUDE.md); the product reasoning is in [`direction.md`](direction.md).

## The idea in one picture

```
workspace folder (git, optional)            external agents
  assay.yaml  notebooks/  queries/             Claude Code, Claude Desktop,
  skills/    data/       .assay-local/         Cursor, ...  (MCP)
        |                                           |
        v                                           v
   Studio (desktop)  <-- same tool surface -->  assay mcp serve
   assay CLI / TUI                                   |
        \                                          /
         +---------- session: one DuckDB hub -----+
                          |
     +----------+---------+----------+-----------+
   local      MotherDuck  Postgres   S3 / R2 /   DuckLake,
   .duckdb    (md:)       MySQL      GCS / Azure Iceberg, Delta
                          SQLite     (httpfs)
        credentials <-- OS keychain or environment variables
```

Assay keeps data where it lives. Each connection is attached into one in-memory DuckDB per workspace (the **hub**); the connection name becomes the catalog name, so `prod_pg.public.orders` and `local.main.customers` can be joined in one query. Nothing is copied into an Assay-specific format.

## The workspace is a folder

A workspace is a directory with an `assay.yaml` at its root. Notebooks (`notebooks/*.asnb`), saved queries (`queries/*.sql`), skills (`skills/<name>/SKILL.md`) and local data (`data/`) are plain, diff-friendly files you can commit and review. Machine-local state lives in `.assay-local/` (git-ignored). Files are the source of truth: writes are compare-and-write, and a watcher picks up changes made by git, an editor or another process.

Trust moves out of the folder. A workspace arrives with whatever its authors committed, so a committed `assay.yaml` can only tighten the agent's limits; looser, per-person settings and sign-in methods live in your own config directory, keyed by workspace id. Reference: [workspaces](reference/workspaces.md).

## Connections and credentials

A connection is a typed definition (Postgres, MotherDuck, S3, ...) in one of three scopes: shared (committed), local (this computer) or user (all workspaces). `internal/connections` holds the type registry and builds the DuckDB `ATTACH` statements; `internal/session` owns the hub and connects on first use, tracking a status per connection. Credentials are kept by `internal/secrets` in the OS keychain or read from environment variables, are never written to a file, and are write-only across the Studio bridge and MCP. Reference: [connections](reference/connections.md).

## One owner per workspace

DuckDB allows one writing process per database file. So one process owns a workspace's hub at a time, recorded in `.assay-local/hub.lock`: Studio, or `assay mcp serve`. Other `assay mcp serve` processes proxy to the owner when it is serving MCP (Studio only does so after you press Start in Settings), so Claude Code and Studio work together and Studio's approval cards apply. `internal/hub` implements the lock, the proxy and take-over.

## One tool surface

Everything the agent can do is a `ToolDef` in a single registry (`internal/agents`): run a query, describe a table, create a record, author a notebook cell, read a skill, and so on. Studio's assistant (`internal/llm`, built on `charmbracelet/fantasy`) and external MCP clients (`internal/mcp`) consume the same registry and pass through the same gating:

- **Sandbox tiers:** `read-only`, `read-write`, `full`.
- **SQL allow-list:** the agent's SQL is parsed and checked against what the tier permits before DuckDB ever binds it. Your own SQL in the query editor is not restricted; notebook cells and live bindings re-run under the read-only policy.
- **Three-gate writes:** the tier allows it, the connection is writable and you approve it; approval is always required for anything that is not a local file.
- **Guardrails:** answers must be grounded in a query, empty results are explained, tool results are size-budgeted.

Reference: [agent policy](reference/agent-policy.md), [MCP](reference/mcp.md).

## Notebooks, skills and artifacts

- **Notebooks** are JSON documents of `sql`, `markdown` and `a2ui` cells. They store questions, not answers: every data cell re-runs when you open the notebook, and outputs are never saved.
- **Skills** are `SKILL.md` documents describing what your metrics mean. The agent sees a name and a line, and loads the body on demand.
- **A2UI artifacts** are small, validated component specs (stats, tables, charts) bound to live SQL. The Go validator rejects unknown fields and the frontend draws them with hand-written CSS and SVG, so model output never reaches a style attribute (`internal/a2ui`). Reference: [notebooks and A2UI](reference/notebooks.md).
- **Data artifacts** are parameterised, live views stored as files in `artifacts/<name>/` (`artifact.yaml` plus `view.a2ui.json`): typed parameters and named queries that are bound by value, never pasted into SQL. Every query is checked against the read-only SQL policy when it is saved and again on every run. Reference: [data artifacts](reference/artifacts.md).

## The programs

- **`assay`** (Cobra CLI in `cmd/`): workspaces, queries, import and export, connections, config, the terminal UI (BubbleTea) and `assay mcp serve`.
- **Studio** (`studio/`): a Wails v2 desktop app, with a Go backend that binds to the same packages as the CLI and a Vue 3 frontend. It starts, owns and serves the hub.
- **Bundles** (`internal/bundle`): a `.assay` file is a zip of notebooks, queries, skills and connection definitions, optionally age-encrypted. It never holds data or credentials.

## Where to read next

| If you want to... | Read |
|---|---|
| Understand why | [`direction.md`](direction.md) |
| See every package | [`CLAUDE.md`](../CLAUDE.md) |
| Look up the shipped surface | [`reference/`](reference/README.md) |
| Build and contribute | [`CONTRIBUTING.md`](../CONTRIBUTING.md) |
| Check what leaves the machine | [Privacy](privacy.md) |
