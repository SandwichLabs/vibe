# Connections

A connection is a place data lives. Opening a workspace dials nothing remote: a connection reads **not connected** until something uses it (a query that names it, `vibe conn connect`, expanding it in Studio). Local `duckdb` and `sqlite` files attach when the workspace opens; everything else attaches on first use (storage and table connections create their secret or load their extension when the workspace opens, which dials nothing).

Definitions are plain settings in `vibe.yaml` (shared), your own `local.yaml` (this computer) or your user `connections.yaml` (all workspaces). **Credentials are never in any of them**; see [workspaces.md](workspaces.md#credentials) for sign-in methods (keychain, `env:PREFIX`, prompt), the fingerprint binding and redaction.

Manage them with `vibe conn add | list | test | connect | rm | move | credentials set|clear|status`. `vibe conn list` shows scope, shadowing, sign-in method and status. Status precedence: `extension-missing` > `needs-credentials` > `error` > `locked-by` > `not-connected` > `ok`.

## Rules every type follows

- A DSN or credential never appears in an `ATTACH` string. Postgres is `ATTACH '' AS x (TYPE postgres, SECRET s)`; the secret is a `CREATE TEMPORARY SECRET` run on the session's non-logging path.
- Every value is quoted by one helper; fields a type does not understand (unknown options) are rejected, not passed on.
- **Extensions are installed only on consent.** A missing extension is `extension-missing`; `vibe conn test|connect --install` (or Studio's Install button) downloads it into Vibe's private extension directory. Connecting never installs.
- **Test** attaches the definition in a throwaway, hardened, in-memory DuckDB with a 10 second limit and reports ok with latency and a count, or a redacted error. It never touches the workspace session. MotherDuck is tested in a subprocess (`vibe __conn-test`, hidden), with the token on stdin only, because one process holds one MotherDuck token.
- **Read-only is a client-side guard.** `read_only` (the default) makes DuckDB refuse writes through ordinary SQL, but a database role that can write can still be reached by other means, so **use a read-only database role** for anything you must not change. See each type below.
- **What the agent may do with a connection** (reads, the three write gates, files and URLs, the never-list) is in [agent-policy.md](agent-policy.md). In short: the agent reads any attached catalog; a write needs the tier, a writable (`read_only: false`) connection and the person's OK; and the OK is **always** required for anything that is not a local `duckdb`/`sqlite` file, so headless MCP refuses those writes. `list_connections` shows the agent each connection's name, type, status, read-only flag, local or remote and (for storage) scope, never a DSN or credential.

## First use, failures and sign-in

- A statement connects the connections it names in qualifier position (`pg.public.orders`, `"pg".t`), outside string literals, dollar-quoted strings and comments. A pending connection that merely shares a word with an unrelated statement (`CREATE TABLE sales`, `AS sales`) is not dialled. If it cannot connect, the statement still runs; only when it then fails because the catalog is missing is the connection's own (redacted) message returned.
- A failed connect is remembered for 30 seconds and not retried by implicit use; an explicit `Connect`, `Preflight` (after the person changes something) or `Test` tries again.
- Errors that reach the agent or an MCP client have the user name hidden too (`user='***'`); the UI still shows it to its owner.
- **A shared `credentials:` line is only a hint** and each `sign_in` is bound to the definition fingerprint; see [workspaces.md](workspaces.md#credentials).

## Types

### `duckdb`

| Field | Notes |
|---|---|
| `path` | A `.duckdb` file. Relative and inside the workspace for a shared connection; absolute only in your own scopes. |

Attach is `ATTACH 'path' AS x (READ_ONLY)`; read-only is enforced by DuckDB.

### `postgres`

| Field | Notes |
|---|---|
| `host` | required |
| `port` | default 5432 |
| `database` | required |
| `sslmode` (option) | `disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full` |
| `user` | per person, readable |
| `password` | per person, write-only |

`ATTACH 'connect_timeout=10 [sslmode=...]' AS x (TYPE postgres, SECRET vibe_x[, READ_ONLY])`: only the non-secret `sslmode` and a 10 second libpq `connect_timeout` ever ride in the string. The `database` value is quoted by DuckDB as one `dbname`, so libpq keywords inside it cannot redirect the connection. DuckDB's Postgres errors echo `password='...'`; Vibe redacts them. `READ_ONLY` refuses `INSERT`/`CREATE` through DuckDB, but `postgres_execute` multi-statement payloads can escape it, so those functions are denied to the agent and a **read-only role** is the real guard. After DDL made elsewhere, refresh the catalog (`RefreshCatalog` runs `CALL pg_clear_cache()`).

### `mysql`

| Field | Notes |
|---|---|
| `host` | required |
| `port` | default 3306 |
| `database` | required |
| `ssl_mode` (option) | `disabled`, `preferred`, `required`, `verify_ca`, `verify_identity` |
| `user` | per person, readable |
| `password` | per person, write-only |

Same shape as Postgres: `CREATE TEMPORARY SECRET (TYPE mysql, HOST, PORT, DATABASE, USER, PASSWORD[, SSL_MODE])`, then `ATTACH '' AS x (TYPE mysql, SECRET vibe_x[, READ_ONLY])`. Nothing but the secret's name is in the `ATTACH`, and `duckdb_databases().path` is empty for the catalog. MariaDB works too. Notes:

- The extension attaches the whole server's database list; Vibe's rail hides MySQL's own `mysql`, `performance_schema` and `sys` schemas (still queryable by name) and Test counts only the connection's database.
- A MySQL 8 account that uses `caching_sha2_password` (the default) is refused over an unencrypted link ("Authentication requires secure connection"): use `ssl_mode: required` (or `preferred`), not `disabled`. Certificate and key file options (`SSL_CA`, `SSL_CERT`, ...) are not offered: they are file paths.
- DuckDB's MySQL errors echo `user="..." password="***" host="..."`; Vibe redacts the password everywhere, hides the user from the agent and treats `Access denied for user` as *needs-credentials*.
- **`READ_ONLY` is a client-side guard**, like Postgres. `mysql_query`, `mysql_execute` and `mysql_clear_cache` run raw statements on the server, so they are denied to the agent in every tier ([agent-policy.md](agent-policy.md#never-in-any-tier)); a **read-only MySQL account** is the real guard. Vibe itself calls `CALL mysql_clear_cache()` when you refresh the catalog.
- The extension also lists the server's own schemas. The agent may not read `mysql.*` (account data and password hashes), `performance_schema.*`, `sys.*` or the server's `information_schema.*` in a MySQL catalog ([agent-policy.md](agent-policy.md#never-in-any-tier)); DuckDB's own `information_schema` is unaffected. Your own SQL may.
- Every agent write needs the person's OK (MySQL is not a local file), so headless MCP refuses them.

### `motherduck`

| Field | Notes |
|---|---|
| `database` | required; one database is one catalog |
| `token` | per person, write-only. A read-scaling token makes the connection read-only |

MotherDuck has no `CREATE SECRET` on DuckDB 1.4.1. The token is `SET motherduck_token` on the hub **when the workspace opens**, after `LOAD motherduck` and before the configuration is locked, and the attach is `ATTACH 'md:<database>?attach_mode=single' AS x`. Consequences:

- Sign in with the keychain, `env:MOTHERDUCK` (reads `MOTHERDUCK_TOKEN`) or prompt. An **ambient `MOTHERDUCK_TOKEN` is removed from the process environment** when a MotherDuck workspace opens, so a stray token is never picked up silently; only an explicit `env:MOTHERDUCK` sign-in uses it.
- `md:` is **never attached without a token** (it would open a browser SSO flow, block and write to stdout, which corrupts `vibe mcp serve`). No token means `needs-credentials`.
- **One MotherDuck account per workspace.** A second MotherDuck connection signed in with a different account is reported as an issue and not registered. A token that changes, or an extension installed after the workspace opened, means the workspace must be **reopened** (`Session.RelaunchRequired()`; Studio offers the relaunch).
- **A MotherDuck connection cannot be read-only** (review #115, D-2): `READ_ONLY` is not supported on `md:` attaches, so `read_only: true` is rejected with "MotherDuck connections can't be attached read-only; use a read-scaling token for read-only access", and the default for `motherduck` is `read_only: false`. Use a **read-scaling token** for read-only access. Agent writes still need approval. A command that opens the workspace read-only (`vibe conn list`) registers the connection but cannot connect it.
- **Unverified until run with a real token:** `attach_mode=single` on a running non-MotherDuck hub. `TestMotherDuckAttachModeSingleAttachesExactlyOneCatalog` asserts that exactly one catalog is added; it skips without `MOTHERDUCK_TOKEN` and `MOTHERDUCK_TEST_DATABASE`.

### `sqlite`

| Field | Notes |
|---|---|
| `path` | A SQLite file; same containment rules as `duckdb` |

`ATTACH 'path' AS x (TYPE sqlite[, READ_ONLY])`; a missing file is an error, never a silently created database.

### `s3`, `gcs`, `r2`

Object storage. These have **no catalog**: connecting creates a scoped temporary secret, and you read data with `read_parquet('<scope>...')`. The **scope** is the storage boundary: the agent may read a URL only when it is a plain literal that starts with the scope of a connection that is ready (`ok` or `not-connected`), never `https://` or another bucket ([agent-policy.md](agent-policy.md#files-and-urls)).

| Field | Notes |
|---|---|
| `scope` | required URL prefix: `s3://bucket/path/`, `gcs://` or `gs://`, `r2://` |
| `region` | s3 only; default `us-east-1` |
| `endpoint`, `url_style`, `use_ssl` (options) | s3, for S3-compatible stores |
| `account_id` (option) | r2, required |
| `key_id` | per person, readable |
| `secret` | per person, write-only |
| `token` | s3 session token, optional, write-only |

Secrets for these connections are created when the workspace opens (and replaced by `Session.Preflight` after a credential change, dropped when it is cleared): creating a secret dials nothing, so a `read_parquet('s3://...')` inside the scope works on first use. Test lists up to 1000 objects under the scope, which needs the network and respects the 10 second limit; a 403 (wrong keys) reads *needs-credentials*.

### `azure`

Azure Blob Storage and Data Lake Storage Gen2. Like `s3` it has **no catalog**: connecting creates a scoped temporary secret and you read data with `read_parquet('az://container/path/file.parquet')`. The extension **reads** Azure; DuckDB 1.4.1 does not write to it (`COPY ... TO 'az://...'` fails), so the connection is read-only whatever the flag says.

| Field | Notes |
|---|---|
| `scope` | required URL prefix: `az://container/path/`, `azure://container/path/` or `abfss://container@account.dfs.core.windows.net/path/`. No query string or SAS token |
| `sign_in` (option) | `account_key` (default), `connection_string` or `service_principal` |
| `account_name` (option) | the storage account (3 to 24 lowercase letters and digits); required for `account_key` and `service_principal` |
| `tenant_id` (option) | required for `service_principal` |
| `key_id` | per person, readable: the service principal's client id |
| `secret` | per person, write-only: the account key, the connection string or the client secret, matching `sign_in` |

- `account_key` builds `DefaultEndpointsProtocol=https;AccountName=...;AccountKey=...;EndpointSuffix=core.windows.net` (the key must be base64, so it cannot add a field) and creates `CREATE TEMPORARY SECRET (TYPE azure, CONNECTION_STRING '...', SCOPE '...')`. `connection_string` passes a pasted string through, but its `AccountName` must be the account the scope and `account_name` say (use it for a sovereign cloud or the Azurite emulator, which needs a `BlobEndpoint`). `service_principal` is `PROVIDER service_principal, TENANT_ID, CLIENT_ID, CLIENT_SECRET, ACCOUNT_NAME, SCOPE`.
- **`credential_chain` (ambient Azure credentials: `az login`, environment variables, managed identity) is deliberately not offered**: it would use whatever the process happens to be signed in as instead of the credentials you entered, and the agent could read with them. SAS tokens are not offered either (a token in a URL would sit in the scope, which is shown, logged and shared).
- Every secret-bearing value (the connection string, the key, the client secret) is flagged secret and never appears in an `ATTACH`, `duckdb_secrets()` output (`redacted`), an error or a log. A rejected key (`AuthenticationFailed`, `AuthorizationFailure`, `AADSTS...`) reads *needs-credentials*.
- Like the other storage types, the secret is made when the workspace opens (and replaced when the credential changes), so a read inside the scope works on first use, and Test lists up to 1000 objects under it. The agent may read a URL only when it starts with the scope exactly as written: `az://events/2024/` does not admit `azure://events/2024/` or `az://events/2023/`.
- **The scope host must be the account's own endpoint**, because DuckDB sends the account credential to whatever host a secret's scope names. `abfss://container@<account>.dfs.core.windows.net/path` (the host must end exactly `.dfs.core.windows.net`), `az://container/path` and `azure://container/path` (no host: the secret's account), or the fully qualified `az://<account>.blob.core.windows.net/container/path` are accepted; `abfss://c@victim.dfs.evil.com/`, `az://evil.com/c/`, an IP or `localhost` host, `.dfs.` hosts in an `az://` URL and `.blob.` hosts in an `abfss://` URL are refused when the connection is saved. When `account_name` is set it must equal the account the scope names, and the account name (3 to 24 lowercase letters and digits) and a base64 account key cannot add `;AccountKey=...` or `;EndpointSuffix=...` to the built connection string. Sovereign clouds (other endpoint suffixes) are not accepted: use the short `az://container/` form with a `connection_string` that names the endpoint.

### `ducklake`

A [DuckLake](https://ducklake.select): a lakehouse whose rows are Parquet files and whose catalog (the metadata) is a database. It attaches as a catalog, so `lake.main.events` works in the rail, notebooks and joins.

**Only you can add one (own scopes only).** A DuckLake catalog names every file it reads, so a crafted catalog can read any file on this computer. `ducklake`, `iceberg` and `delta` definitions are therefore accepted only in **This computer** (`connections.yaml` outside the folder) and **All my workspaces**: adding one to the team's `vibe.yaml` or moving it there is refused ("... trust the metadata of what they point at ..., so only you can add them"), and a `.vibe` bundle import drops them with a warning (export leaves them out too). One that arrives in `vibe.yaml` by a pull (hand-edited) does not stop the workspace opening: it is skipped with an issue ("ask the person who committed it to move it to their own connections") and the other connections work. The same goes for a shared definition with fields its type does not use. A definition with fields the type does not use (`scope`, `host`, `region`, ...) is refused for every type: a committed `scope: "file:///"` on a table source once let the agent read any file.

| Field | Notes |
|---|---|
| `path` | the metadata file: a DuckDB file, same containment rules as `duckdb` (relative paths stay inside the workspace; absolute ones are fine in your own scopes). Created the first time something writes |
| `data_path` (option, required) | where the Parquet files go: a folder (same containment rules, created inside the workspace when needed), **or** a URL inside the scope of an `s3`, `gcs`, `r2` or `azure` connection of this workspace |

`ATTACH 'ducklake:/abs/lake.ducklake' AS x (DATA_PATH '/abs/files/'[, READ_ONLY])`. No credential and no secret of its own; a lake in object storage uses the storage connection's secret. Notes:

- **The catalog is checked on every attach.** After the `ATTACH`, before the lake is usable, the session reads the catalog's own tables and **refuses and detaches the lake** (status *error*: "the lake's catalog (ducklake_data_file) names N file(s) by an absolute path, a URL or a path that leaves the data folder; refusing to read it") if any data file, delete file, scheduled-for-deletion file, table or schema path is absolute (`path_is_relative` false), has a `..` segment, a scheme (`://`), a leading `/` or a backslash, or if the data path stored in the catalog differs from the one configured (DuckLake refuses that itself too), or if anything **under a local data folder is a symbolic link** (a clean relative row such as `link/secret.parquet` is no safer than an absolute one when `link` leads out of the folder). Test refuses it the same way.
- **v1 scope.** The metadata catalog is a local DuckDB file. A Postgres or SQLite metadata catalog (a DSN or a second file inside the `ATTACH` string) is not offered; `postgres:` / `sqlite:` / `duckdb:` prefixes in `path` are rejected. Follow-up: a Postgres metadata catalog through a secret.
- A data URL that is not inside any storage connection's scope (the shared normaliser, `internal/storageurl`: no `..`, `%2e%2e`, `//`, query or alias tricks) is reported as an issue when the workspace opens. Test does not write: a lake whose metadata file does not exist yet tests as "a new lake" without creating it; a read-only connection to a missing file is refused.
- DuckLake attaches a second catalog, `__ducklake_metadata_<name>`, with the lake's own bookkeeping. The rail and `ListCatalogs` hide it, rows naming it never appear in what the agent reads, and the agent cannot read or write it ([agent-policy.md](agent-policy.md)).
- The type is *remote* in the session's sense: it attaches on first use and **every agent write needs the person's OK** (a write creates Parquet files and a snapshot others see). `READ_ONLY` is enforced by DuckLake itself.
- The agent may call the lake's read-only metadata functions (`ducklake_snapshots`, `ducklake_current_snapshot`, `ducklake_last_committed_snapshot`, `ducklake_table_info`); every other `ducklake_*` function (maintenance, `ducklake_list_files`, options) is refused.

### `iceberg`, `delta`

Apache Iceberg and Delta Lake **tables**. These are not catalogs: DuckDB reads one with a table function over its folder or URL, so the connection attaches nothing and holds no credential. It exists so the extension has a home in the product (the consent flow, the *Extension needed* status, loading when the workspace opens) and so Test can prove the table is readable. **Own scopes only**, like `ducklake` above.

| Field | Notes |
|---|---|
| `path` | the table's folder, or a URL (`s3://`, `gcs://`, `r2://`, `az://`, `azure://`, `abfss://`) inside the scope of a storage connection of this workspace |

```sql
-- in the query editor (your own SQL)
SELECT * FROM delta_scan('/abs/path/to/workspace/data/sales');
SELECT * FROM iceberg_scan('s3://acme-lake/warehouse/orders');    -- needs an s3 connection whose scope contains it
SELECT * FROM iceberg_snapshots('/abs/path/to/workspace/data/orders');
```

- **Only point these at tables you trust.** A table's own metadata names its data files (a Delta `_delta_log` `add.path` of `../../x` or an absolute path, an Iceberg manifest list URL, `version_name_format`, `allow_moved_paths`), so a crafted table can read any file this computer can read, and DuckDB's Iceberg reader fetches URLs while it binds. Files that came with a cloned repository can be crafted tables, so **the editor warns when the location is inside the workspace folder**.
- **The agent cannot read them.** `iceberg_scan`, `iceberg_metadata`, `iceberg_snapshots` and `delta_scan` are not on the agent's reader allow-list, in any tier, and not in notebook cells or live bindings (they re-run under the agent's policy); only your own SQL in the query editor uses them ([agent-policy.md](agent-policy.md#allowed-readers)). A table connection exports no scope, so it never widens what the agent may read.
- **Storage.** A table in a bucket is read with the secret of the `s3`, `gcs`, `r2` or `azure` connection whose scope contains it; add that connection first (the location is checked against the connection scopes when the workspace opens, with the same URL normaliser as the agent's reads).
- Read-only: these extensions are not used to write. Test reads the table's schema (a folder) or, for a URL, only reports that the storage connection reads it.
- **Not built (follow-up):** an Iceberg REST catalog (`ATTACH ... (TYPE iceberg)` with an OAuth or token secret). It needs its own secret type and endpoint rules. Iceberg has no end-to-end fixture in the test suite (the policy and path rules are tested with unit tests and a real Delta table).

## DuckDB version and extensions

- **The engine is pinned.** Vibe embeds DuckDB through `go-duckdb` (`github.com/marcboeker/go-duckdb/v2`, currently **v2.4.3 = DuckDB 1.4.1**), and that version is chosen to be one MotherDuck supports: a MotherDuck connection needs a client within the server's supported version window, so go-duckdb is only bumped together with a check against MotherDuck. `vibe workspace info` prints the running DuckDB version and the extensions installed.
- **Extensions download on consent into Vibe's own directory.** Vibe runs DuckDB with autoinstall and autoload off and `extension_directory` set to `<user cache dir>/vibe/duckdb/extensions` (`~/.cache/vibe/duckdb/extensions` on Linux, `~/Library/Caches/vibe/duckdb/extensions` on macOS). Anything in your own `~/.duckdb` is never loaded. A type that needs an extension (`postgres`, `mysql`, `httpfs`, `azure`, `sqlite`, `ducklake`, `iceberg`, `delta`, `motherduck`; `excel` for xlsx) reads `extension-missing` until you agree to the download: **Install** in Studio (*Settings > App > Extensions* or the connection's state) or `vibe conn test|connect --install`. Connecting never installs. Community extensions are off.
- **Offline use is not solved yet.** Nothing ships inside the app, so the first use of each extension needs network access to `extensions.duckdb.org`. Whether to pre-bundle a fixed offline set (and point `extension_directory` at it, with the signing changes macOS needs) is an open decision in the packaging epic (#77, [packaging.md](../packaging.md)). Until then an air-gapped machine can copy a populated extension directory from another machine running the same Vibe version.

## In Vibe Studio

Studio shows connections in a **rail** under the nav (it replaces the old Tables item; `#/tables` stays for deep links) and edits them on a **page**, not a modal.

- **The rail.** `connection > schema > table or view > column`, loaded lazily and keyboard-driven (one tab stop; arrows, Right and Left, Home and End, Enter, `*`, type-ahead, Shift+F10 for the menu, F5 refresh, `/` for the filter, Delete to remove). The workspace's own `local` database and other local files open themselves; remote connections never dial until you expand them. The schema level is hidden while a connection has only `main`. Each row carries a type tile, a padlock when read-only, a scope badge (**Team**, **This computer**, **All my workspaces**) and a status shape: hollow circle (not connected), filled circle (ok), alert (can't connect), key (needs credentials), download arrow (extension needed), padlock (in use by another program). Only a status the person can act on gets a line under the row, with the action (Fix, Add it, Sign in again, Install…, Take over). Under 1000 px the tree is a drawer behind the plug button.
- **Context menu.** Connect or Disconnect, Refresh (clears the catalog cache, and Postgres's own), New query here, Edit…, Test, Duplicate, Remove… (confirms; your database isn't touched and saved credentials are deleted). On a table: New query on this table, Open as datasheet, Copy name.
- **The editor** (`#/connections/new`, `#/connections/<name>`). Step 1 is a type grid generated from `ListConnectionTypes` (every registered type; a type the server marks `soon` would be greyed and unfocusable, but none is). Step 2 is a form generated from the type's fields: connection details, the sign-in method (**My keychain**, **Environment variables**, **Ask me each time**, per person and stored on this computer), the read-only switch (on by default, with the "use a read-only database user" advice; MotherDuck has no switch and recommends a read-scaling token) and the scope picker (**This computer** by default; **Team** explains that the file is committed to git). The name cannot change after the first save. **Test** tries the settings without saving and shows either "Connected in 0.4 seconds" or a plain-language, redacted error with the raw text under *Show details*; a failed test turns Save into **Save anyway**. Ctrl/Cmd+Enter runs Test; leaving with unsaved edits asks first.
- **Secrets are write-only.** The password, token and secret key are typed once, saved to the keychain on Save, and shown afterwards only as `•••• In your keychain · Replace · Clear`. The username and the S3 key id are readable fields. No binding returns a secret: `GetConnectionCredentialStatus` says which method is in use, the readable fields, whether a secret is set, whether it still matches the server, and for environment sign-in which variables are set (never their values). **Test never sends a stored credential to a changed server.** When the definition being tested differs from the saved one in a fingerprint field (host, port, database, path, scope, region, an option such as `sslmode`), or the saved sign-in is no longer bound to the saved definition (a pull changed it), Test uses only credentials typed in that edit, by any sign-in method (keychain, environment, prompt); with none it says "The server changed, so your saved sign-in won't be used. Enter it again to test." Editing a host in the editor likewise puts the secret field in replace state, and an environment sign-in asks before it is re-bound to the new server. **No binding re-binds an environment sign-in on its own.** The workspace layer refuses (`ErrRebindNeedsConfirm`, shown to Studio as `confirm-rebind:`) to choose `env:` for a connection whose saved sign-in was bound to a different server; only `SaveConnection{confirmRebind: true}` / `SetConnectionCredentials{confirmRebind: true}` (the UI sets it after its "Use PREFIX_* for the new server?" confirmation) or `vibe conn credentials set --confirm-rebind` go through. The binding remembers the old server, so the rail reads "server changed (a to b)" for environment sign-ins as it does for the keychain. A duplicate starts at the keychain and inherits no method. In the sign-in prompt, unticking **Remember** keeps the credential in memory for this session and does not change the sign-in method; memory credentials (that and "ask each time") are zeroed when the workspace closes, is switched away from or released. After any credential or sign-in change Studio calls `Session.Preflight`, which clears the 30-second failure cache, so the next use tries again.
- **States.** *Needs credentials* opens the sign-in prompt; if the definition now points at a different server (a pull changed the host) it reads "prod_pg's server changed since you signed in (db.internal to db-new.internal). Sign in again to confirm." and the saved password is not sent to the new server. *Extension missing* opens a consent dialog; nothing downloads without a yes. *In use by another program* offers Take over. A MotherDuck token or extension that arrives after the workspace opened shows **Reopen this workspace to finish connecting**; Studio does it by closing and reopening the workspace session (no app restart).
- **Run on.** The query editor's toolbar and every notebook SQL cell have a connection selector. It is per editor and per cell, never session-global: plain table names run on that connection (pinned for the run with `USE`), qualified names such as `prod_pg.public.customers` work whatever it is and can span connections. A saved query and a notebook cell store the choice with themselves. An AI-authored cell keeps the connection its SQL was previewed on; editing a cell that runs on a connection makes `preview_query` and `write_cell` default to it, and Apply keeps the cell's connection when the proposal names none.
- **Datasheet.** The datasheet has no table list of its own: the rail is the list and the datasheet fills the main pane (`#/tables?table=prod_pg.public.orders`, which connects the connection first). A table on a read-only connection, a view, or a locked file is not editable; the page says why and links to the connection.
- **Settings.** *App > Extensions* lists `postgres`, `mysql`, `httpfs`, `azure`, `sqlite`, `ducklake`, `iceberg`, `delta` and `motherduck` with what they are for, which connections here need them, and Install. *Workspace > The agent* lists the connections the agent can change and says the approval is always asked for anything that is not a local file. The approval card names every target ("Allow change to warehouse", or "Allow delete in warehouse" for deletes), shows the policy's row estimate when there is one, and starts with focus on Cancel. DuckDB cannot uninstall an extension, so Settings has Install but no Remove yet (follow-up).

## Where the type registry lives

`internal/connections` registers the types on `internal/session`'s `ConnectionType` seam; `connections.ListConnectionTypes()` returns the field metadata (name, kind, required, secret, per-person, fingerprint fields, capabilities) that Studio's form (#117) is generated from. `internal/docsref` keeps this page in step with it.
