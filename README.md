<div align="center">

# Vibe

**An agentic workspace for the data you already have.**

Connect to local DuckDB files, MotherDuck, Postgres, SQLite and object storage;
explore, join and edit across all of them; write live notebooks and skills that
live in a folder you can share through git — with an AI agent that reads freely
and asks before it writes. Credentials stay in your OS keychain.

*by [sandwichlabs](https://github.com/SandwichLabs)*

</div>

---

> **In transition (2026-10-06).** Vibe is moving from a single encrypted `.vibe`
> file that *holds* your data to a workspace that *points at* your data. The
> command line and `vibe mcp serve` already work on workspaces; the Studio
> desktop app still opens `.vibe` containers until #111. Tracked in
> epic [#105](https://github.com/SandwichLabs/vibe-app/issues/105). Why and how:
> [`docs/direction.md`](docs/direction.md),
> [`docs/plans/workspaces-and-connections.md`](docs/plans/workspaces-and-connections.md).

## What is Vibe?

A desktop data tool in the DataGrip / DBeaver mould, built around an agent.

- **Workspaces** — a folder you open like an Obsidian vault, switchable at any
  time. Notebooks, saved queries, skills and connection definitions are plain
  files, so a workspace can be a git repo your team shares.
- **Connections** — data stays where it lives. Every source is attached into
  one embedded DuckDB, so you can join MotherDuck to Postgres to a Parquet file
  in one query, using DuckDB's extension suite. Read-only by default.
- **Credentials** — in the OS keychain (or env vars for CI/headless), never in
  the workspace, never in the agent's reach.
- **Notebooks** — cells of SQL, markdown, and live dashboards. Outputs are never
  saved; every cell re-runs when you open the notebook, so the numbers are
  always current.
- **Skills** — markdown documents that tell the agent what your metrics mean,
  which joins are canonical, and what to avoid. Loaded on demand.
- **An agent** — sandboxed, reads freely, and asks before any write. The same
  tools are exposed over **MCP**, so Claude Code, Claude Desktop, Cursor, or
  your own agent can work in the workspace with Studio open or closed.
- **`.vibe` files** — a portable bundle of notebooks, queries and skills
  (optionally encrypted) for sharing outside git. Older data-holding `.vibe`
  files import into a workspace.

Everything works without AI. SQL and notebooks are first-class by hand.

## Command line

```bash
vibe workspace init acme --template order-entry --git   # a folder with vibe.yaml, notebooks/, data/ ...
cd acme
vibe workspace info                                     # connections, counts, agent policy, who owns the hub
vibe import customers.csv --into customers              # CSV / TSV / xlsx into a writable connection
vibe query "SELECT count(*) FROM customers"             # [-c conn] [--format table|csv|json] [--limit N]
vibe export customers -o customers.parquet              # csv, parquet, json or xlsx; or --sql "..."
vibe studio                                             # terminal UI: SQL REPL, table browser, notebook list
vibe studio --gui                                       # the desktop app (opens workspace folders from #111)
vibe mcp serve                                          # let Claude and other MCP clients use the workspace
```

Commands find the workspace the way git finds `.git` (or take `--workspace DIR`).
One process owns a workspace's database at a time; if Studio or `vibe mcp serve`
has it, a command says so. `--read-only` (on `query`, `export` and `studio`) reads without
taking ownership, but the owner keeps the workspace's own `.duckdb` files locked, so it only
reaches other connections such as MotherDuck or Postgres.
Details in [`docs/reference/workspaces.md`](docs/reference/workspaces.md#command-line).

## Who it's for

| Persona | What they do | What they get |
|---|---|---|
| **Analyst / operator** | Asks questions of business data | Notebooks, live dashboards, an agent that grounds every number in a query |
| **Data engineer / builder** | Curates sources and definitions for a team | Shared connection definitions in git, skills that encode the domain, cross-source joins |
| **Agent developer** | Points an external agent at real data | `vibe mcp serve` in a workspace — one tool surface, read-mostly, approvals for writes |

## Architecture

- **Engine:** embedded **DuckDB** as a hub — local files, MotherDuck, Postgres, SQLite and S3/GCS/R2 attached as catalogs.
- **Workspace:** a folder (`vibe.yaml`, `notebooks/`, `queries/`, `skills/`, `data/`); credentials in the OS keychain.
- **Desktop app:** Wails v2 (Go backend + Vue 3 frontend, SchoolHouse design system).
- **AI agent:** `charmbracelet/fantasy` runner over one tool registry; allow-list SQL policy, approval gate for writes; MCP server (SSE in-app, stdio headless).
- **Artifacts:** A2UI pages — a validated, live-SQL-bound component spec the agent draws with.
- **Language:** Go 1.24+ (`CGO_ENABLED=1` for the DuckDB bindings).

```
workspace/ (git) ── vibe.yaml · notebooks/ · queries/ · skills/ · data/
       │
Studio / vibe mcp serve ── DuckDB hub ─┬─ local   (data/local.duckdb)
                                       ├─ prod_pg (Postgres, read-only)
                                       ├─ md      (MotherDuck)
                                       └─ lake    (S3 secret)
                     credentials ◄── OS keychain / env
```

## Roadmap

Epic [#105](https://github.com/SandwichLabs/vibe-app/issues/105) — workspaces first, then connections:

1. **Design pass** (#106) and **DuckDB spike** (#107).
2. **Workspace = folder** — session hub (#108), workspace + file stores (#109), launcher/switcher (#111), MCP on workspaces (#112), CLI (#113).
3. **Connections & credentials** — keychain (#114), connection types (#115), agent SQL policy (#116), connections UI (#117).
4. **Exchange & breadth** — `.vibe` bundles (#118), more types (#119), packaging (#77).

Multi-user / Tailscale mesh remains deferred.

> Full detail: [`docs/direction.md`](docs/direction.md).

## Build

Build automation uses **Taskfile.yml**:

```bash
task              # Default: lint + test + build
task build        # Build binary
task test         # Run all tests (alias: task t)
task lint         # Run golangci-lint
task studio:dev   # Wails dev server for the Studio app
```

Run a single package's tests:

```bash
go test -v ./internal/workspace/
```

## Documentation

| Doc | Purpose |
|---|---|
| [`docs/direction.md`](docs/direction.md) | **Current** north star: workspaces, connections, primitives, sequencing |
| [`docs/plans/workspaces-and-connections.md`](docs/plans/workspaces-and-connections.md) | Implementation plan for epic #105 |
| [`CLAUDE.md`](CLAUDE.md) | Architecture, conventions, key concepts (also guidance for AI assistants) |
| [`motherduck-research.md`](motherduck-research.md) | The MotherDuck platform research that informed the direction |
| [`docs/llm-config.md`](docs/llm-config.md) | LLM provider configuration |
| [`docs/mcp.md`](docs/mcp.md) | `vibe mcp serve` (stdio/SSE), Claude Desktop config, SSE auth/security |
| [`docs/reference/`](docs/reference/) | Shipped surface reference (notebooks, A2UI, import, templates) |
| [`PROJECT.md`](PROJECT.md), [`PLAN.md`](PLAN.md) | Original MVP spec and build phases (file format, runtime) — historical |
| [`docs/ms-access-parity-plan.md`](docs/ms-access-parity-plan.md) | Superseded Access-parity roadmap — historical |

## Design principles

- **Data stays where it lives** — Vibe reaches your data; it doesn't copy it into its own format.
- **The workspace is the app** — a folder of plain files you can version and share.
- **Credentials in the keychain** — never in the folder, a bundle, logs, or the agent's context.
- **Read-only until asked** — connections default to read-only; agent writes need approval.
- **One tool surface** — the built-in agent and external MCP clients see the same tools with the same gating.
- **Live, not frozen** — notebooks store questions, not answers; outputs re-run on open.
- **AI as collaborator, not dependency** — everything works without AI.
- **Progressive disclosure** — skills advertise a line and load on demand; connections connect on first use.
