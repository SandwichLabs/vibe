# Connection recipes

A connection is a place your data lives. Vibe attaches each one into the workspace's embedded DuckDB the first time something uses it, so you can query `connection_name.schema.table` and join across them. This page is the how-to; the complete field list, status meanings and security notes are in the [connections reference](reference/connections.md).

## How connections work

- **Add one** in Studio (the **+** in the connections rail, "New connection": pick a type, fill the form, **Test**, **Save**) or on the command line with `vibe conn add NAME --type TYPE ...`.
- **Read-only by default.** Add `--read-only=false` (or switch it off in the editor) to allow writes. The read-only flag is a guard on Vibe's side; for a real guarantee use a read-only database role. MotherDuck has no read-only attach.
- **Scope** is where the definition is kept: `local` (default; this computer, this workspace), `shared` (in the committed `vibe.yaml`; teammates get the definition and enter their own credentials) or `user` (all your workspaces). Use `--scope`. DuckLake, Iceberg and Delta can only be defined in your own scopes.
- **Credentials never go in the folder.** They go into your OS keychain, or are read from environment variables, or are asked for each time. Nothing secret is ever typed on a command line.
- **Check it** with `vibe conn test NAME` (a throwaway session, 10 second limit) and `vibe conn list` (status per connection).
- **DuckDB extensions** (postgres, mysql, httpfs, ...) are downloaded once, only when you ask for them (Install, `--install`; Excel import and export fetch `excel` the first time). A new connection reads `extension-missing` until you add `--install` to `vibe conn test` or `vibe conn connect`, or press **Install** in Studio. See [Privacy](privacy.md) and [troubleshooting](troubleshooting.md#extension-download-blocked-or-offline).

### Sign-in methods

| Method | `--sign-in` | Where the secret lives |
|---|---|---|
| Keychain (default) | `keychain` | Your OS keychain (macOS Keychain, or a Secret Service such as GNOME Keyring or KWallet). On a terminal, `vibe conn add` prompts and stores it. Without a terminal: `vibe conn credentials set NAME` reads it from stdin. |
| Environment | `env:PREFIX` | Read-only from variables `PREFIX_USER`, `PREFIX_PASSWORD`, `PREFIX_TOKEN`, `PREFIX_KEY_ID`, `PREFIX_SECRET`. `env:MOTHERDUCK` is special: it reads `MOTHERDUCK_TOKEN`. Good for CI, servers and machines with no keychain. You can keep them in a workspace `.env` after you allow it (`vibe workspace env allow`). |
| Prompt | `prompt` | Asked each time; held in memory until Vibe quits. |

Manage saved credentials with `vibe conn credentials set|clear|status NAME`. They are write-only: Vibe never shows a stored secret. Full mapping and the `.env` rules: [workspaces reference, credentials](reference/workspaces.md#credentials).

## Local DuckDB, CSV and Parquet

Every workspace starts with a `local` DuckDB connection (`data/local.duckdb`). Files need no connection at all.

```sh
vibe import data/orders.csv --into orders          # CSV, TSV or xlsx into a table
vibe query "SELECT * FROM read_parquet('data/events.parquet') LIMIT 10"
vibe export orders -o data/orders.parquet          # csv, parquet, json or xlsx
```

Another DuckDB file:

```sh
vibe conn add archive --type duckdb --path /path/to/archive.duckdb
```

In your own SQL (Studio's editor, `vibe query`) any readable path works. The agent can read files only inside the workspace's `data/` folder. See [agent policy](reference/agent-policy.md#files-and-urls).

## SQLite

```sh
vibe conn add legacy --type sqlite --path data/legacy.sqlite
```

No credentials. A path inside the workspace can be `shared`; an absolute path only in your own scopes.

## MotherDuck

```sh
export MOTHERDUCK_TOKEN=...        # from app.motherduck.com
vibe conn add md --type motherduck --database my_db --sign-in env:MOTHERDUCK
vibe conn test md --install
```

Or leave out `--sign-in` on a terminal and paste the token when asked, to keep it in the keychain. In Studio the editor has a database picker once the token is entered. Things to know:

- A named database attaches as one catalog: `md.main.table`.
- To reach several databases of the account, add one connection per database with the same sign-in (`vibe conn add newsroom --type motherduck --database newsroom --sign-in env:MOTHERDUCK`); each is its own catalog (`newsroom.raw.table`). A four-part name such as `md.newsroom.raw.table` does not work in DuckDB.
- It cannot be attached read-only. For read-only access use a MotherDuck read-scaling token.
- A stray `MOTHERDUCK_TOKEN` in your environment is **not** used unless the connection's sign-in is `env:MOTHERDUCK`.
- One MotherDuck account per workspace.

## Postgres

```sh
vibe conn add prod_pg --type postgres --host db.internal --database analytics
# prompts for the user and password and keeps them in the keychain
vibe conn test prod_pg --install
vibe query "SELECT count(*) FROM prod_pg.public.orders"
```

Use a **read-only database role**: Vibe's read-only flag stops ordinary writes but is not a database permission. For TLS add `--option sslmode=require` (`disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full`). For CI or servers: `--sign-in env:PGPROD` with `PGPROD_USER` and `PGPROD_PASSWORD`.

## MySQL and MariaDB

```sh
vibe conn add shop --type mysql --host db.internal --database shop --option ssl_mode=required
```

MySQL 8's default login needs an encrypted connection, so use `ssl_mode=required` (or `preferred`). Same read-only-role advice as Postgres.

## S3, Google Cloud Storage and Cloudflare R2

```sh
vibe conn add lake --type s3 --prefix s3://acme-lake/raw/ --region us-east-1
vibe conn add r2data --type r2 --prefix r2://bucket/path/ --option account_id=<32 hex chars>
vibe conn add gcsdata --type gcs --prefix gcs://bucket/path/
```

The credential is an access key id and secret (prompted, or `--sign-in env:PREFIX` with `PREFIX_KEY_ID` and `PREFIX_SECRET`; `PREFIX_TOKEN` for an S3 session token). For an S3-compatible store add `--option endpoint=...` (and, if needed, `url_style`). Once connected, query files by URL under the prefix:

```sql
SELECT * FROM read_parquet('s3://acme-lake/raw/2026/*.parquet') LIMIT 10;
```

## Azure Blob / ADLS

```sh
vibe conn add events --type azure --prefix az://events/2024/ \
  --option sign_in=account_key --option account_name=acmelake
```

`sign_in` is `account_key` (default), `connection_string` or `service_principal` (also needs `--option tenant_id=...`). The secret is prompted or comes from `env:PREFIX`.

## DuckLake, Iceberg and Delta

```sh
vibe conn add wh --type ducklake --path data/wh.ducklake --option data_path=data/wh_files --read-only=false
vibe conn add sales --type delta --path data/sales
vibe conn add events_ice --type iceberg --path s3://acme-lake/warehouse/events/
```

- Only in your own scopes (the default `local` is fine); never in a committed `vibe.yaml`.
- A `path` or `data_path` that is a URL must sit inside the scope of a storage connection you have already added (S3, GCS, R2 or Azure), which supplies the credential.
- The extension (`ducklake`, `iceberg`, `delta`) downloads once when you press Install or pass `--install`.
- The agent cannot read Iceberg or Delta tables yet; you can in the SQL editor.

## Sharing connections with a team

Add with `--scope shared` to write the definition into `vibe.yaml`, commit it, and each teammate enters their own credentials. Move one later with `vibe conn move NAME --to user|shared|local`. Details: [workspaces reference](reference/workspaces.md).
