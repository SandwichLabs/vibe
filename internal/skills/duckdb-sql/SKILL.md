---
name: duckdb-sql
description: DuckDB SQL dialect tips — FROM-first, GROUP BY ALL, date_trunc, QUALIFY, and the string/date gotchas that trip up SQL written for other engines.
---

# DuckDB SQL dialect notes

Vibe's query engine is DuckDB. It is broadly Postgres-compatible but has
several conveniences (and a few sharp edges) worth knowing before you write
a query by hand rather than by habit from another engine.

## FROM-first syntax

`SELECT` is optional when you just want everything:

```sql
FROM orders WHERE status = 'shipped';
```

Useful for a quick look, but write full `SELECT ... FROM ...` in anything
you're about to hand back to the user or save as a query — it's clearer to
read later.

## GROUP BY ALL

Instead of repeating every non-aggregated column in GROUP BY, DuckDB lets
you write:

```sql
SELECT region, product, SUM(revenue) AS revenue
FROM sales
GROUP BY ALL;
```

This is the preferred style here — it's harder to get wrong when you add or
remove a column later. Likewise `ORDER BY ALL` sorts by every selected
column left to right.

## Dates and times

- `date_trunc('month', order_date)` buckets a timestamp to the start of its
  month (also `'week'`, `'quarter'`, `'year'`, `'day'`).
- `strftime(order_date, '%Y-%m')` formats a date as a string when you need a
  label, not a truncated date value — don't reach for string slicing.
- DuckDB's CSV auto-detection (used by Vibe's importer) is good but not
  perfect: a date column typed as VARCHAR after import usually means mixed
  formats in the source file. Check with `SUMMARIZE` (see `data-profiling`)
  before assuming a column parses as a date.
- Comparing a DATE to a TIMESTAMP column works, but be explicit about
  inclusive/exclusive range bounds. `ts BETWEEN '2024-01-01' AND
  '2024-01-31'` looks like "all of January" but actually excludes anything
  after midnight on the 31st — `BETWEEN`'s upper bound is an exact instant
  (`'2024-01-31' = '2024-01-31 00:00:00'`), so 99.99% of January 31st is
  silently dropped. Use a half-open range instead: `ts >= '2024-01-01' AND
  ts < '2024-02-01'`.

## QUALIFY — filtering on a window function

DuckDB (like Snowflake) supports `QUALIFY`, which lets you filter on a
window function without a wrapping subquery — e.g. "the latest row per
customer":

```sql
SELECT *
FROM orders
QUALIFY ROW_NUMBER() OVER (PARTITION BY customer_id ORDER BY order_date DESC) = 1;
```

Much shorter than the `SELECT * FROM (SELECT *, ROW_NUMBER() OVER (...) rn
FROM orders) WHERE rn = 1` you'd need on an engine without it.

## String gotchas

- String comparison is case-sensitive by default (`'CA' != 'ca'`). Use
  `LOWER(col) = LOWER(?)` or `ILIKE` when matching user-typed or
  inconsistently-cased data — check with a `GROUP BY` first (see
  `data-profiling`) rather than assuming.
- `||` concatenates; `NULL || anything` is `NULL`, not the other operand —
  use `COALESCE` first if a field might be NULL and you still want the rest
  of the string.
- `LIKE` wildcards are `%` and `_`; there's no implicit anchoring, so
  `LIKE '%foo%'` is a substring search, not equality.

## Sampling and limits

- `LIMIT` for a quick peek: it's cheap because it can stop as soon as it has
  enough rows, but the rows you get are whatever the engine happened to scan
  first (often physically-first-on-disk) — not a random or representative
  cross-section of the table.
- `USING SAMPLE 10%` or `USING SAMPLE 100 ROWS` when you actually need a
  representative subset (e.g. eyeballing the spread of values in a column
  before writing an aggregate) — it scans the table to sample from it, so
  it costs more than `LIMIT`, but the rows you get are spread across the
  data instead of clustered at the start.
- Prefer a `LIMIT` on any exploratory query against a table you haven't
  profiled yet — an unbounded `SELECT *` against an unexpectedly large table
  wastes the row budget for no benefit.
