# Reports

A **report** is a banded, print-oriented view over a DuckDB result set — the
Access banded-report homage. Reports are defined as a versioned spec in
`_vibe_reports` and rendered to HTML (and PDF/print) for distribution.

## Bands

A report is composed of ordered **bands**, evaluated against the report's query
result. Each band repeats according to its scope:

| Band | Repeats | Typical content |
|---|---|---|
| **Report header** | Once, at the top | Title, run date, logo. |
| **Page header** | Once per printed page | Column captions. |
| **Group header** | Once per group value | Group label (e.g. "Region: West"). |
| **Detail** | Once per row | The record's field values. |
| **Group footer** | Once per group | Group subtotals/aggregates. |
| **Page footer** | Once per printed page | Page number. |
| **Report footer** | Once, at the bottom | Grand totals. |

## Grouping and sorting

Reports group and sort by one or more columns. Each grouping level gets its own
group header/footer band, enabling running subtotals and the classic Access
grouped-report layout. Aggregates (sum/avg/count/min/max) are computed per group
in the group footer and across all rows in the report footer.

## Live filters and chart/table toggle

In the Studio Report View, filters are applied live against DuckDB and the report
body can toggle between a chart and a table presentation. The underlying query is
editable in Studio Mode only; App Mode users get the locked, parameterized report.
