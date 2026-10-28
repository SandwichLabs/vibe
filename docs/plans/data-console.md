# Data console and SQL autocomplete

Status: **Part A built (#256, 2026-10-28); Part B (the console) is a proposal; R0 automated Linux runs done 2026-10-28; Linux human checks and macOS pending the owner (xterm.js recommended, provisional; go-libghostty no), see [`spike-console-r0-findings.md`](spike-console-r0-findings.md).** Tracking (label `data-console`): epic #255 covers autocomplete #256, the query editor follow-ups #5, and R0-R5 as #257-#262. It has two parts. **Autocomplete** (A1-A3) is small, independent and worth doing first. The **data console** (R0-R5) is a DuckDB-CLI-style REPL in Studio, drawn with a terminal widget (xterm.js after R0), that can become the script-like front of the Query Explorer. Neither part is in the 12-month plan ([`answers-you-can-check.md`](answers-you-can-check.md)), and neither blocks it. Both run through the same query path that receipts (#241) will hook.

## Feasibility in one paragraph

Both are feasible with no new trust surface, provided one rule holds: **the terminal is a renderer for Vibe's own Go REPL, never a shell.** There is no PTY, no subprocess and no `duckdb` binary. The terminal sends keystrokes to Go, and Go writes VT bytes back.

The terminal widget was the main R0 risk and it held, with a recommendation still to confirm. `ghostty-web` (Coder, MIT) is libghostty-vt compiled to WASM behind an xterm.js-style API, but it is **not** a drop-in: R0 found five behaviours that differ (paste, theme, Meta, scrollback units, write callbacks), so R0 recommends xterm.js with WebGL, provisional until the owner's macOS run and Linux human checks ([`spike-console-r0-findings.md`](spike-console-r0-findings.md)).

The real work is in three places:

- a REPL engine that reuses the editor's gates and run path, not a parallel one;
- a session-scoped DuckDB connection;
- a good line editor.

Autocomplete is mostly built already: the `internal/lsp` completer is a DuckDB-aware completer that knows clauses, aliases and three-part names. Today only `vibe lsp` uses it. Studio's CodeMirror has no completion extension at all.

Rough size, with one implementer and the review loop: **autocomplete 1-1.5 weeks; console 6-8 weeks.** About half the console time is the line editor and the script mode.

## Part A: autocomplete in the query writer

### What exists

- **`studio/frontend/src/composables/useVibeEditor.ts`** sets up CodeMirror with `@codemirror/lang-sql` for highlighting, plus history and keymaps. There is no `autocompletion()` extension, so nothing pops up. Only notebook SQL cells use it: the Query Explorer (`QueryEditorView.vue`) is still a plain `<textarea>`, so A2 first moves it onto `useVibeEditor` (corrected 2026-10-28; this was #5's first item).
- **`internal/lsp`** has three parts:
  - `Build(ctx, sess)` reads a catalog (connections → schemas → relations → columns, plus `duckdb_functions()` names) from the connected connections.
  - `Snapshot` merges it into `.vibe-local/catalog.json`.
  - `Complete(text, offset, cat)` returns ranked items. It finds the statement under the cursor, the dotted path being typed and the clause (relation vs column position), resolves aliases from the statement's table refs, and quotes names that need it. It is pure Go, with no I/O. `hover.go` gives column types.
- **`ListCatalog`** (`studio/connections_catalog.go`) feeds the Connections rail, one level at a time.

### Options

| Option | How | Verdict |
|---|---|---|
| A. lang-sql schema completion | Turn on `autocompletion()` and pass lang-sql a `schema` object built from `ListCatalog` | Quick, but a second completer. It is a generic SQL dialect with weak alias and clause awareness, it needs the whole catalog shipped to the page, and it would drift from `vibe lsp` |
| **B. Go completer behind a binding** | A CodeMirror completion source calls `CompleteSQL(text, offset, connection)`, which runs the `internal/lsp` completer over a live catalog cache | **Recommended.** One completer for Studio, notebooks, the console and `vibe lsp` (Neovim, Helix). DuckDB-aware. The IPC round trip is local and well under a frame |
| C. Run `vibe lsp` as an LSP server for CodeMirror | A CodeMirror LSP client talking to a child process | Not worth it. It adds a process and a JSON-RPC layer to get the same `Complete` call |

### Plan

**A1. Live catalog cache (Go).** Add `lsp.Live`, owned by the workspace session in Studio. It holds a `*Catalog` and rebuilds one connection's entry when that connection's status changes to connected, using the existing `afterRun`/status hooks. It also rebuilds on an explicit "Refresh catalog". It never connects anything: completion does not break "connect on first use". Connections that are not connected fall back to their entry in the saved snapshot, marked stale. Studio then writes the snapshot too, which closes finding 1 of [`spike-editor-lsp-findings.md`](spike-editor-lsp-findings.md), so Neovim and Helix get Studio's catalog for free.

- **Caps:** relations per schema, and columns per relation. A Postgres with 40k tables must not stall a keystroke.
- **Size limit:** the binding is thin. `CompleteSQL` validates its input and calls `a.completer.Complete`.

**A2. CodeMirror source (frontend).** Add `autocompletion()` with an async source in `lib/sqlComplete.ts`, which:

- debounces about 60 ms;
- cancels on the next keystroke;
- maps `lsp.ItemKind` to CodeMirror types (connection, schema, table, view, column, function, keyword) and shows `detail`, such as `prod_pg.public.orders · BIGINT`;
- takes the connection from the editor's `ConnectionSelect`, or from the notebook cell's `connection`, so that unqualified names resolve in that catalog first.

lang-sql's keyword completion stays as the fallback when no workspace is open. Tab or Enter accepts, Ctrl+Space opens the list, and the existing Cmd+Enter (run) keeps priority. The list is themed with `--vibe-*` roles.

**As built (#256, 2026-10-28).**

- The Query Explorer was still a `<textarea>`; it moved onto CodeMirror first (`components/SqlEditor.vue` over `createVibeEditor`, Ctrl/Cmd+Enter through the editor keymap).
- `lsp.Live` (`internal/lsp/live.go`) is the live catalog and `CompleteSQL` (`studio/app_complete.go`) the thin binding: one request struct in, `{from, items, stale, building}` out. Offsets cross the bridge in UTF-16 code units and Go converts to bytes.
- The catalog feed is Studio's `App.catalogChanged` (the one place `catalog:changed` is emitted), not a session hook: `internal/session` has no connect or run event. `Live` rebuilds only connections that are connected, debounced, and never connects one.
- Caps live in the `information_schema` query itself (2000 relations per schema, 512 columns per relation, 250k rows).
- `lib/sqlComplete.ts` is the one CodeMirror source for both surfaces; the lang-sql keyword list is its fallback.
- A3 was not built (hover, snippets, `$param` names, `metric()`). Follow-up (#266): lex dollar-quoted strings, `E'...'` strings and `$word` as single tokens in `internal/lsp/lex.go`, to land with `$param` completion (the console in Part B wants it too).

**A3. Polish (optional, not built).**

- Hover for a column's type and a function's signature, through `lsp` hover.
- Snippets for `COPY … TO`, `PIVOT` and `SUMMARIZE`.
- Completion of `$param` names inside an artifact's named queries.
- Later, completion of `metric('…')` names when the semantic layer (#244) lands.

**Not in scope:** AI inline ghost-text suggestions. They would send every keystroke's context to a provider. If they are wanted, they are a separate opt-in feature behind the existing AI toggle. Completion stays catalog-only and works without AI.

**Tests:**

- Go table tests on `Live`: rebuild on a status change, caps, a stale fallback, and no connect side effect.
- A Vitest test that the source maps items and cancels.
- Rodney in the real app: typing `SELECT  FROM prod_pg.pu` lists `public`, and `o.` after `orders o` lists the columns.

## Part B: the data console (REPL)

### What it is

It is a terminal pane in Studio that behaves like the `duckdb` CLI's interactive mode, but runs against the workspace hub:

- you get a `revenue-ops ▸ D` prompt;
- multi-line SQL ends at `;`;
- results are duckbox tables (DuckDB's boxed format, showing the first and last rows with an elision row between them);
- the closed set of dot commands is listed below;
- you get history and Tab completion;
- Ctrl+C cancels the running query;
- session state lives as long as the tab, so temp tables, `USE prod_pg`, and display settings such as `.mode` and `.timer` persist.

It is a person's surface, with the same rights as the Query Explorer, and the agent never reads it.

### Why not run the real `duckdb` CLI

The hub is an in-memory DuckDB inside the Studio process:

- connections are attached lazily through `internal/session`;
- credentials come from the keychain as DuckDB secrets;
- hardening is applied through the connector DSN, including `lock_configuration`.

A separate `duckdb` process sees none of that. Giving it the connections would mean handing credentials to a child process, and its `.shell` and `.system` commands would give the webview a shell. So we keep the CLI's *behaviour* and reimplement it on the hub.

### What "libghostty in the app" can mean

| Path | What it is | Verdict |
|---|---|---|
| 1. `ghostty-web` in the webview | libghostty-vt compiled to WASM, with an xterm.js-style JS API, drawn in the Studio page. npm `ghostty-web` 0.4.0, MIT. The npm ESM is 682 KB with the 423 KB WASM inlined as base64 (the raw `.wasm` is the "about 400 KB"); +184 KB gzip over Studio's JS. It builds from Ghostty source with a small patch, and the project says it will move to the official libghostty WASM build once there is one | **R0 result: works in WebKitGTK, not chosen.** WASM loads under Studio's CSP (frame-src only, no change) on both Linux origins. By the plan's rule it fails on real paste in bracketed mode (the everyday click-then-Ctrl+V path is raw) and is PARTIAL on in-place theme and on Alt as Meta; 10k-line frame pacing on native Wayland is a marginal miss and the default `scrollback` is a byte unit ([`spike-console-r0-findings.md`](spike-console-r0-findings.md)). Revisit if those are fixed upstream. If a `script-src` is ever added, it must carry `'wasm-unsafe-eval'` |
| 2. Native libghostty surface | Embed Ghostty's GPU-rendered surface in a native child view, as the page windows do with WebKitGTK and WKWebView (`studio/pagewindow_*`) | **Not now.** There is no stable, tagged embedding API for the full surface (libghostty-vt is the part that is out). It means per-platform native code layered over a webview, plus focus and IME juggling, to draw text the webview can already draw. Revisit if libghostty ships a stable surface API and the webview path proves too slow |
| 3. `go-libghostty` in Go | Mitchell Hashimoto's cgo bindings to libghostty-vt: terminal state, a screen formatter and key encoders | **Optional, tests only.** It could feed the REPL's output into a real VT state and assert on screen text (golden screens). Cost: Zig ≥ 0.17 and CMake in CI, and the README makes no promise of API stability. **Recommended by R0: no.** It builds here in 2 minutes with Zig 0.17.0 and works, but it needs a Ghostty source build in CI and promises no API stability; byte-level golden tests stay |

`@xterm/xterm` 6.0.0 with `addon-fit` and `addon-webgl` (DOM renderer as the fallback) is the recommended widget for R2, replacing path 1 (owner to confirm). R0 measured it on WebKitGTK: 10k lines at p95 17 to 18 ms, live theme switch, wrapped paste, Alt as Meta. It needs a Ctrl+Shift+C / Cmd+C binding of our own. It weighs +446 KB raw, +113 KB gzip.

### Architecture

```
Vue  ReplTerminal.vue ── useReplTerminal.ts ── xterm.js Terminal (WebGL)
        │ keystrokes, paste, resize                ▲ VT bytes (coalesced ~16 ms)
        ▼                                          │
Wails  ReplOpen / ReplInput / ReplResize / ReplClose      event repl:out:<id>
        │  (thin bindings, studio/app_repl.go)     │
        ▼                                          │
Go     internal/repl.Session ── line editor ── statement splitter ── dot commands
        │                                                    │
        ├── gates: usersql.CheckManaged + CheckParsed (the editor's, unchanged)
        ├── run:   engine path on a pinned *sql.Conn (lazy attach + status as RunQuery)
        └── out:   renderers (duckbox, box, line, csv, json, markdown) → engine.TerminalText
```

**`internal/repl`** is new. It depends on `engine`, `session`, `usersql` and `lsp`, and is unaware of Wails, so the TUI and a CLI can drive it too. Its parts:

- **`Session`** owns an `io.ReadWriter` pair, a size, one pinned `*sql.Conn` from the hub pool, settings (mode, timer, maxrows, maxwidth, nullvalue) and history. At most 4 sessions per workspace, because each one holds a pool connection.
- **The splitter** accumulates lines until a `;` outside strings and comments. The lexer already in `internal/lsp` (`lex.go`) knows strings and comments; it needs dollar-quoted strings added. Before running, `duckdb_extract_statements` gives the authoritative split, as the agent policy does.
- **The renderers** follow the DuckDB CLI's formats, duckbox by default, with column types under the headers and head/tail elision at `maxrows`. Every value goes through `engine.TerminalText`, which the TUI already uses (`internal/tui/sanitize.go`). Data can never emit an escape sequence, so an OSC 52 clipboard write, an OSC 8 link or a title change from a cell is impossible. The REPL itself emits only a fixed set of SGR colours, mapped to the theme.
- **Cancel:** Ctrl+C cancels the statement's context, and duckdb-go interrupts the query. Closing the workspace cancels every session before it takes the write lock.

**Engine change:** `engine.Manager` gets an `ExecuteOn(ctx, conn, connection, sql, params, maxRows)`, so a pinned connection still goes through the lazy attach, the status snapshot and `afterRun`. Without this, the console would be a second run path, and receipts (#241) would miss it. Checked: the hardening options are set through the connector DSN (`session/hardening.go`), so they are database-wide and every pooled connection carries them. The connector's per-connection init only re-enters a moved default catalog, so a console's own `USE` stays local to its pinned connection. `lock_configuration` means DuckDB `SET` of locked options fails in the console exactly as it does in the editor. Display settings are REPL-side dot commands, never `SET`.

**Bindings** go in `studio/app_repl.go`, each under the 25-statement thin-binding rule:

- `ReplOpen(connection) → id`
- `ReplInput(id, data)`
- `ReplResize(id, cols, rows)`
- `ReplClose(id)`

Output goes out on `repl:out:<id>` events, coalesced, with a per-frame byte cap and backpressure (the Go writer blocks when the frontend's acknowledged offset falls too far behind). Input is capped per call.

**Frontend:** `components/ReplTerminal.vue` plus `composables/useReplTerminal.ts`, within the 600/150-line view limits. The terminal theme is built in JS from `getComputedStyle` over the `--vibe-*` roles (background, foreground, cursor, selection and the 16 ANSI slots from the chart and status roles), so it follows the four built-in themes and user themes without hex in a stylesheet. Copy and paste use bracketed paste; Ctrl+Shift+C (Linux) and Cmd+C (macOS) are bound to the clipboard and Ctrl+C stays `^C`, and the theme is set through `options.theme` (xterm.js recolours live). The font is `--sh-font-mono`.

### Dot commands: a closed set

| Kept (as in the DuckDB CLI) | Vibe-specific | Refused, with a one-line reason |
|---|---|---|
| `.help`, `.mode <duckbox\|box\|line\|csv\|json\|markdown>`, `.timer on\|off`, `.maxrows N`, `.maxwidth N`, `.nullvalue S`, `.tables [pattern]`, `.schema [table]`, `.columns`, `.headers`, `.clear`, `.exit` (closes the tab) | `.conn [name]` (switch the default catalog, a `USE`), `.read <query>` (a saved `queries/*.sql` by name, run through the gates), `.save <name>` (save the last statement as a query through `queries.Store`), `.grid` (open the last result in the Query Explorer grid), `.export csv\|parquet` (the editor's `ExportQueryResults` with the native save dialog) | `.shell`, `.system`, `.open`, `.load`, `.cd`, `.import` (use Import), `.output` and `.once` to a path (use `.export`), `.log`, `.excel` |

Anything not in the table is "unknown command", never passed through.

### History and credentials

History is machine-local in `.vibe-local/repl/history`. It is git-ignored and never in a bundle.

The editor's guard allows `ATTACH`, and an attach string can hold a password. So **statements that are `ATTACH`, `DETACH`, `SET` or `PRAGMA`, and anything that failed the gates, are never written to history.** The classifier is the parsed statement type, not a prefix. A test covers an `ATTACH 'postgres:...password=...'`. `CREATE SECRET` is already refused by `CheckManaged`.

DuckDB error text is shown as the editor shows it today, through the same path. A redaction gap there would be a bug in both places, not a new one.

### The script-like query writer

"Replace the plain query writing interface" is best read as **script + console**, not as a terminal instead of an editor:

- The Query Explorer gets a **Console** pane under the CodeMirror editor, replacing the result grid as the default output.
- Cmd+Enter runs the statement under the cursor *in the console's session*, so temp tables and `USE` carry over. Shift+Cmd+Enter runs every statement in order, and running a selection runs the selection.
- Each run is echoed into the console with its output, which gives a transcript like `duckdb < script.sql`.
- Typing directly in the console works too.
- The grid stays, one keystroke away (`.grid`, or a "Show as grid" action on the last result). Terminals are bad at results with forty columns, at selecting cells, and as the source for CSV and Parquet exports.
- Saved queries stay plain `queries/*.sql` files. A script is just a query file with several statements, so there is no new format.

Whether the console becomes the default output is an owner call after a week of use (decision 1).

### TUI and CLI parity

`internal/repl` runs over any `io.ReadWriter`, so two things come almost free:

- **`vibe repl`:** the same console on a real TTY through `golang.org/x/term` raw mode. It mirrors the Studio console, which keeps the "CLI mirrors the UI" rule. The REPL is not an agent command.
- **The TUI's REPL mode** (`internal/tui/repl.go`) can adopt the same renderers, so there is one duckbox formatter instead of two.

### Phases

| Phase | Work | Exit criteria |
|---|---|---|
| **R0 spike** (3-5 days) **Automated Linux runs done 2026-10-28** ([`spike-console-r0-findings.md`](spike-console-r0-findings.md)); Linux human checks (dead keys, IME, physical input) and macOS pending the owner | `ghostty-web` (and xterm.js) in a throwaway Studio route, run against WebKitGTK (Linux) and WKWebView (macOS), with the same harness on xterm.js | Renders and themes. Dead keys, IME composition, Cmd/Ctrl shortcuts, copy, bracketed paste and resize all work. 10k lines of output scroll smoothly. WASM loads under Studio's CSP. Bundle size recorded. Decision recorded: ghostty-web or xterm.js, and go-libghostty for tests or not |
| **R1 engine** (1-1.5 wk) | `internal/repl` (session, splitter, dot commands, renderers, history filter, cancel) and `engine.ExecuteOn`. The line editor is `x/term`'s `Terminal` (it works over any `io.ReadWriter`: history, a completion callback, single line, with multi-line made by accumulating) | Golden byte tests per renderer. A fuzz test that no cell value emits ESC/OSC/CSI. History filter tests. Cancel interrupts a long `range()` query. Run-path parity test: the same statement via `RunQuery` and via the console takes the same gates and status updates |
| **R2 Studio console** (1 wk) | Bindings, events and backpressure. `ReplTerminal.vue`. A **Console** tab in the Query Explorer. Tab completion through `lsp.Live` (A1) | Rodney: open the console, `.tables`, a federated join, Ctrl+C, a theme switch, closing the workspace mid-query. Thin-binding, Vue-size and ast-grep checks green |
| **R3 script mode** (1 wk) | Editor-to-console run (statement, selection, all), `.grid`, `.save`, "Show as grid" | Rodney: a three-statement script with a temp table runs top to bottom, and the grid opens on the last result |
| **R4 line editor** (1-2 wk) | Replace `x/term` with Vibe's own editor: multi-line editing, syntax colouring from the `lsp` lexer, a completion popup drawn in the terminal, Ctrl+R history search | Behaviour tests for each key binding, plus a Rodney pass |
| **R5 parity and hooks** (1 wk) | `vibe repl`, the TUI adopting the renderers, `docs/reference/console.md` and the architecture tree. Later, as the epics land: receipts for console runs (#241), `.asof` (#246), `metric()` (#244, free through the shared pre-pass) | `internal/docsref` and the docs updated |

The order is **A1-A2 first** (it pays off in the editor, the notebooks and later the console), then R0, and stop if R0 fails on both widgets.

### Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| `ghostty-web` is young (0.4.x, a patched Ghostty build) | Resolved by R0 | xterm.js recommended (provisional); ghostty-web stays a later option behind the same adapter shape |
| WebKit quirks (IME, focus, canvas sizing) in WebKitGTK and WKWebView | Medium | R0 ran WebKitGTK in a private compositor (rendering, input, resize, 10k lines all fine for xterm.js; no IM layer there). Dead keys, IME and everything on WKWebView are owner checks in the findings; do not start R2 before the macOS run |
| A second run path creeps in (no receipts, different gates) | Medium | `engine.ExecuteOn` plus a run-path parity test in R1 |
| Pinned connections exhaust the hub pool | Low | At most 4 sessions. Idle sessions release their connection and keep only display settings, with a "session reset" notice |
| A credential in history | Low | The parsed-type history filter and its test. History is local only |
| Escape injection from data | Low | `engine.TerminalText` on every value, plus the fuzz test |
| Wide results in a terminal | Certain | `.grid` and `maxwidth` truncation, as the DuckDB CLI does |

### Non-goals

- A shell, a PTY or any subprocess behind the terminal. Studio never hosts a shell.
- Running the `duckdb` binary.
- The agent reading, typing into or being notified by the console. A later "send this result to the agent" would be a person's action through the existing panel.
- An MCP tool for the console.
- A native libghostty surface (path 2), until a stable embedding API exists.
- Windows (as in `direction.md` §7).

## Owner decisions

1. **Console as default output** in the Query Explorer, or a tab beside the grid. *Default: a tab in R2; decide after R3 use.*
2. **ghostty-web or xterm.js.** *Recommended by R0 (2026-10-28), owner to confirm after the Linux human checks and the macOS run: xterm.js 6.0.0 + fit + webgl.* By the plan's rule ghostty-web is FAIL on row 8 (real paste) and PARTIAL on rows 3 (theme) and 6 (keys); row 10 is a marginal miss and does not decide. xterm.js is PARTIAL on row 6 (two app key bindings) and passed the rest that could be run. See [`spike-console-r0-findings.md`](spike-console-r0-findings.md).
3. **go-libghostty in CI** (adds Zig). *Recommended by R0: no; byte-level golden tests.* It builds and works (2 minutes, Zig 0.17.0) but is not worth a Ghostty source build in CI.
4. **History persistence.** *Default: on, machine-local, filtered as above.*
5. **`vibe repl` as a CLI command.** *Default: yes in R5; it mirrors the UI.*

## Sources

- [libghostty is coming](https://mitchellh.com/writing/libghostty-is-coming) (Mitchell Hashimoto): libghostty-vt as a zero-dependency VT parser and state library.
- [coder/ghostty-web](https://github.com/coder/ghostty-web): the WASM build of libghostty-vt with an xterm.js-compatible API. npm `ghostty-web` 0.4.0, MIT, checked 2026-10-27.
- [`spike-console-r0-findings.md`](spike-console-r0-findings.md): the R0 results matrix, API gap table, bundle sizes, go-libghostty experiment and the owner checklists.
- [go-libghostty](https://next.tangled.org/mitchellh.com/go-libghostty): cgo bindings to libghostty-vt, with no stability promise yet.
- [awesome-libghostty](https://github.com/Uzaaft/awesome-libghostty): other embedders.
