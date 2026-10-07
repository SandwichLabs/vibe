<div align="center">

# Vibe

**An agentic workspace for the data you already have.**

Connect to local DuckDB files, MotherDuck, Postgres, MySQL, SQLite, DuckLake, Iceberg and Delta tables and object storage (S3, GCS, R2, Azure);
explore, join and edit across all of them; write live notebooks and skills that
live in a folder you can share through git — with an AI agent that reads freely
and asks before it writes. Credentials stay in your OS keychain.

*by [sandwichlabs](https://github.com/SandwichLabs)*

</div>

---

> **Status.** The workspaces-and-connections direction (epic
> [#105](https://github.com/SandwichLabs/vibe-app/issues/105)) is built:
> Studio, the command line and `vibe mcp serve` all work on workspace
> **folders** with connections and keychain credentials. A `.vibe` file is a
> sharing bundle, not a database. What is left is packaging and signing
> ([#77](https://github.com/SandwichLabs/vibe-app/issues/77)) and the follow-ups
> in [`docs/direction.md`](docs/direction.md#9-as-delivered-what-changed-from-the-plan).
> Why and how: [`docs/direction.md`](docs/direction.md),
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
- **`.vibe` files** — a portable bundle of notebooks, queries and skills (and,
  if you choose, connection definitions) for sharing outside git: no data, no
  credentials, optionally locked with a passphrase. `vibe export bundle`,
  `vibe import <file.vibe>`, or **Import .vibe…** in Studio (opening a
  `.vibe` file with Vibe starts it). The older data-holding `.vibe` container
  is not supported.

Everything works without AI. SQL and notebooks are first-class by hand.

## Command line

```bash
vibe workspace init acme --template order-entry --git   # a folder with vibe.yaml, notebooks/, data/ ...
cd acme
vibe workspace info                                     # connections, counts, agent policy, who owns the hub
vibe import customers.csv --into customers              # CSV / TSV / xlsx into a writable connection
vibe conn add | list | test | connect | rm | move       # connections; credentials go to the keychain
vibe conn credentials set|clear|status <name>           # write-only: set or clear, never read back
vibe query "SELECT count(*) FROM customers"             # [-c conn] [--format table|csv|json] [--limit N]
vibe export customers -o customers.parquet              # csv, parquet, json or xlsx; or --sql "..."
vibe export bundle -o share.vibe [--encrypt]            # notebooks, queries, skills; no data, no credentials
vibe import share.vibe [--on-conflict rename|overwrite|skip]
vibe export notebook <id> -o nb.vnb                     # one notebook; vibe import notebook nb.vnb takes it back
vibe config llm-key                                     # the agent's API key, kept in the keychain
vibe studio                                             # terminal UI: SQL REPL, table browser, notebook list
vibe studio --gui                                       # the desktop app (add a folder to open it: vibe studio --gui acme,
                                                        #  or a .vibe file to start its import)
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

- **Engine:** embedded **DuckDB** as a hub — local files, MotherDuck, Postgres, MySQL, SQLite and DuckLake attached as catalogs, and S3/GCS/R2/Azure storage plus Iceberg and Delta tables read in place.
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

## Status and what is next

Epic [#105](https://github.com/SandwichLabs/vibe-app/issues/105) (#106–#119) is merged: the design pass and spike, the session hub, workspaces and file stores, the launcher and switcher, MCP and the CLI on workspaces, the keychain, connection types (Postgres, MySQL, MotherDuck, SQLite, S3/GCS/R2, Azure, DuckLake, Iceberg and Delta), the agent's SQL allow-list, the connections UI ([`docs/reference/connections.md`](docs/reference/connections.md#in-vibe-studio)) and `.vibe` bundles.

Open: macOS and Linux packages, signing and CI ([#77](https://github.com/SandwichLabs/vibe-app/issues/77), [`docs/packaging.md`](docs/packaging.md)); a safe agent read path for Iceberg and Delta (#134); notebook export to HTML/PDF/xlsx (#73). Multi-user / Tailscale mesh remains deferred.

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
| [`docs/direction.md`](docs/direction.md) | **Current** north star: workspaces, connections, primitives, and what changed from the plan |
| [`docs/plans/workspaces-and-connections.md`](docs/plans/workspaces-and-connections.md) | The implementation plan for epic #105 (implemented; "as built" summary at the top) |
| [`CLAUDE.md`](CLAUDE.md) | Architecture, conventions, key concepts (also guidance for AI assistants) |
| [`docs/reference/`](docs/reference/) | Shipped surface reference: [workspaces](docs/reference/workspaces.md), [connections](docs/reference/connections.md), [agent policy](docs/reference/agent-policy.md), [notebooks and A2UI](docs/reference/notebooks.md), [import and export](docs/reference/import-office.md), [templates](docs/reference/templates.md), [themes](docs/reference/themes.md) |
| [`docs/README.md`](docs/README.md) | Index of all documentation |
| [`docs/reference/mcp.md`](docs/reference/mcp.md) | `vibe mcp serve` (stdio/SSE), Claude Desktop config, SSE auth/security |
| [`docs/reference/llm-config.md`](docs/reference/llm-config.md) | LLM provider configuration and the API key |
| [`docs/packaging.md`](docs/packaging.md) | Packaging and release plan (#77), file association wiring, extensions |
| [`docs/archive/`](docs/archive/README.md) | Superseded plans and research (original MVP spec, Access parity, MotherDuck research) — historical |

## Design principles

- **Data stays where it lives** — Vibe reaches your data; it doesn't copy it into its own format.
- **The workspace is the app** — a folder of plain files you can version and share.
- **Credentials in the keychain** — never in the folder, a bundle, logs, or the agent's context.
- **Read-only until asked** — connections default to read-only; agent writes need approval.
- **One tool surface** — the built-in agent and external MCP clients see the same tools with the same gating.
- **Live, not frozen** — notebooks store questions, not answers; outputs re-run on open.
- **AI as collaborator, not dependency** — everything works without AI.
- **Progressive disclosure** — skills advertise a line and load on demand; connections connect on first use.
