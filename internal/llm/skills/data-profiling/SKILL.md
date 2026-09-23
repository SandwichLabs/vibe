---
name: data-profiling
description: Profile an unfamiliar table — shape, types, nulls, and cardinality — before answering questions about it or writing SQL against it.
---

# Data profiling

Before answering any question about a table you haven't already queried this
turn, spend one or two cheap queries understanding its shape. Guessing at
column meaning or spelling from the name alone is how wrong answers happen —
`status` might be `'active'`, `'Active'`, or `1`, and you cannot know which
without looking.

## 1. Shape first

```sql
SUMMARIZE <table>;
```

DuckDB's `SUMMARIZE` gives you, per column, in one query: type, min, max,
approx unique count, null count/percentage, and a few sample-based stats for
numeric columns. This alone usually answers "is this column mostly empty",
"is this numeric or categorical", and "how many distinct values" without
three separate queries. Prefer it over guessing.

## 2. Row count and grain

```sql
SELECT COUNT(*) AS n FROM <table>;
```

Then figure out what one row *is* — a transaction, a person, a
person-per-month snapshot? If there's an obvious id/key column, check
whether it's actually unique:

```sql
SELECT COUNT(*) AS n, COUNT(DISTINCT id) AS distinct_ids FROM <table>;
```

If `n != distinct_ids`, the table has duplicate keys or isn't grained the
way its name suggests — say so before summarizing anything by that id.

## 3. Categorical columns: see the real values

For any column you plan to filter or group by, don't assume the spelling —
look:

```sql
SELECT <col>, COUNT(*) FROM <table> GROUP BY ALL ORDER BY 2 DESC LIMIT 20;
```

This catches: mixed casing (`'CA'` vs `'ca'`), unexpected NULLs mixed with
real categories, a "the field is actually free text, not an enum" surprise,
and whether a column you assumed was low-cardinality actually has hundreds
of values.

## 4. Null rates matter more than they look

A column that's 40% NULL isn't "mostly populated" — it means four in ten
rows have no answer for that field, and any aggregate over it (AVG, SUM)
silently drops those rows rather than treating them as zero. State the null
rate explicitly when a column you're reporting on has one, rather than
letting a query's silent NULL-skipping produce a misleadingly clean number.

## 5. Then, and only then, answer the question

Once you know the shape, write the query that actually answers what was
asked. Profiling is preparation, not the deliverable — don't stop at
`SUMMARIZE` output when the user asked a specific question; use what you
learned to write a correct, correctly-scoped query, and cite the real
numbers it returns (see the `trustworthy-numbers` skill for how to report
them).
