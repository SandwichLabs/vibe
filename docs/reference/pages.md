# Page Directives

Pages are markdown documents extended with two things: named SQL blocks and
**directives**. A directive is a brace-delimited tag of the form
`{TYPE: ARGS}` that the page engine resolves against a named result set after
the SQL has run. Directives are resolved top-to-bottom and rendered to HTML
(Studio/web) or terminal widgets (TUI).

Every directive token below is registered in `internal/pages/parser.go`
(`knownTypes` plus the layout-block kinds). If the parser stops recognizing
one, the docs-consistency test in `internal/docsref/` fails — so this list is
always current.

## Directive syntax

The text between the braces is parsed as a **YAML flow map**. That gives the
grammar three rules worth knowing before the examples:

1. **Arguments are comma-separated `key: value` pairs.** The first entry is
   the directive type; its value is the *primary argument* — for data
   directives that is the named result set to render, for input filters it is
   the variable name the control registers.

   ```
   {metric: revenue_total, label: "Revenue"}
   {table: query=monthly_sales}
   ```

   The single-token `key=value` form (`query=monthly_sales`,
   `source=totals`) is also accepted and is equivalent. Don't mix bare
   space-separated arguments — `{metric: query=revenue label="Revenue"}` is
   **not** valid YAML and will fail to parse.

2. **Quote values that contain commas, colons, or spaces** — `label:
   "Total Sales"`, `items: "Open,Closed"`.

3. **Each directive must be its own markdown paragraph** — on its own line
   with a blank line before and after. This matters inside layout blocks: the
   opener, every child, and the closer each need blank lines between them,
   or the parser reports an unclosed block.

## SQL blocks and parameters

A fenced code block with the language `sql` and a name becomes a named result
set that directives can reference:

````
```sql monthly_sales
SELECT month, sum(total) AS total FROM orders GROUP BY ALL ORDER BY month
```
````

SQL blocks support **parameter substitution**:

- `${var}` interpolates a variable as a quoted SQL literal.
- `{#if var} … {/if}` keeps the enclosed SQL fragment only when the variable
  is set — the standard way to make a filter optional:

```sql
SELECT * FROM service_calls {#if status_filter}WHERE status = ${status_filter}{/if}
```

Variables come from page frontmatter defaults, URL/query params, and the
input-filter directives below (a control named `status_filter` feeds
`${status_filter}`). When a rendered filter changes, the page re-renders with
the new value threaded through.

## Data display

### `{table:}`
Renders a named result set as a tabular grid. Supports `title`, `subtitle`,
and client-side pagination via `pageSize`.

```
{table: query=monthly_sales}

{table: monthly_sales, title: "Monthly sales", pageSize: 10}
```

### `{metric:}`
A single headline number (big-stat card), optionally with a label, format
code, and comparison delta/sparkline.

```
{metric: revenue_total, label: "Revenue", fmt: usd}
```

### `{value:}`
Inlines a single scalar value from a result set into surrounding prose. Takes
an explicit `source=` (or an inline `sql:`), plus `column` and `fmt`.

```
Total revenue: {value: source=revenue_total, column: revenue, fmt: usd2}.
```

## Charts

All chart directives bind a result set to a Chart.js visualization. The
generic `{chart:}` directive takes a `type:` argument; the typed aliases below
are shorthand for the common types. Multi-series (`series:` or a `y` list),
stacking (`stacked: true`), axis ranges, and reference lines are supported.

### `{chart:}`
Generic chart; pick the visualization with `type:`.

```
{chart: sales, type: bar, x: month, y: total}

{chart: calls, type: bar, x: technician, y: revenue, series: status, stacked: true}
```

### Typed chart aliases
- `{bar_chart:}` — vertical bars.
- `{line_chart:}` — trend lines over an ordered axis.
- `{area_chart:}` — filled line chart.
- `{pie_chart:}` — proportional slices.
- `{doughnut_chart:}` — pie with a hollow center.
- `{scatter_chart:}` — point cloud over two numeric axes.

```
{line_chart: sales, x: month, y: total}

{pie_chart: by_region, value: total, label: region}
```

## Forms and detail

### `{form:}`
Embeds a bound data-entry form (see [`forms.md`](forms.md)) into a page.

```
{form: id=contacts_form}
```

### `{detail:}`
Renders a single record as a read-detail panel (label/value pairs).

```
{detail: current_order}
```

### `{subform:}`
Embeds a child result set bound to the parent record — the master-detail
continuation used inside a `{form:}`.

```
{subform: id=order_items, link=order_id->id}
```

## Interactive controls

These directives emit interactive widgets. Each registers a **variable** named
by its primary argument; the current value is available to SQL blocks as
`${name}` and to macros. Changing a control re-renders the page with the new
value.

### `{button:}`
A clickable button that runs a macro (see [`macros.md`](macros.md)).

```
{button: label: "New Order", macro: new_order}
```

### `{button_group:}`
A set of mutually exclusive segment buttons. Options come from a result set
(`source` + `column`) or a literal `items` list.

```
{button_group: status_seg, source: statuses, column: status}

{button_group: mode_seg, items: "Open,Closed"}
```

### `{dropdown:}`
A select control whose chosen value becomes a bindable parameter.

```
{dropdown: region_filter, source: regions, column: region, title: "Region"}
```

### `{slider:}`
A numeric range control with `min`, `max`, `step`, and `default`.

```
{slider: price_max, min: 0, max: 1000, step: 10, default: 500}
```

### `{text_input:}`
A free-text parameter control.

```
{text_input: search, placeholder: "Search…", title: "Search"}
```

### `{toggle:}`
A boolean on/off control. Checked maps to `"true"` by default (override with
`checked_value` / `unchecked_value`); unchecked removes the parameter.

```
{toggle: active_only, label: "Active only"}
```

## Layout and flow

Layout blocks wrap other paragraphs. The closer has **no colon** — `{/row}`,
not `{/row:}` — and the opener, each child, and the closer must each be their
own paragraph (blank lines between).

### `{if:}` … `{/if}`
Conditionally renders its body. Two condition forms:

- a bare result-set name — the body renders when that result set has rows;
- a variable-equality expression — `{{var}} == 'x'` or `{{var}} != "x"`.

```
{if: "has_overdue"}

You have overdue invoices.

{/if}
```

### `{row:}` … `{/row}`
Lays out its children in a horizontal row (used to place metrics/charts
side-by-side).

```
{row:}

{metric: revenue, label: "Revenue"}

{metric: orders, label: "Orders"}

{/row}
```

### `{stack:}` … `{/stack}`
Stacks its children vertically (the explicit counterpart to `{row:}`).

```
{stack:}

First block.

Second block.

{/stack}
```

### `{callout: type: …}` … `{/callout}`
A highlighted aside. `type` is one of `info`, `warning`, `success`, `error`.

```
{callout: type: info}

Heads up — imports run inside a transaction.

{/callout}
```

### `{tabs:}` … `{/tabs}`
A tab strip. Each `{tab: Label}` marker starts a new pane; everything until
the next marker (or the closer) belongs to that pane.

```
{tabs:}

{tab: Summary}

Summary content.

{tab: Detail}

Detail content.

{/tabs}
```
