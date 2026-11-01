---
name: data-artifacts
description: Author a data artifact with write_artifact - a parameterised, live view (controls, charts, tables, pivot, drill-down) stored as files whose queries re-run when it opens or a parameter changes.
---

# Data artifacts

A data artifact is a computation plus a view, stored in the workspace as
`artifacts/<name>/artifact.yaml` (typed parameters with defaults and NAMED SQL
queries) and `view.a2ui.json` (an A2UI page bound to those queries). It stores
questions, never answers: every query runs when the artifact opens and again
when a parameter changes. You author it with `write_artifact`; you never see
its results (there is no tool that runs one). Try each query with
`run_query` first (with sample values in place of the parameters), then save.

Use an artifact instead of `create_dashboard` when the person needs controls
(a date range, a region picker) or wants to drill into rows. For a one-off
chart in a notebook, `create_dashboard` or `add_query_cell` is simpler.

In Studio's New dialog (and in the requests it drafts for the person) the kinds
have plain names: a **Live view** is the default A2UI view (no `view:` key, or
`view: a2ui`), a **Page** is `view: html`, and a **Chart script** is
`view: script`. When a request names one of them, author that kind.

## The manifest

```
{
  "title": "Regional sales",
  "description": "Revenue by region, with drill-down to customers.",
  "parameters": [
    {"name": "from", "type": "date", "default": "today-90d", "label": "From"},
    {"name": "to", "type": "date", "default": "today"},
    {"name": "region", "type": "enum", "options_query": "regions", "default": ""}
  ],
  "queries": [
    {"name": "regions", "connection": "prod_pg", "sql": "SELECT DISTINCT region FROM sales.orders ORDER BY 1"},
    {"name": "by_region", "connection": "prod_pg",
     "sql": "SELECT region, sum(amount) AS revenue FROM sales.orders WHERE order_date BETWEEN $from AND $to GROUP BY ALL ORDER BY revenue DESC"}
  ]
}
```

- **Names** (parameters and queries): start with a letter, then letters,
  digits and underscores, at most 64 characters, unique. The artifact name
  (the `name` argument) is lowercase letters, digits, `-` and `_`.
- **Every parameter has a `default`**, so the artifact opens with no input.
  Types: `text` (`max_length`, default and cap 1000), `number` and `integer`
  (optional `min`, `max`), `date`, `boolean`, `enum`. An `enum` has either
  static `options` (up to 200) or `options_query` naming a query whose first
  column supplies the options (that query must not use the parameter it feeds);
  `default: ""` reads as "all".
- **Date defaults** are `YYYY-MM-DD` or one of `today`, `today-<n>d` (1 to
  3660), `start-of-month`, `start-of-year`, resolved each time it runs, so "the
  last 90 days" never goes stale. Prefer these over a frozen date.
- **Queries**: up to 20, each with a `name`, SQL, and an optional `connection`
  (see `list_connections`; the name is the catalog, and it sets the default
  catalog for unqualified table names). A storage connection has no catalog
  and cannot be named here.
- Use `$name` where a value belongs. A value is bound as a prepared-statement
  parameter, never pasted into the SQL.

## Rules for the SQL (the same read policy as `run_query`, in every tier)

Each query is vetted when you save and again on every run, whoever wrote the
file. A refused write returns the reason; fix it and save again.

- **Read-only.** No INSERT, UPDATE, DELETE, DDL, COPY, ATTACH, SET, and nothing
  on the policy's never-list. One statement per query.
- **A `$name` only where a value belongs.** Not inside `'...'`, `"..."`,
  `$$...$$` or a comment (the placeholder would be rewritten there too). Write
  `WHERE note LIKE '%' || $q || '%'`, not `LIKE '%$q%'`.
- **No bare `?` and no positional `$1`.**
- **A parameter is never an argument of `query(...)`, `query_table(...)` or a
  file reader** (`read_csv($path)`): a value cannot choose what is read. Files
  are read only as literals inside the workspace `data/` folder, as in
  `run_query`.
- **Dates**: a plain comparison needs no cast (`order_date BETWEEN $from AND
  $to`). In a function or arithmetic, cast: `$from::DATE - INTERVAL 1 DAY`,
  `date_trunc('month', $from::DATE)`. The save-time check binds the defaults,
  so a binder error is reported back to you.
- `LIMIT $n` takes an integer parameter. `ORDER BY $col` does not work: a
  parameter cannot choose a column. Use `CASE`, or one query per ordering.
- At most 500 rows per query are shown; aggregate in SQL.

## The view

`view` is an A2UI page: `{"title": "...", "components": [...]}`, a flat list of
components with unique `id`s, nested by a `parent` id. It is validated
against the manifest, the schema is closed (unknown fields are errors), and no
field is a colour or style.

- **Data comes only from named queries.** A data-bound component (`stat`,
  `table`, `barChart`, `lineChart`, `areaChart`, `columnChart`, `ringChart`,
  `radarChart`, `sankeyChart`, `heatmap`, `streamChart`) takes
  `"query": "<name>"`. `sql` and `connection` are refused in a view. `stat`
  shows the first column of the first row; charts use the label column then
  numeric columns, as for `create_dashboard`.
- **Controls** name a parameter and re-run only the queries that use it:
  `select` (`param`: an enum or boolean), `dateRange` (`start`, `end`: two
  date parameters), `numberInput` (`param`: number or integer), `textInput`
  (`param`: text). Each takes an optional `label`.
- **`pivot`**: `{"type": "pivot", "query": "q", "rows": ["region"], "columns":
  "month", "value": "revenue"}` lays a long-form result (one row per row key
  and column key, already aggregated in SQL) out as a crosstab. It never
  aggregates; use `GROUP BY` (or `ROLLUP` for totals) in the query.
- **Drill-down**: `"drill": {"param": "region", "column": "region"}` on a
  `table`, `barChart`, `columnChart` or `pivot` makes a click set that
  parameter to the row's value, so other queries that use it re-run.
- Other types (`heading`, `text`, `divider`, `statRow`, `container`, `callout`,
  `badge`, `list`, `code`) work as in `create_dashboard`.

## Worked example

Parameters `from`, `to` (dates) and `region` (an enum fed by a query); the
`by_region` query above; and a `customers` query:

```
{"name": "customers", "connection": "prod_pg",
 "sql": "SELECT customer, sum(amount) AS revenue FROM sales.orders WHERE order_date BETWEEN $from AND $to AND ($region = '' OR region = $region) GROUP BY ALL ORDER BY revenue DESC LIMIT 50"}
```

The view:

```
{"title": "Regional sales", "components": [
  {"id": "h", "type": "heading", "text": "Regional sales"},
  {"id": "ctl", "type": "container", "layout": "row"},
  {"id": "dates", "type": "dateRange", "parent": "ctl", "start": "from", "end": "to", "label": "Period"},
  {"id": "pick", "type": "select", "parent": "ctl", "param": "region", "label": "Region"},
  {"id": "bars", "type": "barChart", "query": "by_region", "label": "Revenue by region",
   "drill": {"param": "region", "column": "region"}},
  {"id": "top", "type": "table", "query": "customers", "label": "Top customers"}
]}
```

Call `write_artifact` with `name: "regional-sales"`, that `manifest` and that
`view`.

## What `write_artifact` checks, and what it returns

Everything below is checked when you save, not when the person opens the
artifact, and **all problems come back in one reply** (numbered, at most 20),
so fix them together and send again. Nothing is written until the whole request
is clean.

- **Result columns.** A saved artifact's reply has `queries: [{"name": "...",
  "columns": [{"name": "region", "type": "VARCHAR"}, ...]}]`: what each query
  returns with its parameters at their defaults (read by binding the query with
  a zero-row limit; no data is read). Name columns by their alias
  (`sum(x) AS revenue` is `revenue`).
- **Pivot and drill.** `rows`, `columns`, `value` and `drill.column` must each
  be a column of the component's query; the error names the component
  (`components[3].rows[0]`), the bad name and the columns that exist.
- **Charts.** A chart reads its columns by position, so its query must return
  enough of them: `stat` 1, `barChart`, `lineChart`, `areaChart`, `columnChart`,
  `radarChart`, `ringChart` 2 (a label, then values), `streamChart` 1, `heatmap`
  and `sankeyChart` 3.
- **Pages.** `date` takes a layout *name*, one of `iso`, `short`, `long`,
  `month`, never a Go layout like `"2006-01-02"`. A query row is an array of
  cells, so `{{ index . 0 }}`, not `{{ .region }}` (`$q.Columns` lists the
  names). An inline `<script>` is refused because the page's policy blocks it:
  put the code in a `.js` file (`<script src="app.js">`, which makes it a
  scripted page) and pass data with `<script type="application/json">{{ json
  (query "q") }}</script>`.
- **Script views.** `view.js` must have balanced brackets and closed strings,
  templates and regular expressions, and define `draw`. This is a structural
  check, not a full parse: a logic error still shows when the person allows and
  opens it.
- **Forms.** `view` and `manifest` may be sent as objects or as JSON text. A
  numeric or boolean default (`"default": 0`) is kept as a number or boolean in
  `artifact.yaml` and in `get_artifact`.

## Editing an existing artifact

`list_artifacts`, then `get_artifact` for the one to change. Edit what it
returned and send it back with the `rev` it gave you; if the person changed it
meanwhile the write is refused and you re-read it. Without a `rev`,
`write_artifact` only creates: an existing name is refused, so you never
overwrite a version you did not read.

A query with very long SQL comes back from `get_artifact` with `"sql": null`
and `"sql_omitted": true` (and its length), never a partial copy. To keep such
a query exactly as stored, send `{"name": "<name>", "keep_sql": true}` (plus
its `connection`) instead of `sql`; this needs the `rev`. To change it, send
the full new SQL. Do not send `keep_sql` together with `sql`.

## Script views (`view: script`)

Instead of an A2UI page, a manifest may say `view: script` and send the text of
`view.js` as `view_source` (not `view`) to `write_artifact`. `get_artifact`
returns it as `view_source` (or `view_source_omitted`, and then
`keep_view_source: true` plus the `rev` keeps it as stored). Use it only when no
A2UI component draws what is needed.

The script runs in a sandboxed worker with no network, no files and no DOM,
**only after the person allows it** in Studio. You cannot allow it, and it asks
again after every edit. It has no browser APIs beyond JavaScript itself, no
`eval` or `new Function`, and cannot load anything but the libraries below. Its
whole world is the `assay` object and one function you define:

```
function draw({params, theme, width, height}) {
  return assay.run('by_region', {from: params.from, to: params.to})  // Promise of {columns, rows, truncated}
    .then(res => ({svg: '<svg xmlns="http://www.w3.org/2000/svg" ...>...</svg>'}))
}
```

- `draw` is a global function. It returns, or resolves to, `{svg: "<svg ...>"}`
  (an SVG document, at most 2 MB, shown as an image: no script, no external
  loads, no fonts beyond the system's, so give text a `font-family`) or
  `{a2ui: {title, components}}` (an A2UI page, validated like `view`, so
  `query`-bound components, controls and `pivot` work).
- It runs when the artifact opens and again on every parameter change; `params`
  holds every parameter's current typed value. `width` is the space available
  in pixels and `height` is 400.
- `assay.run(queryName, params)` runs one of THIS artifact's named queries with
  the values you pass (an object of parameter names to strings, numbers or
  booleans; the SQL is the manifest's, never yours) and resolves to `{columns,
  rows, truncated}` (at most 500 rows). It rejects with a short fixed message
  when the query fails; at most 4 run at once and 60 a minute.
- `assay.theme` (also `ctx.theme`) holds plain colour strings to draw with, so
  the chart follows light and dark: `chart.1`..`chart.6` (also `theme.chart`, an
  array), `fg`, `fg.muted`, `bg`, `bg.alt`, `surface`, `border`, `accent`, and
  `type` (`"light"` or `"dark"`). Use them instead of hard-coded colours.
- `assay.lib(name)` loads a pinned library and returns it: `d3-array`,
  `d3-scale`, `d3-shape`, `d3-format`, `d3-time-format`, `d3-geo` (all leave
  their functions on `d3`, so `assay.lib('d3-scale')` returns an object with
  `scaleLinear`, and the other d3 pieces already loaded), `vega` and
  `vega-lite`. Nothing else can be loaded. Vega runs without `eval`: use
  `new vega.View(vega.parse(spec, null, {ast: true}), {expr:
  vega.expressionInterpreter, renderer: 'none'})` and `await view.toSVG()`, and
  `vl.compile(vlSpec).spec` for vega-lite.
- A draw must finish in 30 seconds and must not keep the worker busy for over a
  second without yielding: a loop that runs longer stops the view
  ("This view stopped responding"). Aggregate in SQL; do not loop over thousands
  of rows to build markup you could chart with a few.

The queries and parameters are still the manifest's and are vetted as for any
artifact; Studio builds the parameter controls from the manifest, so the script
draws only the chart.

## HTML pages (`view: html`)

When no A2UI component draws what is needed, and a script view is too small
(it only returns an SVG), a manifest may say `view: html` and send the page's
**text files** as `html_files` (a `{path: text}` object, paths relative to
`html/`) instead of `view`. The page is a Go `html/template` file set stored
at `artifacts/<name>/html/`. Like every artifact it stores questions, never
answers: the template asks for a NAMED query of the manifest and never sees SQL
or a connection.

- **Nothing renders until the person allows it** in Studio (or the CLI). The
  person reads every file first. You cannot allow a page, run it, or see it
  render, and **every edit you make asks again**. Say so in your reply.
- Keep it small and plain. Write the page so a person can read it in a minute:
  the prompt shows them every file.
- A page is **static** (no script: it renders inside Studio, and links re-render
  it with new parameter values) or **scripted** (a `.js` file, a `<script src>`, or
  an `on...=` attribute: it opens in its own window and may use capabilities).
  The kind is derived from the files, not declared.

Files: `index.html.tmpl` (the entry; other `.tmpl` files directly under `html/`
are partials), optional `.js`/`.mjs` directly under `html/`, and under
`assets/` only: `.css`, `.svg`, `.png`, `.jpg`, `.webp`, `.woff2`, `.json`. You
send text files only; a binary asset already stored is kept when you write
with the `rev`. At most 200 files, 1 MB each, 256 KB per template. No
dotfiles, no folder deeper than `assets/<folder>/`. Optional manifest block:
`"html": {"entry": "index.html.tmpl", "window": {"width": 1000, "height": 700},
"capabilities": {"state": {"scope": "local"}, "download": {}, "ask": {}}}`
(window and capabilities are for scripted pages only).

### The template functions (the whole API)

- `query "name"` returns `{Columns, Rows, Truncated}` for that manifest query
  with the current parameter values (at most 500 rows). `Rows` is a list of
  rows; read a cell with `index $row 0`. The name must be a literal.
- `param "name"` is a parameter's current value; `params` lists the declared
  parameters (`Name`, `Type`, `Label`, `Default`, `Options`) for building
  controls.
- `link "name" value ...` is a URL to this page with those parameters set. It
  is the only way to write an `<a href>` that goes anywhere but `#fragment`.
- Formatting: `number v [decimals]`, `percent v [decimals]`, `date v [layout]`,
  `bytes v`, `truncate s n`.
- Drawing: `scaleLinear d0 d1 r0 r1 v`, `scaleBand n i r0 r1` (returns `.Start`
  and `.Width`), `pathLine xs ys` (an SVG path).
- `json v` writes a value as JSON inside a `<script>` block.

There is no function that takes SQL, a path or a URL, none that returns markup,
and no `safeHTML`: everything is escaped by context. Row values are untrusted:
a database row can hold `<script>`.

**URLs.** `href`, `src` and the like may only be a `link` call, a `#fragment`
or a literal relative `assets/...` path. No absolute URLs, no CDN, no
`<link>` except `rel="stylesheet"` with an `assets/` href, no `target`, `ping`
or `download`, no `<meta http-equiv>`, `<base>` or HTML comment (use
`{{/* */}}`). A refused write tells you which rule to fix.

### Static example (a table and an SVG bar chart)

Manifest: `"view": "html"` and two queries, `by_region` (columns `region`,
`revenue`) and `peak` (one row, one column: `SELECT max(revenue) ...`, so the
bars scale to the data). Two files, sent as `html_files`:

```html index.html.tmpl
<!doctype html>
<html>
<head><link rel="stylesheet" href="assets/style.css"></head>
<body>
{{ $rows := (query "by_region").Rows }}
<h1>Revenue by region</h1>
{{ if not $rows }}<p>No rows.</p>{{ else }}
{{ $peak := index (index (query "peak").Rows 0) 0 }}
{{ $n := len $rows }}
<svg viewBox="0 0 400 200" width="400" height="200" role="img" aria-label="Revenue by region">
{{ range $i, $r := $rows }}
{{ $b := scaleBand $n $i 0 400 }}
<rect x="{{ $b.Start }}" y="{{ scaleLinear 0 $peak 180 20 (index $r 1) }}" width="{{ $b.Width }}" height="{{ scaleLinear 0 $peak 0 160 (index $r 1) }}" fill="currentColor"></rect>
<text x="{{ $b.Start }}" y="196" font-size="10">{{ index $r 0 }}</text>
{{ end }}
</svg>
<table>
<tr><th>Region</th><th>Revenue</th></tr>
{{ range $rows }}<tr><td>{{ index . 0 }}</td><td>{{ number (index . 1) 0 }}</td></tr>{{ end }}
</table>
{{ end }}
</body>
</html>
```

```css assets/style.css
body { font-family: system-ui, sans-serif; margin: 1rem; }
td, th { padding: 0.2rem 0.6rem; text-align: left; }
```

Add controls with `{{ range params }}` and `link`: `<a href="{{ link "region"
"EMEA" }}">EMEA</a>` re-renders the page with `region` set to EMEA (a static
page has no forms and no script).

### Scripted example (vega-lite from `assay-lib/`)

A scripted page has no network, so it can load only its own files and the
pinned libraries at `assay-lib/`: `d3-array`, `d3-color`, `d3-format`,
`d3-geo`, `d3-interpolate`, `d3-path`, `d3-scale`, `d3-shape`, `d3-time`,
`d3-time-format`, `vega`, `vega-lite` and `vega-interpreter` (each a
`<script src="assay-lib/NAME.js">`, leaving a global such as `vega` and
`vegaLite`). **Data reaches the script only through a `{{ json }}` block**
(or `assay.run`); there is no inline script and no `eval`.

```html index.html.tmpl
<!doctype html>
<html>
<body>
<div id="chart"></div>
<script type="application/json" id="rows">{{ json (query "by_region") }}</script>
<script src="assay-lib/vega.js"></script>
<script src="assay-lib/vega-lite.js"></script>
<script src="assay-lib/vega-interpreter.js"></script>
<script src="app.js"></script>
</body>
</html>
```

```js app.js
(async () => {
  const res = JSON.parse(document.getElementById('rows').textContent)
  const values = res.Rows.map((r) => ({ region: r[0], revenue: r[1] }))
  const spec = { data: { values }, mark: 'bar', encoding: { x: { field: 'region', type: 'nominal' }, y: { field: 'revenue', type: 'quantitative' } } }
  const view = new vega.View(vega.parse(vegaLite.compile(spec).spec, null, { ast: true }), { expr: vegaInterpreter.expressionInterpreter, renderer: 'none' })
  document.getElementById('chart').innerHTML = await view.toSVG()
  // assay.run asks for another named query with new values: {columns, rows, truncated}.
  // await assay.run('by_region', { from: '2026-01-01' })
})()
```

(Setting `innerHTML` to the SVG vega itself produced is fine; never put a
database value or a `state` value in `innerHTML`: use `textContent`.)

### Capabilities (scripted pages only)

Declare what the page needs in `html.capabilities`; the person sees each one
in the allow prompt, and a page without a declaration gets nothing. Resolve one
with `const st = await assay.use('state')`, which is `null` when it is not
available: **the page must work without it**.

- `state` (`{"scope": "local"}`): `get(key)`, `set(key, value)`, `delete(key)`,
  `list(prefix)`, `onChange(fn)`. JSON values, 64 KB each, 1 MB in all, kept on
  this computer only (never in the folder or git). Its contents are untrusted
  input: show them with `textContent`.
- `download`: `save({filename, type, data})` with `type` one of `csv`, `json`,
  `svg`, `png`, `pdf`, `text`. It opens a save dialog the person confirms, and
  only from a click or key press.
- `ask`: `ask(prompt, {system})` returns `{text}` or `null` (no model
  configured). One completion on the person's own provider, no tools; the
  prompt may carry query results, so the person is told which provider.

A page can never ask for a credential: do not draw a sign-in form. Studio never
asks for one inside a page, and the person is told to treat one as an attack.
