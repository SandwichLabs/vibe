---
name: task-notebook-cells
description: Write, preview, and commit a notebook cell (SQL or markdown) with write_cell — the workflow for validating a query before it's proposed, picking a view that actually fits the result, and knowing when to ask instead of guess.
---

# Notebook cells

When the task is to add or edit a cell in the user's notebook:

1. `introspect_schema` to see what tables are loaded.
2. `describe_table` on every table you plan to query — column names, types,
   nullability. Don't guess a schema you haven't seen.
3. `run_query` with a small `SELECT ... LIMIT 5` on those tables to see how
   values are actually spelled (`'Fall 2023-2024'` vs `'Fall'`, `'M'` vs
   `'Male'`, etc.) — never invent a value you haven't seen.
4. Draft the SQL.
5. `preview_query` it. Fix and re-preview until it runs clean and returns
   the shape you expect.
6. `write_cell` with that exact SQL.

Never call `write_cell` on SQL you have not run through `preview_query` in
this same turn — not SQL you remember working before, not a copy with one
column swapped. Re-preview after every edit.

A markdown request skips steps 1–5 entirely. If the user asked for prose —
a note, a summary paragraph, a section header — go straight to `write_cell`
with `cell_type: "markdown"`. Marching a markdown request through
`introspect_schema`/`describe_table` wastes steps on a cell that never
touches SQL.

## Editing an existing cell

Preserve the query's intent and structure. Change only what the request
asks for. Keep column aliases — the user's chart or downstream cell may
depend on the exact name. Don't rewrite a working `WHERE` clause into a
different shape just because you'd have written it differently from
scratch. The cell's own type never changes: a `sql` cell stays `sql`, a
`markdown` cell stays `markdown` — there is no cell-type conversion in this
workspace, and proposing the other type would discard the cell's original
content when the user applies it. If the request genuinely wants a
different kind of cell than the one being edited, say so via
`need_more_info` instead of writing a mismatched cell.

## Choosing a view

`preview_query`'s response includes `suggestedView` — the view the actual
result shape fits. Use it unless the user asked for a specific chart type.
The fitting rules it's computed from (identical to the notebook's own view
picker):

- **table** — always fits. Every column, as rows. The safe default when a
  chart would be a guess.
- **bar** / **line** — need at least two columns, and at least one of them
  numeric. First column is the label (even if numeric — `region,
  SUM(revenue)` is still a bar chart), second is the value. `line` is
  preferred over `bar` when the label column reads as a sequence (a name
  like `date`, `month`, `term`, `window`, `year`, `quarter`); otherwise
  it's `bar`. Neither fits a single-column, all-text result, or more than
  25 rows.
- **stat** — needs exactly one row and one number. Two rows, or a result
  with no numeric column, doesn't fit — aggregate or `LIMIT 1` first.

Don't pick `bar`/`line` for a query that returns one row, and don't pick
`stat` for anything that isn't already reduced to a single number.

## Labels

Short and specific: `"Revenue by region"`, not `"Query 1"` or `"Result"`.
The label is how the user finds this cell again later — write it like a
chart title, not a placeholder.

## Markdown cells

`write_cell` accepts `cell_type: "sql"` or `cell_type: "markdown"` —
nothing else. Put a markdown cell's text in the `markdown` field, not
`explanation`. `explanation` is a short aside shown alongside the proposal;
it is not the cell's content, and a markdown cell built with the prose in
`explanation` instead of `markdown` saves as an empty cell.

Dashboards (A2UI pages) are out of scope here — this skill and `write_cell`
only produce `sql` and `markdown` cells. If the request wants a live
multi-panel dashboard, that's a different tool (`create_dashboard`), not a
notebook cell — and it's not available in this mode anyway; say so via
`need_more_info` if asked.

## Truncated and clipped results

`preview_query` always returns one JSON object — `{"rowCount", "columns",
"rows", "truncated", "suggestedView", "viewFit", "note"}`. `truncated` is a
plain boolean; when it's `true`, `rows` holds fewer than `rowCount` and
`note` explains what was cut and what to do about it. Treat `note` as an
instruction, not a suggestion: add a `LIMIT` or aggregate in SQL rather than
reasoning from the visible rows as if they were the whole result.

## When to ask instead of guess

Call `need_more_info` only when the loaded tables genuinely cannot answer
the request — not because the SQL is hard to write. Before calling it, you
should already have run `introspect_schema`/`describe_table` and confirmed
the thing that's missing.

Every question must name the specific table, column, or data source that's
missing and say what having it would let you do:

- Weak: "Which region?"
- Useful: "No loaded table has a `region` column; a store-location export
  would supply one — with it I could split revenue by region instead of
  reporting it as one total."

At most three questions. A vague or generic question ("please clarify your
request") is a rejected call, not a completed one.

## Reporting rules

- Never invent a column, table, or literal value you have not seen in
  `describe_table` or `run_query` output.
- Never `write_cell` SQL that hasn't been `preview_query`'d this turn.
- Never put markdown prose in `explanation` — it belongs in `markdown`.
- Prefer `preview_query`'s `suggestedView` over your own guess.
- Label every cell like a chart title, never "Query 1" / "Result".
