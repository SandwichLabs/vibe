# Workspaces

A **workspace** is a folder, optionally a git repository, that holds your notebooks, saved queries and skills as ordinary files, plus a small `vibe.yaml` that marks it. Your data stays wherever it already lives; the workspace only describes how to reach it.

This page is the file-level reference, the command line, and how Studio opens, creates and switches workspaces (#111). Everything below is implemented by `internal/workspace` and its stores. `.vibe` here means a bundle (see "Sharing without git"); the older data-holding container files are not opened by anything any more.

## In Studio

- **Launcher.** With no workspace open Studio shows the launcher: recent workspaces (name, folder, git branch, last opened; a folder that has gone missing keeps its row with "Folder not found", **Locate…** and remove, and is never dropped automatically), **Open folder…** (Ctrl/Cmd+O) and **New workspace…** (Ctrl/Cmd+N). Up/Down move between rows, Enter opens, Delete or Backspace removes a row from the list (with an Undo). The folder's git branch is read from `.git/HEAD` in plain Go; Studio never runs `git` when it opens a folder (a repository's own config can name programs to run). The list lives in your app preferences, never in the workspace.
- **Open folder on a plain folder.** A folder with no `vibe.yaml` is never initialised silently: Studio asks "Make this folder a workspace?" and, on yes, adds `vibe.yaml`, `notebooks/`, `queries/`, `skills/` and `data/`, leaving your other files alone.
- **New workspace.** Name, where to put it (a new folder named after the name), a git repository (when `git` is installed) and a starter template (see [`templates.md`](templates.md)).
- **Switcher.** The folder button in the top bar (Ctrl/Cmd+Shift+O) lists the open workspace, recents, Open and New. Switching saves open notebooks first, releases the old workspace's hub, then opens the next. A copied folder that shares another workspace's `id` offers **Regenerate id**; saved settings that were made for a different folder at the same path offer a re-confirm.
- **Read-only and Take over.** If another process (usually `vibe mcp serve`) owns the workspace's database, Studio opens it read-only: notebooks, queries and skills stay editable (they are files), local DuckDB files show as in use, and a banner offers **Take over…**. Taking over asks the other program to let go (Claude Desktop and Claude Code sessions that were using it reconnect on their own, and then go through Studio). Studio itself lets go when another program asks.
- **Changed on disk.** The workspace is watched. An open notebook, query or skill that changes on disk with no unsaved edit is reloaded quietly; with an unsaved edit a banner offers **Reload from disk**, **Keep mine** and **Save mine as a copy**. A save that would overwrite a newer file is refused, never forced.
- **Constraints come from the database.** Studio coerces typed values for datasheet edits but no longer carries its own required-field or validation rules (those were Access-era metadata): `NOT NULL`, `CHECK`, keys and foreign keys declared in the database are what stop a bad row, and the datasheet shows the database's message.
- **No Save.** Notebooks, queries and skills save as you go; table edits go straight to their connection. There is no Save, Revert or unsaved-changes prompt.
- **Settings.** *Workspace*: name, default connection, the agent's **team limit** (committed, can only tighten), **this computer** (your own choice, up to the team limit), and Studio's MCP switch with **Copy Claude Code config** / **Copy Claude Desktop config**. *App*: theme and the agent's model, including its API key as a write-only field ("In your keychain, Replace, Clear", and a one-time offer to move a plaintext key out of `~/.vibe.yaml`; see [`llm-config.md`](llm-config.md)). *About*: the stamped version, commit and build date (the same values as `vibe --version`), the Go and DuckDB versions, **Copy build info**, **Report a bug** (opens GitHub's issue chooser with nothing prefilled), links to the licence and privacy pages, and a searchable **Third-party notices** viewer (the embedded `THIRD_PARTY_NOTICES.md`). Neither Workspace nor App shows a token.

Studio opens a workspace at launch without showing the Launcher first:

- **A path argument**: `vibe studio --gui <dir>` (or `--workspace <dir>`, or `vibe-studio <dir>`). A folder inside a workspace opens the enclosing workspace; a folder with no `vibe.yaml` above it is passed through, and Studio asks "Make this folder a workspace?".
- **No argument, started from a terminal inside a workspace** (`vibe studio --gui`, or running `vibe-studio` there): the nearest folder above the current directory that holds a `vibe.yaml`.
- **Started from a desktop launcher** (current directory is your home folder or `/`) or from a folder outside any workspace: the Launcher, as always. For this automatic search, a `vibe.yaml` in your home folder, in a folder above it, or in `/` is never opened (a stray file there would otherwise capture every desktop launch). Name it explicitly to open it.
- **If it cannot be opened** (the folder is gone, `vibe.yaml` does not parse), the Launcher appears with the error. A workspace whose database another process owns opens read-only with the usual **Take over…** banner.

A `.vibe` file (the OS opening one with Vibe, or `vibe studio --gui x.vibe`) opens the **Import .vibe…** dialog instead, and anything else gets a plain notice. On macOS the Finder hands Studio a folder or `.vibe` file as an Apple Event rather than an argument, and while Studio is already running it is handled the same way. How the OS associations are wired is in [`../packaging.md`](../packaging.md#1a-file-opening-as-built-105-slice-3).

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
- **Sign-in is per person.** `sign_in` in your `local.yaml` maps a connection name to `keychain`, `prompt` or `env:PREFIX` for that person. A user-scope connection uses `sign_in` from the user file. Otherwise `keychain`: the definition's `credentials` value is only a UI hint (review #115, see Credentials).
- **Status precedence**, when several apply to one connection: `extension-missing`, then `needs-credentials`, then `error`, then `locked-by`, then `not connected`, then `ok`.

<!-- keys:local -->
| Key | Meaning |
|---|---|
| `version` | Schema version, currently `1`. |
| `connections` | This computer's private connections and overrides, same shape as above. |
| `sign_in` | Connection name to sign-in method, for this person. |
| `sign_in_fingerprint` | Connection name to the definition fingerprint the sign-in was chosen for (written by Vibe). |
| `sign_in_server` | Connection name to the host-level description of that server, so "the server changed (a to b)" can name both (written by Vibe). |
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
| `sign_in_fingerprint` | Connection name to the definition fingerprint the sign-in was chosen for (written by Vibe). |
| `sign_in_server` | Connection name to the host-level description of that server, so "the server changed (a to b)" can name both (written by Vibe). |
<!-- /keys -->

`.vibe-local/state.json` holds `lastNotebook` and `openTabs`; it carries no trust and stays in the folder. It is never a reason for anything to fail: an unreadable file is an empty state.

## Credentials

**(review #115) A shared `credentials:` value is only a hint.** A `credentials:` line in the committed `vibe.yaml` may pre-select a method in the UI; it is never the effective method, because a pulled commit must not choose how your credentials are fetched (a committed `credentials: env:PG_PROD` plus a host of its choosing would otherwise send your environment password there). The effective method comes only from your own per-person `sign_in`, defaulting to the keychain; an empty keychain is *needs-credentials*. Every `sign_in` entry (env included) is stored with the fingerprint of the definition it was chosen for (`sign_in_fingerprint`, in your `local.yaml` or user `connections.yaml`), and if the definition no longer matches (a pull changed the host), even an environment credential is withheld as "the server changed since you signed in" until you choose it again and confirm (`vibe conn credentials set <name> --sign-in env:PREFIX --confirm-rebind`; Studio asks). A hand-written `sign_in` without a fingerprint is withheld the same way.

A connection's secrets never live in `vibe.yaml`, in `local.yaml`, in a log line, in the agent's context or in a bundle. They are kept per person, by a **sign-in method** you choose per connection (it is stored in your `local.yaml`, or the user `connections.yaml` for user connections, never committed):

| Method | Where the secret is | Notes |
|---|---|---|
| `keychain` (default) | your OS keychain, service `vibe` | one JSON entry per connection: `user`, `password`, `token`, `key_id`, `secret`, plus a fingerprint and a host-level description of the server |
| `env:PREFIX` | environment variables, read only | see the mapping below |
| `prompt` | process memory, until Vibe quits | "Remember in my keychain" is off by default and promotes the entry to the keychain only when you tick it |

There is **no plaintext-file fallback**. With no keychain (SSH, a locked collection, no desktop) Vibe says so and names the `env:` alternative. Every keychain call has a five second timeout, so a locked keychain reads "not reachable" instead of freezing Studio.

**Environment mapping** for `env:PREFIX` (read at connect time, never written):

| Variable | Fills |
|---|---|
| `PREFIX_USER` | username (readable) |
| `PREFIX_PASSWORD` | password |
| `PREFIX_TOKEN` | token |
| `PREFIX_KEY_ID` | S3/GCS/R2 key id, or the Azure service principal client id (readable) |
| `PREFIX_SECRET` | S3/GCS/R2 secret key, or the Azure account key, connection string or client secret |

`env:MOTHERDUCK` therefore reads `MOTHERDUCK_TOKEN`: MotherDuck's token is only ever used when you choose that source, never picked up from the ambient environment. An env credential has no fingerprint (choosing it is your say-so).

### The workspace `.env` (#168)

People keep sign-in variables in a git-ignored `.env` in the workspace folder (`MOTHERDUCK_TOKEN=...`), and a desktop launcher gives Studio none of the shell's environment. So `env:PREFIX` sign-ins can read a workspace's `.env`, **after you allow it**, direnv-style:

- **Opt-in per file.** A workspace with an undecided `.env` shows a non-blocking banner (Studio) or, on a terminal, a one-time question (the CLI). It lists **variable names only, never values**, and which connections' `env:` sign-ins they would satisfy. **Allow** is remembered against the file's absolute path **and a sha256 of its content**: any edit makes it "changed" and Vibe asks again, like `direnv allow`. **Deny** is remembered by path and holds until revoked (edits do not re-nag). Denying while a connection already signed in from the `.env` is connected takes effect when the workspace is reopened. **Not now** asks again next time the workspace opens.
- **Concurrent edits.** Studio and the CLI may record decisions at the same time; one update can be lost, which fails safe (you are asked again).
- **Where decisions live.** In `dotenv.json` in your own config directory, beside the workspace's `local.yaml` (`<config dir>/workspaces/<id>-<hash>/`). Never in the workspace folder or a bundle.
- **Status.** Settings > Workspace > *.env file* and `vibe workspace env status` show *allowed*, *denied*, *changed since allowed*, *not decided* or *none*, with **Allow**, **Deny** and **Revoke**.
- **Parsing.** Plain dotenv only: `KEY=value`, `export KEY=value`, single or double quotes (double quotes understand `\n \t \" \\`), `#` comments, CRLF. Nothing is executed or expanded (`$VAR`, `$(cmd)` and backticks stay literal text) and `.envrc` is never evaluated. Invalid lines are skipped with a warning naming the line and variable, never the value. Multi-line quoted values (a PEM block) are skipped whole with one warning. A file over 64 KiB, not a regular file (a FIFO, say), or a symbolic link that points outside the workspace is ignored (the status says why).
- **Scope of the values.** They go into a **per-workspace overlay** that `env:PREFIX` sign-ins consult before the process environment (via `secrets.EnvStore`'s getenv seam). They are never put in the process environment (`os.Setenv`), so they do not reach other workspaces, the agent, agent tools, sandboxed commands or MCP clients. Values of credential-named variables (the redactor's secret-name rule, plus `*_PASS` and `*_URL`/`*DSN`) and any value of 16 characters or more are registered with the redactor, so they do not appear in a log, an error, the UI or the agent; short plain settings such as `PORT=5432` are not masked (they would mangle every error that mentions the number). The status bindings (`GetDotEnvStatus` and friends) return names and state only.
- **Changes while open.** The watcher reports a `dotenv` change. An allowed file that changes becomes *changed*; **the values loaded earlier stay in use until you allow the new content**, and Allow reloads them without reopening the workspace. MotherDuck takes its token only when a workspace opens, so Studio reopens the workspace after you allow a `.env` that supplies it.
- **Git.** If git tracks the `.env`, the prompt, Settings and `env status` warn that a credential is committed and recommend `git rm --cached .env` and rotating it. `vibe workspace init` adds `.env`, `.env.*` and `!.env.example` to `.gitignore`. The check runs git with `core.fsmonitor` off and only when a `.git` exists, and is cached.
- **Headless.** `vibe mcp serve` never prompts: it uses only a saved allow and logs one line (state and variable names) when a `.env` is present but not allowed. The CLI prompts only on a terminal; without one it uses only an existing allow.
- **Not exported.** A `.vibe` bundle never includes `.env` (it holds only notebooks, queries, skills and sanitised connection definitions; a test checks).

**Write-only.** Studio, MCP and the agent can set, replace, clear and ask whether a secret is set, never read it back. Only secrets are write-only: the username and the key id are readable and shown, and redacted errors keep `user='rosa'` while hiding the password. The status the UI gets is `{method, username or keyId, secretSet, fingerprintMatches, available, detail}`.

**Fingerprint.** A keychain or prompt credential is bound to a hash of the connection's type, host, port, database, storage scope, path, region and every option (endpoint, url_style, hostaddr, sslmode, account and so on), checked at resolve time on the environment-resolved definition. If a pull changes any of them the connection becomes *needs-credentials* ("the server changed since you signed in (db.internal to db-new.internal). Sign in again") and the saved password is not sent to the new server. Renaming a connection or toggling read-only does not change it; any option does, on purpose (a redirected endpoint must ask you to sign in again). Re-saving after a server change starts the secret fresh, so the old password is not carried over.

**Redaction.** Every secret value Vibe resolves is registered with a redactor that scrubs it (also as echoed SQL-quoted, URL-escaped or JSON-escaped), plus credential-shaped text: `password='...'`, `password=...`, prefixed names such as `aws_secret_access_key=`, `motherduck_token=` and `PGPASSWORD=`, the DDL form `PASSWORD '...'`, `"password": "..."`, `scheme://user:pw@host` (the password only) and `Bearer ...`. It scrubs these from connect and query errors and from logs (DuckDB's Postgres errors echo `password='...'` verbatim). Only secret-bearing options (`PASSWORD`, `SECRET`, `*TOKEN`, `ACCOUNT_KEY`, `CONNECTION_STRING`, ...) are registered, never plain ones like `HOST`, `USER`, `DATABASE` or `REGION`, so `user='rosa'` stays readable and ordinary words are never masked. Secret DDL (`CREATE TEMPORARY SECRET`) runs only on the session's dedicated non-logging connection, under the attach lock, with every value quoted; the statement text is never logged or returned, and on failure every quoted literal in DuckDB's error is replaced. The agent's SQL policy refuses `CREATE`/`DROP SECRET`, `duckdb_secrets()`, `which_secret()`, any `ATTACH`/`SET`/`COPY`/... that names a secret, in every sandbox tier including `full`, with no approval path.

The agent's own model key is handled the same way: see [`llm-config.md`](llm-config.md).

See [connections.md](connections.md) for each connection type, its fields, what read-only guarantees and the MotherDuck notes.

## The agent limit

The committed `sandbox` and `ask_before_writes` are a **ceiling, not a switch**: they can only tighten what the agent may do. Turning the gate off or raising the tier is stored per person in your own `local.yaml` (`agent:`), outside the folder, so a commit someone pulls can never disable your safeguards; a repository that commits a `.vibe-local/connections.yaml` with `sandbox: full` changes nothing.

What the agent actually gets is `EffectivePolicy(committed, local)`:

- **Sandbox** is your own choice (read-write when you have not made one), lowered to the committed limit. You can go below the limit freely and up to it, never above. A committed `full` is not a grant: with nothing set locally you still get read-write.
- **Ask before writes** is on unless you turned it off locally and the committed file does not require it. A committed `true` cannot be loosened by anyone. A committed `false` or no value loosens nobody.
- **Anything unrecognised** fails closed to `read-only`.

The gate can be turned off only for the workspace's own local files (`duckdb` and `sqlite` connections). For every other connection (Postgres, MotherDuck, anything that is not a local file) the agent **always asks**, whatever your setting, and headless MCP refuses those writes. A write also needs the connection to be writable (`read_only: false`); see [agent-policy.md](agent-policy.md) for the whole policy, tier by tier.

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
- **The watcher** reports changes made by anything other than Vibe's own stores, as `notebook`, `query`, `skill`, `config` or `dotenv` events (created, changed, deleted), after a short debounce. It registers each directory on its own (the operating system does not watch folders recursively), including `skills/<name>/` and folders created later. Its own writes are ignored by content hash. A folder removed or renamed away and later recreated (a branch checkout and back) is watched again. The state of the files when `Watch` returns is the baseline, so load content after starting it. Your own per-person settings live outside the folder and are not watched.
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

The CLI works on a workspace found the way git finds `.git`: `--workspace DIR`, or the current directory and then each parent until a `vibe.yaml` turns up. A `.vibe` path where a workspace folder is expected is refused with "`.vibe` files are no longer opened; create a workspace with `vibe workspace init`"; `vibe import <file.vibe>` is the exception: it imports a bundle. `vibe verify`, `compact`, `rekey`, `dump`, `passphrase` and `info <file>` belonged to the container and are gone (the names stay registered, hidden, to say so).

| Command | What it does |
|---|---|
| `vibe workspace init [dir] [--name N] [--git] [--template T]` | Wraps `workspace.Init` (above). `dir` defaults to `.`. `--git` runs `git init` when git is installed. `--template` (`contacts`, `inventory`, `order-entry`, `training-registrations`) seeds `data/local.duckdb` with the template's sample tables and writes its starter notebook to `notebooks/`. Prints the next steps. `vibe init [dir]` is an alias. |
| `vibe workspace info [--workspace DIR] [--json]` | Root, id and name; every connection with its scope, status (the same words as `vibe conn list` and Studio's rail: `ok`, `not-connected`, `needs-credentials`, `extension-missing`, `locked-by`, `error`), read-only flag and (for attached ones) table count; notebook, query and skill counts; the effective agent policy; who owns the hub. Never takes the hub or creates a data file. |
| `vibe workspace env status\|allow\|deny\|revoke [--workspace DIR] [--json]` | The workspace `.env` decision (see "The workspace `.env`"). `status` prints the state, variable names and the connections they feed, never values. |
| `vibe query "<sql>" [-c conn] [--format table\|csv\|json] [--limit N] [--read-only]` | One statement through the session. `-c` makes that connection's catalog the default for unqualified names. `-` reads the statement from standard input. `--limit` defaults to 1000 (0 for all), and a cap is reported on standard error. |
| `vibe import <file> [--into [conn.]table] [--mode ...] [--key ...]` | See `import-office.md`. A `.vibe` file is routed to `vibe import bundle`. |
| `vibe export bundle [-o file.vibe] [--notebooks a,b] [--queries ...] [--skills ...] [--with-connections] [--with-local-connections a,b] [--encrypt] [--force]` | Write a `.vibe` bundle of the workspace's content (everything unless some are named). Takes no hub lock: it only reads files. See "Sharing without git". |
| `vibe export notebook <id> [-o file.vnb]` / `vibe import notebook <file.vnb> [--on-conflict ...]` | One notebook, through the same checks as a bundle. |
| `vibe import bundle <file.vibe> [--on-conflict rename\|overwrite\|skip] [--with-connections all\|a,b] [--dry-run]` | Import a bundle into the workspace. |
| `vibe export <[conn.]table> \| --sql "<query>" [-o file]` | See `import-office.md`. |
| `vibe studio [dir] [--workspace dir] [--gui] [--read-only]` | The terminal UI: a SQL REPL on the session, a read-only table browser across every attached connection (remote tables are listed without counts), and a notebook list. Connection problems are shown at the top of the REPL, since the alternate screen hides stderr. `--gui` launches the desktop app instead, opening `dir` there, or the workspace around the current directory (never one found only in your home folder, above it, or in `/`). |
| `vibe mcp serve [--workspace DIR]` | See [`mcp.md`](mcp.md). |
| `vibe update [--check] [--yes]` | Look up the latest stable release (an explicit, plain request to github.com) and compare it with this build; if it was installed by `install.sh`, offer `Install vX? [y/N]` (terminal only) or install with `--yes`, by running that release's verified `install.sh`. Otherwise it only reports. Exit 0 when the check worked, 1 on an error. See [install](../install.md#upgrade) and [privacy](../privacy.md). |
| `vibe config updates [off\|weekly]` | Show or set Studio's weekly release check (the Settings, About toggle). Unset until you answer; either answer is saved in your user preferences, never in the workspace. |

**Who may write.** A command that can write (`query`, `import`, `export` without `--read-only`, `studio`) first takes the workspace's hub lock (see "One owner at a time"). If Studio or `vibe mcp serve` holds it, the command stops and names the holder: `workspace acme is open in Vibe Studio (pid 4242), which owns its database; close it, or re-run with --read-only`. Only commands that have a `--read-only` flag (`query`, `export`, `studio`) suggest it. It skips the lock and attaches every connection read-only (and refuses `COPY ... TO`). DuckDB cannot mix a writer with readers on one file, and an owner keeps the workspace's local `.duckdb` files open for writing, so while someone owns the workspace `--read-only` reaches only its other connections (MotherDuck, Postgres, ...). A locked local file is reported in plain words ("a local DuckDB file that Vibe Studio (pid 4242) has open for writing"), and `studio` does not offer read-only when the default database is one. A workspace that has no data file yet reports "has no data file yet" instead of creating one. `studio` offers `--read-only` interactively when stdin is a terminal.

**SQL checks.** The CLI is you acting directly, not the agent, so SQL runs as written and the agent's allow-list does not apply. As a courtesy `query` (and the TUI's REPL) refuse `ATTACH`, `DETACH`, `INSTALL`, `LOAD`, `SET` / `RESET` and `CREATE`/`DROP SECRET`, with an explanation (connections come from the workspace, credentials from the keychain). This is a check for common slips, not a security boundary: DuckDB's configuration lock does not itself stop those statements. The check understands comments and quoted text (`'...'`, `"..."`, `E'...'`, `$tag$...$tag$`). `export --sql` must be exactly one `SELECT`, judged by DuckDB's parser (`json_serialize_sql`), since the text is placed inside `COPY (...)`. A write to a connection that is `read_only` fails with a hint to set `read_only: false`. Writes go straight to their connection; there is nothing to save.

## Sharing without git: `.vibe` bundles

A `.vibe` file is a **bundle**: a zip of a workspace's notebooks, queries and skills, and optionally its connection definitions, for sharing by email or chat when git is not in the picture. It is implemented by `internal/bundle`; Studio and the CLI are two front ends to it. It is a person's action: no agent tool and no MCP method can export or import a bundle (a test keeps `internal/agents`, `internal/llm` and `internal/mcp` from importing the package).

**A bundle holds no data and no credentials.** There is no `data/` directory and no `.duckdb` file in it. Connection definitions never carry a sign-in method, a keychain reference, a fingerprint, a nonce or any per-person setting; they are read from the committed team definitions (and the "This computer" ones the person ticks) and then sanitised with the strictest rule there is, the one for the committed `vibe.yaml`: no absolute path or path outside the workspace, no `${ENV}` reference, nothing credential-shaped, and any option whose value is an absolute path is left out. A definition that cannot be made portable is left out and named in the manifest's `warnings`. The workspace's own default `local` connection is not exported, since there is no data to go with it.

### Format, version 2

```
manifest.json
notebooks/<id>.vnb
queries/<id>.sql
skills/<name>/SKILL.md
connections.yaml            (optional)
```

`manifest.json` has `kind: "vibe/bundle"`, `formatVersion: 2`, `createdAt`, `createdBy` (the app version), `workspaceName`, `warnings`, and `contents`: one `{path, kind, id, sha256, size}` per entry. The entries are the stores' own file bytes, so an export followed by an import into an empty workspace gives byte-identical files. `connections.yaml` is `version: 1` plus `connections:` and nothing else.

### Locking a bundle

A bundle is unencrypted by default. **Lock with a passphrase** (Studio) or `--encrypt` (CLI) wraps the whole zip in one [age](https://age-encryption.org) envelope with a scrypt passphrase recipient, from `filippo.io/age`; there is no bespoke cryptography here. The age header (`age-encryption.org/v1`) is the unencrypted marker: the importer recognises it and asks for the passphrase before anything else. A wrong passphrase says "wrong passphrase", a damaged file says it is damaged, and Vibe cannot recover a forgotten passphrase. The CLI asks twice on a terminal; for scripts `VIBE_BUNDLE_PASSPHRASE` supplies it (less safe: other processes of yours can read the environment). Studio's passphrase crosses the bridge in one direction only and is not stored.

### Importing

Studio: **Import .vibe…** in the workspace menu imports into the open workspace; **Import .vibe…** on the launcher creates a new workspace from the bundle. The file is opened, verified and previewed first; nothing is written until **Import**. Each item that already exists in the workspace gets its own choice: **Keep both** (the new one is saved as `<id>-2`, `-3`, ...), **Replace mine** (a compare-and-write against the revision just read, so a file changed in between is reported, never clobbered) or **Skip**; every item can also be unticked. CLI: `--on-conflict rename|overwrite|skip` (default rename) and `--dry-run`.

Import treats the file as hostile:

- The set of entry names is closed: the five patterns above. Absolute paths, `..`, backslashes, drive letters, directories, symlinks, nested paths, encrypted zip entries and any unknown file refuse the whole bundle. Ids and skill names must pass the stores' own id rules.
- Every entry's SHA-256 and size must match the manifest, and the manifest must list exactly the entries in the archive.
- Caps: 10 MB per entry, 50 MB unpacked in all, 2,000 entries, 64 MB on disk. Declared sizes are checked before decompression and the real byte count is enforced while reading, so a zip bomb is refused. An encrypted bundle's scrypt work factor is capped at 18, the value Vibe itself encrypts with; a header asking for more is refused ("this bundle was made with settings Vibe won't use").
- Each document is parsed by its own store's rules (`notebooks.Coerce`, `queries.Decode`, `skills.Decode`) before anything is written. A skill can never claim `source: built-in` (it is imported as an ordinary skill, with a warning) and cannot take a built-in's name; every skill that arrives in a bundle is saved with `source: import` and badged **Imported** in the Skills view.
- Writes go through `notebooks.Store`, `queries.Store` and `skills.Store`.
- **Connections** are never added unless the person names them (Studio: tick them in the preview; CLI: `--with-connections all|a,b`). The preview shows everything a definition sets (host, database, path, region, storage scope, every option) and warns about TLS turned off, `sslmode=disable|allow|prefer`, a non-AWS endpoint for `s3`, and a storage scope that is a whole bucket. A definition always arrives **read-only**, whatever it asks for (Overwrite keeps your own read/write choice, and clears your sign-in choice and saved credential for that name, so it arrives needing credentials). They go to **This computer** for the workspace, never the committed `vibe.yaml`, and arrive with no credentials, so each shows *needs credentials* until you sign in. Move one to the team later with the usual scope move. A bundle cannot replace a team or all-workspaces connection of the same name; the imported one is renamed (`name_2`) or skipped. An imported notebook still runs its SQL when opened, as any notebook from a git pull does; read a bundle from someone you do not know before you open its notebooks.

A file that is a **formatVersion 1** bundle or a file with SQLite's header gets one plain message: "This is an older .vibe file that holds data. Vibe no longer opens these." An SQLCipher container has no header at all, so it, like anything else that is not a bundle, says "this is not a Vibe bundle (an older .vibe file that held data is not opened either)". A bundle from a newer Vibe asks you to update.

Text the bundle's maker wrote (workspace name, created-by, the manifest's own warnings) is length-capped, stripped of control and bidirectional characters, and shown apart as "From the person who made this bundle". Notebook ids are kept as they are, including `nb_<uuid>` ones, so Replace replaces. Studio reads each existing item's revision when it previews, and Replace only replaces that version.

Single notebooks: `vibe export notebook <id> [-o x.vnb]` and `vibe import notebook <x.vnb> [--on-conflict ...]` (it also accepts Studio's single-notebook export) use `bundle.NotebookPackage`, the bundle's own import path.

The single-notebook export and import in Studio (`ExportNotebook` / `ImportNotebook`) use the same size cap and `bundle.CheckNotebook`, so a notebook is judged by one rule whether it arrives alone or in a bundle.
