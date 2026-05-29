<div align="center">

# Vibe

**The local-first successor to Microsoft Access 97.**

A portable, encrypted, single-file application platform for small businesses —
tables, forms, reports, and an AI agent, all inside one `.vibe` file. No cloud
account, no per-seat fees, no vendor lock-in. The data lives in the building.

*by [sandwichlabs](https://github.com/SandwichLabs)*

</div>

---

## What is Vibe?

Access 97 succeeded because it collapsed the distance between a business problem
and a working solution: an office manager could build a schema, a form, and a
report in an afternoon, and it was theirs. The cloud era traded that sovereignty
for convenience. **Vibe reclaims it with modern primitives.**

A `.vibe` file is an encrypted container that bundles your data, interactive
pages, forms, reports, navigation, and an AI agent into a single portable file —
like the PDF ecosystem, but for data applications.

Vibe ships as **one desktop app with two modes**, mirroring the Access dual-mode
design:

- **Studio Mode** — for builders. Schema designer, page/form/report editors,
  query explorer, and an AI agent that acts as the "wizard" layer.
- **App Mode** — for data-entry workers. A locked-down, generated interface:
  switchboard navigation, forms, and reports only — no schema, SQL, or
  destructive tools.

A terminal-native **BubbleTea TUI** is also available for technical workflows.

## Who it's for

| Persona | What they do | What they get |
|---|---|---|
| **Data Entry Worker** | Front-line input & retrieval | Locked App Mode: curated forms, large targets, validation, switchboard nav |
| **Citizen Developer** | Office manager / power user building a tool | Conversational schema + AI-generated forms; progressive disclosure |
| **Studio Developer** | IT generalist / consultant | Full schema, SQL, agent config, export pipeline, deployment |

## Architecture

- **Container / encryption:** SQLCipher (AES-256, PBKDF2-HMAC-SHA512) — the `.vibe` envelope.
- **Data & query engine:** embedded **DuckDB** (single engine for analytics *and* CRUD), stored as a serialized blob inside the encrypted container.
- **Desktop app:** Wails v2 (Go backend + Vue 3 frontend + Chart.js).
- **TUI:** BubbleTea + Lipgloss + Glamour.
- **AI agent:** JSON-RPC 2.0 + MCP server; sandboxed (`read-only` / `read-write` / `full`) with an approval queue.
- **Language:** Go 1.24+ (`CGO_ENABLED=1` for the SQLCipher & DuckDB bindings).

```
.vibe file = SQLCipher (encrypted container) ─┬─ _vibe_* tables (pages, queries, forms, …)
                                              └─ _vibe_duckdb blob (your data + analytics)
```

## Roadmap

The current focus is **Microsoft Access *application* parity** on the local-first
Go desktop stack:

1. **Forms & CRUD write-back** — bound forms, the four archetypes (columnar / continuous / datasheet / master-detail subform), inline datasheet editing.
2. **Relational schema & visual querying** — relationships, referential integrity, field properties, Filter-by-Form / Filter-by-Selection.
3. **Reports & dashboards** — banded reports (grouping, subtotals, print/PDF) + live filterable dashboards.
4. **App Shell & Switchboard** — hierarchical navigation and the dual-mode lockdown.
5. **Automation & macros** — command-button actions, AutoExec, events; AI-generated automation.
6. **Office bridge & templates** — Excel import/export, starter apps → **v1.0**.

**v1.1** adds multi-user via a **Tailscale** mesh (delta sync, optimistic
concurrency, identity-aware permissions).

> Full detail: [`docs/ms-access-parity-plan.md`](docs/ms-access-parity-plan.md).

## Build

Build automation uses **Taskfile.yml**:

```bash
task              # Default: lint + test + build
task build        # Build binary
task test         # Run all tests (alias: task t)
task lint         # Run golangci-lint
```

Run a single package's tests:

```bash
go test -v ./internal/container/
```

## Documentation

| Doc | Purpose |
|---|---|
| [`CLAUDE.md`](CLAUDE.md) | Architecture, conventions, key concepts (also guidance for AI assistants) |
| [`PROJECT.md`](PROJECT.md) | Original MVP specification (file format, runtime, agent protocol) |
| [`PLAN.md`](PLAN.md) | Original build phases (0–6) |
| [`docs/ms-access-parity-plan.md`](docs/ms-access-parity-plan.md) | **Current** phased roadmap to Access parity |
| [`docs/ms-access-ux-research.txt`](docs/ms-access-ux-research.txt) | UX research grounding the parity target |
| [`docs/EVIDENCE_PARITY_PLAN.md`](docs/EVIDENCE_PARITY_PLAN.md) | Dashboard/reporting component plan (folded into roadmap Phase 3) |

## Design principles

- **File is the app** — fully self-contained, no external dependencies.
- **Local-first & data sovereignty** — your data never leaves your machine.
- **Encryption by default** — passphrase required; no plaintext on disk.
- **Single-user first** — multi-user mesh is a deliberate v1.1.
- **AI as collaborator, not dependency** — everything works without AI.
- **Progressive disclosure** — complexity available, never imposed.
