# Changelog

All notable changes to Vibe are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Vibe uses [Semantic Versioning](https://semver.org/) (pre-1.0: minor versions may change behaviour).

## [Unreleased]

### Added

- **Update check and `vibe update`.** Studio asks once whether to check GitHub for new releases (**Check weekly** or **No thanks**; development builds never ask). Settings, About, Updates has the toggle, **Check now**, the release notes (shown as plain text) and, for a Vibe installed by `install.sh`, **Update to vX**: after you confirm, it downloads that release's `install.sh`, verifies it against the release's `checksums.txt` and runs it (never `--force`), then offers **Restart now** (through the normal quit path, so unsaved-work guards run; the old Studio keeps running until you do). `vibe update [--check] [--yes]` does the same from a terminal; `vibe config updates [off|weekly]` mirrors the toggle. Nothing is checked or installed in the background without your answer; builds from source, `go install` and package managers are told to update the way they were installed. What is sent is listed in [Privacy](docs/privacy.md).

## [0.1.1] - 2026-10-13

### Fixed

- **MotherDuck reads from the agent and over MCP.** While a MotherDuck connection was attached, every agent query was refused (`plan operator DOWNLOAD_SOURCE (L) is not on the allow-list`), including plain aggregates over `data/` files. MotherDuck marks each step of a query plan with where it runs, and the policy now ignores that marker. ([#111](https://github.com/SandwichLabs/vibe/issues/111))
- **`introspect_schema` and `describe_table` connect on first use**, like `run_query`, instead of calling a configured but idle connection "unknown". A failed connect reports its own error.
- **Clearer refusals.** A name whose first part isn't a connection (`newsroom.raw.t`) now says there is no such connection, and a four-part name (`md.newsroom.raw.t`) says a table name has at most three parts. Both explain that each MotherDuck database needs its own connection (same token); see [connections](docs/connections.md#motherduck).

### Changed

- **The `read-write` tier can write notebook cells and skills** (`add_query_cell`, `create_dashboard`, `create_or_update_skill`), so an agent that can read your data can also write up what it finds. Creating or dropping tables and saving queries still need `full`, and "Ask before changes" still applies.

## [0.1.0] - 2026-10-12

The first public release. Vibe is an agentic desktop workspace for working with data where it already lives: connections keep your data in place, a workspace is a folder of plain files, and an agent (inside Studio or your own, over MCP) works with both under strict limits.

### Added

- **Workspaces.** A workspace is a folder (optionally a git repository) with `vibe.yaml`, `notebooks/`, `queries/`, `skills/` and `data/`, plus git-ignored machine-local state in `.vibe-local/`. Files are the source of truth, writes are compare-and-write, and a watcher picks up outside changes. Found by walking up from the current directory, like git. Starter templates (`contacts`, `inventory`, `order-entry`, `training-registrations`) via `vibe workspace init --template`.
- **Connections.** Local DuckDB and SQLite files, MotherDuck, Postgres, MySQL, S3, GCS, Cloudflare R2, Azure Blob/ADLS, DuckLake, Iceberg and Delta, all attached on first use into one in-memory DuckDB hub per workspace. The connection name is the catalog alias, so sources join in one query. Read-only by default, with a visible status per connection and scopes (shared, this computer, all workspaces). A guided editor in Studio and `vibe conn add | list | test | connect | credentials | move | rm`.
- **Credentials.** Kept in the OS keychain, or read from environment variables (`env:PREFIX`), or asked each time. Write-only across the Studio bridge, MCP and the agent. A workspace `.env` is loaded only after you allow it (direnv-style; `vibe workspace env`).
- **Live notebooks.** `sql`, `markdown` and `a2ui` cells; every data cell re-runs when the notebook opens and outputs are never saved. AI-assisted cell authoring (preview a query, then write the cell).
- **Skills.** `SKILL.md` documents that tell the agent what your data means; advertised by name and loaded on demand. Built-in skills are embedded.
- **A2UI artifacts.** Validated, SQL-bound dashboards (stats, tables, bar and line charts) the agent can draw, rendered with hand-written CSS and SVG.
- **Agent.** Studio's chat agent with sandbox tiers (`read-only`, `read-write`, `full`), an SQL allow-list policy, approval cards for writes, grounding checks and result budgets. Providers: a local OpenAI-compatible server (default), Ollama, OpenAI, Anthropic, Google Gemini and Cloudflare AI Gateway.
- **MCP.** `vibe mcp serve` over stdio or loopback SSE exposes the same tool registry, with the same gating, to Claude Code, Claude Desktop, Cursor and other clients. One owner per workspace: further servers proxy to the owner, Studio's approval cards apply to proxied writes, and Studio can take over from a headless server. Studio copies ready-made Claude Code and Claude Desktop config.
- **Studio.** The Wails desktop app: launcher with recent workspaces, a connections rail, query editor with saved queries, notebooks, datasheet editing, import wizard, skills editor, agent panel, settings and four built-in themes plus your own JSON themes.
- **Command line.** `vibe workspace init | info | env`, `query`, `import`, `export`, `conn`, `config llm | llm-key`, `studio`, `mcp serve`, and `init` as an alias.
- **Terminal UI.** `vibe studio` offers a SQL prompt, a read-only table browser and a notebook list.
- **Import and export.** CSV, TSV and Excel in with full-file type inference and a pre-flight check; CSV, Parquet, JSON and Excel out; notebooks as `.vnb`.
- **Bundles.** `.vibe` files are zip bundles of notebooks, queries, skills and (optionally) sanitised connection definitions, with optional age passphrase encryption. They never hold data or credentials.
- **Install and packaging.** `install.sh` for Linux and macOS (amd64 and arm64) with checksum verification, a manifest-based upgrade and uninstall, a `--cli-only` mode and a guard against overwriting another program named `vibe`. Linux tarballs ship desktop, MIME and icon files; macOS ships an ad-hoc-signed `Vibe Studio.app`.
- **Settings, About.** Version, commit and build date, licence, third-party notices, a privacy link and a Report a bug button.
- **Documentation.** Install, quickstart, connection recipes, MCP client setup, troubleshooting, privacy and architecture guides, alongside the reference.

### Security

- The agent's SQL is an **allow-list** parsed before any `EXPLAIN` binds it. A write needs three gates: the tier allows it, the connection is writable, and you approve it. Approval is always required for a connection that is not a local file, so headless MCP refuses those writes.
- A committed `vibe.yaml` can only **tighten** the agent's limits. Looser per-person settings and sign-in methods live outside the folder, keyed by workspace id and path. A saved credential is bound to the server it was entered for and is not sent to a changed one.
- DuckLake, Iceberg and Delta (formats whose metadata names the files to read) can only be defined in your own scopes, never in a committed `vibe.yaml`.
- DuckDB runs hardened: autoinstall and autoload off, community extensions off, an app-private extension directory, and extensions downloaded only when you ask for a feature that needs one (Install, `--install`, or an Excel import or export).
- MCP over SSE binds to `127.0.0.1`, requires a fresh random bearer token, and rejects browser `Origin` headers and non-loopback hosts. The lock file that records the endpoint is same-user only.
- No telemetry. See [docs/privacy.md](docs/privacy.md).

### Known limitations

- **macOS builds are unsigned.** They are ad-hoc signed, not notarised. A browser-downloaded app needs System Settings, Privacy & Security, **Open Anyway**; the install script avoids the prompt. The macOS minimums (14 on Apple silicon, 13 on Intel) come from linked libraries and have not been run on those macOS versions. Permission prompts for Documents or Desktop may reappear after an upgrade.
- **No Windows build.**
- **Single-user.** Sharing is through shared backends and git. There is no multi-user mesh.
- **The chat agent needs a model.** A local server is the default endpoint; hosted providers need your own API key. Everything else works without a model.
- **Extensions need the network once.** DuckDB extensions are downloaded on first use of each connection type (on Install, `--install`, or the first Excel import or export) from `extensions.duckdb.org`; none ship inside the app, and there is no Remove button yet. See [troubleshooting](docs/troubleshooting.md#extension-download-blocked-or-offline) for offline use.
- **MotherDuck** cannot be attached read-only (use a read-scaling token) and is limited to one account per workspace.
- **The agent cannot read Iceberg or Delta tables** yet; you can in the SQL editor. DuckLake supports only a local DuckDB metadata catalog, and Iceberg REST catalogs are not supported.
- **Notebook export** to HTML, PDF or xlsx is not available yet.
- Older data-holding `.vibe` (SQLCipher) files are not opened.
- Linux builds target glibc 2.35 or newer and WebKitGTK 4.1; musl and older distributions are unsupported.

[Unreleased]: https://github.com/SandwichLabs/vibe/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/SandwichLabs/vibe/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/SandwichLabs/vibe/releases/tag/v0.1.0
