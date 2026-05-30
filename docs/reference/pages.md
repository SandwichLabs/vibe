# Page Directives

Pages are markdown documents extended with two things: named SQL blocks and
**directives**. A directive is a brace-delimited tag of the form `{ TYPE : ARGS }`, that the page
engine resolves against a named result set after the SQL has run. Directives are
resolved top-to-bottom and rendered to HTML (Studio/web) or terminal widgets (TUI).

Every directive token below is registered in `internal/pages/parser.go`
(`knownTypes`). If the parser stops recognizing one, the docs-consistency test in
`internal/docsref/` fails — so this list is always current.

## Data display

### `{table:}`
Renders a named result set as a tabular grid.

```
{table: query=monthly_sales}
```

### `{metric:}`
A single headline number (big-stat card), optionally with a label and delta.

```
{metric: query=revenue label="Revenue"}
```

### `{value:}`
Inlines a single scalar value from a result set into surrounding prose.

```
Total open tickets: {value: query=open_tickets}.
```

## Charts

All chart directives bind a result set to a Chart.js visualization. The generic
`{chart:}` directive takes a `kind=` argument; the typed aliases below are
shorthand for the common kinds.

### `{chart:}`
Generic chart; pick the visualization with `kind=`.

```
{chart: query=sales kind=bar x=month y=total}
```

### Typed chart aliases
- `{bar_chart:}` — vertical bars.
- `{line_chart:}` — trend lines over an ordered axis.
- `{area_chart:}` — filled line chart.
- `{pie_chart:}` — proportional slices.
- `{doughnut_chart:}` — pie with a hollow center.
- `{scatter_chart:}` — point cloud over two numeric axes.

```
{line_chart: query=sales x=month y=total}
{pie_chart: query=by_region value=total label=region}
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
{detail: query=current_order}
```

### `{subform:}`
Embeds a child result set bound to the parent record — the master-detail
continuation used inside a `{form:}`.

```
{subform: id=order_items child_key=order_id}
```

## Interactive controls

These directives emit interactive widgets whose state is available to macros and
to downstream SQL parameters.

### `{button:}`
A clickable button that runs a macro (see [`macros.md`](macros.md)).

```
{button: label="New Order" macro=new_order}
```

### `{button_group:}`
A set of mutually related buttons rendered together.

```
{button_group: items="Save,Cancel"}
```

### `{dropdown:}`
A select control whose chosen value becomes a bindable parameter.

```
{dropdown: param=region options=region_list}
```

### `{slider:}`
A numeric range control.

```
{slider: param=threshold min=0 max=100}
```

### `{text_input:}`
A free-text parameter control.

```
{text_input: param=search placeholder="Search…"}
```

### `{toggle:}`
A boolean on/off control.

```
{toggle: param=active label="Active only"}
```

## Layout and flow

### `{if:}` … `{/if:}`
Conditionally renders its body when a result set is non-empty (or a parameter is
truthy).

```
{if: query=has_overdue}
  You have overdue invoices.
{/if:}
```

### `{row:}` … `{/row:}`
Lays out its children in a horizontal row (used to place metrics/charts
side-by-side).

```
{row:}
  {metric: query=revenue}
  {metric: query=orders}
{/row:}
```
