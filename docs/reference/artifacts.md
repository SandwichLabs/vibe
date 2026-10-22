# Data artifacts

A **data artifact** is a computation plus a view, stored as plain files in the workspace. It stores questions (SQL and parameters), never answers: every query runs when the artifact opens and again when a parameter changes.

```
artifacts/<name>/
├── artifact.yaml        title, typed parameters with defaults, NAMED queries over them
└── view.a2ui.json       a declarative A2UI view (closed schema, validated)    (view: a2ui, the default)
    or view.js           a custom view, run in a worker after you allow it     (view: script)
    or html/             an HTML page (templates, CSS, JS, assets)             (view: html; static pages render in Studio)
```

An artifact has exactly one view, the one its `view:` names (a file, or for `view: html` the `html/` folder). Both existing, or the wrong one for the kind, makes the folder broken (it lists with an error). There is no fallback A2UI page for a script view: before you allow it, the artifact opens as source. See [Custom views](#custom-views-view-script).

The folder name is the artifact's name. This page describes the file formats and the view rules; the plan and its decisions are in [`../plans/data-artifacts.md`](../plans/data-artifacts.md).

## The manifest: `artifact.yaml`

```yaml
version: 1
title: Regional sales
description: Revenue by region, with drill-down to customers.
parameters:
  - name: from
    type: date
    default: today-90d
    label: From
  - name: to
    type: date
    default: today
  - name: region
    type: enum
    options_query: regions
    default: ""
queries:
  - name: regions
    connection: prod_pg
    sql: SELECT DISTINCT region FROM sales.orders ORDER BY 1
  - name: by_region
    connection: prod_pg
    sql: |
      SELECT region, sum(amount) AS revenue
      FROM sales.orders
      WHERE order_date BETWEEN $from AND $to
      GROUP BY ALL ORDER BY revenue DESC
view: a2ui
```

The file is decoded strictly: an unknown key is an error, and there is no `${ENV}` interpolation. Parameter and query names start with a letter, then letters, digits and underscores (at most 64 characters), and are unique without regard to case. A query takes up to 64 KB of SQL and an optional `connection`. Every `$name` in a query must be a declared parameter; which parameters a query uses is derived from its SQL. A value is bound as a parameter and never becomes SQL text.

### Parameter types

| Type | Value | Constraints |
|---|---|---|
| `text` | string | `max_length` (default and cap 1,000) |
| `number` | number | `min`, `max` |
| `integer` | whole number within +/-2^53 | `min`, `max` |
| `date` | `YYYY-MM-DD` | none; default may be relative (below) |
| `boolean` | `true` or `false` | none |
| `enum` | string | exactly one of `options` (up to 200) or `options_query` (a query whose first column supplies the options; it may not use the parameter it feeds) |

Every parameter has a `default`, so an artifact opens without input. In a query, write `$from::DATE` where a date is used in a function or arithmetic; a plain comparison (`BETWEEN $from AND $to`) needs no cast.

### Date defaults

A `date` default is a literal `YYYY-MM-DD` or one of a closed set, resolved on the local clock each time the artifact runs: `today`, `today-<n>d` (1 to 3660 days back), `start-of-month`, `start-of-year`.

### Caps

`artifact.yaml` 256 KB, `view.a2ui.json` 1 MB, `view.js` 1 MB (valid UTF-8, not empty, and none of the characters that can make the code differ from what is shown: control characters other than tab, newline and carriage return, DEL, C1 controls, the bidi controls U+202A-202E and U+2066-2069, the line and paragraph separators U+2028/2029 (JavaScript ends a line comment at them), U+200E/200F, U+061C, the soft hyphen U+00AD, the zero-width characters U+200B-200D, U+2060 and U+FEFF, and U+FFF9-FFFC; `artifact.yaml` refuses the same controls, bidi characters and separators), 20 queries, 50 parameters, 200 view components.

## The view: `view.a2ui.json`

The view is an [A2UI](notebooks.md#a2ui-component-reference) page, `{ title, components[] }`, validated against its manifest. The schema is closed (unknown fields are errors) and no view field is a color, style or class: labels and column names are shown as text only. Errors are JSON-path-prefixed, as for any A2UI page.

- **Data comes only from named queries.** A data-bound component (`stat`, `table`, and the charts) takes `query: "<name>"` instead of `sql`. `sql` and `connection` are refused in a view (the query holds the connection). `query` must name a query of the manifest. Inline data (`rows`, `bars`, `points`) is still allowed.
- `refresh` (a timer re-run of a `sql` binding) is refused on a query-bound component: a view's queries re-run when a parameter they use changes.
- **The view-only types** (listed in the [component reference](notebooks.md#artifact-view-components)) are refused in notebooks, chat and `create_dashboard`.
- Every other component type behaves as in [`notebooks.md`](notebooks.md).

### Controls

Each control names its parameter; the manifest holds the semantics (type, options, default).

| Control | Fields | Parameter type |
|---|---|---|
| `select` | `param`, `label` | `enum` (options static or from `options_query`) or `boolean` |
| `dateRange` | `start`, `end`, `label` | both `date` |
| `numberInput` | `param`, `label` | `number` or `integer` |
| `textInput` | `param`, `label` | `text` |

A control whose parameter is not declared, or is of another type, is a validation error. Changing a control re-runs only the queries that use its parameter.

### `pivot`

`{query, rows, columns, value, label?, drill?}` lays out a **long-form** result (one row per row-key and column-key, already aggregated with `GROUP BY` in the query) as a crosstab: `rows` (1 to 8 result column names) are the row headers, `columns` names the column whose values become the column headers, `value` the column shown in each cell. The three roles must use different columns. The component never aggregates: totals come from the SQL (for example `GROUP BY ROLLUP`) and are shown as returned. Two result rows for the same cell is a render error that says to group by those columns. Column names are checked against the result at render, since they are only known after a run. A null key reads `(blank)` and an empty-string key `(empty)`; rows and columns keep the order of first appearance in the result, which the query's `ORDER BY` controls. The grid is a real `<table>` (a column-group header, column headers, row headers); every header and cell is plain text, and a long header is clipped and carries its full text as a tooltip. A truncated result (500 rows) is labelled above the grid.

### Drill-down

`drill: {param, column}` on `table`, `barChart`, `columnChart` and `pivot`. The component must be bound with `query` (a `drill` on inline data is a validation error): clicking a row sets `param` (any declared parameter) to that row's `column` value, validated like any other value, and re-runs the queries that use it. `column` is checked against the result at render: a column that is not there shows an error and turns drill off for that component.

The clicked row's value is offered to the parameter exactly as a control's value is: it must pass the parameter's own rules (a value outside a static `enum`, a number past `max`, a NULL, all refused). A refused value shows the parameter's inline error (and a note under the component), changes nothing and runs nothing. A number is read as text for a `text` or `enum` parameter, and a timestamp as its date for a `date` parameter. The parameter's control shows the drilled value and its **Reset**. The row whose value is the parameter's current value is marked (`aria-current`; on a pivot, the row header when the drill column is a row key); nothing is marked when every row matches. A component keeps showing its previous result while its query re-runs, so keyboard focus stays where it was.

Keyboard: a drillable table row, bar, column or pivot cell is reached with Tab (one stop per component) and the arrow keys, and **Enter** or **Space** drills. On a `pivot`, drill is on the cells (a cell is one result row); on a `table`, the first cell of each row holds the button.

## In Studio

**Artifacts** in the sidebar lists every artifact (`#/artifacts`); a folder that cannot be read lists with its error. Opening one (`#/artifacts/<name>`) shows the view with its controls. Each named query runs once for every component bound to it, and a changed parameter re-runs only the queries that use it. A bad value shows an inline error and runs nothing; **Reset** restores a parameter's default. Results are capped at 500 rows (the view says so when a query hit the cap). A broken artifact opens to its errors, and the list and view update when either file changes on disk (an editor, git, an agent over MCP).

**New** (on the list) asks for a name and creates `artifacts/<name>/` from a small working example (two date parameters, one query, a date-range control and a stat); a name already in use is refused. The name is 1 to 64 lowercase letters, digits, `-` or `_`.

The **Source** tab edits the two files as text in a code editor (**Save**, or Ctrl/Cmd+S). It shows the files as they are on disk, so a broken artifact can be opened there and fixed. Save writes both files exactly as typed (comments and layout in `artifact.yaml` are kept) and sends the one revision over both. What happens depends on what is wrong:

- **A structural error** (a bad manifest, a view that does not match it, a file over its cap) refuses the save and says why; nothing is written.
- **A query the agent's read policy refuses** (a write statement, a parameter used as a file path, an unknown connection) does not refuse the save: the artifact is saved, and the policy's reason for each query is listed as a warning. The run-time gate is the same either way, and a refused run shows only a fixed message, so this list is where the reason is. A person must be able to save while a source is offline.
- **A stale revision** (either file changed on disk since it was opened, for example by an agent's `write_artifact`) is a conflict and writes nothing. The conflict banner offers **Reload from disk**, **Keep mine** (replaces only the revision the banner was raised against; a file that changed again is a fresh conflict) and **Save mine as a copy** (`<name>-copy`, then `<name>-copy-2` and so on; never over an existing artifact). An outside change to untouched text is followed quietly. If the artifact was deleted on disk, **Keep mine** writes it back.

The two files are written one after the other (the view, then the manifest), each atomically, and a save refused midway puts the old view back. A crash between the two leaves a pair that fails validation: it lists with its error and does not run, and the Source tab opens it for fixing.

Leaving the Source tab with unsaved text asks first. In a warning, the workspace's absolute path is shown as `<workspace>`. **Keep mine** needs the revision the banner was raised against; without one (and with the artifact present) it is a conflict, and the banner fetches it again. **Save mine as a copy** skips a `-copy` name that is taken or unusable (a link, a plain file) and uses the next.

**Delete** (on the Source tab, after a confirmation) removes both files and the folder, but only if they are still at the revision the tab shows: an artifact edited meanwhile is a conflict and stays.

## Authoring with the agent

The agent (Studio chat or an MCP client) writes artifacts with `write_artifact` and reads them with `list_artifacts` and `get_artifact` (see [mcp.md](mcp.md#data-artifact-tools) and [agent-policy.md](agent-policy.md#data-artifacts)); the built-in `data-artifacts` skill (`read_skill`) teaches the format. Every query is held to the agent's read policy when it is saved and again on every run, so an artifact written by hand, by git or by a bundle is held to it too. The tools never return query results.

## Running an artifact

A run belongs to the person's UI (Studio's `RunArtifactQuery` binding); no agent tool and no MCP method runs one. The steps, in order:

1. The artifact is loaded from the store, so what runs is what is on disk.
2. `artifacts.Bind` turns the query name and the raw values into the manifest's SQL text, unchanged, plus bound parameters. A value of the wrong shape (a date that is not one, a number past `max`, a value outside an `enum`, a name that is no parameter) is refused here.
3. The save-time checks repeat on the statement and its connection (one statement, every `$name` a parameter token, no parameter as the argument of `query(...)`, `query_table(...)` or a file reader), and the read-only SQL policy classifies the statement with the actual values. A policy refusal shows the fixed "live bindings are read-only" text, and its reason goes to the debug log and to the Source tab's warnings. A lexical, binding or connection refusal shows its own short reason (never the SQL).
4. The statement runs read-only in every sandbox tier (a tier changes what the agent may *write*, not what an artifact may), at most 30 seconds, and returns at most 500 rows.

Because step 3 runs every time, a manifest edited by hand, pulled from git or imported from a bundle is held to the same policy as one the agent saved. Nothing is trusted because it "was vetted once".

## Sharing

- **Git.** `artifacts/` is ordinary workspace content; commit it with the rest.
- **Bundles.** A `.vibe` bundle carries artifacts as two entries each (`artifacts/<name>/artifact.yaml` and its view file, `view.a2ui.json` or `view.js`). An allow never travels. Import checks structure only; the SQL is vetted when it runs ([workspaces.md](workspaces.md#sharing-without-git-vibe-bundles)).
- **CLI.** `vibe artifact list [--json]` mirrors the Artifacts list (name, title, view kind, error; never SQL). `vibe artifact view status|allow|deny|revoke <name>` decides on a script view. `vibe export bundle --artifacts a,b` exports. There is no CLI run or write.

## Credentials

An artifact holds no credential. A query names a connection; the connection's sign-in is resolved by the session as for any query. A connection password or `.env` value never appears in an artifact error, in `get_artifact` or `list_artifacts`, in a log line or in a bundle (an end-to-end test audits each, `internal/hub/artifacts_e2e_test.go`; it covers credentialed connections that fail before attaching, since no network or database extension is available to a plain `go test`).

## Custom views: `view: script`

A manifest that says `view: script` draws itself with `view.js` instead of an A2UI page, for charts the A2UI components cannot draw. The spike ([plan](../plans/data-artifacts.md#spike-custom-view-isolation-run-2026-10-17)) ruled out an iframe (a sandboxed frame can reach Wails' binding IPC, [#174](https://github.com/SandwichLabs/vibe/issues/174)) and chose a dedicated, terminable Web Worker: it receives rows only through `run(queryName, params)` and returns either an SVG document string (shown as an inert image) or an A2UI page (validated by Go first). A `view.html` is not a thing: a bundle that holds one is refused as an unknown entry.

**Status.** Built: trust (below), the view host and the bridge ([The view host](#the-view-host-and-the-worker-contract)).

**Nothing runs until you allow it.** The code in `view.js` is written by someone else or by the agent, so the artifact opens as **source**: a read-only code view of `view.js` and of `artifact.yaml`, with **Allow** and **Deny** and the line "This view runs code written by someone else or by the agent. It cannot reach the network or your files, and can only run this artifact's queries."

- **What an allow covers.** Allow is keyed by the artifact folder's canonical absolute path (symlinks resolved; on macOS and Windows spelled as the file system spells it) plus the `ViewHash`: a sha256 over a version-tagged (`vibe-view-hash/1`), length-prefixed list, entry 0 `artifact.yaml`, then each view file as (path, bytes) sorted by path. Any change to `artifact.yaml` (a query's SQL or connection included) or to `view.js` ends the allow: the view says it **changed since you allowed it** and asks again. Bytes moved from one file to the other change the hash. A clone of the folder at another path, or a copy under another name, is undecided.
- **What you saw is what you allow.** Allow sends the hash of the bytes on screen, and Go refuses it if the files differ by then. If the files change while you are reading (a reload with a different hash while Allow is armed), Studio keeps showing what you were reading, says "This view changed while you were reading it", and keeps Allow off until you click "Show the new version". The read-only editors draw any remaining control or invisible character as a visible marker, and the CLI prints them as `<U+XXXX>`. The view host serves the bytes it hashed (held in memory under the view's token), never a second read.
- **Deny** holds for that folder until revoked, across edits.
- **Where decisions live.** In `views.json` beside `dotenv.json` in your own config directory (the same JSON format as `dotenv.json`). Never in the workspace folder, never in a bundle, never in git. **Settings > Workspace > Custom views** lists them with **Revoke**.
- **Agent edits ask again.** The agent can write a script view (`write_artifact`'s `view_source`) but cannot allow one: there is no tool, MCP method or other path that allows, denies or revokes. Every edit, by anyone, asks again.

**CLI.** `vibe artifact view status|allow|deny|revoke <name>` mirrors the prompt and Settings (as `vibe workspace env` does for `.env`). `status` prints the state, the hash and the source. `allow` shows the source and asks on a terminal; without one it needs `--hash <hash from status> --yes` (you read the source) and is refused if the files changed since. That is scriptable by design: a process that can run your shell can also edit `views.json` directly, so an agent with a shell is outside this feature's threat model. What the feature stops is the agent's tools, MCP and a pulled file allowing a view; it does not stop a program running as you. `vibe artifact list` shows the kind (`script`).

**Sharing.** A script view travels in a bundle as `artifacts/<name>/artifact.yaml` plus `artifacts/<name>/view.js` (the view file must be the one the manifest names), and arrives undecided on the other machine.

**Writing one.** The `data-artifacts` skill (what the agent reads) and [the contract below](#the-view-host-and-the-worker-contract) describe it: define `draw`, call `vibe.run(queryName, params)` for rows, return an SVG string or an A2UI page.

## The view host and the worker contract

An allowed view runs in a dedicated Web Worker, never an iframe. Studio's Go side serves it, a Vue host (`CustomView`, `useViewBridge`) talks to it, and nothing else reaches it.

### What Studio shows

The artifact opens to the view: a control for each manifest parameter (the level-1 controls: `select` for an enum or boolean, `numberInput`, `textInput`, and a `dateRange` for each pair of date parameters, a lone date alone), a **Reset**, and what the view drew. The view draws on open and again after every parameter change (debounced, the latest request wins; a result that arrives for an older request is dropped), when the theme changes and when the width changes. The source stays one click away under **View source**. If the artifact changes on disk, or the decision stops being *allowed* (revoked, denied, edited), the Worker is terminated and the screen returns to the trust prompt. That happens after the file watcher's short debounce; until then the query gate below (every run re-checks the view is still allowed at the hash it was served at) refuses anything from the old Worker. A view that fails to load, or whose draw fails, shows the message the script gave (as text, at most 300 characters); a bridge error is always one of the fixed texts below.

### The contract (what `view.js` sees)

`view.js` is a classic script run by `importScripts` in a Worker. There is no DOM, no `window`, no network, no `indexedDB`, `caches`, `BroadcastChannel`, `localStorage`, `navigator.storage` or `navigator.locks` (a Worker shares Studio's origin, so these are removed before the view loads), no `eval` or `new Function` (the CSP has no `'unsafe-eval'`), no nested worker, and `self.webkit` is undefined. The script's world is `vibe` and one function it defines:

```js
// Define a global draw(); Studio calls it with the current state.
function draw({ params, theme, width, height }) {
  return vibe.run('by_region', { from: params.from, to: params.to })
    .then((res) => ({ svg: '<svg xmlns="http://www.w3.org/2000/svg" ...>...</svg>' }))
}
```

| | |
|---|---|
| `draw({params, theme, width, height})` | Global. Returns, or resolves to, `{svg: string}` or `{a2ui: page}`. `params` is every parameter's current typed value; `width` is the space available in pixels; `height` is 400. A draw over 30 seconds stops the view. |
| `vibe.run(queryName, params)` | Promise of `{columns, rows, truncated}`. Runs one of this artifact's named queries through `RunArtifactViewQuery(token, …)`, which is level 1's `RunArtifactQuery` behind the view's own grant (the artifact is the token's, and the view must still be allowed at the served hash when the query starts and when it returns): the SQL is the manifest's, the values are bound parameters Go re-validates, at most 500 rows and 30 seconds. `params` is a flat object (at most 32 keys named like parameters; values are a string up to 1,000 characters, a finite number, a boolean or null; at most 8 KB of JSON). Rejects with a fixed text. |
| `vibe.lib(name)` | Loads a pinned library by name, from a closed list (below), and returns its global. Synchronous. Any other name throws. |
| `vibe.theme` | Plain strings: `chart.1` to `chart.6` (and `chart`, the six as an array), `fg`, `fg.muted`, `bg`, `bg.alt`, `surface`, `border`, `accent` (hex colours), and `type` (`light` or `dark`). Also passed to `draw` as `theme`. |

Output:

- **`{svg}`**: an SVG document string, at most 2 MB. Studio shows it only as `<img src="blob:…">` with type `image/svg+xml` (revoked when replaced or when the view closes), never in the page's DOM. An SVG image runs no script and loads nothing, so it cannot reach out even if it tries; fonts inside it are the system's (give text a `font-family`), and the page's CSS variables do not apply (use `vibe.theme`).
- **`{a2ui: page}`**: an A2UI page object (the Worker hands it to Studio as JSON text, at most 1 MB), sent to Go and validated with the same context-aware schema as `view.a2ui.json` (`ValidateView` with the manifest's parameters and queries), then rendered by the level-1 renderer with the artifact runtime, so `query`-bound components, controls, `pivot` and `drill` work, and the queries behind them run through the usual lane. An invalid page shows the validation error.

### Libraries

Pinned exact versions, vendored by the frontend build into `dist/vibe-view/lib/` and served from the Studio binary (no CDN), all permissive licences (see `THIRD_PARTY_NOTICES.md`):

| `vibe.lib(...)` | Package | Leaves |
|---|---|---|
| `d3-array`, `d3-format`, `d3-time-format`, `d3-scale`, `d3-shape`, `d3-geo` | d3-array 3.2.4, d3-format 3.1.2, d3-time-format 4.1.0, d3-scale 4.0.2, d3-shape 3.2.0, d3-geo 3.1.1 (their UMD builds, with d3-color, d3-interpolate, d3-time and d3-path as dependencies) | `d3` |
| `vega` | vega 6.4.0, with `vega-interpreter` 2.3.2 bundled as `vega.expressionInterpreter` | `vega` |
| `vega-lite` | vega-lite 6.4.3 (loads `vega` too) | `vegaLite` |

Vega and vega-lite run in the Worker with no DOM and no eval: use the interpreter, and `toSVG()`.

```js
async function draw(ctx) {
  const vl = vibe.lib('vega-lite'), vega = vibe.lib('vega')
  const res = await vibe.run('sales', {})
  const spec = { /* a vega-lite spec with data.values from res.rows */ }
  const view = new vega.View(vega.parse(vl.compile(spec).spec, null, { ast: true }),
    { expr: vega.expressionInterpreter, renderer: 'none' })
  return { svg: await view.toSVG() }
}
```

### Limits and the watchdog

- **Queries:** at most 4 in flight and 60 in any rolling minute; a call over either gets a fixed error and runs nothing. A message that is not exactly `{t: 'run', id, query, params}` with a query named like a manifest query, or that carries anything else (an `sql` field, another artifact's name), is refused the same way; an unknown message type is ignored. A Worker that sends over 100 messages or 16 MB of payload in a second is stopped. A Worker answers each draw once: a repeated or late result is ignored.
- **Fixed error texts** (what `vibe.run` rejects with, never a path, an SQL error or the policy's reason): "That request was not valid.", "Too many queries are running at once.", "Too many queries in the last minute.", "The query could not be run."
- **Heartbeat:** the Worker beats every 250 ms. Studio terminates it after 1 second of silence (4 seconds while it starts; 5 seconds while it parses a library, once per library file; and it must be ready within 15 seconds), or after a draw over 30 seconds, and shows **This view stopped responding** with **Reload**. A script that blocks its thread for over a second (a long synchronous loop, a very large vega chart) is stopped; aggregate in SQL and keep drawing work small. Termination does not touch Studio's UI thread. The watchdog guards liveness, not CPU: a busy loop that yields (async) keeps beating and is stopped only by the 30 second draw limit.
- **Output:** an SVG over 2 MB or an A2UI page over 1 MB is refused with a message.

### How it is served and confined

Go serves the Worker through the Wails asset server's middleware under `/vibe-view/`, with nothing else reachable:

| Path | Serves |
|---|---|
| `/vibe-view/<token>/worker.js` | The host-owned bootstrap (embedded in the binary; it sets up the channel, the heartbeat and `vibe`, then `importScripts` the view). |
| `/vibe-view/<token>/view.js` | The artifact's `view.js`, the exact bytes that were hashed. |
| `/vibe-view/lib/<file>.js` | One pinned library. |

A **token** is 32 random bytes, issued by `OpenArtifactView(name, origin)` only for a view that is *allowed right now*, and bound to that artifact, its `ViewHash` and the open workspace for this Studio process. The view's bytes are read once when the token is issued, hashed together with `artifact.yaml`, compared with the allowed hash and held in memory; those held bytes are what `view.js` serves. Each request for a token's files also re-checks that the decision still allows exactly that hash: a changed, revoked or denied view gets 404 and the token is dropped (it never serves the new bytes, and never a mix). A new token for an artifact replaces its older ones; at most 32 tokens are held. A token for one artifact cannot fetch another's files.

Every response under `/vibe-view/`, errors included, carries `Content-Security-Policy`, `X-Content-Type-Options: nosniff` and `Cache-Control: no-store`. The Worker's own response (its `worker.js`) carries:

```
default-src 'none'; script-src <origin>/vibe-view/<token>/view.js <origin>/vibe-view/lib/;
connect-src 'none'; worker-src 'none'; img-src 'none'; base-uri 'none'; form-action 'none'
```

`<origin>` is the page's own origin, which the page passes in (`location.protocol//location.host`: `wails://wails.localhost:34115` in `task studio:dev`, `wails://wails` in a built app on Linux and macOS, `http://wails.localhost` on Windows); Go accepts only a plain `http`, `https` or `wails` origin and puts nothing else in a header. A path source needs a host, so `'self'` could not be narrowed to these paths. The spike showed the Worker's own response CSP is what holds `connect-src 'none'`: `fetch`, XHR, WebSocket, EventSource, `importScripts` of anything but those paths, and nested workers are all blocked. No path ends in `/` or `index.html` (Wails would inject its runtime into it), and a request under `/vibe-view/` that would match that injection is answered 400, never 404 (Wails would swap a 404 for its own page). There is no directory listing, a path with `..`, `//` or a backslash is 400, nothing from the workspace is reachable except through a token, and only `GET` and `HEAD` are accepted.

The Worker holds one `MessagePort`, handed over in its first message and kept in the bootstrap's closure; every later message uses it, never a broadcast. Studio's page owns the artifact name for every `run`.

## HTML pages: `view: html` (static pages render in Studio)

> **Status.** The file set, the manifest block, the save-time vet, the trust hash, **the renderer and the static host** exist. A *static* page, once the person allows it, renders inside Studio's window (see [Rendering and the static host](#rendering-and-the-static-host)). A *scripted* page can be allowed but waits for its own page window (not built yet, P4). The agent cannot read or write a page (it is shown a note), a bundle skips it, and the full allow prompt (queries and capabilities listed, a CLI that lists them) is step P6. Plan: [`../plans/html-artifacts.md`](../plans/html-artifacts.md).

An HTML page is a Go `html/template` page of your own markup, with optional CSS, JS and assets, whose only data source is the manifest's named queries. Like every artifact it stores questions, never answers.

```
artifacts/<name>/
├── artifact.yaml
└── html/
    ├── index.html.tmpl     the entry template (html.entry); other *.tmpl files are partials
    ├── app.js              optional; any .js/.mjs file makes the page "scripted"
    └── assets/…            style.css, images, fonts, data; at most assets/<folder>/<file>
```

```yaml
view: html
html:
  entry: index.html.tmpl        # default; a .tmpl directly under html/
  window: {width: 1200, height: 800}   # scripted pages only; 200 to 4000
  capabilities:                 # scripted pages only; the closed set below
    state: {scope: local}       # scope is "local" (the only one built)
    download: {}
    ask: {}
```

The block is strict like the rest of the manifest: `html:` only goes with `view: html`, an unknown key or capability is refused, and **capabilities and a window need script** (a static page declares none, so the manifest never promises something the page's host does not offer).

### The file set

A folder that breaks any rule below lists with its error and does not run; nothing is repaired.

- Extensions: `.tmpl` (only directly under `html/`), `.js`, `.mjs` (directly under `html/` or in `assets/`), and `.css`, `.svg`, `.png`, `.jpg`, `.webp`, `.woff2`, `.json` (in `assets/` only, so nothing hashed is unreachable). Names use letters, digits, `.`, `_`, `-`; no dotfile (`.DS_Store` and `Thumbs.db` are skipped: not hashed, not served, not an error), no symbolic link (files are opened without following one), no duplicate path (case-insensitive). A stale `html/` folder beside a manifest that is not `view: html` is ignored until the artifact becomes a page; then its files are replaced or removed with the rest.
- Layout: files directly in `html/`, in `html/assets/`, or in `html/assets/<folder>/`; no other folder. `_vibe` and `vibe-lib` are reserved for the host.
- Caps: 200 files, 5 MB in all, 1 MB per file, 256 KB per template. Text files are UTF-8 with no control, bidi or invisible characters; images and fonts must start with their format's magic bytes; `.json` must be one JSON value; an SVG must be inert: a script, an event handler, a `javascript:` URL, a DOCTYPE or entity, a processing instruction other than the XML declaration, a `foreignObject`, `set`/`animate`, a link or `url()` that is not a `#fragment` or a literal `assets/` path are all refused.
- One `Rev` covers `artifact.yaml` and every file (path and bytes, in path order), so a change to any of them is a conflict for a stale writer. A change on disk under `html/**` is reported by the file watcher as a change to the artifact.

### Static or scripted

The kind is **derived**, never declared: a page is *scripted* if `html/` holds any `.js`/`.mjs` file, a template holds a `<script>` element (also in `<svg>`, or inside an `{{if}}` branch or a partial), or any element has an `on*` attribute. Otherwise it is *static*. Templates are read as markup with every `{{ ... }}` replaced by a marker (both branches of `{{if}}` and `{{range}}` stay visible), using the HTML tokenizer `html/template` is built on. A `{{template}}` call is replaced by the callee's own markup **at the call site**, because a partial runs inside its caller's context (it may close the caller's `<style>`, or open an `<svg>`); a `{{range}}` body is read twice, since it may repeat; if `<svg>` or `<math>` appears anywhere, the whole page is read as foreign content from the first byte. A call cycle, or a page whose expansion (all templates together) is over 5000 calls or 2 MB, is refused, and a template the entry never reaches is read on its own. A template that cannot be read that way is refused: an action inside a tag name or between a tag's attributes (so a tag cannot be spelled by data or split across partials), an action right after `<`, a template that ends inside a tag, and any HTML comment (use `{{/* */}}`; `html/template` strips comments, which can join the text around one into a tag). `<noscript>`, `<xmp>`, `<noembed>` and `<noframes>` are refused: the tokenizer reads them as text, but a page without script shows their content as markup. Inside `<svg>`/`<math>` no element's content is treated as raw text and `<style>` text has its entities decoded, as in a browser. A template action may not fill an SVG paint attribute (`fill`, `stroke`, `filter`, `mask`, `clip-path`, `marker-*`, `cursor`, ...), and any attribute that holds `url(` gets the CSS rules below.

### The template functions

The function map is closed, and one leaf package (`internal/artifacts/pagefuncs`) defines it for both the save-time vet and the renderer:

| Function | Returns |
|---|---|
| `query "name"` | `{Columns, Rows, Truncated}` of that manifest query |
| `param "name"` | the bound value of a parameter |
| `params` | the declared parameters, for building controls |
| `link "name" value …` | a URL to this page with those parameters overridden |
| `number`, `percent`, `date`, `bytes`, `truncate` | formatted text (`date` takes `iso`, `short`, `long` or `month`, never a Go layout) |
| `scaleLinear`, `scaleBand`, `pathLine` | numbers and path text, to draw SVG without script |
| `json v` | the value as plain data, for a `<script type="application/json">` block |

**No function returns `template.HTML`, `JS`, `CSS`, `URL`, `HTMLAttr`, `JSStr` or `Srcset`** (nor a type holding one): data is always escaped by context, and a lint test walks every function's result types to keep it so. The builtin `call` is refused, and so is chaining a function result (`{{ call.X }}`). The functions also normalise what an environment hands them (a named string type, or a trusted type hidden in a row, becomes a plain string).

### The save-time vet

Saving a page (the store's `Put`, and every read of one) runs the vet; a failure is the error the author sees.

- Every template parses against the closed function map with `missingkey=error`, and `html/template`'s context escaper is run over every template, so an escaping error is found at save, not at render. The vet **never executes a page**: each template is run through a synthetic root whose first action fails, which is after the escaper has done its work and before anything of the page runs (so `{{ range 3000000000 }}` or a doubling chain of calls costs nothing). The renderer has no such luck and bounds a render itself (see below): a context cannot interrupt an empty `range`.
- `query`, `param` and `link` take **quoted literal names** that the manifest declares (a computed, piped or variable name is refused); `{{template "x"}}` must name a file or a `{{define}}`, and a template name may be defined only once. The queries and parameters a page uses are derived from this (`PageInfo.Queries`, `.Params`).
- **URL attributes are an allow-list.** `href`, `src`, `srcset`, `poster`, `action`, `formaction`, `xlink:href` (and a few older ones) must be a `link` call, a `#fragment`, or a literal relative `assets/` path; a `<script src>` may also name a top-level `.js` of the page. No absolute URL, no templated value beside other text, and no other action. No `target`, `ping`, `download` or `srcdoc` attribute; `<link>` only as `rel="stylesheet"` with a literal `assets/` href; no `<meta http-equiv>`, `<base>`, `<iframe>`, `<frame>`, `<object>`, `<embed>`. CSS (a `style` attribute, a `<style>` element, a `.css` file) may load only `url(assets/…)` or `url(#…)`: no backslash escape, `@import`, or `image-set()`-style function, and no action inside `url()`. This closes the hint and click paths that open a connection under every CSP layer found in the spike.

### Trust hash

`artifacts.PageHash` is what an allow will be keyed by: a SHA-256 over `artifact.yaml`, the derived kind, the declared capabilities, and every file as path, bytes and role, sorted by path, every field length-prefixed (tag `vibe-page-hash/1`). A renamed, added, removed or moved file, a byte moved across a boundary, a role change, or a switch between static and scripted all change it. The stores return it as `Page.Hash` with every read of a page. `ViewHash` (for `view: script`) is unchanged.

### Rendering and the static host

`artifacts.RenderHTMLPage(ctx, run, artifact, values, w)` executes the entry template with its partials against the closed function map. Its `query` runs the manifest's named query **lazily and once per render**, through a `PageRunner` the host provides; Studio's runs the query of the manifest the page was hashed with (`RunManifestQuery`, the same path as `RunArtifactQuery` but over the held manifest, so an edit on disk after the allow is never run), so every page query is vetted SQL at every run, goes through `artifacts.Bind`, is read-only in every tier, capped at 500 rows and 30 seconds, and a failure is a fixed text (never the policy's reason or a path). The template cannot name a connection, write SQL or read a file.

**The renderer bounds itself**, because nothing interrupts a template that loops without calling a function:

- Parsed templates are rewritten before they run so every `range` is charged what it will iterate (a slice or map's length, an integer's value) and every `{{template}}` call one step, against one budget of 2,000,000 per render. `{{ range 3000000000 }}{{ end }}`, nested loops, and a call chain that doubles at every level end in the "took too long" block, with no goroutine left behind. The deadline and a cancel (a revoke, a new token, a workspace switch) are checked before every action in every list, so inside a loop body a render overruns by at most one action.
- Templates may not call themselves, directly or through others: the vet refuses it at save and the renderer again (a call depth bound by the number of templates).
- **Strings are budgeted too.** The builtins that build strings from their arguments (`printf`, `print`, `println`, `js`, `html`, `urlquery`) are replaced, inside a render, by versions that estimate their result from their arguments (the format, every width, each verb repeating the longest argument, six bytes per input byte for the escapers) and refuse *before* allocating if it cannot fit; every string a page function returns (`number`, `percent`, `date`, `bytes`, `truncate`, `pathLine`, `link`, `json`) is charged the same way. Arguments that are not strings (a row set, a row, a map) are sized by walking what they hold, so one large cell counts in full each time it is named. One budget of 64 MB covers a render, and one query result is capped at 16 MB of cell text (over it the query fails with the fixed block; the runner has already read the rows, so this bounds what the template sees, not the read), so a page cannot hold a gigabyte in variables where neither the output cap nor the deadline would see it; going over is the "too large" block. The names and behaviour are the builtins' own (the vet, which never executes a page, uses the plain map).
- A render is limited to 30 s, and the output to 10 MB. A failed render writes nothing: the host sends a small fixed error document instead (a failed query names the query, a parameter error says so, never more).
- The rendered markup is read again with the vet's rules (no script or frame element, no `on*` handler, no `target`/`ping`/`download`, no `<meta http-equiv>`, a `<link>` only as a local stylesheet, a URL attribute only this page's own route, a `#fragment` or `assets/…`) as defence in depth; a scripted page's own script elements are the one allowed difference.

Values come **only from the query string** of the page request. Each key must be a declared parameter, once, and read as its parameter's type; Bind refuses anything out of range or of the wrong shape on every query. `link "name" value …` writes `page?name=value&…` (relative to the page's route, keeping the token path; the current values stay, the named ones are overridden; a name or value the parameter would refuse is a template error).

The host is `studio/viewserver.go` and `viewserver_page.go`, under the same `/vibe-view/` middleware as script views:

| Route | Serves |
|---|---|
| `/vibe-view/<token>/page?<params>` | the rendered page |
| `/vibe-view/<token>/assets/<path>` | one file of `html/assets/` (by exact path, extensions of the closed list; never a template, never a query) |

- **The token is per (Studio process, artifact)** and is issued by `OpenArtifactPage` only for a static page that is allowed right now. The artifact is the token's, never the path's: no path under one token reaches another artifact. A new token for an artifact replaces its older ones, and a revoke or deny drops them at once.
- **Serve what was hashed.** The files are read once, hashed (`PageHash`) and compared with the allowed hash when the token is issued; the page renders those held bytes. The decision is re-checked (the same workspace, allowed, same hash) on use, at most every half second per page token, and again after a render, so a render that overlapped an edit or a revoke is not delivered.
- **Response headers**, on every response under the page routes, errors included: `Content-Security-Policy: sandbox; default-src 'none'; script-src 'none'; style-src <a> 'unsafe-inline'; img-src <a> data:; font-src <a>; connect-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'` (`<a>` is `<origin>/vibe-view/<token>/assets/`; an error without a page of its own gets `sandbox; ` plus the deny policy), `X-Content-Type-Options: nosniff`, `Cache-Control: no-store`. Sub-resources load only from `assets/`, which runs no query, so an `<img>` or a CSS `url()` pointing at `/page` is refused by the CSP.
- **Limits.** At most 4 renders in flight and 60 a minute per token (429 over). The webview sends no `Sec-Fetch-*` header on these requests (measured in WebKitGTK and WKWebView), so the route also refuses a request whose header says it is an image, style, script, font or `fetch` (403) when one is present. A path that ends in `/` or `index.html` is 400 (Wails would inject its runtime), `GET` and `HEAD` only, a query string over 8 KB is refused.
- **Studio shows the page in `<iframe sandbox="">`** (no `allow-scripts`, `allow-same-origin`, `allow-forms`, `allow-popups` or `allow-top-navigation`), with the artifact's level-1 controls above it; a change to a control reloads the frame from the new query string. The frame's bottom edge can be dragged to resize it (a script-less page cannot report its own height). The page links to itself only with `link`: there are no forms.
- **Studio's own page narrows frame navigation** with a one-directive meta CSP, `frame-src <origin>/vibe-view/` (candidate a1 of the base plan). Without it a click on an outside link inside the frame would navigate the frame, and a link to `/` would load Studio's own page into it; with it both load nothing. It is **emitted into `index.html` at build time for the window's origin** (`studio/frontend/scripts/frame-csp.mjs`): `wails://wails` in a built app on Linux and macOS, `http://wails.localhost` on Windows (`VIBE_STUDIO_ORIGIN` overrides it for a cross build), and `wails://wails.localhost:34115` in `task studio:dev`. The dev build also names `http://localhost:34115` (the same server in a browser, for Rodney layout QA); that origin is only ever emitted by the dev server, never by a build. Before it opens a page the frontend checks that the meta is there and names this window's origin, and shows a plain refusal otherwise.
- **Allowing.** The page shows as its files (the manifest and every file under `html/`, read-only, with role and size) until the person allows it, keyed by the artifact folder's path plus `PageHash` in `views.json`, exactly like a script view. Any edit, by anyone, ends the allow and the frame is torn down. The agent has no tool or MCP method to allow, deny or revoke. `vibe artifact view status|allow|deny|revoke <name>` covers pages too (it prints the file list and every text file).

An agent-written or pulled page is untrusted markup under a hard CSP; the data in it is escaped by context. What is still to come: the scripted host (a page window), the capabilities, the full allow prompt, the agent tool and bundle entries (P4 to P6).
