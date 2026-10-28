# Notebooks & A2UI Reference

## Format version 2 (vibe-app#108)

A notebook carries `schemaVersion: 2`. v2 adds one optional field: a
**`connection`** on `sql` cells and on the `sql`-bound components of `a2ui`
cells (`stat`, `table`, `barChart`, `lineChart` and the KINETIC charts). It names a connection (a
catalog alias matching `^[a-z_][a-z0-9_]{0,62}$`); the cell's `sql` then runs
with that connection as the **default catalog for unqualified table names**,
on a pinned connection (`USE <connection>`). Unset means the workspace's `default_connection`. Catalog-qualified names (`other.main.orders`) work with or without it.

A v1 notebook is a valid v2 notebook with no `connection` set: it loads
unchanged and is stamped v2 the next time it is saved. A notebook written by a
newer Vibe (`schemaVersion` above 2) is refused. A `connection` that names no
known connection on this machine is **kept as written** (cells are never
dropped, queries are never silently retargeted at the default): the cell
fails when it runs with `unknown connection "x"`. A non-string `connection`
is cleared with a warning. The value only ever reaches SQL as a quoted
identifier. Studio's cell header has a **Run on** selector for it (vibe-app#117), and
the agent and imported bundles can set it too.

A **Notebook** is a JSON document, one file per notebook at
`notebooks/<id>.vnb` in the workspace folder (`workspaces.md` has the file
format, atomic writes and conflict handling): a title plus an ordered list of
**cells**. There is no page markup, no directives, and no
inter-cell references — the database is the only shared state. Notebooks
replace the old page system (vibe-app#66/vibe-app#69); this file replaces the retired
`pages.md`.

In Studio, notebooks live in the **Library** (`#/library`, the rail entry that also lists live views and pages; the **Notebooks** chip filters to them). **New** there offers Notebook first; a notebook opens at `#/library/notebook/<id>`, and the old `#/notebooks` paths redirect.

## Cell types

Every cell has an `id` and a `type` of `sql`, `markdown`, or `a2ui`. Every
cell also has `collapsed` (bool, default false): the author's "minimize"
choice. Studio folds the cell to its title row. It is stored in the
notebook.

### sql

A single read-only SQL statement (`source`). Sql cells are the same live,
sandbox-gated read binding an A2UI data-bound component uses: INSERT,
UPDATE, DELETE, and DDL are rejected at run time (the frontend surfaces the
denial as "Notebook cells are read-only queries — use the Query editor to
change data"). A sql cell also carries:

- `view` — how the result renders: `table`, `bar`, `line`, `stat`, or one
  of the KINETIC chart views `area`, `columns`, `rings`, `radar`, `sankey`,
  `heatmap` (each renders the matching A2UI chart from the result's columns
  and rows; see [the chart table](#kinetic-motion-charts) for the shape each
  wants). Studio shows the first four as toolbar buttons and the chart views
  in a **Charts** menu; a view that does not fit the result says why.
- `viewPinned` — when false, the view **auto-fits** to the result shape the
  first time the cell runs (a single scalar value picks `stat`; two numeric
  columns pick `bar`; a time-like first column with a numeric second column
  picks `line`; anything else falls back to `table`). Once a view is
  explicitly chosen (by the user or by an authoring tool that names one),
  `viewPinned` becomes true and auto-fit stops re-deciding it.
- `label` — a short cell title.
- `connection` (v2, optional) — the connection whose catalog is the default
  for unqualified table names in `source`.
- `motion` (optional) — the chart animation, the same closed object an A2UI
  chart's `motion` field takes ([Motion](#motion)). Unset is the default
  spring. Only chart views use it; an invalid value is cleared with a warning,
  never a reason to drop the cell. Studio edits it from the **Motion** row
  under the SQL. An older Vibe drops the field on save.

In Studio the SQL is hidden by default behind a "Show SQL" row, and the
result shows first. Whether it is open is view state and is never stored. A
cell with no SQL, or whose run fails, opens it.

The SQL editor completes connections, schemas, tables, columns and functions
(Ctrl+Space opens the list); the cell's `connection` decides where unqualified
names resolve. See [editor.md](editor.md#completion-in-studio).

### markdown

Plain markdown prose (`source`), rendered with the same renderer the rest of
Studio uses. No directives, no named SQL blocks, no Go template syntax — just
markdown.

### a2ui

An [A2UI](#a2ui-component-reference) page: `{ title, components[] }`. A
single cell can hold a whole small dashboard — multiple components laid out
by `parent` id — rather than one chart per cell.

## Read-only cells

**Outputs are never persisted.** Only a cell's `source`/`view`/`page` is
stored; a sql or a2ui cell's last-run result lives in the Studio UI's memory
only, and is gone the moment the notebook is reopened — every data cell
re-runs live on open. This is deliberate: a notebook stores questions (SQL),
not frozen answers.

Read-only applies to **data access**, not to editing the notebook itself:
you can freely add, reorder, and delete cells, and edit a sql cell's
statement — as long as the statement itself only reads.

## AI authoring

The in-app agent and MCP clients share one tool surface (`internal/agents`).
These tools write notebook content (category `content`, with
`create_or_update_skill`: denied under `read-only`, allowed under
`read-write` and `full`, and asked for like any change when "Ask before
changes" is on; vibe-app#111). Each checks the whole request first and honours
a dry run, so an invalid request is refused with nothing written:

- **`add_query_cell`** — appends a single sql cell. `notebook_id` is
  optional: it appends to that notebook, else to the notebook currently open
  in Studio, else it creates a new notebook titled from `label`. `sql` must be
  one read-only statement (one trailing semicolon is fine; two statements are
  refused, since a cell shows one result), and an unknown `view` is refused
  with the list in `notebooks.Views` instead of being saved as a table.
- **`add_markdown_cell`** — appends a markdown cell (at most 256 KB), or
  inserts it after `after_cell_id`. `notebook_id` works as for
  `add_query_cell`; a new notebook is titled from the first line.
- **`create_dashboard`** — appends a whole A2UI page (multiple components)
  as one a2ui cell, with the same notebook-targeting rules as
  `add_query_cell`. It is a dashboard cell in a notebook; for a saved live
  view, use `write_artifact`. Every `sql` binding is vetted at save the way
  `add_query_cell` vets its SQL (one read-only statement that binds against
  the schema, on the component's own `connection` if it names one); all
  failures come back together, each as `components[i].sql` with the binder's
  error and candidates. The same check runs again each time the cell renders.
  MCP clients read the schema with `read_skill` `a2ui`, a built-in skill
  generated from the component registry (the notebook dashboard types and the
  artifact view types, labelled).

### Reading and editing cells

`get_notebook` returns the notebook's `rev` and every cell as data: its `id`
and `type`; for a sql cell `source`, `view`, `view_pinned`, `label` and
`connection`; for a markdown cell `source`; for an a2ui cell the whole `page`,
in the shape `create_dashboard` and `update_cell` take. `sql` and `markdown`
sources are cut at 4000 characters, and a notebook larger than the tool-result
budget (`RowResultBudgetChars`, 15 000 characters) stops early and lists the
omitted cell ids under `truncated.omitted`; `get_notebook` with `cell_id`
returns that one cell uncut.

Three tools change a notebook's existing cells. Each takes `notebook_id`,
`cell_id` and the `rev` that `get_notebook` returned, and the notebook is
saved with compare-and-write against it: a notebook that changed since
(a person, a git pull, another tool) is refused with "changed since the rev you
sent ... call get_notebook again" and nothing is written. The result carries
the new `rev`.

- **`update_cell`** — sends only the fields that fit the cell's type: `sql`,
  `label`, `view`, `connection` for a sql cell (the SQL is vetted exactly as
  `add_query_cell` vets it, on the cell's connection; a changed `connection`
  re-vets the stored SQL), `markdown` for a markdown cell, `page` (the whole new
  page, validated and its `sql` bindings vetted as `create_dashboard` does) for
  an a2ui cell. A field that does not fit the type is refused, not ignored.
  Setting `view` pins it; an empty `label` clears it.
- **`delete_cell`** — removes the cell.
- **`move_cell`** — `after_cell_id` or `position` (0 = first), exactly one.

Studio reloads an open notebook after any of these (the conflict banner shows
instead when the open copy holds unsaved edits), and merges a plain append in
without disturbing an edit in progress.

`preview_query` (below) is offered in chat and over MCP as well: it runs a
read-only statement and returns the row count, columns, a row sample and the
`suggestedView`, which is how an agent picks the `view` for a cell.

There is no `create_page`/`update_page`/`get_page`/`list_pages` tool — those
were retired with the page system. Asking the agent to "make me a page" or
"build a dashboard" routes to `add_query_cell`/`create_dashboard` instead.

### Task-mode cell authoring

When a notebook cell is authored through the AI-cell dialog ("task mode"),
the agent runs under a stricter loop than ordinary chat: it must call
**`preview_query`** (also available in chat and over MCP; a dry run against the real engine, showing sample rows)
before any terminal action — either **`write_cell`** (commit the proposed
cell) or **`need_more_info`** (ask a clarifying question instead of
guessing). The result handed back to the UI is an envelope built from the
recorded tool calls, never free-form model prose, so a task-mode turn can't
silently hallucinate a result the model never actually ran.

## Export / import

`ExportNotebook` writes a notebook to a `.json` file wrapped as
`{ "kind": "vibe/notebook", "notebook": {...} }`. `ImportNotebook` reads that
same wrapper back — a file with a different `kind` (or no `kind` at all) is
rejected — and re-inserts the notebook through the same whitelist rebuild
(`Coerce`) every load goes through, so a hand-edited or newer-schema file
can't smuggle unexpected fields into the workspace's `notebooks/` folder.

To move notebooks between workspaces or people, use `vibe export notebook <id>`
and `vibe import notebook <file.vnb>` (one notebook, which also accepts the
file Studio's Export writes), or a `.vibe` bundle (`vibe export bundle
--notebooks a,b`, `vibe import <file.vibe>`, or the workspace menu in Studio);
see `workspaces.md`, "Sharing without git". Those replaced the CLI
import/export once planned in #9.
Still planned: HTML/PDF/xlsx snapshot export (vibe-app#73).

## A2UI component reference

A page with parameter controls, a pivot or drill-down, fed by named queries, is a [data artifact](artifacts.md) view, not a notebook cell: those components exist only there (see [Artifact view components](#artifact-view-components)).

A2UI is the `internal/a2ui` package: a closed, validated component schema an
agent (or a hand-authored a2ui cell) uses to describe a small dashboard. Go
validates; Vue renders. **Model output never reaches a style attribute** —
every color is one of a closed enum, and an unknown field on any component is
a validation error, not a silently-ignored one.

### Component types

| Type | Required fields | Optional fields |
|---|---|---|
| `heading` | `text` | `level` |
| `text` | `text` | `muted` |
| `divider` | — | — |
| `stat` | `label` | `value`, `sql`, `connection`, `detail`, `color` |
| `statRow` | — | — (a row container for `stat` children) |
| `table` | — | `columns`, `rows`, `sql`, `connection`, `label` |
| `barChart` | — | `bars`, `sql`, `connection`, `label`, `color`, `motion` |
| `lineChart` | — | `points`, `sql`, `connection`, `label`, `color`, `motion` |
| `areaChart` | — | `columns`, `rows`, `sql`, `connection`, `label`, `motion` |
| `columnChart` | — | `columns`, `rows`, `sql`, `connection`, `label`, `mode`, `sorted`, `motion` |
| `ringChart` | — | `columns`, `rows`, `sql`, `connection`, `label`, `max`, `motion` |
| `radarChart` | — | `columns`, `rows`, `sql`, `connection`, `label`, `max`, `motion` |
| `sankeyChart` | — | `columns`, `rows`, `sql`, `connection`, `label`, `motion` |
| `heatmap` | — | `columns`, `rows`, `sql`, `connection`, `label`, `motion` |
| `streamChart` | — | `columns`, `rows`, `sql`, `connection`, `label`, `refresh`, `window`, `threshold`, `color`, `motion` |
| `list` | `items` | `label`, `color` |
| `code` | `text` | `language` |
| `callout` | `text` | `icon`, `color` |
| `badge` | `text` | `color` |
| `container` | — | `layout`, `gap` (a generic layout wrapper for children) |

Components are a **flat list**, nested by a `parent` id field rather than by
JSON nesting — a `container` or `statRow`'s children point their `parent` at
its id. This is the live source of truth: `internal/a2ui.Components` (and
its presentation order, `internal/a2ui.typeOrder`, used in the agent's system
prompt; the view-only types are the separate `internal/a2ui.ViewComponents`, which the prompt never lists). A docs-consistency test (`internal/docsref`) fails the build if a
component type here goes out of sync with that registry.

### Artifact view components

These types exist **only in a data artifact view** (`artifacts/<name>/view.a2ui.json`, see [`artifacts.md`](artifacts.md)). A notebook `a2ui` cell, a chat `present_data` page or `create_dashboard` that carries one is refused with "only allowed in a data artifact view" (a notebook drops the cell with that warning). They name manifest parameters and queries, so they validate only against a manifest.

| Type | Required fields | Optional fields |
|---|---|---|
| `select` | `param` (an `enum` or `boolean` parameter) | `label` |
| `dateRange` | `start`, `end` (two `date` parameters) | `label` |
| `numberInput` | `param` (a `number` or `integer` parameter) | `label` |
| `textInput` | `param` (a `text` parameter) | `label` |
| `pivot` | `query`, `rows` (1 to 8 column names), `columns`, `value` | `label`, `drill` |

In a view the data-bound types take `query` (a manifest query name) instead of `sql` and `connection`, and `table`, `barChart` and `columnChart` also take `drill` (`{param, column}`). See [`artifacts.md`](artifacts.md).

### Data-bound components

`stat`, `table`, `barChart`, `lineChart` and the seven KINETIC charts accept
either **inline data** (`value`/`rows`/`bars`/`points`, or `columns` + `rows`)
or a live `sql` binding. A `sql`-bound
component re-runs its query every time the page renders — same read-only,
sandbox-gated classifier as a notebook sql cell. An optional `connection`
(v2) sets the default catalog for the binding's unqualified names, exactly as
on a sql cell.

### KINETIC motion charts

Seven animated chart types, ported from the KINETIC motion chart library.
Each takes inline `columns` + `rows` in **exactly the shape its `sql`
returns**, so a page reads the same either way:

| Type | Rows | Draws |
|---|---|---|
| `areaChart` | a label column, then one numeric column per series | smooth (Catmull-Rom) areas over a shared axis; hover to read every series at a label |
| `columnChart` | a label column, then one numeric column per series | vertical columns, `mode` `stacked` (default) or `grouped`; `sorted: true` orders by total |
| `ringChart` | label, value[, max] | up to six concentric progress rings; a row's max falls back to `max`, then 100 |
| `radarChart` | an axis label (3+ rows), then one numeric column per series | a polygon per series, the first solid and the rest dashed ghosts; scaled to `max` or the largest value |
| `sankeyChart` | source, target, weight | ribbons between source and target nodes, thickness by weight, with flowing packets; hover isolates one |
| `heatmap` | row label, column label, value | a grid of cells quantized into four heat levels |
| `streamChart` | label, value (time order) | a ticker of the last `window` values (5–120, default 30), flagged above `threshold`; `refresh` (5–3600 s) re-runs the sql while visible |

The label column is the first column whose values are not numbers (column 0
if all are). Series colors come from the theme's `chart.1` … `chart.6` roles,
never from the page.

### Motion

Every chart (`barChart`, `lineChart` and the seven above) animates in, and
takes an optional, closed `motion` object:

| Field | Values | Default |
|---|---|---|
| `easing` | `spring` (elastic overshoot), `expo` (snappy), `cinema` (ease in-out), `steps` (quantized ticks), `custom` (a damped spring tuned by the next three), `none` (draw the final frame) | `spring` |
| `speed` | 0.25–4 (a multiplier) | 1 |
| `stagger` | `true` cascades values one after another, `false` moves them together | `true` |
| `stiffness` / `damping` / `mass` | 10–500 / 1–100 / 0.1–10 (custom only) | 180 / 13 / 1 |

An unknown field, an unknown easing or an out-of-range number is a validation
error. The values only feed the renderer's easing math and never reach a
style attribute. A chart with animation on shows a replay button. Animation
is off, and charts draw their final frame, when the OS asks for reduced
motion.

### The closed color enum

Every `color` field accepts exactly one of: **`yellow`**, **`black`**,
**`error`**, **`muted`**. There is no free-string color and no hex/RGB
passthrough — a model can only ever pick from this closed set, which is what
keeps model output out of a `style` attribute entirely.

### Size limit

An A2UI page caps at 200 components total; a notebook additionally caps the
**total** number of live (`sql`-bound) components across every a2ui cell in
the notebook at 200, so one pathological notebook can't fire hundreds of
live queries against the file's single DuckDB connection the moment it
opens. Once that budget is exhausted, further data-bound components in later
cells are demoted to static (their `sql` field dropped) rather than the
whole cell being rejected.

---

See also [`templates.md`](templates.md) for the starter notebooks bundled
with each starter template, and [`import-office.md`](import-office.md) for
CSV/XLSX import and table/query export (unrelated to notebook export/import
above, which moves whole notebooks between workspaces).
