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
