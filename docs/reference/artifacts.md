# Data artifacts

A **data artifact** is a computation plus a view, stored as plain files in the workspace. It stores questions (SQL and parameters), never answers: every query runs when the artifact opens and again when a parameter changes.

```
artifacts/<name>/
├── artifact.yaml        title, typed parameters with defaults, NAMED queries over them
└── view.a2ui.json       a declarative A2UI view (closed schema, validated)
```

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

`artifact.yaml` 256 KB, `view.a2ui.json` 1 MB, 20 queries, 50 parameters, 200 view components.

## The view: `view.a2ui.json`

The view is an [A2UI](notebooks.md#a2ui-component-reference) page, `{ title, components[] }`, validated against its manifest. The schema is closed (unknown fields are errors) and no view field is a color, style or class: labels and column names are shown as text only. Errors are JSON-path-prefixed, as for any A2UI page.

- **Data comes only from named queries.** A data-bound component (`stat`, `table`, and the charts) takes `query: "<name>"` instead of `sql`. `sql` and `connection` are refused in a view (the query holds the connection). `query` must name a query of the manifest. Inline data (`rows`, `bars`, `points`) is still allowed.
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

`{query, rows, columns, value, label?, drill?}` lays out a **long-form** result (one row per row-key and column-key, already aggregated with `GROUP BY` in the query) as a crosstab: `rows` (1 to 8 result column names) are the row headers, `columns` names the column whose values become the column headers, `value` the column shown in each cell. The three roles must use different columns. The component never aggregates: totals come from the SQL (for example `GROUP BY ROLLUP`) and are shown as returned. Two result rows for the same cell is a render error that says to group by those columns. Column names are checked against the result at render, since they are only known after a run.

### Drill-down

`drill: {param, column}` on `table`, `barChart`, `columnChart` and `pivot`: clicking a row sets `param` (any declared parameter) to that row's `column` value, validated like any other value, and re-runs the queries that use it. `column` is checked against the result at render.
