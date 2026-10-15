# DuckLake catalogs: SQLite and DuckDB metadata, local or in object storage

Status: planned (2026-10-14), reviewed. Issue #118. Removes the SQLite half of "DuckLake with a Postgres or SQLite metadata catalog" from the deferred list in `docs/direction.md` §9. Postgres metadata catalogs and a free-form "advanced attach" type are out of scope (see [Later](#later)).

## The case this must handle

A lake whose catalog is a SQLite file and whose Parquet files are both in an S3-compatible store (Floci / MinIO, no TLS, path-style URLs). By hand it attaches as:

```sql
INSTALL ducklake; INSTALL sqlite; INSTALL httpfs;
LOAD ducklake; LOAD sqlite; LOAD httpfs;
CREATE SECRET floci (TYPE s3, KEY_ID '…', SECRET '…', REGION 'us-east-1',
  ENDPOINT 'localhost:4567', URL_STYLE 'path', USE_SSL false);
ATTACH 'ducklake:sqlite:s3://oib-data-local/warehouse/ducklake/catalog.sqlite' AS lake (
  DATA_PATH 's3://oib-data-local/warehouse/ducklake/', READ_ONLY);
```

In Vibe this is two connections the person adds in their own scopes, with no SQL and no credential in any file:

```yaml
floci:
  type: s3
  scope: s3://oib-data-local/
  region: us-east-1
  options: { endpoint: "localhost:4567", url_style: path, use_ssl: "false" }
lake:
  type: ducklake
  path: s3://oib-data-local/warehouse/ducklake/catalog.sqlite
  read_only: true
  options: { catalog: sqlite, data_path: s3://oib-data-local/warehouse/ducklake/ }
```

## Why it fails today

1. `ducklakeType.Validate` (`internal/connections/ducklake.go:51-76`) refuses any scheme in `path`: the catalog must be a local DuckDB file.
2. `usesLocalPath` (`internal/workspace/connect.go:200-209`) resolves the catalog against the workspace, and `AttachSQL` requires an absolute path (`ducklake.go:86`) and `os.Stat`s it (`:94-97`), so a URL cannot be a catalog.
3. `use_ssl` is accepted by `storageType.Validate` but missing from `storageType.Info()` (`storage.go:164-192`), so Studio cannot set it (only hand-editing the YAML).
4. Storage secrets are mostly in place already: `Preflight` (`internal/session/session_connect.go:267-317`, called from `internal/workspace/session.go:238`) connects every ready storage connection at open, and again after Save/SetCredentials (`studio/connections_creds.go:97`, `studio/connections_sync.go:124,195`). The gap is narrower: when the storage connection is *not* ready (needs credentials, prompt sign-in, failed), the lake's attach fails with a raw S3 error instead of saying which connection it is waiting on, and nothing orders the two.

## Why not a free-form "manual" connection

It breaks three rules the product depends on: credentials would sit in a config file (the snippet carries `KEY_ID`/`SECRET`); the agent's policy needs to know each connection's scope, its metadata catalog and its own files; and DuckLake's `PostAttach` catalog check (which refuses a catalog naming files outside its data path) would be skipped. A fenced escape hatch is listed under [Later](#later).

## Spike results (go-duckdb v2.4.3, DuckDB 1.4.1, ducklake `f134ad8`, sqlite_scanner `0c93d61`, run 2026-10-14; the CLI 1.5.5 differs and is not relied on)

- **A SQLite catalog in object storage does not work on DuckDB 1.4.1.** `ATTACH 'ducklake:sqlite:s3://…/catalog.sqlite' AS l (DATA_PATH …, READ_ONLY)` fails with "Failed to load DuckLake table data: Unable to open database "s3://…": unable to open database file" (writable or read-only, `http://` too). The sqlite extension accepts `ATTACH 's3://…' (TYPE sqlite)` lazily and the first read fails the same way; `read_blob('s3://…/catalog.sqlite')` works, so the secret is fine. CLI 1.5.5's http catalog works; `core_nightly` has no sqlite build for 1.4.1. Decision: bump DuckDB first (step 2); URL catalogs are built on the bumped version.
- Local: `ATTACH 'ducklake:sqlite:<abs file>' AS l (DATA_PATH '<abs dir>/')` works. The metadata catalog is `__ducklake_metadata_l` (type `sqlite`, schema `main`, 22 tables, same names), and it appears in `duckdb_databases()` on 1.4.1 (so name-based hiding stays necessary).
- `path_is_relative` is `BIGINT` (0/1). The existing `PostAttach` predicate flags a crafted row (`path='/etc/passwd', path_is_relative=0` gives `bad=1`).
- Writable over a URL catalog: could not be reached on 1.4.1 (fails at ATTACH, as above, for any remote catalog); re-check on the bumped version, and keep the rule in `Validate` regardless.
- Storage secret with a local SQLite catalog and `DATA_PATH 's3://…'` (MinIO): reads work. `CREATE OR REPLACE SECRET` (same name, same creds) while attached: reads keep working. `DROP SECRET`: a read served from DuckDB's external file cache still succeeds (so a drop can look harmless); with the cache off, or for a new file, the read errors in about 1 s (HTTP 400 against the default AWS endpoint, no hang); re-creating the secret restores reads. Test and docs should not promise that a drop breaks an already-cached read.

### Step 2 re-run (duckdb-go v2.10506.0, DuckDB 1.5.6, ducklake `ac7595b0`, sqlite_scanner `5274128`, 2026-10-14)

- **A SQLite catalog in object storage works.** A catalog built locally (`ducklake:sqlite:<file>`, `DATA_PATH 's3://lake/dl/'`, parquet flushed to MinIO), copied to `s3://lake/catalog.sqlite` with `COPY (SELECT content FROM read_blob(..)) TO 's3://..' (FORMAT blob)`, attaches from a fresh instance with `ATTACH 'ducklake:sqlite:s3://lake/catalog.sqlite' AS l (DATA_PATH 's3://lake/dl/', READ_ONLY)` and reads its rows. An `INSERT` on the read-only attach is refused by DuckDB ("attached in read-only mode").
- **A writable attach over a URL catalog succeeds at ATTACH and fails at commit** ("Failed to commit DuckLake transaction"; `CHECKPOINT` says the metadata catalog is read-only). The catalog is opened read-only whatever the flag says, so keep the `Validate` rule (refuse `!read_only` with a URL catalog) rather than rely on this failure.
- The `PostAttach` predicate is unchanged on a SQLite catalog: `path_is_relative` is now `BOOLEAN` (was `BIGINT` on 1.4.1; `NOT coalesce(path_is_relative, false)` works for both), an honest lake gives `bad = 0`, a crafted row (`path='/etc/passwd', path_is_relative=0`) gives `bad = 1`. The metadata catalog is still `__ducklake_metadata_<name>`, schema `main`, queried as `__ducklake_metadata_l.main.<table>`; the stored `data_path` row is `ducklake_metadata` (`key='data_path'`, `scope IS NULL`).
- **`__ducklake_metadata_<name>` no longer appears in `duckdb_databases()`, `duckdb_columns()`, `information_schema` or `SHOW ALL TABLES`** on 1.5.6 (it is still addressable by name, and `DESCRIBE`/`SELECT` work). Name-based hiding in the catalog list and the agent policy stays as defence in depth; do not rely on `duckdb_databases()` to find it.
- DuckLake 1.0 **inlines small inserts in the catalog** (no parquet until `CALL ducklake_flush_inlined_data('<lake>')`), so tests that tamper with `ducklake_data_file` or count parquet files must flush first. Path-bearing metadata tables are the same five (`ducklake_data_file`, `ducklake_delete_file`, `ducklake_files_scheduled_for_deletion`, `ducklake_table`, `ducklake_schema`); `ducklake_delete_file` has 13 columns now (`partial_max` added). New plan operator: `DUCKLAKE_INLINE_DATA` (child of `DUCKLAKE_INSERT` / `DUCKLAKE_CREATE_TABLE_AS`), added to the write-operator matcher.
- Extensions on 1.5.6: `ducklake`, `sqlite`, `httpfs`, `azure`, `delta`, `postgres`, `mysql`, `motherduck` (loads; `v1.5.6-2026-10-169`) and `excel` install and load. **`iceberg` now needs the `avro` extension** (its init tries to auto-install it, which the hardened hub forbids): the iceberg type lists `avro` before `iceberg`.

## Design

### Definition (`internal/connections/ducklake.go`)

- New option `catalog`: `duckdb` (default, today's behaviour) or `sqlite`. Unknown values are refused (`oneOf`).
- `path` stays scheme-free: a local file or a plain storage URL (`storageurl.Validate`). Vibe builds the ATTACH target: `ducklake:<path>` for duckdb, `ducklake:sqlite:<path>` for sqlite. In `Validate`, check `IsDataURL(s.Path)` **before** `metadataSchemeRE` (`s3:` matches the scheme regexp), the way `tablesource.go:56-62` does, so `path: "postgres:…"` is still refused.
- **The read-only rule lives in `Validate`**: `!s.ReadOnly && IsDataURL(s.Path)` is refused ("a catalog in object storage can only be read: set read_only"). One place covers save (`conn_crud.go`), open (`validatePreparer.Prepare`) and hand-edited YAML.
- `AttachSQL` branches on URL for both the `filepath.IsAbs` guard and the `os.Stat` check.
- A local SQLite catalog is writable, like a local DuckDB catalog (DuckLake creates it on first write).
- No fingerprint code: `workspace.Fingerprint` already hashes every non-empty option (`internal/workspace/fingerprint.go:25-44`). Add `option:catalog` to `FingerprintFields` (help-text hint only).

### Test / probe

`connections.Test` registers one spec in a throwaway session with no storage secret (`internal/connections/test.go:208-215`), so a URL catalog's ATTACH would fail there. For a URL catalog, `ducklakeType.Probe` short-circuits like `tableSourceType.Probe` (`tablesource.go:81-84`) and `runTest` skips `sess.Attach` for it: "read with the storage connection whose scope contains it; connect the lake from the rail". A local catalog (duckdb or sqlite) is tested as today, including `PostAttach`.

### Resolution (`internal/workspace`)

- `usesLocalPath` returns false for a `ducklake` whose `path` is a URL; `ResolveConnection` then passes the URL through as `Path` (as iceberg/delta already do).
- `checkStorageURLs` (run at open/resolve time only, not at save, so a lake can be saved before its storage connection) checks the catalog URL as well as `data_path`: each must be inside the scope of a storage connection of the workspace (`storageurl.Within`); the error says which ("catalog" / "data path").
- `ServerDescription` for a URL catalog is the URL, not `filepath.Base`.
- `ducklakePreparer` skips its folder making for a URL catalog.
- A local SQLite catalog is already a protected file for the agent (`ProtectedPaths` via `usesLocalPath` + `rc.Path`, `agentaccess.go:66-78`): add a test, no code.
- Bundles: nothing to do; `ducklake` is never exported and is dropped on import (`connections.md:134`).

### Extensions

The extensions a lake needs depend on its spec: `ducklake`, plus `sqlite` for a SQLite catalog, plus `httpfs` when the catalog or data path is a URL.

- New optional `session.SpecExtensionsType { SpecExtensions(Spec) []string }`, consulted by `readyForConnect` (`session_connect.go:221`) and `Test`'s install loop (`test.go:176`), falling back to `Extensions()`.
- New `connections.ExtensionsFor(Def) []string`, used by `workspace.ExtensionNeeds` (`internal/workspace/extensions.go:25-27`, today the static `TypeInfo.Extensions`), so Settings > App > Extensions lists the lake under `sqlite` and `httpfs`.
- `InstallExtension`'s allow-list (`session_connect.go:329-337`) stays static: `sqlite` and `httpfs` are already owned by other types.

### Storage dependency (`internal/session`)

- `session.Spec` gains `Requires []string`. The workspace fills it for a ducklake with the storage connection(s) `checkStorageURLs` matched for the catalog URL and the data path URL.
- `hub.connect` connects each required connection (`connectImplicit(dep)`) before it calls `connectLocked` (which holds `adminMu`, so a dependency connected inside it would deadlock). Cycles and self-reference are refused defensively; a dependency problem is not recorded as the lake's own failure, so there is no backoff after the storage connection is fixed. Storage connections require nothing, so there are no cycles.
- A required connection that is not `ok` leaves the lake in **`StatusError`** (no new status kind; the closed set and its precedence in `session/types.go:18-32` are mirrored in `studio/frontend/src/lib/connections.ts:39-41` and the CLI) with the message "needs the `floci` connection: it needs credentials". A `needs-credentials` kind on the lake would point the rail's Sign-in at a type that has no credentials.
- No re-attach machinery: Preflight replaces a storage secret in place under the same name (`session_connect.go:290-301`), so an attached lake keeps working after a re-sign-in. Step 1 asserts this.

### Agent policy

- The lake widens nothing: the catalog URL is `Path`, not `Scope`, and only a `StorageScopeType` exports a scope (`session/registry.go:60-66`).
- The catalog's bytes are readable by the agent (`read_blob` / `read_text` are allow-listed, `internal/agents/sql_names.go:36`) **if and only if** a storage scope the person chose already covers them. That is the person's scope decision, not something the lake adds. `__ducklake_metadata_*` hiding does not protect a URL catalog's file contents; document this in `connections.md`.
- `sqlite_scan` / `sqlite_attach` stay off the allow-list.
- Tests: with `floci` ready, `read_blob('<catalog URL>')` is allowed; with no storage connection covering it, refused; `sqlite_scan` refused; `__ducklake_metadata_lake` denied for a SQLite catalog.

### Studio and CLI

- S3 form: add `use_ssl` as a "Use TLS" enum/toggle (default on), help "Turn off only for a local S3-compatible store; traffic and keys are sent unencrypted." The bundle importer already reports `use_ssl=false` as a risk (`internal/bundle/validate.go:331`).
- DuckLake form: a `Catalog` enum (`duckdb`, `sqlite`) rendered by the existing `KindEnum` `<select>` (`ConnectionEditorView.vue:124-126`). The form has no kind-dependent fields, so keep one `path` field with combined help: "Catalog file, or a storage URL inside an S3/GCS/R2/Azure connection's scope (read-only)". `useConnectionForm.ts:60-68` already treats `://` paths as outside the workspace.
- The lake's status line shows the dependency message from `StatusError`.
- CLI: `cmd/conn.go:566,572` flag help lists `catalog` and "ducklake: catalog file or storage URL"; no new flags (`--option catalog=sqlite`).

### Docs

`docs/reference/connections.md`: ducklake options table (`:103`), the ATTACH forms (`:141`), URL catalogs are read-only, the dependency message, the extensions, and the catalog-bytes note; s3 section: `use_ssl`. `docs/reference/agent-policy.md`: the catalog-bytes rule. `docs/direction.md` §9: drop the SQLite half of the deferred item. `internal/docsref` must pass.

## Steps (sequential; each a Sonnet implementation, then an Opus adversarial review)

1. **S3 `use_ssl` in the form + Go spike assertions** (small PR). `storage.go` `Info()`, editor help, docs. In the same step, a throwaway Go test (not committed) on go-duckdb's DuckDB confirming: SQLite catalog attach and schema; `NOT coalesce(BIGINT, false)`; writable attach over httpfs fails only at commit; a storage secret replaced/dropped while a lake is attached → next read errors (not hangs). Findings update the spike section above.
2. **DuckDB bump** (own PR, before any DuckLake code). Move from `github.com/marcboeker/go-duckdb/v2` v2.4.3 (DuckDB 1.4.1) to `github.com/duckdb/duckdb-go/v2` at the latest stable DuckDB 1.5.x (v2.10506.0, DuckDB 1.5.6; confirm the tag), with its `mapping` / `arrowmapping` companions and the `duckdb-go-bindings` platform modules (linux, darwin, windows). Change the import path everywhere (session, engine, importer, tests, Wails build tags), then `go mod tidy`. Re-verify: the agent policy's plan-operator names (`docs/reference/agent-policy.md:117`) against `EXPLAIN` output; extension versions and installability for 1.5 (`ducklake`, `sqlite`, `httpfs`, `azure`, `iceberg` (which now also needs `avro`: its consent, status and `--install` paths name both), `delta`, `postgres`, `mysql`, `motherduck`, `excel`) including the app-private extension directory layout; `duckdb_databases()` hiding of `__ducklake_metadata_*`; CI and release (the C toolchain, `duckdb_arrow` tag, musl/macOS/Windows builds, `govulncheck`), `task notices` (regenerate `THIRD_PARTY_NOTICES.md`), and the version shown in Settings > About; update every doc that names the binding or the DuckDB version (`CLAUDE.md` tech stack, `docs/packaging.md`, `docs/reference/connections.md`, `docs/reference/agent-policy.md`, `scripts/notices/duckdb-third-party.md`). **Acceptance:** all gates green, and the spike re-run on the new version: a SQLite catalog uploaded to MinIO attaches `READ_ONLY` via `ducklake:sqlite:s3://…` and reads rows; record whether a writable attach fails at ATTACH or at commit. If it still fails, stop and revisit this plan.
3. **Catalog object vetting (security, pre-existing on main).** Step 2's review showed that a crafted DuckDB-file catalog can hold **views** (or macros) that DuckLake queries as its own tables: a view replacing `ducklake_inlined_data_<n>` (or registered in `ducklake_inlined_data_tables`) over `read_text('/outside/secret.txt')` let the agent, read-only tier, read that file through `SELECT * FROM lake.people` (plan: just `DUCKLAKE_SCAN`). A view over `ducklake_data_file` can also feed clean paths to `PostAttach` and other paths later. On 1.5.6 `duckdb_views()` / `duckdb_tables()` / `information_schema` hide `__ducklake_metadata_<name>`, so: before the ducklake ATTACH, open the catalog file on its own (`ATTACH '<file>' AS <tmp> (READ_ONLY)` for duckdb, `(TYPE sqlite, READ_ONLY)` for sqlite) and refuse any view, macro, or table outside DuckLake's known metadata table set (allow `ducklake_inlined_data_<n>` tables by pattern, as tables only); then detach and attach the lake. Same check in Test. Note the window between check and attach (local file: acceptable, document it; same-process re-attach). Tests: the reviewer's view-over-inlined-data repro and the view-over-`ducklake_data_file` repro, for duckdb and sqlite catalogs, through the agent handler; an honest lake (with inlined data) still attaches; a known table name that is a view (`CREATE VIEW ducklake_data_file …`) is refused by its type, not just its name; a drift test dumps the installed extension's metadata table set and fails when the allow-list is out of date (so a DuckLake upgrade is caught in CI, not by users). Docs: connections.md "The catalog is checked on every attach".
4. **The type.** (the catalog vetting of step 3 also runs for a URL catalog: read it over httpfs with the storage secret before the lake attach.) `catalog` option, Validate rules (URL-before-scheme order, read-only for URL), `AttachSQL` targets, `PostAttach` against a SQLite catalog (crafted-catalog test re-run for `catalog: sqlite`), Test/Probe short-circuit for URL catalogs, `SpecExtensions`. Unit tests in `internal/connections`.
5. **Workspace resolution.** `usesLocalPath`, `ResolveConnection`, `checkStorageURLs` for the catalog URL, `ServerDescription`, `ducklakePreparer`, `ExtensionsFor` / `ExtensionNeeds`, `ProtectedPaths` test for a local SQLite catalog, CLI help. Tests in `internal/workspace`.
6. **Storage dependency.** `Spec.Requires`, ordered connect in `connectLocked`, `StatusError` message, Studio status line. Also re-register dependents when a storage connection changes or goes away (today `syncConnections` re-registers only connections whose own signature changed, `studio/connections_sync.go:79-89`, so a lake keeps a spec that passed an old scope check). Session tests with `sessiontest` fakes.
7. **End to end.** MinIO integration test under the existing `//go:build integration` tag, skipping when MinIO is unavailable (as `minio_integration_test.go` does): SQLite catalog + Parquet in S3, read-only; a local SQLite catalog read-write. Agent-policy tests. Docs. Live QA in Studio with Rodney: add `floci` + `lake`, open the lake from the rail, browse `lake.warehouse`, run a notebook cell; then the same with `floci` signed out (dependency message).

## Acceptance

- The YAML above (credentials in the keychain) opens, the lake connects on first use, and `SELECT count(*) FROM lake.warehouse.dim_students` works in Studio, the CLI and over MCP (read-only).
- With `floci` signed out, the lake shows "needs the `floci` connection: it needs credentials", not an S3 error.
- A writable lake with a URL catalog is refused by `Validate` (save, open, hand edit).
- A catalog URL or data path outside every storage scope is refused when the workspace is opened and the connection resolved (an issue in the rail; order-independent, like `data_path`, iceberg and delta), not at save.
- Every crafted-catalog case is refused for a SQLite catalog as for a DuckDB one.
- No credential appears in any file, log line, error or agent-visible result.

## Later

- **Postgres (and MySQL) metadata catalogs**: host/port/database fields, a per-person sign-in, a spike on how DuckLake takes the login on our DuckDB version (`METADATA_PARAMETERS` vs a named secret); `PostAttach` must take the metadata schema (`public` / `METADATA_SCHEMA`).
- **Advanced attach** (escape hatch): own scopes only; one `CREATE SECRET` + one `ATTACH … AS <name>` parsed and checked (alias = connection name); credentials only as `${secret:…}` placeholders resolved from the keychain, a literal credential refused; not readable by the agent until #134. Would cover Iceberg REST catalogs until they get a type.
