# Workspaces

A **workspace** is a folder, optionally a git repository, that holds your notebooks, saved queries and skills as ordinary files, plus a small `vibe.yaml` that marks it. Your data stays wherever it already lives; the workspace only describes how to reach it.

This page is the file-level reference, plus the command line. Opening, creating and switching workspaces in Studio arrives with #111; the CLI (#113), `vibe mcp serve --workspace` and the hub lock (#112) are in. Until Studio moves over it still opens `.vibe` containers, and everything below is implemented by `internal/workspace` and its stores.

## Layout

```
acme-analytics/
├── vibe.yaml                 committed: id, name, default connection, agent limit, shared connections
├── notebooks/
│   └── weekly-revenue.vnb    one notebook per file (pretty JSON)
├── queries/
│   └── top-customers.sql     SQL with a `-- vibe:` header line
├── skills/
│   └── revenue-definitions/
│       └── SKILL.md          same format as the built-in skills
├── data/                     local files and local .duckdb files
├── .gitignore                .vibe-local/, data/*.duckdb, data/*.duckdb.wal
├── .gitattributes            *.vnb text eol=lf
└── .vibe-local/              never committed; non-trust state only
    ├── hub.lock              held while a process owns the workspace, see "One owner at a time"
    ├── nonce                 this folder's mark, see "Re-cloning"
    └── state.json            open tabs, last notebook
```

Your own settings for a workspace are **not** in the folder (review #109). They live in your user config directory, next to your app preferences:

```
<config dir>/vibe/                       $VIBE_CONFIG_DIR, or the OS user config dir + /vibe
├── connections.yaml                     "All my workspaces" connections
└── workspaces/<id>-<hash>/local.yaml    this workspace on this computer: agent loosening,
                                         sign-in methods, "This computer" connections
```

`<id>` is the workspace id and `<hash>` a short hash of the folder's real path, so two clones of one repository on one machine keep separate settings. The reason is trust: a folder arrives with whatever its authors committed, and these settings can loosen the agent, redefine connections and choose how credentials are fetched, so Vibe only trusts them from a place only you write to. A `.vibe-local/connections.yaml` found inside a folder is never read; opening the workspace reports a warning that it was ignored.

**Re-cloning.** The first time a workspace is opened Vibe writes a random mark into the folder's git-ignored `.vibe-local/nonce`, and records the same value in `local.yaml` when you save settings. If you delete the folder and clone something else (a different repository that happens to carry the same `id`) to the same path, the marks differ, so everything in `local.yaml` (your loosening, sign-in methods, connections) is ignored and a warning explains why. Saving settings is refused until you confirm they belong to this folder (`TrustLocal`). A `local.yaml` with no mark (hand-written or older) is trusted and gets one the next time it is saved.

A relative `VIBE_CONFIG_DIR` is ignored with a warning, since it would mean a different folder for every launch.

`Init` creates everything above except `.vibe-local/`, which appears on the first open (it holds the folder's mark, below). It merges into an existing `.gitignore` and `.gitattributes` and never replaces them, leaves other files in the folder alone, and refuses a folder that already is, or sits inside, a workspace. `vibe.yaml` is written last, so a failed init never leaves something that looks like a workspace but is not. With git on your PATH it can also run `git init`.

## Finding a workspace

`Discover(start)` walks up from `start` to the nearest folder containing a `vibe.yaml`, the way git finds `.git`. The marker is `vibe.yaml`, not a `.vibe/` folder, so it can never be confused with a `.vibe` exchange file. A directory named `vibe.yaml` does not count, and neither does a symlinked `vibe.yaml` (it is refused when opening).

## vibe.yaml

Committed and shared. It carries no secrets and nothing per-person. Unknown keys, duplicate keys and a second YAML document are all errors, so a typo is reported instead of ignored.

<!-- keys:vibe.yaml -->
| Key | Meaning |
|---|---|
| `version` | Schema version, currently `1`. A newer number is refused. |
| `id` | A UUID generated when the workspace is created. It keys keychain entries, so clones of one repository on one machine share credentials. Required. |
| `name` | Display name. Defaults to the folder name. |
| `default_connection` | Connection that unqualified table names run against. `Init` sets it to `local`. |
| `sandbox` | The team limit for the agent: `read-only`, `read-write` or `full`. See "The agent limit". |
| `ask_before_writes` | `true` makes the approval gate mandatory for everyone. |
| `connections` | Shared connection definitions, below. |
<!-- /keys -->

```yaml
version: 1
id: 7f3c2a10-5b1e-4c52-9d0e-2a6f1b8c4d90
name: Acme Analytics
default_connection: local
sandbox: read-write
ask_before_writes: true
connections:
  - name: prod_pg
    type: postgres
    host: db.internal
    port: 5432
    database: analytics
    credentials: keychain
    options:
      sslmode: require
  - name: local
    type: duckdb
    path: data/local.duckdb
    read_only: false
```

### Connection definitions

A definition says what a connection needs, never how you sign in.

<!-- keys:connection -->
| Key | Meaning |
|---|---|
| `name` | The catalog alias. Must match `^[a-z_][a-z0-9_]{0,62}$` and cannot be `system`, `temp`, `memory` or `main`. |
| `type` | `duckdb`, `postgres`, `sqlite`, `motherduck`, `s3`, ... (lower-case). |
| `path` | A file, for file-based types. Relative paths resolve against the workspace folder. |
| `host` | Server host name. |
| `port` | Server port, 0 to 65535. |
| `database` | Database name. |
| `region` | Cloud region, for storage types. |
| `scope` | The storage prefix a storage connection covers, such as `s3://bucket/`. |
| `read_only` | Defaults to `true`. Set `false` to allow writes. |
| `credentials` | A suggestion for how to sign in: `keychain`, `prompt` or `env:PREFIX`. |
| `options` | Extra non-secret settings such as `sslmode`. |
<!-- /keys -->

- **No secrets.** A `password`, `token` or `key` field does not exist, so it is an unknown key. Option names that look like credentials are rejected, and so is any value that embeds a password (`postgres://user:pw@host`, `user:pw@host`).
- **Environment variables, only in your own scopes (review #109).** `path`, `host`, `database`, `region`, `scope` and option values may contain `${NAME}` in your own files (`local.yaml`, the user `connections.yaml`) but **never in `vibe.yaml`**, which is written by someone else: a committed `${VAR}` is a parse error, so a pull cannot read your environment. A variable that is not set is a hard error that names the connection and field, never a silent empty string; an empty but set variable is fine. A variable whose name looks like a credential (`...SECRET...`, `...TOKEN...`, `...PASSWORD...`, `...KEY...`, `AUTH...`) is refused even in your own scopes. `$$` is a literal `$`. The name, type and credentials are never expanded.
- **Paths (review #109).** In `vibe.yaml` every `path` must be relative and stay inside the workspace folder, including through symbolic links: an absolute path, a `~`, a drive letter or a `..` that leaves the folder is a parse error. Your own scopes may use an absolute path, which is your explicit choice, but Vibe only ever creates folders inside the workspace; an absolute path in your own scope whose folder does not exist is reported as an issue when the session opens. (An absolute path in `vibe.yaml` never gets that far: the file does not open.) A relative path in an "All my workspaces" connection is refused, because it belongs to no workspace.
- **Fingerprint.** A hash of type, host, port, database and scope is stored with a keychain entry. If a pull changes any of them, the connection asks you to sign in again rather than sending a saved password to a different server.

## Scopes and precedence

Connection definitions come from three files and are merged most-specific-wins.

| Scope | File | UI word |
|---|---|---|
| local | `<config dir>/vibe/workspaces/<id>-<hash>/local.yaml` | This computer |
| shared | `vibe.yaml` | Team |
| user | `<config dir>/vibe/connections.yaml` | All my workspaces |

`<config dir>` is `$VIBE_CONFIG_DIR` when set, otherwise the operating system's user config directory, the same place Vibe keeps its preferences. A missing file is an empty scope. A user file that cannot be read is skipped with a warning, so one broken file never makes every workspace unopenable.

- **Local beats shared beats user.** When two scopes define the same name, the more specific entry replaces the other whole; fields are not merged. The merged list reports the scope each connection came from and which lower scopes it shadows.
- **A user entry never overrides** a workspace one.
- **Sign-in is per person.** `sign_in` in your `local.yaml` maps a connection name to `keychain`, `prompt` or `env:PREFIX` for that person. A user-scope connection uses `sign_in` from the user file. Otherwise the definition's `credentials` suggestion applies, and then `keychain`.
- **Status precedence**, when several apply to one connection: `extension-missing`, then `needs-credentials`, then `error`, then `locked-by`, then `not connected`, then `ok`.

<!-- keys:local -->
| Key | Meaning |
|---|---|
| `version` | Schema version, currently `1`. |
| `connections` | This computer's private connections and overrides, same shape as above. |
| `sign_in` | Connection name to sign-in method, for this person. |
| `agent` | This person's agent settings: `sandbox` and `ask_before_writes`. |
| `mcp_port` | The loopback port Studio's MCP server last used for this workspace, so a client's saved address survives a restart. Not a secret; the bearer token changes on every start. |
| `nonce` | Written by Vibe. Ties the file to the folder it was made for (see below). |
<!-- /keys -->

<!-- keys:user -->
| Key | Meaning |
|---|---|
| `version` | Schema version, currently `1`. |
| `connections` | Connections available in every workspace on this computer. |
| `sign_in` | Connection name to sign-in method, for those connections. |
<!-- /keys -->

`.vibe-local/state.json` holds `lastNotebook` and `openTabs`; it carries no trust and stays in the folder. It is never a reason for anything to fail: an unreadable file is an empty state.

## The agent limit

The committed `sandbox` and `ask_before_writes` are a **ceiling, not a switch**: they can only tighten what the agent may do. Turning the gate off or raising the tier is stored per person in your own `local.yaml` (`agent:`), outside the folder, so a commit someone pulls can never disable your safeguards; a repository that commits a `.vibe-local/connections.yaml` with `sandbox: full` changes nothing.

What the agent actually gets is `EffectivePolicy(committed, local)`:

- **Sandbox** is your own choice (read-write when you have not made one), lowered to the committed limit. You can go below the limit freely and up to it, never above. A committed `full` is not a grant: with nothing set locally you still get read-write.
- **Ask before writes** is on unless you turned it off locally and the committed file does not require it. A committed `true` cannot be loosened by anyone. A committed `false` or no value loosens nobody.
- **Anything unrecognised** fails closed to `read-only`.

The workspace's own `local` database is the only connection where the gate can be turned off. For every other connection the agent always asks.

## File formats

Every format is written the same way every time, so a save, load, save cycle is byte-identical and git diffs show only real changes.

### Notebooks: `notebooks/<id>.vnb`

Pretty JSON, 2-space indent, no HTML escaping (a `<` in SQL stays a `<`), keys in a fixed order (`schemaVersion`, `title`, `cells`), cell ids kept stable, one trailing newline. Outputs are never stored. Format version 2 (a cell may name a `connection`).

- **The file name is the id.** There is no `id` and no `updatedAt` inside the file, so a rename in an editor or `git mv` is a rename and a timestamp never dirties a diff. The modification time is the "updated" time.
- **File names.** Notebook and query ids are 1 to 64 characters of lowercase letters, digits, `-` and `_`, starting with a letter or digit, and not a Windows device name (`con`, `nul`, `com1`, ...). A file with another name, a malformed file, or one over 10 MB still appears in the list, with an error, so it can be found and fixed; it is never loaded or overwritten.
- **Creating** names the file after the title (`Weekly Revenue` becomes `weekly-revenue.vnb`), adding `-2`, `-3` when taken. **Renaming** changes the title only and keeps the file name, so an id stays stable.
- Every load rebuilds the notebook field by field from a whitelist, so a hand-edited or newer file can never smuggle in a bad cell.

### Queries: `queries/<id>.sql`

```sql
-- vibe: {"title":"Top customers","description":"Biggest spenders","connection":"prod_pg","parameters":[{"name":"min","type":"number","required":true}]}
SELECT customer, sum(amt) AS total
FROM orders
WHERE amt > $min
GROUP BY 1
```

The first line is a SQL comment holding one line of JSON, so any SQL tool still runs the file and the header is a single diff line. Keys: `title`, `description`, `connection` (the connection whose catalog unqualified names use; empty means the workspace default) and `parameters`. A leading byte-order mark and blank or indented lines before the header are ignored. A file without the header is a valid query titled by its file name. A header that cannot be parsed (bad JSON, parameters of the wrong shape) lists the query with an error and refuses saves, so parameters it failed to read are never silently dropped. Line endings are normalised to LF on save. As with notebooks, the file name is the id.

### Skills: `skills/<name>/SKILL.md`

```markdown
---
name: revenue-definitions
description: How we define revenue, net of refunds
table_name: orders
source: import
---

# Revenue
...
```

The same frontmatter as the built-in skills, so Claude Code and other tools can read them. `table_name` (the skill loads only when that table has rows) and `source` (omitted when `user`) are optional. The folder name is the skill name and must equal the `name` in the frontmatter; a skill that does not match lists with an error and is not used, not guessed at. Other files in a skill folder are left alone.

Saving is lossless: frontmatter keys Vibe does not know (`allowed-tools`, `license`, `metadata`, comments) are kept as written and in order, and only blank lines around the body are trimmed, so indentation survives. Line endings are normalised to LF.

## Editing from outside, and conflicts

Files are the source of truth. You can edit them in an editor, `git pull`, or let Claude Code write them.

- **Compare-and-write.** Reading a file captures its content hash (its revision). A save that finds a different hash on disk returns a conflict (`ErrConflict`) and changes nothing. The hash decides, not the modification time, so `touch` or checking out identical content is not a conflict. Saving over a file you never read is also a conflict, and so is deleting one that changed since it was read. The surrounding app offers reload, overwrite or save as copy; overwrite is an explicit option.
- **Revisions travel with the copy.** A notebook, query or skill carries its `rev`; an editor that holds a copy across user time sends it back with the save (Studio does for all three), so a change made by a git pull, an editor or an agent in the same process is a conflict too. A save that sends no `rev` falls back on the revision the store last saw (refreshed by every read, list and write): that fallback is for code that reads and writes in one breath, and never for an editor. The agent can do better: `list_skills` and `read_skill` return a skill's `rev`, and `create_or_update_skill` takes it back as an optional `rev` (and returns the new one), so a skill changed since the agent read it is refused.
- **Atomic writes.** Every write goes to a temp file in the same folder, is synced, then renamed over the target, so a crash never leaves half a file. Temp files left behind by a crash are swept when a workspace is opened. Files over 10 MB are refused. Writers in different processes are not serialised (there is no cross-process lock yet), so the window between the check and the rename is small but real.
- **Safe paths.** Nothing is read or written outside the workspace. `..`, absolute paths and symbolic links that lead outside are refused, and a symbolic link as a file itself is refused.
- **The watcher** reports changes made by anything other than Vibe's own stores, as `notebook`, `query`, `skill` or `config` events (created, changed, deleted), after a short debounce. It registers each directory on its own (the operating system does not watch folders recursively), including `skills/<name>/` and folders created later. Its own writes are ignored by content hash. A folder removed or renamed away and later recreated (a branch checkout and back) is watched again. The state of the files when `Watch` returns is the baseline, so load content after starting it. Your own per-person settings live outside the folder and are not watched.
- **Copying a workspace** (`cp -r`) copies its `id`, so both copies would share keychain entries. Given the list of workspaces you have opened, `DuplicateIDs` reports another live path with the same id and `RegenerateID` gives the copy a new one, editing only the `id:` line.

## Opening a workspace as a session

`OpenSession` builds the workspace's DuckDB hub and registers every merged connection. Local `duckdb` file connections attach straight away (they are local files, nothing to dial); every other type connects on first use. `${ENV}` is expanded only in your own scopes. `default_connection` becomes the default catalog, so `SELECT * FROM sales` reads the default connection's table. A connection that cannot be set up (a missing environment variable, a relative path that escapes the folder, a path in your own scope whose folder does not exist, a type that is not available yet) is reported as an issue with a redacted message and left out, and the rest of the workspace still opens.

## One owner at a time (the hub lock)

DuckDB lets one process write a `.duckdb` file, or many read it, never a mix. So exactly one process per workspace owns the hub: Studio, or a headless `vibe mcp serve`. Ownership is a non-blocking `flock` on `.vibe-local/hub.lock` (the same crash-safe lock `.vibe` files used, `internal/filelock`). It dies with the process, so a crash never leaves the workspace locked; the leftover file is ignored.

`hub.lock` is mode `0600` JSON, written by the owner: `app`, `pid`, `host`, and while it serves MCP an `mcp` object (`url`, `token`), plus `control: "sigusr1"` when it can be asked to let go. Another process reads it with `hub.ReadHolder` without holding the lock, and only trusts it when all of these hold:

- the flock is really held right now (a file left by a crash, or committed to a cloned repository, is ignored);
- it is a regular file owned by the current user with no group or world permissions (this is a same-user trust model; the token is protected by file permissions and your user directory, nothing more);
- the endpoint is a plain `http://` loopback address;
- `.vibe-local` is a real directory (not a symlink) owned by you, and `hub.lock` is a single-link regular file: the lock is opened with `O_NOFOLLOW`, so a symlink or hard link committed to a repository is refused, never written through.

Take over: `hub.RequestRelease` sends `SIGUSR1` to the owner after checking it is on this host, is not the caller, and advertised `control: "sigusr1"`; `hub.TakeOver` then waits for the lock and returns the new hold. A signal rather than a control socket because a socket path under a deep folder can exceed the OS limit and a signal opens no extra listener. The owner's handler closes its session, releases the lock and exits.

On non-unix platforms the lock and take over are no-ops (Windows is not a release target).

## Command line

The CLI works on a workspace found the way git finds `.git`: `--workspace DIR`, or the current directory and then each parent until a `vibe.yaml` turns up. A `.vibe` path anywhere is refused with "`.vibe` files are no longer opened; create a workspace with `vibe workspace init`". `vibe verify`, `compact`, `rekey`, `dump`, `passphrase` and `info <file>` belonged to the container and are gone (the names stay registered, hidden, to say so).

| Command | What it does |
|---|---|
| `vibe workspace init [dir] [--name N] [--git] [--template T]` | Wraps `workspace.Init` (above). `dir` defaults to `.`. `--git` runs `git init` when git is installed. `--template` (`contacts`, `inventory`, `order-entry`, `training-registrations`) seeds `data/local.duckdb` with the template's sample tables and writes its starter notebook to `notebooks/`. Prints the next steps. `vibe init [dir]` is an alias. |
| `vibe workspace info [--workspace DIR] [--json]` | Root, id and name; every connection with its scope, status, read-only flag and (for attached ones) table count; notebook, query and skill counts; the effective agent policy; who owns the hub. Never takes the hub or creates a data file. |
| `vibe query "<sql>" [-c conn] [--format table\|csv\|json] [--limit N] [--read-only]` | One statement through the session. `-c` makes that connection's catalog the default for unqualified names. `-` reads the statement from standard input. `--limit` defaults to 1000 (0 for all), and a cap is reported on standard error. |
| `vibe import <file> [--into [conn.]table] [--mode ...] [--key ...]` | See `import-office.md`. |
| `vibe export <[conn.]table> \| --sql "<query>" [-o file]` | See `import-office.md`. |
| `vibe studio [dir] [--gui] [--read-only]` | The terminal UI: a SQL REPL on the session, a read-only table browser across every attached connection (remote tables are listed without counts), and a notebook list. Connection problems are shown at the top of the REPL, since the alternate screen hides stderr. `--gui` launches the desktop app instead; until #111 teaches Studio to open a workspace folder, `--gui` with a folder is refused (`guiOpensWorkspaces` in `cmd/studio.go`). |
| `vibe mcp serve [--workspace DIR]` | See `docs/mcp.md`. |

**Who may write.** A command that can write (`query`, `import`, `export` without `--read-only`, `studio`) first takes the workspace's hub lock (see "One owner at a time"). If Studio or `vibe mcp serve` holds it, the command stops and names the holder: `workspace acme is open in Vibe Studio (pid 4242), which owns its database; close it, or re-run with --read-only`. Only commands that have a `--read-only` flag (`query`, `export`, `studio`) suggest it. It skips the lock and attaches every connection read-only (and refuses `COPY ... TO`). DuckDB cannot mix a writer with readers on one file, and an owner keeps the workspace's local `.duckdb` files open for writing, so while someone owns the workspace `--read-only` reaches only its other connections (MotherDuck, Postgres, ...). A locked local file is reported in plain words ("a local DuckDB file that Vibe Studio (pid 4242) has open for writing"), and `studio` does not offer read-only when the default database is one. A workspace that has no data file yet reports "has no data file yet" instead of creating one. `studio` offers `--read-only` interactively when stdin is a terminal.

**SQL checks.** The CLI is you acting directly, not the agent, so SQL runs as written and the agent's allow-list does not apply. As a courtesy `query` (and the TUI's REPL) refuse `ATTACH`, `DETACH`, `INSTALL`, `LOAD`, `SET` / `RESET` and `CREATE`/`DROP SECRET`, with an explanation (connections come from the workspace, credentials from the keychain). This is a check for common slips, not a security boundary: DuckDB's configuration lock does not itself stop those statements. The check understands comments and quoted text (`'...'`, `"..."`, `E'...'`, `$tag$...$tag$`). `export --sql` must be exactly one `SELECT`, judged by DuckDB's parser (`json_serialize_sql`), since the text is placed inside `COPY (...)`. A write to a connection that is `read_only` fails with a hint to set `read_only: false`. Writes go straight to their connection; there is nothing to save.
