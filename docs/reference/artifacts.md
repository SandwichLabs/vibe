# Data artifacts

A **data artifact** is a computation plus a view, stored as plain files in the workspace. It stores questions (SQL and parameters), never answers: every query runs when the artifact opens and again when a parameter changes.

```
artifacts/<name>/
├── artifact.yaml        title, typed parameters with defaults, NAMED queries over them
└── view.a2ui.json       a declarative A2UI view (closed schema, validated)    (view: a2ui, the default)
    or view.js           a custom view, run in a worker after you allow it     (view: script)
```

An artifact has exactly one view file, the one its `view:` names. Both existing, or the wrong one for the kind, makes the folder broken (it lists with an error). There is no fallback A2UI page for a script view: before you allow it, the artifact opens as source. See [Custom views](#custom-views-view-script).

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

**Status.** The trust step is built; the worker host is not (the next update). An allowed view shows "Allowed. The view host arrives in the next update." and runs nothing yet.

**Nothing runs until you allow it.** The code in `view.js` is written by someone else or by the agent, so the artifact opens as **source**: a read-only code view of `view.js` and of `artifact.yaml`, with **Allow** and **Deny** and the line "This view runs code written by someone else or by the agent. It cannot reach the network or your files, and can only run this artifact's queries."

- **What an allow covers.** Allow is keyed by the artifact folder's canonical absolute path (symlinks resolved; on macOS and Windows spelled as the file system spells it) plus the `ViewHash`: a sha256 over a version-tagged (`vibe-view-hash/1`), length-prefixed list, entry 0 `artifact.yaml`, then each view file as (path, bytes) sorted by path. Any change to `artifact.yaml` (a query's SQL or connection included) or to `view.js` ends the allow: the view says it **changed since you allowed it** and asks again. Bytes moved from one file to the other change the hash. A clone of the folder at another path, or a copy under another name, is undecided.
- **What you saw is what you allow.** Allow sends the hash of the bytes on screen, and Go refuses it if the files differ by then. If the files change while you are reading (a reload with a different hash while Allow is armed), Studio keeps showing what you were reading, says "This view changed while you were reading it", and keeps Allow off until you click "Show the new version". The read-only editors draw any remaining control or invisible character as a visible marker, and the CLI prints them as `<U+XXXX>`. The step that runs a view (the next update) serves the bytes it hashed, never a second read.
- **Deny** holds for that folder until revoked, across edits.
- **Where decisions live.** In `views.json` beside `dotenv.json` in your own config directory (the same JSON format as `dotenv.json`). Never in the workspace folder, never in a bundle, never in git. **Settings > Workspace > Custom views** lists them with **Revoke**.
- **Agent edits ask again.** The agent can write a script view (`write_artifact`'s `view_source`) but cannot allow one: there is no tool, MCP method or other path that allows, denies or revokes. Every edit, by anyone, asks again.

**CLI.** `vibe artifact view status|allow|deny|revoke <name>` mirrors the prompt and Settings (as `vibe workspace env` does for `.env`). `status` prints the state, the hash and the source. `allow` shows the source and asks on a terminal; without one it needs `--hash <hash from status> --yes` (you read the source) and is refused if the files changed since. That is scriptable by design: a process that can run your shell can also edit `views.json` directly, so an agent with a shell is outside this feature's threat model. What the feature stops is the agent's tools, MCP and a pulled file allowing a view; it does not stop a program running as you. `vibe artifact list` shows the kind (`script`).

**Sharing.** A script view travels in a bundle as `artifacts/<name>/artifact.yaml` plus `artifacts/<name>/view.js` (the view file must be the one the manifest names), and arrives undecided on the other machine.

**Writing one.** The `data-artifacts` skill describes the worker contract in a few lines: define `draw` (or `run`), call `run(queryName, params)` for rows, return an SVG string or an A2UI page.
