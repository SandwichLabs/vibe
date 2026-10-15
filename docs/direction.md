# Vibe direction — an agentic workspace for the data you already have

**Date:** 2026-10-06 (delivered 2026-10-06; see §9)
**Status:** **Delivered.** Epic #105 (issues #106–#119) is merged: the code matches this document, apart from the departures listed in §9. This remains the product north star.
- **Supersedes** the 2026-09-22 direction (Vibe as a "single-file" data workspace), whose primitives mostly carry forward. That version is in git history.
- **Plan:** [`plans/workspaces-and-connections.md`](plans/workspaces-and-connections.md) (implemented; its "as built" summary lists the amendments and open follow-ups).
- **Tracking:** epic #105, issues #106–#119. What is left (packaging and signing in #77, and the follow-ups named in §9) is listed there.
- **Earlier work this builds on:** #62–#70 (shipped in PR #71) and #93 part 1 (PR #104).

---

## 1. The change in one paragraph

Vibe is an **agentic desktop workspace for working with data where it already lives**. It has the shape of DataGrip or DBeaver, with an AI agent, live notebooks, skills and A2UI artifacts built in, and it exposes all of it to external agents over MCP.
- **Connections:** data stays resident in its connections: local DuckDB files, MotherDuck, Postgres, SQLite, S3/GCS/R2, and the files and formats DuckDB's extensions can read. Each connection is attached into one DuckDB hub, so any source can be joined with any other.
- **Workspace:** a folder you open like an Obsidian vault and can switch at any time. Its notebooks, saved queries, skills and connection definitions are plain files, so a workspace can be a **git repository shared with a team**.
- **Credentials:** they live in the **OS keychain** or environment variables, never in the folder.
- **`.vibe` file:** it is an **exchange format** (a bundle) for notebooks, queries, skills and connection definitions. It no longer holds data, and the older data-holding `.vibe` container is not opened or imported at all (§9).

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
- **Types.** `duckdb` file, `postgres`, `mysql`, `motherduck`, `sqlite`, `s3`/`gcs`/`r2`, `azure`, `ducklake` and `iceberg`/`delta` (#115, #119). The metadata-driven formats (`ducklake`, `iceberg`, `delta`) can only be defined in your own scopes, never in a committed `vibe.yaml` (§9).
- **Scopes:** user (every workspace), shared (committed in `vibe.yaml`) and local (this machine).
- **Behaviour.** Connections are read-only by default and only connect when first used. Each has a visible status: connected, error, needs credentials, extension missing, or locked by another process.
- **MotherDuck:** one account per workspace. A MotherDuck connection cannot be attached read-only (DuckDB does not support `READ_ONLY` on `md:`); use a read-scaling token for read-only access.

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

### Import / Export — #118
- **`.vibe` bundle (v2):** a zip of notebooks, queries, skills and connection definitions (never credentials). It is unencrypted by default, with optional passphrase encryption ([age](https://age-encryption.org)). `vibe export bundle`, `vibe import <file.vibe>`, and the workspace menu in Studio; opening a `.vibe` file with Vibe starts the import.
- **Legacy `.vibe` files (v1, SQLCipher + data):** **not supported** (#110 was dropped). They get a plain "not supported" message.
- **Notebook export** to HTML/PDF/xlsx is still #73.

## 4. What stays, what goes, what changes

| | |
|---|---|
| **Stays** | DuckDB as the single engine; notebooks, skills, A2UI, task mode; one tool surface; MCP; guardrails; the #79 approval gate; the #80 holder-naming lock (reused for hub ownership); import wizard (#82); datasheet editing; SchoolHouse theming; TUI as a SQL REPL |
| **Goes** (gone) | The SQLCipher container and `internal/container`; the plaintext temp DuckDB copy; Save / Don't save / Revert for table data (#81, #98); per-file passphrases; `verify`, `compact`, `rekey`, `dump`, `passphrase`; `.vibe` as the thing Studio opens |
| **Changes** | Container → workspace folder. One DuckDB → a hub of connections. `_vibe_*` rows → files. Plaintext LLM key → keychain. Agent `full` deny-list → allow-list. `Unlock` → launcher + switcher. `vibe mcp serve <file>` → `vibe mcp serve [--workspace]`. `.vibe` → exchange bundle. |

## 5. Sequencing

The owner chose to build workspaces first.

```
Phase 0 ─ #106 design pass   #107 spike
              │                 │
Slice 1 ─ #108 session hub ─► #109 workspace + stores ─► (#110 legacy import: dropped) ─► #111 Studio launcher/shell
                     │                    └──────────► #112 hub lock + mcp serve     #113 CLI (closes #72)
Slice 2 ─ #114 secrets ─► #115 connections ─► #116 agent SQL policy ─► #117 Studio connections UI + `vibe conn`
Slice 3 ─ #118 .vibe bundle   #119 more types   file-open wiring (done)   #77 packaging & signing (open)
```

- **Slice 1** shipped a usable workspace with local DuckDB only: git-shared notebooks, agent and MCP working.
- **Slice 2** adds remote sources and the keychain.
- **Slice 3** is exchange and breadth. Done, apart from packaging.

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

- Earlier internal prototypes (a DuckDB hub with ATTACH, schema discovery and secret-handling lessons; a notebook, A2UI, skills and task-mode prototype). Not part of this repository.
- A three-persona UX panel review (2026-09-26): the trust findings this direction answers. Its write-up is not part of the public repository.
- `docs/archive/ms-access-parity-plan.md` and related: the Access-parity roadmap, superseded 2026-09-22.

## 9. As delivered: what changed from the plan

Everything above shipped. These are the places where the result differs from what was first written, in the order they matter.

- **No legacy import (scope change, #110 closed).** The plan made the legacy `.vibe` importer permanent. It was dropped by an owner decision recorded on the epic (2026-10-06), which also let the container code go. Studio, the CLI and MCP never open a v1 file, `internal/container` and the container commands (`info`, `verify`, `compact`, `rekey`, `dump`, `passphrase`) are gone (the names stay registered as hidden stubs that point to `vibe workspace init`), and a v1 or SQLCipher `.vibe` is told "not supported". `MigratePages` went with it, so old pages are not migrated.
- **The agent's SQL policy is an allow-list, parsed first (#116).** The syntactic check happens before any `EXPLAIN` binds the statement. Reads are the readers the policy names, over `data/` and the scope of a ready storage connection; writes need three gates (tier, writable connection, person's approval), and the approval is always required for anything that is not a local file, so headless MCP refuses those writes. See [`reference/agent-policy.md`](reference/agent-policy.md).
- **Metadata-driven formats are own-scope only (#119).** DuckLake, Iceberg and Delta read whatever files their own metadata names, so a crafted table in a cloned repository could read any file on the machine. They can be defined only in your own scopes (`local.yaml`, the user `connections.yaml`), never in a committed `vibe.yaml`, and `iceberg_scan`, `iceberg_metadata`, `iceberg_snapshots` and `delta_scan` are not agent readers in any tier. Giving the agent a safe path is #134 (a separate agent hub with external access off, plan D13).
- **Trust moved out of the folder.** A workspace arrives with whatever its authors committed, so a committed `vibe.yaml` can only *tighten* the agent's limits, its `credentials:` line is only a UI hint, `${VAR}` interpolation and absolute paths are refused in it, and per-person settings (looser agent policy, sign-in method) live outside the folder, keyed by workspace id and the folder's real path.
- **MotherDuck cannot be read-only.** `READ_ONLY` is not supported on `md:` attaches, so the type defaults to `read_only: false` and rejects `true`; read-only comes from a read-scaling token.
- **Bundles use age (#118).** The optional passphrase envelope is an [age](https://age-encryption.org) scrypt recipient, not a bespoke format.
- **Packaging is wired but not shipped.** Launch arguments, macOS `OnFileOpen`, the `Info.plist` document types and the Linux `.desktop` and MIME files are in the repo ([`packaging.md`](packaging.md) §1a). Signing, notarisation, CI, the release pipeline, the offline-extension decision and the signed-build keychain check remain epic #77.

### Open-source launch decisions (2026-10-11, epic #51)

Recorded under #36 and #39; the audit and the plan are in [`plans/open-source-launch.md`](plans/open-source-launch.md).

- **Name:** keep **Vibe**; the CLI binary stays `vibe`. Because Mistral Vibe also installs a `vibe` command, `install.sh` will carry a collision guard (#47) (it refuses to overwrite another vendor's `vibe`) and the README says so. Revisit before 1.0.
- **Repo and module:** `SandwichLabs/vibe`, Go module `github.com/SandwichLabs/vibe`. Private until the go-public step (#50).
- **Licence:** Apache-2.0.
- **History:** the public repo is a filtered copy of `vibe-app` `main`. `landing/`, `docs/reviews/ux-panel-2026-09-26/` and `docs/archive/*.txt` were dropped from all history, the 88 MB binary was never on `main`, and a work-email author was mailmapped.
- **Reporting:** GitHub only, no email address. Vulnerabilities go through GitHub private vulnerability reporting; everything else through Issues.
- **Contributions:** DCO sign-off (`git commit -s`), no CLA.
- **Old repo:** `SandwichLabs/vibe-app` is archived (read-only, pointer in its README). All development happens in `SandwichLabs/vibe`; `vibe-app#N` references in the docs are history.
- **Telemetry:** none.
- **v0.1.0 shape:** unsigned, ad-hoc-signed tarballs installed by `install.sh`; signing, notarisation and Homebrew stay with the packaging plan ([`packaging.md`](packaging.md)).
- **Pruned for the public:** the ast-grep rules moved to `tools/ast-grep/` and `RODNEY.md` to `docs/contributing/rodney.md`.

### Open follow-ups

- #134: agent reads of Iceberg and Delta through a confined hub.
- #123: auto-number gap off-by-one after reopening a file.
- MotherDuck has only been exercised against the spike and with fakes; a pass with a real token is outstanding.
- Iceberg REST catalogs; DuckLake with a Postgres metadata catalog (a DuckDB or SQLite catalog, on this computer or read-only in object storage, is supported).
- Removing a downloaded extension from Studio (there is Install but no Remove).
- #77: signing and notarisation, CI and release, extension bundling, keychain on signed builds.
- #73: notebook export to HTML, PDF and xlsx. #56 (Studio Home) is superseded by the launcher.
