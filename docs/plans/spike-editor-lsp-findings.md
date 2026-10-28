# Spike: your own editor and a Vibe language server

**Status: landed in #112.** The shipped reference is [`docs/reference/editor.md`](../reference/editor.md). Merge changes from the spike: `Source.Path` and the test-only editor override were removed or moved to tests, relation kinds reuse `engine.RelationKind`, the editor is checked for on `PATH` before a scratch file is made, and tests pin the scratch modes (0600/0700), the no-shell split and that the snapshot carries no connection definition or credential. Cut in review: the server's first-start bootstrap snapshot. Not built: VS Code and Zed extensions, hover beyond tables and columns.

**Ask.** (1) From the TUI or the CLI, edit a query in the system editor
(`$EDITOR`, so Neovim for a Neovim user). (2) A language server that any
LSP-capable editor can use to complete connections, schemas, tables and columns
from the workspace.

**Result.** Both work end to end on this branch. Verified in a real terminal:
`vibe studio` → `ctrl+o` → Neovim opens the query → `o.` completes
`id / customer / total` from `vibe lsp` → `:wq` → the TUI runs it and shows the
result. Setup for each editor is in [`editors/README.md`](../../editors/README.md).

## 1. Editor hand-off

### What other tools do

- **Which editor:** `$VISUAL`, then `$EDITOR`, then `vi` (git, psql, crontab,
  less). The value can carry arguments (`code --wait`, `emacsclient -t`), so
  it is split shell-style, not looked up as one path.
- **psql's `\e`:** writes the query buffer to a temp file, runs the editor,
  reads it back, runs it. A non-zero editor exit (`:cq`) is "cancel".
- **Bubble Tea:** `tea.ExecProcess` suspends the program, hands the terminal
  to a child process, and resumes with a message when it exits. No extra
  dependency is needed.

### What was built

- `internal/editor`: `Resolve` ($VISUAL / $EDITOR / `vi`, quote-aware
  splitting), `NewScratch`, `Command`, `Finish`, `Edit` (foreground, for the
  CLI).
- **The scratch file lives in `<workspace>/.vibe-local/edit/`, not `/tmp`.**
  Editors find their language server's root by walking up to a marker file
  (Neovim `root_markers`, Helix `roots`), and a buffer in `/tmp` has no
  `vibe.yaml` above it. The editor also gets `VIBE_WORKSPACE=<root>`.
- TUI REPL: `ctrl+o` edits the current input; `\e` + Enter edits the last
  query. `ctrl+e` was the obvious key but is the textinput's end-of-line.
  A changed, non-empty buffer runs (echoed as `sql> (from editor)`) and joins
  the history with its newlines intact, so `\e` brings back the multi-line
  text. An unchanged buffer or `:cq` runs nothing.
- CLI: `vibe query -e` (or `vibe query -e "SELECT ..."` to start from text).
  `:cq` exits 1 with "nothing was run".

## 2. The language server

### The constraint that decides the design

DuckDB lets one process open a `.duckdb` file read-write **or** several open it
read-only, never both (`internal/hub` doc). An editor starts its language
server when you open a `.sql` file and keeps it for hours. If `vibe lsp` held
even a read-only attach, `vibe studio`, Studio and `vibe query` could not open
the workspace's database for writing. So the server must not keep the hub open.

Options considered:

| Option | Verdict |
|---|---|
| LSP opens the workspace read-only and keeps it | Blocks the owner; rejected |
| LSP calls the owner's MCP endpoint (hub.lock) | Live, but only while an owner runs, and the tools are per-table and shaped for agents (many round trips per completion). A later option for live checks |
| The owner serves LSP itself, `vibe lsp` proxies stdio to it (as `vibe mcp serve` does) | Best long-term for live diagnostics; more moving parts than a spike needs |
| **A catalog snapshot file the owner writes; the LSP reads it** | **Chosen.** Lock-free, works with no owner running, survives restarts, trivial to reload |

### What was built

- `internal/lsp/catalog.go`: `Catalog` (connections → schemas → tables/views →
  columns, plus DuckDB function names). `Build` reads one connection in one
  `information_schema` query, never dials (only `StatusOK` connections), and
  skips DuckDB's own `memory` catalog and `pg_*` plumbing. `Snapshot` merges
  into the existing file, so a connection that is not connected right now keeps
  what was last known. Written atomically, mode 0600, at
  `.vibe-local/catalog.json` (git-ignored; names and types only, no rows, DSNs
  or credentials).
- Writers: the TUI (on open and after every REPL statement, in the background,
  only when it owns the workspace); `vibe lsp snapshot` (through the CLI's
  normal open: the hub lock briefly, or no local files while an owner holds
  it; connects all or `-c` connections, skips ones that fail). The server
  itself writes nothing (see "Cut in review" below).
- `internal/lsp/lex.go`, `complete.go`, `hover.go`: a small SQL lexer (quoted
  names, strings, both comment styles, unterminated runs at the cursor) and a
  completer that works on the statement under the cursor:
  - after `FROM` / `JOIN` / `INTO` / `UPDATE` (and the comma list after
    `FROM`): the default schema's tables and views, connections, schemas;
  - `conn.` → schemas and the default schema's tables; `conn.schema.` /
    `schema.` → tables;
  - `alias.` or `table.` → columns, resolving `FROM orders o JOIN pg.public.x y`;
  - bare words in expressions → the statement's columns and aliases, then
    functions, then keywords; nothing inside strings or comments;
  - names that need it are inserted `"quoted"`.
  Hover shows a relation's columns, a column's type, or a connection.
- `internal/lsp/server.go`: hand-rolled JSON-RPC over stdio (Content-Length
  framing; no new dependency). `initialize` (negotiates UTF-8 positions when
  the client offers them, else UTF-16), full document sync, `completion`
  (trigger `.`), `hover`, `shutdown`/`exit`. The snapshot is re-read when its
  mtime or size changes.
- `vibe lsp` picks the workspace from `--workspace`, `$VIBE_WORKSPACE`, the
  client's `rootUri`, then the working directory.
- `editors/nvim`: `require("vibe").setup()` registers the server (Neovim 0.11
  `vim.lsp.config`/`enable`, with a 0.10 fallback), starts it only under a
  `vibe.yaml`, and adds `:VibeRun` (buffer or range through `vibe query -`,
  result in a split) and `:VibeSnapshot`. `editors/helix/languages.toml` and an
  eglot line cover Helix and Emacs.

### Verification

- Unit tests: `internal/editor`, `internal/tui/edit_test.go`,
  `internal/lsp` (completion and hover table tests, a protocol round trip over
  pipes including UTF-16 positions, `Build` against a real attached DuckDB file,
  merge and reload).
- Headless Neovim 0.11.4 against the built binary: attach with root detection,
  `o.` column completion, `FROM` relation completion, hover on a view,
  `:VibeRun`. (The bootstrap path was verified, then cut in review.)
- tmux: the TUI → `ctrl+o` → Neovim with LSP omni-completion → `:wq` → result
  in the REPL. `vibe query -e` success, `:cq` and usage errors.

## Gaps and next steps

In rough priority order:

1. **Studio and `vibe mcp serve` should write the snapshot too.** (Studio: done in #256, `lsp.Live` rewrites it after every catalog change while Studio owns the workspace; `vibe mcp serve` still does not.) Studio is
   usually the owner, and while it is, `vibe lsp snapshot` cannot read the
   local DuckDB files. One call to `lsp.Snapshot` after open, connect, DDL and
   Refresh closes this.
2. **Prune removed connections.** Merge keeps connections that are not
   connected now; it should also drop ones no longer in the workspace.
3. **Diagnostics.** The server can open its *own* in-memory DuckDB (no
   workspace files, so no lock conflict) and use `json_serialize_sql()` for
   parse errors without binding. Binding errors (unknown table/column) can be
   checked against the snapshot. A live `EXPLAIN` needs the owner (the proxy
   option above).
4. **Signature help** for functions (`duckdb_functions()` has parameter names
   and types) and **CTE names** in completion.
5. **Run from the editor through the owner.** `:VibeRun` uses `vibe query`,
   which needs the hub lock or `--read-only`; while the TUI or Studio is open
   it should send the statement to the owner (MCP `run_query` is already there).
   A `workspace/executeCommand` `vibe.run` would make this editor-agnostic.
6. **VS Code and Zed extensions** (a few dozen lines each: launch `vibe lsp`
   for `sql`).
7. **A Queries view in the TUI** that opens `queries/<id>.sql` itself in the
   editor (files are the source of truth, and Studio's watcher already picks
   up outside edits).
8. Incremental document sync, multi-root workspaces, and notebook (`.vnb`)
   SQL cells are out of scope for the spike.

## Cut in review: the server taking its own first snapshot

The spike had `vibe lsp` take a snapshot itself on first start when there was none and no owner. Review cut it, for four reasons; do not bring it back without `hub.ProtectStdout` and a guard limiting it to local files:

1. **A race with the owner.** It checked `ReadHolder`, then opened read-only without the lock. The read-only attach holds DuckDB's shared file lock, so a Studio, TUI or `vibe mcp serve` starting in that window could not attach `data/local.duckdb` read-write.
2. **It dialled the default connection, whatever its type.** A committed `vibe.yaml` can name Postgres or MotherDuck as the default, so "opening a file does not dial your Postgres" was false.
3. **It bypassed the CLI's open path.** It used the keychain through the resolver (which can raise a GUI prompt on macOS) and skipped the `.env` allow.
4. **It never protected stdout**, which is the LSP channel. MotherDuck sign-in and DuckDB's C code can print there and corrupt the stream (spike #107 finding I).

The server now opens no database and writes no file. Completion is empty until the TUI or `vibe lsp snapshot` writes a snapshot; `vibe lsp snapshot` goes through `cliOpen` (hub lock briefly, or remote-only while an owner holds the workspace), skips connections that fail with a warning, and fails only when none could be snapshotted. Scratch buffers and `catalog.json` are written without following symbolic links under `.vibe-local/`, a message body over 32 MiB ends the server, and a panic in a handler answers with -32603.
