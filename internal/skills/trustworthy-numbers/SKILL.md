---
name: trustworthy-numbers
description: Grounding rules for reporting numbers — every figure must come from a query run this turn, with n and filters stated, and empty results explained rather than guessed at.
---

# Trustworthy numbers

The single most damaging failure mode for a data agent is a confident,
wrong number. A user who catches you inventing a figure once will never
trust a real one again. These rules exist to make that failure structurally
hard to reach.

## Every number needs a query, from THIS turn

Never state a count, a sum, an average, or a claim like "all", "none", "most",
or "every" unless you ran a query in the current turn that proves it.
Recalling a number from earlier in the conversation is not verification —
that earlier query may have used a different filter, a different table, or a
subset the user has since changed. If you need the number again, query
again; it's cheap and it's the only way to be sure it's still true.

This applies to summaries and prose even more than to a one-line answer.
Writing "most customers are in California" *feels* like remembering, but
it's a claim exactly as strong as a bar chart — verify it the same way:

```sql
SELECT state, COUNT(*) FROM customers GROUP BY ALL ORDER BY 2 DESC LIMIT 5;
```

## Always state `n`

A number with no denominator is close to meaningless. "Average order value
is $340" from 3 orders is not the same claim as from 3,000 — say which. When
you report an aggregate, report the row count it was computed over in the
same sentence or the same table.

## State your filters

If a number is scoped — a date range, a status filter, an excluded category
— say so explicitly, in the same breath as the number. "Revenue was $50k
(March, orders with status = 'completed', n=212)" is a trustworthy sentence.
"Revenue was $50k" invites the user to assume it's everything, forever,
which is usually not what the query computed.

## Empty results are information, not silence

A query that returns zero rows is a real answer — "no rows matched" — not a
signal to fall back on a guess or a remembered figure. Say plainly that
nothing matched, and say what you filtered on, so the user can tell whether
the filter was too narrow or the answer is genuinely "there is no such
data". Never paper over an empty result with a plausible-sounding number.

## NULL is not zero

`AVG()` and `SUM()` silently skip NULLs; `COUNT(col)` (vs `COUNT(*)`) does
too. A column that's frequently NULL will make an aggregate look cleaner
than the data actually is. If a meaningful fraction of rows have NULL in a
column you're reporting on, say so — see the `data-profiling` skill for how
to check null rates before you get here.

## When in doubt, show the query

If a number might be surprising or the user might want to check your work,
either show the SQL you ran or call `present_data` to render the underlying
rows/chart inline — a number the user can see the source of is more
trustworthy than one asserted in prose alone.
