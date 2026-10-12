# Import & Export

## Import

One **Import** wizard (`ImportView.vue`, on `internal/importer`) handles both
CSV/TSV and XLSX. It is also the only importer: the Studio bindings
(`SelectImportFile`, `InspectImport`, `ImportFile`) and `vibe import` both
call `importer.Import` on the workspace session — there is no separate
"quick" path and no per-row skip-and-continue behaviour. Every import is
**all-or-nothing**: it runs in one DuckDB transaction, and any unexpected
failure rolls the whole thing back.

### Flow

1. **Pick a file.** The wizard sniffs the *full* file (not a sample) to
   infer a type per column and suggest a primary key, and shows a preview of
   the first 50 rows.
2. **Name the table.** If the name matches an existing table, a mode picker
   appears (see below); otherwise the table is created.
3. **Types and key.** Each column header has a type select; a "Primary key"
   select sits above the grid (create/replace only — append/upsert always use
   the existing table's key). Every override is checked losslessly before
   anything is written — a value that doesn't fit is listed as a bad value
   (row, column, value, expected type), not silently rounded or dropped.
4. **Import.** A live, debounced dry run shows exactly what will happen
   ("Will add 5 new rows, update 3, leave 789 unchanged.") before the click.

### The type list

A closed, friendly list, mirrored between `internal/importer.Types` and
`lib/importTypes.ts`: **Text**, **Integer**, **Decimal** (with a scale, so
money columns land as `DECIMAL(18,2)`), **Number** (a plain double, for
values a decimal can't represent losslessly — scientific notation or more
than 4 decimal places), **Date**, **Date & time**, **True/False**. Whole
numbers infer as Integer; values with up to 4 decimal places infer as
Decimal; anything else numeric-looking but not both of those infers as
Number. A currency-formatted string (`$1,200.50`) stays Text — it is never
silently cleaned.

### Primary key

The suggested key is a column that is unique and non-null across the whole
file, and looks like an identifier (named `id`, ending in `_id`/`Id`, or the
first such candidate). If nothing qualifies, the default is **Add an
auto-number id column**; **No key** is also available, but a table imported
without one can't be edited or have rows deleted in the datasheet — use
**Replace table** on a later import to add a key retroactively.

### The four modes (only when the table already exists)

- **Update matching rows** (upsert) — needs a single-column primary key
  present in both the table and the file. Inserts new keys, updates rows
  whose non-key columns changed, and leaves the rest alone — re-running the
  same file a second time is a no-op. Duplicate or null keys **within the
  file** block the whole import (a silent duplicate-key collapse is exactly
  the kind of quiet data loss this wizard exists to prevent).
- **Replace table** — drops and recreates the table from the wizard's
  current types and key, then reloads it from the file. Blocked when the
  table has an incoming or outgoing foreign key ("`orders` refers to
  `customers`, so `customers` can't be replaced. Use Update matching rows.")
  — dropping the table would either fail outright or silently lose the
  relationship.
- **Add rows** (append) — inserts the file's rows as new rows, matching
  columns by (normalized) name. Blocked if the table has a primary key and
  any file row's key already exists (with the count) — that would create
  in-table duplicates.
- **Import as a new table** — pre-fills the next free name (`customers_2`,
  ...) and proceeds as a fresh create.

Missing/extra columns (file vs. table, matched by name) block append and
upsert until resolved. The mode picker pre-selects the safest available
option, so accepting the default never duplicates rows.

### The skill written on import

Every successful (non-dry-run, non-blocked) import writes or refreshes a
`workspace-<table>` Skill (`agents.WriteImportSkill`) with the table's actual
stored shape — row count, column types, null rates, and a few sample values —
so the agent has grounded facts about the new table without needing a model
call to guess at them. The skill's name is a slug of the table name (letters
and digits lowercased, everything else collapsed to `-`), so `order_items`
becomes `workspace-order-items`; two table names that slug to the same value
get a deterministic disambiguating suffix. An unbound user skill already
sitting at that exact name (not one this importer wrote) is never
overwritten in place — it keeps its name, and the import skill lands at the
hashed-suffix name instead. Writing the skill is best-effort: a failure logs
a warning but never fails the import itself. See `internal/skills/` and
the Skills view for how skills load.

Foreign keys are not part of import — declare them with plain SQL (an inline
`REFERENCES` on the child column) via the Query Explorer, or ask the agent to
do it with `create_table`. Imported tables never have foreign keys of their
own, so append/upsert onto one always uses the plain (non-FK) preflight.

### CLI

```
vibe import <file> [--into [conn.]table] [--workspace DIR]
            [--mode create|replace|append|upsert] [--key COL|auto|none]
            [--sheet S] [--no-header] [--type col=type ...] [--dry-run]
```

`vibe import` runs `importer.Import` on the workspace's DuckDB session, the
same call as the Studio wizard, so an imported table is immediately visible to
`vibe query`, Studio, the agent and MCP (#113 closed #72: the CLI used to write
into the old SQLite envelope).

- **Target.** `--into` is `table`, `conn.table` or `conn.schema.table`. The
  connection defaults to the workspace's `default_connection`. Without
  `--into` the table is named after the file. The connection must be writable
  (`read_only: false`); a read-only one is refused with a pointer to that
  setting.
- **Formats.** CSV, TSV and `.xlsx`. Parquet is not an import format: query it
  in place with `read_parquet('file.parquet')`, or attach it as a connection.
- **Modes.** If the table already exists and `--mode` is omitted, the command
  errors rather than guessing a destructive default. It prints the same
  summary line the wizard shows, or the bad-value list on failure, and exits
  non-zero on anything but a clean, unblocked import. `--dry-run` writes
  nothing (no table, no skill).
- **Ownership.** It takes the workspace's hub lock for the duration, so while
  Studio or `vibe mcp serve` has the workspace open it stops with a message
  naming that process.
- A successful import writes or refreshes the `workspace-<table>` skill into
  `skills/`, as Studio's does.

Notebooks move between workspaces with `vibe export notebook` / `vibe import notebook`, or in `.vibe` bundles (`vibe export bundle`, `vibe import <file.vibe>`); see [workspaces.md](workspaces.md#sharing-without-git-vibe-bundles). That replaced the `vibe notebook import/export` once planned in #9.

## Export

There is no single "Office Links" surface — exporting a table or a query
result is a small, focused action next to the data itself:

| Where | Binding(s) | Formats |
|---|---|---|
| Query Explorer result | `ExportQueryResults` | CSV, Parquet |
| A notebook | `ExportNotebook` | the notebook's own JSON document |

XLSX export (the CLI `vibe export`; Studio has no xlsx export binding) uses DuckDB's `excel` extension (`COPY ... TO '...xlsx' WITH
(FORMAT xlsx)`). Exports are written to a user-chosen path on disk; nothing
leaves the machine. An xlsx export is an xlsx or an error: there is no
silent fallback to CSV.

**Excel support needs a one-time download.** Vibe runs DuckDB with
autoinstall/autoload off and its own private extension directory
(`<user cache dir>/vibe/duckdb/extensions`), so an Excel extension you
installed elsewhere (for example `~/.duckdb`) is not used. The first xlsx
import or export fetches the `excel` extension once (about 2 MB) and needs
network access; if that fails the action reports "Excel support needs a
one-time download ... check your network connection" and changes nothing.
After that it works offline.

Banded reports, and their PDF/Word export, were part of the Access
application layer removed in #68 — a banded report migrates into a notebook
on first open of an old file (a markdown cell + one sql cell over its
source). HTML/PDF/xlsx export of a notebook is tracked as a notebook
feature in #73, not a report one.

### CLI

```
vibe export <[conn.]table> -o file.{csv,parquet,json,xlsx} [--force]
vibe export --sql "<query>" [-c conn] -o file.{csv,parquet,json,xlsx}
vibe export <[conn.]table> [--format csv|json]          # to standard output
```

`vibe export` writes a table or a query's result with DuckDB's `COPY ... TO`
through the workspace session; the format comes from the file's extension. An
existing destination is only replaced with `--force`. Excel needs the `excel`
extension (fetched once, see above). Without `-o` the rows go to standard
output as CSV or JSON. Exporting only reads, so `--read-only` works without
taking the workspace's hub lock.

`vibe query "<sql>" [--format table|csv|json] [--limit N]` prints a result
directly; see `docs/reference/workspaces.md` (Command line).
`vibe notebook import/export` is tracked in #9.
