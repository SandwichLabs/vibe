# Changelog

All notable changes to Vibe are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Vibe uses [Semantic Versioning](https://semver.org/) (pre-1.0: minor versions may change behaviour).

## [Unreleased]

### Added

- **Data artifacts.** A parameterised, live view stored as files in `artifacts/<name>/` (`artifact.yaml` with typed parameters and named queries, plus an A2UI `view.a2ui.json`). Parameter controls (select, date range, number, text), a `pivot` crosstab and drill-down re-run only the queries that use a changed parameter. Studio has an Artifacts list, a view and a Source tab with conflict handling; the agent and MCP clients author with `write_artifact`, `list_artifacts` and `get_artifact` (no tool runs an artifact); `.vibe` bundles carry them; `vibe artifact list` and `vibe export bundle --artifacts` mirror the UI. A value is only ever a bound parameter, and every query is checked against the read-only SQL policy when it is saved and again on every run, so a hand-edited, pulled or bundled artifact is held to it too. See `docs/reference/artifacts.md`. ([#162](https://github.com/SandwichLabs/vibe/issues/162))

### Changed

- The data artifact plan records that custom (level 2) views are not built: the sandboxed-iframe design was dropped after the Linux isolation spike (a frame can reach the Wails binding IPC, [#174](https://github.com/SandwichLabs/vibe/issues/174)); a worker reshape awaits a macOS run ([#158](https://github.com/SandwichLabs/vibe/issues/158)).

## [0.1.4] - 2026-10-16

### Added

- **Set up storage without leaving the lake.** When a DuckLake, Iceberg or Delta connection points at `s3://`, `gs://`, `r2://` or `az://`, the editor names the storage connection that will read it, and if there is none offers **Set up storage**, prefilled, then brings you back to the lake. `vibe conn add` prints the matching `vibe conn add` hint. S3 presets (Amazon S3, MinIO, Backblaze B2, Floci, other S3-compatible) fill in the endpoint, URL style and TLS fields. ([#130](https://github.com/SandwichLabs/vibe/issues/130))
- **Test a lake through its storage.** Test on a lake connects its storage connection first and says which of the two failed.
- **AWS SSO sign-in for S3 (preview).** An `s3` connection can sign in with an IAM Identity Center profile (`--sign-in aws-sso:PROFILE`, or the "AWS profile (SSO)" card). Studio shows a sign-in banner with a code; `vibe conn credentials sign-in NAME` does the same in a terminal, and `aws sso login` works too (shared `~/.aws/sso/cache`). Short-lived keys are renewed before they expire, and an ended session signs the connection out rather than reading anonymously. Not yet verified against a real Identity Center. ([#131](https://github.com/SandwichLabs/vibe/issues/131))

### Changed

- **`AWS_REGION` is no longer read.** An `s3` connection outside `us-east-1` that only worked because your shell set `AWS_REGION` now fails with an AWS signing error (`AuthorizationHeaderMalformed` or a 301). Set `region` on the connection (`--region`, or the Region field).
- **Wildcards on s3, gcs and r2 are expanded by Vibe.** `*` and `**` still work in `read_parquet`, `FROM '...'` and `glob()`; `?` and `[...]` are refused in a pattern, one statement expands to at most 10,000 objects, and the pattern must start with the scope of a ready storage connection. Azure is unchanged. ([#141](https://github.com/SandwichLabs/vibe/pull/141))
- **A `TEMP MACRO` or `TEMP FUNCTION` lasts only for the run that created it.** TEMP tables and views are unaffected.
- **The query editor, `vibe query` and the TUI refuse** `INSTALL`, `LOAD`, `UPDATE EXTENSIONS`, `CREATE`/`DROP SECRET`, and `SET`/`RESET`/`PRAGMA` of `s3_url_compatibility_mode`. Install extensions with the connection's Install button or `--install`.

### Security

- **S3 reads never use your shell's AWS credentials.** DuckDB copied `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN` and `AWS_REGION` into every session, so a storage URL no connection covered was read as you. Every session now has a catch-all deny secret per scheme. ([#135](https://github.com/SandwichLabs/vibe/issues/135))
- **A URL can no longer redirect a connection's signed request.** `?s3_endpoint=` and the other `?s3_*` URL parameters, which can come from an Iceberg manifest or a DuckLake catalog, overrode the secret's endpoint; the whole URL is now the object key. ([#138](https://github.com/SandwichLabs/vibe/issues/138))

### Fixed

- **A lake could attach without its storage secret** when connections were re-registered concurrently (a regression from 0.1.3). ([#137](https://github.com/SandwichLabs/vibe/pull/137))

## [0.1.3] - 2026-10-15

### Added

- **DuckLake catalogs in SQLite, and in object storage.** The DuckLake type takes a `catalog` option (`duckdb` or `sqlite`) and a catalog that is a storage URL (read-only), so a lake whose SQLite catalog and Parquet files both live in an S3-compatible store attaches as two connections, with no SQL and no credential in any file. A catalog is vetted before it is attached. ([#118](https://github.com/SandwichLabs/vibe/issues/118))
- **Storage dependencies.** A DuckLake, Iceberg or Delta connection whose URLs are in object storage connects its storage connection first, and says `needs the "NAME" connection` when that is not ready.
- **Use TLS** on S3 connections (default on), for local S3-compatible stores such as MinIO and Floci.

### Changed

- **DuckDB 1.5.6.**
- The connection form no longer writes an option's default into the YAML; a blank choice reads "Default (…)".

## [0.1.2] - 2026-10-14

### Added

- **Update check and `vibe update`.** Studio asks once whether to check GitHub for new releases (**Check weekly** or **No thanks**; development builds never ask). Settings, About, Updates has the toggle, **Check now**, the release notes (shown as plain text) and, for a Vibe installed by `install.sh`, **Update to vX**: after you confirm, it downloads that release's `install.sh`, verifies it against the release's `checksums.txt` and runs it (never `--force`), then offers **Restart now** (through the normal quit path, so unsaved-work guards run; the old Studio keeps running until you do). `vibe update [--check] [--yes]` does the same from a terminal; `vibe config updates [off|weekly]` mirrors the toggle. Nothing is checked or installed in the background without your answer; builds from source, `go install` and package managers are told to update the way they were installed. What is sent is listed in [Privacy](docs/privacy.md).
- **Motion charts in A2UI.** Seven new chart types (`areaChart`, `columnChart`, `ringChart`, `radarChart`, `sankeyChart`, `heatmap`, `streamChart`), each with inline data or a live `sql` binding, and a closed `motion` object on every chart (easing preset, speed, stagger, spring tuning), off under reduced motion. ([#117](https://github.com/SandwichLabs/vibe/pull/117))

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

[Unreleased]: https://github.com/SandwichLabs/vibe/compare/v0.1.4...HEAD
[0.1.4]: https://github.com/SandwichLabs/vibe/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/SandwichLabs/vibe/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/SandwichLabs/vibe/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/SandwichLabs/vibe/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/SandwichLabs/vibe/releases/tag/v0.1.0
