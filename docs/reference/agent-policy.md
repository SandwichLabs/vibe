# The agent's SQL policy

> This is the policy of #116 (plan §4.5), as built. It replaced the old deny-list: the agent's SQL is an **allow-list per tier**, and the person's own SQL (the Studio editor, notebook cells you write, `vibe query`) is not covered by it. The same policy applies to Studio's agent chat, to `vibe mcp serve` and to Studio's MCP server; there is one tool surface.

The agent (Studio chat or an MCP client) reaches data through `run_query` and the record and schema tools. Every statement goes through four layers, cheapest and least side-effecting first:

1. **Statement split.** DuckDB's own `duckdb_extract_statements` is the authority on how many statements there are. The policy's lexer must agree with it, or the query is denied ("couldn't verify this query"). Every statement of a script is checked.
2. **A syntactic check before any `EXPLAIN`.** Binding has side effects: `EXPLAIN SELECT * FROM read_csv('https://x/?q=secret')` would sniff the file and send an HTTP request, so the classifier's own `EXPLAIN` could exfiltrate. A `SELECT`-shaped statement is parsed by DuckDB (`json_serialize_sql`) on a **separate, hardened, in-memory DuckDB with `enable_external_access=false`**, never the hub, and its AST is walked; every other statement is checked on its tokens. Both enforce the never-list, the table-function allow-list and the literal-path rule. A parse failure is a denial.
3. **A plan-operator allow-list** (`EXPLAIN (FORMAT JSON)` on the real connection, only after step 2): the second layer. It sees through views and macros that wrap a denied function, and refuses any operator it does not know. A plan may contain no more file/URL readers than the statement wrote out and vetted.
4. **Write gates** (below).

## What each tier allows

The tier is the person's `sandbox` setting, clamped to the team limit in `vibe.yaml` (see [workspaces.md](workspaces.md#the-agent-limit)).

| | `read-only` | `read-write` (default) | `full` |
|---|---|---|---|
| `SELECT`, `WITH`, `FROM`-first, `VALUES`, `TABLE`, `DESCRIBE`, `SHOW`, `SUMMARIZE`, `EXPLAIN` (never `ANALYZE`), allow-listed `PRAGMA` and `CALL`, over **any attached catalog** including remote ones (`pg.public.t`) | yes | yes | yes |
| Files and URLs, only as the policy's rules below allow | yes | yes | yes |
| `INSERT`, `UPDATE`, `DELETE`, `MERGE`, `INSERT ... ON CONFLICT` into a writable catalog | no | yes | yes |
| `CREATE`, `DROP`, `ALTER` of tables, views, sequences, schemas, indexes, macros, types on a writable catalog | no | no | yes |
| `COPY (...) TO '<literal path in data/>'` | no | no | yes |
| Content tools: `add_query_cell`, `create_dashboard`, `create_or_update_skill` (workspace files, not data; #111) | no | yes | yes |
| Everything in the never-list | no | no | **no** |

Record tools follow the same tiers (`create_record`, `update_record`, `delete_record`, `bulk_insert`, `import_csv` are data tools; `create_table`, `add_column`, `drop_table` are schema tools).

## Writes need three gates

A write runs only when **all three** hold:

1. **The tier allows it** (table above).
2. **Every connection it changes is writable** (`read_only: false`, and DuckDB itself reports the catalog writable). The target catalogs come from the SQL (`INSERT INTO other.orders` targets `other`; the unqualified name targets the default connection; a two-part name `x.t` is a schema of the default connection or a catalog `x`, so both are checked; `CREATE TEMP TABLE` targets `temp`; `CREATE INDEX` targets its table's catalog). A script's targets are the union over its statements. A read-only connection refuses the write in every tier, naming it, and nothing is asked.
3. **The person approves it.** "Ask before changes" is the person's setting (committed `vibe.yaml` can force it on, never off), **and it is forced on for any connection that is not a local file**: Postgres, MySQL, MotherDuck, DuckLake (it writes Parquet files and snapshots others see) and anything else other than the workspace's own `duckdb`/`sqlite` files. That holds whatever the person's loosening.

There is no approver on headless `vibe mcp serve`, so it **refuses writes to non-local connections** with this message (and nothing is changed):

> run_query changes pg, which is not a local file in this workspace, so it always needs your OK in Vibe Studio (even when "Ask before changes" is off), and there is no one to ask over MCP. Nothing was changed. Open the workspace in Vibe Studio: with its MCP server on, `vibe mcp serve` passes through to it and Studio shows you an approval card, or make the change from Studio's agent chat.

Through Studio (chat, or MCP proxied to Studio) the approval card says which non-local connections the statement changes. The policy is checked again after you approve, in case the tier or the connection changed while the card was open.

## Files and URLs

A file or URL is read only when it is written out as a **plain string literal** in a reader, and:

- **File**: an absolute path, no `..`, that after resolving symlinks lies **inside the workspace's `data/` folder**. Relative paths are refused (DuckDB resolves them against the process's working directory, which is not the workspace); the error names the `data/` folder's absolute path, and so does `list_connections`. A glob (`data/*.csv`) is accepted when its literal directory prefix is inside `data/` and no symlink under it points out of `data/`. A symlink inside `data/` that points out is refused.
- **URL**: only a URL that starts with the **scope** of a **ready storage connection** (s3, gcs, r2, azure; status `ok` or `not-connected`, not one that needs credentials or failed). One shared normaliser (`internal/storageurl`, also used when a workspace is opened and when a connection is saved) decides what a URL is: a known storage scheme (`s3://`, `s3a://`, `s3n://`, `gcs://`, `gs://`, `r2://`, `az://`, `azure://`, `abfss://`) written in lower case, with **no query string or fragment, no percent-escape (so `%2e%2e` is no way round `..`), no backslash, space or control character, no glob character other than a `*` in a read (`[` `]` `{` `}` `!` are refused: DuckDB would read `a[1].db` as `a1.db`), no `.` / `..` segment and no empty segment (`//`)**. The match is then a literal, case-sensitive prefix. Every scope is treated as a folder and gets a trailing `/` (`s3://bucket` never admits `s3://bucket-evil/`, and `az://events/2024` never admits `az://events/2024-evil/`). A scope whose scheme is not a storage scheme (`file:///`, `http://`, `hf://`) widens nothing, and **only a storage connection (s3, gcs, r2, azure) exports a scope at all**: a `delta`, `iceberg` or `ducklake` connection with a `scope` in its definition is refused when it is saved and exports nothing if it got in by hand-editing. `https://`, `http://`, `file://`, `hf://` and the rest are never readable. Aliases are never guessed at: `gs://` does not match a `gcs://` scope, and for Azure the scheme must be the one the scope was written with (`az://`, `azure://` and `abfss://container@account.dfs.core.windows.net/` are three different strings to the policy).
- **Not a literal**: `getvariable(...)`, concatenation (`'a' || 'b'`), casts, columns, lists (`['a','b']`), parameters, `E'...'`, `$$...$$`, `U&'...'` and adjacent literals (`'a' 'b'`, which SQL concatenates) are all refused as paths.
- The bare forms are readers too: `FROM 'file.csv'`, `DESCRIBE 'file.csv'`, `SUMMARIZE 'file.csv'`, and `FROM s.csv` (DuckDB joins the parts into a file name) get the same rule. A name that is an existing table is a table.
- Reads inside views and macros are held to the same rule: a view or macro whose body reads a file outside `data/`, calls a denied function, or passes a parameter into a reader is refused when the agent uses it (the plan layer also counts readers).
- `COPY ... TO` may write only an absolute, glob-free path inside `data/` whose parent exists, and never a database, WAL or extension file (`.duckdb`, `.ducklake`, `.db`, `.wal`, `.sqlite`, `.duckdb_extension`, ...), nor a symlink, nor anything in a connection's own files: the catalog file of a DuckLake and its whole data folder, and any duckdb or sqlite connection file, whatever it is called. `COPY ... FROM` (a file, or `FROM DATABASE`) is refused: load a file with `INSERT INTO t SELECT * FROM read_csv('<path in data/>')`.

### Allowed readers

`read_csv`, `read_csv_auto`, `read_parquet`, `parquet_scan`, `parquet_metadata`, `parquet_schema`, `parquet_file_metadata`, `parquet_kv_metadata`, `read_json`, `read_json_auto`, `read_ndjson`, `read_ndjson_auto`, `read_json_objects`, `read_json_objects_auto`, `read_ndjson_objects`, `read_text`, `read_blob`, `glob`.

**Reader options.** Only an explicit list of parsing options may follow the path: `header`, `delim`, `delimiter`, `sep`, `quote`, `escape`, `columns`, `union_by_name`, `auto_detect`, `all_varchar`, `skip`, `null_padding`, `ignore_errors`, `sample_size`, `dateformat`, `timestampformat`, `types`, `dtypes`, `names`, `normalize_names`, `filename`, `hive_partitioning`, `hive_types`, `hive_types_autocast`, `format`, `records`, `maximum_object_size`, `compression`, `newline`, `comment`, `decimal_separator`, `encoding`, `binary_as_string`, `file_row_number`, `parallel`, `auto_type_candidates`, `nullstr`, `strict_mode`, `max_line_size`, `buffer_size`, `maximum_depth`, `field_appearance_threshold`, `map_inference_threshold`, `convert_strings_to_integers`, `thousands`. Any other (`rejects_table`, `store_rejects`, `version_name_format`, `allow_moved_paths`, ...) is refused ("not on the agent's list of safe reader options"), in both `name = value` and `name := value` forms.

**Iceberg and Delta are not agent readers (#119 review).** `iceberg_scan`, `iceberg_metadata`, `iceberg_snapshots` and `delta_scan` are refused in every tier, by the syntactic layer, before any `EXPLAIN` binds the statement (the iceberg readers fetched URLs during binding). The metadata of such a table chooses which files are read (`_delta_log` `add.path` with `../` or an absolute path, a `file://` or `http://` URL, Iceberg's `version_name_format` and `allow_moved_paths`, a manifest list URL), so no check on the literal path can confine them. They stay available to **you**, in your own SQL in the query editor (not in notebook cells or live bindings, which re-run under this policy), through an `iceberg` or `delta` connection you defined. `COPY ... TO` still refuses a path under a `_delta_log` folder, a file ending `metadata.json` or `.avro`, and `version-hint.text` (defence in depth: the agent can write plain files with `COPY ... (QUOTE '', ESCAPE '')`, which is why the readers are not allowed at all).

Not allowed (no path rule can make them safe here): `sniff_csv`, `parquet_bloom_probe`, `read_xlsx`, `st_read`, `iceberg_*`, `delta_scan`, `sqlite_*`, `mysql_*`, `postgres_*`.

## Allowed table functions (besides the readers)

`range`, `generate_series`, `unnest`, `generate_subscripts`, `duckdb_tables`, `duckdb_columns`, `duckdb_views`, `duckdb_schemas`, `duckdb_constraints`, `duckdb_indexes`, `duckdb_sequences`, `duckdb_keywords`, `duckdb_functions`, `duckdb_types`, `pragma_table_info`, and DuckLake's read-only metadata functions (#119) `ducklake_snapshots`, `ducklake_current_snapshot`, `ducklake_last_committed_snapshot`, `ducklake_table_info`. Any other table function is refused ("not on the agent's allow-list"), including every extension's: the rest of `ducklake_*` (`ducklake_set_option`, `_expire_snapshots`, `_merge_adjacent_files`, `_rewrite_data_files`, `_cleanup_old_files`, `_delete_orphaned_files`, `_add_data_files`, `_flush_inlined_data`, `_set_commit_message`, `_options`, `_list_files`, `_table_insertions` / `_deletions` / `_changes`) changes the lake or shows where its files live, and a catalog-qualified form (`lake.snapshots()`, `CALL lake.set_option(...)`) is refused too. A table macro an owner defined is allowed when its body passes the same rules.

## Allowed `CALL`

`CALL` takes literal arguments only and only these: `duckdb_tables`, `duckdb_columns`, `duckdb_views`, `duckdb_schemas`, `duckdb_constraints`, `duckdb_indexes`, `duckdb_sequences`, `duckdb_keywords`, `duckdb_types`, `pragma_table_info`. `CALL read_text(...)`, `CALL glob(...)`, `CALL postgres_execute(...)`, `CALL pg_clear_cache()` (Vibe refreshes remote catalogs itself) and the rest are refused.

## Allowed `PRAGMA`

`table_info`, `show_tables`, `show_tables_expanded`, `database_size`, `storage_info`, `version`, `platform` (literal arguments only). `database_list` is not on the list: it exposes file paths.

## Never, in any tier

Refused in `full` too, and no approval can lift them:

- `ATTACH`, `DETACH`, `USE` (connections come from the workspace; name the connection in the table instead).
- Secrets: `CREATE`/`DROP SECRET`, `duckdb_secrets()`, `which_secret()`, `duckdb_secret_types()`, anything that names a secret (the #114 rule, now also enforced on the AST).
- `SET`, `RESET`, `PRAGMA` writes. **`SET VARIABLE` is refused entirely** (spike finding D: it runs subqueries and table functions, so a constant-only form was not worth the surface; `getvariable()` stays callable but can never name a path).
- `INSTALL`, `LOAD`, `FORCE INSTALL`, `UPDATE EXTENSIONS`, with or without a path.
- `EXPORT DATABASE`, `IMPORT DATABASE`, `COPY FROM DATABASE`, `COPY ... FROM`.
- `EXPLAIN ANALYZE` (it executes the statement; `EXPLAIN` of a write is refused too), `PREPARE`/`EXECUTE`, transactions, `CHECKPOINT`, `VACUUM`, `ANALYZE`, `TRUNCATE`, `COMMENT ON`, `GRANT`/`REVOKE`.
- `CREATE`/`DROP`/`ALTER DATABASE`, `CREATE SHARE`, `CREATE EXTENSION`, and any DDL kind not listed above.
- Raw passthroughs: `postgres_query`, `postgres_execute`, `postgres_scan` (inline DSN), `postgres_attach`, `postgres_scan_pushdown`, `mysql_*` (`mysql_query`, `mysql_execute`, `mysql_clear_cache`, `mysql_scan`, `mysql_attach`: the mysql extension adds exactly the first three, checked against `duckdb_functions()` in the integration test), `sqlite_*`. `postgres_execute` escapes `READ_ONLY` with a multi-statement payload (spike #107), and the mysql ones run raw SQL on the server, so these are refused outright.
- DuckLake's own bookkeeping: the catalog `__ducklake_metadata_<name>` that DuckLake attaches beside every lake (DuckDB or SQLite kind) is neither readable nor writable (it names every data file, and a write corrupts the lake), and rows naming it never appear in a result (`duckdb_tables()`, `SHOW ALL TABLES`, ...). This hides the lake's tables, not the catalog *file*: a catalog in object storage is readable with `read_blob` / `read_text` exactly where a storage connection's scope you chose covers its URL (the lake adds no scope), and `sqlite_scan` of a SQLite catalog file is refused (not on the allow-list).
- The MySQL server's own schemas in a MySQL catalog: `shop.mysql.user` (password hashes), `shop.performance_schema.*`, `shop.sys.*` and the server's `shop.information_schema.*`. DuckDB's own `information_schema` is unaffected. Query the connection's own database.
- `query()` and `query_table()` unless their argument is one plain string literal; the SQL or name it carries is then checked as a read. `json_execute_serialized_sql()` always.
- MotherDuck sharing and administration: every function named `md_*`, `__md_*`, `motherduck_*`, `non_md_*`, `get_md_token`, `print_md_token`, and the AI functions `prompt*` and `embedding` (they send data to a remote service). Checked against `duckdb_functions()` after `LOAD motherduck`: the extension adds `MD_*` table and scalar functions, `GET_MD_TOKEN`, `PRINT_MD_TOKEN`, `PROMPT*`, `EMBEDDING` and internal `__MD_*` ones.
- Settings and the environment: `duckdb_settings()`, `current_setting()`, `getenv()`, `duckdb_extensions()`, `duckdb_variables()`, `duckdb_logs`, `duckdb_temporary_files()`, `duckdb_external_file_cache()`, `sql_auto_complete()`, the other internal `duckdb_*` state functions, and any function starting `__`.
- `duckdb_databases()` and `pragma_database_list()` (they expose file paths): use `list_connections`. `SHOW DATABASES` is refused for the same reason.
- `nextval()` / `setval()` in a read (they change a sequence); `INSERT ... DEFAULT nextval(...)` is a write.
- `SUMMARIZE` over a remote catalog (it copies the whole table out of the server); `DESCRIBE` is catalog-only and allowed.

## The string net

For statements the policy checks on tokens (`INSERT`/`UPDATE`/`DELETE`/`MERGE`, `CREATE`/`DROP`/`ALTER`, `COPY`, and view and macro bodies), every string literal and quoted identifier must, before any `EXPLAIN` binds the statement, be either a file or URL the policy already accepted as a reader path, or **inert**: no scheme (`://`, `s3:/`, a drive letter), not starting with `/`, `~`, `./`, `../` or `\\`, and not ending in a data-file extension (`.csv`, `.parquet`, `.json`, `.db`, ...). Otherwise the statement is refused ("a file or URL here can't be checked"). It does not depend on knowing every place SQL can name a table source, so an unknown form cannot make the classifier's own `EXPLAIN` read a file or send a request.

It is blunt on purpose, so **inserting or updating path- or URL-looking values (`'/etc/passwd'`, `'report.csv'`) is refused**: that needs the user, or a reader on a path inside `data/`. `'N/A'` and `'a/b'` are fine.

It does **not** apply to read-only (`SELECT`-shaped) queries: there the parsed AST is authoritative, a string constant is never a table source, and every source form (`FROM`, `JOIN`, `PIVOT`, `UNPIVOT`, `TABLE`, replacement scans, table functions) is a node that is vetted. `WHERE path = '/home/x/a.csv'`, `LIKE 'https://%'`, `LIKE '%.csv'` and `regexp_extract(path, '/([^/]+)$', 1)` all work.

**`PIVOT`/`UNPIVOT` as a statement** (`PIVOT t ON a USING sum(b)`, `CREATE TABLE x AS PIVOT ...`) is refused with a reason: DuckDB expands it into several statements. Use `SELECT * FROM t PIVOT (sum(b) FOR a IN (...))`, which works.

## `list_connections`

An introspect tool, visible in every tier, over MCP too: for each registered connection its `name`, `type`, `status`, `read_only`, `location` (`local` or `remote`), `catalog` (false for a storage connection) and, for storage, its `scope` (a `delta` or `iceberg` table connection has none: it widens nothing); plus the default connection and the absolute path of the workspace's `data/` folder. It never returns a host, a DSN, a credential or a path outside the workspace. `introspect_schema(connection?)` and `describe_table` take catalog-qualified names (`catalog.schema.table`); an agent-authored notebook cell (`add_query_cell`, `write_cell` after `preview_query`) may carry a `connection`, validated against the registered names and classified on that connection.

## Limits worth knowing

- **A script whose later statements depend on earlier ones** (`CREATE TABLE a (...); INSERT INTO a ...`) cannot be verified, because each statement is checked against the database as it is before the script runs. Send them as separate calls.
- **The `PIVOT` statement** (as opposed to `FROM t PIVOT (... IN (...))`) expands into several statements inside DuckDB and is refused.
- **Unqualified or schema-only names** cost one listing of the catalogs (two round trips for an attached Postgres); a fully qualified name in a Postgres catalog does not.
- **Files can change between the check and the read.** A symlink swapped into `data/` in that window is not caught; the folder is yours.
- **`read_only` is a client-side guard.** Use a read-only database role for anything the agent must not change; the policy refuses `postgres_execute`, but a role that can write can be reached by other means.
- Remote scan and write plan operators are matched by name pattern (`*_SCAN` over a table, `PG_INSERT`, `MYSQL_INSERT`, `DUCKLAKE_UPDATE`, ...). MySQL's (`MYSQL_SCAN`, `MYSQL_INSERT`, `MYSQL_CREATE_TABLE_AS`) and DuckLake's (`DUCKLAKE_SCAN`, `DUCKLAKE_INSERT`, `DUCKLAKE_UPDATE`, `DUCKLAKE_DELETE`, and `DUCKLAKE_INLINE_DATA`, the child DuckLake 1.0 adds under an insert or CREATE TABLE AS) were re-taken from a real server and a real lake on DuckDB 1.5.6 (same names as on 1.4.1 apart from `DUCKLAKE_INLINE_DATA`). MotherDuck's were taken from the extension's 1.4.1 binary and not re-checked live on 1.5.6 (`DOWNLOAD_BRIDGE`, `UPLOAD_SOURCE`, ...). With an `md:` catalog attached, hybrid execution marks **every** operator of every plan with where it runs (`HASH_GROUP_BY (L)` here, `SEQ_SCAN (R)` on MotherDuck's server); the marker is stripped before the name is checked (#111: before that, every read was refused while MotherDuck was attached, `count(*)` over a `data/` CSV included). A read that this layer refuses names the operator in the error.
- A three-part name whose first part is not a connection (`newsroom.raw.t`) is refused as "no connection named ...", and a four-part name (`motherduck.newsroom.raw.t`) as "a table name has at most three parts". A MotherDuck connection attaches one database; another database of the same account needs its own connection (same token).

## Where it lives

`internal/agents`: `sql_policy.go` (the analyzer), `sql_classify.go` (parser connections, catalog, plan check, tier gate), `sql_paths.go` (path and scope rules), `sql_names.go` (the closed lists), `sql_lex.go` (lexer), `policy_handler.go` (gates, `list_connections`). The name lists above are exported (`agents.PolicyReaderFunctions()` and friends) and `internal/docsref` fails when this page leaves one out.
