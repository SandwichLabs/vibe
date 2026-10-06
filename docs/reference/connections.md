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

## Where the type registry lives

`internal/connections` registers the types on `internal/session`'s `ConnectionType` seam; `connections.ListConnectionTypes()` returns the field metadata (name, kind, required, secret, per-person, fingerprint fields, capabilities) that Studio's form (#117) is generated from. `internal/docsref` keeps this page in step with it.
