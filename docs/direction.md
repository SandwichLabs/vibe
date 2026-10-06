# Vibe direction — an agentic workspace for the data you already have

**Date:** 2026-10-06
**Status:** Adopted. This is the product north star.
- **Supersedes** the 2026-09-22 direction (Vibe as a "single-file" data workspace), whose primitives mostly carry forward. That version is in git history.
- **Plan:** [`plans/workspaces-and-connections.md`](plans/workspaces-and-connections.md).
- **Tracking:** epic #105, issues #106–#119.
- **Earlier work this builds on:** #62–#70 (shipped in PR #71) and #93 part 1 (PR #104).

> **Today vs target.** Until Slice 1 (#108–#113) lands, the code still opens one `.vibe` container that holds the data. This document describes where we are going. CLAUDE.md's *Architecture* section describes what exists.

---

## 1. The change in one paragraph

Vibe is an **agentic desktop workspace for working with data where it already lives**. It has the shape of DataGrip or DBeaver, with an AI agent, live notebooks, skills and A2UI artifacts built in, and it exposes all of it to external agents over MCP.
- **Connections:** data stays resident in its connections: local DuckDB files, MotherDuck, Postgres, SQLite, S3/GCS/R2, and the files and formats DuckDB's extensions can read. Each connection is attached into one DuckDB hub, so any source can be joined with any other.
- **Workspace:** a folder you open like an Obsidian vault and can switch at any time. Its notebooks, saved queries, skills and connection definitions are plain files, so a workspace can be a **git repository shared with a team**.
- **Credentials:** they live in the **OS keychain** or environment variables, never in the folder.
- **`.vibe` file:** it becomes an **exchange format** for notebooks, queries and skills, and is the legacy format we import from. It no longer holds data.

## 2. Why

- **What users asked for.** Early feedback was consistent. A file that *holds* the data is less useful than a tool that *reaches* the data people already have, especially shared backends like MotherDuck, where the data is already shared and versioned. The single-file container made every team workflow a copy.
- **Why the old container caused trouble.** It caused most of the trust problems the 2026-09-26 persona panel found (#93): a temp plaintext copy, Save-or-lose semantics, last-writer-wins between Studio and MCP, and no way to share notebooks without sharing data. PR #104 patched those. This direction removes their cause.
- **Prior art.** `../duck-tape` proved the hub shape (attach every source into one DuckDB, connection name = catalog alias, read-only by default). Its unbuilt PRD has the right ideas for project discovery, env interpolation and schema caching. Its secret handling (plaintext DSNs, logged) is what we will not do.

## 3. The primitives

Eight nouns. Everything in the product is one of these.

### 3.1 Workspace — #109, #111
- **What it is:** a folder with a `vibe.yaml` at the root. That file holds the id, name, default connection, agent settings and shared connection definitions.
- **What's in it:** `notebooks/*.vnb`, `queries/*.sql`, `skills/<name>/SKILL.md` and `data/`, which holds local files and the default `data/local.duckdb`.
- **Machine-local state:** lives in `.vibe-local/`, which git ignores.
- **Discovery:** found by walking up from the current directory, the way git finds `.git`.
- **Studio:** opens workspaces from a launcher with a recent-workspaces list, and switches between them from the shell header.
- **Files are the source of truth.** Writes are compare-and-write, and a watcher picks up changes from git, editors, Claude Code and other processes.
- **Git-friendly.** Files serialise in a stable, diff-friendly form. Outputs are never persisted.

### 3.2 Connections — #108, #115
- **Definition and naming.** A named, typed definition attached into the session's in-memory DuckDB hub. The name is the catalog alias in SQL (`prod_pg.public.orders`).
- **Types.** `duckdb` file, `postgres`, `motherduck`, `sqlite` and `s3`/`gcs`/`r2` first. `mysql`, `azure`, `ducklake` and `iceberg`/`delta` follow (#119).
- **Scopes:** user (every workspace), shared (committed in `vibe.yaml`) and local (this machine).
- **Behaviour.** Connections are read-only by default and only connect when first used. Each has a visible status: connected, error, needs credentials, extension missing, or locked by another process.
- **MotherDuck:** one account per workspace.

### 3.3 Credentials — #114
- **Where they live:** the OS keychain, or `env:` references for CI, headless use and development. There is no plaintext fallback.
- **Write-only across every boundary.** Studio's UI, MCP and the agent can set or clear a credential, never read it.
- **What's covered:** per-person values (username, password, tokens, keys) live with the credential, never in committed config.
- **Kept out of logs and errors.** Secrets are redacted from logs and errors.
- **The LLM API key** moves here too.

### 3.4 Tables
- **Where they live:** tables stay in their sources. DuckDB is the single engine and queries them in place.
- **Editing.** Datasheet editing, CRUD, import (CSV, xlsx, Parquet, JSON via extensions) and export work against any **writable** connection. The default target is the workspace's local DuckDB.
- **No save step.** Writes are the source's own transactions, so connection data has nothing to Save or Revert.

### 3.5 Notebooks — #66
Unchanged in spirit.
- **Format:** JSON documents of `sql` / `markdown` / `a2ui` cells, one file per notebook.
- **Running:** outputs are never persisted, and every data cell re-runs on open.
- **Connections (format v2, #108):** a SQL cell or A2UI binding may name a `connection`, and otherwise uses the workspace default.
- **Agent authoring** uses task mode: `preview_query` before a terminal `write_cell` or `need_more_info`.

### 3.6 Skills — #62
- **Format:** `SKILL.md` documents. Built-ins are embedded, and workspace skills live in `skills/<name>/SKILL.md`.
- **Compatibility:** they use the same format external agents such as Claude Code read.
- **Loading:** the agent sees each skill's name and description and loads the body on demand.
- **Table-bound skills:** gate on a catalog-qualified table, checked lazily so opening a workspace never connects to a remote source.

### 3.7 Artifacts (A2UI) — #67
Unchanged: `{ title, components[] }`, thirteen component types, a closed color enum, unknown fields rejected, inline data or a live `sql` binding, Go validates and Vue renders. Bindings gain an optional `connection`.

### 3.8 Agent + MCP — #63, #64, #70, #79, #116
- **One tool surface.** A single registry in `internal/agents` is used by the in-app runner and exposed over MCP with the same gating.
- **Default: reads freely, writes need permission.** A write needs all three of:
  - the sandbox tier allows it;
  - the connection is writable;
  - the user approves it. The #79 approval gate is forced on for any connection that isn't local.
- **SQL policy is an allow-list.** The agent can never attach databases, touch secrets or settings, install extensions, use raw-SQL passthroughs, or read files or URLs outside the workspace's data and the configured storage scopes.
- **MCP.** `vibe mcp serve` discovers the workspace from the current directory. When Studio already has the workspace open, it hands requests to Studio's server (#112), because DuckDB allows only one writer per local file.

### Import / Export — #110, #118
- **`.vibe` bundle (v2):** a zip of notebooks, queries, skills and connection definitions (never credentials). It is unencrypted by default, with optional passphrase encryption.
- **Legacy `.vibe` files (v1, SQLCipher + data):** imported permanently. The data becomes `data/<name>.duckdb`, registered as a connection.
- **Notebook export** to HTML/PDF/xlsx is still #73.

## 4. What stays, what goes, what changes

| | |
|---|---|
| **Stays** | DuckDB as the single engine; notebooks, skills, A2UI, task mode; one tool surface; MCP; guardrails; the #79 approval gate; the #80 holder-naming lock (reused for hub ownership); import wizard (#82); datasheet editing; SchoolHouse theming; TUI as a SQL REPL |
| **Goes** | The SQLCipher container as the place data lives; the plaintext temp DuckDB copy; Save / Don't save / Revert for table data (#81, #98); per-file passphrases; `vibe verify`, `compact`, `rekey`, `dump`, `passphrase` (#118); `.vibe` as the thing Studio opens |
| **Changes** | Container → workspace folder. One DuckDB → a hub of connections. `_vibe_*` rows → files. Plaintext LLM key → keychain. Agent `full` deny-list → allow-list. `Unlock` → launcher + switcher. `vibe mcp serve <file>` → `vibe mcp serve [--workspace]`. `.vibe` → exchange format and legacy import. |

## 5. Sequencing

The owner chose to build workspaces first.

```
Phase 0 ─ #106 design pass   #107 spike
              │                 │
Slice 1 ─ #108 session hub ─► #109 workspace + stores ─► #110 legacy import ─► #111 Studio launcher/shell
                     │                    └──────────► #112 hub lock + mcp serve     #113 CLI (closes #72)
Slice 2 ─ #114 secrets ─► #115 connections ─► #116 agent SQL policy ─► #117 Studio connections UI + `vibe conn`
Slice 3 ─ #118 .vibe bundle   #119 more types   #77 packaging (folder open, .vibe = import)
```

- **Slice 1** ships a usable workspace with local DuckDB only: git-shared notebooks, legacy files migrated in, agent and MCP working.
- **Slice 2** adds remote sources and the keychain.
- **Slice 3** is exchange and breadth.

## 6. Design principles

- **Data stays where it lives.** Vibe reaches data; it doesn't copy it into its own format.
- **The workspace is the app.** A folder of plain files you can version, diff, review and share.
- **Credentials in the keychain.** Never in the folder, the bundle, the logs, or the agent's context.
- **Read-only until asked.** Connections default to read-only, and agent writes need approval.
- **One tool surface.** The built-in agent and external MCP clients see the same tools with the same gating.
- **AI as collaborator, not dependency.** Everything works without AI; notebooks and SQL are first-class by hand.
- **Live, not frozen.** Notebooks and artifacts store questions (SQL), not answers.
- **Progressive disclosure.** Skills advertise a line and load on demand; connections connect on first use.

## 7. Non-goals

- Bound forms, App Mode, macros, a data-entry persona (still gone, see #68).
- A Python execution plane. Scheduled refresh may come later as a notebook feature.
- A plaintext-file fallback for credentials.
- A git client: commit, push and merge stay in the user's git tool. Studio shows branch and dirty state only.
- Multi-user / Tailscale mesh. Still deferred; shared backends plus git cover the sharing need for now.
- Windows, for this epic (macOS + Linux per #77).
- A terminal or server-side A2UI renderer.

## 8. Prior art and references

- `../duck-tape/`: `cmd/database.go` (hub and ATTACH), `docs/PRD-schema-intelligence.md` (project discovery, env interpolation, prepare-based validation, schema cache), `PRODUCTION_READINESS.md` (the secret-handling gaps to avoid).
- `../schoolwork-workspace/`: notebook, A2UI, skills and task-mode prior art (the 2026-09-22 direction).
- `docs/reviews/ux-panel-2026-09-26/SYNTHESIS.md`: the trust findings this direction answers.
- `docs/ms-access-parity-plan.md` and related: the Access-parity roadmap, superseded 2026-09-22.
