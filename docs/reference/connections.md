# Connections

A connection is a place data lives. Opening a workspace dials nothing remote: a connection reads **not connected** until something uses it (a query that names it, `vibe conn connect`, expanding it in Studio). Local `duckdb` and `sqlite` files attach when the workspace opens; everything else attaches on first use.

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

## In Vibe Studio

Studio shows connections in a **rail** under the nav (it replaces the old Tables item; `#/tables` stays for deep links) and edits them on a **page**, not a modal.

- **The rail.** `connection > schema > table or view > column`, loaded lazily and keyboard-driven (one tab stop; arrows, Right and Left, Home and End, Enter, `*`, type-ahead, Shift+F10 for the menu, F5 refresh, `/` for the filter, Delete to remove). The workspace's own `local` database and other local files open themselves; remote connections never dial until you expand them. The schema level is hidden while a connection has only `main`. Each row carries a type tile, a padlock when read-only, a scope badge (**Team**, **This computer**, **All my workspaces**) and a status shape: hollow circle (not connected), filled circle (ok), alert (can't connect), key (needs credentials), download arrow (extension needed), padlock (in use by another program). Only a status the person can act on gets a line under the row, with the action (Fix, Add it, Sign in again, Install…, Take over). Under 1000 px the tree is a drawer behind the plug button.
- **Context menu.** Connect or Disconnect, Refresh (clears the catalog cache, and Postgres's own), New query here, Edit…, Test, Duplicate, Remove… (confirms; your database isn't touched and saved credentials are deleted). On a table: New query on this table, Open as datasheet, Copy name.
- **The editor** (`#/connections/new`, `#/connections/<name>`). Step 1 is a type grid generated from `ListConnectionTypes` (MySQL and DuckLake are greyed "coming soon" until #119). Step 2 is a form generated from the type's fields: connection details, the sign-in method (**My keychain**, **Environment variables**, **Ask me each time**, per person and stored on this computer), the read-only switch (on by default, with the "use a read-only database user" advice; MotherDuck has no switch and recommends a read-scaling token) and the scope picker (**This computer** by default; **Team** explains that the file is committed to git). The name cannot change after the first save. **Test** tries the settings without saving and shows either "Connected in 0.4 seconds" or a plain-language, redacted error with the raw text under *Show details*; a failed test turns Save into **Save anyway**. Ctrl/Cmd+Enter runs Test; leaving with unsaved edits asks first.
- **Secrets are write-only.** The password, token and secret key are typed once, saved to the keychain on Save, and shown afterwards only as `•••• In your keychain · Replace · Clear`. The username and the S3 key id are readable fields. No binding returns a secret: `GetConnectionCredentialStatus` says which method is in use, the readable fields, whether a secret is set, whether it still matches the server, and for environment sign-in which variables are set (never their values). **Test never sends a stored credential to a changed server.** When the definition being tested differs from the saved one in a fingerprint field (host, port, database, path, scope, region, an option such as `sslmode`), or the saved sign-in is no longer bound to the saved definition (a pull changed it), Test uses only credentials typed in that edit, by any sign-in method (keychain, environment, prompt); with none it says "The server changed, so your saved sign-in won't be used. Enter it again to test." Editing a host in the editor likewise puts the secret field in replace state, and an environment sign-in asks before it is re-bound to the new server. **No binding re-binds an environment sign-in on its own.** The workspace layer refuses (`ErrRebindNeedsConfirm`, shown to Studio as `confirm-rebind:`) to choose `env:` for a connection whose saved sign-in was bound to a different server; only `SaveConnection{confirmRebind: true}` / `SetConnectionCredentials{confirmRebind: true}` (the UI sets it after its "Use PREFIX_* for the new server?" confirmation) or `vibe conn credentials set --confirm-rebind` go through. The binding remembers the old server, so the rail reads "server changed (a to b)" for environment sign-ins as it does for the keychain. A duplicate starts at the keychain and inherits no method. In the sign-in prompt, unticking **Remember** keeps the credential in memory for this session and does not change the sign-in method; memory credentials (that and "ask each time") are zeroed when the workspace closes, is switched away from or released. After any credential or sign-in change Studio calls `Session.Preflight`, which clears the 30-second failure cache, so the next use tries again.
- **States.** *Needs credentials* opens the sign-in prompt; if the definition now points at a different server (a pull changed the host) it reads "prod_pg's server changed since you signed in (db.internal to db-new.internal). Sign in again to confirm." and the saved password is not sent to the new server. *Extension missing* opens a consent dialog; nothing downloads without a yes. *In use by another program* offers Take over. A MotherDuck token or extension that arrives after the workspace opened shows **Reopen this workspace to finish connecting**; Studio does it by closing and reopening the workspace session (no app restart).
- **Run on.** The query editor's toolbar and every notebook SQL cell have a connection selector. It is per editor and per cell, never session-global: plain table names run on that connection (pinned for the run with `USE`), qualified names such as `prod_pg.public.customers` work whatever it is and can span connections. A saved query and a notebook cell store the choice with themselves. An AI-authored cell keeps the connection its SQL was previewed on; editing a cell that runs on a connection makes `preview_query` and `write_cell` default to it, and Apply keeps the cell's connection when the proposal names none.
- **Datasheet.** The datasheet has no table list of its own: the rail is the list and the datasheet fills the main pane (`#/tables?table=prod_pg.public.orders`, which connects the connection first). A table on a read-only connection, a view, or a locked file is not editable; the page says why and links to the connection.
- **Settings.** *App > Extensions* lists `postgres`, `httpfs`, `sqlite` and `motherduck` with what they are for, which connections here need them, and Install. *Workspace > The agent* lists the connections the agent can change and says the approval is always asked for anything that is not a local file. The approval card names every target ("Allow change to warehouse", or "Allow delete in warehouse" for deletes), shows the policy's row estimate when there is one, and starts with focus on Cancel. DuckDB cannot uninstall an extension, so Settings has Install but no Remove yet (follow-up).

## Where the type registry lives

`internal/connections` registers the types on `internal/session`'s `ConnectionType` seam; `connections.ListConnectionTypes()` returns the field metadata (name, kind, required, secret, per-person, fingerprint fields, capabilities) that Studio's form (#117) is generated from. `internal/docsref` keeps this page in step with it.
