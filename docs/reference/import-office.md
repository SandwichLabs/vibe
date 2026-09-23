# Import & Export

## Import

The **Import** view (`ImportView.vue`) brings CSV and XLSX data into a DuckDB
table. There are two paths, matching the two bindings behind them:

### Quick CSV import

The one-step **Import CSV** path (`ImportCSV` binding) delegates parsing and
type inference to DuckDB's CSV sniffer (`read_csv_auto`): numeric, boolean,
and date columns arrive typed, and quoted fields with embedded commas are
handled correctly. Re-importing into an existing table appends rows, matching
columns **by name**.

### Spreadsheet import (CSV or XLSX, with a preview)

Pick a `.csv` or `.xlsx` file (multiple sheets supported for XLSX) and the
view walks:

1. **Preview** (`PreviewSpreadsheet` binding) — detect the sheet list, the
   header row, and suggest a DuckDB type per column from a sample of the data.
2. **Adjust** — pick the sheet, toggle whether the first row is a header,
   override any column's suggested type, and name the target table.
3. **Mode** — `create` a new table, or `append` matching rows into an
   existing one by column name.
4. **Import** (`ImportSpreadsheet` binding) — loads rows into DuckDB inside
   one transaction; a row that violates a constraint (e.g. a duplicate
   primary key) is skipped and counted as rejected rather than failing the
   whole import.

Both paths are non-destructive: a failed or partial import never corrupts
existing tables.

### The skill written on import

Every successful import (CSV or spreadsheet) also writes or refreshes a
`workspace-<table>` Skill (`agents.WriteImportSkill`) with the table's actual
stored shape — row count, column types, null rates, and a few sample values —
so the agent has grounded facts about the new table without needing a model
call to guess at them. This is best-effort: a failure to write the skill logs
a warning but never fails the import itself. See `internal/llm/skills/` and
the Skills view for how skills load.

Foreign keys are not part of import — declare them with plain SQL (an inline
`REFERENCES` on the child column) via the Query Explorer, or ask the agent to
do it with `create_table`.

## Export

There is no single "Office Links" surface — exporting a table or a query
result is a small, focused action next to the data itself:

| Where | Binding(s) | Formats |
|---|---|---|
| Query Explorer result | `ExportQueryResults` | CSV, Parquet |
| A table (Tables view / agent tooling) | `ExportTableCSV`, `ExportTableJSON`, `ExportTableXLSX` | CSV, JSON, XLSX |
| A saved query | `ExportQueryXLSX`, `AnalyzeQueryInExcel` (an XLSX-export alias) | XLSX |
| A notebook | `ExportNotebook` | the notebook's own JSON document |

XLSX export uses DuckDB's `excel` extension (`COPY ... TO '...xlsx' WITH
(FORMAT xlsx)`), falling back to CSV automatically if the extension is
unavailable in a given build. Exports are written to a user-chosen path on
disk; nothing leaves the machine.

Banded reports, and their PDF/Word export, were part of the Access
application layer removed in #68 — a banded report migrates into a notebook
on first open of an old file (a markdown cell + one sql cell over its
source); print/PDF export of a notebook may come later as a notebook
feature, not a report one.
