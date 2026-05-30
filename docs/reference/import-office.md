# Import & Office Links

## Import Spreadsheet Wizard

The **Import** Spreadsheet Wizard ingests a messy CSV/XLSX file into a clean
relational DuckDB table — the Access "Import Spreadsheet Wizard" homage. The
wizard walks these steps:

1. **Choose source** — pick a `.csv` or `.xlsx` file (multiple sheets supported
   for XLSX).
2. **Preview & header row** — detect the header row and skip leading junk.
3. **Column types** — infer DuckDB types (INTEGER, DOUBLE, DATE, TEXT, BOOLEAN)
   per column, with manual overrides.
4. **Map columns** — rename columns, drop unwanted ones, choose/append to a target
   table.
5. **Key & relationships** — pick a primary key and optionally link the new table
   to an existing one (writing a `_vibe_relationships` row).
6. **Import** — load rows into DuckDB, reporting rejected/coerced rows.

The wizard is non-destructive: a failed or partial import never corrupts existing
tables.

## Office Links

**Office** Links are the export/round-trip bridges to the Microsoft Office surface
that Access shipped (Analyze It / Publish It / Mail It):

| Link | Produces | Backing macro action |
|---|---|---|
| **Analyze in Excel** | `.xlsx` of the current result set / report | `Export` |
| **Publish to Word / PDF** | `.pdf` (or `.rtf`) of the current report | `Export` |
| **Mail as attachment** | An email with the exported object attached | `EmailObject` |

Office Links operate on the currently open object (form record source, query
result, or report) and reuse the report renderer for fidelity. Exports are written
to a user-chosen path; nothing leaves the machine unless the user mails it.
