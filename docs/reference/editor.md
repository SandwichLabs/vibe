# Your own editor: `$EDITOR` and `vibe lsp`

Two pieces, usable separately. Both are for people at a keyboard; neither adds anything for agents (agents use [MCP](mcp.md)).

1. **Edit a query in your editor** from the terminal UI (`vibe studio`) or `vibe query -e`.
2. **`vibe lsp`**, a Language Server for `.sql` files in a workspace, for Neovim, Helix, Emacs (eglot) and any other editor with an LSP client.

Setup files for Neovim and Helix are in [`editors/`](../../editors/README.md).

## Edit in `$VISUAL` / `$EDITOR`

The editor is `$VISUAL`, then `$EDITOR`, then `vi`. The value may carry arguments (`code --wait`, `emacsclient -t`). It is split into words with single and double quotes understood and **run directly, never through a shell**, so `$(...)`, `;` and `&&` in the variable are plain text. If the program is not on your `PATH`, Vibe says so and names `$VISUAL` / `$EDITOR`.

| Where | How |
|---|---|
| TUI REPL | `ctrl+o` opens the current input. `\e` then Enter opens the last query (as in psql). |
| CLI | `vibe query -e` opens an empty buffer; `vibe query -e "SELECT ..."` starts from that text; `-` (standard input) cannot be combined with `-e`. |

`vibe query --saved ID -p name=value` runs a saved query with its declared `$name` parameters (see [workspaces.md](workspaces.md#queries-queriesidsql)); it does not combine with `-e`.

Save and quit to run the query. Quitting with an error (`:cq` in Vim) or leaving the buffer unchanged or empty runs nothing (`vibe query -e` says so and exits 0).

The buffer is a scratch file, `<workspace>/.vibe-local/edit/query-*.sql`, created mode 0600 in a folder that must be a real directory of yours (a symbolic link there, as a cloned repository could ship, is refused; the folder is set to 0700), and removed when the editor exits. It sits under the workspace so an editor that finds its language-server root by walking up to `vibe.yaml` attaches `vibe lsp` to it; the editor also gets `VIBE_WORKSPACE` in its environment. Outside a workspace the file goes to the system temp folder.

## Completion in Studio

The Query Explorer and the SQL cells of a notebook complete as you type, from the same completer `vibe lsp` uses. **Ctrl+Space** opens the list, **Enter** or **Tab** accepts, **Escape** closes it, and **Ctrl/Cmd+Enter** still runs the query, whether or not the list is open.

It offers what the statement under the cursor can take: connections, schemas, tables and views, columns (also through aliases, `o.` after `FROM orders o`), DuckDB functions and keywords. A name that needs double quotes is inserted quoted. A list item shows its detail on the right (a column's type and table, a table's schema and column count).

- **Connected connections are live.** Their schema is read again after a connect, a run that changed something, creating or dropping a table, or **Refresh catalog**.
- **Other connections** come from the last saved snapshot (`.vibe-local/catalog.json`) when it knew them, and otherwise show only their name, so `prod_pg.` is offered but its tables are not until you connect it.
- **Completion never connects a connection and never runs your SQL.** The catalog holds names and types only: no rows, no connection definition, no credential. A very large catalog is capped (2000 tables or views per schema, 512 columns per table, and 250,000 columns per connection, read from the default schema first so a large schema that sorts early cannot hide it).
- **Run on decides unqualified names.** In the Query Explorer the Run-on connection, in a notebook cell the cell's connection (else the workspace default), is the catalog `FROM ord` resolves against.
- With no workspace open, or if the call fails, the list falls back to SQL keywords.

## `vibe lsp`

A stdio language server for `.sql` files. It completes connections, schemas, tables, views, columns (through aliases: `o.` after `FROM orders o`), DuckDB functions and keywords, and hover shows a table's columns or a column's type.

The workspace is `--workspace`, else `$VIBE_WORKSPACE`, else the editor's root folder, else the nearest folder above the working directory that holds a `vibe.yaml`.

### Where the schema comes from

DuckDB lets one process write a database file or several read it, never both, and an editor keeps its language server for hours. So the server opens no database, dials no connection and writes no file. It reads a snapshot, `.vibe-local/catalog.json`, and reloads it when it changes. The snapshot holds **names and types only** (connections, schemas, tables, views, columns, function names): no rows, no connection definition, no credential. It is written mode 0600 and is git-ignored with the rest of `.vibe-local/`; the TUI, Studio (while it owns the workspace) and `vibe lsp snapshot` write it, never through a symbolic link, and nothing outside `.vibe-local/`.

- The TUI writes it when it opens the workspace and after each statement, so a connection you connect in the REPL shows up in completion.
- Studio writes it after every catalog change (a connect, a run that attached or changed something, Refresh catalog), merged with what is already there. It writes at most the caps above per connection.
- `vibe lsp snapshot` writes it on demand (`-c NAME` for some connections). It takes the workspace briefly, as `vibe query` does. When Studio or the TUI owns it, local DuckDB files are skipped (their owner keeps those current) and the other connections are read. A connection that cannot be connected is skipped with a warning and keeps what an earlier snapshot knew; the command fails when no connection could be snapshotted. With `-c`, an unknown name is a usage error (exit non-zero, before the hub is opened or the snapshot written), and a named connection that cannot be connected is warned about, the others are still written, and the command then exits non-zero. A local file skipped because the workspace is open elsewhere is not a failure.

**Completion is empty until Studio, the TUI or `vibe lsp snapshot` has written a snapshot.** Run `vibe lsp snapshot` once (Neovim's `:VibeSnapshot` does) or keep Studio or `vibe studio` open.

### Neovim (0.10+)

```lua
{
  "SandwichLabs/vibe",
  ft = "sql",
  config = function(plugin)
    vim.opt.rtp:append(plugin.dir .. "/editors/nvim")
    require("vibe").setup() -- { cmd = { "/path/to/vibe" } } if vibe is not on PATH
  end,
}
```

The plugin starts the server for `sql` buffers with a `vibe.yaml` above them, and adds `:VibeRun` (run the buffer or a range through `vibe query -`) and `:VibeSnapshot`. Without a plugin manager, `vim.lsp.config("vibe", { cmd = { "vibe", "lsp" }, filetypes = { "sql" }, root_markers = { "vibe.yaml" } })` and `vim.lsp.enable("vibe")` give completion and hover.

### Helix

Copy [`editors/helix/languages.toml`](../../editors/helix/languages.toml) into `~/.config/helix/languages.toml`.

### Emacs (eglot)

```elisp
(add-to-list 'eglot-server-programs '(sql-mode . ("vibe" "lsp")))
```

### VS Code, Zed

They need a small extension that launches `vibe lsp`; none is written yet.
