# Editor integration

The reference page is [`docs/reference/editor.md`](../docs/reference/editor.md). Two pieces, usable separately:

1. **Edit in your own editor.** In the TUI REPL (`slaw studio`), `ctrl+o` opens
   the current input in `$VISUAL` / `$EDITOR`, and `\e` + Enter opens the last
   query (as in psql). Save and quit to run it; quit with an error (`:cq` in
   Vim) or without changes to run nothing. From the shell, `slaw query -e`
   does the same (`slaw query -e "SELECT ..."` starts from that text).
   The buffer is a scratch file under `<workspace>/.slaw-local/edit/`, and the
   editor gets `SLAW_WORKSPACE` in its environment, so the language server
   below attaches to it.
2. **`slaw lsp`**, a Language Server for `.sql` files in a workspace (a folder
   with a `slaw.yaml`). It completes connections, schemas, tables, views,
   columns (through aliases as well: `o.` after `FROM orders o`), DuckDB
   functions and keywords, and hover shows a table's columns or a column's type.

## How the server gets the schema

DuckDB lets one process write a `.duckdb` file or several read it, never both,
and an editor keeps its language server running for hours, so `slaw lsp` never
opens the workspace's database. It reads a snapshot of the catalog,
`.slaw-local/catalog.json` (names and types only, mode 0600, git-ignored), and
reloads it when it changes:

- `slaw studio` (the TUI) writes it when it opens the workspace and after each
  statement, so a connection you connect in the REPL shows up in completion.
- `slaw lsp snapshot` writes it on demand (`-c name` for some connections). It
  takes the workspace briefly, as `slaw query` does; while the TUI or Studio
  owns it, local DuckDB files are skipped (their owner keeps those current).

The server itself opens no database and writes no file, so completion is empty
until the TUI or `slaw lsp snapshot` has written a snapshot.

## Neovim (0.10+)

The plugin is `editors/nvim` in this repository. With lazy.nvim:

```lua
{
  "SandwichLabs/slaw",
  ft = "sql",
  config = function(plugin)
    vim.opt.rtp:append(plugin.dir .. "/editors/nvim")
    require("slaw").setup() -- { cmd = { "/path/to/slaw" } } if slaw is not on PATH
  end,
}
```

It registers the `slaw` language server for `sql` buffers that have a
`slaw.yaml` above them (or `$SLAW_WORKSPACE`), and adds:

- `:SlawRun` runs the buffer, or a range (`:'<,'>SlawRun`), through
  `slaw query -` and shows the result in a split.
- `:SlawSnapshot` runs `slaw lsp snapshot`.

Without a plugin manager, `vim.lsp.config("slaw", { cmd = { "slaw", "lsp" },
filetypes = { "sql" }, root_markers = { "slaw.yaml" } })` and
`vim.lsp.enable("slaw")` are enough for completion.

## Helix

Copy [`helix/languages.toml`](helix/languages.toml) into
`~/.config/helix/languages.toml`.

## Emacs (eglot)

```elisp
(add-to-list 'eglot-server-programs '(sql-mode . ("slaw" "lsp")))
```

## VS Code, Zed

Both need a small extension that launches `slaw lsp` for SQL files; not
written yet (see `docs/plans/spike-editor-lsp-findings.md`).
