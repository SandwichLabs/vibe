# Notebooks & A2UI Reference

A **Notebook** is a JSON document stored in `_vibe_notebooks`: a title plus
an ordered list of **cells**. There is no page markup, no directives, and no
inter-cell references — the database is the only shared state. Notebooks
replace the old page system (#66/#69); this file replaces the retired
`pages.md`.

## Cell types

Every cell has an `id` and a `type` of `sql`, `markdown`, or `a2ui`.

### sql

A single read-only SQL statement (`source`). Sql cells are the same live,
sandbox-gated read binding an A2UI data-bound component uses: INSERT,
UPDATE, DELETE, and DDL are rejected at run time (the frontend surfaces the
denial as "Notebook cells are read-only queries — use the Query editor to
change data"). A sql cell also carries:

- `view` — how the result renders: `table`, `bar`, `line`, or `stat`.
- `viewPinned` — when false, the view **auto-fits** to the result shape the
  first time the cell runs (a single scalar value picks `stat`; two numeric
  columns pick `bar`; a time-like first column with a numeric second column
  picks `line`; anything else falls back to `table`). Once a view is
  explicitly chosen (by the user or by an authoring tool that names one),
  `viewPinned` becomes true and auto-fit stops re-deciding it.
- `label` — a short cell title.

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
Two tools write notebook content (category `content`, gated the same as any
other write — denied under `read-only`/`read-write`, allowed under `full`):

- **`add_query_cell`** — appends a single sql cell. `notebook_id` is
  optional: it appends to that notebook, else to the notebook currently open
  in Studio, else it creates a new notebook titled from `label`.
- **`create_dashboard`** — appends a whole A2UI page (multiple components)
  as one a2ui cell, with the same notebook-targeting rules as
  `add_query_cell`.

There is no `create_page`/`update_page`/`get_page`/`list_pages` tool — those
were retired with the page system. Asking the agent to "make me a page" or
"build a dashboard" routes to `add_query_cell`/`create_dashboard` instead.

### Task-mode cell authoring

When a notebook cell is authored through the AI-cell dialog ("task mode"),
the agent runs under a stricter loop than ordinary chat: it must call
**`preview_query`** (a dry run against the real engine, showing sample rows)
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
can't smuggle unexpected fields into `_vibe_notebooks`.

## A2UI component reference

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
| `stat` | `label` | `value`, `sql`, `detail`, `color` |
| `statRow` | — | — (a row container for `stat` children) |
| `table` | — | `columns`, `rows`, `sql`, `label` |
| `barChart` | — | `bars`, `sql`, `label`, `color` |
| `lineChart` | — | `points`, `sql`, `label`, `color` |
| `list` | `items` | `label`, `color` |
| `code` | `text` | `language` |
| `callout` | `text` | `icon`, `color` |
| `badge` | `text` | `color` |
| `container` | — | `layout`, `gap` (a generic layout wrapper for children) |

Components are a **flat list**, nested by a `parent` id field rather than by
JSON nesting — a `container` or `statRow`'s children point their `parent` at
its id. This is the live source of truth: `internal/a2ui.Components` (and
its presentation order, `internal/a2ui.typeOrder`, used in the agent's system
prompt). A docs-consistency test (`internal/docsref`) fails the build if a
component type here goes out of sync with that registry.

### Data-bound components

`stat`, `table`, `barChart`, and `lineChart` accept either **inline data**
(`value`/`rows`/`bars`/`points`) or a live `sql` binding. A `sql`-bound
component re-runs its query every time the page renders — same read-only,
sandbox-gated classifier as a notebook sql cell.

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
above, which moves whole notebooks between `.vibe` files).
