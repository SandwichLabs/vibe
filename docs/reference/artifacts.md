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

## Authoring with the agent

The agent (Studio chat or an MCP client) writes artifacts with `write_artifact` and reads them with `list_artifacts` and `get_artifact` (see [mcp.md](mcp.md#data-artifact-tools) and [agent-policy.md](agent-policy.md#data-artifacts)); the built-in `data-artifacts` skill (`read_skill`) teaches the format. Every query is held to the agent's read policy when it is saved and again on every run, so an artifact written by hand, by git or by a bundle is held to it too. The tools never return query results.
