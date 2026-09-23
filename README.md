<div align="center">

# Vibe

**A local-first, encrypted, single-file agentic data workspace.**

DuckDB tables, notebooks, skills, and an AI agent in one portable `.vibe` file —
open it in the Studio app or point any MCP client at it. No cloud account, no
per-seat fees, no vendor lock-in. The data lives in the building.

*by [sandwichlabs](https://github.com/SandwichLabs)*

</div>

---

## What is Vibe?

MotherDuck showed what a data platform looks like when it is built for agents:
tables, notebooks, guides that tell the model what your data means, and MCP so
any agent can use all three. Vibe is the local, offline, file-shaped version of
that idea. A `.vibe` file is an encrypted container holding:

- **Tables** — an embedded DuckDB database. Import CSV and spreadsheets, edit
  rows in a datasheet, export to CSV, JSON, xlsx, or Parquet.
- **Notebooks** — cells of SQL, markdown, and live dashboards. Outputs are never
  saved; every cell re-runs when you open the notebook, so the numbers are
  always current.
- **Skills** — markdown documents that tell the agent what your metrics mean,
  which joins are canonical, and what to avoid. Loaded on demand, stored in the
  file.
- **An agent** — sandboxed, with an approval queue, that explores the data,
  writes notebook cells, and draws live charts. The same tools are exposed over
  **MCP**, so Claude Desktop, Cursor, or your own agent can work on the file
  with Studio closed.

Everything works without AI. SQL and notebooks are first-class by hand.

> **Direction change (2026-09-22).** Vibe was previously aimed at Microsoft
> Access 97 application parity (forms, switchboard, App Mode, macros). That
> layer was removed in favour of the primitives above. The reasoning and
> the plan are in [`docs/direction.md`](docs/direction.md).

## Who it's for

| Persona | What they do | What they get |
|---|---|---|
| **Analyst / operator** | Asks questions of business data | Notebooks, live dashboards, an agent that grounds every number in a query |
| **Builder** | Curates a data workspace for a team | Import wizard, skills that encode the domain, exports, encrypted portability |
| **Agent developer** | Points an external agent at a dataset | `vibe mcp serve file.vibe` — one tool surface, sandboxed, offline |

## Architecture

- **Container / encryption:** SQLCipher (AES-256, PBKDF2-HMAC-SHA512) — the `.vibe` envelope.
- **Data & query engine:** embedded **DuckDB** (single engine), stored as a serialized blob inside the encrypted container.
- **Desktop app:** Wails v2 (Go backend + Vue 3 frontend, SchoolHouse design system).
- **AI agent:** `charmbracelet/fantasy` runner over one tool registry; sandboxed (`read-only` / `read-write` / `full`) with an approval queue; MCP server (SSE in-app, stdio headless).
- **Artifacts:** A2UI pages — a validated, live-SQL-bound component spec the agent draws with.
- **Language:** Go 1.24+ (`CGO_ENABLED=1` for the SQLCipher & DuckDB bindings).

```
.vibe file = SQLCipher (encrypted container) ─┬─ _vibe_* tables (notebooks, skills, queries, …)
                                              └─ _vibe_duckdb blob (your data)
```

## Roadmap

The current arc is the **primitive migration**, tracked in GitHub issues:

1. **One tool surface** (#63) — the in-app agent and the MCP server share one registry.
2. **Skills** (#62) and **A2UI artifacts** (#67), in parallel.
3. **Notebooks** (#66) — the cell model plus agent task-mode authoring. Pages migrate here.
4. **Headless MCP** (#64), **guardrails** (#70).
5. **Remove the Access layer** (#68, shipped) and **retire the page system, cut the TUI to a REPL** (#69, shipped).

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
go test -v ./internal/container/
```

## Documentation

| Doc | Purpose |
|---|---|
| [`docs/direction.md`](docs/direction.md) | **Current** north star: primitives, what goes, sequencing |
| [`CLAUDE.md`](CLAUDE.md) | Architecture, conventions, key concepts (also guidance for AI assistants) |
| [`motherduck-research.md`](motherduck-research.md) | The MotherDuck platform research that informed the direction |
| [`docs/llm-config.md`](docs/llm-config.md) | LLM provider configuration |
| [`docs/mcp.md`](docs/mcp.md) | `vibe mcp serve` (stdio/SSE), Claude Desktop config, SSE auth/security |
| [`docs/reference/`](docs/reference/) | Shipped surface reference (notebooks, A2UI, import, templates) |
| [`PROJECT.md`](PROJECT.md), [`PLAN.md`](PLAN.md) | Original MVP spec and build phases (file format, runtime) — historical |
| [`docs/ms-access-parity-plan.md`](docs/ms-access-parity-plan.md) | Superseded Access-parity roadmap — historical |

## Design principles

- **File is the app** — fully self-contained, no external dependencies.
- **Local-first & data sovereignty** — your data never leaves your machine.
- **Encryption by default** — passphrase required; no plaintext on disk.
- **One tool surface** — the built-in agent and external MCP clients see the same tools with the same gating.
- **Live, not frozen** — notebooks store questions, not answers; outputs re-run on open.
- **AI as collaborator, not dependency** — everything works without AI.
- **Progressive disclosure** — skills advertise a line and load on demand.
